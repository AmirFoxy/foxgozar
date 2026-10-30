package app

import (
	"bufio"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"foxgozar/internal/clash"
	"foxgozar/internal/core"
	"foxgozar/internal/gen"
	"foxgozar/internal/state"
)

// A slot is one complete core instance: sing-box, Xray when needed, and
// their own internal ports and files. Two slots exist so a new config can
// be brought up next to the running one and take over new connections,
// while connections already open finish on the old one. A reload inside a
// single sing-box closes every connection; this does not.
type slot struct {
	id       int
	sb, xr   *core.Process
	hash     [32]byte // config the running sing-box was started with
	xrayHash [32]byte
	// The core binary changed: an in-place reload would keep the old one.
	restart bool

	client     *clash.Client
	clientPort int
	clientTok  string
}

// How long an old instance may keep serving the connections it already has.
const drainLimit = 15 * time.Minute

func (a *App) newSlot(id int) *slot {
	run := a.Paths.Run
	cfg := filepath.Join(run, fmt.Sprintf("sing-box-%d.json", id))
	xcfg := filepath.Join(run, fmt.Sprintf("xray-%d.json", id))
	name := fmt.Sprintf("sing-box/%d", id+1)
	sl := &slot{id: id}
	sl.sb = &core.Process{
		Name: name, Bin: a.SingBoxBin(), Log: a.Log,
		Args:  []string{"run", "-c", cfg, "-D", run, "--disable-color"},
		Check: []string{"check", "-c", cfg},
	}
	sl.sb.OnExit = func(error) { a.onCoreExit(sl) }
	sl.xr = &core.Process{
		Name: fmt.Sprintf("xray/%d", id+1), Bin: a.XrayBin(), Log: a.Log,
		Args:  []string{"run", "-c", xcfg},
		Check: []string{"run", "-test", "-c", xcfg},
	}
	return sl
}

// Ports: slot 0 uses the configured ones, slot n the nth port above them.
// Xray's local SOCKS ports start 2000 apart.
func (sl *slot) tproxy(s state.Settings) int { return s.TProxyPort + sl.id }
func (sl *slot) dns(s state.Settings) int    { return s.DNSPort + sl.id }
func (sl *slot) clashPort(s state.Settings) int {
	return s.ClashPort + sl.id
}
func (sl *slot) xrayBase() int { return gen.XrayBasePort + 2000*sl.id }

func (sl *slot) configPath(run string) string {
	return filepath.Join(run, fmt.Sprintf("sing-box-%d.json", sl.id))
}

func (a *App) clientFor(sl *slot) *clash.Client {
	st := a.Store.View()
	port := sl.clashPort(st.Settings)
	a.mu.Lock()
	defer a.mu.Unlock()
	if sl.client == nil || sl.clientPort != port || sl.clientTok != st.Token {
		sl.client, sl.clientPort, sl.clientTok = clash.New(port, st.Token), port, st.Token
	}
	return sl.client
}

func (a *App) envFor(st state.State, sl *slot) gen.Env {
	return gen.Env{
		RuleDir:    a.RuleDir(),
		CacheFile:  filepath.Join(a.Paths.Run, fmt.Sprintf("cache-%d.db", sl.id)),
		Secret:     st.Token,
		TProxyPort: sl.tproxy(st.Settings),
		DNSPort:    sl.dns(st.Settings),
		ClashPort:  sl.clashPort(st.Settings),
		XrayBase:   sl.xrayBase(),
	}
}

// configHash ignores what does not need new config: delays and names.
func (a *App) configHash(st state.State, sl *slot) [32]byte {
	for i := range st.Nodes {
		st.Nodes[i].Delay, st.Nodes[i].TestedAt, st.Nodes[i].Name = 0, 0, ""
	}
	b, _ := gen.Build(st, a.envFor(st, sl))
	x, _ := gen.BuildXray(st, sl.xrayBase())
	return sha256.Sum256(append(b, x...))
}

// writeSlot writes the configs for sl.
func (a *App) writeSlot(st state.State, sl *slot) error {
	b, err := gen.Build(st, a.envFor(st, sl))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(a.Paths.Run, 0o755); err != nil {
		return err
	}
	return state.WriteFileAtomic(sl.configPath(a.Paths.Run), b, 0o600)
}

