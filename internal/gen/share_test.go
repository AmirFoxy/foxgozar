package gen

import (
	"encoding/json"
	"strings"
	"testing"

	"foxgozar/internal/link"
	"foxgozar/internal/state"
)

func TestShare(t *testing.T) {
	sh := state.Share{Enabled: true, Port: 8443, SNI: "www.speedtest.net",
		PrivateKey: "priv", PublicKey: "jNXHt1yRo0vDuchQlIP6Z0ZvjT3KtzVI-T4E7RoLJS0", ShortID: "ab12",
		Users: []state.ShareUser{
			{ID: "ua", Name: "Ali", UUID: "11111111-2222-3333-4444-555555555555", Enabled: true},
			{ID: "ub", Name: "Sara", UUID: "11111111-2222-3333-4444-666666666666", Enabled: false},
		}}
	b, err := BuildShare(sh, 9190, "s")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`"listen_port": 8443`, `"default_mark": 255`, `"ip_is_private": true`, `"private_key": "priv"`,
		`"tag": "u-ua"`, `"external_controller": "127.0.0.1:9190"`} {
		if !strings.Contains(s, want) {
			t.Errorf("share config lacks %s", want)
		}
	}
	if strings.Contains(s, "666666666666") || strings.Contains(s, "u-ub") {
		t.Error("a paused account must not be able to connect")
	}
	var cfg struct {
		Route struct{ Rules []map[string]any }
	}
	_ = json.Unmarshal(b, &cfg)
	// The private-address block comes before the per-account rules.
	if cfg.Route.Rules[1]["ip_is_private"] != true || cfg.Route.Rules[2]["outbound"] != "u-ua" {
		t.Fatalf("rule order: %v", cfg.Route.Rules)
	}

	l := ShareLink(sh, sh.Users[0], "5.6.7.8", "FoxGozar Ali")
	n, err := link.Parse(l)
	if err != nil {
		t.Fatalf("%s: %v", l, err)
	}
	if n.Server != "5.6.7.8" || n.Port != 8443 || n.Type != "vless" || n.Name != "FoxGozar Ali" || n.Outbound["uuid"] != sh.Users[0].UUID {
		t.Fatalf("link parsed as %+v", n)
	}
	tls, _ := n.Outbound["tls"].(map[string]any)
	r, _ := tls["reality"].(map[string]any)
	if r["public_key"] != sh.PublicKey || r["short_id"] != "ab12" || tls["server_name"] != sh.SNI {
		t.Fatalf("reality settings lost: %v", tls)
	}

	sh.Users[0].Enabled = false
	if _, err := BuildShare(sh, 9190, "s"); err == nil {
		t.Fatal("no active account: nothing to run")
	}
}
