package link

import (
	"encoding/json"
	"strings"

	"zirgozar/internal/state"
)

// Result of reading a pasted text or a subscription body.
type Result struct {
	Nodes   []state.Node
	Skipped []string // one line per thing that could not be used, with why
}

// sing-box outbound types we pass through from a JSON config as they are.
var passThrough = map[string]bool{
	"vless": true, "vmess": true, "trojan": true, "shadowsocks": true, "hysteria": true,
	"hysteria2": true, "tuic": true, "anytls": true, "socks": true, "http": true,
	"ssh": true, "shadowtls": true, "naive": true, "snell": true,
}

// ParseBody accepts: share links one per line, the same base64-encoded
// (the usual subscription format), or a sing-box JSON config.
func ParseBody(body string) Result {
	body = strings.TrimSpace(strings.TrimPrefix(body, "\xef\xbb\xbf"))
	if strings.HasPrefix(body, "{") || strings.HasPrefix(body, "[") {
		if r, ok := parseJSON(body); ok {
			return r
		}
	}
	if !strings.Contains(body, "://") {
		if dec, err := b64(body); err == nil {
			body = string(dec)
		}
	}
	var r Result
	seen := map[string]bool{}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		n, err := Parse(line)
		if err != nil {
			r.Skipped = append(r.Skipped, short(line)+": "+err.Error())
			continue
		}
		if seen[n.ID] {
			continue
		}
		seen[n.ID] = true
		r.Nodes = append(r.Nodes, n)
	}
	return r
}

func parseJSON(body string) (Result, bool) {
	var r Result
	var cfg struct {
		Outbounds []map[string]any `json:"outbounds"`
		Endpoints []map[string]any `json:"endpoints"`
	}
	if strings.HasPrefix(body, "[") {
		if json.Unmarshal([]byte(body), &cfg.Outbounds) != nil {
			return r, false
		}
	} else if json.Unmarshal([]byte(body), &cfg) != nil {
		return r, false
	}
	add := func(ob map[string]any, endpoint bool) {
		typ, _ := ob["type"].(string)
		if typ == "" {
			if _, isXray := ob["protocol"]; isXray {
				r.Skipped = append(r.Skipped, "Xray JSON outbound: paste its share link instead")
			}
			return
		}
		if !endpoint && !passThrough[typ] {
			return // selector, urltest, direct, block, dns ...
		}
		name, _ := ob["tag"].(string)
		delete(ob, "tag")
		delete(ob, "detour")
		n := state.Node{Name: name, Outbound: normalizeNumbers(ob).(map[string]any), Endpoint: endpoint}
		Finish(&n)
		r.Nodes = append(r.Nodes, n)
	}
	for _, ob := range cfg.Outbounds {
		if t, _ := ob["type"].(string); t == "wireguard" {
			r.Skipped = append(r.Skipped, "legacy WireGuard outbound: use an endpoint")
			continue
		}
		add(ob, false)
	}
	for _, ep := range cfg.Endpoints {
		if t, _ := ep["type"].(string); t == "wireguard" {
			add(ep, true)
		}
	}
	return r, len(r.Nodes) > 0 || len(r.Skipped) > 0
}

// JSON numbers come back as float64; ports and such must stay integers so
// IDs computed from parsed links and from JSON agree.
func normalizeNumbers(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			t[k] = normalizeNumbers(x)
		}
		return t
	case []any:
		for i, x := range t {
			t[i] = normalizeNumbers(x)
		}
		return t
	case float64:
		if t == float64(int(t)) {
			return int(t)
		}
	}
	return v
}

func short(s string) string {
	if len(s) > 40 {
		return s[:40] + "…"
	}
	return s
}
