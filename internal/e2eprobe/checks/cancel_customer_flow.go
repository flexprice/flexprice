package checks

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/e2eprobe"
	sdkdtos "github.com/flexprice/go-sdk/v2/models/dtos"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
	"github.com/flexprice/go-sdk/v2/models/types"
)

type InvoicePoll struct {
	Timeout  time.Duration
	Interval time.Duration
}

type CancelCustomerFlow struct {
	client e2eprobe.Client
	reg    e2eprobe.Registry
	runID  string
	poll   InvoicePoll
}

func NewCancelCustomerFlow(c e2eprobe.Client, r e2eprobe.Registry, runID string, poll InvoicePoll) *CancelCustomerFlow {
	if poll.Timeout == 0 {
		poll.Timeout = 30 * time.Second
	}
	if poll.Interval == 0 {
		poll.Interval = 5 * time.Second
	}
	return &CancelCustomerFlow{client: c, reg: r, runID: runID, poll: poll}
}

func (s *CancelCustomerFlow) Name() string         { return "cancel-customer-flow" }
func (s *CancelCustomerFlow) Kind() e2eprobe.Kind { return e2eprobe.KindScenario }

func (s *CancelCustomerFlow) Run(ctx context.Context) error {
	subs := s.reg.Ephemerals("subscription")
	if len(subs) == 0 {
		return nil
	}
	sort.Slice(subs, func(i, j int) bool { return subs[i].CreatedAt.Before(subs[j].CreatedAt) })
	target := subs[0]

	cancelType := types.CancellationTypeImmediate
	prorate := types.ProrationBehaviorCreateProrations
	// Ask Flexprice to generate a final invoice for usage accrued up to the
	// cancellation instant. Without this, ephemeral customers leave behind
	// pending unbilled usage even though the subscription is gone — which
	// defeats the cycle-invoice-probe's coverage of the invoice generation
	// path for these customers.
	generateInvoice := types.CancelImmediatelyInvoicePolicyGenerateInvoice
	if _, err := s.client.Subscriptions().Cancel(ctx, target.ID, types.CancelSubscriptionRequest{
		CancellationType:               cancelType,
		ProrationBehavior:              &prorate,
		CancelImmediatelyInovicePolicy: &generateInvoice,
		Reason:                         strPtr("e2eprobe-cancel-customer-flow"),
	}); err != nil {
		// A previous tick may have already cancelled the sub server-side (e.g.
		// the network call partially succeeded — TCP reset after the write,
		// before the response). The upstream returns `{}` on a retry of an
		// already-cancelled sub. Check the actual state: if already cancelled,
		// the work is done — archive and continue without alerting.
		if statusErr := s.checkAlreadyCancelled(ctx, target.ID); statusErr == nil {
			s.reg.ArchiveEphemeral("subscription", target.ID)
			return nil
		}
		return e2eprobe.Errorf(map[string]string{"subscription_id": target.ID}, "cancel %s: %w", target.ID, err)
	}

	if err := s.pollSubStatusCancelled(ctx, target.ID); err != nil {
		return err
	}
	s.reg.ArchiveEphemeral("subscription", target.ID)

	// Also delete the customer — cancel alone leaves a zombie customer in Flexprice.
	// Best-effort: if the delete fails we log a warning but don't fail the check
	// because the subscription cancellation already succeeded.
	subResp, err := s.client.Subscriptions().Get(ctx, target.ID)
	if err == nil {
		internalCustID := extractSubCustomerID(subResp)
		extCustID := extractSubExternalCustomerID(subResp)
		if internalCustID != "" {
			if _, delErr := s.client.Customers().Delete(ctx, internalCustID); delErr != nil {
				// 404 means janitor already cleaned it — treat as success.
				var apiErr *sdkerrors.APIError
				if !errors.As(delErr, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
					// Log the failure as a warning attribute but don't return an error.
					_ = e2eprobe.Errorf(map[string]string{
						"subscription_id":      target.ID,
						"internal_customer_id": internalCustID,
						"external_customer_id": extCustID,
					}, "delete customer %s (best-effort): %w", internalCustID, delErr)
				}
			}
			if extCustID != "" {
				s.reg.ArchiveEphemeral("customer", extCustID)
			}
		}
	}
	return nil
}

