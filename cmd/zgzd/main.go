// zgzd is the Zirgozar daemon and its command line.
//
//	zgzd run                  the service (procd starts this)
//	zgzd status               state of the tunnel
//	zgzd start | stop | restart
//	zgzd import < links.txt   add share links (or a sing-box JSON)
//	zgzd sub URL              add a subscription
//	zgzd test                 measure every node
//	zgzd select ID|auto
//	zgzd nodes | logs | config
//	zgzd update sing-box|xray|rules
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"zirgozar/internal/api"
	"zirgozar/internal/app"
	"zirgozar/internal/state"
)

var version = "dev"

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

var paths = app.Paths{
	Etc: env("ZGZ_ETC", "/etc/zirgozar"),
	Bin: env("ZGZ_BIN", "/usr/share/zirgozar/bin"),
	Run: env("ZGZ_RUN", "/var/run/zirgozar"),
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd, args := os.Args[1], os.Args[2:]
	if cmd == "run" {
		if err := run(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if cmd == "version" {
		fmt.Println(version)
		return
	}
	if err := client(cmd, args); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: zgzd run | status | start | stop | restart | import | sub URL | test | select ID | nodes | logs | config | update sing-box|xray|rules | version")
	os.Exit(2)
}

func run() error {
	st, err := state.Open(filepath.Join(paths.Etc, "state.json"))
	if err != nil {
		return err
	}
	a := app.New(paths, st)
	a.Log.Add("zgzd", "zirgozar "+version+" starting")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer cancel()

	srv := &http.Server{
		Handler:           (&api.Server{App: a}).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", st.View().Settings.APIPort))
	if err != nil {
		return err
	}
	go func() { _ = srv.Serve(ln) }()

	a.Boot(ctx)
	<-ctx.Done()

	// procd stop: leave the network as it was without us.
	enabled := st.View().Settings.Enabled
	a.Stop()
	if enabled {
		// Stop() records "off"; a service stop is not the user switching
		// off, so the next boot must still connect.
		_ = st.Update(func(s *state.State) error { s.Settings.Enabled = true; return nil })
	}
	sh, c := context.WithTimeout(context.Background(), 2*time.Second)
	defer c()
	return srv.Shutdown(sh)
}

// ---------------------------------------------------------------- CLI

func call(method, path string, body any, out any) error {
	b, err := os.ReadFile(filepath.Join(paths.Etc, "state.json"))
	if err != nil {
		return fmt.Errorf("is zgzd installed? %w", err)
	}
	var st state.State
	st.Settings = state.Defaults()
	if err := json.Unmarshal(b, &st); err != nil {
		return err
	}
	var rd io.Reader
	if body != nil {
		j, _ := json.Marshal(body)
		rd = bytes.NewReader(j)
	}
	req, _ := http.NewRequest(method, fmt.Sprintf("http://127.0.0.1:%d%s", st.Settings.APIPort, path), rd)
	req.Header.Set("Authorization", "Bearer "+st.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		return fmt.Errorf("zgzd is not running? %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			if out != nil {
				_ = json.Unmarshal(data, out)
			}
			return fmt.Errorf("%s", e.Error)
		}
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if out == nil {
		var pretty bytes.Buffer
		if json.Indent(&pretty, data, "", "  ") == nil {
			fmt.Println(pretty.String())
		} else {
			fmt.Println(string(data))
		}
		return nil
	}
	return json.Unmarshal(data, out)
}

func client(cmd string, args []string) error {
	switch cmd {
	case "status":
		return call("GET", "/api/status", nil, nil)
	case "start":
		t := time.Now()
		err := call("POST", "/api/start", nil, nil)
		fmt.Printf("(%d ms)\n", time.Since(t).Milliseconds())
		return err
	case "stop":
		return call("POST", "/api/stop", nil, nil)
	case "restart":
		return call("POST", "/api/restart", nil, nil)
	case "import":
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		return call("POST", "/api/nodes", map[string]string{"text": string(b)}, nil)
	case "sub":
		if len(args) < 1 {
			usage()
		}
		return call("POST", "/api/subs", map[string]any{"url": args[0]}, nil)
	case "nodes":
		var ns []struct {
			ID, Name, Type, Server string
			Port, Delay            int
		}
		if err := call("GET", "/api/nodes", nil, &ns); err != nil {
			return err
		}
		for _, n := range ns {
			d := "-"
			if n.Delay > 0 {
				d = fmt.Sprintf("%dms", n.Delay)
			} else if n.Delay < 0 {
				d = "fail"
			}
			fmt.Printf("%-14s %-6s %-11s %s\n", n.ID, d, n.Type, n.Name)
		}
		return nil
	case "test":
		var res map[string]int
		if err := call("POST", "/api/test", nil, &res); err != nil {
			return err
		}
		ids := make([]string, 0, len(res))
		for id := range res {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool {
			a, b := res[ids[i]], res[ids[j]]
			if (a < 0) != (b < 0) {
				return b < 0
			}
			return a < b
		})
		ok := 0
		for _, id := range ids {
			if res[id] > 0 {
				ok++
				fmt.Printf("%-14s %dms\n", id, res[id])
			}
		}
		fmt.Printf("%d of %d nodes answered\n", ok, len(res))
		return nil
	case "select":
		if len(args) < 1 {
			usage()
		}
		return call("POST", "/api/select", map[string]string{"id": args[0]}, nil)
	case "logs":
		var ls []struct {
			Time   int64
			Source string
			Text   string
		}
		if err := call("GET", "/api/logs", nil, &ls); err != nil {
			return err
		}
		for _, l := range ls {
			fmt.Printf("%s %-8s %s\n", time.Unix(l.Time, 0).Format("15:04:05"), l.Source, l.Text)
		}
		return nil
	case "config":
		return call("GET", "/api/config", nil, nil)
	case "settings":
		if len(args) == 0 {
			return call("GET", "/api/settings", nil, nil)
		}
		// zgzd settings key=value ...
		patch := map[string]any{}
		for _, kv := range args {
			k, v, _ := strings.Cut(kv, "=")
			var j any
			if json.Unmarshal([]byte(v), &j) != nil {
				j = v
			}
			patch[k] = j
		}
		return call("PUT", "/api/settings", patch, nil)
	case "update":
		if len(args) < 1 {
			usage()
		}
		if args[0] == "rules" {
			return call("POST", "/api/rulesets/update", nil, nil)
		}
		return call("POST", "/api/core/"+args[0]+"/update", nil, nil)
	}
	usage()
	return nil
}
