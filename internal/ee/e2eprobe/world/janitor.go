package world

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/go-sdk/v2/models/types"
)

// OrphanSweeper removes resources a scenario owns that outlived their run.
type OrphanSweeper interface {
	SweepOrphans(ctx context.Context, cutoff time.Time) (int, []string)
}

// SweepReport is what one janitor pass did and could not do.
type SweepReport struct {
	Archived        int      // registry entries removed
	Orphans         int      // ephemeral customers found in the tenant and deleted
	TaxAssociations int      // associations whose subscription is gone
	Swept           int      // resources removed by registered sweepers
	Failures        []string // deletes that failed and will be retried
	Legacy          []string // persistent customers no persona uses
}

func (w *World) AddSweeper(s OrphanSweeper) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sweepers = append(w.sweepers, s)
}

// Sweep removes ephemerals older than cutoff: registered ones first, then any
// the tenant still holds from earlier runs, then tax associations pointing at
// vanished subscriptions, then whatever registered sweepers own.
func (w *World) Sweep(ctx context.Context, cutoff time.Time) SweepReport {
	var report SweepReport
	w.sweepRegistry(ctx, cutoff, &report)
	w.sweepOrphans(ctx, cutoff, &report)
	w.sweepTaxAssociations(ctx, &report)
	w.mu.RLock()
	sweepers := append([]OrphanSweeper(nil), w.sweepers...)
	w.mu.RUnlock()
	for _, s := range sweepers {
		removed, failed := s.SweepOrphans(ctx, cutoff)
		report.Swept += removed
		report.Failures = append(report.Failures, failed...)
	}
	return report
}

func (w *World) sweepRegistry(ctx context.Context, cutoff time.Time, report *SweepReport) {
	for _, e := range w.eph.List("customer") {
		if e.CreatedAt.After(cutoff) {
			continue
		}
		resp, err := w.client.Customers().GetByExternalID(ctx, e.ID)
		switch {
		case IsNotFound(err):
		case err != nil:
			report.Failures = append(report.Failures, fmt.Sprintf("customer %s: %v", e.ID, err))
			continue
		case resp.CustomerResponse != nil && resp.CustomerResponse.ID != nil:
			if err := w.deleteCustomer(ctx, *resp.CustomerResponse.ID, e.ID); err != nil {
				report.Failures = append(report.Failures, fmt.Sprintf("customer %s: %v", e.ID, err))
				continue
			}
		}
		w.eph.Archive("customer", e.ID)
		report.Archived++
	}
	// Cancelled subscriptions are retained server-side; the registry entry just expires.
	for _, e := range w.eph.List("subscription") {
		if e.CreatedAt.After(cutoff) {
			continue
		}
		w.eph.Archive("subscription", e.ID)
		report.Archived++
	}
}

// isEphemeralCustomer is deliberately loose so customers from earlier probe
// generations, tagged differently, are still recognised.
func isEphemeralCustomer(c types.CustomerResponse) bool {
	if c.ExternalID != nil && (strings.HasPrefix(*c.ExternalID, EphemeralPrefix) || strings.HasPrefix(*c.ExternalID, "e2eprobe-cust-eph-")) {
		return true
	}
	if c.Name != nil && strings.Contains(*c.Name, "Ephemeral") {
		return true
	}
	return c.Metadata["e2eprobe_cohort"] == EphemeralCohort || c.Metadata["e2eprobe_role"] == EphemeralCohort
}

