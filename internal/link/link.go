// Package link turns share links (vless://, vmess://, ss://, ...) and
// subscription bodies into sing-box outbounds.
package link

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"zirgozar/internal/state"
)

var ErrUnsupported = errors.New("unsupported")

// Parse reads one share link.
func Parse(raw string) (state.Node, error) {
	raw = strings.TrimSpace(raw)
	i := strings.Index(raw, "://")
	if i <= 0 {
		return state.Node{}, fmt.Errorf("not a link")
	}
	scheme := strings.ToLower(raw[:i])
	var (
		n   state.Node
		err error
	)
	switch scheme {
	case "vless":
		n, err = parseVLESSTrojan(raw, "vless")
	case "trojan":
		n, err = parseVLESSTrojan(raw, "trojan")
	case "vmess":
		n, err = parseVMess(raw)
	case "ss":
		n, err = parseSS(raw)
	case "hysteria2", "hy2":
		n, err = parseHy2(raw)
	case "hysteria", "hy":
		n, err = parseHy1(raw)
	case "tuic":
		n, err = parseTUIC(raw)
	case "anytls":
		n, err = parseAnyTLS(raw)
	case "wireguard", "wg":
		n, err = parseWG(raw)
	case "socks", "socks5", "socks4", "http", "https":
		n, err = parseSocksHTTP(raw, scheme)
	default:
		return state.Node{}, fmt.Errorf("%w scheme %q", ErrUnsupported, scheme)
	}
	if err != nil {
		return state.Node{}, err
	}
	n.Link = raw
	Finish(&n)
	return n, nil
}

// Finish fills the derived fields: ID, a name when there is none.
func Finish(n *state.Node) {
	if s, ok := n.Outbound["server"].(string); ok {
		n.Server = s
	}
	if p, ok := n.Outbound["server_port"].(int); ok {
		n.Port = p
	}
	if t, ok := n.Outbound["type"].(string); ok {
		n.Type = t
	}
	if n.Endpoint {
		if peers, ok := n.Outbound["peers"].([]any); ok && len(peers) > 0 {
			if p, ok := peers[0].(map[string]any); ok {
				n.Server, _ = p["address"].(string)
				n.Port, _ = p["port"].(int)
			}
		}
	}
	n.ID = NodeID(n.Outbound)
	if strings.TrimSpace(n.Name) == "" {
		n.Name = fmt.Sprintf("%s %s:%d", n.Type, n.Server, n.Port)
	}
}

// NodeID is stable for the same server settings, so re-importing or a
// subscription refresh keeps IDs (and therefore the user's choice).
func NodeID(ob map[string]any) string {
	b, _ := json.Marshal(ob) // map keys are sorted by encoding/json
	h := sha1.Sum(b)
	return "n" + hex.EncodeToString(h[:6])
}

// ---------------------------------------------------------------- helpers

func port(s string) (int, error) {
	p, err := strconv.Atoi(s)
	if err != nil || p <= 0 || p > 65535 {
		return 0, fmt.Errorf("bad port %q", s)
	}
	return p, nil
}

func hostPort(u *url.URL) (string, int, error) {
	h := u.Hostname()
	if h == "" {
		return "", 0, errors.New("missing server")
	}
	p, err := port(u.Port())
	return h, p, err
}

func fragment(u *url.URL) string {
	if u.Fragment != "" {
		return u.Fragment
	}
	if s, err := url.PathUnescape(u.RawFragment); err == nil {
		return s
	}
	return ""
}

func b64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	s = strings.NewReplacer("\n", "", "\r", "", " ", "").Replace(s)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, errors.New("not base64")
}

