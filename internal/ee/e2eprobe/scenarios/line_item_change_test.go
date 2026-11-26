package scenarios

import (
	"fmt"
	"testing"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/probetest"
	"github.com/flexprice/go-sdk/v2/models/types"
)

func TestLineItemChange_UsesSumCommitPrice(t *testing.T) {
	d := newTestDeps(t)
	r := run(t, d, newLineItemChange(d.Deps), "baseline-a")
	if r.Status != e2eprobe.StatusPass {
		t.Fatalf("result = %+v", r)
	}
	want := d.World.Fixtures().PriceID("sum_commit")
	if want == "" {
		t.Fatal("fixtures must index the sum_commit price")
	}
	if len(d.fc.Subscription.LineItems) != 1 {
		t.Fatalf("line item requests = %+v", d.fc.Subscription.LineItems)
	}
	req := d.fc.Subscription.LineItems[0]
	if req.PriceID == nil || *req.PriceID != want || req.Price != nil {
		t.Errorf("request = %+v, want price_id %s", req, want)
	}
	if req.StartDate == nil || !req.StartDate.Equal(d.now) {
		t.Errorf("start_date = %v, want %v", req.StartDate, d.now)
	}
	sub := d.fc.Subscription.Items[r.Attrs["ephemeral_subscription_id"]]
	if planPrices := len(d.fc.Price.Items); len(sub.LineItems) != planPrices+1 {
		t.Errorf("clone has %d line items, want %d", len(sub.LineItems), planPrices+1)
	}
}

func TestLineItemChange_MismatchWhenNothingAdded(t *testing.T) {
	d := newTestDeps(t)
	d.fc.Subscription.CreateLineItemFn = func(string, types.CreateSubscriptionLineItemRequest) error { return nil }
	r := run(t, d, newLineItemChange(d.Deps), "baseline-a")
	if r.Status != e2eprobe.StatusFail || r.Step != "assert_line_item" || r.Infra {
		t.Fatalf("result = %+v", r)
	}
	n := len(d.fc.Price.Items)
	if want := fmt.Sprintf("line items: want %d, got %d", n+1, n); len(r.Mismatches) != 1 || r.Mismatches[0] != want {
		t.Errorf("mismatches = %v, want %q", r.Mismatches, want)
	}
}

func TestLineItemChange_CreateErrorIsInfra(t *testing.T) {
	d := newTestDeps(t)
	d.fc.Subscription.CreateLineItemFn = func(string, types.CreateSubscriptionLineItemRequest) error {
		return probetest.ServerError(502)
	}
	r := run(t, d, newLineItemChange(d.Deps), "baseline-a")
	if r.Status != e2eprobe.StatusFail || r.Step != "create_line_item" || !r.Infra {
		t.Fatalf("result = %+v", r)
	}
	if r.Attrs["ephemeral_subscription_id"] == "" {
		t.Errorf("a failure must still name the clone: %v", r.Attrs)
	}
}