func (w *World) sweepOrphans(ctx context.Context, cutoff time.Time, report *SweepReport) {
	resp, err := w.client.Customers().Query(ctx, types.CustomerFilter{Limit: ptr(int64(1000))})
	if err != nil {
		report.Failures = append(report.Failures, fmt.Sprintf("query customers: %v", err))
		return
	}
	if resp.ListCustomersResponse == nil {
		return
	}
	known := w.knownExternalIDs()
	for _, cust := range resp.ListCustomersResponse.Items {
		ext := ""
		if cust.ExternalID != nil {
			ext = *cust.ExternalID
		}
		if cust.Metadata["e2eprobe_cohort"] == PersistentCohort && !known[ext] {
			report.Legacy = append(report.Legacy, ext)
			continue
		}
		if !isEphemeralCustomer(cust) || cust.ID == nil || cust.CreatedAt == nil || cust.CreatedAt.After(cutoff) {
			continue
		}
		if err := w.deleteCustomer(ctx, *cust.ID, ext); err != nil {
			report.Failures = append(report.Failures, fmt.Sprintf("orphan %s: %v", ext, err))
			continue
		}
		report.Orphans++
	}
}

// knownExternalIDs is every customer the suite expects to exist.
func (w *World) knownExternalIDs() map[string]bool {
	known := map[string]bool{}
	for _, spec := range w.suite.Personas {
		known[spec.ExternalIDOrDefault()] = true
		if spec.Payment != nil {
			if spec.Payment.FixedCustomer != "" {
				known[spec.Payment.FixedCustomer] = true
			}
			// The checkout-link scenario keeps one persistent customer per gateway for its expiry watch.
			known["e2eprobe-cust-pay-"+spec.Payment.Provider+"-expiry"] = true
		}
	}
	return known
}

// deleteCustomer cancels the customer's open subscriptions, which would block
// the delete, then deletes it. A 404 anywhere means another pass got there first.
func (w *World) deleteCustomer(ctx context.Context, customerID, ext string) error {
	subs, err := w.client.Subscriptions().Query(ctx, types.SubscriptionFilter{CustomerID: ptr(customerID)})
	if err != nil {
		return fmt.Errorf("query subscriptions: %w", err)
	}
	if subs.ListSubscriptionsResponse != nil {
		immediate := types.CancellationTypeImmediate
		policy := types.CancelImmediatelyInvoicePolicyGenerateInvoice
		for _, sub := range subs.ListSubscriptionsResponse.Items {
			if sub.ID == nil || (sub.SubscriptionStatus != nil && *sub.SubscriptionStatus == types.SubscriptionStatusCancelled) {
				continue
			}
			if _, err := w.client.Subscriptions().Cancel(ctx, *sub.ID, types.CancelSubscriptionRequest{
				CancellationType:               immediate,
				CancelImmediatelyInvoicePolicy: &policy,
				Reason:                         ptr("e2eprobe janitor"),
			}); err != nil && !IsNotFound(err) {
				return fmt.Errorf("cancel subscription %s: %w", *sub.ID, err)
			}
		}
	}
	if _, err := w.client.Customers().Delete(ctx, customerID); err != nil && !IsNotFound(err) {
		return fmt.Errorf("delete: %w", err)
	}
	w.info(ctx, "world: removed ephemeral customer", "external_customer_id", ext, "customer_id", customerID)
	return nil
}

func (w *World) sweepTaxAssociations(ctx context.Context, report *SweepReport) {
	taxRateID := w.Fixtures().TaxRateID()
	if taxRateID == "" {
		return
	}
	resp, err := w.client.TaxAssociations().List(ctx, nil, nil, nil, &taxRateID)
	if err != nil {
		report.Failures = append(report.Failures, fmt.Sprintf("list tax associations: %v", err))
		return
	}
	if resp == nil || resp.ListTaxAssociationsResponse == nil {
		return
	}
	for _, ta := range resp.ListTaxAssociationsResponse.Items {
		if ta.ID == nil || ta.EntityID == nil {
			continue
		}
		if _, err := w.client.Subscriptions().Get(ctx, *ta.EntityID); !IsNotFound(err) {
			continue
		}
		if _, err := w.client.TaxAssociations().Delete(ctx, *ta.ID); err != nil && !IsNotFound(err) {
			report.Failures = append(report.Failures, fmt.Sprintf("tax association %s: %v", *ta.ID, err))
			continue
		}
		report.TaxAssociations++
	}
}
