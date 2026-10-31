package app

import (
	"context"
	"strings"
	"sync"
	"time"

	"foxgozar/internal/state"
)

// Two background records of the servers:
//
//   - steadiness: every half hour, a few tiny fresh connections through each
//     server. One that opens them unsteadily is left out of the automatic
//     choice (SmartAuto) while a steady one is left.
//   - uptime: every ten minutes, whether each server answers, kept a week,
//     so it shows which server drops, and when.

const (
	steadyEvery   = 30 * time.Minute
	steadyTries   = 6
	uptimeSlot    = 10 * time.Minute
	uptimeKeep    = 7 * 24 * 6 // a week of ten-minute slots
	uptimeUp      = 'u'
	uptimeDown    = 'd'
	uptimeUnknown = '-'
)

type steadiness struct {
	mu       sync.Mutex
	unstable map[string]bool
}

// noteSteadiness records the latest verdict on a server, from the
// background check or a speed test, and applies it when it changed what
// the automatic choice should skip.
func (a *App) noteSteadiness(node string, unstable bool) {
	s := a.steady
	s.mu.Lock()
	was := s.unstable[node]
	s.unstable[node] = unstable
	s.mu.Unlock()
	if was == unstable || !a.Store.View().Settings.SmartAuto {
		return
	}
	name := node
	st := a.Store.View()
	if n := st.NodeByID(node); n != nil {
		name = n.Name
	}
	if unstable {
		a.Log.Add("foxgozar", name+": new connections come up unsteadily; the automatic choice skips it while a steady server is left")
	} else {
		a.Log.Add("foxgozar", name+": steady again; back in the automatic choice")
	}
	go func() { _ = a.Apply(context.Background()) }()
}

// avoidSet is what the automatic choice skips now.
func (a *App) avoidSet() map[string]bool {
	if !a.Store.View().Settings.SmartAuto {
		return nil
	}
	s := a.steady
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]bool{}
	for k, v := range s.unstable {
		if v {
			out[k] = true
		}
	}
	return out
}

func (a *App) serverWatch(ctx context.Context) {
	tick := time.NewTicker(uptimeSlot)
	defer tick.Stop()
	// The first steadiness check a few minutes after start, then every half
	// hour; uptime on every tick.
	nextSteady := time.Now().Add(3 * time.Minute)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		st := a.Store.View()
		if !st.Settings.Enabled || !a.running() || len(st.Nodes) == 0 {
			a.recordUptime(st, nil)
			continue
		}
		a.mu.Lock()
		gaming := a.gaming
		a.mu.Unlock()
		a.recordUptime(st, a.answering(ctx, st))
		if gaming || time.Now().Before(nextSteady) {
			continue
		}
		nextSteady = time.Now().Add(steadyEvery)
		for _, n := range st.Nodes {
			if ctx.Err() != nil {
				return
			}
			setup := a.setupSamples(ctx, n.ID, steadyTries, 25*time.Second)
			_, _, unstable := judgeSetup(setup)
			a.noteSteadiness(n.ID, unstable)
		}
	}
}

// answering asks the core whether each server answers now: one request
// through each, eight at a time.
func (a *App) answering(ctx context.Context, st state.State) map[string]bool {
	out := map[string]bool{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, n := range st.Nodes {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			c, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			d, err := a.Clash().Delay(c, id, st.Settings.TestURL, 8*time.Second)
			mu.Lock()
			out[id] = err == nil && d > 0
			mu.Unlock()
		}(n.ID)
	}
	wg.Wait()
	return out
}

// recordUptime adds this slot's mark for every server: up, down, or unknown
// (up nil: not connected, so nothing was measured). Slots missed while the
// router was off are filled with unknown.
func (a *App) recordUptime(st state.State, up map[string]bool) {
	slot := time.Now().Truncate(uptimeSlot).Unix()
	_ = a.Store.Update(func(s *state.State) error {
		if s.Uptime == nil {
			s.Uptime = map[string]state.UptimeLog{}
		}
		for _, n := range s.Nodes {
			mark := byte(uptimeUnknown)
			if up != nil {
				mark = uptimeDown
				if up[n.ID] {
					mark = uptimeUp
				}
			}
			s.Uptime[n.ID] = addMark(s.Uptime[n.ID], slot, mark)
		}
		for id := range s.Uptime { // servers that were removed
			if s.NodeByID(id) == nil {
				delete(s.Uptime, id)
			}
		}
		return nil
	})
}

// addMark appends mark for the slot starting at slot (unix seconds),
// filling skipped slots with unknown and keeping the last week.
func addMark(l state.UptimeLog, slot int64, mark byte) state.UptimeLog {
	step := int64(uptimeSlot / time.Second)
	if l.Marks == "" {
		return state.UptimeLog{Start: slot, Marks: string(mark)}
	}
	last := l.Start + int64(len(l.Marks)-1)*step
	switch {
	case slot <= last: // the same slot again (a restart): the newer mark wins
		if slot == last {
			l.Marks = l.Marks[:len(l.Marks)-1] + string(mark)
		}
		return l
	case slot-last > step:
		gap := int((slot-last)/step) - 1
		if gap > uptimeKeep {
			gap = uptimeKeep
		}
		l.Marks += strings.Repeat(string(rune(uptimeUnknown)), gap)
	}
	l.Marks += string(mark)
	if over := len(l.Marks) - uptimeKeep; over > 0 {
		l.Marks = l.Marks[over:]
	}
	l.Start = slot - int64(len(l.Marks)-1)*step
	return l
}

// UptimePct is the share of measured slots in which the server answered,
// in percent; -1 when nothing was measured.
func UptimePct(l state.UptimeLog) int {
	up, measured := 0, 0
	for i := 0; i < len(l.Marks); i++ {
		switch l.Marks[i] {
		case uptimeUp:
			up++
			measured++
		case uptimeDown:
			measured++
		}
	}
	if measured == 0 {
		return -1
	}
	return up * 100 / measured
}

// UptimeView is a server's week, for the UI.
type UptimeView struct {
	Start int64  `json:"start"` // unix seconds of the first slot
	Step  int    `json:"step"`  // seconds per slot
	Marks string `json:"marks"` // u up, d down, - unknown
	Pct   int    `json:"pct"`
}

func (a *App) Uptime() map[string]UptimeView {
	out := map[string]UptimeView{}
	for id, l := range a.Store.View().Uptime {
		out[id] = UptimeView{Start: l.Start, Step: int(uptimeSlot / time.Second), Marks: l.Marks, Pct: UptimePct(l)}
	}
	return out
}
