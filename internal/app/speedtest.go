package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// A server's real usefulness is more than its ping: how fast it downloads,
// and whether new connections come up steadily. A server whose connections
// sometimes take seconds to open (packets lost while connecting, as with
// unencrypted servers under filtering) feels slow even with a fine ping.

// SpeedResult is one server's last test.
type SpeedResult struct {
	Node     string  `json:"node"`
	Mbps     float64 `json:"mbps"`     // download, megabits per second; 0 = failed
	Setup    []int   `json:"setup"`    // ms to open each fresh connection and get a reply; -1 failed
	Best     int     `json:"best"`     // the fastest setup: what the server can do
	Slow     int     `json:"slow"`     // setups far above the best, or failed
	Unstable bool    `json:"unstable"` // Slow is not a one-off
	At       int64   `json:"at"`
	Error    string  `json:"error,omitempty"`
}

const (
	setupTries   = 8
	downloadURL  = "https://speed.cloudflare.com/__down?bytes=25000000"
	downloadTime = 8 * time.Second
)

type speedTests struct {
	mu  sync.Mutex
	res map[string]SpeedResult
}

func (a *App) speedClient(node string, keepAlive bool, timeout time.Duration) (*http.Client, *http.Transport) {
	st := a.Store.View()
	proxy := &url.URL{Scheme: "socks5", Host: fmt.Sprintf("127.0.0.1:%d", a.act().probePort(st.Settings)), User: url.UserPassword(node, st.Token)}
	tr := &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: !keepAlive, DisableCompression: true,
		ResponseHeaderTimeout: timeout}
	return &http.Client{Transport: tr, Timeout: timeout}, tr
}

// judgeSetup decides from the setup times whether connections come up
// steadily. The yardstick is the fastest setup, what the server can do:
// the median would not do, since on a bad server most setups are the slow
// ones. A setup is slow when it failed, or took over three times the best
// and over a second more. Two slow ones in eight is no accident.
func judgeSetup(setup []int) (best, slow int, unstable bool) {
	best = -1
	for _, d := range setup {
		if d >= 0 && (best < 0 || d < best) {
			best = d
		}
	}
	if best < 0 {
		return 0, len(setup), true
	}
	for _, d := range setup {
		if d < 0 || (d > 3*best && d > best+1000) {
			slow++
		}
	}
	return best, slow, slow >= 2
}

// SpeedTest measures one server: steadiness of new connections, then the
// download speed. It uses about 25 MB of the server's traffic at most.
func (a *App) SpeedTest(ctx context.Context, node string) (SpeedResult, error) {
	if !a.running() {
		return SpeedResult{}, errors.New("connect first: the test goes through the tunnel")
	}
	st := a.Store.View()
	if st.NodeByID(node) == nil {
		return SpeedResult{}, errors.New("no such server")
	}
	if !a.claim("speedtest") {
		return SpeedResult{}, errors.New("a speed test is already running")
	}
	defer a.release("speedtest")
	r := SpeedResult{Node: node, At: time.Now().Unix()}

	// 1. Fresh connections, one after another. At most 30 s in all: the web
	// server gives a request 60.
	c, tr := a.speedClient(node, false, 6*time.Second)
	sctx, scancel := context.WithTimeout(ctx, 30*time.Second)
	defer scancel()
	for i := 0; i < setupTries && sctx.Err() == nil; i++ {
		t0 := time.Now()
		req, _ := http.NewRequestWithContext(sctx, http.MethodGet, st.Settings.TestURL, nil)
		resp, err := c.Do(req)
		if err != nil {
			r.Setup = append(r.Setup, -1)
			continue
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
		r.Setup = append(r.Setup, int(time.Since(t0).Milliseconds()))
	}
	tr.CloseIdleConnections()
	r.Best, r.Slow, r.Unstable = judgeSetup(r.Setup)

	// 2. Download for a few seconds; the rate counts from the first byte, so
	// connection setup (measured above) does not lower it.
	c, tr = a.speedClient(node, true, downloadTime+15*time.Second)
	defer tr.CloseIdleConnections()
	dctx, cancel := context.WithTimeout(ctx, downloadTime+10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(dctx, http.MethodGet, downloadURL, nil)
	resp, err := c.Do(req)
	if err != nil {
		r.Error = "download: " + err.Error()
	} else {
		var n int64
		buf := make([]byte, 64<<10)
		t0 := time.Now()
		for time.Since(t0) < downloadTime {
			k, err := resp.Body.Read(buf)
			n += int64(k)
			if err != nil {
				break
			}
		}
		resp.Body.Close()
		if secs := time.Since(t0).Seconds(); secs > 0 && n > 0 {
			r.Mbps = float64(n) * 8 / secs / 1e6
		} else {
			r.Error = "download: nothing arrived"
		}
	}

	a.speed.mu.Lock()
	a.speed.res[node] = r
	a.speed.mu.Unlock()
	a.Log.Add("foxgozar", fmt.Sprintf("speed test %s: %.1f Mbps, connection setup best %d ms, %d of %d slow",
		node, r.Mbps, r.Best, r.Slow, len(r.Setup)))
	return r, nil
}

// SpeedResults are the last test of every server tested since the start.
func (a *App) SpeedResults() map[string]SpeedResult {
	a.speed.mu.Lock()
	defer a.speed.mu.Unlock()
	out := make(map[string]SpeedResult, len(a.speed.res))
	for k, v := range a.speed.res {
		out[k] = v
	}
	return out
}
