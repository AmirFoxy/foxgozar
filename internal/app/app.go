// Package app is foxgozar's brain: it owns the core process, the firewall and
// the background jobs, and is what the API calls into.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"foxgozar/internal/clash"
	"foxgozar/internal/core"
	"foxgozar/internal/fw"
	"foxgozar/internal/gen"
	"foxgozar/internal/ruleset"
	"foxgozar/internal/state"
	"foxgozar/internal/sub"
)

type Paths struct {
	Etc string // /etc/foxgozar: state.json, rules/
	Bin string // where downloaded cores live
	Run string // /var/run/foxgozar: generated config, cache
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

	slots  []*slot
	active int // the slot new connections go to

	op               sync.Mutex // one start/stop/apply at a time
	mu               sync.Mutex
	phase            string
	message          string
	since            time.Time
	delays           map[string]int // live measurements, persisted now and then
	busy             map[string]bool
	baseUp, baseDown int64 // traffic of instances already stopped
	dev              *devStats
}

func New(p Paths, st *state.Store) *App {
	a := &App{Paths: p, Store: st, Log: core.NewRing(800), phase: Stopped, since: time.Now(),
		delays: map[string]int{}, busy: map[string]bool{}, dev: newDevStats()}
	for i := 0; i < maxSlots; i++ {
		a.slots = append(a.slots, a.newSlot(i))
	}
	return a
}

func (a *App) SingBoxBin() string { return filepath.Join(a.Paths.Bin, "sing-box") }
func (a *App) XrayBin() string    { return filepath.Join(a.Paths.Bin, "xray") }
func (a *App) RuleDir() string    { return filepath.Join(a.Paths.Etc, "rules") }

// act is the slot new connections go to.
func (a *App) act() *slot { return a.slots[a.active] }

// Clash is the API of the instance new connections go to.
func (a *App) Clash() *clash.Client { return a.clientFor(a.act()) }

func (a *App) running() bool { return a.act().sb.Running() }

