package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"foxgozar/internal/clash"
	"foxgozar/internal/core"
	"foxgozar/internal/gen"
	"foxgozar/internal/ruleset"
	"foxgozar/internal/state"
)

// The sharing server: its own sing-box, independent of the tunnel, so it
// keeps serving whether FoxGozar is connected or not and even with no
// servers at all (Starlink needs none).

const shareFWSection = "foxgozar_share"

func (a *App) shareConfigPath() string { return filepath.Join(a.Paths.Run, "share.json") }

func (a *App) shareProcess() *core.Process {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.share == nil {
		cfg := a.shareConfigPath()
		a.share = &core.Process{
			Name: "share", Bin: a.SingBoxBin(), Log: a.Log,
			Args:  []string{"run", "-c", cfg, "--disable-color"},
			Check: []string{"check", "-c", cfg},
		}
	}
	return a.share
}

// The sharing server's own Clash API, for counting each account's traffic.
func shareClashPort(s state.Settings) int { return s.ClashPort + 100 }

// SetShare saves the sharing settings, making the server key on first use
// (newKey makes a fresh one: every account's link changes), and brings the
// server and its firewall opening in line.
func (a *App) SetShare(ctx context.Context, enabled bool, port int, sni string, newKey bool) (state.Share, error) {
	st := a.Store.View()
	sh := state.Share{Port: 8443, SNI: "www.speedtest.net"}
	if st.Share != nil {
		sh = *st.Share
	}
	if port != 0 {
		sh.Port = port
	}
	if sni = strings.TrimSpace(sni); sni != "" {
		sh.SNI = sni
	}
	if sh.Port < 1 || sh.Port > 65535 {
		return sh, errors.New("port must be 1 to 65535")
	}
	for _, p := range []int{st.Settings.APIPort, st.Settings.MixedPort, shareClashPort(st.Settings), 22, 53, 80, 443} {
		if sh.Port == p && p != 0 {
			return sh, fmt.Errorf("port %d is already used on the router", p)
		}
	}
	if enabled && (sh.PrivateKey == "" || newKey) {
		if err := a.ensureSingBox(ctx); err != nil {
			return sh, err
		}
		priv, pub, err := realityKeys(a.SingBoxBin())
		if err != nil {
			return sh, err
		}
		sh.PrivateKey, sh.PublicKey, sh.ShortID = priv, pub, state.RandomHex(4)
	}
	sh.Enabled = enabled
	if err := a.Store.Update(func(s *state.State) error {
		c := sh
		if s.Share != nil {
			c.Users = s.Share.Users // accounts change only through their own calls
		}
		s.Share = &c
		return nil
	}); err != nil {
		return sh, err
	}
	return sh, a.syncShare(a.Store.View())
}

// AddShareUser makes an account for one person.
func (a *App) AddShareUser(name string, limit int64) (state.ShareUser, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return state.ShareUser{}, errors.New("give the account a name: who is it for?")
	}
	u := state.ShareUser{ID: "u" + state.RandomHex(4), Name: name, UUID: newUUID(), Enabled: true,
		Created: time.Now().Unix(), Limit: limit}
	err := a.Store.Update(func(s *state.State) error {
		if s.Share == nil {
			s.Share = &state.Share{Port: 8443, SNI: "www.speedtest.net"}
		}
		for _, x := range s.Share.Users {
			if strings.EqualFold(x.Name, name) {
				return fmt.Errorf("there is already an account named %q", name)
			}
		}
		s.Share.Users = append(s.Share.Users, u)
		return nil
	})
	if err != nil {
		return u, err
	}
	a.Log.Add("foxgozar", "sharing: account for "+name+" added")
	return u, a.syncShare(a.Store.View())
}

