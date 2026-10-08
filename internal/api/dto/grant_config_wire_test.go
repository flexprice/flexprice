package dto

import (
	"encoding/json"
	"testing"

	"github.com/shopspring/decimal"
)

// Anonymous embedding flattens in JSON, so the wire format must be byte-identical to
// the version that listed the grant fields inline.
func TestGrantConfigEmbedsFlat(t *testing.T) {
	q := decimal.NewFromInt(1000)
	v := 2
	b, err := json.Marshal(&AggregatedEntitlementBucket{
		EntitlementID:  "ent_1",
		SourceEntityID: "plan_1",
		GrantConfig:    GrantConfig{GrantMeasure: "quantity", GrantQuota: &q, GrantDurationValue: &v, GrantDurationUnit: "hour"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"entitlement_id":"ent_1","source_entity_id":"plan_1","grant_measure":"quantity","grant_quota":"1000","grant_duration_value":2,"grant_duration_unit":"hour"}`
	if string(b) != want {
		t.Fatalf("wire format changed\n got: %s\nwant: %s", b, want)
	}
}

// A bucket with no ceiling has no quota either, so without the flag the two are
// indistinguishable on the wire.
func TestGrantConfigUnlimitedBucket(t *testing.T) {
	b, err := json.Marshal(&AggregatedEntitlementBucket{
		EntitlementID: "ent_1",
		GrantConfig:   GrantConfig{GrantMeasure: "quantity", GrantUnlimited: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"entitlement_id":"ent_1","source_entity_id":"","grant_measure":"quantity","grant_unlimited":true}`
	if string(b) != want {
		t.Fatalf("got: %s\nwant: %s", b, want)
	}
}
