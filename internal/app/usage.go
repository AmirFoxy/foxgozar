package app

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"foxgozar/internal/state"
)

// Traffic per server per month, kept across restarts: for a server with a
// monthly allowance, and to see which one carries what. Counted from the
// core's open connections every few seconds, like device usage: long
// transfers are counted in full, the last seconds of very short ones can
// be missed, so it is a close estimate rather than the provider's meter.

const (
	usageEvery  = 5 * time.Second
	usageSave   = 10 * time.Minute // flash writes
	usageMonths = 12
)

type usageCounter struct {
	mu      sync.Mutex
	conns   map[string][3]int64 // slot+conn id -> up, down, generation
	pending map[string][2]int64 // node -> up, down not saved yet
	gen     int64
}

func month(t time.Time) string { return t.Format("2006-01") }

func (a *App) usageWatch(ctx context.Context) {
	tick := time.NewTicker(usageEvery)
	defer tick.Stop()
	lastSave := time.Now()
	for {
		select {
		case <-ctx.Done():
			a.saveUsage()
			return
		case <-tick.C:
		}
		a.countUsage(ctx)
		if time.Since(lastSave) > usageSave {
			lastSave = time.Now()
			a.saveUsage()
		}
	}
}

// countUsage adds what every open connection moved since the last look to
// the server it goes through.
func (a *App) countUsage(ctx context.Context) {
	st := a.Store.View()
	u := a.usage
	u.mu.Lock()
	defer u.mu.Unlock()
	u.gen++
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
		for _, conn := range cs.Connections {
			id, _ := conn["id"].(string)
			node := ""
			chains, _ := conn["chains"].([]any)
			for _, ch := range chains {
				if s, _ := ch.(string); st.NodeByID(s) != nil {
					node = s
					break
				}
			}
			if id == "" || node == "" {
				continue // direct, blocked, or WARP: no server's allowance
			}
			key := fmt.Sprint(sl.id, id)
			up, _ := conn["upload"].(float64)
			down, _ := conn["download"].(float64)
			prev, seen := u.conns[key]
			du, dd := int64(up), int64(down)
			if seen {
				du, dd = du-prev[0], dd-prev[1]
			}
			if du < 0 || dd < 0 {
				du, dd = 0, 0
			}
			u.conns[key] = [3]int64{int64(up), int64(down), u.gen}
			p := u.pending[node]
			u.pending[node] = [2]int64{p[0] + du, p[1] + dd}
		}
	}
	for k, v := range u.conns {
		if v[2] != u.gen {
			delete(u.conns, k)
		}
	}
}

// saveUsage adds the counted traffic to this month, and drops months older
// than a year.
func (a *App) saveUsage() {
	u := a.usage
	u.mu.Lock()
	pending := u.pending
	u.pending = map[string][2]int64{}
	u.mu.Unlock()
	if len(pending) == 0 {
		return
	}
	m := month(time.Now())
	_ = a.Store.Update(func(s *state.State) error {
		if s.Usage == nil {
			s.Usage = map[string]map[string][2]int64{}
		}
		if s.Usage[m] == nil {
			s.Usage[m] = map[string][2]int64{}
		}
		for node, p := range pending {
			x := s.Usage[m][node]
			s.Usage[m][node] = [2]int64{x[0] + p[0], x[1] + p[1]}
		}
		pruneMonths(s.Usage, usageMonths)
		return nil
	})
}

func pruneMonths(u map[string]map[string][2]int64, keep int) {
	months := make([]string, 0, len(u))
	for m := range u {
		months = append(months, m)
	}
	sort.Strings(months)
	for i := 0; i < len(months)-keep; i++ {
		delete(u, months[i])
	}
}

// MonthUsage is this month's traffic per server, unsaved counts included.
func (a *App) MonthUsage() (string, map[string][2]int64) {
	m := month(time.Now())
	out := map[string][2]int64{}
	for node, x := range a.Store.View().Usage[m] {
		out[node] = x
	}
	a.usage.mu.Lock()
	for node, p := range a.usage.pending {
		x := out[node]
		out[node] = [2]int64{x[0] + p[0], x[1] + p[1]}
	}
	a.usage.mu.Unlock()
	return m, out
}
