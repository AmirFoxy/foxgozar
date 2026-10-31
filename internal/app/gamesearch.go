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

// steamCapsules asks Steam's store, in one request, for the portrait
// library picture of each app: 1200x1800 ("2x"), the shape of a game card,
// where the old header picture is a small 460x215 landscape one. Newer
// games keep pictures under hashed paths, so addresses come from Steam.
func (a *App) steamCapsules(ctx context.Context, ids []int) (map[int]string, error) {
	type idq struct {
		AppID int `json:"appid"`
	}
	q := map[string]any{
		"ids":          []idq{},
		"context":      map[string]string{"language": "english", "country_code": "US"},
		"data_request": map[string]bool{"include_assets": true},
	}
	for _, id := range ids {
		q["ids"] = append(q["ids"].([]idq), idq{id})
	}
	in, _ := json.Marshal(q)
	b, err := a.Fetcher().Get(ctx, "https://api.steampowered.com/IStoreBrowseService/GetItems/v1?input_json="+url.QueryEscape(string(in)), 4<<20)
	if err != nil {
		return nil, err
	}
	var r struct {
		Response struct {
			Items []struct {
				AppID  int `json:"appid"`
				Assets struct {
					Format string `json:"asset_url_format"`
					Cap2x  string `json:"library_capsule_2x"`
					Cap    string `json:"library_capsule"`
					Hero   string `json:"library_hero"`
				} `json:"assets"`
			} `json:"store_items"`
		} `json:"response"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	out := map[int]string{}
	for _, it := range r.Response.Items {
		file := it.Assets.Cap2x
		if file == "" {
			file = it.Assets.Cap
		}
		if file == "" { // no portrait: the wide, high-resolution hero picture, cropped by the card
			file = it.Assets.Hero
		}
		if file == "" || !strings.Contains(it.Assets.Format, "${FILENAME}") || strings.ContainsAny(file, " \"<>") {
			continue
		}
		out[it.AppID] = "https://shared.akamai.steamstatic.com/store_item_assets/" + strings.Replace(it.Assets.Format, "${FILENAME}", file, 1)
	}
	return out, nil
}

// steamArt asks Steam for an app's header picture: the fallback when it
// has no portrait one.
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
	a.mu.Lock()
	if a.artTried == nil {
		a.artTried = map[int]bool{}
	}
	for _, g := range allGames(st) {
		// A small landscape header cached before portraits were used is
		// looked up again, once per run (a game with no portrait keeps it).
		if u := st.ArtCache[strconv.Itoa(g.Steam)]; g.Steam > 0 && !a.artTried[g.Steam] &&
			(u == "" || strings.Contains(u, "/header")) {
			missing = append(missing, g.Steam)
		}
	}
	a.mu.Unlock()
	if len(missing) == 0 || !a.claim("game-art") {
		return
	}
	a.mu.Lock()
	for _, id := range missing {
		a.artTried[id] = true
	}
	a.mu.Unlock()
	go func() {
		defer a.release("game-art")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		found := map[string]string{}
		caps, err := a.steamCapsules(ctx, missing)
		if err != nil {
			a.Log.Add("foxgozar", "game pictures: "+err.Error())
		}
		for _, id := range missing {
			if u := caps[id]; u != "" {
				found[strconv.Itoa(id)] = u
			} else if u, err := a.steamArt(ctx, id); err == nil {
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
