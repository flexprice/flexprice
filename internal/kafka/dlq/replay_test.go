package dlq

import "testing"

// parseCount underpins the loop guard: a mis-parse that returns a low number
// would let a message replay forever, so pin the edge cases.
func TestParseCount(t *testing.T) {
	cases := map[string]int{
		"":     0, // never replayed
		"0":    0,
		"2":    2,
		"3":    3,
		"-1":   0, // malformed -> treat as unreplayed, guard still trips at MaxReplays
		"abc":  0,
		"  1 ": 0, // not trimmed on purpose; strconv rejects -> 0
	}
	for in, want := range cases {
		if got := parseCount(in); got != want {
			t.Errorf("parseCount(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("", 5); got != "(no reason)" {
		t.Errorf("empty reason = %q, want (no reason)", got)
	}
	if got := truncate("short", 80); got != "short" {
		t.Errorf("short = %q, want short", got)
	}
	if got := truncate("abcdef", 3); got != "abc" {
		t.Errorf("truncate len = %q, want abc", got)
	}
}
