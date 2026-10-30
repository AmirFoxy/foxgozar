package app

import (
	"bufio"
	"context"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"foxgozar/internal/state"
)

// DeviceView is one LAN device as the UI shows it: what the router knows
// about it, its policy, and what it used through the tunnel.
type DeviceView struct {
	MAC      string  `json:"mac"`
	Name     string  `json:"name"`               // the user's name, else the DHCP hostname
	Hostname string  `json:"hostname,omitempty"` // from DHCP
	IP       string  `json:"ip,omitempty"`
	Online   bool    `json:"online"`
	Saved    bool    `json:"saved"` // has a policy of its own
	Policy   string  `json:"policy"`
	Node     string  `json:"node,omitempty"`
	Up       int64   `json:"up"`   // through the tunnel since the service started
	Down     int64   `json:"down"` //
	RateUp   float64 `json:"rate_up"`
	RateDown float64 `json:"rate_down"`
	Conns    int     `json:"connections"`
}

type lease struct{ ip, host string }

// leases reads dnsmasq's lease file: "expiry mac ip hostname clientid".
func leases() map[string]lease {
	out := map[string]lease{}
	f, err := os.Open("/tmp/dhcp.leases")
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		x := strings.Fields(sc.Text())
		if len(x) < 4 {
			continue
		}
		host := x[3]
		if host == "*" {
			host = ""
		}
		out[strings.ToLower(x[1])] = lease{ip: x[2], host: host}
	}
	return out
}

// neighbours reads the ARP table for the LAN interfaces: ip -> mac of every
// complete entry.
func neighbours(lan []string) map[string]string {
	out := map[string]string{}
	isLAN := map[string]bool{}
	for _, l := range lan {
		isLAN[l] = true
	}
	f, err := os.Open("/proc/net/arp")
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Scan() // header
	for sc.Scan() {
		x := strings.Fields(sc.Text())
		// IP, HW type, Flags, HW address, Mask, Device; flag 0x2 = complete
		if len(x) < 6 || x[2] != "0x2" || !isLAN[x[5]] || x[3] == "00:00:00:00:00:00" {
			continue
		}
		out[x[0]] = strings.ToLower(x[3])
	}
	return out
}

// devStats turns the open connections of every core instance into usage
// per device. It runs only while someone looks at the devices page.
type devStats struct {
	mu      sync.Mutex
	conns   map[string][3]int64 // slot+conn id -> up, down, seen generation
	totals  map[string][2]int64 // mac -> up, down
	rates   map[string][2]float64
	open    map[string]int
	last    time.Time
	gen     int64
	until   time.Time
	running bool
}

func newDevStats() *devStats {
	return &devStats{conns: map[string][3]int64{}, totals: map[string][2]int64{},
		rates: map[string][2]float64{}, open: map[string]int{}}
}

// watch keeps the collector running for another minute.
func (a *App) watchDevices() {
	d := a.dev
	d.mu.Lock()
	d.until = time.Now().Add(time.Minute)
	if d.running {
		d.mu.Unlock()
		return
	}
	d.running = true
	d.mu.Unlock()
	go func() {
		for {
			a.collectDevices()
			time.Sleep(2 * time.Second)
			d.mu.Lock()
			if time.Now().After(d.until) {
				d.running = false
				d.rates = map[string][2]float64{}
				d.mu.Unlock()
				return
			}
			d.mu.Unlock()
		}
	}()
}

