// Package app is zgzd's brain: it owns the core process, the firewall and
// the background jobs, and is what the API calls into.
package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"zirgozar/internal/clash"
	"zirgozar/internal/core"
	"zirgozar/internal/fw"
	"zirgozar/internal/gen"
	"zirgozar/internal/ruleset"
	"zirgozar/internal/state"
	"zirgozar/internal/sub"
)

type Paths struct {
	Etc string // /etc/zirgozar: state.json, rules/
	Bin string // where downloaded cores live
	Run string // /var/run/zirgozar: generated config, cache
}

const (
	Stopped    = "stopped"
	Starting   = "starting"
	Connected  = "connected"
	Failed     = "failed"
	Installing = "installing"
)

type App struct {
	Paths Paths
	Store *state.Store
	Log   *core.Ring

	sb    *core.Process
	clash *clash.Client

	op       sync.Mutex // one start/stop/apply at a time
	mu       sync.Mutex
	phase    string
	message  string
	since    time.Time
	lastHash [32]byte
	delays   map[string]int // live measurements, persisted now and then
	busy     map[string]bool
}

func New(p Paths, st *state.Store) *App {
	a := &App{Paths: p, Store: st, Log: core.NewRing(800), phase: Stopped, since: time.Now(),
		delays: map[string]int{}, busy: map[string]bool{}}
	s := st.View()
	cfg := filepath.Join(p.Run, "sing-box.json")
	a.sb = &core.Process{
		Name: "sing-box", Bin: a.SingBoxBin(), Log: a.Log,
		Args:  []string{"run", "-c", cfg, "-D", p.Run, "--disable-color"},
		Check: []string{"check", "-c", cfg},
	}
	a.sb.OnExit = func(error) { a.onCoreExit() }
	a.clash = clash.New(s.Settings.ClashPort, s.Token)
	return a
}

func (a *App) SingBoxBin() string { return filepath.Join(a.Paths.Bin, "sing-box") }
func (a *App) RuleDir() string    { return filepath.Join(a.Paths.Etc, "rules") }

func (a *App) setPhase(p, msg string) {
	a.mu.Lock()
	if a.phase != p {
		a.since = time.Now()
	}
	a.phase, a.message = p, msg
	a.mu.Unlock()
	if msg != "" {
		a.Log.Add("zgzd", p+": "+msg)
	}
}

func (a *App) Phase() (string, string, time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.phase, a.message, a.since
}

// Fetcher downloads through the tunnel when it is up, then directly.
func (a *App) Fetcher() *core.Fetcher {
	s := a.Store.View().Settings
	f := &core.Fetcher{Mirror: s.GitHubMirror}
	if a.sb.Running() && s.MixedPort > 0 {
		f.Clients = append(f.Clients, core.HTTPClient(s.MixedPort, 2*time.Minute))
	}
	f.Clients = append(f.Clients, core.HTTPClient(0, 2*time.Minute))
	return f
}

// Boot runs once when zgzd starts.
func (a *App) Boot(ctx context.Context) {
	// Whatever a previous zgzd left behind points at a core that is gone.
	fw.Down()
	s := a.Store.View().Settings
	if s.Enabled {
		go func() {
			if err := a.Start(ctx); err != nil {
				a.Log.Add("zgzd", "start at boot: "+err.Error())
			}
		}()
	} else {
		_ = fw.KillSwitch(false, nil)
	}
	go a.loop(ctx)
}

