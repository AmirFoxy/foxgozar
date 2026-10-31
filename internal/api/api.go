// Package api is the HTTP API the web UI and the CLI use.
//
// Auth: either the token from state.json (CLI, scripts) or a LuCI session
// id, checked against rpcd, so anyone logged in to LuCI can use the UI and
// nobody else can.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"foxgozar/internal/app"
	"foxgozar/internal/clash"
	"foxgozar/internal/core"
	"foxgozar/internal/link"
	"foxgozar/internal/qrsvg"
	"foxgozar/internal/state"
)

type Server struct {
	App *app.App

	mu       sync.Mutex
	sessions map[string]time.Time // LuCI sessions recently confirmed
}

func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	h := func(pattern string, fn func(w http.ResponseWriter, r *http.Request) (any, error)) {
		m.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			v, err := fn(w, r)
			if v == nil && err == nil && w.Header().Get("Content-Type") != "" {
				return // streamed
			}
			writeJSON(w, v, err)
		})
	}
	h("GET /api/status", s.status)
	h("POST /api/start", s.start)
	h("POST /api/stop", s.stop)
	h("POST /api/restart", s.restart)

	h("GET /api/settings", s.getSettings)
	h("PUT /api/settings", s.putSettings)

	h("GET /api/nodes", s.nodes)
	h("POST /api/nodes", s.addNodes)
	h("PUT /api/nodes/{id}", s.editNode)
	h("DELETE /api/nodes/{id}", s.deleteNode)
	h("POST /api/nodes/delete", s.deleteNodes)
	h("POST /api/select", s.selectNode)
	h("POST /api/test", s.testAll)
	h("POST /api/test/{id}", s.testOne)
	h("GET /api/services", s.servicesLast)
	h("GET /api/games", s.gamesLast)
	h("POST /api/games/test", s.gamesTest)
	h("PUT /api/games/{id}/route", s.gameRoute)
	h("GET /api/games/search", s.searchGames)
	h("POST /api/games/add", s.addFound)
	h("POST /api/games/{id}/remove", s.removeFromList)
	h("POST /api/games/{id}/check", s.checkGameDirect)
	h("PUT /api/games/hidden", s.hiddenGames)
	h("POST /api/games/custom", s.addGame)
	h("DELETE /api/games/custom/{id}", s.removeGame)
	h("GET /api/antilag", s.antiLag)
	h("PUT /api/antilag", s.putAntiLag)
	h("POST /api/ping", s.ping)
	h("POST /api/services/test", s.servicesTest)
	h("GET /api/nodes/{id}/share", s.share)

	h("GET /api/subs", s.subs)
	h("POST /api/subs", s.addSub)
	h("PUT /api/subs/{id}", s.editSub)
	h("DELETE /api/subs/{id}", s.deleteSub)
	h("POST /api/subs/{id}/update", s.updateSub)

	h("GET /api/warp", s.warpStatus)
	h("GET /api/devices", s.devices)
	h("PUT /api/devices/{mac}", s.setDevice)
	h("DELETE /api/devices/{mac}", s.forgetDevice)

	h("GET /api/rules", s.rules)
	h("PUT /api/rules", s.putRules)

	h("GET /api/logs", s.logs)
	h("GET /api/connections", s.connections)
	h("DELETE /api/connections", s.closeConnections)
	m.HandleFunc("GET /api/traffic", s.traffic)

	h("GET /api/core", s.coreInfo)
	h("POST /api/core/{kind}/update", s.coreUpdate)
	h("POST /api/rulesets/update", s.rulesetUpdate)
	h("GET /api/config", s.config)
	h("GET /api/usage", s.usage)
	h("GET /api/uptime", s.uptime)
	h("GET /api/health", s.health)
	h("POST /api/doctor", s.doctor)
	h("POST /api/doctor/apply", s.doctorApply)
	h("GET /api/speedtest", s.speedResults)
	h("POST /api/speedtest/{id}", s.speedTest)
	h("GET /api/sharing", s.shareServer)
	h("PUT /api/sharing", s.putShareServer)
	h("POST /api/sharing/users", s.addShareUser)
	h("PUT /api/sharing/users/{id}", s.editShareUser)
	h("DELETE /api/sharing/users/{id}", s.deleteShareUser)
	h("GET /api/sharing/users/{id}/link", s.shareUserLink)
	h("GET /api/backup", s.backup)
	h("POST /api/restore", s.restore)

	return s.cors(s.auth(m))
}

