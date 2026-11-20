package scenario

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/shopspring/decimal"
)

func TestContext_CollectsMismatchesAndAttrs(t *testing.T) {
	deps, _ := newDeps(t, true)
	taxed, _ := deps.World.Persona("taxed")
	sc := NewContext(deps, taxed, "usage")
	sc.Money("sum cost", decimal.RequireFromString("1.00"), decimal.RequireFromString("1.005"))
	sc.Money("tax", decimal.RequireFromString("1.00"), decimal.RequireFromString("1.02"))
	sc.Quantity("count", decimal.NewFromInt(30), decimal.NewFromInt(30))
	sc.Count("line items", 3, 2)
	sc.Truth("subscription active", true)
	sc.Attr("subscription_id", "sub_1")
	r := sc.Result("assert_usage")
	if r.Status != e2eprobe.StatusFail || r.Step != "assert_usage" || len(r.Mismatches) != 2 {
		t.Fatalf("result = %+v", r)
	}
	if r.Mismatches[0] != "tax: want 1.00, got 1.02" || r.Mismatches[1] != "line items: want 3, got 2" {
		t.Errorf("mismatch text = %v", r.Mismatches)
	}
	if r.Attrs["persona"] != "taxed" || r.Attrs["external_customer_id"] != "e2eprobe-cust-persistent-0" || r.Attrs["scenario"] != "usage" || r.Attrs["subscription_id"] != "sub_1" {
		t.Errorf("attrs = %v", r.Attrs)
	}
	if sc.Persona() != taxed || sc.Client() != deps.Client || sc.World() != deps.World || sc.Ledger() != deps.Ledger || sc.Inbox() != deps.Inbox || sc.Suite() != deps.Suite || sc.RunID() != "run-test" || sc.Tick() != deps.Tick || !sc.Now().Equal(testNow) || sc.Scenario() != "usage" || sc.Logger() == nil {
		t.Error("getters must hand back the deps")
	}

	clean := NewContext(deps, taxed, "usage")
	clean.Truth("ok", true)
	if r := clean.Result("assert"); r.Status != e2eprobe.StatusPass || r.Attrs["persona"] != "taxed" {
		t.Errorf("no mismatches must pass with attrs, got %+v", r)
	}
}

func TestContext_SkipAndFailCarryPersonaAttrs(t *testing.T) {
	deps, _ := newDeps(t, true)
	taxed, _ := deps.World.Persona("taxed")
	sc := NewContext(deps, taxed, "usage")
	sc.Attr("window", "10:00-11:00")
	skip := sc.Skip("window", "partial hour")
	if skip.Status != e2eprobe.StatusSkip || skip.Reason != "partial hour" || skip.Attrs["persona"] != "taxed" || skip.Attrs["window"] != "10:00-11:00" {
		t.Errorf("skip = %+v", skip)
	}
	fail := sc.Fail("get", errors.New("boom"))
	if fail.Status != e2eprobe.StatusFail || fail.Reason != "boom" || fail.Attrs["scenario"] != "usage" || fail.Attrs["external_customer_id"] == "" {
		t.Errorf("fail = %+v", fail)
	}
	tenant := NewContext(deps, nil, "preflight")
	if r := tenant.Result("x"); r.Attrs["persona"] != "" || r.Attrs["scenario"] != "preflight" {
		t.Errorf("tenant-level attrs = %v", r.Attrs)
	}
	if tenant.Persona() != nil {
		t.Error("tenant-level context has no persona")
	}
}

func TestContext_CloneUsesPersonaAndScenario(t *testing.T) {
	deps, fc := newDeps(t, true)
	taxed, _ := deps.World.Persona("taxed")
	sc := NewContext(deps, taxed, "tax-preview")
	eph, err := sc.Clone(context.Background())
	if err != nil || eph.Scenario() != "tax-preview" {
		t.Fatalf("clone: %+v %v", eph, err)
	}
	if fc.Customer.Items[eph.ExternalID()].Metadata["e2eprobe_persona"] != "taxed" {
		t.Error("clone must be stamped from the context's persona")
	}
	if _, err := NewContext(deps, nil, "preflight").Clone(context.Background()); err == nil {
		t.Error("a tenant-level context cannot clone")
	}
}
