// Package state holds everything foxgozar persists: settings, nodes and
// subscriptions. One JSON file, written atomically, guarded by one mutex.
package state

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"foxgozar/internal/warp"
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
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Enabled  bool     `json:"enabled"`
	Domains  []string `json:"domains,omitempty"`  // suffix match
	Keywords []string `json:"keywords,omitempty"` // the domain contains it
	IPs      []string `json:"ips,omitempty"`      // CIDR
	RuleSet  []string `json:"rule_set,omitempty"`
	// Links to rule-sets or plain lists, downloaded and kept up to date.
	URLs   []string `json:"urls,omitempty"`
	Target string   `json:"target"` // "direct", "block", "proxy" or a node ID
}

// Device is a LAN device with its own policy, found by MAC address.
type Device struct {
	MAC  string `json:"mac"`            // lower case, colon separated
	Name string `json:"name,omitempty"` // set by the user; the DHCP hostname otherwise
	// "proxy" (through the tunnel), "direct" (never enters the core),
	// "node" (through Node only) or "block" (no internet).
	Policy string `json:"policy"`
	Node   string `json:"node,omitempty"`
	// Hours with no internet, every day, whatever the policy.
	Schedule *Schedule `json:"schedule,omitempty"`
}

// Schedule is a daily window, "HH:MM" to "HH:MM" in the user's time; it may
// cross midnight ("22:00" to "07:00"). The router's clock is usually UTC, so
// the user's offset from UTC (minutes, from the browser when it was saved)
// is kept with it.
type Schedule struct {
	From string `json:"from"`
	To   string `json:"to"`
	TZ   int    `json:"tz"`
}

func clockMinutes(s string) (int, bool) {
	var h, m int
	if n, err := fmt.Sscanf(s, "%d:%d", &h, &m); n != 2 || err != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// Valid reports whether both times parse and differ.
func (sc Schedule) Valid() bool {
	f, ok1 := clockMinutes(sc.From)
	t, ok2 := clockMinutes(sc.To)
	return ok1 && ok2 && f != t && sc.TZ >= -14*60 && sc.TZ <= 14*60
}

// Active reports whether now falls inside the window.
func (sc Schedule) Active(now time.Time) bool {
	if !sc.Valid() {
		return false
	}
	f, _ := clockMinutes(sc.From)
	t, _ := clockMinutes(sc.To)
	local := now.UTC().Add(time.Duration(sc.TZ) * time.Minute)
	m := local.Hour()*60 + local.Minute()
	if f < t {
		return m >= f && m < t
	}
	return m >= f || m < t // across midnight
}

// Blocked reports whether the device has no internet at this moment.
func (d Device) Blocked(now time.Time) bool {
	return d.Policy == "block" || (d.Schedule != nil && d.Schedule.Active(now))
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
	// Known malware, phishing and crypto-mining destinations.
	BlockMalware bool `json:"block_malware"`
	QUICBlock    bool `json:"quic_block"` // reject UDP/443 so browsers fall back to TCP

	RemoteDNS string `json:"remote_dns"`
	DirectDNS string `json:"direct_dns"`
	FakeIP    bool   `json:"fake_ip"`

	// Policy for devices without one of their own: "proxy" or "direct".
	DefaultPolicy string `json:"default_policy"`

	KillSwitch bool `json:"kill_switch"`

	// When every server fails, carry traffic over Cloudflare WARP instead.
	WarpBackup bool `json:"warp_backup"`

	// Keep the core running while disconnected, so Connect is one nft call.
	KeepWarm bool `json:"keep_warm"`

	// When every server fails, try TLS fragment and multiplex on and off by
	// itself, before WARP.
	AutoRescue bool `json:"auto_rescue"`

	// Anti-lag: OpenWrt's SQM with cake on the internet port, so downloads
	// do not make games and calls wait. Needs the line's real speeds.
	// Game downloads (Steam, Epic, Battle.net, Riot) go direct: they are
	// huge, and spare the server's traffic and speed.
	GameDownloadsDirect bool `json:"game_downloads_direct"`
	// After a match, a game whose own servers answer directly and faster
	// than the tunnel is switched to direct for the next one.
	AutoDirectGames bool `json:"auto_direct_games"`

	AntiLag  bool `json:"anti_lag"`
	DownMbps int  `json:"down_mbps,omitempty"`
	UpMbps   int  `json:"up_mbps,omitempty"`

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
	Devices  []Device       `json:"devices"`
	Warp     *Warp          `json:"warp,omitempty"`
	Share    *Share         `json:"share,omitempty"`
	// A path of its own per game: "direct" or a server id. Absent = the
	// tunnel, like everything else.
	GameRoutes map[string]string `json:"game_routes,omitempty"`
	// Games the user hid from the Games tab, and games the user added.
	// Traffic per server per month ("2006-01" -> node -> up, down), the
	// last twelve months.
	Usage       map[string]map[string][2]int64 `json:"usage,omitempty"`
	HiddenGames []string                       `json:"hidden_games,omitempty"`
	// Steam app id -> its header picture's address, as Steam's own API
	// gives it (newer games keep pictures under hashed paths).
	ArtCache    map[string]string `json:"art_cache,omitempty"`
	CustomGames []CustomGame      `json:"custom_games,omitempty"`
}

// CustomGame is a game the user added by name; FoxGozar found the rest
// (its Steam id, so its art) in Steam's store when it could.
type CustomGame struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Steam int    `json:"steam,omitempty"`
}

