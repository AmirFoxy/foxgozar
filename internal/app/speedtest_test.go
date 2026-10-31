package app

import "testing"

func TestJudgeSetup(t *testing.T) {
	// Server 2 on the owner's router, two runs: some, then most setups slow.
	for _, setup := range [][]int{
		{3139, 6080, 599, 750, 5410, 599, 710, 599},
		{4426, 4960, 880, 842, 5979, 5897, 868, 5596}, // slow ones are the majority
	} {
		if _, slow, bad := judgeSetup(setup); slow < 3 || !bad {
			t.Fatalf("%v: slow %d unstable %v", setup, slow, bad)
		}
	}
	// Server 1: steady, one setup a little slower (the first: DNS).
	if best, slow, bad := judgeSetup([]int{1182, 863, 829, 821, 833, 861, 834, 815}); best != 815 || slow != 0 || bad {
		t.Fatalf("steady server: best %d slow %d unstable %v", best, slow, bad)
	}
	// One hiccup is not instability.
	if _, slow, bad := judgeSetup([]int{600, 600, 2900, 600, 600, 600, 600, 600}); slow != 1 || bad {
		t.Fatalf("one-off: slow %d unstable %v", slow, bad)
	}
	// Failures count as slow.
	if _, slow, bad := judgeSetup([]int{-1, -1, 600, 600}); slow != 2 || !bad {
		t.Fatalf("failures: slow %d unstable %v", slow, bad)
	}
	if _, _, bad := judgeSetup([]int{-1, -1}); !bad {
		t.Fatal("nothing worked: unstable")
	}
}

func TestSummarizePing(t *testing.T) {
	// Steady: min 90, small jitter, P90 close to min.
	s := summarize([]int{91, 90, 92, 91, 90, 93, 91, 90, 92, 91})
	if s.Min != 90 || s.P90 != 92 || s.Jitter > 3 {
		t.Fatalf("steady: %+v", s)
	}
	// Same best ping, but spikes: P90 shows them, the minimum does not.
	s = summarize([]int{90, 91, 300, 90, 92, 250, 91, 90, 280, 91})
	if s.Min != 90 || s.P90 < 250 || s.Jitter < 50 {
		t.Fatalf("spiky: %+v", s)
	}
	// One lost round trip in ten: still a number.
	if s := summarize([]int{90, 90, -1, 90, 90, 90, 90, 90, 90, 90}); s.P90 != 90 {
		t.Fatalf("one lost: %+v", s)
	}
	// Two lost: no honest 90th percentile.
	if s := summarize([]int{90, -1, 90, -1, 90, 90, 90, 90, 90, 90}); s.P90 != -1 || s.Min != 90 {
		t.Fatalf("two lost: %+v", s)
	}
	if s := summarize([]int{-1, -1}); s.Min != -1 {
		t.Fatalf("all lost: %+v", s)
	}
}
