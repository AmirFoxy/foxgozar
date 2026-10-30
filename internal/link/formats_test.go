package link

import (
	"encoding/base64"
	"testing"
)

const clashSample = `
mixed-port: 7890
mode: rule
proxies:
  - name: "🇩🇪 vless-reality"
    type: vless
    server: 1.2.3.4
    port: 443
    uuid: 11111111-2222-3333-4444-555555555555
    network: tcp
    tls: true
    udp: true
    flow: xtls-rprx-vision
    servername: www.speedtest.net
    client-fingerprint: chrome
    reality-opts:
      public-key: jNXHt1yRo0vDuchQlIP6Z0ZvjT3KtzVI-T4E7RoLJS0
      short-id: ab12
  - name: vless-ws-cdn
    type: vless
    server: cdn.example.com
    port: 443
    uuid: 11111111-2222-3333-4444-555555555555
    tls: true
    network: ws
    ws-opts:
      path: /ws
      max-early-data: 2048
      headers:
        Host: h.example.com
  - name: vless-xhttp
    type: vless
    server: x.example.com
    port: 443
    uuid: 11111111-2222-3333-4444-555555555555
    tls: true
    network: xhttp
    xhttp-opts:
      path: /x
      mode: packet-up
  - name: vmess-grpc
    type: vmess
    server: 5.6.7.8
    port: 8443
    uuid: 11111111-2222-3333-4444-555555555555
    alterId: 0
    cipher: auto
    tls: true
    servername: g.example.com
    network: grpc
    grpc-opts:
      grpc-service-name: svc
  - {name: trojan-1, type: trojan, server: t.example.com, port: 443, password: secret, sni: t.example.com, skip-cert-verify: true}
  - name: ss-obfs
    type: ss
    server: 9.9.9.9
    port: 8388
    cipher: chacha20-ietf-poly1305
    password: "pa:ss"
    plugin: obfs
    plugin-opts:
      mode: http
      host: bing.com
  - name: hy2-hop
    type: hysteria2
    server: h.example.com
    port: 443
    ports: 20000-30000
    password: pw
    obfs: salamander
    obfs-password: o
    sni: h.example.com
  - name: tuic-1
    type: tuic
    server: tuic.example.com
    port: 443
    uuid: 11111111-2222-3333-4444-555555555555
    password: pw
    congestion-controller: bbr
    alpn: [h3]
  - name: wg-1
    type: wireguard
    server: 162.159.192.1
    port: 2408
    ip: 172.16.0.2
    ipv6: 2606:4700:110:8a36::1
    private-key: YNXtAzepDqRv9H52osJVDQnznT5AL11eEWeP1fXvTHg=
    public-key: bmXOC+F1FxEMF9dyiK2H5/1SUtzH0JuVo51h2wPfgyo=
    reserved: [1, 2, 3]
    mtu: 1280
  - {name: anytls-1, type: anytls, server: a.example.com, port: 443, password: pw, sni: a.example.com}
  - {name: socks-1, type: socks5, server: 10.0.0.1, port: 1080, username: u, password: p}
  - {name: ssr-old, type: ssr, server: 1.1.1.1, port: 1, cipher: none, password: x, protocol: origin, obfs: plain}
proxy-groups:
  - {name: PROXY, type: select, proxies: [vless-ws-cdn]}
`

func TestClash(t *testing.T) {
	r := ParseBody(clashSample)
	if len(r.Nodes) != 11 || len(r.Skipped) != 1 {
		t.Fatalf("nodes %d, skipped %v", len(r.Nodes), r.Skipped)
	}
	by := map[string]map[string]any{}
	core := map[string]string{}
	for _, n := range r.Nodes {
		by[n.Name] = n.Outbound
		core[n.Name] = n.Core
	}
	if get(by["🇩🇪 vless-reality"], "tls", "reality", "short_id") != "ab12" || by["🇩🇪 vless-reality"]["flow"] != "xtls-rprx-vision" {
		t.Errorf("reality: %v", by["🇩🇪 vless-reality"])
	}
	if get(by["vless-ws-cdn"], "transport", "max_early_data") != 2048 {
		t.Errorf("ws early data: %v", by["vless-ws-cdn"]["transport"])
	}
	if core["vless-xhttp"] != "xray" {
		t.Error("xhttp should go to Xray")
	}
	if by["ss-obfs"]["plugin"] != "obfs-local" || by["ss-obfs"]["password"] != "pa:ss" {
		t.Errorf("ss: %v", by["ss-obfs"])
	}
	if sp, _ := by["hy2-hop"]["server_ports"].([]any); len(sp) == 0 {
		t.Errorf("hy2 hops: %v", by["hy2-hop"])
	}
	if get(by["trojan-1"], "tls", "insecure") != true {
		t.Errorf("trojan: %v", by["trojan-1"])
	}
	if get(by["vmess-grpc"], "transport", "service_name") != "svc" {
		t.Errorf("vmess grpc: %v", by["vmess-grpc"])
	}
}

func TestClashBase64(t *testing.T) {
	r := ParseBody(base64.StdEncoding.EncodeToString([]byte(clashSample)))
	if len(r.Nodes) != 11 {
		t.Fatalf("base64 Clash: %d nodes", len(r.Nodes))
	}
}

func TestXrayConfigs(t *testing.T) {
	body := `[
 {"remarks": "🇳🇱 NL xhttp", "outbounds": [
   {"tag": "proxy", "protocol": "vless", "settings": {"vnext": [{"address": "nl.example.com", "port": 443,
     "users": [{"id": "11111111-2222-3333-4444-555555555555", "encryption": "none"}]}]},
    "streamSettings": {"network": "xhttp", "security": "tls", "tlsSettings": {"serverName": "nl.example.com"},
     "xhttpSettings": {"path": "/x", "mode": "auto"}, "sockopt": {"dialerProxy": "fragment"}}},
   {"tag": "fragment", "protocol": "freedom", "settings": {"fragment": {"packets": "tlshello"}}},
   {"tag": "direct", "protocol": "freedom"}]},
 {"remarks": "trojan", "outbounds": [
   {"tag": "proxy", "protocol": "trojan", "settings": {"servers": [{"address": "t.example.com", "port": 443, "password": "p"}]},
    "streamSettings": {"network": "tcp", "security": "tls"}}]}
]`
	r := ParseBody(body)
	if len(r.Nodes) != 2 {
		t.Fatalf("nodes %d skipped %v", len(r.Nodes), r.Skipped)
	}
	n := r.Nodes[0]
	if n.Core != "xray" || n.Name != "🇳🇱 NL xhttp" || n.Server != "nl.example.com" || n.Port != 443 {
		t.Fatalf("bad: %+v", n)
	}
	if get(n.Outbound, "streamSettings", "sockopt", "dialerProxy") != nil {
		t.Error("dialerProxy to an outbound that is not here must go")
	}
}

func TestSIP008(t *testing.T) {
	body := `{"version": 1, "servers": [{"id": "x", "remarks": "SS one", "server": "1.2.3.4", "server_port": 8388,
  "password": "pw", "method": "aes-256-gcm"}]}`
	r := ParseBody(body)
	if len(r.Nodes) != 1 || r.Nodes[0].Name != "SS one" || r.Nodes[0].Outbound["method"] != "aes-256-gcm" {
		t.Fatalf("bad: %+v", r)
	}
}
