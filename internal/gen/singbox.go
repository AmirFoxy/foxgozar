// Package gen builds the sing-box configuration from the saved state.
//
// The design that makes switching instant: every node is in the config at
// once, behind a urltest group ("auto") and a selector ("proxy"). Changing
// node is a call to the core's API, never a restart.
package gen

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"foxgozar/internal/state"
)

const (
	TagProxy  = "proxy"
	TagAuto   = "auto"
	TagDirect = "direct"

	// Every packet the core sends carries this mark, so the firewall never
	// sends the core's own traffic back into itself.
	RoutingMark = 0xff
)

type Env struct {
	// Ports of this core instance. Two instances can run side by side while
	// one replaces the other, so these come from the caller, not Settings.
	TProxyPort, DNSPort, ClashPort, XrayBase int

	RuleDir   string // where the .srs files live
	CacheFile string
	Secret    string // Clash API secret
}

// Rule-sets, by tag. A rule-set is referenced only when its file exists, so
// a missing download never stops the core from starting.
var (
	RSIranIP     = "geoip-ir"
	RSIranSite   = "geosite-category-ir"
	RSAds        = "geosite-category-ads-all"
	RSPrivateIP  = "geoip-private"
	RSMalwareIP  = "geoip-malware"
	RSPhishingIP = "geoip-phishing"
	RSMiners     = "geosite-cryptominers"
	KnownRuleSet = []string{RSIranIP, RSIranSite, RSAds, RSPrivateIP, RSMalwareIP, RSPhishingIP, RSMiners}
)

func (e Env) has(tag string) bool {
	_, err := os.Stat(filepath.Join(e.RuleDir, tag+".srs"))
	return err == nil
}

// OrderedNodes puts the fastest known nodes first: urltest uses the first
// member until its first round of tests is in.
func OrderedNodes(nodes []state.Node) []state.Node {
	out := append([]state.Node(nil), nodes...)
	rank := func(n state.Node) int {
		switch {
		case n.Delay > 0:
			return n.Delay
		case n.Delay == 0:
			return 1 << 20
		default:
			return 1 << 21
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i]) < rank(out[j]) })
	return out
}