// Share is a server on the router that people elsewhere connect to, so
// they use this router's internet: for a shutdown, when the router has a
// second way out (Starlink) and others reach it over the domestic network.
// VLESS with Reality: no certificate to get, and it looks like a visit to
// SNI. The server key is shared; each person has an account of their own
// (a UUID), so they can be told apart, counted, paused or removed.
type Share struct {
	Enabled    bool        `json:"enabled"`
	Port       int         `json:"port"`
	SNI        string      `json:"sni"`
	PrivateKey string      `json:"private_key"`
	PublicKey  string      `json:"public_key"`
	ShortID    string      `json:"short_id"`
	Users      []ShareUser `json:"users"`
	// Single account of 6.15.0, before accounts; moved into Users.
	UUID string `json:"uuid,omitempty"`
}

// ShareUser is one person's account on the sharing server.
type ShareUser struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	UUID    string `json:"uuid"`
	Enabled bool   `json:"enabled"`
	Created int64  `json:"created"`
	// Traffic through the router, saved now and then; and when last seen.
	Up       int64 `json:"up"`
	Down     int64 `json:"down"`
	LastSeen int64 `json:"last_seen,omitempty"`
	// Optional data limit in bytes: the account pauses itself at it. 0 = none.
	Limit int64 `json:"limit,omitempty"`
}

func (sh *Share) UserByID(id string) *ShareUser {
	for i := range sh.Users {
		if sh.Users[i].ID == id {
			return &sh.Users[i]
		}
	}
	return nil
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
		DirectDNS:     "isp", // the provider’s DNS: Iranian sites get Iranian addresses
		DefaultPolicy: "proxy",
		KeepWarm:      true,
		AutoRescue:    true,
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

// CurrentVersion is the state file format; migrate brings older files up.
const CurrentVersion = 2

func migrate(s *State) {
	if s.Version < 2 {
		// Version 2: the direct DNS default became the provider's own. A
		// file still on the old default never chose 8.8.8.8 itself.
		if s.Settings.DirectDNS == "udp://8.8.8.8" {
			s.Settings.DirectDNS = "isp"
		}
	}
	if sh := s.Share; sh != nil && sh.UUID != "" {
		// 6.15.0 had one account for everyone: it becomes the first one.
		sh.Users = append(sh.Users, ShareUser{ID: "u" + RandomHex(4), Name: "1", UUID: sh.UUID, Enabled: true, Created: time.Now().Unix()})
		sh.UUID = ""
	}
	s.Version = CurrentVersion
}

func Open(path string) (*Store, error) {
	st := &Store{path: path}
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		st.s = State{Version: CurrentVersion, Settings: Defaults()}
	case err != nil:
		return nil, err
	default:
		st.s.Settings = Defaults()
		if err := json.Unmarshal(b, &st.s); err != nil {
			// Never lose a user's nodes to one bad write: keep the broken
			// file aside and start clean.
			_ = os.Rename(path, path+".broken-"+time.Now().Format("20060102-150405"))
			st.s = State{Version: CurrentVersion, Settings: Defaults()}
		}
	}
	migrate(&st.s)
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
	if st.s.Devices == nil {
		st.s.Devices = []Device{}
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

func (s *State) DeviceByMAC(mac string) *Device {
	for i := range s.Devices {
		if s.Devices[i].MAC == mac {
			return &s.Devices[i]
		}
	}
	return nil
}

// Warp is the WARP account FoxGozar registered, and the endpoints it tries.
type Warp struct {
	Account   warp.Account `json:"account"`
	Endpoints []string     `json:"endpoints"`
}
