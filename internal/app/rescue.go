package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"foxgozar/internal/gen"
	"foxgozar/internal/state"
)

// Rescue: when every server stops answering, filtering has often changed
// rather than the servers. People then try TLS fragment and multiplex on
// and off by hand until something gets through. FoxGozar does the same by
// itself: each combination in turn, kept if a server answers with it, and
// put back as it was if none does. WARP (when on) takes over only after.

type rescueCombo struct{ Fragment, Mux bool }

func (c rescueCombo) String() string {
	onOff := func(b bool) string {
		if b {
			return "on"
		}
		return "off"
	}
	return "TLS fragment " + onOff(c.Fragment) + ", multiplex " + onOff(c.Mux)
}

// rescueCombos are the combinations to try from the current one: the one
// closest to it (one switch changed) first.
func rescueCombos(cur rescueCombo) []rescueCombo {
	return []rescueCombo{
		{!cur.Fragment, cur.Mux},
		{cur.Fragment, !cur.Mux},
		{!cur.Fragment, !cur.Mux},
	}
}

const (
	rescueEvery = 30 * time.Minute // at most one round per this
	rescueWait  = 20 * time.Second // for a new configuration to settle
)

func (a *App) rescuing() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.inRescue
}

// rescueWatch checks every minute whether any server answers; after two
// failed checks in a row it runs a rescue round.
func (a *App) rescueWatch(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	fails := 0
	var last time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		st := a.Store.View()
		if !st.Settings.Enabled || !st.Settings.AutoRescue || !a.running() || len(st.Nodes) == 0 {
			fails = 0
			continue
		}
		if a.anyServerAnswers(ctx) {
			fails = 0
			continue
		}
		if fails++; fails < 2 || time.Since(last) < rescueEvery {
			continue
		}
		last = time.Now()
		a.rescue(ctx)
	}
}

func (a *App) anyServerAnswers(ctx context.Context) bool {
	c, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	res, err := a.Clash().GroupDelay(c, gen.TagAuto, a.Store.View().Settings.TestURL, 6*time.Second)
	return err == nil && anyOK(res)
}

func (a *App) rescue(ctx context.Context) {
	a.mu.Lock()
	a.inRescue = true
	a.mu.Unlock()
	defer func() { a.mu.Lock(); a.inRescue = false; a.mu.Unlock() }()

	s := a.Store.View().Settings
	orig := rescueCombo{s.TLSFragment, s.Mux}
	a.Log.Add("foxgozar", "every server failed; trying other connection settings ("+orig.String()+" now)")
	set := func(c rescueCombo) error {
		if err := a.Store.Update(func(st *state.State) error {
			st.Settings.TLSFragment, st.Settings.Mux = c.Fragment, c.Mux
			return nil
		}); err != nil {
			return err
		}
		if err := a.Apply(ctx); err != nil {
			return err
		}
		// Apply selects the servers again; while on WARP, traffic stays there
		// until a combination has been shown to work.
		a.mu.Lock()
		onWarp := a.onWarp
		a.mu.Unlock()
		if onWarp {
			c, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			_ = a.Clash().Select(c, gen.TagProxy, gen.TagWarp)
		}
		return nil
	}
	var tried []string
	for _, c := range rescueCombos(orig) {
		if ctx.Err() != nil || !a.Store.View().Settings.Enabled {
			break
		}
		if err := set(c); err != nil {
			a.Log.Add("foxgozar", "rescue: "+err.Error())
			continue
		}
		time.Sleep(rescueWait)
		if a.anyServerAnswers(ctx) {
			a.Log.Add("foxgozar", "servers answer again with "+c.String()+"; keeping it")
			a.mu.Lock()
			a.lastRescue = fmt.Sprintf("%s (%s)", c.String(), time.Now().Format("2006-01-02 15:04"))
			a.mu.Unlock()
			return
		}
		tried = append(tried, c.String())
	}
	_ = set(orig)
	a.Log.Add("foxgozar", "no setting helped ("+strings.Join(tried, "; ")+"); back to "+orig.String())
}

// LastRescue says what the last successful rescue changed, for the UI.
func (a *App) LastRescue() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastRescue
}
