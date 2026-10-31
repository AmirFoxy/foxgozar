package gen

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"foxgozar/internal/state"
)

// ShareUserTag is the outbound of one sharing account. Every account has
// its own (all of them plain "direct"), so the open connections, which
// name their outbound, tell whose traffic is whose.
func ShareUserTag(id string) string { return "u-" + id }

// BuildShare is the config of the sharing server: a separate sing-box that
// takes VLESS/Reality connections and sends them straight out of the
// router. Its packets carry RoutingMark, so the tunnel (with "router's own
// traffic through the tunnel" on) leaves them alone: shared traffic goes
// out the router's own way, the second connection (Starlink) in dualwan.
// Nobody coming in this way reaches the LAN or the router itself.
// Only enabled accounts are in it; with none there is nothing to run.
func BuildShare(sh state.Share, clashPort int, secret string) ([]byte, error) {
	if sh.Port < 1 || sh.Port > 65535 || sh.PrivateKey == "" || sh.SNI == "" {
		return nil, fmt.Errorf("share: incomplete settings")
	}
	var users, outbounds []any
	rules := []any{
		map[string]any{"action": "sniff"},
		map[string]any{"ip_is_private": true, "action": "reject"},
	}
	for _, u := range sh.Users {
		if !u.Enabled || u.UUID == "" {
			continue
		}
		users = append(users, map[string]any{"name": u.ID, "uuid": u.UUID, "flow": "xtls-rprx-vision"})
		outbounds = append(outbounds, map[string]any{"type": "direct", "tag": ShareUserTag(u.ID)})
		rules = append(rules, map[string]any{"auth_user": []any{u.ID}, "outbound": ShareUserTag(u.ID)})
	}
	if len(users) == 0 {
		return nil, fmt.Errorf("share: no active accounts")
	}
	outbounds = append(outbounds, map[string]any{"type": "direct", "tag": "direct"})
	cfg := map[string]any{
		"log": map[string]any{"level": "warn", "timestamp": true},
		"dns": map[string]any{
			"servers": []any{map[string]any{"type": "local", "tag": "local"}},
		},
		"inbounds": []any{map[string]any{
			"type": "vless", "tag": "share-in", "listen": "::", "listen_port": sh.Port,
			"users": users,
			"tls": map[string]any{
				"enabled": true, "server_name": sh.SNI,
				"reality": map[string]any{
					"enabled":     true,
					"handshake":   map[string]any{"server": sh.SNI, "server_port": 443},
					"private_key": sh.PrivateKey,
					"short_id":    []any{sh.ShortID},
				},
			},
		}},
		"outbounds": outbounds,
		"route": map[string]any{
			"rules":                   rules,
			"final":                   "direct",
			"default_mark":            RoutingMark,
			"default_domain_resolver": "local",
		},
		"experimental": map[string]any{
			"clash_api": map[string]any{
				"external_controller": fmt.Sprintf("127.0.0.1:%d", clashPort),
				"secret":              secret,
			},
		},
	}
	return json.MarshalIndent(cfg, "", "  ")
}

// ShareLink is one account's vless:// link, for v2rayNG, Hiddify,
// Streisand, NekoBox and the like.
func ShareLink(sh state.Share, u state.ShareUser, host, name string) string {
	q := url.Values{}
	q.Set("encryption", "none")
	q.Set("flow", "xtls-rprx-vision")
	q.Set("security", "reality")
	q.Set("sni", sh.SNI)
	q.Set("fp", "chrome")
	q.Set("pbk", sh.PublicKey)
	q.Set("sid", sh.ShortID)
	q.Set("type", "tcp")
	l := url.URL{Scheme: "vless", User: url.User(u.UUID), Host: host + ":" + strconv.Itoa(sh.Port),
		RawQuery: q.Encode(), Fragment: name}
	return l.String()
}
