package checks

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/synthetic"
	"github.com/flexprice/go-sdk/v2/models/dtos"
)

func TestNewCustomerLifecycle_Happy(t *testing.T) {
	// Swap extractSubscriptionID to read the sub ID from the fake's response wrapper.
	origExtract := extractSubscriptionID
	extractSubscriptionID = func(resp interface{}) string {
		r, ok := resp.(*dtos.CreateSubscriptionResponse)
		if !ok || r == nil || r.DtoSubscriptionResponse == nil || r.DtoSubscriptionResponse.ID == nil {
			return ""
		}
		return *r.DtoSubscriptionResponse.ID
	}
	t.Cleanup(func() { extractSubscriptionID = origExtract })

	fc := newFakeClient()
	reg := synthetic.NewRegistry()
	reg.LoadSeeds(synthetic.Seeds{PlanIDs: []string{"plan_seed"}})
	s := NewNewCustomerLifecycle(fc, reg, "run-1", NewCustomerLifecycleOpts{
		MaxEphemerals: 20,
		AnalyticsPoll: NewCustomerLifecyclePoll{Timeout: 30 * time.Millisecond, Interval: 5 * time.Millisecond},
	})
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(fc.customers.created) != 1 || len(fc.subs.created) != 1 || len(fc.events.ingested) != 3 {
		t.Errorf("counts: cust=%d sub=%d ev=%d", len(fc.customers.created), len(fc.subs.created), len(fc.events.ingested))
	}
	if len(reg.Ephemerals("customer")) != 1 || len(reg.Ephemerals("subscription")) != 1 {
		t.Errorf("ephemerals: cust=%d sub=%d", len(reg.Ephemerals("customer")), len(reg.Ephemerals("subscription")))
	}
	subEph := reg.Ephemerals("subscription")[0]
	if subEph.ID == "" {
		t.Error("ephemeral sub ID empty")
	}
}

func TestNewCustomerLifecycle_NoPlanSeedsIsNoOp(t *testing.T) {
	fc := newFakeClient()
	s := NewNewCustomerLifecycle(fc, synthetic.NewRegistry(), "run-1", NewCustomerLifecycleOpts{MaxEphemerals: 20})
	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fc.customers.created) != 0 {
		t.Errorf("expected no customer creates")
	}
}

func TestNewCustomerLifecycle_RespectsCap(t *testing.T) {
	fc := newFakeClient()
	reg := synthetic.NewRegistry()
	reg.LoadSeeds(synthetic.Seeds{PlanIDs: []string{"plan_seed"}})
	for i := 0; i < 20; i++ {
		reg.RegisterEphemeral("customer", "old", time.Now())
	}
	s := NewNewCustomerLifecycle(fc, reg, "run-1", NewCustomerLifecycleOpts{MaxEphemerals: 20})
	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fc.customers.created) != 0 {
		t.Errorf("expected skip but created %d", len(fc.customers.created))
	}
}
