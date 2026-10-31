package gen

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"foxgozar/internal/link"
	"foxgozar/internal/ruleset"
	"foxgozar/internal/state"
	"foxgozar/internal/warp"
)

var sampleLinks = []string{
	"vless://11111111-2222-3333-4444-555555555555@1.2.3.4:443?flow=xtls-rprx-vision&security=reality&sni=www.speedtest.net&fp=chrome&pbk=jNXHt1yRo0vDuchQlIP6Z0ZvjT3KtzVI-T4E7RoLJS0&sid=ab12&type=tcp#reality",
	"vless://11111111-2222-3333-4444-555555555555@cdn.example.com:443?security=tls&type=ws&host=h.example.com&path=%2Fws%3Fed%3D2048#ws",
	"trojan://secret@t.example.com:443?type=grpc&serviceName=g#trojan-grpc",
	"ss://" + "Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTpwYXNz" + "@9.9.9.9:8388#ss",
	"hy2://pass@example.com:443,20000-30000/?obfs=salamander&obfs-password=o&sni=example.com#hy2",
	"tuic://11111111-2222-3333-4444-555555555555:pw@tuic.example.com:443?congestion_control=bbr&alpn=h3#tuic",
	"anytls://pw@a.example.com:443?sni=a.example.com#anytls",
	"wireguard://YNXtAzepDqRv9H52osJVDQnznT5AL11eEWeP1fXvTHg=@162.159.192.1:2408?publickey=bmXOC+F1FxEMF9dyiK2H5/1SUtzH0JuVo51h2wPfgyo=&address=172.16.0.2&reserved=1,2,3#warp",
}

func sampleState(t *testing.T) state.State {
	st := state.State{Token: "secret", Settings: state.Defaults()}
	for _, l := range sampleLinks {
		n, err := link.Parse(l)
		if err != nil {
			t.Fatalf("%s: %v", l, err)
		}
		// Round-trip through JSON like the store does: ints become floats.
		b, _ := json.Marshal(n)
		var back state.Node
		_ = json.Unmarshal(b, &back)
		st.Nodes = append(st.Nodes, back)
	}
	st.Settings.BlockAds = true
	st.Settings.QUICBlock = true
	st.Settings.Mux = true
	st.Settings.TLSFragment = true
	st.Rules = []state.ShuntRule{
		{ID: "r1", Enabled: true, Domains: []string{"openai.com"}, Target: st.Nodes[0].ID},
		{ID: "r2", Enabled: true, Domains: []string{"example.org"}, Target: "direct"},
		{ID: "r3", Enabled: true, IPs: []string{"8.8.8.8/32"}, Target: "block"},
	}
	return st
}

func TestBuild(t *testing.T) {
	st := sampleState(t)
	dir := os.Getenv("FGZ_RULEDIR")
	if dir == "" {
		dir = t.TempDir()
	}
	b, err := Build(st, Env{RuleDir: dir, CacheFile: "cache.db", Secret: "s", TProxyPort: 7893, DNSPort: 1053, ClashPort: 9090, ProbePort: 9100})
	if err != nil {
		t.Fatal(err)
	}
	if out := os.Getenv("FGZ_DUMP"); out != "" {
		_ = os.WriteFile(out, b, 0o644)
	}
	s := string(b)
	for _, want := range []string{`"type": "urltest"`, `"type": "selector"`, `"type": "tproxy"`, `"hijack-dns"`, `"default_mark": 255`, `"endpoints"`} {
		if !strings.Contains(s, want) {
			t.Errorf("config lacks %s", want)
		}
	}
	// Changing only the selection must not change the config.
	st.Settings.Mode, st.Settings.Selected = "manual", st.Nodes[1].ID
	b2, _ := Build(st, Env{RuleDir: dir, CacheFile: "cache.db", Secret: "s", TProxyPort: 7893, DNSPort: 1053, ClashPort: 9090, ProbePort: 9100})
	if string(b) != string(b2) {
		t.Error("selection leaked into the config")
	}
}

