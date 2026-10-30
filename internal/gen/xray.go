package gen

import (
	"encoding/json"
	"fmt"
	"strings"

	"foxgozar/internal/state"
)

// XrayBasePort is where the local SOCKS ports for Xray nodes start.
const XrayBasePort = 21000

// XrayPorts gives every Xray node a local port. It follows the order nodes
// were added in, not their delays, so measurements never move ports and
// never force a restart of Xray.
func XrayPorts(nodes []state.Node, base int) map[string]int {
	if base == 0 {
		base = XrayBasePort
	}
	ports := map[string]int{}
	i := 0
	for _, n := range nodes {
		if n.Core == "xray" {
			ports[n.ID] = base + i
			i++
		}
	}
	return ports
}

func HasXray(nodes []state.Node) bool {
	for _, n := range nodes {
		if n.Core == "xray" {
			return true
		}
	}
	return false
}

// BuildXray renders Xray's config: one SOCKS inbound per node, routed to
// that node's outbound. Everything Xray sends carries RoutingMark, and its
// own DNS lookups (for server names) go out directly, so Xray never depends
// on the tunnel it is part of.
func BuildXray(st state.State, base int) ([]byte, error) {
	ports := XrayPorts(st.Nodes, base)
	if len(ports) == 0 && !WarpOn(st) {
		return nil, fmt.Errorf("no Xray nodes")
	}
	var inbounds, outbounds, rules []any
	rules = append(rules, map[string]any{"inboundTag": []any{"dns-internal"}, "outboundTag": "direct"})
	for _, n := range st.Nodes {
		if n.Core != "xray" {
			continue
		}
		in := "in-" + n.ID
		inbounds = append(inbounds, map[string]any{
			"tag": in, "listen": "127.0.0.1", "port": ports[n.ID], "protocol": "socks",
			"settings": map[string]any{"udp": true, "auth": "noauth"},
		})
		ob := deepCopy(n.Outbound)
		ob["tag"] = n.ID
		ss, _ := ob["streamSettings"].(map[string]any)
		if ss == nil {
			ss = map[string]any{}
			ob["streamSettings"] = ss
		}
		sock, _ := ss["sockopt"].(map[string]any)
		if sock == nil {
			sock = map[string]any{}
		}
		sock["mark"] = RoutingMark
		sock["domainStrategy"] = "UseIPv4"
		if st.Settings.IPv6 {
			sock["domainStrategy"] = "UseIP"
		}
		ss["sockopt"] = sock
		outbounds = append(outbounds, ob)
		rules = append(rules, map[string]any{"inboundTag": []any{in}, "outboundTag": n.ID})
	}
	if WarpOn(st) {
		for i, ep := range st.Warp.Endpoints {
			in, out := fmt.Sprintf("in-warp-%d", i+1), WarpTag(i)
			inbounds = append(inbounds, map[string]any{
				"tag": in, "listen": "127.0.0.1", "port": WarpPort(base, i), "protocol": "socks",
				"settings": map[string]any{"udp": true, "auth": "noauth"},
			})
			ob, extra := warpOutbound(st.Warp, out, ep, i)
			outbounds = append(outbounds, ob)
			outbounds = append(outbounds, extra...)
			rules = append(rules, map[string]any{"inboundTag": []any{in}, "outboundTag": out})
		}
	}
	outbounds = append(outbounds, map[string]any{
		"tag": "direct", "protocol": "freedom",
		"streamSettings": map[string]any{"sockopt": map[string]any{"mark": RoutingMark}},
	})
	cfg := map[string]any{
		"log": map[string]any{"loglevel": xrayLogLevel(st.Settings.LogLevel)},
		"dns": map[string]any{
			"servers":       []any{xrayDNS(st.Settings.DirectDNS), "1.1.1.1"},
			"tag":           "dns-internal",
			"queryStrategy": "UseIPv4",
		},
		"inbounds":  inbounds,
		"outbounds": outbounds,
		"routing":   map[string]any{"domainStrategy": "AsIs", "rules": rules},
	}
	return json.MarshalIndent(cfg, "", "  ")
}

// sing-box's view of an Xray node: a local SOCKS server.
func xraySocks(id string, port int) map[string]any {
	return map[string]any{"type": "socks", "tag": id, "server": "127.0.0.1", "server_port": port, "version": "5"}
}

func xrayDNS(spec string) string {
	spec = strings.TrimSpace(spec)
	switch {
	case spec == "" || spec == "local":
		return "8.8.8.8"
	case strings.HasPrefix(spec, "udp://"):
		return strings.TrimPrefix(spec, "udp://")
	}
	return spec
}

func xrayLogLevel(l string) string {
	switch l {
	case "debug", "trace":
		return "debug"
	case "info":
		return "info"
	case "error", "fatal", "panic":
		return "error"
	}
	return "warning"
}

func deepCopy(m map[string]any) map[string]any {
	b, _ := json.Marshal(m)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

// WARP, carried by Xray: plain WireGuard to Cloudflare is filtered in Iran,
// but a few random UDP packets sent ahead of the handshake get a share of
// endpoints through. Measured on an Iranian ISP: 0 of 12 endpoints without
// noise, about one in four with it, so several run side by side and
// sing-box's urltest picks one that answers.
const TagWarp = "warp"

func WarpOn(st state.State) bool {
	return st.Settings.WarpBackup && st.Warp != nil && len(st.Warp.Endpoints) > 0
}

func WarpTag(i int) string         { return fmt.Sprintf("warp-%d", i+1) }
func WarpPort(base, i int) int     { return base + 1000 + i }
func NeedXray(st state.State) bool { return HasXray(st.Nodes) || WarpOn(st) }

func warpOutbound(w *state.Warp, tag, endpoint string, i int) (map[string]any, []any) {
	a := w.Account
	addrs := []any{a.Address4 + "/32"}
	wg := map[string]any{"tag": tag, "protocol": "wireguard", "settings": map[string]any{
		"secretKey": a.PrivateKey, "address": addrs, "mtu": 1280, "noKernelTun": true,
		"peers": []any{map[string]any{"publicKey": a.PeerKey, "endpoint": endpoint,
			"allowedIPs": []any{"0.0.0.0/0", "::/0"}, "keepAlive": 25}}}}
	if len(a.Reserved) == 3 {
		wg["settings"].(map[string]any)["reserved"] = []any{a.Reserved[0], a.Reserved[1], a.Reserved[2]}
	}
	mark := map[string]any{"mark": RoutingMark}
	// Two ways of adding the noise both got through in tests; alternate so a
	// change in how either is filtered leaves the other half working.
	if i%2 == 0 {
		noise := []any{
			map[string]any{"rand": "10-20", "delay": "10-16"},
			map[string]any{"rand": "10-20", "delay": "10-16"},
			map[string]any{"rand": "10-20", "delay": "10-16"},
		}
		wg["streamSettings"] = map[string]any{"sockopt": mark, "finalmask": map[string]any{
			"udp": []any{map[string]any{"type": "noise", "settings": map[string]any{"noise": noise}}}}}
		return wg, nil
	}
	via := tag + "-noise"
	wg["streamSettings"] = map[string]any{"sockopt": map[string]any{"dialerProxy": via}}
	return wg, []any{map[string]any{"tag": via, "protocol": "freedom",
		"settings": map[string]any{"noises": []any{
			map[string]any{"type": "rand", "packet": "10-20", "delay": "10-16"},
			map[string]any{"type": "rand", "packet": "10-20", "delay": "10-16"},
		}},
		"streamSettings": map[string]any{"sockopt": mark}}}
}
