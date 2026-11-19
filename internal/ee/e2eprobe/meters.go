package e2eprobe

import "github.com/flexprice/go-sdk/v2/models/types"

const (
	PlanLookupKey     = "e2eprobe_plan"
	SharedCouponCode  = "E2EPROBE_COUPON_10PCT"
	SharedTaxRateCode = "E2EPROBE_TAX_10PCT"
)

// Meter is one metered feature the probe provisions; the key names it in
// event profiles and the suite. Lookup keys and event names keep the legacy
// shape so existing tenants are reused rather than re-seeded.
type Meter struct {
	Key         string
	LookupKey   string
	EventName   string
	DisplayName string
	Aggregation types.AggregationType
	Field       string
	Multiplier  string

	FilterKey    string
	FilterValues []string

	// MeterBucket windows the meter itself (grandfathered); PriceBucket windows
	// the price. The server rejects both on one pair.
	MeterBucket types.WindowSize
	PriceBucket types.WindowSize

	// NoEntitlement keeps the plan's soft-limit allowance off this meter so
	// billed usage is exactly units × price.
	NoEntitlement bool
}

var meters = []Meter{
	{Key: "count", DisplayName: "E2EProbe Count", Aggregation: types.AggregationTypeCount},
	{Key: "sum", DisplayName: "E2EProbe Sum", Aggregation: types.AggregationTypeSum, Field: "amount"},
	{Key: "avg", DisplayName: "E2EProbe Avg", Aggregation: types.AggregationTypeAvg, Field: "amount"},
	{Key: "count_unique", DisplayName: "E2EProbe CountUnique", Aggregation: types.AggregationTypeCountUnique, Field: "user_id"},
	{Key: "latest", DisplayName: "E2EProbe Latest", Aggregation: types.AggregationTypeLatest, Field: "amount"},
	{Key: "max", DisplayName: "E2EProbe Max", Aggregation: types.AggregationTypeMax, Field: "amount", MeterBucket: types.WindowSizeHour},
	{Key: "sum_multiplier", DisplayName: "E2EProbe SumMul", Aggregation: types.AggregationTypeSumWithMultiplier, Field: "amount", Multiplier: "1000"},
	{Key: "sum_filtered", DisplayName: "E2EProbe Sum (api-only)", Aggregation: types.AggregationTypeSum, Field: "amount", FilterKey: "source", FilterValues: []string{"api"}},
	{Key: "max_15min", DisplayName: "E2EProbe Max 15min", Aggregation: types.AggregationTypeMax, Field: "amount", MeterBucket: types.WindowSizeFifteenMin},
	{Key: "sum_hour", DisplayName: "E2EProbe Sum Hour", Aggregation: types.AggregationTypeSum, Field: "amount", MeterBucket: types.WindowSizeHour},
	{Key: "max_day", DisplayName: "E2EProbe Max Day", Aggregation: types.AggregationTypeMax, Field: "amount", MeterBucket: types.WindowSizeDay},
	{Key: "max_price_hour", DisplayName: "E2EProbe Max (price bucket)", Aggregation: types.AggregationTypeMax, Field: "amount", PriceBucket: types.WindowSizeHour},
	{Key: "sum_price_hour", DisplayName: "E2EProbe Sum (price bucket)", Aggregation: types.AggregationTypeSum, Field: "amount", PriceBucket: types.WindowSizeHour},
	{Key: "sum_commit", DisplayName: "E2EProbe Sum (commitment)", Aggregation: types.AggregationTypeSum, Field: "amount", NoEntitlement: true},
}

func init() {
	for i := range meters {
		meters[i].LookupKey = "e2eprobe_" + meters[i].Key + "_feature"
		meters[i].EventName = "e2eprobe_" + meters[i].Key
	}
}

// Meters lists the catalog in provisioning order.
func Meters() []Meter {
	return append([]Meter(nil), meters...)
}

func MeterByKey(key string) (Meter, bool) {
	for _, m := range meters {
		if m.Key == key {
			return m, true
		}
	}
	return Meter{}, false
}

func MeterByEventName(name string) (Meter, bool) {
	for _, m := range meters {
		if m.EventName == name {
			return m, true
		}
	}
	return Meter{}, false
}

func (m Meter) Bucketed() bool {
	return m.Bucket() != ""
}

// Bucket is the window the meter bills in, from whichever side defines it.
func (m Meter) Bucket() types.WindowSize {
	if m.MeterBucket != "" {
		return m.MeterBucket
	}
	return m.PriceBucket
}
