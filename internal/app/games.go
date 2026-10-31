package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"foxgozar/internal/core"
	"foxgozar/internal/gen"
	"foxgozar/internal/state"
)

// Game servers speak UDP and are not reachable as such through a proxy. What
// is measured instead is the round trip through each connection to a host
// in the same city as the game's servers, on a connection that is already
// open - the way a game's traffic travels once it is playing. The hosts are
// Ookla Speedtest servers in data centres and on backbones, which answer
// /speedtest/latency.txt for exactly this. Checked against in-game ping on
// a real connection: Luxembourg 89 ms against Dota 2 EU West 86-89.

// Region is a city, with two hosts there in case one is down.
type Region struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Hosts []string `json:"-"`
}

var Regions = []Region{
	{"lux", "Luxembourg", []string{"speedtest.root.lu:8080", "speedtest2.bce.lu:8080"}},
	{"ams", "Amsterdam", []string{"speedtest-ams.retn.net.prod.hosts.ooklaserver.net:8080", "nkf-ams.speedtest.royalehosting.net:8080"}},
	{"fra", "Frankfurt", []string{"de-fra2-lg1.level66.network.prod.hosts.ooklaserver.net:8080", "speedtest.23m.com.prod.hosts.ooklaserver.net:8080"}},
	{"par", "Paris", []string{"speedtest.pa10.fdcservers.net:8080", "st1.scaleway.com.prod.hosts.ooklaserver.net:8080"}},
	{"vie", "Vienna", []string{"speedtest.vie.fdcservers.net:8080", "speedtest03.timewarp.at.prod.hosts.ooklaserver.net:8080"}},
	{"sto", "Stockholm", []string{"speedtest-sto.retn.net.prod.hosts.ooklaserver.net:8080", "se-speedt02.hy.nis.telia.net.prod.hosts.ooklaserver.net:8080"}},
	{"bah", "Bahrain", []string{"speedtest.batelco.com.prod.hosts.ooklaserver.net:8080", "stest.bh.zain.com:8080"}},
	{"dxb", "Dubai", []string{"speedtest1.etisalat.ae:8080", "dxbsouth.speedtest.du.ae.prod.hosts.ooklaserver.net:8080"}},
}

// Server is one of a game's own server regions, as the game names it, and
// the city it is in.
type Server struct {
	Name   string `json:"name"`
	Region string `json:"region"`
}

// Game: its servers that Iranian players use, and where to get its art.
type Game struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Steam   int      `json:"steam,omitempty"` // Steam app id: banner art from Steam's CDN
	Art     string   `json:"art,omitempty"`   // other official art, when not on Steam
	Servers []Server `json:"servers"`
}

var Games = []Game{
	{"dota2", "Dota 2", 570, "", []Server{{"EU West", "lux"}, {"EU East", "vie"}, {"Stockholm", "sto"}, {"Dubai", "dxb"}}},
	{"cs2", "Counter-Strike 2", 730, "", []Server{{"Luxembourg", "lux"}, {"Vienna", "vie"}, {"Frankfurt", "fra"}, {"Stockholm", "sto"}, {"Dubai", "dxb"}}},
	{"lol", "League of Legends", 0, "https://ddragon.leagueoflegends.com/cdn/img/champion/splash/Jinx_0.jpg",
		[]Server{{"EUW", "ams"}, {"EUNE", "ams"}, {"ME", "bah"}}},
	{"valorant", "Valorant", 0, "https://cmsassets.rgpub.io/sanity/images/dsfx7636/news_live/9167b57d3c081d04e75abe28387d87a27bb83953-3440x1020.jpg?w=1600",
		[]Server{{"Frankfurt", "fra"}, {"Paris", "par"}, {"Stockholm", "sto"}, {"Bahrain", "bah"}}},
	{"bf6", "Battlefield 6", 2807960, "", []Server{{"Europe (Frankfurt)", "fra"}, {"Europe (Amsterdam)", "ams"}, {"Middle East", "bah"}}},
	{"cod", "Call of Duty", 1938090, "", []Server{{"Europe", "fra"}, {"Middle East", "dxb"}}},
	{"apex", "Apex Legends", 1172470, "", []Server{{"Frankfurt", "fra"}, {"Bahrain", "bah"}}},
	{"pubg", "PUBG: Battlegrounds", 578080, "", []Server{{"Europe", "fra"}, {"Middle East", "dxb"}}},
	{"overwatch", "Overwatch 2", 2357570, "", []Server{{"Europe", "ams"}, {"Middle East", "bah"}}},
	{"rocket", "Rocket League", 252950, "", []Server{{"Europe", "ams"}, {"Middle East", "bah"}}},
	{"fortnite", "Fortnite", 0, "https://cdn2.unrealengine.com/01-fnbr-32-00-reload-social-environment-shot12-3840x2160-3840x2160-21e8d672dcad.jpg?resize=1&w=1600",
		[]Server{{"Europe", "fra"}, {"Middle East", "bah"}}},
	{"wow", "World of Warcraft", 0, "https://blz-contentstack-images.akamaized.net/v3/assets/blt9c12f249ac15c7ec/bltb5a24e5ab1e2cfb0/6a88e3589b942efdb6f74110/wow-thumbnail-homepage.jpg",
		[]Server{{"Europe", "par"}}},
}

