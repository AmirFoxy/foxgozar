package app

import (
	"context"
	"fmt"
	"os"
	"time"

	"foxgozar/internal/core"
	"foxgozar/internal/gen"
	"foxgozar/internal/state"
	"foxgozar/internal/warp"
)

// How many WARP endpoints run side by side. About one in four gets through
// the filtering on a given try, so six usually leaves at least one.
const warpEndpoints = 6

// EnsureWarp gets the backup ready when it has just been turned on: an
// account, endpoints to try, and Xray to carry them.
func (a *App) EnsureWarp(ctx context.Context) error {
	st := a.Store.View()
	if !st.Settings.WarpBackup || (st.Warp != nil && len(st.Warp.Endpoints) > 0) {
		return nil
	}
	var acc warp.Account
	var err error
	if st.Warp != nil && st.Warp.Account.PrivateKey != "" {
		acc = st.Warp.Account
	} else {
		for _, c := range a.Fetcher().Clients {
			if acc, err = warp.Register(ctx, c); err == nil {
				break
			}
		}
		if err != nil {
			return fmt.Errorf("could not register with WARP: %w", err)
		}
		a.Log.Add("foxgozar", "WARP account registered")
	}
	if _, e := os.Stat(a.XrayBin()); e != nil {
		if err := a.installCore(ctx, core.Xray); err != nil {
			return fmt.Errorf("WARP needs Xray, which could not be downloaded: %w", err)
		}
	}
	return a.Store.Update(func(s *state.State) error {
		s.Warp = &state.Warp{Account: acc, Endpoints: warp.Candidates(warpEndpoints)}
		return nil
	})
}

// WarpStatus is what the settings page shows about the backup.
type WarpStatus struct {
	Enabled    bool           `json:"enabled"`
	Registered bool           `json:"registered"`
	Active     bool           `json:"active"` // carrying traffic right now
	Endpoints  map[string]int `json:"endpoints"`
	Working    int            `json:"working"`
}

func (a *App) WarpStatus(ctx context.Context) WarpStatus {
	st := a.Store.View()
	ws := WarpStatus{Enabled: st.Settings.WarpBackup, Registered: st.Warp != nil, Endpoints: map[string]int{}}
	a.mu.Lock()
	ws.Active = a.onWarp
	a.mu.Unlock()
	if !gen.WarpOn(st) || !a.running() {
		return ws
	}
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	px, err := a.Clash().Proxies(c)
	if err != nil {
		return ws
	}
	for i, ep := range st.Warp.Endpoints {
		d := px[gen.WarpTag(i)].LastDelay()
		ws.Endpoints[ep] = d
		if d > 0 {
			ws.Working++
		}
	}
	return ws
}

// refreshWarp measures the endpoints and replaces those that do not answer
// with new random ones. A config change, so it goes through a new core
// instance and cuts nothing.
func (a *App) refreshWarp(ctx context.Context) (working int, err error) {
	st := a.Store.View()
	if !gen.WarpOn(st) || !a.running() {
		return 0, nil
	}
	// What the core measured before this check: an endpoint is replaced only
	// when it failed both times, since one lost handshake proves little.
	prev := map[string]int{}
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	if px, e := a.Clash().Proxies(c); e == nil {
		for i := range st.Warp.Endpoints {
			if h := px[gen.WarpTag(i)].History; len(h) > 0 {
				prev[gen.WarpTag(i)] = h[len(h)-1].Delay
			} else {
				prev[gen.WarpTag(i)] = 1 // not measured yet: give it a chance
			}
		}
	}
	cancel()
	c, cancel = context.WithTimeout(ctx, 15*time.Second)
	res, err := a.Clash().GroupDelay(c, gen.TagWarp, st.Settings.TestURL, 8*time.Second)
	cancel()
	if err != nil {
		return 0, err
	}
	fresh := warp.Candidates(warpEndpoints * 3)[1:] // the default one is always filtered here
	var eps []string
	for i, ep := range st.Warp.Endpoints {
		tag := gen.WarpTag(i)
		if res[tag] > 0 {
			working++
		}
		if res[tag] > 0 || prev[tag] > 0 {
			eps = append(eps, ep)
		} else if len(fresh) > 0 {
			eps = append(eps, fresh[0])
			fresh = fresh[1:]
		}
	}
	changed := false
	for i := range eps {
		changed = changed || eps[i] != st.Warp.Endpoints[i]
	}
	if !changed {
		return working, nil
	}
	a.Log.Add("foxgozar", fmt.Sprintf("WARP: %d of %d endpoints answer; replacing the ones that failed twice", working, len(eps)))
	if err := a.Store.Update(func(s *state.State) error {
		if s.Warp != nil {
			s.Warp.Endpoints = eps
		}
		return nil
	}); err != nil {
		return working, err
	}
	return working, a.Apply(ctx)
}

