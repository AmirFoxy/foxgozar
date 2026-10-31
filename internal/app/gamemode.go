package app

import (
	"context"
	"strconv"
	"strings"
	"time"

	"foxgozar/internal/gen"
)

// Game mode: the router's CPU is small (700 MHz on the owner's), and the
// background jobs (a subscription update, new rule-sets, the new core
// instance they bring) take it for seconds. In a match that shows as ping
// spikes. While a game is being played, they wait.

// gamePort reports whether port is one a known game plays on.
func gamePort(port int) bool {
	for _, ranges := range gen.GamePorts {
		for _, r := range ranges {
			lo, hi, _ := strings.Cut(r, ":")
			a, err1 := strconv.Atoi(lo)
			b, err2 := strconv.Atoi(hi)
			if err2 != nil {
				b = a
			}
			if err1 == nil && port >= a && port <= b {
				return true
			}
		}
	}
	return false
}

// gameActive reports whether any open connection is a game's match
// traffic: UDP to a game port.
func (a *App) gameActive(ctx context.Context) bool {
	if !a.running() {
		return false
	}
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cs, err := a.Clash().Connections(c)
	if err != nil {
		return false
	}
	for _, conn := range cs.Connections {
		md, _ := conn["metadata"].(map[string]any)
		if n, _ := md["network"].(string); n != "udp" {
			continue
		}
		var port int
		switch p := md["destinationPort"].(type) {
		case string:
			port, _ = strconv.Atoi(p)
		case float64:
			port = int(p)
		}
		if gamePort(port) {
			return true
		}
	}
	return false
}

// quietForGame is checked by the background loop: true while a game is on,
// so the heavy jobs wait for the next round. Logged when it starts and ends.
func (a *App) quietForGame(ctx context.Context) bool {
	on := a.gameActive(ctx)
	a.mu.Lock()
	changed := on != a.gaming
	a.gaming = on
	a.mu.Unlock()
	if changed && on {
		a.Log.Add("foxgozar", "a game is on: background updates wait until it ends")
	} else if changed {
		a.Log.Add("foxgozar", "game over: background updates resume")
	}
	return on
}
