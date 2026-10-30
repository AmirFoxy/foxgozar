package link

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"foxgozar/internal/state"
)

// Other subscription formats. Clash proxies are turned into the share links
// the rest of this package already reads, so every detail it handles
// (Reality, XHTTP, VLESS encryption, port hopping) works for them too.

// parseClash reads a Clash or Clash Meta (mihomo) config's proxies.
func parseClash(body string) (Result, bool) {
	var cfg struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if yaml.Unmarshal([]byte(body), &cfg) != nil || len(cfg.Proxies) == 0 {
		return Result{}, false
	}
	var r Result
	seen := map[string]bool{}
	for _, p := range cfg.Proxies {
		name := str(p["name"])
		l, err := clashToLink(p)
		if err != nil {
			r.Skipped = append(r.Skipped, name+": "+err.Error())
			continue
		}
		n, err := Parse(l)
		if err != nil {
			r.Skipped = append(r.Skipped, name+": "+err.Error())
			continue
		}
		if !seen[n.ID] {
			seen[n.ID] = true
			r.Nodes = append(r.Nodes, n)
		}
	}
	return r, true
}

func str(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case int:
		return strconv.Itoa(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case []any:
		parts := make([]string, 0, len(t))
		for _, x := range t {
			parts = append(parts, str(x))
		}
		return strings.Join(parts, ",")
	}
	return fmt.Sprint(v)
}

func sub(m map[string]any, key string) map[string]any {
	if x, ok := m[key].(map[string]any); ok {
		return x
	}
	return map[string]any{}
}

func yes(v any) bool {
	b, ok := v.(bool)
	return ok && b || str(v) == "true" || str(v) == "1"
}

func set(q url.Values, k, v string) {
	if v != "" {
		q.Set(k, v)
	}
}

// clashTLS fills the TLS and transport parameters shared by vless, vmess
// and trojan.
func clashTLS(p map[string]any, q url.Values) {
	sni := str(p["servername"])
	if sni == "" {
		sni = str(p["sni"])
	}
	set(q, "sni", sni)
	set(q, "fp", str(p["client-fingerprint"]))
	set(q, "alpn", str(p["alpn"]))
	if yes(p["skip-cert-verify"]) {
		q.Set("allowInsecure", "1")
	}
	if ro := sub(p, "reality-opts"); len(ro) > 0 {
		q.Set("security", "reality")
		set(q, "pbk", str(ro["public-key"]))
		set(q, "sid", str(ro["short-id"]))
	} else if yes(p["tls"]) {
		q.Set("security", "tls")
	}
	network := str(p["network"])
	switch network {
	case "", "tcp":
		q.Set("type", "tcp")
	case "ws":
		q.Set("type", "ws")
		o := sub(p, "ws-opts")
		path := str(o["path"])
		if ed := str(o["max-early-data"]); ed != "" && ed != "0" {
			path += "?ed=" + ed
		}
		set(q, "path", path)
		set(q, "host", str(sub(o, "headers")["Host"]))
	case "grpc":
		q.Set("type", "grpc")
		set(q, "serviceName", str(sub(p, "grpc-opts")["grpc-service-name"]))
	case "h2":
		q.Set("type", "h2")
		o := sub(p, "h2-opts")
		set(q, "path", str(o["path"]))
		set(q, "host", str(o["host"]))
	case "http": // HTTP header obfuscation over TCP
		q.Set("type", "tcp")
		q.Set("headerType", "http")
		o := sub(p, "http-opts")
		set(q, "path", strings.Split(str(o["path"]), ",")[0])
		set(q, "host", str(sub(o, "headers")["Host"]))
	case "xhttp":
		q.Set("type", "xhttp")
		o := sub(p, "xhttp-opts")
		set(q, "path", str(o["path"]))
		set(q, "host", str(o["host"]))
		set(q, "mode", str(o["mode"]))
	case "httpupgrade":
		q.Set("type", "httpupgrade")
		o := sub(p, "http-upgrade-opts")
		if len(o) == 0 {
			o = sub(p, "ws-opts")
		}
		set(q, "path", str(o["path"]))
		set(q, "host", str(sub(o, "headers")["Host"]))
	default:
		q.Set("type", network)
	}
}

