package app

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	active := false
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
		if !gamePort(port) {
			continue
		}
		active = true
		// Remember where the game really plays: its own servers, which the
		// direct check pings later.
		if ip, _ := md["destinationIP"].(string); ip != "" {
			a.sawGameServer(port, ip)
		}
	}
	return active
}

// gameServersKept is how many servers per game are remembered: the most
// recent ones.
const gameServersKept = 6

func (a *App) sawGameServer(port int, ip string) {
	if net.ParseIP(ip) == nil || net.ParseIP(ip).IsPrivate() {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.gameSeen == nil {
		a.gameSeen = map[string]map[string]time.Time{}
	}
	for id := range gen.GamePorts {
		if !gamePortOf(id, port) {
			continue
		}
		m := a.gameSeen[id]
		if m == nil {
			m = map[string]time.Time{}
			a.gameSeen[id] = m
		}
		m[ip] = time.Now()
		for len(m) > gameServersKept { // drop the oldest
			oldest := ""
			for k, t := range m {
				if oldest == "" || t.Before(m[oldest]) {
					oldest = k
				}
			}
			delete(m, oldest)
		}
	}
}

func gamePortOf(id string, port int) bool {
	for _, r := range gen.GamePorts[id] {
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
	return false
}

// DirectCheck is how a game's own servers answer outside the tunnel.
type DirectCheck struct {
	Servers []ServerCheck `json:"servers"`
	// Best direct round trip over the servers that answered; 0 if none.
	BestDirect int `json:"best_direct"`
	// The game's best path through the tunnel, from the last game test, to
	// compare with; 0 if not measured.
	BestTunnel int    `json:"best_tunnel"`
	Advice     string `json:"advice"` // "direct", "tunnel", "unknown" or "play-first"
}

type ServerCheck struct {
	IP string `json:"ip"`
	Ms int    `json:"ms"` // average ICMP round trip, -1 when it did not answer
}

// CheckGameDirect pings the servers this game was seen playing on, outside
// the tunnel. A ping (ICMP) never enters the tunnel's rules, so it shows
// whether the game server itself is reachable from this line, and how far.
// Some game servers do not answer pings at all: then nothing can be said.
func (a *App) CheckGameDirect(ctx context.Context, game string) (DirectCheck, error) {
	if _, ok := gen.GamePorts[game]; !ok {
		return DirectCheck{}, errors.New("this game's traffic cannot be told apart, so it cannot go direct")
	}
	a.mu.Lock()
	var ips []string
	for ip := range a.gameSeen[game] {
		ips = append(ips, ip)
	}
	a.mu.Unlock()
	out := DirectCheck{Servers: []ServerCheck{}, Advice: "play-first"}
	if len(ips) == 0 {
		return out, nil
	}
	sort.Strings(ips)
	var wg sync.WaitGroup
	res := make([]ServerCheck, len(ips))
	for i, ip := range ips {
		wg.Add(1)
		go func(i int, ip string) {
			defer wg.Done()
			res[i] = ServerCheck{IP: ip, Ms: icmpPing(ctx, ip)}
		}(i, ip)
	}
	wg.Wait()
	out.Servers = res
	for _, r := range res {
		if r.Ms > 0 && (out.BestDirect == 0 || r.Ms < out.BestDirect) {
			out.BestDirect = r.Ms
		}
	}
	// The tunnel's best to any of the game's cities, from the last test.
	g := a.LastGames()
	for _, gm := range g.Games {
		if gm.ID != game {
			continue
		}
		for id, regions := range g.Results {
			if id == DirectPath {
				continue
			}
			for _, sv := range gm.Servers {
				if ms := regions[sv.Region]; ms > 0 && (out.BestTunnel == 0 || ms < out.BestTunnel) {
					out.BestTunnel = ms
				}
			}
		}
	}
	switch {
	case out.BestDirect == 0:
		out.Advice = "unknown"
	case out.BestTunnel == 0 || out.BestDirect < out.BestTunnel:
		out.Advice = "direct"
	default:
		out.Advice = "tunnel"
	}
	return out, nil
}

// icmpPing: the average of a few pings, -1 when none came back. The
// router's own ping is ICMP, which the tunnel's rules leave alone.
func icmpPing(ctx context.Context, ip string) int {
	c, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(c, "ping", "-c", "4", "-W", "2", "-q", ip).Output()
	// "round-trip min/avg/max = 92.4/99.7/163.1 ms"
	s := string(out)
	i := strings.Index(s, "min/avg/max")
	if i < 0 {
		return -1
	}
	f := strings.Split(strings.TrimSpace(strings.SplitN(s[i:], "=", 2)[1]), "/")
	if len(f) < 2 {
		return -1
	}
	v, err := strconv.ParseFloat(f[1], 64)
	if err != nil {
		return -1
	}
	return int(v + 0.5)
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
