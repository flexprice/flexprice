package e2eprobe

import (
	"testing"

	"github.com/flexprice/go-sdk/v2/models/types"
)

func TestMeters_KeysUniqueAndLookupKeysMatchLegacy(t *testing.T) {
	meters := Meters()
	if len(meters) != 14 {
		t.Fatalf("expected 14 meters, got %d", len(meters))
	}
	seen := map[string]bool{}
	for _, m := range meters {
		if seen[m.Key] {
			t.Errorf("duplicate key %q", m.Key)
		}
		seen[m.Key] = true
		if m.LookupKey != "e2eprobe_"+m.Key+"_feature" {
			t.Errorf("%s: lookup key %q breaks the legacy naming the tenant already carries", m.Key, m.LookupKey)
		}
		if m.EventName != "e2eprobe_"+m.Key {
			t.Errorf("%s: event name %q", m.Key, m.EventName)
		}
		if _, ok := MeterByKey(m.Key); !ok {
			t.Errorf("MeterByKey(%q) missing", m.Key)
		}
		if got, ok := MeterByEventName(m.EventName); !ok || got.Key != m.Key {
			t.Errorf("MeterByEventName(%q) = %+v", m.EventName, got)
		}
	}
	if _, ok := MeterByKey("nope"); ok {
		t.Error("unknown key must not resolve")
	}
}

func TestMeter_Bucket(t *testing.T) {
	cases := []struct {
		key    string
		bucket types.WindowSize
	}{
		{"max", types.WindowSizeHour},
		{"max_price_hour", types.WindowSizeHour},
		{"sum_price_hour", types.WindowSizeHour},
		{"max_15min", types.WindowSizeFifteenMin},
		{"sum_hour", types.WindowSizeHour},
		{"max_day", types.WindowSizeDay},
		{"sum", ""},
		{"count", ""},
	}
	for _, tc := range cases {
		m, ok := MeterByKey(tc.key)
		if !ok {
			t.Fatalf("missing meter %s", tc.key)
		}
		if m.Bucket() != tc.bucket || m.Bucketed() != (tc.bucket != "") {
			t.Errorf("%s: bucket = %q bucketed = %v, want %q", tc.key, m.Bucket(), m.Bucketed(), tc.bucket)
		}
	}
	commit, _ := MeterByKey("sum_commit")
	if !commit.NoEntitlement {
		t.Error("sum_commit must carry no entitlement so billed usage equals units × price")
	}
	mul, _ := MeterByKey("sum_multiplier")
	if mul.Multiplier != "1000" || mul.Aggregation != types.AggregationTypeSumWithMultiplier {
		t.Errorf("sum_multiplier = %+v", mul)
	}
	filtered, _ := MeterByKey("sum_filtered")
	if filtered.FilterKey != "source" || len(filtered.FilterValues) != 1 || filtered.FilterValues[0] != "api" {
		t.Errorf("sum_filtered = %+v", filtered)
	}
}
