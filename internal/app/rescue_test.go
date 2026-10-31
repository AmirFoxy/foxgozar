package app

import "testing"

func TestRescueCombos(t *testing.T) {
	for _, cur := range []rescueCombo{{false, false}, {true, false}, {false, true}, {true, true}} {
		got := rescueCombos(cur)
		if len(got) != 3 {
			t.Fatalf("%v: %v", cur, got)
		}
		seen := map[rescueCombo]bool{cur: true}
		for _, c := range got {
			if seen[c] {
				t.Fatalf("%v: %v repeats a combination or the current one", cur, got)
			}
			seen[c] = true
		}
		// Closest first: one switch changed.
		if d := diff(cur, got[0]); d != 1 {
			t.Fatalf("%v: first try changes %d switches", cur, d)
		}
	}
}

func diff(a, b rescueCombo) int {
	n := 0
	if a.Fragment != b.Fragment {
		n++
	}
	if a.Mux != b.Mux {
		n++
	}
	return n
}
