package ledger

import (
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/go-sdk/v2/models/types"
	"github.com/shopspring/decimal"
)

// Oracle predicts a meter's aggregate over the ticks a persona actually sent.
type Oracle struct {
	profile  Profile
	interval time.Duration
}

func NewOracle(p Profile, interval time.Duration) Oracle {
	return Oracle{profile: p, interval: interval}
}

// events are the profile's templates for the meter that pass its filter.
func (o Oracle) events(m e2eprobe.Meter) []Event {
	var out []Event
	for _, ev := range o.profile.Events {
		if ev.Meter != m.Key {
			continue
		}
		if m.FilterKey != "" && !contains(m.FilterValues, ev.Props[m.FilterKey]) {
			continue
		}
		out = append(out, ev)
	}
	return out
}

// Events is how many events of the meter the ticks carried.
func (o Oracle) Events(m e2eprobe.Meter, ticks []time.Time) int {
	n := 0
	for _, ev := range o.profile.Events {
		if ev.Meter == m.Key {
			n++
		}
	}
	return n * len(ticks)
}

// Expect is the aggregate Flexprice must report for the meter over the ticks.
func (o Oracle) Expect(m e2eprobe.Meter, ticks []time.Time) decimal.Decimal {
	events := o.events(m)
	if len(events) == 0 || len(ticks) == 0 {
		return decimal.Zero
	}
	nTicks := decimal.NewFromInt(int64(len(ticks)))
	switch m.Aggregation {
	case types.AggregationTypeCount:
		return decimal.NewFromInt(int64(len(events))).Mul(nTicks)
	case types.AggregationTypeSum:
		return sumOf(events, m.Field).Mul(nTicks)
	case types.AggregationTypeSumWithMultiplier:
		return sumOf(events, m.Field).Mul(decimal.RequireFromString(m.Multiplier)).Mul(nTicks)
	case types.AggregationTypeAvg:
		return sumOf(events, m.Field).Div(decimal.NewFromInt(int64(len(events))))
	case types.AggregationTypeCountUnique:
		seen := map[string]bool{}
		for _, ev := range events {
			seen[ev.Props[m.Field]] = true
		}
		return decimal.NewFromInt(int64(len(seen)))
	case types.AggregationTypeLatest:
		latest := events[0]
		for _, ev := range events[1:] {
			if ev.At > latest.At {
				latest = ev
			}
		}
		return value(latest, m.Field)
	case types.AggregationTypeMax:
		if !m.Bucketed() {
			return maxOf(events, m.Field)
		}
		return o.bucketedMax(m, events, ticks)
	}
	return decimal.Zero
}

// bucketedMax sums, over every bucket the events touch, that bucket's maximum.
func (o Oracle) bucketedMax(m e2eprobe.Meter, events []Event, ticks []time.Time) decimal.Decimal {
	buckets := map[time.Time]decimal.Decimal{}
	for _, tickEnd := range ticks {
		for _, ev := range events {
			b := bucketStart(timestamp(ev, tickEnd, o.interval), m.Bucket())
			v := value(ev, m.Field)
			if cur, ok := buckets[b]; !ok || v.GreaterThan(cur) {
				buckets[b] = v
			}
		}
	}
	total := decimal.Zero
	for _, v := range buckets {
		total = total.Add(v)
	}
	return total
}

func bucketStart(ts time.Time, w types.WindowSize) time.Time {
	ts = ts.UTC()
	switch w {
	case types.WindowSizeMinute:
		return ts.Truncate(time.Minute)
	case types.WindowSizeFifteenMin:
		return ts.Truncate(15 * time.Minute)
	case types.WindowSizeThirtyMin:
		return ts.Truncate(30 * time.Minute)
	case types.WindowSizeHour:
		return ts.Truncate(time.Hour)
	case types.WindowSizeThreeHour:
		return ts.Truncate(3 * time.Hour)
	case types.WindowSizeSixHour:
		return ts.Truncate(6 * time.Hour)
	case types.WindowSizeTwelveHour:
		return ts.Truncate(12 * time.Hour)
	case types.WindowSizeDay:
		return ts.Truncate(24 * time.Hour)
	case types.WindowSizeWeek:
		return ts.Truncate(7 * 24 * time.Hour)
	case types.WindowSizeMonth:
		return time.Date(ts.Year(), ts.Month(), 1, 0, 0, 0, 0, time.UTC)
	}
	return ts
}

func value(ev Event, field string) decimal.Decimal {
	v, err := decimal.NewFromString(ev.Props[field])
	if err != nil {
		return decimal.Zero
	}
	return v
}

func sumOf(events []Event, field string) decimal.Decimal {
	total := decimal.Zero
	for _, ev := range events {
		total = total.Add(value(ev, field))
	}
	return total
}

func maxOf(events []Event, field string) decimal.Decimal {
	best := value(events[0], field)
	for _, ev := range events[1:] {
		if v := value(ev, field); v.GreaterThan(best) {
			best = v
		}
	}
	return best
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