func TestUserRuleKinds(t *testing.T) {
	st := sampleState(t)
	dir := t.TempDir()
	have, missing := "https://x.test/have.txt", "https://x.test/missing.txt"
	_ = os.WriteFile(dir+"/"+ruleset.URLTag(have)+".srs", []byte("SRS\x00"), 0o644)
	st.Rules = []state.ShuntRule{{ID: "k", Enabled: true, Keywords: []string{"openai"}, URLs: []string{have, missing}, Target: "direct"}}
	b, err := Build(st, Env{RuleDir: dir, TProxyPort: 7893, DNSPort: 1053, ClashPort: 9090})
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		DNS   struct{ Rules []map[string]any }
		Route struct {
			Rules   []map[string]any
			RuleSet []map[string]any `json:"rule_set"`
		}
	}
	_ = json.Unmarshal(b, &cfg)
	var rule map[string]any
	for _, r := range cfg.Route.Rules {
		if _, ok := r["domain_keyword"]; ok {
			rule = r
		}
	}
	if rule == nil || rule["outbound"] != TagDirect {
		t.Fatalf("keyword rule missing: %v", cfg.Route.Rules)
	}
	rs, _ := rule["rule_set"].([]any)
	if len(rs) != 1 || rs[0] != ruleset.URLTag(have) {
		t.Fatalf("only the downloaded list may be used: %v", rs)
	}
	found := false
	for _, r := range cfg.DNS.Rules {
		if _, ok := r["domain_keyword"]; ok && r["server"] == "dns-direct" {
			found = true
		}
	}
	if !found {
		t.Error("direct keyword domains should resolve with the direct DNS")
	}
}

func TestDNSServerSpecs(t *testing.T) {
	for spec, typ := range map[string]string{
		"https://1.1.1.1/dns-query": "https", "tls://9.9.9.9": "tls", "8.8.8.8": "udp",
		"local": "local", "quic://dns.adguard.com": "quic", "h3://1.1.1.1/dns-query": "h3",
	} {
		s, err := dnsServer("t", spec, "")
		if err != nil || s["type"] != typ {
			t.Errorf("%s: %v %v", spec, s, err)
		}
	}
	if _, err := dnsServer("t", "ftp://x", ""); err == nil {
		t.Error("ftp should be rejected")
	}
}