func clashHostPort(p map[string]any) (string, error) {
	server, port := str(p["server"]), str(p["port"])
	if server == "" || port == "" {
		return "", fmt.Errorf("missing server or port")
	}
	if strings.Contains(server, ":") && !strings.HasPrefix(server, "[") {
		server = "[" + server + "]" // IPv6
	}
	return server + ":" + port, nil
}

func clashToLink(p map[string]any) (string, error) {
	hp, err := clashHostPort(p)
	if err != nil {
		return "", err
	}
	name := url.PathEscape(str(p["name"]))
	q := url.Values{}
	typ := str(p["type"])
	switch typ {
	case "vless":
		enc := str(p["encryption"])
		if enc == "" {
			enc = "none"
		}
		q.Set("encryption", enc)
		set(q, "flow", str(p["flow"]))
		clashTLS(p, q)
		return fmt.Sprintf("vless://%s@%s?%s#%s", url.PathEscape(str(p["uuid"])), hp, q.Encode(), name), nil
	case "trojan":
		clashTLS(p, q)
		if q.Get("security") == "" {
			q.Set("security", "tls")
		}
		return fmt.Sprintf("trojan://%s@%s?%s#%s", url.PathEscape(str(p["password"])), hp, q.Encode(), name), nil
	case "vmess":
		clashTLS(p, q)
		v := map[string]any{"v": "2", "ps": str(p["name"]), "add": str(p["server"]), "port": str(p["port"]),
			"id": str(p["uuid"]), "aid": str(p["alterId"]), "scy": str(p["cipher"]), "net": q.Get("type"),
			"type": q.Get("headerType"), "host": q.Get("host"), "path": q.Get("path"), "sni": q.Get("sni"),
			"alpn": q.Get("alpn"), "fp": q.Get("fp")}
		if v["type"] == "" {
			v["type"] = "none"
		}
		if v["net"] == "grpc" {
			v["path"] = q.Get("serviceName")
		}
		if q.Get("security") == "tls" {
			v["tls"] = "tls"
		}
		if q.Get("allowInsecure") == "1" {
			v["allowInsecure"] = "1"
		}
		b, _ := json.Marshal(v)
		return "vmess://" + base64.StdEncoding.EncodeToString(b), nil
	case "ss":
		user := base64.RawURLEncoding.EncodeToString([]byte(str(p["cipher"]) + ":" + str(p["password"])))
		if plugin := str(p["plugin"]); plugin != "" {
			o := sub(p, "plugin-opts")
			var parts []string
			switch plugin {
			case "obfs":
				parts = []string{"obfs-local", "obfs=" + str(o["mode"])}
				if h := str(o["host"]); h != "" {
					parts = append(parts, "obfs-host="+h)
				}
			case "v2ray-plugin":
				parts = []string{"v2ray-plugin", "mode=" + str(o["mode"])}
				if h := str(o["host"]); h != "" {
					parts = append(parts, "host="+h)
				}
				if pa := str(o["path"]); pa != "" {
					parts = append(parts, "path="+pa)
				}
				if yes(o["tls"]) {
					parts = append(parts, "tls")
				}
			default:
				return "", fmt.Errorf("%w ss plugin %q", ErrUnsupported, plugin)
			}
			q.Set("plugin", strings.Join(parts, ";"))
		}
		l := fmt.Sprintf("ss://%s@%s", user, hp)
		if len(q) > 0 {
			l += "/?" + q.Encode()
		}
		return l + "#" + name, nil
	case "hysteria2", "hy2":
		set(q, "sni", str(p["sni"]))
		if yes(p["skip-cert-verify"]) {
			q.Set("insecure", "1")
		}
		set(q, "obfs", str(p["obfs"]))
		set(q, "obfs-password", str(p["obfs-password"]))
		set(q, "mport", str(p["ports"]))
		set(q, "alpn", str(p["alpn"]))
		return fmt.Sprintf("hy2://%s@%s/?%s#%s", url.PathEscape(str(p["password"])), hp, q.Encode(), name), nil
	case "hysteria":
		auth := str(p["auth-str"])
		if auth == "" {
			auth = str(p["auth_str"])
		}
		set(q, "auth", auth)
		set(q, "upmbps", strings.Fields(str(p["up"]) + " ")[0])
		set(q, "downmbps", strings.Fields(str(p["down"]) + " ")[0])
		set(q, "obfsParam", str(p["obfs"]))
		set(q, "peer", str(p["sni"]))
		set(q, "alpn", str(p["alpn"]))
		if yes(p["skip-cert-verify"]) {
			q.Set("insecure", "1")
		}
		return fmt.Sprintf("hysteria://%s?%s#%s", hp, q.Encode(), name), nil
	case "tuic":
		set(q, "congestion_control", str(p["congestion-controller"]))
		set(q, "udp_relay_mode", str(p["udp-relay-mode"]))
		set(q, "alpn", str(p["alpn"]))
		set(q, "sni", str(p["sni"]))
		if yes(p["skip-cert-verify"]) {
			q.Set("allow_insecure", "1")
		}
		if yes(p["disable-sni"]) {
			q.Set("disable_sni", "1")
		}
		return fmt.Sprintf("tuic://%s:%s@%s?%s#%s", url.PathEscape(str(p["uuid"])), url.PathEscape(str(p["password"])), hp, q.Encode(), name), nil
	case "anytls":
		set(q, "sni", str(p["sni"]))
		if yes(p["skip-cert-verify"]) {
			q.Set("insecure", "1")
		}
		return fmt.Sprintf("anytls://%s@%s?%s#%s", url.PathEscape(str(p["password"])), hp, q.Encode(), name), nil
	case "wireguard":
		peer := p
		if peers, ok := p["peers"].([]any); ok && len(peers) > 0 {
			if pm, ok := peers[0].(map[string]any); ok {
				peer = pm
				if hp2, err := clashHostPort(pm); err == nil {
					hp = hp2
				}
			}
		}
		set(q, "publickey", str(peer["public-key"]))
		set(q, "presharedkey", str(peer["pre-shared-key"]))
		addr := str(p["ip"])
		if v6 := str(p["ipv6"]); v6 != "" {
			addr += "," + v6
		}
		set(q, "address", addr)
		set(q, "reserved", str(peer["reserved"]))
		set(q, "mtu", str(p["mtu"]))
		return fmt.Sprintf("wireguard://%s@%s?%s#%s", url.PathEscape(str(p["private-key"])), hp, q.Encode(), name), nil
	case "socks5", "http":
		scheme := "socks"
		if typ == "http" {
			scheme = "http"
			if yes(p["tls"]) {
				scheme = "https"
			}
		}
		auth := ""
		if u := str(p["username"]); u != "" {
			auth = url.UserPassword(u, str(p["password"])).String() + "@"
		}
		return fmt.Sprintf("%s://%s%s#%s", scheme, auth, hp, name), nil
	}
	return "", fmt.Errorf("%w type %q", ErrUnsupported, typ)
}

