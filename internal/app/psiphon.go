package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"foxgozar/internal/core"
	"foxgozar/internal/gen"
	"foxgozar/internal/state"
)

// Psiphon for chosen domains: one tunnel-core process with local proxies
// on loopback. The domains are routed to it only while it reports an
// established tunnel; before that, and whenever it drops, they keep using
// the normal tunnel, so nothing stops loading because of Psiphon.

func psiphonPorts(s state.Settings) (socks, http int) { return s.ClashPort + 110, s.ClashPort + 111 }

func (a *App) PsiphonBin() string { return filepath.Join(a.Paths.Bin, "psiphon") }

type psiphonState struct {
	mu         sync.Mutex
	installing bool
	connected  bool
	region     string // the exit country Psiphon reports
	since      time.Time
	lastErr    string
	exitIP     string
	candidates int      // servers Psiphon has to try, from its notices
	regions    []string // exit countries it can reach
	lastWarn   time.Time
}

func (a *App) psiphonProcess() *core.Process {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.psiphon == nil {
		cfg := filepath.Join(a.Paths.Run, "psiphon.json")
		a.psiphon = &core.Process{
			Name: "psiphon", Bin: a.PsiphonBin(), Log: a.Log,
			Args:   []string{"-config", cfg},
			OnLine: a.psiphonLine,
			OnExit: func(error) { a.setPsiphonConnected(false, "") },
		}
	}
	return a.psiphon
}

// psiphonLine reads Psiphon's notices: whether a tunnel is up and where it
// exits. Notices stay out of the log; the changes they mean are logged.
func (a *App) psiphonLine(line string) bool {
	n, ok := gen.ParsePsiphonNotice(line)
	if !ok {
		return true
	}
	switch n.Type {
	case "Tunnels":
		a.setPsiphonConnected(n.Count > 0, "")
	case "ConnectedServerRegion":
		a.setPsiphonConnected(true, n.Region)
	case "CandidateServers":
		// How many servers it has to try for the chosen country: 0 means
		// it has nothing to connect to. Logged when the number changes.
		a.psi.mu.Lock()
		changed := a.psi.candidates != n.Count
		a.psi.candidates = n.Count
		a.psi.mu.Unlock()
		if changed {
			a.Log.Add("foxgozar", fmt.Sprintf("Psiphon: %d servers to try for this country", n.Count))
		}
	case "AvailableEgressRegions":
		if len(n.Regions) == 0 {
			a.Log.Add("foxgozar", "Psiphon: no exit countries known yet (no server list)")
		} else {
			a.Log.Add("foxgozar", "Psiphon: exit countries available: "+strings.Join(n.Regions, " "))
		}
		a.psi.mu.Lock()
		a.psi.regions = n.Regions
		a.psi.mu.Unlock()
	case "Alert", "Warning", "Error":
		// Failed attempts repeat every few seconds: one line per 30 s.
		a.psi.mu.Lock()
		quiet := time.Since(a.psi.lastWarn) < 30*time.Second
		if !quiet {
			a.psi.lastWarn = time.Now()
		}
		a.psi.mu.Unlock()
		if n.Message != "" && !quiet {
			a.Log.Add("foxgozar", "Psiphon "+strings.ToLower(n.Type)+": "+n.Message)
		}
	}
	return false
}

func (a *App) setPsiphonConnected(on bool, region string) {
	p := a.psi
	p.mu.Lock()
	changed := p.connected != on
	p.connected = on
	if region != "" {
		p.region = region
	}
	if !on {
		p.region, p.exitIP = "", ""
	}
	if changed {
		p.since = time.Now()
	}
	reg := p.region
	p.mu.Unlock()
	if !changed {
		return
	}
	if on {
		a.Log.Add("foxgozar", "Psiphon connected "+reg+"; the chosen domains now go through it")
	} else {
		a.Log.Add("foxgozar", "Psiphon not connected; the chosen domains use the tunnel")
	}
	// The core config names Psiphon only while it is up.
	go func() {
		if err := a.Apply(context.Background()); err != nil {
			a.Log.Add("foxgozar", "Psiphon: "+err.Error())
		}
	}()
}

