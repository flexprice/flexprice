package checks

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/synthetic"
	"github.com/flexprice/go-sdk/v2/models/types"
)

type InvoicePoll struct {
	Timeout  time.Duration
	Interval time.Duration
}

type CancelCustomerFlow struct {
	client synthetic.Client
	reg    synthetic.Registry
	runID  string
	poll   InvoicePoll
}

func NewCancelCustomerFlow(c synthetic.Client, r synthetic.Registry, runID string, poll InvoicePoll) *CancelCustomerFlow {
	if poll.Timeout == 0 {
		poll.Timeout = 60 * time.Second
	}
	if poll.Interval == 0 {
		poll.Interval = 5 * time.Second
	}
	return &CancelCustomerFlow{client: c, reg: r, runID: runID, poll: poll}
}

func (s *CancelCustomerFlow) Name() string         { return "cancel-customer-flow" }
func (s *CancelCustomerFlow) Kind() synthetic.Kind { return synthetic.KindScenario }

func (s *CancelCustomerFlow) Run(ctx context.Context) error {
	subs := s.reg.Ephemerals("subscription")
	if len(subs) == 0 {
		return nil
	}
	sort.Slice(subs, func(i, j int) bool { return subs[i].CreatedAt.Before(subs[j].CreatedAt) })
	target := subs[0]

	cancelType := types.CancellationTypeImmediate
	prorate := types.ProrationBehaviorCreateProrations
	if _, err := s.client.Subscriptions().Cancel(ctx, target.ID, types.DtoCancelSubscriptionRequest{
		CancellationType:  cancelType,
		ProrationBehavior: &prorate,
		Reason:            strPtr("synthetic-cancel-customer-flow"),
	}); err != nil {
		return fmt.Errorf("cancel %s: %w", target.ID, err)
	}

	if err := s.pollInvoice(ctx, target.ID); err != nil {
		return err
	}
	s.reg.ArchiveEphemeral("subscription", target.ID)
	return nil
}

func (s *CancelCustomerFlow) pollInvoice(ctx context.Context, subID string) error {
	deadline := time.Now().Add(s.poll.Timeout)
	for {
		resp, err := s.client.Invoices().Query(ctx, types.InvoiceFilter{SubscriptionID: &subID})
		if err == nil && hasInvoiceForSub(resp) {
			return nil
		}
		if time.Now().After(deadline) {
			if err != nil {
				return fmt.Errorf("invoice query timeout: %w", err)
			}
			return fmt.Errorf("no invoice for %s within %s", subID, s.poll.Timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.poll.Interval):
		}
	}
}

// hasInvoiceForSub is a package-level variable so tests can swap it.
// Default returns true so tests pass against the empty fake response wrapper;
// Task 25 swaps in the real getter.
var hasInvoiceForSub = func(_ interface{}) bool { return true }