// Start brings the tunnel up. It is quick when the core and rule-sets are
// already on disk: write config, start, wait for the socket, one nft call.
func (a *App) Start(ctx context.Context) error {
	a.op.Lock()
	defer a.op.Unlock()
	t0 := time.Now()
	_ = a.Store.Update(func(s *state.State) error { s.Settings.Enabled = true; return nil })
	st := a.Store.View()
	a.syncKillSwitch(st.Settings, false)
	if len(st.Nodes) == 0 {
		a.setPhase(Failed, "no nodes: add a config or a subscription first")
		return errors.New("no nodes")
	}
	if core.Version(a.SingBoxBin()) == "" {
		a.setPhase(Installing, "downloading sing-box")
		if err := a.installCore(ctx, core.SingBox); err != nil {
			a.setPhase(Failed, "could not download sing-box: "+err.Error())
			return err
		}
	}
	if st.Settings.IranDirect && ruleset.Age(a.RuleDir(), gen.RSIranIP) > 1<<61 {
		a.setPhase(Installing, "downloading Iran rule-sets")
		if _, err := ruleset.Update(ctx, a.Fetcher(), a.RuleDir(), a.wantedRuleSets(st.Settings)); err != nil {
			// Not fatal: .ir domains still go direct, the rest waits for later.
			a.Log.Add("zgzd", "rule-sets: "+err.Error())
		}
	}
	a.setPhase(Starting, "")
	if err := a.writeConfig(st); err != nil {
		a.setPhase(Failed, err.Error())
		return err
	}
	if err := a.sb.Validate(); err != nil {
		a.setPhase(Failed, "config rejected by sing-box: "+err.Error())
		return err
	}
	if err := a.sb.Start(); err != nil {
		a.setPhase(Failed, err.Error())
		return err
	}
	if !core.WaitListening(st.Settings.TProxyPort, 20*time.Second, a.sb.Running) {
		a.sb.Stop()
		msg := "sing-box did not start listening"
		if e := a.sb.LastError(); e != "" {
			msg += ": " + e
		}
		a.setPhase(Failed, msg)
		return errors.New(msg)
	}
	if err := fw.Up(a.fwConfig(st.Settings)); err != nil {
		a.sb.Stop()
		fw.Down()
		a.setPhase(Failed, "firewall: "+err.Error())
		return err
	}
	a.applySelection(ctx, st.Settings)
	a.syncKillSwitch(st.Settings, true)
	a.setPhase(Connected, fmt.Sprintf("up in %d ms", time.Since(t0).Milliseconds()))
	return nil
}

func (a *App) Stop() {
	a.op.Lock()
	defer a.op.Unlock()
	a.stopLocked()
}

func (a *App) stopLocked() {
	_ = a.Store.Update(func(s *state.State) error { s.Settings.Enabled = false; return nil })
	fw.Down()
	a.sb.Stop()
	a.persistDelays()
	_ = fw.KillSwitch(false, nil)
	a.setPhase(Stopped, "")
}

// Apply brings a running tunnel in line with the saved state. Changes that
// do not alter the core config (choosing a node, names, delays) cost nothing;
// others are a reload inside the running core, not a restart.
func (a *App) Apply(ctx context.Context) error {
	a.op.Lock()
	defer a.op.Unlock()
	st := a.Store.View()
	if !st.Settings.Enabled || !a.sb.Running() {
		return nil
	}
	if len(st.Nodes) == 0 {
		a.stopLocked()
		return nil
	}
	h := a.configHash(st)
	if h != a.lastHash {
		if err := a.writeConfig(st); err != nil {
			return err
		}
		if err := a.sb.Validate(); err != nil {
			a.Log.Add("zgzd", "new config rejected, keeping the running one: "+err.Error())
			return fmt.Errorf("config rejected: %w", err)
		}
		// Rules stay in place: packets wait in the kernel for the few
		// hundred ms the core needs to reopen its sockets.
		if err := a.sb.Reload(); err != nil {
			return err
		}
		time.Sleep(150 * time.Millisecond)
		core.WaitListening(st.Settings.TProxyPort, 10*time.Second, a.sb.Running)
		a.Log.Add("zgzd", "configuration reloaded")
	}
	if err := fw.Up(a.fwConfig(st.Settings)); err != nil {
		return err
	}
	a.applySelection(ctx, st.Settings)
	a.syncKillSwitch(st.Settings, true)
	return nil
}