// GameResults: node -> region -> ms, -1 when the region did not answer.
type GameResults struct {
	Regions []Region                  `json:"regions"`
	Games   []Game                    `json:"games"`
	Results map[string]map[string]int `json:"results"`
	// The same paths and regions: what nine in ten round trips stay under
	// (-1: more than one in ten lost), and the jitter.
	P90    map[string]map[string]int `json:"p90,omitempty"`
	Jitter map[string]map[string]int `json:"jitter,omitempty"`
	At     int64                     `json:"at,omitempty"`
	// game id -> path of its own; and the games that can have one
	Routes   map[string]string `json:"routes,omitempty"`
	Routable map[string]bool   `json:"routable"`
	Hidden   []string          `json:"hidden,omitempty"`
}

// TestGames measures every region through every server, eight at a time,
// as round trips on an open connection: see pingVia.
func (a *App) TestGames(ctx context.Context) (GameResults, error) {
	if !a.running() {
		return GameResults{}, errors.New("connect first: latency is measured through the core")
	}
	if !a.claim("games") {
		return a.LastGames(), errors.New("a test is already running")
	}
	defer a.release("games")
	st := a.Store.View()
	nodes := st.Nodes
	probe := a.act().probePort(st.Settings)
	out := GameResults{Regions: Regions, Games: Games, Results: map[string]map[string]int{},
		P90: map[string]map[string]int{}, Jitter: map[string]map[string]int{}, At: time.Now().Unix()}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	// Every server, and the direct path (outside the tunnel) as one more.
	paths := []string{DirectPath}
	for _, n := range nodes {
		paths = append(paths, n.ID)
	}
	for _, p := range paths {
		out.Results[p], out.P90[p], out.Jitter[p] = map[string]int{}, map[string]int{}, map[string]int{}
		for _, r := range Regions {
			wg.Add(1)
			go func(id string, r Region) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				best := PingStats{Min: -1}
				for _, host := range r.Hosts { // the second only when the first is down
					target := "http://" + host + "/speedtest/latency.txt"
					if id == DirectPath {
						best = pingDirect(ctx, target)
					} else {
						best = pingVia(ctx, probe, id, st.Token, target)
					}
					if best.Min > 0 {
						break
					}
				}
				mu.Lock()
				out.Results[id][r.ID] = best.Min
				out.P90[id][r.ID] = best.P90
				out.Jitter[id][r.ID] = best.Jitter
				mu.Unlock()
			}(p, r)
		}
	}
	wg.Wait()
	a.mu.Lock()
	a.games = &out
	a.mu.Unlock()
	return a.LastGames(), nil // with the routes, like every other answer
}

func (a *App) LastGames() GameResults {
	st := a.Store.View()
	a.mu.Lock()
	defer a.mu.Unlock()
	out := GameResults{Regions: Regions, Results: map[string]map[string]int{}}
	if a.games != nil {
		out = *a.games
	}
	out.Games = allGames(st)
	out.Hidden = st.HiddenGames
	out.Routes, out.Routable = st.GameRoutes, map[string]bool{}
	for id := range gen.GamePorts {
		out.Routable[id] = true
	}
	return out
}