func (a *App) setPhase(p, msg string) {
	a.mu.Lock()
	if a.phase != p {
		a.since = time.Now()
	}
	a.phase, a.message = p, msg
	a.mu.Unlock()
	if msg != "" {
		a.Log.Add("foxgozar", p+": "+msg)
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
	if a.running() && s.MixedPort > 0 {
		f.Clients = append(f.Clients, core.HTTPClient(s.MixedPort, 2*time.Minute))
	}
	f.Clients = append(f.Clients, core.HTTPClient(0, 2*time.Minute))
	return f
}

// Boot runs once when foxgozar starts.
func (a *App) Boot(ctx context.Context) {
	// Whatever a previous foxgozar left behind points at a core that is gone.
	fw.Down()
	s := a.Store.View().Settings
	if s.Enabled {
		go func() {
			if err := a.Start(ctx); err != nil {
				a.Log.Add("foxgozar", "start at boot: "+err.Error())
			}
		}()
	} else {
		_ = fw.KillSwitch(false, nil)
		if s.KeepWarm {
			go a.Warm()
		}
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
	if _, err := os.Stat(a.SingBoxBin()); err != nil {
		a.setPhase(Installing, "downloading sing-box")
		if err := a.installCore(ctx, core.SingBox); err != nil {
			a.setPhase(Failed, "could not download sing-box: "+err.Error())
			return err
		}
	}
	if _, err := os.Stat(a.XrayBin()); err != nil && gen.HasXray(st.Nodes) {
		a.setPhase(Installing, "downloading Xray")
		if err := a.installCore(ctx, core.Xray); err != nil {
			// Not fatal: the sing-box nodes still work.
			a.Log.Add("foxgozar", "could not download Xray: "+err.Error())
		}
	}
	if missing := a.missingRuleSets(st.Settings); len(missing) > 0 {
		a.setPhase(Installing, "downloading rule-sets")
		if _, err := ruleset.Update(ctx, a.Fetcher(), a.RuleDir(), missing); err != nil {
			// Not fatal: .ir domains still go direct, the rest waits for later.
			a.Log.Add("foxgozar", "rule-sets: "+err.Error())
		}
	}
	a.setPhase(Starting, "")
	// Nothing is routed yet, so an existing warm core may simply reload.
	if err := a.warmLocked(st, false); err != nil {
		a.setPhase(Failed, err.Error())
		return err
	}
	sl := a.act()
	if !core.WaitListening(sl.tproxy(st.Settings), 20*time.Second, sl.sb.Running) {
		msg := "sing-box did not start listening"
		if e := sl.sb.LastError(); e != "" {
			msg += ": " + e
		}
		a.stopSlot(sl)
		a.setPhase(Failed, msg)
		return errors.New(msg)
	}
	if err := fw.Up(a.fwConfig(st, sl)); err != nil {
		a.stopSlot(sl)
		fw.Down()
		a.setPhase(Failed, "firewall: "+err.Error())
		return err
	}
	a.applySelection(ctx, st.Settings)
	a.syncKillSwitch(st.Settings, true)
	a.setPhase(Connected, fmt.Sprintf("up in %d ms", time.Since(t0).Milliseconds()))
	return nil
}

// warmLocked makes the active instance run the current config. With live
// traffic (live == true) a changed config goes to a fresh instance next to
// the running one, which keeps its open connections until they end; with no
// traffic, or too little memory for two, the instance reloads in place.
// Each sing-box exec costs about a second on a 700 MHz router, so a cold
// start skips the separate config check: a bad config exits at once and is
// reported.
func (a *App) warmLocked(st state.State, live bool) error {
	cur := a.act()
	if !cur.sb.Running() {
		return a.startSlot(st, cur)
	}
	if a.configHash(st, cur) == cur.hash {
		return a.syncXray(st, cur)
	}
	if !live || memAvailable() < swapMemNeeded {
		if cur.restart {
			a.stopSlot(cur)
			return a.startSlot(st, cur)
		}
		return a.reloadSlot(st, cur)
	}
	next := a.freeSlot()
	if err := a.startSlot(st, next); err != nil {
		a.stopSlot(next)
		return err
	}
	if !core.WaitListening(next.tproxy(st.Settings), 20*time.Second, next.sb.Running) {
		msg := "the new configuration did not start"
		if e := next.sb.LastError(); e != "" {
			msg += ": " + e
		}
		a.stopSlot(next)
		return errors.New(msg)
	}
	// From here new connections go to next; open ones stay where they are,
	// because TPROXY hands packets of an established connection to the
	// socket that owns it, whatever port the rule names.
	if err := fw.Up(a.fwConfig(st, next)); err != nil {
		a.stopSlot(next)
		return err
	}
	a.active = next.id
	a.applySelection(context.Background(), st.Settings)
	go a.drain(cur)
	return nil
}

// Stop disconnects: the network goes back to normal at once. With KeepWarm
// the core stays up (idle, no traffic through it) so the next Connect is
// instant.
func (a *App) Stop() {
	a.op.Lock()
	defer a.op.Unlock()
	a.stopLocked(!a.Store.View().Settings.KeepWarm)
}

// Shutdown is the service stopping: everything goes, the core included.
func (a *App) Shutdown() {
	a.op.Lock()
	defer a.op.Unlock()
	a.stopLocked(true)
}

func (a *App) stopLocked(full bool) {
	_ = a.Store.Update(func(s *state.State) error { s.Settings.Enabled = false; return nil })
	fw.Down()
	// An instance finishing old connections has nothing left to serve.
	for _, other := range a.slots {
		if other != a.act() && other.sb.Running() {
			a.stopSlot(other)
		}
	}
	if full {
		a.stopSlot(a.act())
	}
	a.persistDelays()
	_ = fw.KillSwitch(false, nil)
	a.setPhase(Stopped, "")
}

// Apply brings the core in line with the saved state. Changes that do not
// alter the core config (choosing a node, names, delays) cost nothing.
func (a *App) Apply(ctx context.Context) error {
	a.op.Lock()
	defer a.op.Unlock()
	st := a.Store.View()
	if !a.running() {
		return nil
	}
	if len(st.Nodes) == 0 {
		a.stopLocked(true)
		return nil
	}
	// A warm core follows every change too, so Connect stays instant.
	if before := a.act(); a.configHash(st, before) != before.hash {
		if err := a.warmLocked(st, st.Settings.Enabled); err != nil {
			a.Log.Add("foxgozar", "new config not applied, the running one stays: "+err.Error())
			return err
		}
		if a.act() != before {
			a.Log.Add("foxgozar", "configuration applied; open connections continue on the previous instance")
			// warmLocked already installed the rules for the new instance.
			a.syncKillSwitch(st.Settings, true)
			return nil
		}
		core.WaitListening(a.act().tproxy(st.Settings), 10*time.Second, a.act().sb.Running)
		a.Log.Add("foxgozar", "configuration reloaded")
	}
	if !st.Settings.Enabled {
		return nil
	}
	if err := fw.Up(a.fwConfig(st, a.act())); err != nil {
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
	cl := a.Clash()
	// The API comes up with the core; give it a moment on a fresh start.
	for i := 0; i < 20; i++ {
		if err := cl.Select(c, gen.TagProxy, target); err == nil {
			return
		} else if i == 19 {
			a.Log.Add("foxgozar", "select: "+err.Error())
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
	if a.running() {
		a.applySelection(ctx, a.Store.View().Settings)
	}
	return nil
}

func (a *App) onCoreExit(sl *slot) {
	a.mu.Lock()
	isActive := a.slots[a.active] == sl
	a.mu.Unlock()
	if !isActive {
		// An instance that was finishing old connections: nothing to restore.
		go func() {
			a.op.Lock()
			defer a.op.Unlock()
			if a.act() != sl {
				a.stopSlot(sl)
			}
		}()
		return
	}
	// The process supervisor restarts the core. Until it listens again the
	// rules would send everything into a closed port, so take them away -
	// unless the kill switch wants devices offline rather than exposed.
	st := a.Store.View()
	if !st.Settings.Enabled {
		return // a warm core: the supervisor restarts it, nothing routes through it
	}
	fw.Down()
	a.syncKillSwitch(st.Settings, false)
	a.setPhase(Starting, "sing-box stopped unexpectedly, restarting")
	go func() {
		if core.WaitListening(sl.tproxy(st.Settings), 60*time.Second, nil) && a.Store.View().Settings.Enabled {
			a.op.Lock()
			defer a.op.Unlock()
			st := a.Store.View()
			if a.act() != sl {
				return
			}
			if err := fw.Up(a.fwConfig(st, sl)); err == nil {
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
		a.Log.Add("foxgozar", "kill switch: "+err.Error())
	}
}

func (a *App) fwConfig(st state.State, sl *slot) fw.Config {
	s := st.Settings
	c := fw.Config{
		TProxyPort: sl.tproxy(s), DNSPort: sl.dns(s), LANInterfaces: s.LANInterfaces,
		ProxyRouter: s.ProxyRouter, IPv6: s.IPv6, CoreMark: gen.RoutingMark,
		FakeIP: s.FakeIP, DefaultDirect: s.DefaultPolicy == "direct",
	}
	for _, d := range st.Devices {
		switch d.Policy {
		case "block":
			c.BlockMACs = append(c.BlockMACs, d.MAC)
		case "direct":
			c.DirectMACs = append(c.DirectMACs, d.MAC)
		case "proxy", "node":
			c.TunnelMACs = append(c.TunnelMACs, d.MAC)
		}
	}
	if s.IranDirect {
		v4, v6, err := ruleset.CIDRs(a.SingBoxBin(), a.RuleDir(), gen.RSIranIP)
		if err == nil {
			c.DirectV4, c.DirectV6 = v4, v6
		} else if !errors.Is(err, os.ErrNotExist) {
			a.Log.Add("foxgozar", "Iran IP list: "+err.Error())
		}
	}
	return c
}

func (a *App) wantedRuleSets(s state.Settings) []string {
	tags := []string{gen.RSIranIP, gen.RSIranSite}
	if s.BlockAds {
		tags = append(tags, gen.RSAds)
	}
	if s.BlockMalware {
		tags = append(tags, gen.RSMalwareIP, gen.RSPhishingIP, gen.RSMiners)
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
	a.Log.Add("foxgozar", fmt.Sprintf("installing %s %s", k, r.Tag))
	_, err = f.Install(ctx, k, r, a.Paths.Bin)
	return err
}

// forceNew makes the next Apply start a fresh instance even though the
// config is the same: a new core binary or new rule-set files.
func (a *App) forceNew() {
	a.op.Lock()
	cur := a.act()
	cur.hash, cur.xrayHash = [32]byte{}, [32]byte{}
	cur.restart = true
	a.op.Unlock()
}

// UpdateCore installs the newest core and moves traffic onto it, the same
// way as any other change: open connections finish on the old binary.
func (a *App) UpdateCore(ctx context.Context, k core.Kind) (string, error) {
	if !a.claim("core-" + string(k)) {
		return "", errors.New("already updating")
	}
	defer a.release("core-" + string(k))
	if err := a.installCore(ctx, k); err != nil {
		return "", err
	}
	if a.running() {
		a.forceNew()
		if err := a.Apply(ctx); err != nil {
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
	if len(updated) > 0 && a.running() {
		a.forceNew() // files changed under the same names
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
	if !a.running() {
		return nil, errors.New("connect first: delays are measured through the core")
	}
	s := a.Store.View().Settings
	c, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	res, err := a.Clash().GroupDelay(c, gen.TagAuto, s.TestURL, 5*time.Second)
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
	if !a.running() {
		return 0, errors.New("connect first")
	}
	d, err := a.Clash().Delay(ctx, id, a.Store.View().Settings.TestURL, 5*time.Second)
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
	CoreRunning bool   `json:"core_running"`
	Draining    bool   `json:"draining,omitempty"` // a previous instance still finishes connections
}

// Totals is the traffic since the service started, across every instance.
func (a *App) Totals(ctx context.Context) (up, down int64, conns int, mem int64) {
	a.mu.Lock()
	up, down = a.baseUp, a.baseDown
	a.mu.Unlock()
	for _, sl := range a.slots {
		if !sl.sb.Running() {
			continue
		}
		c, cancel := context.WithTimeout(ctx, 2*time.Second)
		cs, err := a.clientFor(sl).Connections(c)
		cancel()
		if err != nil {
			continue
		}
		up += cs.UploadTotal
		down += cs.DownloadTotal
		conns += len(cs.Connections)
		mem += cs.Memory
	}
	return
}

func (a *App) Status(ctx context.Context) Status {
	st := a.Store.View()
	ph, msg, since := a.Phase()
	s := Status{Phase: ph, Message: msg, Since: since.Unix(), Enabled: st.Settings.Enabled,
		Mode: st.Settings.Mode, Selected: st.Settings.Selected, Nodes: len(st.Nodes), CoreRunning: a.running(),
		Draining: a.draining() > 0}
	if ph == Connected && a.running() {
		c, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if px, err := a.Clash().Proxies(c); err == nil {
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
		s.Upload, s.Download, s.Conns, s.Memory = a.Totals(ctx)
	}
	return s
}

// loop runs the background jobs.
func (a *App) loop(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	var n int
	failures := 0
	var lastRuleTry time.Time
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
		if st.Settings.Enabled && a.running() {
			c, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := a.Clash().Ping(c)
			cancel()
			if err != nil {
				failures++
				if failures >= 3 {
					a.Log.Add("foxgozar", "core not answering, restarting it")
					failures = 0
					a.op.Lock()
					fw.Down()
					a.stopSlot(a.act())
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
					a.Log.Add("foxgozar", "subscription "+s.Name+": "+err.Error())
				}
			}
		}
		// Rule-sets daily: the upstream lists are rebuilt every day. A failed
		// attempt waits an hour rather than retrying every minute.
		if ruleset.Age(a.RuleDir(), gen.RSIranIP) > 24*time.Hour && time.Since(lastRuleTry) > time.Hour {
			lastRuleTry = time.Now()
			if err := a.UpdateRuleSets(ctx); err != nil {
				a.Log.Add("foxgozar", "rule-sets: "+err.Error())
			}
		}
		// Measurements reach flash only twice an hour.
		if n%30 == 0 {
			a.collectDelays(ctx)
			a.persistDelays()
		}
	}
}

// collectDelays copies what the urltest group measured on its own.
func (a *App) collectDelays(ctx context.Context) {
	if !a.running() {
		return
	}
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	px, err := a.Clash().Proxies(c)
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
	b, err := os.ReadFile(a.act().configPath(a.Paths.Run))
	if err != nil {
		return json.RawMessage(`null`)
	}
	return b
}

// Warm starts the core without routing anything through it, so that the
// first Connect after a boot is as quick as the rest.
func (a *App) Warm() {
	a.op.Lock()
	defer a.op.Unlock()
	st := a.Store.View()
	if len(st.Nodes) == 0 || st.Settings.Enabled {
		return
	}
	if _, err := os.Stat(a.SingBoxBin()); err != nil {
		return
	}
	if err := a.warmLocked(st, false); err != nil {
		a.Log.Add("foxgozar", "warm start: "+err.Error())
	}
}

func (a *App) missingRuleSets(s state.Settings) []string {
	var out []string
	for _, tag := range a.wantedRuleSets(s) {
		if ruleset.Age(a.RuleDir(), tag) > 1<<61 {
			out = append(out, tag)
		}
	}
	return out
}

// EnsureRuleSets downloads the rule-sets the settings now ask for and do
// not have yet, so turning on an option takes effect right away instead of
// at the next daily update.
func (a *App) EnsureRuleSets(ctx context.Context) error {
	missing := a.missingRuleSets(a.Store.View().Settings)
	if len(missing) == 0 {
		return nil
	}
	_, err := ruleset.Update(ctx, a.Fetcher(), a.RuleDir(), missing)
	return err
}