// ShareUserPatch changes an account; nil fields stay as they are.
type ShareUserPatch struct {
	Name       *string `json:"name"`
	Enabled    *bool   `json:"enabled"`
	Limit      *int64  `json:"limit"`
	ResetUsage bool    `json:"reset_usage"`
	NewLink    bool    `json:"new_link"` // a new UUID: the old link stops working
}

func (a *App) UpdateShareUser(id string, p ShareUserPatch) error {
	err := a.Store.Update(func(s *state.State) error {
		if s.Share == nil || s.Share.UserByID(id) == nil {
			return errors.New("no such account")
		}
		u := s.Share.UserByID(id)
		if p.Name != nil && strings.TrimSpace(*p.Name) != "" {
			u.Name = strings.TrimSpace(*p.Name)
		}
		if p.Enabled != nil {
			u.Enabled = *p.Enabled
		}
		if p.Limit != nil && *p.Limit >= 0 {
			u.Limit = *p.Limit
		}
		if p.ResetUsage {
			u.Up, u.Down = 0, 0
		}
		if p.NewLink {
			u.UUID = newUUID()
		}
		return nil
	})
	if err != nil {
		return err
	}
	return a.syncShare(a.Store.View())
}

func (a *App) DeleteShareUser(id string) error {
	var name string
	err := a.Store.Update(func(s *state.State) error {
		if s.Share == nil || s.Share.UserByID(id) == nil {
			return errors.New("no such account")
		}
		kept := s.Share.Users[:0]
		for _, u := range s.Share.Users {
			if u.ID == id {
				name = u.Name
				continue
			}
			kept = append(kept, u)
		}
		s.Share.Users = kept
		return nil
	})
	if err != nil {
		return err
	}
	a.Log.Add("foxgozar", "sharing: account for "+name+" removed")
	return a.syncShare(a.Store.View())
}

// shareUsage is traffic counted since the last save, per account, and
// what is open right now.
type shareUsage struct {
	mu      sync.Mutex
	conns   map[string][3]int64 // connection id -> up, down, generation
	pending map[string][2]int64 // account id -> up, down not saved yet
	open    map[string]int
	rate    map[string][2]float64
	gen     int64
	last    time.Time
}

// shareWatch counts each account's traffic while the server runs, saves it
// every minute, and pauses accounts that reach their limit.
func (a *App) shareWatch(ctx context.Context) {
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	n := 0
	for {
		select {
		case <-ctx.Done():
			a.saveShareUsage()
			return
		case <-tick.C:
		}
		if !a.shareProcess().Running() {
			continue
		}
		a.collectShare(ctx)
		if n++; n%20 == 0 {
			a.saveShareUsage()
		}
	}
}

func (a *App) collectShare(ctx context.Context) {
	st := a.Store.View()
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	cs, err := clash.New(shareClashPort(st.Settings), st.Token).Connections(c)
	cancel()
	if err != nil {
		return
	}
	u := a.shareUse
	u.mu.Lock()
	defer u.mu.Unlock()
	now := time.Now()
	secs := now.Sub(u.last).Seconds()
	u.last = now
	u.gen++
	open := map[string]int{}
	delta := map[string][2]int64{}
	for _, conn := range cs.Connections {
		id, _ := conn["id"].(string)
		var who string
		chains, _ := conn["chains"].([]any)
		for _, ch := range chains {
			if s, _ := ch.(string); strings.HasPrefix(s, "u-") {
				who = strings.TrimPrefix(s, "u-")
			}
		}
		if id == "" || who == "" {
			continue
		}
		up, _ := conn["upload"].(float64)
		down, _ := conn["download"].(float64)
		prev, seen := u.conns[id]
		du, dd := int64(up), int64(down)
		if seen {
			du, dd = du-prev[0], dd-prev[1]
		}
		if du < 0 || dd < 0 {
			du, dd = 0, 0
		}
		u.conns[id] = [3]int64{int64(up), int64(down), u.gen}
		p := u.pending[who]
		u.pending[who] = [2]int64{p[0] + du, p[1] + dd}
		d := delta[who]
		delta[who] = [2]int64{d[0] + du, d[1] + dd}
		open[who]++
	}
	for k, v := range u.conns {
		if v[2] != u.gen {
			delete(u.conns, k)
		}
	}
	u.open = open
	u.rate = map[string][2]float64{}
	if secs > 0 && secs < 30 {
		for who, d := range delta {
			u.rate[who] = [2]float64{float64(d[0]) / secs, float64(d[1]) / secs}
		}
	}
}