func writeJSON(w http.ResponseWriter, v any, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err != nil {
		code := http.StatusBadRequest
		var he httpErr
		if errors.As(err, &he) {
			code = he.code
		}
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if v == nil {
		v = map[string]bool{"ok": true}
	}
	_ = json.NewEncoder(w).Encode(v)
}

type httpErr struct {
	code int
	msg  string
}

func (e httpErr) Error() string { return e.msg }

func decode(r *http.Request, v any) error {
	b, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("bad request body: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------- auth

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if o := r.Header.Get("Origin"); o != "" {
			w.Header().Set("Access-Control-Allow-Origin", o)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Luci-Session")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Private-Network", "true")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if tok == "" {
			tok = r.URL.Query().Get("token") // EventSource cannot set headers
		}
		sid := r.Header.Get("X-Luci-Session")
		if sid == "" {
			sid = r.URL.Query().Get("sid")
		}
		// Through uhttpd's CGI only standard headers arrive; LuCI's own
		// session cookie does.
		for _, name := range []string{"sysauth_https", "sysauth_http", "sysauth"} {
			if sid != "" {
				break
			}
			if c, err := r.Cookie(name); err == nil {
				sid = c.Value
			}
		}
		if (tok != "" && tok == s.App.Store.View().Token) || (sid != "" && s.luciSession(sid)) {
			next.ServeHTTP(w, r)
			return
		}
		writeJSON(w, nil, httpErr{http.StatusUnauthorized, "not logged in"})
	})
}

// luciSession asks rpcd whether sid is a live LuCI session allowed to use
// this app. Answers are cached for a minute.
func (s *Server) luciSession(sid string) bool {
	if len(sid) != 32 {
		return false
	}
	s.mu.Lock()
	if s.sessions == nil {
		s.sessions = map[string]time.Time{}
	}
	if t, ok := s.sessions[sid]; ok && time.Since(t) < time.Minute {
		s.mu.Unlock()
		return true
	}
	s.mu.Unlock()
	arg, _ := json.Marshal(map[string]string{"ubus_rpc_session": sid, "scope": "access-group",
		"object": "luci-app-foxgozar", "function": "write"})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ubus", "call", "session", "access", string(arg)).Output()
	if err != nil {
		return false
	}
	var res struct {
		Access bool `json:"access"`
	}
	if json.Unmarshal(out, &res) != nil || !res.Access {
		return false
	}
	s.mu.Lock()
	s.sessions[sid] = time.Now()
	s.mu.Unlock()
	return true
}

// ---------------------------------------------------------------- handlers

func (s *Server) status(w http.ResponseWriter, r *http.Request) (any, error) {
	return s.App.Status(r.Context()), nil
}

func (s *Server) start(w http.ResponseWriter, r *http.Request) (any, error) {
	// Detached from the request: closing the page must not abort a start.
	err := s.App.Start(context.Background())
	return s.App.Status(r.Context()), err
}

func (s *Server) stop(w http.ResponseWriter, r *http.Request) (any, error) {
	s.App.Stop()
	return s.App.Status(r.Context()), nil
}

func (s *Server) restart(w http.ResponseWriter, r *http.Request) (any, error) {
	s.App.Stop()
	err := s.App.Start(context.Background())
	return s.App.Status(r.Context()), err
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) (any, error) {
	return s.App.Store.View().Settings, nil
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) (any, error) {
	cur := s.App.Store.View().Settings
	// Partial update: fields absent from the body keep their value.
	b, _ := json.Marshal(cur)
	var merged map[string]any
	_ = json.Unmarshal(b, &merged)
	var patch map[string]any
	if err := decode(r, &patch); err != nil {
		return nil, err
	}
	for k, v := range patch {
		if k == "enabled" { // only start/stop change this
			continue
		}
		merged[k] = v
	}
	b, _ = json.Marshal(merged)
	var next state.Settings
	if err := json.Unmarshal(b, &next); err != nil {
		return nil, err
	}
	if err := validateSettings(next); err != nil {
		return nil, err
	}
	if err := s.App.Store.Update(func(st *state.State) error { st.Settings = next; return nil }); err != nil {
		return nil, err
	}
	if err := s.App.EnsureWarp(context.Background()); err != nil {
		// Not left on half-working: the switch goes back off and says why.
		_ = s.App.Store.Update(func(st *state.State) error { st.Settings.WarpBackup = false; return nil })
		return nil, err
	}
	if err := s.App.EnsureRuleSets(context.Background()); err != nil {
		// The setting is saved; it starts working once the download succeeds.
		s.App.Log.Add("foxgozar", "rule-sets: "+err.Error())
	}
	return next, s.App.Apply(context.Background())
}