func (a *App) psiphonPort() int {
	a.psi.mu.Lock()
	defer a.psi.mu.Unlock()
	if !a.psi.connected {
		return 0
	}
	s, _ := psiphonPorts(a.Store.View().Settings)
	return s
}

// syncPsiphon starts, restarts or stops Psiphon to match the settings,
// downloading it first when needed.
func (a *App) syncPsiphon(ctx context.Context) error {
	st := a.Store.View()
	proc := a.psiphonProcess()
	p := st.Psiphon
	if p == nil || !p.Enabled {
		if proc.Running() {
			proc.Stop()
			a.Log.Add("foxgozar", "Psiphon stopped")
		}
		a.setPsiphonConnected(false, "")
		return nil
	}
	if _, err := os.Stat(a.PsiphonBin()); err != nil {
		if err := a.installPsiphon(ctx); err != nil {
			return err
		}
	}
	socks, http := psiphonPorts(st.Settings)
	upstream := ""
	if p.ViaTunnel && st.Settings.MixedPort > 0 {
		upstream = fmt.Sprintf("socks5://127.0.0.1:%d", st.Settings.MixedPort)
	}
	b, err := gen.BuildPsiphon(*p, socks, http, filepath.Join(a.Paths.Run, "psiphon-data"), upstream)
	if err != nil {
		return err
	}
	path := filepath.Join(a.Paths.Run, "psiphon.json")
	old, _ := os.ReadFile(path)
	if bytes.Equal(old, b) && proc.Running() {
		return nil
	}
	if err := os.MkdirAll(filepath.Join(a.Paths.Run, "psiphon-data"), 0o700); err != nil {
		return err
	}
	if err := state.WriteFileAtomic(path, b, 0o600); err != nil {
		return err
	}
	if proc.Running() {
		proc.Stop()
		a.setPsiphonConnected(false, "")
	}
	a.Log.Add("foxgozar", "Psiphon starting, exit "+orAny(p.Region))
	return proc.Start()
}

func orAny(r string) string {
	if r == "" {
		return "any country"
	}
	return r
}

func (a *App) installPsiphon(ctx context.Context) error {
	a.psi.mu.Lock()
	if a.psi.installing {
		a.psi.mu.Unlock()
		return errors.New("Psiphon is being downloaded")
	}
	a.psi.installing = true
	a.psi.mu.Unlock()
	defer func() { a.psi.mu.Lock(); a.psi.installing = false; a.psi.mu.Unlock() }()
	a.Log.Add("foxgozar", "downloading Psiphon")
	if err := a.Fetcher().InstallPsiphon(ctx, a.Paths.Bin); err != nil {
		return fmt.Errorf("Psiphon download: %w", err)
	}
	a.Log.Add("foxgozar", "Psiphon installed")
	return nil
}

// PsiphonPatch changes the settings; nil fields stay as they are.
type PsiphonPatch struct {
	Enabled   *bool     `json:"enabled"`
	Region    *string   `json:"region"`
	Config    *string   `json:"config"` // "" removes it
	Domains   *[]string `json:"domains"`
	ViaTunnel *bool     `json:"via_tunnel"`
}

