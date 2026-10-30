package link

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func get(m map[string]any, path ...string) any {
	var v any = m
	for _, p := range path {
		mm, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = mm[p]
	}
	return v
}

func TestVLESSReality(t *testing.T) {
	n, err := Parse("vless://11111111-2222-3333-4444-555555555555@1.2.3.4:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=www.speedtest.net&fp=chrome&pbk=PUBKEY&sid=ab12&type=tcp#My%20Node")
	if err != nil {
		t.Fatal(err)
	}
	ob := n.Outbound
	if n.Name != "My Node" || ob["type"] != "vless" || ob["server_port"] != 443 || ob["flow"] != "xtls-rprx-vision" {
		t.Fatalf("bad: %+v", n)
	}
	if get(ob, "tls", "reality", "public_key") != "PUBKEY" || get(ob, "tls", "server_name") != "www.speedtest.net" ||
		get(ob, "tls", "utls", "fingerprint") != "chrome" {
		t.Fatalf("bad tls: %v", ob["tls"])
	}
	if ob["transport"] != nil {
		t.Fatal("tcp must have no transport")
	}
}

func TestVLESSWebSocketEarlyData(t *testing.T) {
	n, err := Parse("vless://id@cdn.example.com:443?security=tls&type=ws&host=h.example.com&path=%2Fws%3Fed%3D2048#x")
	if err != nil {
		t.Fatal(err)
	}
	tr := n.Outbound["transport"].(map[string]any)
	if tr["path"] != "/ws" || tr["max_early_data"] != 2048 || get(tr, "headers", "Host") != "h.example.com" {
		t.Fatalf("bad transport: %v", tr)
	}
	if get(n.Outbound, "tls", "server_name") != "cdn.example.com" {
		t.Fatalf("sni should default to the server name: %v", n.Outbound["tls"])
	}
}

func TestXHTTPUnsupported(t *testing.T) {
	if _, err := Parse("vless://id@1.2.3.4:443?type=xhttp&security=tls"); err == nil || !strings.Contains(err.Error(), "Xray") {
		t.Fatalf("want an Xray error, got %v", err)
	}
}

func TestVMess(t *testing.T) {
	j := `{"v":"2","ps":"vm","add":"5.6.7.8","port":"8080","id":"uuid","aid":"0","scy":"auto","net":"grpc","type":"none","host":"","path":"svc","tls":"tls","sni":"s.example"}`
	n, err := Parse("vmess://" + base64.StdEncoding.EncodeToString([]byte(j)))
	if err != nil {
		t.Fatal(err)
	}
	if n.Name != "vm" || n.Outbound["server_port"] != 8080 || get(n.Outbound, "transport", "service_name") != "svc" {
		t.Fatalf("bad: %v", n.Outbound)
	}
}

func TestShadowsocksForms(t *testing.T) {
	userinfo := base64.RawURLEncoding.EncodeToString([]byte("chacha20-ietf-poly1305:pa:ss"))
	a, err := Parse("ss://" + userinfo + "@9.9.9.9:8388#A")
	if err != nil {
		t.Fatal(err)
	}
	whole := base64.StdEncoding.EncodeToString([]byte("chacha20-ietf-poly1305:pa:ss@9.9.9.9:8388"))
	b, err := Parse("ss://" + whole + "#B")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != b.ID || a.Outbound["password"] != "pa:ss" {
		t.Fatalf("both forms must give the same node: %v / %v", a.Outbound, b.Outbound)
	}
	c, err := Parse("ss://2022-blake3-aes-128-gcm:a2V5@1.1.1.1:443")
	if err != nil || c.Outbound["method"] != "2022-blake3-aes-128-gcm" {
		t.Fatalf("plain userinfo: %v %v", err, c.Outbound)
	}
}

func TestHysteria2PortHopping(t *testing.T) {
	n, err := Parse("hy2://pass@example.com:443,20000-30000/?obfs=salamander&obfs-password=o&sni=example.com&insecure=1#h")
	if err != nil {
		t.Fatal(err)
	}
	sp := n.Outbound["server_ports"].([]any)
	if len(sp) != 2 || sp[0] != "443:443" || sp[1] != "20000:30000" || n.Outbound["server_port"] != nil {
		t.Fatalf("bad ports: %v", n.Outbound)
	}
	if get(n.Outbound, "obfs", "type") != "salamander" || get(n.Outbound, "tls", "insecure") != true {
		t.Fatalf("bad: %v", n.Outbound)
	}
}

func TestTrojanDefaultsTLS(t *testing.T) {
	n, err := Parse("trojan://secret@t.example.com:443?type=grpc&serviceName=g#t")
	if err != nil {
		t.Fatal(err)
	}
	if get(n.Outbound, "tls", "enabled") != true || n.Outbound["password"] != "secret" {
		t.Fatalf("bad: %v", n.Outbound)
	}
}

func TestWireGuardEndpoint(t *testing.T) {
	n, err := Parse("wireguard://cHJpdg==@162.159.192.1:2408?publickey=cHVi&address=172.16.0.2,2606:4700::1&reserved=1,2,3&mtu=1280#wg")
	if err != nil {
		t.Fatal(err)
	}
	if !n.Endpoint || n.Server != "162.159.192.1" || n.Port != 2408 {
		t.Fatalf("bad: %+v", n)
	}
	addrs := n.Outbound["address"].([]any)
	if addrs[0] != "172.16.0.2/32" || addrs[1] != "2606:4700::1/128" {
		t.Fatalf("bad address: %v", addrs)
	}
}

func TestSubscriptionBase64AndDedup(t *testing.T) {
	lines := "trojan://a@x.example:443#1\ntrojan://a@x.example:443#dup\nvless://id@1.2.3.4:443?type=xhttp\ngarbage\n"
	r := ParseBody(base64.StdEncoding.EncodeToString([]byte(lines)))
	if len(r.Nodes) != 1 || len(r.Skipped) != 2 {
		t.Fatalf("nodes %d skipped %v", len(r.Nodes), r.Skipped)
	}
}

func TestSingBoxJSON(t *testing.T) {
	body := `{"outbounds":[{"type":"selector","tag":"s","outbounds":["a"]},{"type":"vless","tag":"a","server":"1.2.3.4","server_port":443,"uuid":"u"},{"type":"direct","tag":"d"}]}`
	r := ParseBody(body)
	if len(r.Nodes) != 1 || r.Nodes[0].Name != "a" || r.Nodes[0].Port != 443 {
		t.Fatalf("bad: %+v", r)
	}
	// The same server from a link must get the same ID.
	l, _ := Parse("vless://u@1.2.3.4:443?type=tcp")
	delete(l.Outbound, "packet_encoding")
	b1, _ := json.Marshal(l.Outbound)
	b2, _ := json.Marshal(r.Nodes[0].Outbound)
	if string(b1) != string(b2) {
		t.Fatalf("\n%s\n%s", b1, b2)
	}
}