func TestBuildXray(t *testing.T) {
	links := []string{
		"vless://11111111-2222-3333-4444-555555555555@dl5.example.xyz:12082?alpn=h2%2Ch3&encryption=mlkem768x25519plus.native.0rtt.ekPNZCqVq-MrMXaDMvKSwwmRHTeAK0ecGvGJjLmf75NATPNQT_VqcAF9o0p-PMcmhlkTdJJ9vNifo4B63adrc_CK1vY1PnNfBYkjZtMwKmoqielILPQ8-6iTkdGHBaOg_cmPSXIBwUOge4UdN2JSRHwZKKi0upSfsVwPGIlsYJsORNpRC-CxxziceOBiI4aSipNzQqBxewlC_7al6bZ1YBhTXnSe_FFanGA8dCDEomzEa9CLUCTESRWujQF7iQmMZ9qVVwUh3wMVsCzJbhFRvQu_7zeLWXC-fTC7GDoBL5lwplUWGHXAyKaYVAnBvigKnAiKaQBvcYQxVbtQeRSYOgiaHwpONFxEwKo7D1F8mCOmoJU1zwNxrKsKp0yrY9vKBkqrS9Bsq6xI2svPX5ZRcoNlHsMIq3URvTgYbaKLDzShHhs_pEsPFku742QzVjGQFXB7HfSs9yKFOoM4qPgwyKGBwxUFTRG8ecGGTTrPcbRdrcc8mbmLP1UfkFF9lcaCLUAXjYuzv3MqL7qRm1oWHYlJePtK_0Cu1NERdtwzV7VqN8JfNJFtxDy_xXKsQqimJrmPNQNYwBWq_IB6z3Sz7Okv4Iod5ZsWd0pE9vUo1wxg-6t93RA4-rR5KcYIDRY6SecuNTpypVxZbThd53yhn7B0OZlrcQIcnFmbHdSYx9ZwOUqerqFJxquXonSUANEex0wAOmTGVQtX2LhQ8HyDX4d_XYyLuOozGvkkMZFNxkxQ_clBtGR9jJaguLm65To7s4SoTRNi43EipPWQiKFUlFZ9kwMKfvKLYJACGFtE6BKWJAu8SpxO-0SyouSwRtNuGjQnWrGQTylDq0HPaVGlKoeQkDpVV2t_l6kWsVMXCLBcTVgQpCiY53GXStfAEoVvG_ISIZdVKaiVCieu3LyriDV9fsiFJDlDerUTlumt4RsLsXCRlnghV0WC5KGk3qPEncsEN8jD_rcb8BiPB6bDvvNIImBwihCeQdgCdHebHnSzoAabQPCW_ORmtVpVzPtH1OI3sZcBUsvNZKwyKGMhAww07pHFjrdreGe-rghkKMHOVdMhg4yEhutMaByJKtzPkKU4WcK5kNokbjqaROx03FKtx4YxEidaaWlY9dxtd3AEMdxR25Qk8lkxpnAO0pcXTWcWUdmh0SYZPwUxyOTI6QdqmogGm8oDFZl2koCP1GqoQvmuVMpt34nPNAfAqnrGGtw6SayXORhGyfI2RXq8PnwC2ku4_hYIv1VETNIIP1kw8bdNtdYk62pOoUCLCZyKXgJS-7JPo4AOltC2ajkTr_JX-ak0doFzhJDGNlHJX3VMpVx3reBct9xtB8nNOpSfKZd2CDq2EgODFWy6Sodm0eaO1Ye4MvVW4GFYY2iCUCUF9AuMbqUHEXhtIcFShiecNvDM89Jbv7BkDHZtGTeezeKLOeMaO1IL9DGJJLURnYO4VCC48hd4F1OSQIFSGGAxYneQ4Hq_tUsQtzokZlVlvlmhLckTtMBDTZi1UAUpDJxKe0g1ukGTysyZnzeL22ZQf2ztviWGrCKARha1qJEE-Shc--k9siOHxQbTJ6l1jdM&fp=chrome&security=tls&sni=&type=tcp#enc",
		"vless://11111111-2222-3333-4444-555555555555@1.2.3.4:443?type=xhttp&security=reality&sni=www.speedtest.net&fp=chrome&pbk=jNXHt1yRo0vDuchQlIP6Z0ZvjT3KtzVI-T4E7RoLJS0&sid=ab12&mode=auto&path=%2Fx#xhttp",
	}
	st := state.State{Settings: state.Defaults()}
	for _, l := range links {
		n, err := link.Parse(l)
		if err != nil {
			t.Fatal(err)
		}
		st.Nodes = append(st.Nodes, n)
	}
	b, err := BuildXray(st, 0)
	if err != nil {
		t.Fatal(err)
	}
	if out := os.Getenv("FGZ_XRAY_DUMP"); out != "" {
		_ = os.WriteFile(out, b, 0o644)
	}
	sb, err := Build(st, Env{RuleDir: t.TempDir(), TProxyPort: 7893, DNSPort: 1053, ClashPort: 9090, ProbePort: 9100})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sb), `"server_port": 21001`) {
		t.Error("second Xray node should be a SOCKS outbound on 21001")
	}
}

func TestWarpConfigs(t *testing.T) {
	st := state.State{Settings: state.Defaults(), Token: "s"}
	n, _ := link.Parse("vless://id@1.2.3.4:443?type=tcp#a")
	st.Nodes = []state.Node{n}
	st.Settings.WarpBackup = true
	st.Warp = &state.Warp{
		Account:   warp.Account{PrivateKey: "YNXtAzepDqRv9H52osJVDQnznT5AL11eEWeP1fXvTHg=", PeerKey: "bmXOC+F1FxEMF9dyiK2H5/1SUtzH0JuVo51h2wPfgyo=", Address4: "172.16.0.2", Reserved: []int{1, 2, 3}},
		Endpoints: []string{"162.159.192.146:4233", "188.114.97.38:3854"},
	}
	x, err := BuildXray(st, 21000)
	if err != nil {
		t.Fatal(err)
	}
	if out := os.Getenv("FGZ_XRAY_DUMP"); out != "" {
		_ = os.WriteFile(out, x, 0o644)
	}
	b, err := Build(st, Env{RuleDir: t.TempDir(), TProxyPort: 7893, DNSPort: 1053, ClashPort: 9090, XrayBase: 21000})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"tag": "warp"`, `"server_port": 22001`, `"warp"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("sing-box config lacks %s", want)
		}
	}
}