func truthy(s string) bool {
	switch strings.ToLower(s) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func splitList(s string) []any {
	var out []any
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ---------------------------------------------------------------- TLS + transports

type params struct {
	security, sni, fp, alpn, pbk, sid, insecure    string
	network, path, host, service, headerType, mode string
}

func readParams(q url.Values) params {
	g := func(keys ...string) string {
		for _, k := range keys {
			if v := q.Get(k); v != "" {
				return v
			}
		}
		return ""
	}
	return params{
		security:   strings.ToLower(g("security")),
		sni:        g("sni", "peer", "serverName"),
		fp:         g("fp", "fingerprint"),
		alpn:       g("alpn"),
		pbk:        g("pbk", "publicKey"),
		sid:        g("sid", "shortId"),
		insecure:   g("allowInsecure", "insecure", "allow_insecure", "skip-cert-verify"),
		network:    strings.ToLower(g("type", "network", "net")),
		path:       g("path"),
		host:       g("host"),
		service:    g("serviceName", "service_name"),
		headerType: strings.ToLower(g("headerType")),
		mode:       g("mode"),
	}
}

func tlsBlock(p params, server string, defaultOn bool) map[string]any {
	sec := p.security
	if sec == "" && defaultOn {
		sec = "tls"
	}
	if sec != "tls" && sec != "reality" && sec != "xtls" {
		return nil
	}
	t := map[string]any{"enabled": true}
	sni := p.sni
	if sni == "" && p.host != "" && net.ParseIP(server) != nil {
		sni = strings.Split(p.host, ",")[0]
	}
	if sni == "" && net.ParseIP(server) == nil {
		sni = server
	}
	if sni != "" {
		t["server_name"] = sni
	}
	if truthy(p.insecure) {
		t["insecure"] = true
	}
	if p.alpn != "" {
		t["alpn"] = splitList(p.alpn)
	}
	fp := strings.ToLower(p.fp)
	if sec == "reality" {
		if fp == "" || fp == "none" {
			fp = "chrome"
		}
		r := map[string]any{"enabled": true, "public_key": p.pbk}
		if p.sid != "" {
			r["short_id"] = p.sid
		}
		t["reality"] = r
	}
	if fp != "" && fp != "none" {
		if fp == "random" || fp == "randomized" {
			fp = "randomized"
		}
		t["utls"] = map[string]any{"enabled": true, "fingerprint": fp}
	}
	return t
}

// transport returns the sing-box v2ray transport, or an error when the
// network needs Xray (xhttp, kcp).
func transport(p params) (map[string]any, error) {
	switch p.network {
	case "", "tcp", "raw":
		if p.headerType == "http" {
			t := map[string]any{"type": "http", "method": "GET"}
			if p.path != "" {
				t["path"] = strings.Split(p.path, ",")[0]
			}
			if p.host != "" {
				t["host"] = splitList(p.host)
			}
			return t, nil
		}
		return nil, nil
	case "ws", "websocket":
		t := map[string]any{"type": "ws"}
		path := p.path
		if path == "" {
			path = "/"
		}
		// Xray-style early data: /path?ed=2048
		if i := strings.Index(path, "?ed="); i >= 0 {
			if ed, err := strconv.Atoi(path[i+4:]); err == nil {
				t["max_early_data"] = ed
				t["early_data_header_name"] = "Sec-WebSocket-Protocol"
				path = path[:i]
			}
		}
		t["path"] = path
		if p.host != "" {
			t["headers"] = map[string]any{"Host": p.host}
		}
		return t, nil
	case "grpc", "gun":
		t := map[string]any{"type": "grpc"}
		if p.service != "" {
			t["service_name"] = p.service
		}
		return t, nil
	case "httpupgrade":
		t := map[string]any{"type": "httpupgrade"}
		if p.host != "" {
			t["host"] = p.host
		}
		if p.path != "" {
			t["path"] = p.path
		}
		return t, nil
	case "h2", "http":
		t := map[string]any{"type": "http"}
		if p.host != "" {
			t["host"] = splitList(p.host)
		}
		if p.path != "" {
			t["path"] = p.path
		}
		return t, nil
	case "quic":
		return map[string]any{"type": "quic"}, nil
	case "xhttp", "splithttp", "kcp", "mkcp":
		return nil, fmt.Errorf("%w transport %q (needs Xray)", ErrUnsupported, p.network)
	}
	return nil, fmt.Errorf("%w transport %q", ErrUnsupported, p.network)
}

// ---------------------------------------------------------------- protocols

func parseVLESSTrojan(raw, kind string) (state.Node, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return state.Node{}, err
	}
	server, prt, err := hostPort(u)
	if err != nil {
		return state.Node{}, err
	}
	secret := u.User.Username()
	if pw, ok := u.User.Password(); ok && kind == "trojan" {
		secret = secret + ":" + pw
	}
	if s, err := url.PathUnescape(secret); err == nil {
		secret = s
	}
	if secret == "" {
		return state.Node{}, errors.New("missing id")
	}
	q := u.Query()
	p := readParams(q)
	ob := map[string]any{"type": kind, "server": server, "server_port": prt}
	if kind == "vless" {
		ob["uuid"] = secret
		if f := q.Get("flow"); f != "" {
			ob["flow"] = f
		}
		ob["packet_encoding"] = "xudp"
	} else {
		ob["password"] = secret
	}
	if t := tlsBlock(p, server, kind == "trojan"); t != nil {
		ob["tls"] = t
	}
	tr, err := transport(p)
	if err != nil {
		return state.Node{}, err
	}
	if tr != nil {
		ob["transport"] = tr
	}
	return state.Node{Name: fragment(u), Outbound: ob}, nil
}

type vmessJSON struct {
	PS       string          `json:"ps"`
	Add      string          `json:"add"`
	Port     json.RawMessage `json:"port"`
	ID       string          `json:"id"`
	Aid      json.RawMessage `json:"aid"`
	Scy      string          `json:"scy"`
	Net      string          `json:"net"`
	Type     string          `json:"type"`
	Host     string          `json:"host"`
	Path     string          `json:"path"`
	TLS      string          `json:"tls"`
	SNI      string          `json:"sni"`
	ALPN     string          `json:"alpn"`
	FP       string          `json:"fp"`
	Insecure json.RawMessage `json:"allowInsecure"`
}

func rawNum(r json.RawMessage) string {
	return strings.Trim(string(r), `" `)
}

func parseVMess(raw string) (state.Node, error) {
	body := raw[len("vmess://"):]
	if i := strings.IndexAny(body, "#?"); i >= 0 && !strings.HasPrefix(strings.TrimSpace(body), "{") {
		// some exporters append a fragment after the base64
		if _, err := b64(body[:i]); err == nil {
			body = body[:i]
		}
	}
	b, err := b64(body)
	if err != nil {
		return state.Node{}, fmt.Errorf("vmess: %w", err)
	}
	var v vmessJSON
	if err := json.Unmarshal(b, &v); err != nil {
		return state.Node{}, fmt.Errorf("vmess: %w", err)
	}
	prt, err := port(rawNum(v.Port))
	if err != nil {
		return state.Node{}, err
	}
	if v.Add == "" || v.ID == "" {
		return state.Node{}, errors.New("vmess: missing server or id")
	}
	aid, _ := strconv.Atoi(rawNum(v.Aid))
	scy := v.Scy
	if scy == "" {
		scy = "auto"
	}
	ob := map[string]any{"type": "vmess", "server": v.Add, "server_port": prt, "uuid": v.ID,
		"security": scy, "alter_id": aid, "packet_encoding": "xudp"}
	p := params{security: strings.ToLower(v.TLS), sni: v.SNI, fp: v.FP, alpn: v.ALPN,
		insecure: rawNum(v.Insecure), network: strings.ToLower(v.Net), path: v.Path, host: v.Host,
		headerType: strings.ToLower(v.Type)}
	if p.network == "grpc" {
		p.service = v.Path
	}
	if t := tlsBlock(p, v.Add, false); t != nil {
		ob["tls"] = t
	}
	tr, err := transport(p)
	if err != nil {
		return state.Node{}, err
	}
	if tr != nil {
		ob["transport"] = tr
	}
	return state.Node{Name: v.PS, Outbound: ob}, nil
}

func parseSS(raw string) (state.Node, error) {
	body := raw[len("ss://"):]
	name := ""
	if i := strings.Index(body, "#"); i >= 0 {
		name, _ = url.PathUnescape(body[i+1:])
		body = body[:i]
	}
	query := ""
	if i := strings.Index(body, "?"); i >= 0 {
		query = body[i+1:]
		body = body[:i]
	}
	body = strings.TrimSuffix(body, "/")
	var userinfo, hostport string
	if i := strings.LastIndex(body, "@"); i >= 0 {
		userinfo, hostport = body[:i], body[i+1:]
		if dec, err := b64(userinfo); err == nil && strings.Contains(string(dec), ":") {
			userinfo = string(dec)
		} else if u, err := url.PathUnescape(userinfo); err == nil {
			userinfo = u
		}
	} else {
		dec, err := b64(body)
		if err != nil {
			return state.Node{}, fmt.Errorf("ss: %w", err)
		}
		s := string(dec)
		j := strings.LastIndex(s, "@")
		if j < 0 {
			return state.Node{}, errors.New("ss: malformed")
		}
		userinfo, hostport = s[:j], s[j+1:]
	}
	method, password, ok := strings.Cut(userinfo, ":")
	if !ok {
		return state.Node{}, errors.New("ss: missing password")
	}
	host, ps, err := net.SplitHostPort(hostport)
	if err != nil {
		return state.Node{}, fmt.Errorf("ss: %w", err)
	}
	prt, err := port(ps)
	if err != nil {
		return state.Node{}, err
	}
	ob := map[string]any{"type": "shadowsocks", "server": host, "server_port": prt,
		"method": strings.ToLower(method), "password": password}
	if query != "" {
		q, _ := url.ParseQuery(query)
		if pl := q.Get("plugin"); pl != "" {
			parts := strings.SplitN(pl, ";", 2)
			plugin := parts[0]
			if plugin == "simple-obfs" {
				plugin = "obfs-local"
			}
			if plugin != "obfs-local" && plugin != "v2ray-plugin" {
				return state.Node{}, fmt.Errorf("%w ss plugin %q", ErrUnsupported, plugin)
			}
			ob["plugin"] = plugin
			if len(parts) == 2 {
				ob["plugin_opts"] = parts[1]
			}
		}
	}
	return state.Node{Name: name, Outbound: ob}, nil
}

func parseHy2(raw string) (state.Node, error) {
	// Port hopping links look like host:443,20000-30000 which url.Parse rejects.
	body := raw[strings.Index(raw, "://")+3:]
	hops := ""
	if at := strings.LastIndex(body, "@"); at >= 0 {
		rest := body[at+1:]
		end := strings.IndexAny(rest, "/?#")
		if end < 0 {
			end = len(rest)
		}
		hp := rest[:end]
		if c := strings.LastIndex(hp, ":"); c >= 0 && strings.ContainsAny(hp[c+1:], ",-") {
			ports := hp[c+1:]
			first := strings.FieldsFunc(ports, func(r rune) bool { return r == ',' || r == '-' })[0]
			hops = ports
			body = body[:at+1] + hp[:c+1] + first + rest[end:]
		}
	}
	u, err := url.Parse("hy2://" + body)
	if err != nil {
		return state.Node{}, err
	}
	server := u.Hostname()
	prt := 443
	if u.Port() != "" {
		if prt, err = port(u.Port()); err != nil {
			return state.Node{}, err
		}
	}
	pass := u.User.Username()
	if pw, ok := u.User.Password(); ok {
		pass += ":" + pw
	}
	pass, _ = url.PathUnescape(pass)
	q := u.Query()
	ob := map[string]any{"type": "hysteria2", "server": server, "server_port": prt, "password": pass}
	if mp := q.Get("mport"); mp != "" {
		hops = mp
	}
	if hops != "" {
		var sp []any
		for _, r := range strings.Split(hops, ",") {
			r = strings.ReplaceAll(strings.TrimSpace(r), "-", ":")
			if r == "" {
				continue
			}
			if !strings.Contains(r, ":") {
				r += ":" + r // sing-box wants ranges only
			}
			sp = append(sp, r)
		}
		ob["server_ports"] = sp
		delete(ob, "server_port")
		ob["hop_interval"] = "30s"
	}
	if o := q.Get("obfs"); o != "" && o != "none" {
		ob["obfs"] = map[string]any{"type": o, "password": q.Get("obfs-password")}
	}
	if up := q.Get("upmbps"); up != "" {
		if v, err := strconv.Atoi(up); err == nil {
			ob["up_mbps"] = v
		}
	}
	if dn := q.Get("downmbps"); dn != "" {
		if v, err := strconv.Atoi(dn); err == nil {
			ob["down_mbps"] = v
		}
	}
	p := readParams(q)
	t := tlsBlock(p, server, true)
	t["alpn"] = []any{"h3"}
	if p.alpn != "" {
		t["alpn"] = splitList(p.alpn)
	}
	delete(t, "utls") // QUIC: no uTLS
	if pin := q.Get("pinSHA256"); pin != "" {
		t["certificate_public_key_sha256"] = []any{strings.ReplaceAll(pin, ":", "")}
	}
	ob["tls"] = t
	n := state.Node{Name: fragment(u), Outbound: ob}
	if n.Port == 0 {
		n.Port = prt
	}
	return n, nil
}

func parseHy1(raw string) (state.Node, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return state.Node{}, err
	}
	server, prt, err := hostPort(u)
	if err != nil {
		return state.Node{}, err
	}
	q := u.Query()
	ob := map[string]any{"type": "hysteria", "server": server, "server_port": prt}
	if a := q.Get("auth"); a != "" {
		ob["auth_str"] = a
	}
	up, _ := strconv.Atoi(q.Get("upmbps"))
	dn, _ := strconv.Atoi(q.Get("downmbps"))
	if up == 0 {
		up = 50
	}
	if dn == 0 {
		dn = 100
	}
	ob["up_mbps"], ob["down_mbps"] = up, dn
	if o := q.Get("obfsParam"); o != "" {
		ob["obfs"] = o
	}
	p := readParams(q)
	t := tlsBlock(p, server, true)
	delete(t, "utls")
	if p.alpn == "" {
		t["alpn"] = []any{"hysteria"}
	}
	ob["tls"] = t
	return state.Node{Name: fragment(u), Outbound: ob}, nil
}

