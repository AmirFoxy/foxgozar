package gen

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"foxgozar/internal/link"
	"foxgozar/internal/state"
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
	b, err := Build(st, Env{RuleDir: dir, CacheFile: "cache.db", Secret: "s", TProxyPort: 7893, DNSPort: 1053, ClashPort: 9090})
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
	b2, _ := Build(st, Env{RuleDir: dir, CacheFile: "cache.db", Secret: "s", TProxyPort: 7893, DNSPort: 1053, ClashPort: 9090})
	if string(b) != string(b2) {
		t.Error("selection leaked into the config")
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
	sb, err := Build(st, Env{RuleDir: t.TempDir(), TProxyPort: 7893, DNSPort: 1053, ClashPort: 9090})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sb), `"server_port": 21001`) {
		t.Error("second Xray node should be a SOCKS outbound on 21001")
	}
}