// allGames is the built-in list and the user's own games. A game the user
// added has no known server cities: every measured city is shown for it.
func allGames(st state.State) []Game {
	out := append([]Game(nil), Games...)
	for _, c := range st.CustomGames {
		g := Game{ID: c.ID, Name: c.Name, Steam: c.Steam}
		for _, r := range Regions {
			g.Servers = append(g.Servers, Server{Name: r.Name, Region: r.ID})
		}
		out = append(out, g)
	}
	return out
}

// SetHiddenGames chooses which games the Games tab leaves out.
func (a *App) SetHiddenGames(hidden []string) error {
	known := map[string]bool{}
	for _, g := range allGames(a.Store.View()) {
		known[g.ID] = true
	}
	var keep []string
	for _, id := range hidden {
		if known[id] {
			keep = append(keep, id)
		}
	}
	return a.Store.Update(func(s *state.State) error { s.HiddenGames = keep; return nil })
}

// AddGame adds a game by name. Its Steam id (for the official art) is
// looked up in Steam's store, through the tunnel; a game Steam does not
// have is added by its name alone.
func (a *App) AddGame(ctx context.Context, name string) (state.CustomGame, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 80 {
		return state.CustomGame{}, errors.New("give the game's name")
	}
	g := state.CustomGame{ID: CustomGamePrefix + state.RandomHex(4), Name: name}
	if id, found, err := steamLookup(ctx, a.Fetcher(), name); err == nil && id > 0 {
		g.Steam, g.Name = id, found
	} else if err != nil {
		a.Log.Add("foxgozar", "Steam lookup for "+name+": "+err.Error())
	}
	err := a.Store.Update(func(s *state.State) error {
		for _, x := range allGames(*s) {
			if strings.EqualFold(x.Name, g.Name) || (g.Steam > 0 && x.Steam == g.Steam) {
				return fmt.Errorf("%s is already in the list", x.Name)
			}
		}
		s.CustomGames = append(s.CustomGames, g)
		return nil
	})
	return g, err
}

// RemoveGame removes a game the user added.
func (a *App) RemoveGame(id string) error {
	return a.Store.Update(func(s *state.State) error {
		kept := s.CustomGames[:0]
		found := false
		for _, c := range s.CustomGames {
			if c.ID == id {
				found = true
				continue
			}
			kept = append(kept, c)
		}
		if !found {
			return errors.New("only games you added can be removed; built-in ones can be hidden")
		}
		s.CustomGames = kept
		return nil
	})
}

// steamLookup finds a game in Steam's store search: its app id and its
// name as Steam writes it.
func steamLookup(ctx context.Context, f *core.Fetcher, name string) (int, string, error) {
	u := "https://store.steampowered.com/api/storesearch/?l=english&cc=US&term=" + url.QueryEscape(name)
	b, err := f.Get(ctx, u, 1<<20)
	if err != nil {
		return 0, "", err
	}
	var r struct {
		Items []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"items"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return 0, "", err
	}
	for _, it := range r.Items {
		if it.ID > 0 && it.Type == "app" {
			return it.ID, it.Name, nil
		}
	}
	return 0, "", nil
}

// DirectPath is the path outside the tunnel, in results and routes.
const DirectPath = gen.GameDirect

// SetGameRoute gives a game a path of its own: DirectPath, a server id,
// or "" for the tunnel like everything else.
func (a *App) SetGameRoute(ctx context.Context, game, via string) error {
	if _, ok := gen.GamePorts[game]; !ok {
		return errors.New("this game cannot be given a path of its own: its ports are not known well enough")
	}
	err := a.Store.Update(func(s *state.State) error {
		if via != "" && via != DirectPath && s.NodeByID(via) == nil {
			return errors.New("no such server")
		}
		if s.GameRoutes == nil {
			s.GameRoutes = map[string]string{}
		}
		if via == "" {
			delete(s.GameRoutes, game)
		} else {
			s.GameRoutes[game] = via
		}
		return nil
	})
	if err != nil {
		return err
	}
	return a.Apply(ctx)
}

