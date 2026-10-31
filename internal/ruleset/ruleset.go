// Package ruleset keeps the routing rule-sets (Iran, ads) on disk, and
// turns the Iran IP list into CIDRs for the firewall so Iranian traffic
// never enters the core at all.
package ruleset

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"foxgozar/internal/core"
	"foxgozar/internal/state"
)

const base = "https://raw.githubusercontent.com/Chocolate4U/Iran-sing-box-rules/rule-set/"

// Update downloads every rule-set in tags and returns the ones whose content
// changed. It keeps a file that fails to download, so an update can only
// make things newer, never missing.
func Update(ctx context.Context, f *core.Fetcher, dir string, tags []string) (updated []string, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var errs []string
	for _, t := range tags {
		b, e := f.Get(ctx, base+t+".srs", 32<<20)
		if e != nil {
			errs = append(errs, t+": "+e.Error())
			continue
		}
		if len(b) < 8 || string(b[:3]) != "SRS" {
			errs = append(errs, t+": not a rule-set file")
			continue
		}
		path := filepath.Join(dir, t+".srs")
		if old, e := os.ReadFile(path); e == nil && bytes.Equal(old, b) {
			// Unchanged: record the check, leave the core alone.
			now := time.Now()
			_ = os.Chtimes(path, now, now)
			continue
		}
		if e := state.WriteFileAtomic(filepath.Join(dir, t+".srs"), b, 0o644); e != nil {
			errs = append(errs, t+": "+e.Error())
			continue
		}
		_ = os.Remove(filepath.Join(dir, t+".cidr"))
		updated = append(updated, t)
	}
	if len(errs) > 0 {
		err = fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return updated, err
}

// Age of a rule-set file; a very large value when it is missing.
func Age(dir, tag string) time.Duration {
	fi, err := os.Stat(filepath.Join(dir, tag+".srs"))
	if err != nil {
		return 1 << 62
	}
	return time.Since(fi.ModTime())
}

// CIDRs returns the ip_cidr entries of an IP rule-set, split by family.
// The decompiled list is cached next to the .srs file.
func CIDRs(singBox, dir, tag string) (v4, v6 []string, err error) {
	srs := filepath.Join(dir, tag+".srs")
	cache := filepath.Join(dir, tag+".cidr")
	si, err := os.Stat(srs)
	if err != nil {
		return nil, nil, err
	}
	if ci, e := os.Stat(cache); e == nil && !ci.ModTime().Before(si.ModTime()) {
		if b, e := os.ReadFile(cache); e == nil {
			v4, v6 = split(strings.Fields(string(b)))
			return v4, v6, nil
		}
	}
	tmp := filepath.Join(os.TempDir(), "fgz-"+tag+".json")
	defer os.Remove(tmp)
	if out, e := exec.Command(singBox, "rule-set", "decompile", "-o", tmp, srs).CombinedOutput(); e != nil {
		return nil, nil, fmt.Errorf("decompile %s: %v %s", tag, e, strings.TrimSpace(string(out)))
	}
	b, err := os.ReadFile(tmp)
	if err != nil {
		return nil, nil, err
	}
	var rs struct {
		Rules []struct {
			IPCIDR []string `json:"ip_cidr"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(b, &rs); err != nil {
		return nil, nil, err
	}
	var all []string
	for _, r := range rs.Rules {
		all = append(all, r.IPCIDR...)
	}
	_ = state.WriteFileAtomic(cache, []byte(strings.Join(all, "\n")), 0o644)
	v4, v6 = split(all)
	return v4, v6, nil
}

func split(all []string) (v4, v6 []string) {
	for _, c := range all {
		if strings.Contains(c, ":") {
			v6 = append(v6, c)
		} else {
			v4 = append(v4, c)
		}
	}
	return
}

// URLTag is the rule-set tag a user's rule-set link is kept under.
func URLTag(url string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(url)))
	return "url-" + hex.EncodeToString(h[:5])
}

// UpdateURLs downloads rule-sets from the user's own links. A link may give
// a compiled sing-box rule-set (.srs), a source rule-set (JSON), or a plain
// list: one domain or IP range per line, Clash style ("DOMAIN-SUFFIX,x.com")
// accepted too. Anything but .srs is compiled with sing-box. Only the links
// whose file is missing are fetched, unless all is set. Like Update, a
// failed download leaves the old file in place.
func UpdateURLs(ctx context.Context, f *core.Fetcher, singBox, dir string, urls []string, all bool) (updated []string, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var errs []string
	for _, u := range urls {
		tag := URLTag(u)
		path := filepath.Join(dir, tag+".srs")
		if !all && Age(dir, tag) < 1<<61 {
			continue
		}
		b, e := f.Get(ctx, u, 32<<20)
		if e == nil {
			b, e = toSRS(singBox, b)
		}
		if e != nil {
			errs = append(errs, u+": "+e.Error())
			continue
		}
		if old, e := os.ReadFile(path); e == nil && bytes.Equal(old, b) {
			now := time.Now()
			_ = os.Chtimes(path, now, now)
			continue
		}
		if e := state.WriteFileAtomic(path, b, 0o644); e != nil {
			errs = append(errs, u+": "+e.Error())
			continue
		}
		updated = append(updated, tag)
	}
	if len(errs) > 0 {
		err = fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return updated, err
}

func toSRS(singBox string, b []byte) ([]byte, error) {
	if len(b) >= 3 && string(b[:3]) == "SRS" {
		return b, nil
	}
	src := b
	if t := bytes.TrimSpace(b); len(t) == 0 || t[0] != '{' {
		var err error
		if src, err = ListSource(string(b)); err != nil {
			return nil, err
		}
	}
	dir, err := os.MkdirTemp("", "fgz-rs")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	in, out := filepath.Join(dir, "in.json"), filepath.Join(dir, "out.srs")
	if err := os.WriteFile(in, src, 0o644); err != nil {
		return nil, err
	}
	if o, e := exec.Command(singBox, "rule-set", "compile", "-o", out, in).CombinedOutput(); e != nil {
		return nil, fmt.Errorf("not a rule-set or a list: %s", strings.TrimSpace(string(o)))
	}
	return os.ReadFile(out)
}

// ListSource turns a plain list into a sing-box source rule-set.
func ListSource(text string) ([]byte, error) {
	var suffix, full, keyword, cidr []string
	for _, line := range strings.Split(text, "\n") {
		if i := strings.Index(line, " #"); i >= 0 { // trailing comment
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		kind, v := "", line
		if i := strings.Index(line, ","); i > 0 {
			kind, v = strings.ToUpper(line[:i]), line[i+1:]
			if j := strings.Index(v, ","); j >= 0 { // "IP-CIDR,1.2.3.0/24,no-resolve"
				v = v[:j]
			}
		}
		v = strings.TrimPrefix(strings.TrimSpace(v), "+.")
		switch {
		case kind == "DOMAIN":
			full = append(full, v)
		case kind == "DOMAIN-KEYWORD":
			keyword = append(keyword, v)
		case kind == "IP-CIDR" || kind == "IP-CIDR6":
			cidr = append(cidr, v)
		case kind == "DOMAIN-SUFFIX" || kind == "":
			if _, _, e := net.ParseCIDR(v); e == nil {
				cidr = append(cidr, v)
			} else if ip := net.ParseIP(v); ip != nil {
				cidr = append(cidr, v)
			} else if strings.Contains(v, ".") && !strings.ContainsAny(v, " /:") {
				suffix = append(suffix, v)
			}
		}
	}
	rule := map[string]any{}
	for k, v := range map[string][]string{"domain_suffix": suffix, "domain": full, "domain_keyword": keyword, "ip_cidr": cidr} {
		if len(v) > 0 {
			rule[k] = v
		}
	}
	if len(rule) == 0 {
		return nil, fmt.Errorf("no domains or IP ranges found")
	}
	return json.Marshal(map[string]any{"version": 2, "rules": []any{rule}})
}