func Build(st state.State, env Env) ([]byte, error) {
	s := st.Settings
	nodes := OrderedNodes(st.Nodes)
	if len(nodes) == 0 {
		return nil, fmt.Errorf("no nodes")
	}

	var outbounds, endpoints []any
	tags := make([]any, 0, len(nodes))
	xrayPorts := XrayPorts(st.Nodes, env.XrayBase)
	for _, n := range nodes {
		if n.Core == "xray" {
			outbounds = append(outbounds, xraySocks(n.ID, xrayPorts[n.ID]))
			tags = append(tags, n.ID)
			continue
		}
		ob := map[string]any{}
		for k, v := range n.Outbound {
			ob[k] = v
		}
		ob["tag"] = n.ID
		// Proxy servers are named by domain often enough; resolve them
		// outside the tunnel or the tunnel can never come up.
		if host, _ := ob["server"].(string); host != "" && net.ParseIP(host) == nil {
			ob["domain_resolver"] = "dns-direct"
		}
		if s.Mux && muxable(n.Type, ob) {
			ob["multiplex"] = map[string]any{"enabled": true, "protocol": "h2mux", "max_connections": 4, "padding": true}
		}
		if s.TLSFragment {
			if t, ok := ob["tls"].(map[string]any); ok && n.Type != "hysteria2" && n.Type != "tuic" && n.Type != "hysteria" {
				t2 := map[string]any{}
				for k, v := range t {
					t2[k] = v
				}
				t2["fragment"] = true
				ob["tls"] = t2
			}
		}
		if n.Endpoint {
			endpoints = append(endpoints, ob)
		} else {
			outbounds = append(outbounds, ob)
		}
		tags = append(tags, n.ID)
	}

	interval := s.TestInterval
	if interval < 30 {
		interval = 30
	}
	tolerance := s.Tolerance
	if tolerance <= 0 {
		tolerance = 50
	}
	auto := map[string]any{
		"type": "urltest", "tag": TagAuto, "outbounds": tags,
		"url": s.TestURL, "interval": fmt.Sprintf("%ds", interval),
		"tolerance": tolerance, "idle_timeout": "30m",
		"interrupt_exist_connections": s.InterruptOnSwitch,
	}
	sel := map[string]any{
		"type": "selector", "tag": TagProxy,
		"outbounds": append([]any{TagAuto}, tags...),
		"default":   TagAuto, // the manual choice is applied through the API, so changing it never changes the config
		// Switching never cuts what is already open: those connections finish
		// on the old node, new ones use the new node.
		"interrupt_exist_connections": false,
	}
	outbounds = append(outbounds, auto, sel, map[string]any{"type": "direct", "tag": TagDirect})

	ruleSets := []any{}
	useRS := func(tag string) bool {
		if !env.has(tag) {
			return false
		}
		for _, r := range ruleSets {
			if r.(map[string]any)["tag"] == tag {
				return true
			}
		}
		ruleSets = append(ruleSets, map[string]any{"type": "local", "tag": tag, "format": "binary",
			"path": filepath.Join(env.RuleDir, tag+".srs")})
		return true
	}

	// ------------------------------------------------------------ DNS
	strategy := "ipv4_only"
	if s.IPv6 {
		strategy = "prefer_ipv4"
	}
	remote, err := dnsServer("dns-remote", s.RemoteDNS, TagProxy)
	if err != nil {
		return nil, fmt.Errorf("remote DNS: %w", err)
	}
	direct, err := dnsServer("dns-direct", s.DirectDNS, "")
	if err != nil {
		return nil, fmt.Errorf("direct DNS: %w", err)
	}
	dnsServers := []any{remote, direct,
		// resolves DNS servers given by name (dns.google, ...)
		map[string]any{"type": "udp", "tag": "dns-bootstrap", "server": "8.8.8.8"},
		// names only the router knows (router.lan, DHCP hostnames)
		map[string]any{"type": "udp", "tag": "dns-lan", "server": "127.0.0.1", "server_port": 53}}
	dnsRules := []any{
		map[string]any{"domain_suffix": []any{"lan", "local", "localdomain", "in-addr.arpa", "ip6.arpa"}, "server": "dns-lan"},
	}
	if s.FakeIP {
		dnsServers = append(dnsServers, map[string]any{"type": "fakeip", "tag": "dns-fake",
			"inet4_range": "198.18.0.0/15", "inet6_range": "fc00::/18"})
	}
	if s.BlockAds && useRS(RSAds) {
		dnsRules = append(dnsRules, map[string]any{"rule_set": []any{RSAds}, "action": "predefined", "rcode": "NXDOMAIN"})
	}
	if s.BlockMalware && useRS(RSMiners) {
		dnsRules = append(dnsRules, map[string]any{"rule_set": []any{RSMiners}, "action": "predefined", "rcode": "NXDOMAIN"})
	}
	if s.IranDirect {
		r := map[string]any{"domain_suffix": []any{"ir"}, "server": "dns-direct"}
		dnsRules = append(dnsRules, r)
		if useRS(RSIranSite) {
			dnsRules = append(dnsRules, map[string]any{"rule_set": []any{RSIranSite}, "server": "dns-direct"})
		}
	}
	for _, sr := range st.Rules {
		if sr.Enabled && sr.Target == "direct" && len(sr.Domains) > 0 {
			dnsRules = append(dnsRules, map[string]any{"domain_suffix": toAny(sr.Domains), "server": "dns-direct"})
		}
	}
	if s.FakeIP {
		dnsRules = append(dnsRules, map[string]any{"query_type": []any{"A", "AAAA"}, "server": "dns-fake"})
	}
	dns := map[string]any{
		"servers": dnsServers, "rules": dnsRules, "final": "dns-remote",
		"strategy": strategy, "cache_capacity": 4096,
	}

	// ------------------------------------------------------------ inbounds
	listen := "0.0.0.0"
	if s.IPv6 {
		listen = "::"
	}
	inbounds := []any{
		map[string]any{"type": "tproxy", "tag": "tproxy-in", "listen": listen, "listen_port": env.TProxyPort},
		map[string]any{"type": "direct", "tag": "dns-in", "listen": listen, "listen_port": env.DNSPort},
	}
	if s.MixedPort > 0 {
		// Shared by both instances during a swap, so the port stays the same
		// for devices configured to use it.
		inbounds = append(inbounds, map[string]any{"type": "mixed", "tag": "mixed-in", "listen": listen, "listen_port": s.MixedPort, "reuse_addr": true})
	}

	// ------------------------------------------------------------ route
	rules := []any{
		map[string]any{"action": "sniff", "timeout": "100ms"},
		map[string]any{"inbound": []any{"dns-in"}, "action": "hijack-dns"},
		map[string]any{"protocol": []any{"dns"}, "action": "hijack-dns"},
		map[string]any{"ip_is_private": true, "outbound": TagDirect},
	}
	if s.QUICBlock {
		rules = append(rules, map[string]any{"network": []any{"udp"}, "port": []any{443}, "action": "reject"})
	}
	if s.BlockAds && useRS(RSAds) {
		rules = append(rules, map[string]any{"rule_set": []any{RSAds}, "action": "reject"})
	}
	if s.BlockMalware {
		var rs []any
		for _, tag := range []string{RSMalwareIP, RSPhishingIP, RSMiners} {
			if useRS(tag) {
				rs = append(rs, tag)
			}
		}
		if len(rs) > 0 {
			rules = append(rules, map[string]any{"rule_set": rs, "action": "reject"})
		}
	}
	// User rules come before the Iran rule so a user can send an Iranian
	// site through the tunnel if they want.
	for _, sr := range st.Rules {
		if !sr.Enabled {
			continue
		}
		r := map[string]any{}
		if len(sr.Domains) > 0 {
			r["domain_suffix"] = toAny(sr.Domains)
		}
		if len(sr.IPs) > 0 {
			r["ip_cidr"] = toAny(sr.IPs)
		}
		var rs []any
		for _, t := range sr.RuleSet {
			if useRS(t) {
				rs = append(rs, t)
			}
		}
		if len(rs) > 0 {
			r["rule_set"] = rs
		}
		if len(r) == 0 {
			continue
		}
		switch sr.Target {
		case "block":
			r["action"] = "reject"
		case "direct":
			r["outbound"] = TagDirect
		case "proxy", "":
			r["outbound"] = TagProxy
		default:
			if st.NodeByID(sr.Target) == nil {
				continue
			}
			r["outbound"] = sr.Target
		}
		rules = append(rules, r)
	}
	if s.IranDirect {
		rules = append(rules, map[string]any{"domain_suffix": []any{"ir"}, "outbound": TagDirect})
		var rs []any
		if useRS(RSIranSite) {
			rs = append(rs, RSIranSite)
		}
		if useRS(RSIranIP) {
			rs = append(rs, RSIranIP)
		}
		if len(rs) > 0 {
			rules = append(rules, map[string]any{"rule_set": rs, "outbound": TagDirect})
		}
	}
	// Devices tied to one server: after the Iran and user rules, so those
	// still apply to them, and instead of the shared choice for the rest.
	byNode := map[string][]any{}
	var nodeOrder []string
	for _, d := range st.Devices {
		if d.Policy != "node" || st.NodeByID(d.Node) == nil {
			continue
		}
		if _, ok := byNode[d.Node]; !ok {
			nodeOrder = append(nodeOrder, d.Node)
		}
		byNode[d.Node] = append(byNode[d.Node], d.MAC)
	}
	for _, id := range nodeOrder {
		rules = append(rules, map[string]any{"source_mac_address": byNode[id], "outbound": id})
	}

	route := map[string]any{
		"rules": rules, "rule_set": ruleSets, "final": TagProxy,
		"default_mark":            RoutingMark,
		"default_domain_resolver": "dns-direct",
	}

	cfg := map[string]any{
		"log":       map[string]any{"level": logLevel(s.LogLevel), "timestamp": true},
		"dns":       dns,
		"inbounds":  inbounds,
		"outbounds": outbounds,
		"route":     route,
		"experimental": map[string]any{
			"clash_api": map[string]any{
				"external_controller": fmt.Sprintf("127.0.0.1:%d", env.ClashPort),
				"secret":              env.Secret,
			},
			"cache_file": map[string]any{"enabled": true, "path": env.CacheFile, "store_fakeip": s.FakeIP},
		},
	}
	if len(endpoints) > 0 {
		cfg["endpoints"] = endpoints
	}
	return json.MarshalIndent(cfg, "", "  ")
}

