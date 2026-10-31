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

func TestGamePort(t *testing.T) {
	for p, want := range map[int]bool{27015: true, 27200: true, 27201: false, 5000: true, 7500: true, 8181: true, 9050: true, 443: false, 53: false} {
		if gamePort(p) != want {
			t.Errorf("port %d: got %v", p, !want)
		}
	}
}

func TestSawGameServer(t *testing.T) {
	a := &App{}
	a.sawGameServer(5100, "185.40.65.10")    // LoL
	a.sawGameServer(27020, "155.133.248.10") // Valve: Dota 2 and CS2 share these ports
	a.sawGameServer(5100, "192.168.1.5")     // a LAN address is never a game server
	if len(a.gameSeen["lol"]) != 1 || len(a.gameSeen["dota2"]) != 1 || len(a.gameSeen["cs2"]) != 1 {
		t.Fatalf("seen: %v", a.gameSeen)
	}
	for i := 0; i < 10; i++ {
		a.sawGameServer(5100, "185.40.66."+string(rune('0'+i)))
	}
	if len(a.gameSeen["lol"]) != gameServersKept {
		t.Fatalf("kept %d, want %d", len(a.gameSeen["lol"]), gameServersKept)
	}
}