// syncXray runs Xray in sl when there are nodes only Xray speaks, and stops
// it when there are none. Xray has no in-place reload: it restarts only when
// its own nodes change.
func (a *App) syncXray(st state.State, sl *slot) error {
	if !gen.HasXray(st.Nodes) {
		sl.xr.Stop()
		sl.xrayHash = [32]byte{}
		return nil
	}
	if _, err := os.Stat(a.XrayBin()); err != nil {
		a.Log.Add("foxgozar", "Xray is not installed yet; its nodes are skipped until it is")
		return nil
	}
	b, err := gen.BuildXray(st, sl.xrayBase())
	if err != nil {
		return err
	}
	h := sha256.Sum256(b)
	if h == sl.xrayHash && sl.xr.Running() {
		return nil
	}
	if err := state.WriteFileAtomic(filepath.Join(a.Paths.Run, fmt.Sprintf("xray-%d.json", sl.id)), b, 0o600); err != nil {
		return err
	}
	if sl.xr.Running() {
		if err := sl.xr.Validate(); err != nil {
			return fmt.Errorf("config rejected by Xray: %w", err)
		}
		sl.xr.Stop()
	}
	sl.xrayHash = h
	return sl.xr.Start()
}

// startSlot brings sl up cold with the current state.
func (a *App) startSlot(st state.State, sl *slot) error {
	sl.restart = false
	if err := a.syncXray(st, sl); err != nil {
		return err
	}
	if err := a.writeSlot(st, sl); err != nil {
		return err
	}
	if err := sl.sb.Start(); err != nil {
		return err
	}
	sl.hash = a.configHash(st, sl)
	return nil
}

// reloadSlot changes sl's config in place. Open connections through it are
// closed; used only when nothing is routed through it, or memory is short.
func (a *App) reloadSlot(st state.State, sl *slot) error {
	if err := a.syncXray(st, sl); err != nil {
		return err
	}
	if err := a.writeSlot(st, sl); err != nil {
		return err
	}
	if err := sl.sb.Validate(); err != nil {
		return fmt.Errorf("config rejected by sing-box: %w", err)
	}
	if err := sl.sb.Reload(); err != nil {
		return err
	}
	sl.hash = a.configHash(st, sl)
	time.Sleep(150 * time.Millisecond)
	return nil
}

func (a *App) stopSlot(sl *slot) {
	sl.sb.Stop()
	sl.xr.Stop()
	sl.hash, sl.xrayHash = [32]byte{}, [32]byte{}
}

// drain lets old finish the connections it has, then stops it.
func (a *App) drain(old *slot) {
	c := a.clientFor(old)
	deadline := time.Now().Add(drainLimit)
	var up, down int64
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		if !old.sb.Running() {
			break
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		cs, err := c.Connections(ctx)
		cancel()
		if err != nil {
			break
		}
		up, down = cs.UploadTotal, cs.DownloadTotal
		if len(cs.Connections) == 0 {
			break
		}
	}
	a.op.Lock()
	defer a.op.Unlock()
	if a.slots[a.active] == old || !old.sb.Running() {
		return
	}
	a.stopSlot(old)
	a.mu.Lock()
	a.baseUp += up
	a.baseDown += down
	a.mu.Unlock()
	a.Log.Add("foxgozar", fmt.Sprintf("previous core instance finished its connections and stopped (%s)", time.Since(deadline.Add(-drainLimit)).Round(time.Second)))
}

// memAvailable in bytes, from /proc/meminfo; 0 when unknown.
func memAvailable() int64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 2 && fields[0] == "MemAvailable:" {
			kb, _ := strconv.ParseInt(fields[1], 10, 64)
			return kb * 1024
		}
	}
	return 0
}

// A second sing-box (and Xray) take roughly 60-80 MB while both run.
const swapMemNeeded = 120 << 20

// Up to this many instances run at once: the active one, and older ones
// still finishing their connections after quick successive changes.
const maxSlots = 3

// freeSlot returns a stopped slot for a new instance. When every other
// slot is still finishing connections, the one with the fewest open gives
// way: usually one that only carried short requests, rather than the one
// holding a long download or a video.
func (a *App) freeSlot() *slot {
	var pick *slot
	fewest := -1
	for _, sl := range a.slots {
		if sl == a.act() {
			continue
		}
		if !sl.sb.Running() {
			return sl
		}
		n := 0
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if cs, err := a.clientFor(sl).Connections(ctx); err == nil {
			n = len(cs.Connections)
		}
		cancel()
		if fewest < 0 || n < fewest {
			pick, fewest = sl, n
		}
	}
	a.Log.Add("foxgozar", fmt.Sprintf("many changes in a row; closing %d connections left on an older instance", fewest))
	a.stopSlot(pick)
	return pick
}

// draining counts older instances still finishing connections.
func (a *App) draining() int {
	n := 0
	for _, sl := range a.slots {
		if sl != a.act() && sl.sb.Running() {
			n++
		}
	}
	return n
}
