package scenarios

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/scenario"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/world"
	"github.com/flexprice/go-sdk/v2/models/types"
)

// cancellation retires the oldest clone past the grace period: it cancels the
// clone's subscriptions with a final invoice, deletes the customer and drops the
// registry entries, leaving anything that resists to the janitor.
type cancellation struct {
	minAge time.Duration
	poll   pollWindow
}

func newCancellation(scenario.Deps) *cancellation {
	return &cancellation{minAge: 20 * time.Minute, poll: pollWindow{every: 5 * time.Second, within: time.Minute}}
}

func (*cancellation) Name() string               { return "cancel" }
func (*cancellation) Kind() e2eprobe.Kind        { return e2eprobe.KindFlow }
func (*cancellation) Requires() []e2eprobe.Trait { return []e2eprobe.Trait{e2eprobe.TraitSubscription} }

func (s *cancellation) Run(ctx context.Context, sc *scenario.Context) e2eprobe.Result {
	reg := sc.World().Ephemerals()
	target, ok := oldestBefore(reg.List("customer"), sc.Now().Add(-s.minAge))
	if !ok {
		return sc.Skip("nothing_old_enough", fmt.Sprintf("no ephemeral customer older than %s", s.minAge))
	}
	ext := target.ID
	sc.Attr("ephemeral_external_id", ext)
	cust, err := sc.Client().Customers().GetByExternalID(ctx, ext)
	switch {
	case world.IsNotFound(err):
		reg.Archive("customer", ext)
		return sc.Skip("already_gone", "customer already removed; registry entry archived")
	case err != nil:
		return sc.Fail("get_customer", err)
	case cust.CustomerResponse == nil || cust.CustomerResponse.ID == nil:
		return sc.Fail("get_customer", errors.New("customer lookup returned no id"))
	}
	customerID := *cust.CustomerResponse.ID
	sc.Attr("ephemeral_customer_id", customerID)

	subs, err := sc.Client().Subscriptions().Query(ctx, types.SubscriptionFilter{CustomerID: &customerID})
	if err != nil {
		return sc.Fail("query_subscriptions", err)
	}
	var items []types.SubscriptionResponse
	if subs.ListSubscriptionsResponse != nil {
		items = subs.ListSubscriptionsResponse.Items
	}
	immediate := types.CancellationTypeImmediate
	policy := types.CancelImmediatelyInvoicePolicyGenerateInvoice
	reason := "e2eprobe cancel"
	req := types.CancelSubscriptionRequest{CancellationType: immediate, CancelImmediatelyInvoicePolicy: &policy, Reason: &reason}
	ids := make([]string, 0, len(items))
	allCancelled := true
	for _, sub := range items {
		if sub.ID == nil {
			continue
		}
		id := *sub.ID
		ids = append(ids, id)
		if sub.SubscriptionStatus != nil && *sub.SubscriptionStatus == types.SubscriptionStatusCancelled {
			continue
		}
		_, cancelErr := sc.Client().Subscriptions().Cancel(ctx, id, req)
		if world.IsNotFound(cancelErr) {
			continue
		}
		// A cancel that errored after taking effect still reads back as cancelled.
		pollErr := s.poll.until(ctx, func() error { return cancelled(ctx, sc, id) })
		switch {
		case pollErr == nil:
		case cancelErr != nil:
			return sc.Fail("cancel_subscription", cancelErr)
		case e2eprobe.IsInfra(pollErr):
			return sc.Fail("await_cancelled", pollErr)
		default:
			allCancelled = false
			sc.Truth(fmt.Sprintf("subscription %s reached cancelled (%v)", id, pollErr), false)
		}
	}
	if !allCancelled {
		return sc.Result("cancel")
	}
	if _, err := sc.Client().Customers().Delete(ctx, customerID); err != nil && !world.IsNotFound(err) {
		return sc.Fail("delete_customer", err)
	}
	reg.Archive("customer", ext)
	for _, id := range ids {
		reg.Archive("subscription", id)
	}
	sc.Attr("subscriptions", strconv.Itoa(len(ids)))
	return sc.Result("cancel")
}

// oldestBefore picks the earliest entry created at or before cutoff.
func oldestBefore(entries []world.Entry, cutoff time.Time) (world.Entry, bool) {
	var oldest world.Entry
	found := false
	for _, e := range entries {
		if e.CreatedAt.After(cutoff) {
			continue
		}
		if !found || e.CreatedAt.Before(oldest.CreatedAt) {
			oldest, found = e, true
		}
	}
	return oldest, found
}

// cancelled is nil once the subscription reads as cancelled or is gone.
func cancelled(ctx context.Context, sc *scenario.Context, id string) error {
	resp, err := sc.Client().Subscriptions().Get(ctx, id)
	if world.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if resp.SubscriptionResponse == nil || resp.SubscriptionResponse.SubscriptionStatus == nil {
		return errors.New("subscription read has no status")
	}
	if status := *resp.SubscriptionResponse.SubscriptionStatus; status != types.SubscriptionStatusCancelled {
		return fmt.Errorf("subscription %s is %s", id, status)
	}
	return nil
}