// FGZ_BODY=file: parse a subscription body and dump the core configs it
// makes to FGZ_DUMP and FGZ_XRAY_DUMP, for checking with the real cores.
func TestBodyFile(t *testing.T) {
	path := os.Getenv("FGZ_BODY")
	if path == "" {
		t.Skip("FGZ_BODY not set")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	r := link.ParseBody(string(b))
	t.Logf("%d nodes, skipped: %v", len(r.Nodes), r.Skipped)
	st := state.State{Settings: state.Defaults(), Token: "s", Nodes: r.Nodes}
	sb, err := Build(st, Env{RuleDir: t.TempDir(), TProxyPort: 7893, DNSPort: 1053, ClashPort: 9090, XrayBase: 21000})
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(os.Getenv("FGZ_DUMP"), sb, 0o644)
	if x, err := BuildXray(st, 21000); err == nil {
		_ = os.WriteFile(os.Getenv("FGZ_XRAY_DUMP"), x, 0o644)
	}
}

func TestGameRoutes(t *testing.T) {
	st := sampleState(t)
	st.GameRoutes = map[string]string{"dota2": GameDirect, "valorant": st.Nodes[0].ID, "lol": "n-gone", "bf6": GameDirect}
	b, err := Build(st, Env{RuleDir: t.TempDir(), TProxyPort: 7893, DNSPort: 1053, ClashPort: 9090})
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Route struct{ Rules []map[string]any }
	}
	_ = json.Unmarshal(b, &cfg)
	got := map[string]string{} // first port -> outbound
	firstUser := -1
	for i, r := range cfg.Route.Rules {
		if pr, ok := r["port_range"].([]any); ok {
			got[pr[0].(string)], _ = r["outbound"].(string)
		}
		if _, ok := r["domain_suffix"]; ok && firstUser < 0 {
			firstUser = i
		}
	}
	if got["27015:27200"] != TagDirect || got["7000:8000"] != st.Nodes[0].ID {
		t.Fatalf("game routes: %v", got)
	}
	if len(got) != 2 {
		t.Fatalf("a removed server, or a game without known ports, must not get a rule: %v", got)
	}
}

func TestGameDownloadsDirect(t *testing.T) {
	st := sampleState(t)
	env := Env{RuleDir: t.TempDir(), TProxyPort: 7893, DNSPort: 1053, ClashPort: 9090}
	b, _ := Build(st, env)
	if strings.Contains(string(b), "steamcontent.com") {
		t.Fatal("off: no download rule")
	}
	st.Settings.GameDownloadsDirect = true
	b, err := Build(st, env)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(b), "steamcontent.com") != 2 { // the route rule and the DNS rule
		t.Fatal("on: downloads must go direct and resolve with the provider's DNS")
	}
	if strings.Contains(string(b), "steampowered.com") {
		t.Fatal("the store itself (logins) must stay on the tunnel")
	}
}

func TestAutoAvoidsUnsteady(t *testing.T) {
	st := sampleState(t)
	env := Env{RuleDir: t.TempDir(), TProxyPort: 7893, DNSPort: 1053, ClashPort: 9090}
	members := func(b []byte) []any {
		var cfg struct{ Outbounds []map[string]any }
		_ = json.Unmarshal(b, &cfg)
		for _, o := range cfg.Outbounds {
			if o["tag"] == TagAuto {
				return o["outbounds"].([]any)
			}
		}
		return nil
	}
	b, _ := Build(st, env)
	all := len(members(b))
	env.Avoid = map[string]bool{st.Nodes[0].ID: true}
	b, _ = Build(st, env)
	got := members(b)
	if len(got) != all-1 {
		t.Fatalf("one unsteady server: %d of %d left", len(got), all)
	}
	for _, m := range got {
		if m == st.Nodes[0].ID {
			t.Fatal("the unsteady server is still in the automatic choice")
		}
	}
	if !strings.Contains(string(b), `"outbound": "`+st.Nodes[0].ID+`"`) && !strings.Contains(string(b), `"tag": "`+st.Nodes[0].ID+`"`) {
		t.Fatal("it must stay selectable by hand")
	}
	// All unsteady: the automatic choice keeps them all rather than none.
	env.Avoid = map[string]bool{}
	for _, n := range st.Nodes {
		env.Avoid[n.ID] = true
	}
	b, _ = Build(st, env)
	if len(members(b)) != all {
		t.Fatal("never leave the automatic choice empty")
	}
}
