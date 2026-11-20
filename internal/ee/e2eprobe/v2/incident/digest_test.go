package incident

import (
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
)

func TestFormatDigest_ListsOpenAndSkipping(t *testing.T) {
	now := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	d := Digest{
		GeneratedAt: now,
		Open:        []Incident{{Key: Key{Scenario: "usage", Persona: "taxed"}, Kind: KindFailure, OpenedAt: now.Add(-3 * time.Hour), Summary: "assert: sum: want 30, got 20", Runs: 12}},
		Bindings: []BindingState{
			{Key: Key{Scenario: "lifecycle", Persona: "baseline-a"}, Runs: 10, Passes: 0, SkippingSince: now.Add(-2 * time.Hour), Last: e2eprobe.Skip("persona", "not provisioned")},
			{Key: Key{Scenario: "usage", Persona: "taxed"}, Runs: 20, Passes: 8},
			{Key: Key{Scenario: "wallet-debit", Persona: "wallet-a"}, Runs: 5, Passes: 5},
		},
		Extra: map[string]string{"inbox.total": "42", "events/day": "28080"},
	}
	out := FormatDigest(d)
	for _, want := range []string{"e2eprobe digest", "2026-11-19", "Open incidents (1)", "usage · taxed", "open for 3h", "Skipping (1)", "lifecycle · baseline-a", "not provisioned", "3 bindings · 35 runs · 13 passed", "events/day: 28080", "inbox.total: 42"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
