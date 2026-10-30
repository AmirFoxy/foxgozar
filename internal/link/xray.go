package link

import (
	"encoding/json"
	"net/url"
	"strings"

	"foxgozar/internal/state"
)

// needsXray reports whether a vless/trojan link uses something only Xray
// speaks: VLESS encryption (ML-KEM), XHTTP, mKCP.
func needsXray(kind string, q url.Values, p params) bool {
	if kind == "vless" {
		if e := q.Get("encryption"); e != "" && e != "none" {
			return true
		}
	}
	switch p.network {
	case "xhttp", "splithttp", "kcp", "mkcp":
		return true
	}
	return false
}

// xrayNode builds an Xray outbound (as in Xray's own JSON config) for a
// link sing-box cannot handle. foxgozar runs these in an Xray process and hands
// sing-box a local SOCKS port for each.
func xrayNode(kind, server string, port int, secret string, q url.Values, p params, name string) (state.Node, error) {
	ob := map[string]any{"protocol": kind}
	if kind == "vless" {
		user := map[string]any{"id": secret, "encryption": "none"}
		if e := q.Get("encryption"); e != "" {
			user["encryption"] = e
		}
		if f := q.Get("flow"); f != "" {
			user["flow"] = f
		}
		ob["settings"] = map[string]any{"vnext": []any{map[string]any{
			"address": server, "port": port, "users": []any{user}}}}
	} else {
		ob["settings"] = map[string]any{"servers": []any{map[string]any{
			"address": server, "port": port, "password": secret}}}
	}

	network := p.network
	switch network {
	case "", "tcp":
		network = "raw"
	case "splithttp":
		network = "xhttp"
	case "mkcp":
		network = "kcp"
	case "h2":
		network = "http"
	}
	ss := map[string]any{"network": network}

	sec := p.security
	if sec == "" && kind == "trojan" {
		sec = "tls"
	}
	sni := p.sni
	if sni == "" && p.host != "" {
		sni = strings.Split(p.host, ",")[0]
	}
	fp := p.fp
	switch sec {
	case "tls":
		t := map[string]any{}
		if sni != "" {
			t["serverName"] = sni
		}
		if p.alpn != "" {
			t["alpn"] = strings.Split(p.alpn, ",")
		}
		if fp != "" && fp != "none" {
			t["fingerprint"] = fp
		}
		if truthy(p.insecure) {
			t["allowInsecure"] = true
		}
		ss["security"] = "tls"
		ss["tlsSettings"] = t
	case "reality":
		if fp == "" || fp == "none" {
			fp = "chrome"
		}
		r := map[string]any{"serverName": sni, "fingerprint": fp, "publicKey": p.pbk}
		if p.sid != "" {
			r["shortId"] = p.sid
		}
		if spx := q.Get("spx"); spx != "" {
			r["spiderX"] = spx
		}
		if pqv := q.Get("pqv"); pqv != "" {
			r["mldsa65Verify"] = pqv
		}
		ss["security"] = "reality"
		ss["realitySettings"] = r
	default:
		ss["security"] = "none"
	}

	switch network {
	case "raw":
		if p.headerType == "http" {
			req := map[string]any{"path": []any{orSlash(p.path)}}
			if p.host != "" {
				req["headers"] = map[string]any{"Host": strings.Split(p.host, ",")}
			}
			ss["rawSettings"] = map[string]any{"header": map[string]any{"type": "http", "request": req}}
		}
	case "ws":
		ss["wsSettings"] = map[string]any{"path": orSlash(p.path), "host": p.host}
	case "httpupgrade":
		ss["httpupgradeSettings"] = map[string]any{"path": orSlash(p.path), "host": p.host}
	case "grpc":
		g := map[string]any{"serviceName": p.service}
		if p.mode == "multi" {
			g["multiMode"] = true
		}
		ss["grpcSettings"] = g
	case "xhttp":
		x := map[string]any{"path": orSlash(p.path)}
		if p.host != "" {
			x["host"] = p.host
		}
		if p.mode != "" {
			x["mode"] = p.mode
		}
		if extra := q.Get("extra"); extra != "" {
			var e map[string]any
			if json.Unmarshal([]byte(extra), &e) == nil {
				x["extra"] = e
			}
		}
		ss["xhttpSettings"] = x
	case "kcp":
		k := map[string]any{}
		if s := q.Get("seed"); s != "" {
			k["seed"] = s
		}
		if p.headerType != "" {
			k["header"] = map[string]any{"type": p.headerType}
		}
		ss["kcpSettings"] = k
	case "http":
		h := map[string]any{"path": orSlash(p.path)}
		if p.host != "" {
			h["host"] = strings.Split(p.host, ",")
		}
		ss["httpSettings"] = h
	}
	ob["streamSettings"] = ss

	n := state.Node{Name: name, Core: "xray", Outbound: ob, Server: server, Port: port, Type: kind}
	return n, nil
}

func orSlash(s string) string {
	if s == "" {
		return "/"
	}
	return s
}