func anyOK(res map[string]int) bool {
	for _, d := range res {
		if d > 0 {
			return true
		}
	}
	return false
}

// warpWatch moves traffic onto WARP when every server has failed, and back
// as soon as one answers again. Switching is a selector change through the
// core's API: instant, no new instance.
func (a *App) warpWatch(ctx context.Context) {
	// Every 10 s: two failed checks in a row and a failed test of every
	// server move traffic within about 30 s of an outage.
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	var n, fails int
	// The first check waits: new endpoints need their handshakes first.
	lastRefresh := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		n++
		st := a.Store.View()
		a.mu.Lock()
		on := a.onWarp
		a.mu.Unlock()
		if !st.Settings.Enabled || !a.running() || !gen.WarpOn(st) {
			if on {
				a.setWarp(false)
			}
			fails = 0
			continue
		}
		if a.rescuing() {
			continue // settings are being tried; judge the servers after
		}
		cl := a.Clash()
		if on {
			if n%6 != 0 { // servers are checked once a minute while on WARP
				continue
			}
			c, cancel := context.WithTimeout(ctx, 12*time.Second)
			res, err := cl.GroupDelay(c, gen.TagAuto, st.Settings.TestURL, 6*time.Second)
			cancel()
			if err == nil && anyOK(res) {
				a.setWarp(false)
				a.op.Lock()
				a.applySelection(ctx, a.Store.View().Settings)
				a.op.Unlock()
				a.Log.Add("foxgozar", "a server answers again; traffic is back on the servers")
			}
			continue
		}
		// Endpoints filtered since the last look are replaced every half hour,
		// so the backup is ready before it is needed.
		if time.Since(lastRefresh) > 30*time.Minute {
			lastRefresh = time.Now()
			if _, err := a.refreshWarp(ctx); err != nil {
				a.Log.Add("foxgozar", "WARP check: "+err.Error())
			}
		}
		// Is the server in use alive?
		c, cancel := context.WithTimeout(ctx, 8*time.Second)
		px, err := cl.Proxies(c)
		cancel()
		if err != nil {
			continue
		}
		now := px[gen.TagProxy].Now
		if gen.IsGroup(now) {
			now = px[now].Now
		}
		c, cancel = context.WithTimeout(ctx, 8*time.Second)
		d, err := cl.Delay(c, now, st.Settings.TestURL, 6*time.Second)
		cancel()
		if err == nil && d > 0 {
			fails = 0
			continue
		}
		if fails++; fails < 2 {
			continue // one lost request is not an outage
		}
		c, cancel = context.WithTimeout(ctx, 12*time.Second)
		res, err := cl.GroupDelay(c, gen.TagAuto, st.Settings.TestURL, 6*time.Second)
		cancel()
		if err == nil && anyOK(res) {
			fails = 0 // others work; urltest moves to them by itself
			continue
		}
		c, cancel = context.WithTimeout(ctx, 15*time.Second)
		wres, err := cl.GroupDelay(c, gen.TagWarp, st.Settings.TestURL, 8*time.Second)
		cancel()
		if err != nil || !anyOK(wres) {
			// New endpoints mean a new core instance: not more than every two
			// minutes, however long the outage lasts.
			if time.Since(lastRefresh) > 2*time.Minute {
				a.Log.Add("foxgozar", "every server failed, and no WARP endpoint answers either; trying new endpoints")
				lastRefresh = time.Now()
				_, _ = a.refreshWarp(ctx)
			}
			continue
		}
		c, cancel = context.WithTimeout(ctx, 3*time.Second)
		err = cl.Select(c, gen.TagProxy, gen.TagWarp)
		cancel()
		if err == nil {
			a.setWarp(true)
			a.Log.Add("foxgozar", "every server failed; traffic now goes over WARP until one answers again")
		}
	}
}

func (a *App) setWarp(on bool) {
	a.mu.Lock()
	a.onWarp = on
	a.mu.Unlock()
}
