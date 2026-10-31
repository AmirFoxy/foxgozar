package app

import (
	"errors"
	"net"
	"net/http"
	"testing"

	"foxgozar/internal/state"
)

func TestCleanDomain(t *testing.T) {
	for in, want := range map[string]string{
		"example.com":                      "example.com",
		"https://www.Example.com/path?x=1": "example.com",
		"  chat.openai.com  ":              "chat.openai.com",
		"http://sub.site.ir:8080/a":        "sub.site.ir",
	} {
		if got, err := cleanDomain(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "localhost", "not a site"} {
		if _, err := cleanDomain(bad); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}

func TestFilterAddress(t *testing.T) {
	if !filterAddress(net.ParseIP("10.10.34.36")) || filterAddress(net.ParseIP("10.10.35.1")) || filterAddress(net.ParseIP("8.8.8.8")) {
		t.Fatal("only 10.10.34.0/24 is Iran's filter")
	}
}

func TestSuggestRoute(t *testing.T) {
	d := func(ok bool) PathResult { return PathResult{Path: DirectPath, OK: ok} }
	s := func(id string, ok bool, ms int) PathResult { return PathResult{Path: id, OK: ok, Ms: ms} }
	cases := []struct {
		paths     []PathResult
		want, why string
	}{
		{[]PathResult{d(true), s("a", true, 300)}, "direct", "direct-works"},
		{[]PathResult{d(false), s("a", true, 300), s("b", true, 200)}, "proxy", "only-tunnel"},
		{[]PathResult{d(false), s("a", false, 0), s("b", true, 200), s("c", true, 150)}, "c", "only-some"},
		{[]PathResult{d(false), s("a", false, 0)}, "", "nothing"},
	}
	for i, c := range cases {
		if got, why := suggestRoute(c.paths); got != c.want || why != c.why {
			t.Errorf("case %d: %s %s", i, got, why)
		}
	}
}

func TestCurrentRoute(t *testing.T) {
	st := state.State{Rules: []state.ShuntRule{{Enabled: true, Domains: []string{"openai.com"}, Target: "nX"}}}
	if currentRoute(st, "chat.openai.com") != "nX" || currentRoute(st, "notopenai.com") != "" {
		t.Fatal("suffix match on whole labels only")
	}
}

func TestBotCheckAndCountry(t *testing.T) {
	h := http.Header{}
	h.Set("Cf-Mitigated", "challenge")
	if !botCheck(h, nil) {
		t.Fatal("Cloudflare's header is a challenge")
	}
	if !botCheck(http.Header{}, []byte("<title>Just a moment...</title>")) {
		t.Fatal("a challenge page")
	}
	if botCheck(http.Header{}, []byte(`{"error":{"code":"unsupported_country_region_territory"}}`)) || !countryBlock([]byte(`unsupported_country_region_territory`)) {
		t.Fatal("a country block is a refusal, not a challenge")
	}
	if botCheck(http.Header{}, []byte("Forbidden")) {
		t.Fatal("a plain 403 is not a challenge")
	}
}

func TestSuggestCautious(t *testing.T) {
	// A browser check directly, the servers fine: keep the tunnel.
	paths := []PathResult{{Path: DirectPath, OK: true, Detail: "challenge"}, {Path: "a", OK: true, Ms: 300}}
	if got, _ := suggestRoute(paths); got != "proxy" {
		t.Fatalf("challenge only: %s", got)
	}
	if !sanctioned("chat.openai.com") || !sanctioned("open.spotify.com") || sanctioned("notspotify.com") || sanctioned("digikala.com") {
		t.Fatal("sanctioned list matches whole labels")
	}
}

func TestClassifyIntercepted(t *testing.T) {
	p, d := classify(errors.New(`Get "https://youtube.com/": http: server gave HTTP response to HTTPS client`))
	if p != "filtered" || d != "intercepted" {
		t.Fatalf("%s %s", p, d)
	}
}
