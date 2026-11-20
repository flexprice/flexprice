package scenarios

import (
	"context"
	"errors"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/scenario"
	"github.com/flexprice/go-sdk/v2/models/types"
)

// lineItemChange adds the plan's commitment usage price to a clone's
// subscription and expects exactly one more line item to show.
type lineItemChange struct{}

func newLineItemChange(scenario.Deps) *lineItemChange { return &lineItemChange{} }

func (*lineItemChange) Name() string        { return "line-item-change" }
func (*lineItemChange) Kind() e2eprobe.Kind { return e2eprobe.KindFlow }
func (*lineItemChange) Requires() []e2eprobe.Trait {
	return []e2eprobe.Trait{e2eprobe.TraitSubscription}
}

func (*lineItemChange) Run(ctx context.Context, sc *scenario.Context) e2eprobe.Result {
	priceID := sc.World().Fixtures().PriceID("sum_commit")
	if priceID == "" {
		return sc.Skip("fixtures", "plan has no sum_commit price yet")
	}
	eph, err := sc.Clone(ctx)
	if err != nil {
		return sc.Fail("clone", err)
	}
	noteClone(sc, eph)
	before, err := lineItems(ctx, sc, eph.SubscriptionID())
	if err != nil {
		return sc.Fail("get_subscription", err)
	}
	start := sc.Now().UTC()
	if _, err := sc.Client().Subscriptions().CreateLineItem(ctx, eph.SubscriptionID(), types.CreateSubscriptionLineItemRequest{
		PriceID:   &priceID,
		StartDate: &start,
		Metadata:  map[string]string{"e2eprobe": "true", "e2eprobe_run_id": sc.RunID()},
	}); err != nil {
		return sc.Fail("create_line_item", err)
	}
	after, err := lineItems(ctx, sc, eph.SubscriptionID())
	if err != nil {
		return sc.Fail("get_subscription", err)
	}
	sc.Count("line items", before+1, after)
	return sc.Result("assert_line_item")
}

func lineItems(ctx context.Context, sc *scenario.Context, subID string) (int, error) {
	resp, err := sc.Client().Subscriptions().Get(ctx, subID)
	if err != nil {
		return 0, err
	}
	if resp.SubscriptionResponse == nil {
		return 0, errors.New("subscription read has no body")
	}
	return len(resp.SubscriptionResponse.LineItems), nil
}