// checkAlreadyCancelled returns nil if the subscription is already in the
// CANCELLED state. Any other state (or read error) returns a non-nil error so
// the caller can decide what to do. This is the idempotency check for retries
// after a partial-success failure (e.g. TCP reset post-write).
func (s *CancelCustomerFlow) checkAlreadyCancelled(ctx context.Context, subID string) error {
	resp, err := s.client.Subscriptions().Get(ctx, subID)
	if err != nil {
		return err
	}
	if isCancelled(resp) {
		return nil
	}
	return fmt.Errorf("sub %s not cancelled (observed: %s)", subID, observedSubStatus(resp))
}

// pollSubStatusCancelled polls Subscriptions.Get until the subscription reaches
// the CANCELLED terminal state. This replaces the previous invoice-presence poll
// which produced false alerts because the server-side InvoiceFilter{SubscriptionID}
// query was unreliable (returned 0 items even when invoices existed in the DB).
// Sub cancellation is synchronous on the backend, so a 30s window is sufficient
// to absorb any processing lag.
func (s *CancelCustomerFlow) pollSubStatusCancelled(ctx context.Context, subID string) error {
	deadline := time.Now().Add(s.poll.Timeout)
	for {
		resp, err := s.client.Subscriptions().Get(ctx, subID)
		if err == nil {
			if isCancelled(resp) {
				return nil
			}
			observedStatus := observedSubStatus(resp)
			if time.Now().After(deadline) {
				return e2eprobe.Errorf(
					map[string]string{"subscription_id": subID, "observed_status": observedStatus},
					"sub %s did not reach cancelled status within %s (observed: %s)",
					subID, s.poll.Timeout, observedStatus,
				)
			}
		} else {
			if time.Now().After(deadline) {
				return e2eprobe.Errorf(map[string]string{"subscription_id": subID}, "get sub %s timeout: %w", subID, err)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.poll.Interval):
		}
	}
}

// isCancelled returns true when the GetSubscriptionResponse contains a
// subscription with SubscriptionStatus == "cancelled".
func isCancelled(resp interface{}) bool {
	type subGetter interface {
		GetSubscriptionResponse() *types.SubscriptionResponse
	}
	g, ok := resp.(subGetter)
	if !ok || g == nil {
		return false
	}
	inner := g.GetSubscriptionResponse()
	if inner == nil {
		return false
	}
	st := inner.SubscriptionStatus
	if st == nil {
		return false
	}
	return *st == types.SubscriptionStatusCancelled
}

// observedSubStatus extracts the subscription_status string for error messages.
func observedSubStatus(resp interface{}) string {
	type subGetter interface {
		GetSubscriptionResponse() *types.SubscriptionResponse
	}
	g, ok := resp.(subGetter)
	if !ok || g == nil {
		return "unknown"
	}
	inner := g.GetSubscriptionResponse()
	if inner == nil || inner.SubscriptionStatus == nil {
		return "unknown"
	}
	return fmt.Sprintf("%v", *inner.SubscriptionStatus)
}

// extractSubExternalCustomerID reads the external customer ID from the embedded
// Customer object in a GetSubscriptionResponse. Returns "" when unavailable.
func extractSubExternalCustomerID(resp interface{}) string {
	r, ok := resp.(*sdkdtos.GetSubscriptionResponse)
	if !ok || r == nil {
		return ""
	}
	inner := r.GetSubscriptionResponse()
	if inner == nil || inner.Customer == nil || inner.Customer.ExternalID == nil {
		return ""
	}
	return *inner.Customer.ExternalID
}