func parseTUIC(raw string) (state.Node, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return state.Node{}, err
	}
	server, prt, err := hostPort(u)
	if err != nil {
		return state.Node{}, err
	}
	pw, _ := u.User.Password()
	q := u.Query()
	ob := map[string]any{"type": "tuic", "server": server, "server_port": prt,
		"uuid": u.User.Username(), "password": pw}
	if cc := q.Get("congestion_control"); cc != "" {
		ob["congestion_control"] = cc
	}
	if m := q.Get("udp_relay_mode"); m != "" {
		ob["udp_relay_mode"] = m
	}
	p := readParams(q)
	t := tlsBlock(p, server, true)
	delete(t, "utls")
	if p.alpn == "" {
		t["alpn"] = []any{"h3"}
	}
	if truthy(q.Get("disable_sni")) {
		t["disable_sni"] = true
	}
	ob["tls"] = t
	return state.Node{Name: fragment(u), Outbound: ob}, nil
}

func parseAnyTLS(raw string) (state.Node, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return state.Node{}, err
	}
	server, prt, err := hostPort(u)
	if err != nil {
		return state.Node{}, err
	}
	pass, _ := url.PathUnescape(u.User.Username())
	ob := map[string]any{"type": "anytls", "server": server, "server_port": prt, "password": pass}
	ob["tls"] = tlsBlock(readParams(u.Query()), server, true)
	return state.Node{Name: fragment(u), Outbound: ob}, nil
}