func validateSettings(s state.Settings) error {
	ports := map[int]string{}
	// The core's own ports are used three times over (p, p+1, p+2): a new
	// instance starts next to the running one when the config changes.
	for _, x := range []struct {
		name  string
		port  int
		width int
	}{
		{"tproxy_port", s.TProxyPort, 3}, {"dns_port", s.DNSPort, 3}, {"clash_port", s.ClashPort, 3},
		{"api_port", s.APIPort, 1}, {"mixed_port", s.MixedPort, 1},
	} {
		if x.name == "mixed_port" && x.port == 0 {
			continue
		}
		if x.port < 1 || x.port+x.width-1 > 65535 {
			return fmt.Errorf("%s: invalid port", x.name)
		}
		for p := x.port; p < x.port+x.width; p++ {
			if o, dup := ports[p]; dup {
				return fmt.Errorf("%s and %s overlap (port %d)", x.name, o, p)
			}
			ports[p] = x.name
		}
	}
	if s.Mode != "auto" && s.Mode != "manual" {
		return errors.New("mode must be auto or manual")
	}
	if len(s.LANInterfaces) == 0 {
		return errors.New("at least one LAN interface is needed")
	}
	switch s.DefaultPolicy {
	case "proxy", "direct":
	default:
		return errors.New("default_policy must be proxy or direct")
	}
	return nil
}

type nodeView struct {
	state.Node
	Group string `json:"group"`
}

func (s *Server) nodes(w http.ResponseWriter, r *http.Request) (any, error) {
	st := s.App.Store.View()
	subName := map[string]string{}
	for _, sb := range st.Subs {
		subName[sb.ID] = sb.Name
	}
	delays := s.App.Delays()
	out := make([]nodeView, 0, len(st.Nodes))
	for _, n := range st.Nodes {
		if d, ok := delays[n.ID]; ok {
			n.Delay = d
		}
		n.Outbound = nil // secrets stay on the router unless asked for one node
		out = append(out, nodeView{Node: n, Group: subName[n.Sub]})
	}
	return out, nil
}