// saveShareUsage adds what was counted to the saved totals, and pauses
// accounts over their limit.
func (a *App) saveShareUsage() {
	u := a.shareUse
	u.mu.Lock()
	pending, open := u.pending, u.open
	u.pending = map[string][2]int64{}
	u.mu.Unlock()
	if len(pending) == 0 && len(open) == 0 {
		return
	}
	now := time.Now().Unix()
	var paused []string
	_ = a.Store.Update(func(s *state.State) error {
		if s.Share == nil {
			return nil
		}
		for i := range s.Share.Users {
			x := &s.Share.Users[i]
			p := pending[x.ID]
			x.Up += p[0]
			x.Down += p[1]
			if p[0]+p[1] > 0 || open[x.ID] > 0 {
				x.LastSeen = now
			}
			if x.Enabled && x.Limit > 0 && x.Up+x.Down >= x.Limit {
				x.Enabled = false
				paused = append(paused, x.Name)
			}
		}
		return nil
	})
	if len(paused) > 0 {
		a.Log.Add("foxgozar", "sharing: data limit reached, paused: "+strings.Join(paused, ", "))
		if err := a.syncShare(a.Store.View()); err != nil {
			a.Log.Add("foxgozar", "sharing: "+err.Error())
		}
	}
}

func (a *App) ensureSingBox(ctx context.Context) error {
	if _, err := os.Stat(a.SingBoxBin()); err == nil {
		return nil
	}
	return a.installCore(ctx, core.SingBox)
}

// syncShare starts, restarts or stops the sharing server to match st.
func (a *App) syncShare(st state.State) error {
	p := a.shareProcess()
	sh := st.Share
	active := 0
	if sh != nil {
		for _, u := range sh.Users {
			if u.Enabled {
				active++
			}
		}
	}
	if sh == nil || !sh.Enabled || active == 0 || sh.PrivateKey == "" {
		// Off, or nobody allowed in yet: nothing listens, the port is closed.
		if p.Running() {
			p.Stop()
			a.Log.Add("foxgozar", "sharing stopped")
		}
		return openSharePort(0)
	}
	if _, err := os.Stat(a.SingBoxBin()); err != nil {
		return errors.New("sing-box is not installed yet")
	}
	b, err := gen.BuildShare(*sh, shareClashPort(st.Settings), st.Token)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(a.Paths.Run, 0o755); err != nil {
		return err
	}
	old, _ := os.ReadFile(a.shareConfigPath())
	if !bytes.Equal(old, b) || !p.Running() {
		if err := state.WriteFileAtomic(a.shareConfigPath(), b, 0o600); err != nil {
			return err
		}
		if err := p.Validate(); err != nil {
			return err
		}
		// An account added or removed: reload in place (people connected
		// reconnect by themselves within seconds).
		if p.Running() {
			if err := p.Reload(); err != nil {
				return err
			}
		} else if err := p.Start(); err != nil {
			return err
		}
		a.Log.Add("foxgozar", fmt.Sprintf("sharing on port %d, %d accounts", sh.Port, active))
	}
	return openSharePort(sh.Port)
}