// parseSIP008 reads the Shadowsocks subscription format.
func parseSIP008(body string) (Result, bool) {
	var cfg struct {
		Servers []struct {
			Remarks    string `json:"remarks"`
			Server     string `json:"server"`
			Port       int    `json:"server_port"`
			Password   string `json:"password"`
			Method     string `json:"method"`
			Plugin     string `json:"plugin"`
			PluginOpts string `json:"plugin_opts"`
		} `json:"servers"`
	}
	if json.Unmarshal([]byte(body), &cfg) != nil || len(cfg.Servers) == 0 {
		return Result{}, false
	}
	var r Result
	for _, s := range cfg.Servers {
		user := base64.RawURLEncoding.EncodeToString([]byte(s.Method + ":" + s.Password))
		l := fmt.Sprintf("ss://%s@%s:%d", user, s.Server, s.Port)
		if s.Plugin != "" {
			l += "/?plugin=" + url.QueryEscape(s.Plugin+";"+s.PluginOpts)
		}
		n, err := Parse(l + "#" + url.PathEscape(s.Remarks))
		if err != nil {
			r.Skipped = append(r.Skipped, s.Remarks+": "+err.Error())
			continue
		}
		r.Nodes = append(r.Nodes, n)
	}
	return r, true
}

// xrayProtocols run as they are on the Xray core.
var xrayProtocols = map[string]bool{"vless": true, "vmess": true, "trojan": true, "shadowsocks": true,
	"socks": true, "http": true, "wireguard": true, "hysteria": true}

