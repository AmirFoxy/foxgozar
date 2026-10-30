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
	"strconv"
	"strings"
	"sync"
	"time"

	"zirgozar/internal/app"
	"zirgozar/internal/clash"
	"zirgozar/internal/core"
	"zirgozar/internal/link"
	"zirgozar/internal/state"
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

	h("GET /api/subs", s.subs)
	h("POST /api/subs", s.addSub)
	h("PUT /api/subs/{id}", s.editSub)
	h("DELETE /api/subs/{id}", s.deleteSub)
	h("POST /api/subs/{id}/update", s.updateSub)

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
		"object": "luci-app-zirgozar", "function": "write"})
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
	return next, s.App.Apply(context.Background())
}

func validateSettings(s state.Settings) error {
	ports := map[int]string{}
	for name, p := range map[string]int{"tproxy_port": s.TProxyPort, "dns_port": s.DNSPort, "clash_port": s.ClashPort, "api_port": s.APIPort, "mixed_port": s.MixedPort} {
		if name == "mixed_port" && p == 0 {
			continue
		}
		if p < 1 || p > 65535 {
			return fmt.Errorf("%s: invalid port", name)
		}
		if o, dup := ports[p]; dup {
			return fmt.Errorf("%s and %s use the same port", name, o)
		}
		ports[p] = name
	}
	if s.Mode != "auto" && s.Mode != "manual" {
		return errors.New("mode must be auto or manual")
	}
	if len(s.LANInterfaces) == 0 {
		return errors.New("at least one LAN interface is needed")
	}
	switch s.ACLMode {
	case "all", "only", "except":
	default:
		return errors.New("acl_mode must be all, only or except")
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
	}
	if err := s.App.Store.Update(func(st *state.State) error { st.Rules = rules; return nil }); err != nil {
		return nil, err
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
	for r.Context().Err() == nil {
		_ = s.App.Clash().StreamTraffic(r.Context(), func(t clash.Traffic) {
			fmt.Fprintf(w, "data: {\"up\":%d,\"down\":%d}\n\n", t.Up, t.Down)
			fl.Flush()
		})
		// Core not running or restarting: report zero and retry.
		fmt.Fprint(w, "data: {\"up\":0,\"down\":0}\n\n")
		fl.Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}
}

func (s *Server) coreInfo(w http.ResponseWriter, r *http.Request) (any, error) {
	return map[string]string{
		"sing_box": core.Version(s.App.SingBoxBin()),
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

func subByID(s *Server, id string) *state.Subscription {
	v := s.App.Store.View()
	return v.SubByID(id)
}
