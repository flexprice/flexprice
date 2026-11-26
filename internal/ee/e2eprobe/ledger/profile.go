// Package ledger schedules the probe's own usage and predicts what Flexprice
// must report for it: every batch is a pure function of the profile, the
// persona and the tick, so expected totals are computable, not recorded.
package ledger

import "sort"

// Event is one event template within a tick. At is the position inside the
// tick as a fraction, so the schedule is valid for any tick length.
type Event struct {
	Meter string
	At    float64
	Props map[string]string
}

// Profile is the batch a persona sends every tick.
type Profile struct {
	Name   string
	Events []Event
}

func (p Profile) Size() int {
	return len(p.Events)
}

// Meters lists the profile's meter keys in catalog order.
func (p Profile) Meters() []string {
	seen := map[string]bool{}
	var out []string
	for _, ev := range p.Events {
		if !seen[ev.Meter] {
			seen[ev.Meter] = true
			out = append(out, ev.Meter)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return meterOrder[out[i]] < meterOrder[out[j]] })
	return out
}

var meterOrder = map[string]int{
	"count": 0, "sum": 1, "avg": 2, "count_unique": 3, "latest": 4, "max": 5, "sum_multiplier": 6,
	"sum_filtered": 7, "max_15min": 8, "sum_hour": 9, "max_day": 10, "max_price_hour": 11, "sum_price_hour": 12, "sum_commit": 13,
}

func amount(v string) map[string]string { return map[string]string{"amount": v} }

var profiles = map[string]Profile{
	"standard": {Name: "standard", Events: []Event{
		{Meter: "count", At: 0.10}, {Meter: "count", At: 0.20}, {Meter: "count", At: 0.30}, {Meter: "count", At: 0.40}, {Meter: "count", At: 0.50},
		{Meter: "sum", At: 0.15, Props: amount("10")}, {Meter: "sum", At: 0.35, Props: amount("10")}, {Meter: "sum", At: 0.55, Props: amount("10")},
		{Meter: "avg", At: 0.25, Props: amount("4")}, {Meter: "avg", At: 0.65, Props: amount("8")},
		{Meter: "max", At: 0.30, Props: amount("3")}, {Meter: "max", At: 0.70, Props: amount("7")},
		{Meter: "count_unique", At: 0.12, Props: map[string]string{"user_id": "u1"}}, {Meter: "count_unique", At: 0.22, Props: map[string]string{"user_id": "u1"}},
		{Meter: "count_unique", At: 0.32, Props: map[string]string{"user_id": "u2"}}, {Meter: "count_unique", At: 0.42, Props: map[string]string{"user_id": "u3"}},
		{Meter: "latest", At: 0.20, Props: amount("1")}, {Meter: "latest", At: 0.80, Props: amount("9")},
		{Meter: "sum_multiplier", At: 0.45, Props: amount("5")}, {Meter: "sum_multiplier", At: 0.85, Props: amount("5")},
		{Meter: "sum_filtered", At: 0.33, Props: map[string]string{"amount": "4", "source": "api"}},
		{Meter: "sum_filtered", At: 0.53, Props: map[string]string{"amount": "4", "source": "api"}},
		{Meter: "sum_filtered", At: 0.73, Props: map[string]string{"amount": "4", "source": "web"}},
		{Meter: "max_price_hour", At: 0.36, Props: amount("3")}, {Meter: "max_price_hour", At: 0.76, Props: amount("7")},
		{Meter: "sum_price_hour", At: 0.48, Props: amount("5")}, {Meter: "sum_price_hour", At: 0.88, Props: amount("5")},
	}},
	"bucketed": {Name: "bucketed", Events: []Event{
		{Meter: "max_15min", At: 0.10, Props: amount("3")}, {Meter: "max_15min", At: 0.70, Props: amount("7")},
		{Meter: "sum_hour", At: 0.40, Props: amount("5")}, {Meter: "sum_hour", At: 0.60, Props: amount("5")},
		{Meter: "max_day", At: 0.30, Props: amount("3")}, {Meter: "max_day", At: 0.90, Props: amount("7")},
	}},
}

func Profiles() map[string]Profile {
	out := make(map[string]Profile, len(profiles))
	for k, v := range profiles {
		out[k] = v
	}
	return out
}

func Lookup(name string) (Profile, bool) {
	p, ok := profiles[name]
	return p, ok
}