// xrayConfigNode takes the proxy outbound of a full Xray config, as panels
// and v2rayNG export them, and keeps it as an Xray node.
func xrayConfigNode(ob map[string]any, name string) (state.Node, error) {
	proto := str(ob["protocol"])
	if !xrayProtocols[proto] {
		return state.Node{}, fmt.Errorf("%w Xray protocol %q", ErrUnsupported, proto)
	}
	ob = normalizeNumbers(ob).(map[string]any)
	delete(ob, "tag")
	delete(ob, "mux")
	if ss, ok := ob["streamSettings"].(map[string]any); ok {
		if so, ok := ss["sockopt"].(map[string]any); ok {
			// Chains to other outbounds of the original file (fragment,
			// noise) do not exist here.
			delete(so, "dialerProxy")
		}
	}
	server, port := "", 0
	settings := sub(ob, "settings")
	for _, key := range []string{"vnext", "servers", "peers"} {
		if list, ok := settings[key].([]any); ok && len(list) > 0 {
			if m, ok := list[0].(map[string]any); ok {
				server = str(m["address"])
				port, _ = strconv.Atoi(str(m["port"]))
				if ep := str(m["endpoint"]); ep != "" {
					if i := strings.LastIndex(ep, ":"); i > 0 {
						server = ep[:i]
						port, _ = strconv.Atoi(ep[i+1:])
					}
				}
			}
			break
		}
	}
	if server == "" { // the newer flat form: settings.address, settings.port
		server = str(settings["address"])
		port, _ = strconv.Atoi(str(settings["port"]))
	}
	if server == "" || port == 0 {
		return state.Node{}, fmt.Errorf("Xray %s outbound without a server", proto)
	}
	n := state.Node{Name: name, Core: "xray", Outbound: ob, Server: server, Port: port, Type: proto}
	n.ID = NodeID(ob)
	if strings.TrimSpace(n.Name) == "" {
		n.Name = fmt.Sprintf("%s %s:%d", proto, server, port)
	}
	return n, nil
}

// parseXrayConfigs reads one full Xray config or a list of them.
func parseXrayConfigs(body string) (Result, bool) {
	var list []map[string]any
	if strings.HasPrefix(body, "[") {
		if json.Unmarshal([]byte(body), &list) != nil {
			return Result{}, false
		}
	} else {
		var one map[string]any
		if json.Unmarshal([]byte(body), &one) != nil {
			return Result{}, false
		}
		list = []map[string]any{one}
	}
	var r Result
	found := false
	for _, cfg := range list {
		obs, _ := cfg["outbounds"].([]any)
		for _, o := range obs {
			ob, ok := o.(map[string]any)
			if !ok || ob["protocol"] == nil {
				continue
			}
			found = true
			proto := str(ob["protocol"])
			if proto == "freedom" || proto == "blackhole" || proto == "dns" || proto == "loopback" {
				continue
			}
			name := str(cfg["remarks"])
			if name == "" {
				name = str(ob["tag"])
			}
			n, err := xrayConfigNode(ob, name)
			if err != nil {
				r.Skipped = append(r.Skipped, name+": "+err.Error())
				continue
			}
			r.Nodes = append(r.Nodes, n)
			break // a config's first proxy outbound is the one it uses
		}
	}
	return r, found
}
