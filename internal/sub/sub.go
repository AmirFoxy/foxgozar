// Package sub refreshes subscriptions.
package sub

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"foxgozar/internal/core"
	"foxgozar/internal/link"
	"foxgozar/internal/state"
)

// Most panels (Marzban, 3x-ui, Hiddify) answer this User-Agent with plain
// share links, which is what we read best.
const DefaultUA = "v2rayN/7.13"

type Info struct {
	Upload, Download, Total, Expire int64
}

// parseUserInfo reads "upload=1; download=2; total=3; expire=4".
func parseUserInfo(h string) (i Info) {
	for _, part := range strings.Split(h, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		switch strings.ToLower(k) {
		case "upload":
			i.Upload = n
		case "download":
			i.Download = n
		case "total":
			i.Total = n
		case "expire":
			i.Expire = n
		}
	}
	return
}

// Fetch downloads and parses one subscription.
func Fetch(ctx context.Context, f *core.Fetcher, s state.Subscription) ([]state.Node, Info, []string, error) {
	ua := s.UserAgent
	if ua == "" {
		ua = DefaultUA
	}
	body, hdr, err := f.GetUA(ctx, s.URL, ua, 16<<20)
	if err != nil {
		return nil, Info{}, nil, err
	}
	res := link.ParseBody(string(body))
	if len(res.Nodes) == 0 {
		msg := "no usable nodes in the subscription"
		if len(res.Skipped) > 0 {
			msg += ": " + res.Skipped[0]
		}
		return nil, Info{}, res.Skipped, fmt.Errorf("%s", msg)
	}
	for i := range res.Nodes {
		res.Nodes[i].Sub = s.ID
	}
	return res.Nodes, parseUserInfo(hdr.Get("Subscription-Userinfo")), res.Skipped, nil
}

// Merge replaces the nodes of subscription id with fresh ones, keeping the
// delays already measured for nodes that did not change.
func Merge(st *state.State, id string, fresh []state.Node) {
	old := map[string]state.Node{}
	kept := st.Nodes[:0]
	for _, n := range st.Nodes {
		if n.Sub == id {
			old[n.ID] = n
			continue
		}
		kept = append(kept, n)
	}
	have := map[string]bool{}
	for _, n := range kept {
		have[n.ID] = true
	}
	for _, n := range fresh {
		if have[n.ID] {
			continue // the same server already added by hand or by another sub
		}
		have[n.ID] = true
		if o, ok := old[n.ID]; ok {
			n.Delay, n.TestedAt = o.Delay, o.TestedAt
		}
		kept = append(kept, n)
	}
	st.Nodes = kept
}

// Due reports whether a subscription should be refreshed now.
func Due(s state.Subscription, now time.Time) bool {
	if !s.Enabled || s.Interval <= 0 {
		return false
	}
	return now.Sub(time.Unix(s.UpdatedAt, 0)) >= time.Duration(s.Interval)*time.Hour
}