// SetPsiphon saves the settings and applies them in the background (the
// first start downloads Psiphon, which takes a while).
func (a *App) SetPsiphon(p PsiphonPatch) error {
	err := a.Store.Update(func(s *state.State) error {
		if s.Psiphon == nil {
			s.Psiphon = &state.Psiphon{Region: "US", Domains: []string{"cdninstagram.com", "fbcdn.net"}}
		}
		x := s.Psiphon
		if p.Region != nil {
			if !gen.ValidPsiphonRegion(*p.Region) {
				return fmt.Errorf("unknown country %q", *p.Region)
			}
			x.Region = *p.Region
		}
		if p.Config != nil {
			c := strings.TrimSpace(*p.Config)
			if c != "" {
				var m map[string]any
				if json.Unmarshal([]byte(c), &m) != nil {
					return errors.New("the Psiphon config must be a JSON object, as Psiphon gives it")
				}
				for _, k := range []string{"PropagationChannelId", "SponsorId"} {
					if v, _ := m[k].(string); v == "" {
						return fmt.Errorf("the Psiphon config has no %s", k)
					}
				}
			}
			x.Config = c
		}
		if p.Domains != nil {
			var ds []string
			for _, d := range *p.Domains {
				d = strings.ToLower(strings.Trim(strings.TrimSpace(d), "."))
				if d != "" {
					ds = append(ds, d)
				}
			}
			x.Domains = ds
		}
		if p.ViaTunnel != nil {
			x.ViaTunnel = *p.ViaTunnel
		}
		if p.Enabled != nil {
			x.Enabled = *p.Enabled
		}
		if x.Enabled && x.Config == "" {
			return errors.New("paste your Psiphon config before turning it on")
		}
		return nil
	})
	if err != nil {
		return err
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := a.syncPsiphon(ctx); err != nil {
			a.psi.mu.Lock()
			a.psi.lastErr = err.Error()
			a.psi.mu.Unlock()
			a.Log.Add("foxgozar", "Psiphon: "+err.Error())
			return
		}
		a.psi.mu.Lock()
		a.psi.lastErr = ""
		a.psi.mu.Unlock()
		// The domain list may have changed while Psiphon stayed up.
		_ = a.Apply(context.Background())
	}()
	return nil
}

// PsiphonStatus is what the UI shows. The pasted config itself is never
// sent back: only whether there is one.
type PsiphonStatus struct {
	Enabled    bool     `json:"enabled"`
	Region     string   `json:"region"`
	Domains    []string `json:"domains"`
	ViaTunnel  bool     `json:"via_tunnel"`
	HasConfig  bool     `json:"has_config"`
	Supported  bool     `json:"supported"` // a build exists for this router
	Installed  bool     `json:"installed"`
	Installing bool     `json:"installing"`
	Running    bool     `json:"running"`
	Connected  bool     `json:"connected"`
	ExitRegion string   `json:"exit_region,omitempty"`
	ExitIP     string   `json:"exit_ip,omitempty"`
	Since      int64    `json:"since,omitempty"`
	Error      string   `json:"error,omitempty"`
	Regions    []string `json:"regions"`
}

func (a *App) PsiphonStatus() PsiphonStatus {
	st := a.Store.View()
	s := PsiphonStatus{Region: "US", Domains: []string{"cdninstagram.com", "fbcdn.net"}, Regions: gen.PsiphonRegions,
		Supported: core.PsiphonArch() != ""}
	if p := st.Psiphon; p != nil {
		s.Enabled, s.Region, s.Domains, s.ViaTunnel, s.HasConfig = p.Enabled, p.Region, p.Domains, p.ViaTunnel, p.Config != ""
	}
	_, err := os.Stat(a.PsiphonBin())
	s.Installed = err == nil
	s.Running = a.psiphonProcess().Running()
	a.psi.mu.Lock()
	s.Installing, s.Connected, s.ExitRegion, s.ExitIP, s.Error = a.psi.installing, a.psi.connected, a.psi.region, a.psi.exitIP, a.psi.lastErr
	if a.psi.connected {
		s.Since = a.psi.since.Unix()
	}
	a.psi.mu.Unlock()
	return s
}

// PsiphonExitIP asks a public service, through Psiphon, which address
// sites see: the check regionhop runs too (api.ipify.org, then
// checkip.amazonaws.com).
func (a *App) PsiphonExitIP(ctx context.Context) (string, error) {
	a.psi.mu.Lock()
	up := a.psi.connected
	a.psi.mu.Unlock()
	if !up {
		return "", errors.New("Psiphon is not connected yet")
	}
	_, port := psiphonPorts(a.Store.View().Settings)
	c := core.HTTPClient(port, 20*time.Second)
	var last error
	for _, u := range []string{"https://api.ipify.org", "https://checkip.amazonaws.com"} {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		resp, err := c.Do(req)
		if err != nil {
			last = err
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
		resp.Body.Close()
		ip := strings.TrimSpace(string(b))
		if resp.StatusCode == 200 && ip != "" {
			a.psi.mu.Lock()
			a.psi.exitIP = ip
			a.psi.mu.Unlock()
			return ip, nil
		}
		last = fmt.Errorf("%s: HTTP %d", u, resp.StatusCode)
	}
	return "", last
}
