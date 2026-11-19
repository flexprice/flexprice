package ledger

import (
	"testing"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
)

func TestProfiles_SizesAndOffsetsUnique(t *testing.T) {
	standard, ok := Lookup("standard")
	if !ok || standard.Size() != 27 {
		t.Fatalf("standard profile: ok=%v size=%d", ok, standard.Size())
	}
	bucketed, ok := Lookup("bucketed")
	if !ok || bucketed.Size() != 6 {
		t.Fatalf("bucketed profile: ok=%v size=%d", ok, bucketed.Size())
	}
	if _, ok := Lookup("firehose"); ok {
		t.Error("unknown profile must not resolve")
	}
	for name, p := range Profiles() {
		seen := map[string]map[float64]bool{}
		for _, ev := range p.Events {
			if _, ok := e2eprobe.MeterByKey(ev.Meter); !ok {
				t.Errorf("%s: unknown meter %q", name, ev.Meter)
			}
			if ev.At <= 0 || ev.At >= 1 {
				t.Errorf("%s: offset %v must be inside the tick", name, ev.At)
			}
			if seen[ev.Meter] == nil {
				seen[ev.Meter] = map[float64]bool{}
			}
			if seen[ev.Meter][ev.At] {
				t.Errorf("%s: meter %s has two events at offset %v", name, ev.Meter, ev.At)
			}
			seen[ev.Meter][ev.At] = true
		}
	}
	if keys := standard.Meters(); len(keys) != 10 || keys[0] != "count" || keys[len(keys)-1] != "sum_price_hour" {
		t.Errorf("standard meters = %v", keys)
	}
	if keys := bucketed.Meters(); len(keys) != 3 {
		t.Errorf("bucketed meters = %v", keys)
	}
}
