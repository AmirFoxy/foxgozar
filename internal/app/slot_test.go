package app

import (
	"context"
	"testing"
	"time"

	"foxgozar/internal/state"
)

func TestPickISPDNS(t *testing.T) {
	one := "# Interface wan\nnameserver 192.168.100.1\nnameserver fe80::1\n"
	if got := pickISPDNS(one); got != "udp://192.168.100.1" {
		t.Errorf("one provider: %q", got)
	}
	// Starlink on wan, the domestic line on wan2: Iranian sites ask wan2.
	two := "# Interface wan\nnameserver 100.64.0.1\n# Interface wan2\nnameserver 2001:db8::1\nnameserver 10.0.0.1\n"
	if got := pickISPDNS(two); got != "udp://10.0.0.1" {
		t.Errorf("dual WAN: %q", got)
	}
	if got := pickISPDNS("# nothing\n"); got != "" {
		t.Errorf("none: %q", got)
	}
}

func TestCustomGamePrefix(t *testing.T) {
	for _, g := range Games {
		if len(g.ID) >= len(CustomGamePrefix) && g.ID[:len(CustomGamePrefix)] == CustomGamePrefix {
			t.Fatalf("built-in game %s starts with the custom prefix", g.ID)
		}
	}
}

func TestPruneMonths(t *testing.T) {
	u := map[string]map[string][2]int64{}
	for _, m := range []string{"2025-10", "2025-11", "2025-12", "2026-01", "2026-02"} {
		u[m] = map[string][2]int64{"n": {1, 2}}
	}
	pruneMonths(u, 3)
	if len(u) != 3 || u["2025-10"] != nil || u["2025-11"] != nil || u["2026-02"] == nil {
		t.Fatalf("kept %v", u)
	}
}

// LastGames once deadlocked: a deferred call took a.mu while the deferred
// Unlock had not run yet, and every API call after it hung.
func TestLastGamesReturns(t *testing.T) {
	st, err := state.Open(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatal(err)
	}
	a := New(Paths{Etc: t.TempDir(), Bin: t.TempDir(), Run: t.TempDir()}, st)
	done := make(chan struct{})
	go func() {
		a.LastGames()
		a.LastGames()
		a.Status(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("LastGames did not return: a lock is held twice")
	}
}
