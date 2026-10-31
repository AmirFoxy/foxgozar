package gen

import (
	"encoding/json"
	"fmt"
	"strings"

	"foxgozar/internal/state"
)

// TagPsiphon is the outbound the chosen domains take while Psiphon is up.
const TagPsiphon = "psiphon"

// PsiphonRegions are the exit countries Psiphon offers ("" lets it pick).
// An allowlist, so nothing else can reach the config.
var PsiphonRegions = []string{"", "US", "CA", "GB", "DE", "NL", "FR", "AT", "BE", "BG", "CH", "CZ", "DK", "EE",
	"ES", "FI", "IE", "IT", "LT", "NO", "PL", "RO", "RS", "SE", "IN", "ID", "JP", "SG", "AU"}

func ValidPsiphonRegion(r string) bool {
	for _, x := range PsiphonRegions {
		if x == r {
			return true
		}
	}
	return false
}

// BuildPsiphon is the config for Psiphon's tunnel core: the user's own
// config merged with FoxGozar's fields. Defaults apply only where the
// user's config is silent; the protected fields are set last and always
// win, so nothing pasted can move the proxies off loopback or change the
// country. upstream, when set, is a proxy Psiphon connects through.
func BuildPsiphon(p state.Psiphon, socksPort, httpPort int, dataDir, upstream string) ([]byte, error) {
	cfg := map[string]any{}
	if strings.TrimSpace(p.Config) == "" {
		return nil, fmt.Errorf("paste your Psiphon config first")
	}
	if err := json.Unmarshal([]byte(p.Config), &cfg); err != nil {
		return nil, fmt.Errorf("the Psiphon config must be a JSON object: %w", err)
	}
	for _, k := range []string{"PropagationChannelId", "SponsorId"} {
		if s, _ := cfg[k].(string); s == "" {
			return nil, fmt.Errorf("the Psiphon config has no %s", k)
		}
	}
	if !ValidPsiphonRegion(p.Region) {
		return nil, fmt.Errorf("unknown country %q", p.Region)
	}
	def := func(k string, v any) {
		if _, ok := cfg[k]; !ok {
			cfg[k] = v
		}
	}
	def("EmitDiagnosticNotices", true) // warnings say why it does not connect; FoxGozar filters them
	def("UseIndistinguishableTLS", true)
	def("EstablishTunnelTimeoutSeconds", 0) // keep trying, never give up
	if upstream != "" {
		cfg["UpstreamProxyUrl"] = upstream
	}
	cfg["EgressRegion"] = p.Region
	cfg["ListenInterface"] = "lo"
	cfg["LocalSocksProxyPort"] = socksPort
	cfg["DisableLocalSocksProxy"] = false
	cfg["LocalHttpProxyPort"] = httpPort // for the exit check only
	cfg["DisableLocalHTTPProxy"] = false
	cfg["DataRootDirectory"] = dataDir
	return json.MarshalIndent(cfg, "", "  ")
}

// PsiphonNotice is what FoxGozar reads from Psiphon's notice lines.
type PsiphonNotice struct {
	Type    string   // the noticeType
	Count   int      // Tunnels: how many are established; CandidateServers: how many to try
	Region  string   // ConnectedServerRegion: the exit country
	Regions []string // AvailableEgressRegions: the countries Psiphon can exit from
	Message string   // Alert, Warning, Error: what it says
}

// ParsePsiphonNotice reads one output line of Psiphon's tunnel core. Its
// notices are JSON objects; anything else is not one.
func ParsePsiphonNotice(line string) (PsiphonNotice, bool) {
	i := strings.IndexByte(line, '{')
	if i < 0 {
		return PsiphonNotice{}, false
	}
	var n struct {
		NoticeType string `json:"noticeType"`
		Data       struct {
			Count   int      `json:"count"`
			Region  string   `json:"serverRegion"`
			Regions []string `json:"regions"`
			Message string   `json:"message"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(line[i:]), &n) != nil || n.NoticeType == "" {
		return PsiphonNotice{}, false
	}
	// From a process's output: country codes are never trusted to be
	// anything but two capitals.
	code := func(r string) bool { return len(r) == 2 && r[0] >= 'A' && r[0] <= 'Z' && r[1] >= 'A' && r[1] <= 'Z' }
	out := PsiphonNotice{Type: n.NoticeType, Count: n.Data.Count, Message: n.Data.Message}
	if code(n.Data.Region) {
		out.Region = n.Data.Region
	}
	for _, r := range n.Data.Regions {
		if code(r) {
			out.Regions = append(out.Regions, r)
		}
	}
	if len(out.Message) > 300 {
		out.Message = out.Message[:300]
	}
	return out, true
}