// pingVia measures round trips through one server the way a game feels
// them: on a connection that is already open. The first request opens the
// path (TCP, TLS, WebSocket, the proxy's own handshake) and is not counted;
// the next few travel the open connection, and the lowest is kept. A reply
// that came on a new connection would include that setup again, so it is
// not used. -1 when the region cannot be reached.
func pingVia(ctx context.Context, probePort int, node, secret, target string) PingStats {
	proxy := &url.URL{Scheme: "socks5", Host: fmt.Sprintf("127.0.0.1:%d", probePort), User: url.UserPassword(node, secret)}
	return pingOn(ctx, &http.Transport{Proxy: http.ProxyURL(proxy), MaxIdleConnsPerHost: 1, DisableCompression: true,
		ResponseHeaderTimeout: 5 * time.Second}, target)
}

// pingDirect is pingVia without the tunnel: the path a game takes when it
// is routed direct.
func pingDirect(ctx context.Context, target string) PingStats {
	return pingOn(ctx, &http.Transport{DialContext: core.MarkedDialer(gen.RoutingMark).DialContext, MaxIdleConnsPerHost: 1,
		DisableCompression: true, ResponseHeaderTimeout: 5 * time.Second}, target)
}

func pingOn(ctx context.Context, tr *http.Transport, target string) PingStats {
	defer tr.CloseIdleConnections()
	hc := &http.Client{Transport: tr, Timeout: 7 * time.Second}
	get := func() (time.Duration, bool, error) {
		reused := false
		tctx := httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
			GotConn: func(i httptrace.GotConnInfo) { reused = i.Reused },
		})
		req, _ := http.NewRequestWithContext(tctx, http.MethodGet, target, nil)
		t0 := time.Now()
		resp, err := hc.Do(req)
		if err != nil {
			return 0, false, err
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
		return time.Since(t0), reused, nil
	}
	if _, _, err := get(); err != nil {
		return PingStats{Min: -1}
	}
	var samples []int
	for i := 0; i < pingSamples && ctx.Err() == nil; i++ {
		d, reused, err := get()
		switch {
		case err != nil:
			samples = append(samples, -1) // lost, as far as a game is concerned
		case reused:
			samples = append(samples, int(d.Milliseconds()))
		}
		time.Sleep(40 * time.Millisecond) // spread over time, as game packets are
	}
	return summarize(samples)
}

// pingSamples round trips are taken on the open connection: enough to see
// the spikes a game feels, not only the best case.
const pingSamples = 10

// PingStats: Min is the best round trip (-1: unreachable); P90 is what
// nine in ten stay under, so it carries the spikes; Jitter is the average
// change from one round trip to the next.
type PingStats struct{ Min, P90, Jitter int }

func summarize(samples []int) PingStats {
	var ok []int
	lost := 0
	for _, s := range samples {
		if s < 0 {
			lost++
		} else {
			ok = append(ok, s)
		}
	}
	if len(ok) == 0 {
		return PingStats{Min: -1}
	}
	jit, n := 0, 0
	prev := -1
	for _, s := range samples {
		if s < 0 {
			continue
		}
		if prev >= 0 {
			d := s - prev
			if d < 0 {
				d = -d
			}
			jit += d
			n++
		}
		prev = s
	}
	sorted := append([]int(nil), ok...)
	sort.Ints(sorted)
	st := PingStats{Min: sorted[0]}
	if n > 0 {
		st.Jitter = jit / n
	}
	// A lost round trip counts as the worst of all for the 90th percentile.
	all := append(sorted, make([]int, lost)...)
	for i := len(sorted); i < len(all); i++ {
		all[i] = 1 << 20
	}
	p := all[(len(all)*9+9)/10-1]
	if p == 1<<20 {
		p = -1 // more than one in ten lost: no number would be honest
	}
	st.P90 = p
	return st
}

// PingVia measures one target through one server, for the CLI.
func (a *App) PingVia(ctx context.Context, node, target string) int {
	st := a.Store.View()
	return pingVia(ctx, a.act().probePort(st.Settings), node, st.Token, target).Min
}

// CustomGamePrefix starts the id of every game the user added; no
// built-in id starts with it.
const CustomGamePrefix = "my"
