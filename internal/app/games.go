package app

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Game servers speak UDP, and a proxy has no ping path to them. What can be
// measured honestly is a short request through each connection to the data
// centre region the game's servers run in: it ranks connections the same
// way in-game ping would, and runs a little higher than it.

// Region is a data-centre region with a plain-HTTP endpoint inside it.
type Region struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

var Regions = []Region{
	{"fra", "Frankfurt", "http://dynamodb.eu-central-1.amazonaws.com/"},
	{"par", "Paris", "http://dynamodb.eu-west-3.amazonaws.com/"},
	{"sto", "Stockholm", "http://dynamodb.eu-north-1.amazonaws.com/"},
	{"ams", "Amsterdam", "http://mirror.nl.leaseweb.net/"},
	{"bah", "Bahrain", "http://dynamodb.me-south-1.amazonaws.com/"},
	{"uae", "UAE", "http://dynamodb.me-central-1.amazonaws.com/"},
}

// Game: where its servers for Iranian players usually are, nearest first.
type Game struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Regions []string `json:"regions"`
}

var Games = []Game{
	{"dota2", "Dota 2", []string{"uae", "fra", "sto"}},
	{"cs2", "Counter-Strike 2", []string{"uae", "fra", "sto"}},
	{"lol", "League of Legends", []string{"bah", "ams"}},
	{"valorant", "Valorant", []string{"bah", "fra", "par"}},
	{"bf6", "Battlefield 6", []string{"bah", "fra"}},
	{"cod", "Call of Duty: Warzone", []string{"uae", "fra"}},
	{"wow", "World of Warcraft", []string{"par"}},
	{"overwatch", "Overwatch 2", []string{"bah", "par"}},
	{"fortnite", "Fortnite", []string{"bah", "fra"}},
	{"pubg", "PUBG: Battlegrounds", []string{"uae", "fra"}},
	{"apex", "Apex Legends", []string{"bah", "fra"}},
	{"rocket", "Rocket League", []string{"bah", "fra"}},
}

// GameResults: node -> region -> ms, -1 when the region did not answer.
type GameResults struct {
	Regions []Region                  `json:"regions"`
	Games   []Game                    `json:"games"`
	Results map[string]map[string]int `json:"results"`
	At      int64                     `json:"at,omitempty"`
}

// TestGames measures every region through every server, eight at a time.
// Each region is measured twice and the lower kept: the first request also
// pays for opening the connection through the proxy.
func (a *App) TestGames(ctx context.Context) (GameResults, error) {
	if !a.running() {
		return GameResults{}, errors.New("connect first: latency is measured through the core")
	}
	if !a.claim("games") {
		return a.LastGames(), errors.New("a test is already running")
	}
	defer a.release("games")
	cl := a.Clash()
	nodes := a.Store.View().Nodes
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
				for i := 0; i < 2; i++ {
					c, cancel := context.WithTimeout(ctx, 7*time.Second)
					d, err := cl.Delay(c, id, r.URL, 5*time.Second)
					cancel()
					if err == nil && d > 0 && (best < 0 || d < best) {
						best = d
					}
					if best < 0 && i == 0 {
						break // unreachable once: not worth a second wait
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
