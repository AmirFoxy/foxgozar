// Package state holds everything foxgozar persists: settings, nodes and
// subscriptions. One JSON file, written atomically, guarded by one mutex.
package state

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Node is one server. Outbound is a ready sing-box outbound (or endpoint,
// for WireGuard) without its tag; the tag is always the node ID.
type Node struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Server   string `json:"server"`
	Port     int    `json:"port"`
	Sub      string `json:"sub,omitempty"`
	Link     string `json:"link,omitempty"`
	Endpoint bool   `json:"endpoint,omitempty"`
	// "xray" when only Xray can speak this node (VLESS encryption, XHTTP);
	// Outbound is then an Xray outbound, not a sing-box one.
	Core     string         `json:"core,omitempty"`
	Outbound map[string]any `json:"outbound"`
	// Last measured delay in ms; 0 unknown, -1 failed. Used to order the
	// urltest group so the first pick after a start is already a good one.
	Delay    int   `json:"delay,omitempty"`
	TestedAt int64 `json:"tested_at,omitempty"`
}

type Subscription struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	UserAgent string `json:"user_agent,omitempty"`
	Interval  int    `json:"interval"` // hours, 0 = manual only
	Enabled   bool   `json:"enabled"`
	UpdatedAt int64  `json:"updated_at,omitempty"`
	LastError string `json:"last_error,omitempty"`
	NodeCount int    `json:"node_count"`
	Upload    int64  `json:"upload,omitempty"`
	Download  int64  `json:"download,omitempty"`
	Total     int64  `json:"total,omitempty"`
	Expire    int64  `json:"expire,omitempty"`
}

// ShuntRule sends matching traffic somewhere other than the default.
type ShuntRule struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Enabled bool     `json:"enabled"`
	Domains []string `json:"domains,omitempty"` // suffix match
	IPs     []string `json:"ips,omitempty"`     // CIDR
	RuleSet []string `json:"rule_set,omitempty"`
	Target  string   `json:"target"` // "direct", "block", "proxy" or a node ID
}

type Device struct {
	MAC  string `json:"mac"`
	Name string `json:"name,omitempty"`
}

type Settings struct {
	Enabled bool `json:"enabled"`

	// "auto": fastest working node, switched by the core itself.
	// "manual": Selected is used.
	Mode     string `json:"mode"`
	Selected string `json:"selected,omitempty"`

	TestURL           string `json:"test_url"`
	TestInterval      int    `json:"test_interval"` // seconds
	Tolerance         int    `json:"tolerance"`     // ms
	InterruptOnSwitch bool   `json:"interrupt_on_switch"`

	TProxyPort int `json:"tproxy_port"`
	DNSPort    int `json:"dns_port"`
	MixedPort  int `json:"mixed_port"` // 0 = off
	ClashPort  int `json:"clash_port"`
	APIPort    int `json:"api_port"`

	LANInterfaces []string `json:"lan_interfaces"`
	ProxyRouter   bool     `json:"proxy_router"` // the router's own traffic
	IPv6          bool     `json:"ipv6"`

	IranDirect bool `json:"iran_direct"`
	BlockAds   bool `json:"block_ads"`
	QUICBlock  bool `json:"quic_block"` // reject UDP/443 so browsers fall back to TCP

	RemoteDNS string `json:"remote_dns"`
	DirectDNS string `json:"direct_dns"`
	FakeIP    bool   `json:"fake_ip"`

	// Access control: "all", "only" (listed devices only) or "except".
	ACLMode string   `json:"acl_mode"`
	Devices []Device `json:"devices,omitempty"`

	KillSwitch bool `json:"kill_switch"`

	// Keep the core running while disconnected, so Connect is one nft call.
	KeepWarm bool `json:"keep_warm"`

	Mux          bool   `json:"mux"`
	TLSFragment  bool   `json:"tls_fragment"`
	LogLevel     string `json:"log_level"`
	GitHubMirror string `json:"github_mirror,omitempty"`
	CoreChannel  string `json:"core_channel"` // "stable" or "beta"
	Language     string `json:"language"`
}