func logLevel(l string) string {
	switch l {
	case "trace", "debug", "info", "warn", "error", "fatal", "panic":
		return l
	}
	return "warn"
}

func muxable(typ string, ob map[string]any) bool {
	switch typ {
	case "vmess", "trojan", "shadowsocks":
		return true
	case "vless":
		f, _ := ob["flow"].(string)
		return f == "" // vision does not mix with mux
	}
	return false
}

func toAny(ss []string) []any {
	out := make([]any, 0, len(ss))
	for _, s := range ss {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// dnsServer turns "https://1.1.1.1/dns-query", "tls://9.9.9.9",
// "udp://8.8.8.8", "8.8.8.8" or "local" into a sing-box DNS server.
func dnsServer(tag, spec, detour string) (map[string]any, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, fmt.Errorf("empty")
	}
	if spec == "local" {
		return map[string]any{"type": "local", "tag": tag}, nil
	}
	if !strings.Contains(spec, "://") {
		spec = "udp://" + spec
	}
	u, err := url.Parse(spec)
	if err != nil {
		return nil, err
	}
	typ := u.Scheme
	switch typ {
	case "udp", "tcp", "tls", "quic", "https", "h3":
	case "doh":
		typ = "https"
	case "dot":
		typ = "tls"
	default:
		return nil, fmt.Errorf("unknown DNS scheme %q", u.Scheme)
	}
	srv := map[string]any{"type": typ, "tag": tag, "server": u.Hostname()}
	if p := u.Port(); p != "" {
		var n int
		fmt.Sscanf(p, "%d", &n)
		srv["server_port"] = n
	}
	if (typ == "https" || typ == "h3") && u.Path != "" && u.Path != "/dns-query" {
		srv["path"] = u.Path
	}
	if net.ParseIP(u.Hostname()) == nil {
		srv["domain_resolver"] = "dns-bootstrap"
	}
	if detour != "" {
		srv["detour"] = detour
	}
	return srv, nil
}
