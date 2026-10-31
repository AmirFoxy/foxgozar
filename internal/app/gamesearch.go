package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"foxgozar/internal/state"
)

// Game pictures and the live search for adding games. Both ask Steam's
// store, through the tunnel.

// steamArt asks Steam for an app's header picture. Newer games keep their
// pictures under hashed paths, so the address cannot be guessed.
func (a *App) steamArt(ctx context.Context, id int) (string, error) {
	b, err := a.Fetcher().Get(ctx, "https://store.steampowered.com/api/appdetails?filters=basic&appids="+strconv.Itoa(id), 2<<20)
	if err != nil {
		return "", err
	}
	var r map[string]struct {
		Success bool `json:"success"`
		Data    struct {
			Header string `json:"header_image"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return "", err
	}
	x := r[strconv.Itoa(id)]
	if !x.Success || !strings.HasPrefix(x.Data.Header, "https://") {
		return "", errors.New("no picture")
	}
	return x.Data.Header, nil
}

// ensureArt looks up, in the background, the pictures of every Steam game
// on the list that has none cached yet.
func (a *App) ensureArt() {
	st := a.Store.View()
	var missing []int
	for _, g := range allGames(st) {
		if g.Steam > 0 && st.ArtCache[strconv.Itoa(g.Steam)] == "" {
			missing = append(missing, g.Steam)
		}
	}
	if len(missing) == 0 || !a.claim("game-art") {
		return
	}
	go func() {
		defer a.release("game-art")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		found := map[string]string{}
		for _, id := range missing {
			if u, err := a.steamArt(ctx, id); err == nil {
				found[strconv.Itoa(id)] = u
			}
		}
		if len(found) == 0 {
			return
		}
		_ = a.Store.Update(func(s *state.State) error {
			if s.ArtCache == nil {
				s.ArtCache = map[string]string{}
			}
			for k, v := range found {
				s.ArtCache[k] = v
			}
			return nil
		})
	}()
}

// GameHit is one search result: a game already known (built in, or added
// before; perhaps hidden), or one found in Steam's store.
type GameHit struct {
	ID     string `json:"id,omitempty"` // when already known
	Name   string `json:"name"`
	Steam  int    `json:"steam,omitempty"`
	Image  string `json:"image,omitempty"`
	Shown  bool   `json:"shown"`  // already on the Games tab
	Hidden bool   `json:"hidden"` // known but hidden: adding shows it again
}

// SearchGames finds games by name: the known ones first, then Steam's.
func (a *App) SearchGames(ctx context.Context, q string) ([]GameHit, error) {
	q = strings.TrimSpace(q)
	if len([]rune(q)) < 2 {
		return []GameHit{}, nil
	}
	st := a.Store.View()
	hidden := map[string]bool{}
	for _, id := range st.HiddenGames {
		hidden[id] = true
	}
	out := []GameHit{}
	known := map[int]bool{}
	lq := strings.ToLower(q)
	for _, g := range allGames(st) {
		if g.Steam > 0 {
			known[g.Steam] = true
		}
		if strings.Contains(strings.ToLower(g.Name), lq) {
			out = append(out, GameHit{ID: g.ID, Name: g.Name, Steam: g.Steam, Image: g.Art,
				Shown: !hidden[g.ID], Hidden: hidden[g.ID]})
		}
	}
	b, err := a.Fetcher().Get(ctx, "https://store.steampowered.com/api/storesearch/?l=english&cc=US&term="+url.QueryEscape(q), 1<<20)
	if err != nil {
		if len(out) > 0 {
			return out, nil // the known games still answer
		}
		return nil, fmt.Errorf("Steam store: %w", err)
	}
	var r struct {
		Items []struct {
			ID    int    `json:"id"`
			Name  string `json:"name"`
			Type  string `json:"type"`
			Image string `json:"tiny_image"`
		} `json:"items"`
	}
	_ = json.Unmarshal(b, &r)
	for _, it := range r.Items {
		if it.Type != "app" || it.ID <= 0 || known[it.ID] || len(out) >= 10 {
			continue
		}
		img := it.Image
		if !strings.HasPrefix(img, "https://") {
			img = ""
		}
		out = append(out, GameHit{Name: it.Name, Steam: it.ID, Image: img})
	}
	return out, nil
}

// AddFound adds a search result: a known hidden game is shown again; a
// Steam game is added with its picture.
func (a *App) AddFound(ctx context.Context, hit GameHit) error {
	st := a.Store.View()
	for _, g := range allGames(st) {
		if (hit.ID != "" && g.ID == hit.ID) || (hit.Steam > 0 && g.Steam == hit.Steam) {
			return a.Store.Update(func(s *state.State) error {
				kept := s.HiddenGames[:0]
				for _, id := range s.HiddenGames {
					if id != g.ID {
						kept = append(kept, id)
					}
				}
				s.HiddenGames = kept
				return nil
			})
		}
	}
	name := strings.TrimSpace(hit.Name)
	if name == "" || len(name) > 80 {
		return errors.New("give the game's name")
	}
	g := state.CustomGame{ID: CustomGamePrefix + state.RandomHex(4), Name: name, Steam: hit.Steam}
	if err := a.Store.Update(func(s *state.State) error { s.CustomGames = append(s.CustomGames, g); return nil }); err != nil {
		return err
	}
	a.ensureArt()
	return nil
}

// RemoveFromList takes a game off the Games tab: one the user added is
// deleted, a built-in one is hidden (search brings it back).
func (a *App) RemoveFromList(id string) error {
	if strings.HasPrefix(id, CustomGamePrefix) {
		return a.RemoveGame(id)
	}
	return a.Store.Update(func(s *state.State) error {
		for _, h := range s.HiddenGames {
			if h == id {
				return nil
			}
		}
		s.HiddenGames = append(s.HiddenGames, id)
		return nil
	})
}