type State struct {
	Version  int            `json:"version"`
	Token    string         `json:"token"`
	Settings Settings       `json:"settings"`
	Nodes    []Node         `json:"nodes"`
	Subs     []Subscription `json:"subscriptions"`
	Rules    []ShuntRule    `json:"rules"`
}

func Defaults() Settings {
	return Settings{
		Mode:          "auto",
		TestURL:       "https://cp.cloudflare.com/generate_204",
		TestInterval:  120,
		Tolerance:     80,
		TProxyPort:    7893,
		DNSPort:       1053,
		MixedPort:     7890,
		ClashPort:     9090,
		APIPort:       9095,
		LANInterfaces: []string{"br-lan"},
		ProxyRouter:   true,
		IranDirect:    true,
		RemoteDNS:     "https://1.1.1.1/dns-query",
		DirectDNS:     "udp://8.8.8.8",
		ACLMode:       "all",
		KeepWarm:      true,
		LogLevel:      "warn",
		CoreChannel:   "stable",
		Language:      "fa",
	}
}

// Store is the single owner of State. Read with View, change with Update.
type Store struct {
	path string
	mu   sync.RWMutex
	s    State
	subs []chan struct{}
}

func Open(path string) (*Store, error) {
	st := &Store{path: path}
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		st.s = State{Version: 1, Settings: Defaults()}
	case err != nil:
		return nil, err
	default:
		st.s.Settings = Defaults()
		if err := json.Unmarshal(b, &st.s); err != nil {
			// Never lose a user's nodes to one bad write: keep the broken
			// file aside and start clean.
			_ = os.Rename(path, path+".broken-"+time.Now().Format("20060102-150405"))
			st.s = State{Version: 1, Settings: Defaults()}
		}
	}
	if st.s.Token == "" {
		st.s.Token = RandomHex(16)
	}
	if st.s.Nodes == nil {
		st.s.Nodes = []Node{}
	}
	if st.s.Subs == nil {
		st.s.Subs = []Subscription{}
	}
	if st.s.Rules == nil {
		st.s.Rules = []ShuntRule{}
	}
	return st, st.save()
}

// View gives a deep copy, safe to keep and modify.
func (st *Store) View() State {
	st.mu.RLock()
	defer st.mu.RUnlock()
	var c State
	b, _ := json.Marshal(st.s)
	_ = json.Unmarshal(b, &c)
	return c
}

// Update applies fn and persists. If fn returns an error nothing changes.
func (st *Store) Update(fn func(s *State) error) error {
	st.mu.Lock()
	b, _ := json.Marshal(st.s)
	var work State
	_ = json.Unmarshal(b, &work)
	if err := fn(&work); err != nil {
		st.mu.Unlock()
		return err
	}
	st.s = work
	err := st.save()
	subs := append([]chan struct{}(nil), st.subs...)
	st.mu.Unlock()
	for _, c := range subs {
		select {
		case c <- struct{}{}:
		default:
		}
	}
	return err
}

// Changed returns a channel that receives after every Update.
func (st *Store) Changed() <-chan struct{} {
	c := make(chan struct{}, 1)
	st.mu.Lock()
	st.subs = append(st.subs, c)
	st.mu.Unlock()
	return c
}

func (st *Store) save() error {
	b, err := json.MarshalIndent(st.s, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(st.path, b, 0o600)
}

func WriteFileAtomic(path string, b []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func RandomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *State) NodeByID(id string) *Node {
	for i := range s.Nodes {
		if s.Nodes[i].ID == id {
			return &s.Nodes[i]
		}
	}
	return nil
}

func (s *State) SubByID(id string) *Subscription {
	for i := range s.Subs {
		if s.Subs[i].ID == id {
			return &s.Subs[i]
		}
	}
	return nil
}
