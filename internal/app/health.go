package app

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"foxgozar/internal/core"
	"foxgozar/internal/gen"
	"foxgozar/internal/ruleset"
)

// The health check: is the protection doing what it should, right now.

// HealthItem is one check. State is "ok", "warn", "bad" or "info".
type HealthItem struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
}

func (a *App) Health(ctx context.Context) []HealthItem {
	st := a.Store.View()
	s := st.Settings
	connected := s.Enabled && a.running()
	var mu sync.Mutex
	var out []HealthItem
	add := func(it HealthItem) { mu.Lock(); out = append(out, it); mu.Unlock() }

	if connected {
		add(HealthItem{ID: "tunnel", State: "ok"})
	} else {
		add(HealthItem{ID: "tunnel", State: "bad"})
	}

	var wg sync.WaitGroup
	if connected && s.MixedPort > 0 {
		wg.Add(2)
		go func() { defer wg.Done(); add(a.checkExit(ctx, s.MixedPort)) }()
		go func() { defer wg.Done(); add(a.checkDNSLeak(ctx, s.MixedPort, a.act().dns(s))) }()
	}
	wg.Add(1)
	go func() { defer wg.Done(); add(checkIPv6(s.IPv6, connected)) }()
	wg.Wait()

	switch {
	case s.KillSwitch:
		add(HealthItem{ID: "killswitch", State: "ok"})
	default:
		add(HealthItem{ID: "killswitch", State: "info"})
	}
	if age := ruleset.Age(a.RuleDir(), gen.RSIranIP); age > 3*24*time.Hour {
		days := int(age.Hours() / 24)
		if age > 1<<61 {
			add(HealthItem{ID: "rulesets", State: "warn", Detail: "missing"})
		} else {
			add(HealthItem{ID: "rulesets", State: "warn", Detail: fmt.Sprint(days)})
		}
	} else {
		add(HealthItem{ID: "rulesets", State: "ok"})
	}
	var subErr []string
	for _, sb := range st.Subs {
		if sb.Enabled && sb.LastError != "" {
			subErr = append(subErr, sb.Name)
		}
	}
	if len(subErr) > 0 {
		add(HealthItem{ID: "subs", State: "warn", Detail: strings.Join(subErr, ", ")})
	} else if len(st.Subs) > 0 {
		add(HealthItem{ID: "subs", State: "ok"})
	}

	order := map[string]int{"tunnel": 0, "exit": 1, "dns": 2, "ipv6": 3, "killswitch": 4, "rulesets": 5, "subs": 6}
	for i := 1; i < len(out); i++ { // a handful: insertion sort keeps it simple
		for j := i; j > 0 && order[out[j].ID] < order[out[j-1].ID]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// trace asks Cloudflare, through the tunnel, who is asking: the exit
// address and its country.
func trace(ctx context.Context, mixedPort int) (ip, loc string, err error) {
	c := core.HTTPClient(mixedPort, 12*time.Second)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://cloudflare.com/cdn-cgi/trace", nil)
	resp, err := c.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 4<<10))
	for sc.Scan() {
		k, v, _ := strings.Cut(sc.Text(), "=")
		switch k {
		case "ip":
			ip = v
		case "loc":
			loc = v
		}
	}
	return ip, loc, nil
}

func (a *App) checkExit(ctx context.Context, mixedPort int) HealthItem {
	ip, loc, err := trace(ctx, mixedPort)
	switch {
	case err != nil:
		return HealthItem{ID: "exit", State: "bad", Detail: "no answer"}
	case loc == "IR":
		return HealthItem{ID: "exit", State: "bad", Detail: ip + " IR"}
	}
	return HealthItem{ID: "exit", State: "ok", Detail: ip + " " + loc}
}

// checkDNSLeak resolves whoami.akamai.net the way a device on the LAN
// does, through FoxGozar's DNS. Akamai answers with the address of the
// resolver that asked it: one in Iran means names leak around the tunnel.
func (a *App) checkDNSLeak(ctx context.Context, mixedPort, dnsPort int) HealthItem {
	r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, fmt.Sprintf("127.0.0.1:%d", dnsPort))
	}}
	c, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	ips, err := r.LookupHost(c, "whoami.akamai.net")
	if err != nil || len(ips) == 0 {
		return HealthItem{ID: "dns", State: "warn", Detail: "no answer"}
	}
	resolver := ips[0]
	if net.ParseIP(resolver) != nil && strings.HasPrefix(resolver, "198.18.") {
		return HealthItem{ID: "dns", State: "info", Detail: "fakeip"} // Fake IP hides the answer
	}
	country := ipCountry(ctx, mixedPort, resolver)
	if country == "IR" {
		return HealthItem{ID: "dns", State: "bad", Detail: resolver + " IR"}
	}
	return HealthItem{ID: "dns", State: "ok", Detail: strings.TrimSpace(resolver + " " + country)}
}

// ipCountry asks ip-api.com, through the tunnel, where an address is.
func ipCountry(ctx context.Context, mixedPort int, ip string) string {
	c := core.HTTPClient(mixedPort, 10*time.Second)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://ip-api.com/line/"+ip+"?fields=countryCode", nil)
	resp, err := c.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 16))
	cc := strings.TrimSpace(string(b))
	if len(cc) != 2 {
		return ""
	}
	return cc
}

// checkIPv6: a line with IPv6 is a way around the tunnel unless FoxGozar
// carries IPv6 (setting on) or turns it away (setting off, connected).
func checkIPv6(on, connected bool) HealthItem {
	out, _ := exec.Command("ip", "-6", "route", "show", "default").Output()
	if strings.TrimSpace(string(out)) == "" {
		return HealthItem{ID: "ipv6", State: "ok", Detail: "none"} // the line has no IPv6: nothing to leak
	}
	if on || connected {
		return HealthItem{ID: "ipv6", State: "ok", Detail: "covered"}
	}
	return HealthItem{ID: "ipv6", State: "warn", Detail: "open"}
}
