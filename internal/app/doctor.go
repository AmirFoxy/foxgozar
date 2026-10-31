package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"foxgozar/internal/core"
	"foxgozar/internal/gen"
	"foxgozar/internal/state"
)

// The site doctor: a site does not open, and the question is which way it
// does. It is tried directly and through every server, and the answer says
// what is wrong (filtered here, refused by the site, down) and which rule
// fixes it.

// PathResult is one way to the site.
type PathResult struct {
	Path   string `json:"path"` // "direct" or a server id
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Status int    `json:"status,omitempty"`
	Ms     int    `json:"ms,omitempty"`
	// What went wrong: "filtered" (Iran's filter answered), "refused" (the
	// site said no to this address: 403/451), "timeout", "reset", "tls",
	// "dns", "error".
	Problem string `json:"problem,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

type DoctorResult struct {
	Domain  string       `json:"domain"`
	Paths   []PathResult `json:"paths"`
	Suggest string       `json:"suggest"` // "direct", "proxy", a server id, or "" (nothing works)
	Why     string       `json:"why"`     // "direct-works", "only-tunnel", "only-some", "nothing"
	Current string       `json:"current,omitempty"`
	// The service refuses Iran: it never goes direct, whatever its page did.
	Sanctioned bool `json:"sanctioned,omitempty"`
}

// Iran's filter answers DNS for blocked names with these addresses.
func filterAddress(ip net.IP) bool {
	_, n, _ := net.ParseCIDR("10.10.34.0/24")
	return n.Contains(ip)
}

// cleanDomain takes what a user pastes (a link, a domain with a path) and
// keeps the host name.
func cleanDomain(s string) (string, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Hostname() == "" || !strings.Contains(u.Hostname(), ".") || strings.ContainsAny(u.Hostname(), " /") {
		return "", errors.New("give a site's address, like example.com")
	}
	return strings.TrimPrefix(u.Hostname(), "www."), nil
}

func classify(err error) (string, string) {
	if err == nil {
		return "", ""
	}
	msg := err.Error()
	switch {
	// A plain-HTTP answer on the secure port: Iran's filter stepped in with
	// its own page.
	case strings.Contains(msg, "HTTP response to HTTPS client"):
		return "filtered", "intercepted"
	case errors.Is(err, context.DeadlineExceeded) || strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline"):
		return "timeout", ""
	case strings.Contains(msg, "reset") || strings.Contains(msg, "EOF") || strings.Contains(msg, "broken pipe"):
		return "reset", ""
	case strings.Contains(msg, "tls") || strings.Contains(msg, "certificate") || strings.Contains(msg, "x509"):
		return "tls", ""
	case strings.Contains(msg, "no such host") || strings.Contains(msg, "lookup"):
		return "dns", ""
	}
	if len(msg) > 120 {
		msg = msg[:120]
	}
	return "error", msg
}

func probeSite(ctx context.Context, tr *http.Transport, domain string) PathResult {
	defer tr.CloseIdleConnections()
	c := &http.Client{Transport: tr, Timeout: 12 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+domain+"/", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36")
	t0 := time.Now()
	resp, err := c.Do(req)
	if err != nil {
		p, d := classify(err)
		return PathResult{Problem: p, Detail: d}
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
	resp.Body.Close()
	r := PathResult{Status: resp.StatusCode, Ms: int(time.Since(t0).Milliseconds())}
	switch {
	case (resp.StatusCode == 403 || resp.StatusCode == 503) && botCheck(resp.Header, body):
		// A "are you human" page (Cloudflare and the like): every program
		// that is not a browser gets one. The site answered: it is reachable.
		r.OK, r.Detail = true, "challenge"
	case resp.StatusCode == 403 || resp.StatusCode == 451:
		r.Problem = "refused"
		if countryBlock(body) {
			r.Detail = "country"
		}
	case resp.StatusCode >= 500:
		r.Problem = "error"
		r.Detail = resp.Status
	default:
		r.OK = true
	}
	return r
}

// botCheck: the answer is a browser check, not a refusal. Cloudflare marks
// its challenges with a header; other providers say so in the page.
func botCheck(h http.Header, body []byte) bool {
	if h.Get("Cf-Mitigated") == "challenge" {
		return true
	}
	b := strings.ToLower(string(body))
	if countryBlock(body) {
		return false
	}
	return strings.Contains(b, "challenge-platform") || strings.Contains(b, "just a moment") ||
		strings.Contains(b, "checking your browser") || strings.Contains(b, "captcha")
}

// countryBlock: the site refuses this country (Iran, for direct; the
// server's, through a server), and says so.
func countryBlock(body []byte) bool {
	b := strings.ToLower(string(body))
	for _, s := range []string{"unsupported_country", "not available in your country", "not available in your region",
		"error code: 1009", "has banned the country", "your country", "your region", "sanction"} {
		if strings.Contains(b, s) {
			return true
		}
	}
	return false
}

// Doctor tries the site every way there is.
func (a *App) Doctor(ctx context.Context, raw string) (DoctorResult, error) {
	domain, err := cleanDomain(raw)
	if err != nil {
		return DoctorResult{}, err
	}
	if !a.running() {
		return DoctorResult{}, errors.New("connect first: the servers are tried through the tunnel")
	}
	st := a.Store.View()
	res := DoctorResult{Domain: domain, Current: currentRoute(st, domain)}
	type job struct{ path, name string }
	jobs := []job{{DirectPath, "direct"}}
	for _, n := range st.Nodes {
		jobs = append(jobs, job{n.ID, n.Name})
	}
	out := make([]PathResult, len(jobs))
	probe := a.act().probePort(st.Settings)
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for i, j := range jobs {
		wg.Add(1)
		go func(i int, j job) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var r PathResult
			if j.path == DirectPath {
				r = a.probeDirect(ctx, domain)
			} else {
				proxy := &url.URL{Scheme: "socks5", Host: fmt.Sprintf("127.0.0.1:%d", probe), User: url.UserPassword(j.path, st.Token)}
				r = probeSite(ctx, &http.Transport{Proxy: http.ProxyURL(proxy), TLSClientConfig: &tls.Config{}}, domain)
			}
			r.Path, r.Name = j.path, j.name
			out[i] = r
		}(i, j)
	}
	wg.Wait()
	res.Paths = out
	// Services that refuse Iran often open their front page anyway and only
	// refuse after the login: never suggested direct, whatever the page did.
	if sanctioned(domain) {
		res.Sanctioned = true
		for i := range res.Paths {
			if res.Paths[i].Path == DirectPath && res.Paths[i].OK {
				res.Paths[i].OK, res.Paths[i].Problem, res.Paths[i].Detail = false, "refused", "sanctioned"
			}
		}
	}
	res.Suggest, res.Why = suggestRoute(res.Paths)
	return res, nil
}

// sanctionedServices refuse users in Iran, though some open their front
// page: what a page test cannot see.
var sanctionedServices = []string{
	"openai.com", "chatgpt.com", "oaistatic.com", "oaiusercontent.com",
	"spotify.com", "scdn.co",
	"claude.ai", "anthropic.com",
	"gemini.google.com", "aistudio.google.com", "notebooklm.google.com",
	"figma.com", "docker.com", "docker.io", "cursor.com", "cursor.sh",
}

func sanctioned(domain string) bool {
	for _, s := range sanctionedServices {
		if domain == s || strings.HasSuffix(domain, "."+s) {
			return true
		}
	}
	return false
}

// probeDirect: Iran's filter shows first in DNS, so the name is resolved
// with the provider's own DNS before the site is opened outside the tunnel.
func (a *App) probeDirect(ctx context.Context, domain string) PathResult {
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", domain)
	if err != nil {
		return PathResult{Problem: "dns"}
	}
	for _, ip := range ips {
		if filterAddress(ip) {
			return PathResult{Problem: "filtered"}
		}
	}
	d := core.MarkedDialer(gen.RoutingMark)
	d.Timeout = 8 * time.Second
	r := probeSite(ctx, &http.Transport{DialContext: d.DialContext, TLSClientConfig: &tls.Config{}}, domain)
	// A reset or a timeout on a name that resolved fine is how filtering
	// by SNI looks from here.
	if r.Problem == "reset" || r.Problem == "timeout" {
		r.Detail = "sni"
	}
	return r
}

// suggestRoute: direct when it works (faster, and no server traffic);
// otherwise the tunnel when the automatic choice can reach it; a single
// server when only some do.
func suggestRoute(paths []PathResult) (string, string) {
	var direct *PathResult
	var working []PathResult
	servers := 0
	for i := range paths {
		if paths[i].Path == DirectPath {
			direct = &paths[i]
			continue
		}
		servers++
		if paths[i].OK {
			working = append(working, paths[i])
		}
	}
	// A browser check proves the site answers, not that it works for this
	// country: direct counts only on a real page, while a server works.
	directProven := direct != nil && direct.OK && (direct.Detail != "challenge" || len(working) == 0)
	switch {
	case directProven:
		return "direct", "direct-works"
	case len(working) == 0:
		return "", "nothing"
	case len(working) == servers:
		return "proxy", "only-tunnel"
	}
	best := working[0]
	for _, w := range working[1:] {
		if w.Ms < best.Ms {
			best = w
		}
	}
	return best.Path, "only-some"
}

// currentRoute says where the site goes now by the user's own rules: a
// target, or "" when no rule names it.
func currentRoute(st state.State, domain string) string {
	for _, r := range st.Rules {
		if !r.Enabled {
			continue
		}
		for _, d := range r.Domains {
			if domain == d || strings.HasSuffix(domain, "."+d) {
				return r.Target
			}
		}
	}
	return ""
}

// ApplyDoctor makes the suggested rule: the site's own rule is updated if
// it has one, otherwise one is added.
func (a *App) ApplyDoctor(ctx context.Context, raw, target string) error {
	domain, err := cleanDomain(raw)
	if err != nil {
		return err
	}
	err = a.Store.Update(func(s *state.State) error {
		if target != "direct" && target != "proxy" && s.NodeByID(target) == nil {
			return errors.New("no such server")
		}
		for i := range s.Rules {
			if len(s.Rules[i].Domains) == 1 && s.Rules[i].Domains[0] == domain && len(s.Rules[i].IPs)+len(s.Rules[i].Keywords)+len(s.Rules[i].URLs) == 0 {
				s.Rules[i].Target, s.Rules[i].Enabled = target, true
				return nil
			}
		}
		s.Rules = append(s.Rules, state.ShuntRule{ID: "r" + state.RandomHex(4), Name: domain, Enabled: true,
			Domains: []string{domain}, Target: target})
		return nil
	})
	if err != nil {
		return err
	}
	a.Log.Add("foxgozar", "site doctor: "+domain+" now goes "+target)
	return a.Apply(ctx)
}