func (a *App) collectDevices() {
	st := a.Store.View()
	arp := neighbours(st.Settings.LANInterfaces)
	type sample struct {
		key, mac string
		up, down int64
	}
	var samples []sample
	for _, sl := range a.slots {
		if !sl.sb.Running() {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		cs, err := a.clientFor(sl).Connections(ctx)
		cancel()
		if err != nil {
			continue
		}
		for _, c := range cs.Connections {
			id, _ := c["id"].(string)
			md, _ := c["metadata"].(map[string]any)
			src, _ := md["sourceIP"].(string)
			mac := arp[src]
			if id == "" || mac == "" {
				continue // the router's own traffic, or a device that left
			}
			up, _ := c["upload"].(float64)
			down, _ := c["download"].(float64)
			samples = append(samples, sample{string(rune('0'+sl.id)) + id, mac, int64(up), int64(down)})
		}
	}

	d := a.dev
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	secs := now.Sub(d.last).Seconds()
	first := d.last.IsZero()
	d.last = now
	d.gen++
	delta := map[string][2]int64{}
	open := map[string]int{}
	for _, s := range samples {
		prev, seen := d.conns[s.key]
		du, dd := s.up, s.down
		if seen {
			du, dd = s.up-prev[0], s.down-prev[1]
		}
		if du < 0 || dd < 0 {
			du, dd = 0, 0
		}
		d.conns[s.key] = [3]int64{s.up, s.down, d.gen}
		t := d.totals[s.mac]
		d.totals[s.mac] = [2]int64{t[0] + du, t[1] + dd}
		x := delta[s.mac]
		delta[s.mac] = [2]int64{x[0] + du, x[1] + dd}
		open[s.mac]++
	}
	for k, v := range d.conns {
		if v[2] != d.gen {
			delete(d.conns, k) // closed; its bytes are already counted
		}
	}
	d.open = open
	d.rates = map[string][2]float64{}
	if !first && secs > 0 {
		for mac, x := range delta {
			d.rates[mac] = [2]float64{float64(x[0]) / secs, float64(x[1]) / secs}
		}
	}
}

// Devices lists every device the router knows of or that has a policy.
func (a *App) Devices() []DeviceView {
	a.watchDevices()
	st := a.Store.View()
	ls := leases()
	arp := neighbours(st.Settings.LANInterfaces)
	online := map[string]string{} // mac -> ip
	for ip, mac := range arp {
		online[mac] = ip
	}
	seen := map[string]*DeviceView{}
	get := func(mac string) *DeviceView {
		if v, ok := seen[mac]; ok {
			return v
		}
		v := &DeviceView{MAC: mac, Policy: st.Settings.DefaultPolicy}
		seen[mac] = v
		return v
	}
	for mac, l := range ls {
		v := get(mac)
		v.Hostname, v.IP = l.host, l.ip
	}
	for mac, ip := range online {
		v := get(mac)
		v.Online, v.IP = true, ip
	}
	for _, d := range st.Devices {
		v := get(d.MAC)
		v.Saved, v.Policy, v.Node = true, d.Policy, d.Node
		v.Name = d.Name
	}

	a.dev.mu.Lock()
	out := make([]DeviceView, 0, len(seen))
	for mac, v := range seen {
		if v.Name == "" {
			v.Name = v.Hostname
		}
		t := a.dev.totals[mac]
		r := a.dev.rates[mac]
		v.Up, v.Down, v.RateUp, v.RateDown, v.Conns = t[0], t[1], r[0], r[1], a.dev.open[mac]
		out = append(out, *v)
	}
	a.dev.mu.Unlock()

	sort.Slice(out, func(i, j int) bool {
		if out[i].Online != out[j].Online {
			return out[i].Online
		}
		ni, nj := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if ni != nj {
			if ni == "" || nj == "" {
				return nj == "" // named devices first
			}
			return ni < nj
		}
		return out[i].MAC < out[j].MAC
	})
	return out
}

// SetDevice saves a device's name and policy and applies it. Direct and
// block take effect in the firewall at once; a server choice reaches the
// core the same way as any other change, without cutting connections.
func (a *App) SetDevice(ctx context.Context, d state.Device) error {
	err := a.Store.Update(func(st *state.State) error {
		if cur := st.DeviceByMAC(d.MAC); cur != nil {
			*cur = d
		} else {
			st.Devices = append(st.Devices, d)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return a.Apply(ctx)
}

// ForgetDevice drops a device's own policy: it follows the default again.
func (a *App) ForgetDevice(ctx context.Context, mac string) error {
	err := a.Store.Update(func(st *state.State) error {
		kept := st.Devices[:0]
		for _, d := range st.Devices {
			if d.MAC != mac {
				kept = append(kept, d)
			}
		}
		st.Devices = kept
		return nil
	})
	if err != nil {
		return err
	}
	return a.Apply(ctx)
}
