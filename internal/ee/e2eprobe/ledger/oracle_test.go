package ledger

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/shopspring/decimal"
)

func sixTicks() []time.Time {
	start := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	var out []time.Time
	for i := 1; i <= 6; i++ {
		out = append(out, start.Add(time.Duration(i)*tick))
	}
	return out
}

func TestOracle_Expect(t *testing.T) {
	standard, _ := Lookup("standard")
	bucketed, _ := Lookup("bucketed")
	cases := []struct {
		profile Profile
		meter   string
		want    string
		events  int
	}{
		{standard, "count", "30", 30},
		{standard, "sum", "180", 18},
		{standard, "avg", "6", 12},
		{standard, "max", "7", 12},
		{standard, "count_unique", "3", 24},
		{standard, "latest", "9", 12},
		{standard, "sum_multiplier", "60000", 12},
		{standard, "sum_filtered", "48", 18},
		{standard, "max_price_hour", "7", 12},
		{standard, "sum_price_hour", "60", 12},
		{bucketed, "max_15min", "28", 12},
		{bucketed, "sum_hour", "60", 12},
		{bucketed, "max_day", "7", 12},
		{standard, "sum_commit", "0", 0},
	}
	for _, tc := range cases {
		m, ok := e2eprobe.MeterByKey(tc.meter)
		if !ok {
			t.Fatalf("meter %s", tc.meter)
		}
		o := NewOracle(tc.profile, tick)
		got := o.Expect(m, sixTicks())
		if !got.Equal(decimal.RequireFromString(tc.want)) {
			t.Errorf("%s/%s: Expect = %s, want %s", tc.profile.Name, tc.meter, got, tc.want)
		}
		if n := o.Events(m, sixTicks()); n != tc.events {
			t.Errorf("%s/%s: Events = %d, want %d", tc.profile.Name, tc.meter, n, tc.events)
		}
	}
}

func TestOracle_BucketedMaxCountsEachBucketOnce(t *testing.T) {
	bucketed, _ := Lookup("bucketed")
	m, _ := e2eprobe.MeterByKey("max_15min")
	o := NewOracle(bucketed, tick)
	one := sixTicks()[:1]
	if got := o.Expect(m, one); !got.Equal(decimal.NewFromInt(7)) {
		t.Errorf("one tick inside one quarter hour = %s, want 7", got)
	}
	day, _ := e2eprobe.MeterByKey("max_day")
	if got := o.Expect(day, sixTicks()); !got.Equal(decimal.NewFromInt(7)) {
		t.Errorf("six ticks inside one day = %s, want 7", got)
	}
}
