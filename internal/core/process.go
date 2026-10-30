// Package core runs and installs the proxy cores (sing-box, and Xray for the
// few transports sing-box lacks).
package core

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Process supervises one core process: start, crash restart with backoff,
// in-place reload, stop.
type Process struct {
	Name   string
	Bin    string
	Args   []string
	Check  []string // args for a config check, nil when there is none
	Log    *Ring
	OnExit func(err error) // called when the process dies on its own

	mu      sync.Mutex
	cmd     *exec.Cmd
	want    bool
	started time.Time
	done    chan struct{}
	fails   int
	lastErr string
}

func (p *Process) Running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cmd != nil
}

func (p *Process) Since() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.started
}

func (p *Process) LastError() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastErr
}

// Validate runs the core's own config check, so a bad config is reported
// instead of taking down a tunnel that works.
func (p *Process) Validate() error {
	if p.Check == nil {
		return nil
	}
	out, err := exec.Command(p.Bin, p.Check...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return errors.New(lastLines(msg, 3))
	}
	return nil
}

func (p *Process) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd != nil {
		return nil
	}
	p.want = true
	p.fails = 0
	return p.spawnLocked()
}

func (p *Process) spawnLocked() error {
	cmd := exec.Command(p.Bin, p.Args...)
	cmd.Env = append(os.Environ(), "ENABLE_DEPRECATED_SPECIAL_OUTBOUNDS=false")
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	cmd.SysProcAttr = sysProcAttr()
	if err := cmd.Start(); err != nil {
		pw.Close()
		p.lastErr = err.Error()
		return err
	}
	p.cmd = cmd
	p.started = time.Now()
	p.done = make(chan struct{})
	p.Log.Add(p.Name, "started, pid "+fmt.Sprint(cmd.Process.Pid))
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64*1024), 64*1024)
		for sc.Scan() {
			p.Log.Add(p.Name, sc.Text())
		}
	}()
	done := p.done
	go func() {
		err := cmd.Wait()
		pw.Close()
		p.mu.Lock()
		p.cmd = nil
		close(done)
		want := p.want
		if want {
			p.fails++
			p.lastErr = fmt.Sprintf("exited: %v", err)
			p.Log.Add(p.Name, p.lastErr)
		}
		fails := p.fails
		p.mu.Unlock()
		if !want {
			return
		}
		if p.OnExit != nil {
			p.OnExit(err)
		}
		// Crash loop protection: 1s, 2s, 4s ... capped at 30s.
		delay := time.Second << min(fails-1, 5)
		if delay > 30*time.Second {
			delay = 30 * time.Second
		}
		time.Sleep(delay)
		p.mu.Lock()
		if p.want && p.cmd == nil {
			if err := p.spawnLocked(); err != nil {
				p.Log.Add(p.Name, "restart failed: "+err.Error())
			}
		}
		p.mu.Unlock()
	}()
	return nil
}

// Reload asks the core to re-read its config in place (sing-box: SIGHUP).
func (p *Process) Reload() error {
	p.mu.Lock()
	cmd := p.cmd
	p.mu.Unlock()
	if cmd == nil {
		return p.Start()
	}
	return cmd.Process.Signal(syscall.SIGHUP)
}

func (p *Process) Stop() {
	p.mu.Lock()
	p.want = false
	cmd, done := p.cmd, p.done
	p.mu.Unlock()
	if cmd == nil {
		return
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}
	p.Log.Add(p.Name, "stopped")
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// Listening reports whether something listens on TCP port (any address),
// read straight from /proc so no connection is ever made to the core.
func Listening(port int) bool {
	want := fmt.Sprintf(":%04X ", port)
	for _, f := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, line := range bytes.Split(b, []byte{'\n'}) {
			fields := strings.Fields(string(line))
			// local_address is field 1, state 0A is LISTEN
			if len(fields) > 3 && strings.HasSuffix(fields[1]+" ", want) && fields[3] == "0A" {
				return true
			}
		}
	}
	return false
}

// WaitListening polls every 50ms. The core usually listens within 300ms.
func WaitListening(port int, timeout time.Duration, alive func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if Listening(port) {
			return true
		}
		if alive != nil && !alive() {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// Ring keeps the last lines of every core's output for the log page.
type Ring struct {
	mu    sync.Mutex
	lines []LogLine
	seq   int64
	max   int
}

type LogLine struct {
	Seq    int64  `json:"seq"`
	Time   int64  `json:"time"`
	Source string `json:"source"`
	Text   string `json:"text"`
}

func NewRing(max int) *Ring { return &Ring{max: max} }

func (r *Ring) Add(source, text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	r.lines = append(r.lines, LogLine{Seq: r.seq, Time: time.Now().Unix(), Source: source, Text: text})
	if len(r.lines) > r.max {
		r.lines = append([]LogLine(nil), r.lines[len(r.lines)-r.max:]...)
	}
}

func (r *Ring) Since(seq int64) []LogLine {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []LogLine{}
	for _, l := range r.lines {
		if l.Seq > seq {
			out = append(out, l)
		}
	}
	return out
}
