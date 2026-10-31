package gen

import (
	"encoding/json"
	"strings"
	"testing"

	"foxgozar/internal/state"
)

func TestBuildPsiphon(t *testing.T) {
	p := state.Psiphon{Enabled: true, Region: "US",
		// What a pasted config might try: other ports, all interfaces, another country.
		Config: `{"PropagationChannelId":"X","SponsorId":"Y","LocalSocksProxyPort":1080,"ListenInterface":"any","EgressRegion":"DE","EmitDiagnosticNotices":false}`}
	b, err := BuildPsiphon(p, 18100, 18101, "/tmp/psi", "socks5://127.0.0.1:7890")
	if err != nil {
		t.Fatal(err)
	}
	var c map[string]any
	_ = json.Unmarshal(b, &c)
	want := map[string]any{"EgressRegion": "US", "ListenInterface": "lo", "LocalSocksProxyPort": 18100.0, "LocalHttpProxyPort": 18101.0,
		"DataRootDirectory": "/tmp/psi", "UpstreamProxyUrl": "socks5://127.0.0.1:7890", "PropagationChannelId": "X",
		"EmitDiagnosticNotices": false, "EstablishTunnelTimeoutSeconds": 0.0}
	for k, v := range want {
		if c[k] != v {
			t.Errorf("%s = %v, want %v", k, c[k], v)
		}
	}
	for _, bad := range []state.Psiphon{
		{Region: "US"},                              // no config
		{Region: "US", Config: `[1]`},               // not an object
		{Region: "US", Config: `{"SponsorId":"Y"}`}, // no channel id
		{Region: "XX", Config: `{"PropagationChannelId":"X","SponsorId":"Y"}`},
	} {
		if _, err := BuildPsiphon(bad, 1, 2, "/d", ""); err == nil {
			t.Errorf("%+v must be refused", bad)
		}
	}
}

func TestParsePsiphonNotice(t *testing.T) {
	n, ok := ParsePsiphonNotice(`{"data":{"count":1},"noticeType":"Tunnels","timestamp":"2026-10-31T10:00:00.000Z"}`)
	if !ok || n.Type != "Tunnels" || n.Count != 1 {
		t.Fatalf("%+v %v", n, ok)
	}
	n, _ = ParsePsiphonNotice(`{"data":{"serverRegion":"US"},"noticeType":"ConnectedServerRegion"}`)
	if n.Region != "US" {
		t.Fatalf("%+v", n)
	}
	if n, _ := ParsePsiphonNotice(`{"data":{"serverRegion":"<script>"},"noticeType":"ConnectedServerRegion"}`); n.Region != "" {
		t.Fatal("a region that is not two capitals must be dropped")
	}
	if _, ok := ParsePsiphonNotice("plain text"); ok {
		t.Fatal("not a notice")
	}
}

func TestPsiphonRoute(t *testing.T) {
	st := sampleState(t)
	st.Psiphon = &state.Psiphon{Enabled: true, Region: "US", Domains: []string{"cdninstagram.com", "fbcdn.net"}}
	env := Env{RuleDir: t.TempDir(), TProxyPort: 7893, DNSPort: 1053, ClashPort: 9090}
	b, _ := Build(st, env)
	if strings.Contains(string(b), TagPsiphon) {
		t.Fatal("Psiphon not connected: the domains must stay on the tunnel")
	}
	env.PsiphonPort = 18100
	b, err := Build(st, env)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `"server_port": 18100`) || !strings.Contains(s, `"outbound": "psiphon"`) || !strings.Contains(s, "cdninstagram.com") {
		t.Fatalf("connected: the domains must go through Psiphon")
	}
	st.Psiphon.Enabled = false
	if b, _ := Build(st, env); strings.Contains(string(b), TagPsiphon) {
		t.Fatal("switched off: no Psiphon")
	}
}

func TestParsePsiphonDiagnostics(t *testing.T) {
	n, _ := ParsePsiphonNotice(`{"data":{"regions":["US","DE","x1"]},"noticeType":"AvailableEgressRegions"}`)
	if len(n.Regions) != 2 || n.Regions[0] != "US" {
		t.Fatalf("%+v", n)
	}
	n, _ = ParsePsiphonNotice(`{"data":{"count":0,"region":"US"},"noticeType":"CandidateServers"}`)
	if n.Type != "CandidateServers" || n.Count != 0 {
		t.Fatalf("%+v", n)
	}
	n, _ = ParsePsiphonNotice(`{"data":{"message":"fetch remote server list failed"},"noticeType":"Warning"}`)
	if n.Message != "fetch remote server list failed" {
		t.Fatalf("%+v", n)
	}
}
