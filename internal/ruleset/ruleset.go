// Package ruleset keeps the routing rule-sets (Iran, ads) on disk, and
// turns the Iran IP list into CIDRs for the firewall so Iranian traffic
// never enters the core at all.
package ruleset

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"foxgozar/internal/core"
	"foxgozar/internal/state"
)

const base = "https://raw.githubusercontent.com/Chocolate4U/Iran-sing-box-rules/rule-set/"

// Update downloads every rule-set in tags. It keeps a file that fails to
// download, so an update can only make things newer, never missing.
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
