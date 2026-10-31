package app

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"foxgozar/internal/state"
)

// Anti-lag: when someone downloads, the modem's buffers fill and every
// packet waits behind them; ping in a game jumps by tens or hundreds of ms.
// cake, OpenWrt's SQM queue, keeps the queue on the router just below the
// line's speed, where it can let game and call packets go first. Measured
// on the owner's line before it: 100 ms idle, 125 ms average and 198 ms
// peaks with two downloads running.
//
// FoxGozar uses OpenWrt's own sqm-scripts: the official packages, installed
// on first use, and one named section in /etc/config/sqm that it alone
// touches.

const sqmSection = "foxgozar"

// shapeShare of the measured speed is what cake gets: just below the line,
// so the queue builds here and not in the modem.
const shapeShare = 0.9

type antiLagState struct {
	mu         sync.Mutex
	installing bool
	lastErr    string
}

// sqmScript writes (or switches off) the SQM section. dev is the internet
// port's device; speeds are the line's, in Mbit/s.
func sqmScript(on bool, dev string, downMbps, upMbps int) string {
	if !on {
		return fmt.Sprintf("uci -q set sqm.%[1]s.enabled='0' && uci commit sqm\n", sqmSection)
	}
	kbit := func(mbps int) int { return int(float64(mbps) * 1000 * shapeShare) }
	return fmt.Sprintf(`uci -q delete sqm.%[1]s
uci set sqm.%[1]s=queue
uci set sqm.%[1]s.enabled='1'
uci set sqm.%[1]s.interface='%[2]s'
uci set sqm.%[1]s.download='%[3]d'
uci set sqm.%[1]s.upload='%[4]d'
uci set sqm.%[1]s.qdisc='cake'
uci set sqm.%[1]s.script='piece_of_cake.qos'
uci set sqm.%[1]s.linklayer='none'
uci commit sqm
`, sqmSection, dev, kbit(downMbps), kbit(upMbps))
}

func runSh(script string) error {
	if out, err := exec.Command("sh", "-c", script).CombinedOutput(); err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func wanDevice() string {
	out, err := exec.Command("uci", "-q", "get", "network.wan.device").Output()
	if d := strings.TrimSpace(string(out)); err == nil && d != "" {
		return d
	}
	return "wan"
}

func sqmInstalled() bool {
	return exec.Command("sh", "-c", "[ -x /etc/init.d/sqm ] && [ -n \"$(find /lib/modules -name 'sch_cake.ko' 2>/dev/null)\" ]").Run() == nil
}

// AntiLagActive reports whether cake is on the internet port right now.
func AntiLagActive() bool {
	out, _ := exec.Command("tc", "qdisc", "show", "dev", wanDevice()).Output()
	return strings.Contains(string(out), "cake")
}

// syncAntiLag brings SQM in line with the settings, installing it first
// when it is turned on for the first time.
func (a *App) syncAntiLag(ctx context.Context) error {
	s := a.Store.View().Settings
	if !s.AntiLag {
		if out, _ := exec.Command("uci", "-q", "get", "sqm."+sqmSection).Output(); strings.TrimSpace(string(out)) == "" {
			return nil // never set up: nothing to undo
		}
		if err := runSh(sqmScript(false, "", 0, 0) + "/etc/init.d/sqm restart >/dev/null 2>&1\n"); err != nil {
			return err
		}
		a.Log.Add("foxgozar", "anti-lag off")
		return nil
	}
	if s.DownMbps <= 0 || s.UpMbps <= 0 {
		return errors.New("enter your line's download and upload speed first")
	}
	if !sqmInstalled() {
		a.lag.mu.Lock()
		a.lag.installing = true
		a.lag.mu.Unlock()
		a.Log.Add("foxgozar", "anti-lag: installing OpenWrt's sqm-scripts and cake")
		err := runSh("apk add sqm-scripts kmod-sched-cake || (apk update && apk add sqm-scripts kmod-sched-cake)")
		a.lag.mu.Lock()
		a.lag.installing = false
		a.lag.mu.Unlock()
		if err != nil {
			return fmt.Errorf("could not install SQM: %w", err)
		}
	}
	if err := runSh(sqmScript(true, wanDevice(), s.DownMbps, s.UpMbps) +
		"/etc/init.d/sqm enable\n/etc/init.d/sqm restart >/dev/null 2>&1\n"); err != nil {
		return err
	}
	a.Log.Add("foxgozar", fmt.Sprintf("anti-lag on: %d down / %d up Mbit/s, shaped at %d%%", s.DownMbps, s.UpMbps, int(shapeShare*100)))
	return nil
}

// AntiLagStatus is what the UI shows.
type AntiLagStatus struct {
	On         bool   `json:"on"`
	DownMbps   int    `json:"down_mbps"`
	UpMbps     int    `json:"up_mbps"`
	Installed  bool   `json:"installed"`
	Installing bool   `json:"installing"`
	Active     bool   `json:"active"` // cake is on the port now
	Error      string `json:"error,omitempty"`
}

func (a *App) AntiLagStatus() AntiLagStatus {
	s := a.Store.View().Settings
	a.lag.mu.Lock()
	defer a.lag.mu.Unlock()
	return AntiLagStatus{On: s.AntiLag, DownMbps: s.DownMbps, UpMbps: s.UpMbps, Installed: sqmInstalled(),
		Installing: a.lag.installing, Active: AntiLagActive(), Error: a.lag.lastErr}
}

// SetAntiLag saves the settings and applies them in the background (the
// first time installs packages, which takes a while).
func (a *App) SetAntiLag(on bool, down, up int) error {
	if down < 0 || up < 0 || down > 10000 || up > 10000 {
		return errors.New("speeds are in Mbit/s, 1 to 10000")
	}
	if on && (down == 0 || up == 0) {
		return errors.New("enter your line's download and upload speed first")
	}
	if err := a.Store.Update(func(st *state.State) error {
		st.Settings.AntiLag, st.Settings.DownMbps, st.Settings.UpMbps = on, down, up
		return nil
	}); err != nil {
		return err
	}
	go func() {
		err := a.syncAntiLag(context.Background())
		a.lag.mu.Lock()
		a.lag.lastErr = ""
		if err != nil {
			a.lag.lastErr = err.Error()
		}
		a.lag.mu.Unlock()
		if err != nil {
			a.Log.Add("foxgozar", "anti-lag: "+err.Error())
		}
	}()
	return nil
}