func (a *App) applySelection(ctx context.Context, s state.Settings) {
	target := gen.TagAuto
	if s.Mode == "manual" && s.Selected != "" {
		target = s.Selected
	}
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	// The API comes up with the core; give it a moment on a fresh start.
	for i := 0; i < 20; i++ {
		if err := a.clash.Select(c, gen.TagProxy, target); err == nil {
			return
		} else if i == 19 {
			a.Log.Add("zgzd", "select: "+err.Error())
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Select switches node instantly through the core API.
func (a *App) Select(ctx context.Context, id string) error {
	err := a.Store.Update(func(s *state.State) error {
		if id == "" || id == gen.TagAuto {
			s.Settings.Mode, s.Settings.Selected = "auto", ""
			return nil
		}
		if s.NodeByID(id) == nil {
			return fmt.Errorf("no such node")
		}
		s.Settings.Mode, s.Settings.Selected = "manual", id
		return nil
	})
	if err != nil {
		return err
	}
	if a.sb.Running() {
		a.applySelection(ctx, a.Store.View().Settings)
	}
	return nil
}

func (a *App) onCoreExit() {
	// The process supervisor restarts the core. Until it listens again the
	// rules would send everything into a closed port, so take them away -
	// unless the kill switch wants devices offline rather than exposed.
	st := a.Store.View()
	fw.Down()
	a.syncKillSwitch(st.Settings, false)
	a.setPhase(Starting, "sing-box stopped unexpectedly, restarting")
	go func() {
		if core.WaitListening(st.Settings.TProxyPort, 60*time.Second, nil) && a.Store.View().Settings.Enabled {
			a.op.Lock()
			defer a.op.Unlock()
			st := a.Store.View()
			if err := fw.Up(a.fwConfig(st.Settings)); err == nil {
				a.applySelection(context.Background(), st.Settings)
				a.syncKillSwitch(st.Settings, true)
				a.setPhase(Connected, "recovered")
			}
		}
	}()
}

func (a *App) syncKillSwitch(s state.Settings, tunnelUp bool) {
	on := s.Enabled && s.KillSwitch && !tunnelUp
	if err := fw.KillSwitch(on, s.LANInterfaces); err != nil {
		a.Log.Add("zgzd", "kill switch: "+err.Error())
	}
}

func (a *App) env(st state.State) gen.Env {
	return gen.Env{RuleDir: a.RuleDir(), CacheFile: filepath.Join(a.Paths.Run, "cache.db"), Secret: st.Token}
}

func (a *App) writeConfig(st state.State) error {
	b, err := gen.Build(st, a.env(st))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(a.Paths.Run, 0o755); err != nil {
		return err
	}
	if err := state.WriteFileAtomic(filepath.Join(a.Paths.Run, "sing-box.json"), b, 0o600); err != nil {
		return err
	}
	a.lastHash = a.configHash(st)
	return nil
}

// configHash ignores what does not need a reload: delays and names.
func (a *App) configHash(st state.State) [32]byte {
	for i := range st.Nodes {
		st.Nodes[i].Delay, st.Nodes[i].TestedAt, st.Nodes[i].Name = 0, 0, ""
	}
	b, _ := gen.Build(st, a.env(st))
	return sha256.Sum256(b)
}

func (a *App) fwConfig(s state.Settings) fw.Config {
	c := fw.Config{
		TProxyPort: s.TProxyPort, DNSPort: s.DNSPort, LANInterfaces: s.LANInterfaces,
		ProxyRouter: s.ProxyRouter, IPv6: s.IPv6, CoreMark: gen.RoutingMark,
		ACLMode: s.ACLMode, FakeIP: s.FakeIP,
	}
	for _, d := range s.Devices {
		c.MACs = append(c.MACs, d.MAC)
	}
	if s.IranDirect {
		v4, v6, err := ruleset.CIDRs(a.SingBoxBin(), a.RuleDir(), gen.RSIranIP)
		if err == nil {
			c.DirectV4, c.DirectV6 = v4, v6
		} else if !errors.Is(err, os.ErrNotExist) {
			a.Log.Add("zgzd", "Iran IP list: "+err.Error())
		}
	}
	return c
}

func (a *App) wantedRuleSets(s state.Settings) []string {
	tags := []string{gen.RSIranIP, gen.RSIranSite}
	if s.BlockAds {
		tags = append(tags, gen.RSAds)
	}
	return tags
}

func (a *App) installCore(ctx context.Context, k core.Kind) error {
	s := a.Store.View().Settings
	f := a.Fetcher()
	r, err := f.Latest(ctx, k, s.CoreChannel == "beta")
	if err != nil {
		return err
	}
	a.Log.Add("zgzd", fmt.Sprintf("installing %s %s", k, r.Tag))
	_, err = f.Install(ctx, k, r, a.Paths.Bin)
	return err
}

// UpdateCore installs the newest core and, if the tunnel is up, restarts it.
func (a *App) UpdateCore(ctx context.Context, k core.Kind) (string, error) {
	if !a.claim("core-" + string(k)) {
		return "", errors.New("already updating")
	}
	defer a.release("core-" + string(k))
	if err := a.installCore(ctx, k); err != nil {
		return "", err
	}
	if k == core.SingBox && a.sb.Running() {
		a.op.Lock()
		fw.Down()
		a.sb.Stop()
		a.op.Unlock()
		if err := a.Start(ctx); err != nil {
			return "", err
		}
	}
	return core.Version(filepath.Join(a.Paths.Bin, string(k))), nil
}

func (a *App) UpdateRuleSets(ctx context.Context) error {
	if !a.claim("rules") {
		return errors.New("already updating")
	}
	defer a.release("rules")
	updated, err := ruleset.Update(ctx, a.Fetcher(), a.RuleDir(), a.wantedRuleSets(a.Store.View().Settings))
	if len(updated) > 0 && a.sb.Running() {
		a.lastHash = [32]byte{} // force the reload: files changed under the same names
		if e := a.Apply(ctx); e != nil && err == nil {
			err = e
		}
	}
	return err
}

// UpdateSub refreshes one subscription and applies it.
func (a *App) UpdateSub(ctx context.Context, id string) error {
	if !a.claim("sub-" + id) {
		return errors.New("already updating")
	}
	defer a.release("sub-" + id)
	view := a.Store.View()
	s := view.SubByID(id)
	if s == nil {
		return errors.New("no such subscription")
	}
	nodes, info, _, err := sub.Fetch(ctx, a.Fetcher(), *s)
	_ = a.Store.Update(func(st *state.State) error {
		ss := st.SubByID(id)
		if ss == nil {
			return nil
		}
		ss.UpdatedAt = time.Now().Unix()
		if err != nil {
			ss.LastError = err.Error()
			return nil
		}
		ss.LastError = ""
		ss.NodeCount = len(nodes)
		ss.Upload, ss.Download, ss.Total, ss.Expire = info.Upload, info.Download, info.Total, info.Expire
		sub.Merge(st, id, nodes)
		return nil
	})
	if err != nil {
		return err
	}
	return a.Apply(ctx)
}

func (a *App) claim(k string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.busy[k] {
		return false
	}
	a.busy[k] = true
	return true
}

func (a *App) release(k string) {
	a.mu.Lock()
	delete(a.busy, k)
	a.mu.Unlock()
}

// TestAll measures every node through the running core, in parallel.
func (a *App) TestAll(ctx context.Context) (map[string]int, error) {
	if !a.sb.Running() {
		return nil, errors.New("connect first: delays are measured through the core")
	}
	s := a.Store.View().Settings
	c, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	res, err := a.clash.GroupDelay(c, gen.TagAuto, s.TestURL, 5*time.Second)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	for _, n := range a.Store.View().Nodes {
		if d, ok := res[n.ID]; ok && d > 0 {
			a.delays[n.ID] = d
		} else {
			a.delays[n.ID] = -1
		}
	}
	out := make(map[string]int, len(a.delays))
	for k, v := range a.delays {
		out[k] = v
	}
	a.mu.Unlock()
	return out, nil
}

func (a *App) TestOne(ctx context.Context, id string) (int, error) {
	if !a.sb.Running() {
		return 0, errors.New("connect first")
	}
	d, err := a.clash.Delay(ctx, id, a.Store.View().Settings.TestURL, 5*time.Second)
	a.mu.Lock()
	if err != nil || d <= 0 {
		a.delays[id] = -1
	} else {
		a.delays[id] = d
	}
	a.mu.Unlock()
	return d, err
}

func (a *App) persistDelays() {
	a.mu.Lock()
	d := a.delays
	a.delays = map[string]int{}
	a.mu.Unlock()
	if len(d) == 0 {
		return
	}
	now := time.Now().Unix()
	_ = a.Store.Update(func(s *state.State) error {
		for i := range s.Nodes {
			if v, ok := d[s.Nodes[i].ID]; ok {
				s.Nodes[i].Delay, s.Nodes[i].TestedAt = v, now
			}
		}
		return nil
	})
}

// Delays returns the latest known delay of every node.
func (a *App) Delays() map[string]int {
	out := map[string]int{}
	for _, n := range a.Store.View().Nodes {
		if n.Delay != 0 {
			out[n.ID] = n.Delay
		}
	}
	a.mu.Lock()
	for k, v := range a.delays {
		out[k] = v
	}
	a.mu.Unlock()
	return out
}

type Status struct {
	Phase       string `json:"phase"`
	Message     string `json:"message,omitempty"`
	Since       int64  `json:"since"`
	Enabled     bool   `json:"enabled"`
	Mode        string `json:"mode"`
	Selected    string `json:"selected,omitempty"`
	Current     string `json:"current,omitempty"` // node actually carrying traffic
	CurrentName string `json:"current_name,omitempty"`
	Delay       int    `json:"delay,omitempty"`
	Nodes       int    `json:"nodes"`
	Upload      int64  `json:"upload"`
	Download    int64  `json:"download"`
	Conns       int    `json:"connections"`
	Memory      int64  `json:"memory"`
	SingBox     string `json:"sing_box,omitempty"`
}

func (a *App) Status(ctx context.Context) Status {
	st := a.Store.View()
	ph, msg, since := a.Phase()
	s := Status{Phase: ph, Message: msg, Since: since.Unix(), Enabled: st.Settings.Enabled,
		Mode: st.Settings.Mode, Selected: st.Settings.Selected, Nodes: len(st.Nodes)}
	if ph == Connected && a.sb.Running() {
		c, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if px, err := a.clash.Proxies(c); err == nil {
			now := px[gen.TagProxy].Now
			if now == gen.TagAuto {
				now = px[gen.TagAuto].Now
			}
			s.Current = now
			s.Delay = px[now].LastDelay()
			if n := st.NodeByID(now); n != nil {
				s.CurrentName = n.Name
			}
		}
		if cs, err := a.clash.Connections(c); err == nil {
			s.Upload, s.Download, s.Conns, s.Memory = cs.UploadTotal, cs.DownloadTotal, len(cs.Connections), cs.Memory
		}
	}
	return s
}

func (a *App) Clash() *clash.Client { return a.clash }

// loop runs the background jobs.
func (a *App) loop(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	var n int
	failures := 0
	for {
		select {
		case <-ctx.Done():
			a.persistDelays()
			return
		case <-tick.C:
		}
		n++
		st := a.Store.View()
		// Health: the core answers its API, or it is restarted.
		if st.Settings.Enabled && a.sb.Running() {
			c, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := a.clash.Ping(c)
			cancel()
			if err != nil {
				failures++
				if failures >= 3 {
					a.Log.Add("zgzd", "core not answering, restarting it")
					failures = 0
					a.op.Lock()
					fw.Down()
					a.sb.Stop()
					a.op.Unlock()
					go a.Start(ctx)
				}
			} else {
				failures = 0
			}
		}
		// Subscriptions on their own schedules.
		for _, s := range st.Subs {
			if sub.Due(s, time.Now()) {
				if err := a.UpdateSub(ctx, s.ID); err != nil {
					a.Log.Add("zgzd", "subscription "+s.Name+": "+err.Error())
				}
			}
		}
		// Rule-sets weekly.
		if ruleset.Age(a.RuleDir(), gen.RSIranIP) > 7*24*time.Hour && a.Phase0() == Connected {
			if err := a.UpdateRuleSets(ctx); err != nil {
				a.Log.Add("zgzd", "rule-sets: "+err.Error())
			}
		}
		// Measurements reach flash only twice an hour.
		if n%30 == 0 {
			a.collectDelays(ctx)
			a.persistDelays()
		}
	}
}

func (a *App) Phase0() string {
	p, _, _ := a.Phase()
	return p
}

// collectDelays copies what the urltest group measured on its own.
func (a *App) collectDelays(ctx context.Context) {
	if !a.sb.Running() {
		return
	}
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	px, err := a.clash.Proxies(c)
	if err != nil {
		return
	}
	a.mu.Lock()
	for _, n := range a.Store.View().Nodes {
		if p, ok := px[n.ID]; ok && len(p.History) > 0 {
			if d := p.LastDelay(); d > 0 {
				a.delays[n.ID] = d
			} else {
				a.delays[n.ID] = -1
			}
		}
	}
	a.mu.Unlock()
}

// ConfigJSON is the generated core config, for the diagnostics page.
func (a *App) ConfigJSON() json.RawMessage {
	b, err := os.ReadFile(filepath.Join(a.Paths.Run, "sing-box.json"))
	if err != nil {
		return json.RawMessage(`null`)
	}
	return b
}
