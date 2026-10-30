package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"foxgozar/internal/clash"
	"foxgozar/internal/core"
	"foxgozar/internal/gen"
	"foxgozar/internal/warp"
)

// warpTest registers a WARP account and measures n candidate endpoints with
// a throwaway sing-box, leaving the running tunnel alone. A diagnostic:
// it shows whether WARP is reachable from this network at all.
func warpTest(n int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	// Through the tunnel's local proxy when it is up, then directly.
	var acc warp.Account
	var err error
	for _, port := range []int{7890, 0} {
		acc, err = warp.Register(ctx, core.HTTPClient(port, 30*time.Second))
		if err == nil {
			break
		}
		fmt.Println("register:", err)
	}
	if err != nil {
		return err
	}
	fmt.Printf("account %s… address %s reserved %v\n", acc.ID[:8], acc.Address4, acc.Reserved)

	var eps, tags []any
	for i, ep := range warp.Candidates(n) {
		tag := fmt.Sprintf("w%d %s", i+1, ep)
		eps = append(eps, acc.Endpoint(tag, ep))
		tags = append(tags, tag)
	}
	cfg := map[string]any{
		"log":       map[string]any{"level": os.Getenv("FGZ_WARP_LOG")},
		"endpoints": eps,
		"outbounds": []any{map[string]any{"type": "urltest", "tag": "warp", "outbounds": tags,
			"url": "https://cp.cloudflare.com/generate_204", "interval": "10m"}},
		// Marked like the real core: the firewall lets it out directly.
		"route":        map[string]any{"final": "warp", "default_mark": gen.RoutingMark},
		"experimental": map[string]any{"clash_api": map[string]any{"external_controller": "127.0.0.1:9199", "secret": "t"}},
	}
	b, _ := json.Marshal(cfg)
	dir, _ := os.MkdirTemp("", "fgz-warp")
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "c.json")
	_ = os.WriteFile(path, b, 0o600)
	cmd := exec.Command(filepath.Join(paths.Bin, "sing-box"), "run", "-c", path)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	defer cmd.Process.Kill()
	cl := clash.New(9199, "t")
	for i := 0; i < 50 && cl.Ping(ctx) != nil; i++ {
		time.Sleep(100 * time.Millisecond)
	}
	// WireGuard needs a handshake before the first request can pass.
	time.Sleep(3 * time.Second)
	res, err := cl.GroupDelay(ctx, "warp", "https://cp.cloudflare.com/generate_204", 6*time.Second)
	if err != nil {
		return err
	}
	sort.Slice(tags, func(i, j int) bool { return res[tags[i].(string)] > res[tags[j].(string)] })
	ok := 0
	for _, t := range tags {
		if d := res[t.(string)]; d > 0 {
			ok++
			fmt.Printf("  %-34s %d ms\n", t, d)
		} else {
			fmt.Printf("  %-34s failed\n", t)
		}
	}
	fmt.Printf("%d of %d WARP endpoints work from this network\n", ok, len(tags))
	return nil
}

// warpXrayTest tries WARP through Xray with UDP noise sent ahead of the
// WireGuard handshake, in three ways, against n endpoints each.
func warpXrayTest(n int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var acc warp.Account
	var err error
	for _, port := range []int{7890, 0} {
		if acc, err = warp.Register(ctx, core.HTTPClient(port, 30*time.Second)); err == nil {
			break
		}
	}
	if err != nil {
		return err
	}
	noise := []any{
		map[string]any{"rand": "10-20", "delay": "10-16"},
		map[string]any{"rand": "10-20", "delay": "10-16"},
		map[string]any{"rand": "10-20", "delay": "10-16"},
	}
	finalmask := map[string]any{"udp": []any{map[string]any{"type": "noise", "settings": map[string]any{"noise": noise}}}}
	legacy := []any{map[string]any{"type": "rand", "packet": "10-20", "delay": "10-16"},
		map[string]any{"type": "rand", "packet": "10-20", "delay": "10-16"}}
	mark := map[string]any{"mark": gen.RoutingMark}

	type probe struct {
		name string
		port int
	}
	var probes []probe
	var inbounds, outbounds, rules []any
	port := 30100
	for _, ep := range warp.Candidates(n) {
		for _, variant := range []string{"A", "B", "C"} {
			tag := variant + " " + ep
			wg := map[string]any{"tag": tag, "protocol": "wireguard", "settings": map[string]any{
				"secretKey": acc.PrivateKey, "address": []any{acc.Address4 + "/32"}, "mtu": 1280, "noKernelTun": true,
				"reserved": acc.Reserved, "peers": []any{map[string]any{"publicKey": acc.PeerKey, "endpoint": ep,
					"allowedIPs": []any{"0.0.0.0/0", "::/0"}, "keepAlive": 25}}}}
			switch variant {
			case "A":
				wg["streamSettings"] = map[string]any{"sockopt": mark, "finalmask": finalmask}
			case "B":
				wg["streamSettings"] = map[string]any{"sockopt": map[string]any{"dialerProxy": "fm-" + tag}}
				outbounds = append(outbounds, map[string]any{"tag": "fm-" + tag, "protocol": "freedom",
					"streamSettings": map[string]any{"sockopt": mark, "finalmask": finalmask}})
			case "C":
				wg["streamSettings"] = map[string]any{"sockopt": map[string]any{"dialerProxy": "nz-" + tag}}
				outbounds = append(outbounds, map[string]any{"tag": "nz-" + tag, "protocol": "freedom",
					"settings": map[string]any{"noises": legacy}, "streamSettings": map[string]any{"sockopt": mark}})
			}
			outbounds = append(outbounds, wg)
			inbounds = append(inbounds, map[string]any{"tag": "in-" + tag, "listen": "127.0.0.1", "port": port,
				"protocol": "socks", "settings": map[string]any{"udp": true}})
			rules = append(rules, map[string]any{"inboundTag": []any{"in-" + tag}, "outboundTag": tag})
			probes = append(probes, probe{tag, port})
			port++
		}
	}
	cfg := map[string]any{"log": map[string]any{"loglevel": "warning"}, "inbounds": inbounds, "outbounds": outbounds,
		"routing": map[string]any{"rules": rules}}
	b, _ := json.Marshal(cfg)
	dir, _ := os.MkdirTemp("", "fgz-warpx")
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "x.json")
	_ = os.WriteFile(path, b, 0o600)
	cmd := exec.Command(filepath.Join(paths.Bin, "xray"), "run", "-c", path)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	defer cmd.Process.Kill()
	time.Sleep(3 * time.Second)

	type result struct {
		name string
		ms   int
		err  string
	}
	results := make(chan result, len(probes))
	for _, p := range probes {
		go func(p probe) {
			u, _ := url.Parse(fmt.Sprintf("socks5://127.0.0.1:%d", p.port))
			hc := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(u)}}
			t0 := time.Now()
			resp, err := hc.Get("https://cp.cloudflare.com/generate_204")
			if err != nil {
				results <- result{p.name, 0, err.Error()}
				return
			}
			resp.Body.Close()
			results <- result{p.name, int(time.Since(t0).Milliseconds()), ""}
		}(p)
	}
	var all []result
	for range probes {
		all = append(all, <-results)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].name < all[j].name })
	ok := 0
	for _, r := range all {
		if r.ms > 0 {
			ok++
			fmt.Printf("  %-30s %d ms\n", r.name, r.ms)
		} else {
			fmt.Printf("  %-30s failed\n", r.name)
		}
	}
	fmt.Printf("%d of %d work\n", ok, len(all))
	return nil
}