func parseWG(raw string) (state.Node, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return state.Node{}, err
	}
	server, prt, err := hostPort(u)
	if err != nil {
		return state.Node{}, err
	}
	q := u.Query()
	priv, _ := url.PathUnescape(u.User.Username())
	// Keys are base64: a "+" left unescaped in the query reads as a space.
	key := func(k string) string { return strings.ReplaceAll(q.Get(k), " ", "+") }
	pub := key("publickey")
	if pub == "" {
		pub = key("peer_public_key")
	}
	if priv == "" || pub == "" {
		return state.Node{}, errors.New("wireguard: missing keys")
	}
	addr := q.Get("address")
	if addr == "" {
		addr = q.Get("ip")
	}
	var addrs []any
	for _, a := range strings.Split(addr, ",") {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if !strings.Contains(a, "/") {
			if strings.Contains(a, ":") {
				a += "/128"
			} else {
				a += "/32"
			}
		}
		addrs = append(addrs, a)
	}
	if len(addrs) == 0 {
		return state.Node{}, errors.New("wireguard: missing address")
	}
	peer := map[string]any{"address": server, "port": prt, "public_key": pub,
		"allowed_ips": []any{"0.0.0.0/0", "::/0"}}
	if psk := key("presharedkey"); psk != "" {
		peer["pre_shared_key"] = psk
	}
	if r := q.Get("reserved"); r != "" {
		var res []any
		for _, x := range strings.Split(r, ",") {
			if v, err := strconv.Atoi(strings.TrimSpace(x)); err == nil {
				res = append(res, v)
			}
		}
		if len(res) == 3 {
			peer["reserved"] = res
		}
	}
	ob := map[string]any{"type": "wireguard", "address": addrs, "private_key": priv, "peers": []any{peer}}
	if m, err := strconv.Atoi(q.Get("mtu")); err == nil && m > 0 {
		ob["mtu"] = m
	} else {
		ob["mtu"] = 1280
	}
	return state.Node{Name: fragment(u), Outbound: ob, Endpoint: true}, nil
}

func parseSocksHTTP(raw, scheme string) (state.Node, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return state.Node{}, err
	}
	server, prt, err := hostPort(u)
	if err != nil {
		return state.Node{}, err
	}
	typ := "socks"
	if scheme == "http" || scheme == "https" {
		typ = "http"
	}
	ob := map[string]any{"type": typ, "server": server, "server_port": prt}
	user := u.User.Username()
	pw, _ := u.User.Password()
	// socks://base64(user:pass)@host:port, as v2rayN exports it
	if typ == "socks" && pw == "" && user != "" {
		if dec, err := b64(user); err == nil && strings.Contains(string(dec), ":") {
			user, pw, _ = strings.Cut(string(dec), ":")
		}
	}
	if user != "" {
		ob["username"] = user
		ob["password"] = pw
	}
	if scheme == "socks4" {
		ob["version"] = "4"
	}
	if scheme == "https" {
		ob["tls"] = tlsBlock(readParams(u.Query()), server, true)
	}
	return state.Node{Name: fragment(u), Outbound: ob}, nil
}