func (s *Server) addNodes(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Text string `json:"text"`
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	res := link.ParseBody(req.Text)
	added := 0
	err := s.App.Store.Update(func(st *state.State) error {
		for _, n := range res.Nodes {
			if st.NodeByID(n.ID) != nil {
				continue
			}
			n.Sub = ""
			st.Nodes = append(st.Nodes, n)
			added++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if added > 0 {
		err = s.App.Apply(context.Background())
	}
	return map[string]any{"added": added, "skipped": res.Skipped}, err
}

func (s *Server) editNode(w http.ResponseWriter, r *http.Request) (any, error) {
	id := r.PathValue("id")
	var req struct {
		Name *string `json:"name"`
		Link *string `json:"link"`
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	var fresh *state.Node
	if req.Link != nil {
		n, err := link.Parse(*req.Link)
		if err != nil {
			return nil, err
		}
		fresh = &n
	}
	err := s.App.Store.Update(func(st *state.State) error {
		n := st.NodeByID(id)
		if n == nil {
			return httpErr{404, "no such node"}
		}
		if fresh != nil {
			name, sub := n.Name, n.Sub
			*n = *fresh
			n.Name, n.Sub = name, sub
			if fresh.Name != "" {
				n.Name = fresh.Name
			}
			if st.Settings.Selected == id {
				st.Settings.Selected = n.ID
			}
		}
		if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
			n.Name = strings.TrimSpace(*req.Name)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return nil, s.App.Apply(context.Background())
}

func (s *Server) removeNodes(ids map[string]bool) error {
	err := s.App.Store.Update(func(st *state.State) error {
		kept := st.Nodes[:0]
		for _, n := range st.Nodes {
			if !ids[n.ID] {
				kept = append(kept, n)
			}
		}
		st.Nodes = kept
		if ids[st.Settings.Selected] {
			st.Settings.Mode, st.Settings.Selected = "auto", ""
		}
		return nil
	})
	if err != nil {
		return err
	}
	return s.App.Apply(context.Background())
}

func (s *Server) deleteNode(w http.ResponseWriter, r *http.Request) (any, error) {
	return nil, s.removeNodes(map[string]bool{r.PathValue("id"): true})
}

func (s *Server) deleteNodes(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		IDs    []string `json:"ids"`
		Failed bool     `json:"failed"` // every node whose last test failed
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for _, id := range req.IDs {
		ids[id] = true
	}
	if req.Failed {
		for id, d := range s.App.Delays() {
			if d < 0 {
				ids[id] = true
			}
		}
	}
	return map[string]int{"deleted": len(ids)}, s.removeNodes(ids)
}

func (s *Server) selectNode(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	return nil, s.App.Select(r.Context(), req.ID)
}

func (s *Server) testAll(w http.ResponseWriter, r *http.Request) (any, error) {
	return s.App.TestAll(r.Context())
}

func (s *Server) testOne(w http.ResponseWriter, r *http.Request) (any, error) {
	d, err := s.App.TestOne(r.Context(), r.PathValue("id"))
	if err != nil {
		return map[string]int{"delay": -1}, nil
	}
	return map[string]int{"delay": d}, nil
}

func (s *Server) subs(w http.ResponseWriter, r *http.Request) (any, error) {
	return s.App.Store.View().Subs, nil
}

func (s *Server) addSub(w http.ResponseWriter, r *http.Request) (any, error) {
	var req state.Subscription
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	req.URL = strings.TrimSpace(req.URL)
	if !strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://") {
		return nil, errors.New("subscription URL must start with http:// or https://")
	}
	req.ID = "s" + state.RandomHex(4)
	if req.Name == "" {
		req.Name = hostOf(req.URL)
	}
	if req.Interval == 0 {
		req.Interval = 12
	}
	req.Enabled = true
	if err := s.App.Store.Update(func(st *state.State) error {
		for _, x := range st.Subs {
			if x.URL == req.URL {
				return errors.New("this subscription is already added")
			}
		}
		st.Subs = append(st.Subs, req)
		return nil
	}); err != nil {
		return nil, err
	}
	err := s.App.UpdateSub(r.Context(), req.ID)
	return subByID(s, req.ID), err
}

func hostOf(u string) string {
	u = strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	if i := strings.IndexAny(u, "/:?"); i >= 0 {
		u = u[:i]
	}
	return u
}

func (s *Server) editSub(w http.ResponseWriter, r *http.Request) (any, error) {
	id := r.PathValue("id")
	var req struct {
		Name      *string `json:"name"`
		URL       *string `json:"url"`
		UserAgent *string `json:"user_agent"`
		Interval  *int    `json:"interval"`
		Enabled   *bool   `json:"enabled"`
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	err := s.App.Store.Update(func(st *state.State) error {
		x := st.SubByID(id)
		if x == nil {
			return httpErr{404, "no such subscription"}
		}
		if req.Name != nil {
			x.Name = *req.Name
		}
		if req.URL != nil {
			x.URL = strings.TrimSpace(*req.URL)
		}
		if req.UserAgent != nil {
			x.UserAgent = *req.UserAgent
		}
		if req.Interval != nil {
			x.Interval = *req.Interval
		}
		if req.Enabled != nil {
			x.Enabled = *req.Enabled
		}
		return nil
	})
	return subByID(s, id), err
}

func (s *Server) deleteSub(w http.ResponseWriter, r *http.Request) (any, error) {
	id := r.PathValue("id")
	ids := map[string]bool{}
	err := s.App.Store.Update(func(st *state.State) error {
		kept := st.Subs[:0]
		for _, x := range st.Subs {
			if x.ID != id {
				kept = append(kept, x)
			}
		}
		st.Subs = kept
		for _, n := range st.Nodes {
			if n.Sub == id {
				ids[n.ID] = true
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return nil, s.removeNodes(ids)
}

func (s *Server) updateSub(w http.ResponseWriter, r *http.Request) (any, error) {
	id := r.PathValue("id")
	err := s.App.UpdateSub(context.Background(), id)
	return subByID(s, id), err
}

func (s *Server) rules(w http.ResponseWriter, r *http.Request) (any, error) {
	return s.App.Store.View().Rules, nil
}

func (s *Server) putRules(w http.ResponseWriter, r *http.Request) (any, error) {
	var rules []state.ShuntRule
	if err := decode(r, &rules); err != nil {
		return nil, err
	}
	for i := range rules {
		if rules[i].ID == "" {
			rules[i].ID = "r" + state.RandomHex(4)
		}
		for _, u := range rules[i].URLs {
			if !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
				return nil, fmt.Errorf("%q: a list link starts with https://", u)
			}
		}
	}
	if err := s.App.Store.Update(func(st *state.State) error { st.Rules = rules; return nil }); err != nil {
		return nil, err
	}
	// New links are fetched now, so the rule works as soon as it is saved.
	if _, err := s.App.EnsureRuleURLs(r.Context(), false); err != nil {
		s.App.Log.Add("foxgozar", "your rule lists: "+err.Error())
		_ = s.App.Apply(context.Background())
		return nil, fmt.Errorf("saved, but a list could not be downloaded: %w", err)
	}
	return rules, s.App.Apply(context.Background())
}

func (s *Server) logs(w http.ResponseWriter, r *http.Request) (any, error) {
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	return s.App.Log.Since(since), nil
}

func (s *Server) connections(w http.ResponseWriter, r *http.Request) (any, error) {
	c, err := s.App.Clash().Connections(r.Context())
	if err != nil {
		return clash.Connections{Connections: []map[string]any{}}, nil
	}
	return c, nil
}

func (s *Server) closeConnections(w http.ResponseWriter, r *http.Request) (any, error) {
	return nil, s.App.Clash().CloseAll(r.Context())
}

// traffic streams the live rate as Server-Sent Events.
func (s *Server) traffic(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(200)
	fl.Flush()
	// From the totals of every core instance, not one instance's stream:
	// during a config change the old and the new instance both carry traffic.
	prevUp, prevDown, _, _ := s.App.Totals(r.Context())
	prev := time.Now()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
		}
		up, down, _, _ := s.App.Totals(r.Context())
		secs := time.Since(prev).Seconds()
		prev = time.Now()
		ru, rd := float64(up-prevUp)/secs, float64(down-prevDown)/secs
		if ru < 0 || rd < 0 { // an instance stopped between two readings
			ru, rd = 0, 0
		}
		prevUp, prevDown = up, down
		fmt.Fprintf(w, "data: {\"up\":%d,\"down\":%d}\n\n", int64(ru), int64(rd))
		fl.Flush()
	}
}

func (s *Server) coreInfo(w http.ResponseWriter, r *http.Request) (any, error) {
	return map[string]string{
		"sing_box": core.Version(s.App.SingBoxBin()),
		"xray":     core.Version(s.App.XrayBin()),
	}, nil
}

func (s *Server) coreUpdate(w http.ResponseWriter, r *http.Request) (any, error) {
	k := core.Kind(r.PathValue("kind"))
	if k != core.SingBox && k != core.Xray {
		return nil, httpErr{404, "unknown core"}
	}
	v, err := s.App.UpdateCore(context.Background(), k)
	return map[string]string{"version": v}, err
}

func (s *Server) rulesetUpdate(w http.ResponseWriter, r *http.Request) (any, error) {
	return nil, s.App.UpdateRuleSets(context.Background())
}

func (s *Server) config(w http.ResponseWriter, r *http.Request) (any, error) {
	return s.App.ConfigJSON(), nil
}

// Backup is everything the user set up, in one file: settings, servers,
// subscriptions, rules, devices and the WARP account. Not the API token:
// that belongs to this router.
type Backup struct {
	App      string               `json:"app"`
	Exported int64                `json:"exported"`
	Settings state.Settings       `json:"settings"`
	Nodes    []state.Node         `json:"nodes"`
	Subs     []state.Subscription `json:"subscriptions"`
	Rules    []state.ShuntRule    `json:"rules"`
	Devices  []state.Device       `json:"devices"`
	Warp     *state.Warp          `json:"warp,omitempty"`
	Share    *state.Share         `json:"share,omitempty"`
}

const backupApp = "foxgozar"

func (s *Server) backup(w http.ResponseWriter, r *http.Request) (any, error) {
	st := s.App.Store.View()
	b := Backup{App: backupApp, Exported: time.Now().Unix(), Settings: st.Settings, Nodes: st.Nodes,
		Subs: st.Subs, Rules: st.Rules, Devices: st.Devices, Warp: st.Warp, Share: st.Share}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="foxgozar-backup-%s.json"`, time.Now().Format("2006-01-02")))
	return b, nil
}

// restore replaces everything with a backup. Whether the tunnel is on stays
// as it is now; the rest is the backup's.
func (s *Server) restore(w http.ResponseWriter, r *http.Request) (any, error) {
	var b Backup
	if err := decode(r, &b); err != nil {
		return nil, err
	}
	if b.App != backupApp {
		return nil, httpErr{400, "not a FoxGozar backup file"}
	}
	// Fields a backup from an older version lacks take today's defaults.
	merged := state.Defaults()
	raw, _ := json.Marshal(b.Settings)
	_ = json.Unmarshal(raw, &merged)
	if err := validateSettings(merged); err != nil {
		return nil, fmt.Errorf("backup settings: %w", err)
	}
	for _, d := range b.Devices {
		if !macRe.MatchString(d.MAC) {
			return nil, fmt.Errorf("backup has a bad device address %q", d.MAC)
		}
	}
	err := s.App.Store.Update(func(st *state.State) error {
		merged.Enabled = st.Settings.Enabled
		st.Settings = merged
		st.Nodes, st.Subs, st.Rules, st.Devices, st.Warp, st.Share = b.Nodes, b.Subs, b.Rules, b.Devices, b.Warp, b.Share
		if st.Nodes == nil {
			st.Nodes = []state.Node{}
		}
		if st.Subs == nil {
			st.Subs = []state.Subscription{}
		}
		if st.Rules == nil {
			st.Rules = []state.ShuntRule{}
		}
		if st.Devices == nil {
			st.Devices = []state.Device{}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.App.Log.Add("foxgozar", fmt.Sprintf("restored a backup: %d servers, %d subscriptions, %d rules, %d devices",
		len(b.Nodes), len(b.Subs), len(b.Rules), len(b.Devices)))
	if err := s.App.EnsureRuleSets(context.Background()); err != nil {
		s.App.Log.Add("foxgozar", "rule-sets: "+err.Error())
	}
	if err := s.App.SyncShare(); err != nil {
		s.App.Log.Add("foxgozar", "sharing: "+err.Error())
	}
	return map[string]int{"nodes": len(b.Nodes), "subscriptions": len(b.Subs), "rules": len(b.Rules), "devices": len(b.Devices)},
		s.App.Apply(context.Background())
}

func subByID(s *Server, id string) *state.Subscription {
	v := s.App.Store.View()
	return v.SubByID(id)
}

func (s *Server) devices(w http.ResponseWriter, r *http.Request) (any, error) {
	return s.App.Devices(), nil
}

var macRe = regexp.MustCompile(`^[0-9a-f]{2}(:[0-9a-f]{2}){5}$`)

func (s *Server) setDevice(w http.ResponseWriter, r *http.Request) (any, error) {
	mac := strings.ToLower(r.PathValue("mac"))
	if !macRe.MatchString(mac) {
		return nil, httpErr{400, "not a MAC address"}
	}
	var req struct {
		Name   *string `json:"name"`
		Policy *string `json:"policy"`
		Node   *string `json:"node"`
		// absent: unchanged; null: removed
		Schedule json.RawMessage `json:"schedule"`
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	st := s.App.Store.View()
	d := state.Device{MAC: mac, Policy: st.Settings.DefaultPolicy}
	if cur := st.DeviceByMAC(mac); cur != nil {
		d = *cur
	}
	if req.Name != nil {
		d.Name = strings.TrimSpace(*req.Name)
	}
	if req.Policy != nil {
		d.Policy = *req.Policy
	}
	if req.Node != nil {
		d.Node = *req.Node
	}
	switch {
	case len(req.Schedule) == 0:
	case string(req.Schedule) == "null":
		d.Schedule = nil
	default:
		var sc state.Schedule
		if err := json.Unmarshal(req.Schedule, &sc); err != nil || !sc.Valid() {
			return nil, errors.New("schedule: give two different times as HH:MM")
		}
		d.Schedule = &sc
	}
	switch d.Policy {
	case "proxy", "direct", "block":
		d.Node = ""
	case "node":
		if st.NodeByID(d.Node) == nil {
			return nil, errors.New("choose a server that exists")
		}
	default:
		return nil, errors.New("policy must be proxy, direct, node or block")
	}
	return nil, s.App.SetDevice(context.Background(), d)
}

func (s *Server) forgetDevice(w http.ResponseWriter, r *http.Request) (any, error) {
	return nil, s.App.ForgetDevice(context.Background(), strings.ToLower(r.PathValue("mac")))
}

func (s *Server) warpStatus(w http.ResponseWriter, r *http.Request) (any, error) {
	return s.App.WarpStatus(r.Context()), nil
}

func (s *Server) servicesLast(w http.ResponseWriter, r *http.Request) (any, error) {
	return s.App.LastServices(), nil
}

func (s *Server) servicesTest(w http.ResponseWriter, r *http.Request) (any, error) {
	return s.App.TestServices(r.Context())
}

// share gives a node's share link and its QR code, for the phone apps.
func (s *Server) share(w http.ResponseWriter, r *http.Request) (any, error) {
	st := s.App.Store.View()
	n := st.NodeByID(r.PathValue("id"))
	if n == nil {
		return nil, httpErr{404, "no such server"}
	}
	if n.Link == "" {
		return nil, errors.New("this server was added as a sing-box JSON config, so it has no share link")
	}
	svg, err := qrsvg.Encode(n.Link)
	if err != nil {
		return map[string]string{"link": n.Link}, nil // the link alone still helps
	}
	return map[string]string{"link": n.Link, "svg": svg}, nil
}

func (s *Server) antiLag(w http.ResponseWriter, r *http.Request) (any, error) {
	return s.App.AntiLagStatus(), nil
}

func (s *Server) putAntiLag(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		On   bool `json:"on"`
		Down int  `json:"down_mbps"`
		Up   int  `json:"up_mbps"`
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if err := s.App.SetAntiLag(req.On, req.Down, req.Up); err != nil {
		return nil, err
	}
	return s.App.AntiLagStatus(), nil
}

func (s *Server) searchGames(w http.ResponseWriter, r *http.Request) (any, error) {
	return s.App.SearchGames(r.Context(), r.URL.Query().Get("q"))
}

func (s *Server) addFound(w http.ResponseWriter, r *http.Request) (any, error) {
	var hit app.GameHit
	if err := decode(r, &hit); err != nil {
		return nil, err
	}
	if err := s.App.AddFound(r.Context(), hit); err != nil {
		return nil, err
	}
	return s.App.LastGames(), nil
}

func (s *Server) checkGameDirect(w http.ResponseWriter, r *http.Request) (any, error) {
	return s.App.CheckGameDirect(r.Context(), r.PathValue("id"))
}

func (s *Server) removeFromList(w http.ResponseWriter, r *http.Request) (any, error) {
	if err := s.App.RemoveFromList(r.PathValue("id")); err != nil {
		return nil, err
	}
	return s.App.LastGames(), nil
}

func (s *Server) hiddenGames(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Hidden []string `json:"hidden"`
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if err := s.App.SetHiddenGames(req.Hidden); err != nil {
		return nil, err
	}
	return s.App.LastGames(), nil
}

func (s *Server) addGame(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if _, err := s.App.AddGame(r.Context(), req.Name); err != nil {
		return nil, err
	}
	return s.App.LastGames(), nil
}

func (s *Server) removeGame(w http.ResponseWriter, r *http.Request) (any, error) {
	if err := s.App.RemoveGame(r.PathValue("id")); err != nil {
		return nil, err
	}
	return s.App.LastGames(), nil
}

func (s *Server) gameRoute(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Via string `json:"via"` // "direct", a server id, or "" for the tunnel
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if err := s.App.SetGameRoute(r.Context(), r.PathValue("id"), req.Via); err != nil {
		return nil, err
	}
	return s.App.LastGames(), nil
}

func (s *Server) doctor(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Site string `json:"site"`
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	return s.App.Doctor(r.Context(), req.Site)
}

func (s *Server) doctorApply(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Site   string `json:"site"`
		Target string `json:"target"`
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	return nil, s.App.ApplyDoctor(r.Context(), req.Site, req.Target)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) (any, error) {
	return s.App.Health(r.Context()), nil
}

func (s *Server) uptime(w http.ResponseWriter, r *http.Request) (any, error) {
	return s.App.Uptime(), nil
}

func (s *Server) usage(w http.ResponseWriter, r *http.Request) (any, error) {
	m, nodes := s.App.MonthUsage()
	var up, down int64
	for _, x := range nodes {
		up, down = up+x[0], down+x[1]
	}
	return map[string]any{"month": m, "nodes": nodes, "up": up, "down": down}, nil
}

func (s *Server) speedResults(w http.ResponseWriter, r *http.Request) (any, error) {
	return s.App.SpeedResults(), nil
}

func (s *Server) speedTest(w http.ResponseWriter, r *http.Request) (any, error) {
	return s.App.SpeedTest(r.Context(), r.PathValue("id"))
}

func (s *Server) shareServer(w http.ResponseWriter, r *http.Request) (any, error) {
	return s.App.ShareInfo(), nil
}

func (s *Server) addShareUser(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Name  string `json:"name"`
		Limit int64  `json:"limit"` // bytes, 0 = none
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if _, err := s.App.AddShareUser(req.Name, req.Limit); err != nil {
		return nil, err
	}
	return s.App.ShareInfo(), nil
}

func (s *Server) editShareUser(w http.ResponseWriter, r *http.Request) (any, error) {
	var p app.ShareUserPatch
	if err := decode(r, &p); err != nil {
		return nil, err
	}
	if err := s.App.UpdateShareUser(r.PathValue("id"), p); err != nil {
		return nil, err
	}
	return s.App.ShareInfo(), nil
}

func (s *Server) deleteShareUser(w http.ResponseWriter, r *http.Request) (any, error) {
	if err := s.App.DeleteShareUser(r.PathValue("id")); err != nil {
		return nil, err
	}
	return s.App.ShareInfo(), nil
}

// shareUserLink is one account's link and its QR code.
func (s *Server) shareUserLink(w http.ResponseWriter, r *http.Request) (any, error) {
	for _, u := range s.App.ShareInfo().Users {
		if u.ID != r.PathValue("id") {
			continue
		}
		if u.Link == "" {
			return nil, errors.New("no link yet: turn sharing on, and the router needs an internet address")
		}
		svg, _ := qrsvg.Encode(u.Link)
		return map[string]string{"link": u.Link, "svg": svg, "name": u.Name}, nil
	}
	return nil, httpErr{404, "no such account"}
}

func (s *Server) putShareServer(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Enabled bool   `json:"enabled"`
		Port    int    `json:"port"`
		SNI     string `json:"sni"`
		NewKey  bool   `json:"new_key"`
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	if _, err := s.App.SetShare(r.Context(), req.Enabled, req.Port, req.SNI, req.NewKey); err != nil {
		return nil, err
	}
	return s.shareServer(w, r)
}

func (s *Server) gamesLast(w http.ResponseWriter, r *http.Request) (any, error) {
	return s.App.LastGames(), nil
}

func (s *Server) gamesTest(w http.ResponseWriter, r *http.Request) (any, error) {
	return s.App.TestGames(r.Context())
}

func (s *Server) ping(w http.ResponseWriter, r *http.Request) (any, error) {
	var req struct {
		Node, URL string
	}
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	return map[string]int{"ms": s.App.PingVia(r.Context(), req.Node, req.URL)}, nil
}
