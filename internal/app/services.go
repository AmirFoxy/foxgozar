package app

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Service is a site checked through every server: some servers pass the
// general test yet cannot open YouTube or Instagram.
type Service struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

var Services = []Service{
	{"youtube", "YouTube", "https://www.youtube.com/generate_204"},
	{"instagram", "Instagram", "https://www.instagram.com/robots.txt"},
	{"telegram", "Telegram", "https://telegram.org/robots.txt"},
	{"google", "Google", "https://www.google.com/generate_204"},
	{"x", "X", "https://x.com/robots.txt"},
}

// ServiceResults: node -> service -> delay in ms, -1 when it did not open.
type ServiceResults struct {
	Services []Service                 `json:"services"`
	Results  map[string]map[string]int `json:"results"`
	At       int64                     `json:"at,omitempty"`
}

// TestServices opens every service through every server, eight requests
// at a time so a small router is not swamped.
func (a *App) TestServices(ctx context.Context) (ServiceResults, error) {
	if !a.running() {
		return ServiceResults{}, errors.New("connect first: services are tested through the core")
	}
	if !a.claim("services") {
		return a.LastServices(), errors.New("a test is already running")
	}
	defer a.release("services")
	cl := a.Clash()
	nodes := a.Store.View().Nodes
	out := ServiceResults{Services: Services, Results: map[string]map[string]int{}, At: time.Now().Unix()}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, n := range nodes {
		out.Results[n.ID] = map[string]int{}
		for _, s := range Services {
			wg.Add(1)
			go func(id string, s Service) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				c, cancel := context.WithTimeout(ctx, 9*time.Second)
				d, err := cl.Delay(c, id, s.URL, 7*time.Second)
				cancel()
				if err != nil || d <= 0 {
					d = -1
				}
				mu.Lock()
				out.Results[id][s.ID] = d
				mu.Unlock()
			}(n.ID, s)
		}
	}
	wg.Wait()
	a.mu.Lock()
	a.services = &out
	a.mu.Unlock()
	return out, nil
}

// LastServices is the most recent test, or an empty one.
func (a *App) LastServices() ServiceResults {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.services == nil {
		return ServiceResults{Services: Services, Results: map[string]map[string]int{}}
	}
	return *a.services
}