// openSharePort lets the port in from the internet side (the "wan" zone,
// which dualwan puts the domestic line in), or with 0 closes it again.
// A named section in /etc/config/firewall, touched only when it changes.
func openSharePort(port int) error {
	cur, _ := exec.Command("uci", "-q", "get", "firewall."+shareFWSection+".dest_port").Output()
	have := strings.TrimSpace(string(cur))
	want := ""
	if port > 0 {
		want = strconv.Itoa(port)
	}
	if have == want {
		return nil
	}
	var script string
	if port == 0 {
		script = "uci -q delete firewall." + shareFWSection + "\n"
	} else {
		script = fmt.Sprintf(`uci -q delete firewall.%[1]s
uci set firewall.%[1]s=rule
uci set firewall.%[1]s.name='FoxGozar sharing'
uci set firewall.%[1]s.src='wan'
uci set firewall.%[1]s.proto='tcp'
uci set firewall.%[1]s.dest_port='%[2]d'
uci set firewall.%[1]s.target='ACCEPT'
`, shareFWSection, port)
	}
	script += "uci commit firewall\n/etc/init.d/firewall reload >/dev/null 2>&1\n"
	if out, err := exec.Command("sh", "-c", script).CombinedOutput(); err != nil {
		return fmt.Errorf("firewall: %v %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// realityKeys makes a Reality key pair with sing-box itself.
func realityKeys(bin string) (priv, pub string, err error) {
	out, err := exec.Command(bin, "generate", "reality-keypair").Output()
	if err != nil {
		return "", "", fmt.Errorf("reality keys: %w", err)
	}
	for _, l := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "PrivateKey":
			priv = strings.TrimSpace(v)
		case "PublicKey":
			pub = strings.TrimSpace(v)
		}
	}
	if priv == "" || pub == "" {
		return "", "", fmt.Errorf("reality keys: unexpected output %q", out)
	}
	return priv, pub, nil
}

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// ShareInfo is what the UI shows: the server, and each account with its
// link and usage. The server's private key never leaves the router.
type ShareInfo struct {
	Enabled bool            `json:"enabled"`
	Port    int             `json:"port"`
	SNI     string          `json:"sni"`
	Running bool            `json:"running"`
	Address string          `json:"address,omitempty"`
	Via     string          `json:"via,omitempty"` // the interface the address is on
	DualWAN bool            `json:"dualwan"`
	Users   []ShareUserView `json:"users"`
}

type ShareUserView struct {
	state.ShareUser
	UUID     string  `json:"uuid,omitempty"` // in the link only
	Link     string  `json:"link,omitempty"`
	Online   bool    `json:"online"`
	Conns    int     `json:"connections"`
	RateUp   float64 `json:"rate_up"`
	RateDown float64 `json:"rate_down"`
}

func (a *App) ShareInfo() ShareInfo {
	st := a.Store.View()
	info := ShareInfo{DualWAN: dualWANOn(), Port: 8443, SNI: "www.speedtest.net", Users: []ShareUserView{}}
	if st.Share == nil {
		return info
	}
	sh := st.Share
	info.Enabled, info.Port, info.SNI = sh.Enabled, sh.Port, sh.SNI
	info.Running = a.shareProcess().Running()
	// People reach the router over the domestic line: wan2 in dualwan,
	// otherwise the only one there is.
	for _, ifc := range []string{"wan2", "wan"} {
		if ip := ifaceIPv4(ifc); ip != "" {
			info.Address, info.Via = ip, ifc
			break
		}
	}
	u := a.shareUse
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, x := range sh.Users {
		v := ShareUserView{ShareUser: x}
		p := u.pending[x.ID]
		v.Up, v.Down = x.Up+p[0], x.Down+p[1]
		v.Conns = u.open[x.ID]
		v.Online = info.Running && v.Conns > 0
		r := u.rate[x.ID]
		v.RateUp, v.RateDown = r[0], r[1]
		if info.Address != "" && sh.PublicKey != "" {
			v.Link = gen.ShareLink(*sh, x, info.Address, "FoxGozar "+x.Name)
		}
		info.Users = append(info.Users, v)
	}
	return info
}

// ifaceIPv4 asks netifd for the address of a logical interface.
func ifaceIPv4(name string) string {
	out, err := exec.Command("ifstatus", name).Output()
	if err != nil {
		return ""
	}
	var s struct {
		Up   bool `json:"up"`
		Addr []struct {
			Address string `json:"address"`
		} `json:"ipv4-address"`
	}
	if json.Unmarshal(out, &s) != nil || !s.Up || len(s.Addr) == 0 {
		return ""
	}
	return s.Addr[0].Address
}

// ------------------------------------------------------------ dual WAN

// In dualwan (see `foxgozar dualwan`), the domestic line is interface wan2
// with its routes in table DualWANTable; the default route stays on wan
// (Starlink). Iranian addresses are reached over wan2: FoxGozar keeps them
// in IranTable, looked up just after the tunnel's own rule.
const (
	DualWANTable = 102
	IranTable    = 103
	iranRulePref = 110
)

func dualWANOn() bool {
	out, err := exec.Command("uci", "-q", "get", "network.wan2").Output()
	return err == nil && strings.TrimSpace(string(out)) == "interface"
}

// domesticGateway is wan2's default route: "via" address and device.
func domesticGateway() (gw, dev string) {
	out, err := exec.Command("ip", "-4", "route", "show", "table", strconv.Itoa(DualWANTable), "default").Output()
	if err != nil {
		return "", ""
	}
	f := strings.Fields(string(out))
	for i := 0; i+1 < len(f); i++ {
		switch f[i] {
		case "via":
			gw = f[i+1]
		case "dev":
			dev = f[i+1]
		}
	}
	return gw, dev
}

// syncIranRoutes keeps IranTable pointing every Iranian range at the
// domestic line while dualwan is on, and empty otherwise.
func (a *App) syncIranRoutes() {
	key := ""
	var batch strings.Builder
	if dualWANOn() {
		if gw, dev := domesticGateway(); dev != "" {
			v4, _, err := iranCIDRs(a)
			if err == nil && len(v4) > 0 {
				via := ""
				if gw != "" {
					via = " via " + gw
				}
				for _, c := range v4 {
					fmt.Fprintf(&batch, "route replace %s%s dev %s table %d\n", c, via, dev, IranTable)
				}
				h := sha256.Sum256([]byte(batch.String()))
				key = hex.EncodeToString(h[:8])
			}
		}
	}
	a.mu.Lock()
	same := a.iranKey == key
	a.mu.Unlock()
	if same {
		return
	}
	_ = exec.Command("ip", "-4", "route", "flush", "table", strconv.Itoa(IranTable)).Run()
	for exec.Command("ip", "-4", "rule", "del", "pref", strconv.Itoa(iranRulePref)).Run() == nil {
	}
	if key != "" {
		cmd := exec.Command("ip", "-4", "-batch", "-")
		cmd.Stdin = strings.NewReader(batch.String())
		if out, err := cmd.CombinedOutput(); err != nil {
			a.Log.Add("foxgozar", "dual WAN routes: "+strings.TrimSpace(string(out)))
			return
		}
		if out, err := exec.Command("ip", "-4", "rule", "add", "pref", strconv.Itoa(iranRulePref), "lookup", strconv.Itoa(IranTable)).CombinedOutput(); err != nil {
			a.Log.Add("foxgozar", "dual WAN rule: "+strings.TrimSpace(string(out)))
			return
		}
		a.Log.Add("foxgozar", "dual WAN: Iranian addresses go over the domestic line")
	}
	a.mu.Lock()
	a.iranKey = key
	a.mu.Unlock()
}

func iranCIDRs(a *App) (v4, v6 []string, err error) {
	return ruleset.CIDRs(a.SingBoxBin(), a.RuleDir(), gen.RSIranIP)
}

// SyncShare brings the sharing server in line with the saved settings.
func (a *App) SyncShare() error { return a.syncShare(a.Store.View()) }
