package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"sync"
	"time"
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
	{"valorant", "Valorant", 0, "", []Server{{"Frankfurt", "fra"}, {"Paris", "par"}, {"Stockholm", "sto"}, {"Bahrain", "bah"}}},
	{"bf6", "Battlefield 6", 2807960, "", []Server{{"Europe (Frankfurt)", "fra"}, {"Europe (Amsterdam)", "ams"}, {"Middle East", "bah"}}},
	{"cod", "Call of Duty", 1938090, "", []Server{{"Europe", "fra"}, {"Middle East", "dxb"}}},
	{"apex", "Apex Legends", 1172470, "", []Server{{"Frankfurt", "fra"}, {"Bahrain", "bah"}}},
	{"pubg", "PUBG: Battlegrounds", 578080, "", []Server{{"Europe", "fra"}, {"Middle East", "dxb"}}},
	{"overwatch", "Overwatch 2", 2357570, "", []Server{{"Europe", "ams"}, {"Middle East", "bah"}}},
	{"rocket", "Rocket League", 252950, "", []Server{{"Europe", "ams"}, {"Middle East", "bah"}}},
	{"fortnite", "Fortnite", 0, "", []Server{{"Europe", "fra"}, {"Middle East", "bah"}}},
	{"wow", "World of Warcraft", 0, "", []Server{{"Europe", "par"}}},
}

// GameResults: node -> region -> ms, -1 when the region did not answer.
type GameResults struct {
	Regions []Region                  `json:"regions"`
	Games   []Game                    `json:"games"`
	Results map[string]map[string]int `json:"results"`
	At      int64                     `json:"at,omitempty"`
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
	out := GameResults{Regions: Regions, Games: Games, Results: map[string]map[string]int{}, At: time.Now().Unix()}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, n := range nodes {
		out.Results[n.ID] = map[string]int{}
		for _, r := range Regions {
			wg.Add(1)
			go func(id string, r Region) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				best := -1
				for _, host := range r.Hosts { // the second only when the first is down
					if best = pingVia(ctx, probe, id, st.Token, "http://"+host+"/speedtest/latency.txt"); best > 0 {
						break
					}
				}
				mu.Lock()
				out.Results[id][r.ID] = best
				mu.Unlock()
			}(n.ID, r)
		}
	}
	wg.Wait()
	a.mu.Lock()
	a.games = &out
	a.mu.Unlock()
	return out, nil
}

func (a *App) LastGames() GameResults {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.games == nil {
		return GameResults{Regions: Regions, Games: Games, Results: map[string]map[string]int{}}
	}
	return *a.games
}

// pingVia measures round trips through one server the way a game feels
// them: on a connection that is already open. The first request opens the
// path (TCP, TLS, WebSocket, the proxy's own handshake) and is not counted;
// the next few travel the open connection, and the lowest is kept. A reply
// that came on a new connection would include that setup again, so it is
// not used. -1 when the region cannot be reached.
func pingVia(ctx context.Context, probePort int, node, secret, target string) int {
	proxy := &url.URL{Scheme: "socks5", Host: fmt.Sprintf("127.0.0.1:%d", probePort), User: url.UserPassword(node, secret)}
	tr := &http.Transport{Proxy: http.ProxyURL(proxy), MaxIdleConnsPerHost: 1, DisableCompression: true,
		ResponseHeaderTimeout: 5 * time.Second}
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
		return -1
	}
	best := time.Duration(-1)
	for i := 0; i < 4; i++ {
		d, reused, err := get()
		if err != nil || !reused {
			continue
		}
		if best < 0 || d < best {
			best = d
		}
	}
	if best < 0 {
		return -1
	}
	return int(best.Milliseconds())
}

// PingVia measures one target through one server, for the CLI.
func (a *App) PingVia(ctx context.Context, node, target string) int {
	st := a.Store.View()
	return pingVia(ctx, a.act().probePort(st.Settings), node, st.Token, target)
}
