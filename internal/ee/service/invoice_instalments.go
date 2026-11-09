package service

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/billingterm"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// errStaleDraft is the cause of refusing to finalize or charge a draft that bills instalments no longer scheduled.
var errStaleDraft = errors.New("draft bills instalments that are no longer scheduled")

// refreshStaleDraft recomputes a subscription draft that went stale since it was computed: it bills an instalment an
// edit cancelled or another invoice claimed, misses one that fell due, like a triggered milestone, or predates a change
// to one of the subscription's commitments.
func (s *invoiceService) refreshStaleDraft(
	ctx context.Context,
	inv *invoice.Invoice,
	source dto.InvoiceStateChangeSource,
) error {
	// A manually edited draft is never recomputed; finalizing it fails if it bills a stale instalment.
	if inv.InvoiceStatus != types.InvoiceStatusDraft || inv.InvoiceType != types.InvoiceTypeSubscription ||
		inv.SubscriptionID == nil || inv.IsManuallyEdited {
		return nil
	}

	billed, err := s.billedInstalmentIDs(ctx, inv.ID)
	if err != nil {
		return err
	}

	stale, err := s.staleInstalments(ctx, billed)
	if err != nil {
		return err
	}

	enabled, err := newCommitmentService(s.ServiceParams).IsEnabled(ctx)
	if err != nil {
		return err
	}

	var missing []string
	var changed bool
	if enabled {
		subscriptionIDs, err := s.invoicedSubscriptionIDs(ctx, *inv.SubscriptionID)
		if err != nil {
			return err
		}

		if missing, err = s.missingInstalments(ctx, subscriptionIDs, inv, billed); err != nil {
			return err
		}

		if inv.LastComputedAt != nil {
			if changed, err = s.commitmentChangedSince(ctx, subscriptionIDs, *inv.LastComputedAt); err != nil {
				return err
			}
		}
	}

	if len(stale) == 0 && len(missing) == 0 && !changed {
		return nil
	}

	s.Logger.Info(ctx, "invoice.stale_draft_recomputed",
		"operation", "invoice.stale_draft_recomputed",
		"invoice_id", inv.ID,
		"stale_instalment_ids", stale,
		"missing_instalment_ids", missing,
		"commitment_changed", changed,
	)

	recomputed, _, err := s.ComputeInvoice(ctx, inv.ID, &dto.InvoiceComputeRequest{InvoiceStateChangeSource: source})
	if err != nil {
		return err
	}

	*inv = *recomputed
	return nil
}

// invoicedSubscriptionIDs returns the subscriptions whose charges the subscription's invoices carry: itself and, for a
// parent, its grouped children.
func (s *invoiceService) invoicedSubscriptionIDs(ctx context.Context, subscriptionID string) ([]string, error) {
	sub, err := s.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	children, err := getGroupedInvoicingChildren(ctx, s.ServiceParams, sub, false)
	if err != nil {
		return nil, err
	}

	return append([]string{sub.ID}, lo.Map(children, func(child *subscription.Subscription, _ int) string {
		return child.ID
	})...), nil
}

// commitmentChangedSince reports whether a commitment of the subscriptions was written after computedAt, which a draft
// computed then does not reflect. A compute overlapping the write can still stamp a later time; the post-commit
// recompute covers that.
func (s *invoiceService) commitmentChangedSince(ctx context.Context, subscriptionIDs []string, computedAt time.Time) (bool, error) {
	filter := commitment.NewNoLimitFilter()
	filter.SubscriptionIDs = subscriptionIDs
	listed, err := s.CommitmentRepo.List(ctx, filter)
	if err != nil {
		return false, err
	}

	return lo.SomeBy(listed, func(c *commitment.Commitment) bool { return c.UpdatedAt.After(computedAt) }), nil
}

// staleInstalments returns those of the billed instalments that are no longer scheduled.
func (s *invoiceService) staleInstalments(ctx context.Context, billed []string) ([]string, error) {
	if len(billed) == 0 {
		return nil, nil
	}

	scheduled, err := s.BillingInstalmentRepo.List(ctx, &billingterm.InstalmentFilter{
		IDs:      billed,
		Statuses: []types.InstalmentStatus{types.InstalmentStatusScheduled},
	})
	if err != nil {
		return nil, err
	}

	return lo.Without(billed, lo.Map(scheduled, func(inst *billingterm.Instalment, _ int) string { return inst.ID })...), nil
}

// missingInstalments returns the scheduled instalments of the subscriptions due within a subscription draft that it
// does not bill.
func (s *invoiceService) missingInstalments(
	ctx context.Context,
	subscriptionIDs []string,
	inv *invoice.Invoice,
	billed []string,
) ([]string, error) {
	// A trial-start invoice drops its instalments and a paused period's invoice is never computed.
	reason := types.InvoiceBillingReason(inv.BillingReason)
	if reason == types.InvoiceBillingReasonSubscriptionTrialStart || reason == types.InvoiceBillingReasonSubscriptionPaused ||
		inv.PeriodStart == nil || inv.PeriodEnd == nil {
		return nil, nil
	}

	billAt, ok := instalmentBillAt(subscriptionReferencePoint(reason), *inv.PeriodStart, *inv.PeriodEnd)
	if !ok {
		return nil, nil
	}

	due, err := dueInstalments(ctx, s.ServiceParams, subscriptionIDs, billAt)
	if err != nil {
		return nil, err
	}

	return lo.Without(lo.Map(due, func(inst *billingterm.Instalment, _ int) string { return inst.ID }), billed...), nil
}

// claimInstalments marks the scheduled instalments on a finalizing invoice as billed by it at its finalization, in the
// caller's finalize transaction, and returns the ids of those it could not claim because they are no longer scheduled.
func (s *invoiceService) claimInstalments(ctx context.Context, inv *invoice.Invoice) (unclaimed []string, err error) {
	ids, err := s.billedInstalmentIDs(ctx, inv.ID)
	if err != nil || len(ids) == 0 {
		return nil, err
	}

	claimed, err := s.BillingInstalmentRepo.Claim(ctx, inv.ID, lo.FromPtrOr(inv.FinalizedAt, clock.Now()), ids)
	if err != nil {
		return nil, err
	}

	publishInstalmentEvents(ctx, s.ServiceParams, types.WebhookEventBillingInstalmentInvoiced, claimed)
	claimedIDs := lo.Map(claimed, func(inst *billingterm.Instalment, _ int) string { return inst.ID })
	return lo.Without(ids, claimedIDs...), nil
}

// requireFreshDraft refuses to charge a draft that bills instalments no longer scheduled: paying it in full finalizes
// it without a refresh, so it would bill them twice.
func (s *invoiceService) requireFreshDraft(ctx context.Context, inv *invoice.Invoice) error {
	if inv.InvoiceStatus != types.InvoiceStatusDraft {
		return nil
	}

	billed, err := s.billedInstalmentIDs(ctx, inv.ID)
	if err != nil {
		return err
	}

	stale, err := s.staleInstalments(ctx, billed)
	if err != nil || len(stale) == 0 {
		return err
	}

	return staleDraftError(inv.ID, stale, "Recompute the invoice before paying it")
}

// requireNoDraftPayment refuses to change a commitment's instalments while a draft billing them is being paid: the
// payment would finalize the draft with instalments the change cancels, so they would be billed twice. It locks each
// draft, as a charge does before it starts, for the rest of the caller's transaction.
func (s *invoiceService) requireNoDraftPayment(ctx context.Context, commitmentID string) error {
	lineFilter := types.NewNoLimitInvoiceLineItemFilter()
	lineFilter.EntityType = lo.ToPtr(string(types.InvoiceLineItemEntityTypeCommitment))
	lineFilter.EntityIDs = []string{commitmentID}
	lines, err := s.InvoiceLineItemRepo.List(ctx, lineFilter)
	if err != nil {
		return err
	}

	invoiceIDs := lo.Uniq(lo.FilterMap(lines, func(line *invoice.InvoiceLineItem, _ int) (string, bool) {
		return line.InvoiceID, line.BillingInstalmentID != nil
	}))
	if len(invoiceIDs) == 0 {
		return nil
	}

	invoiceFilter := types.NewNoLimitInvoiceFilter()
	invoiceFilter.InvoiceIDs = invoiceIDs
	invoiceFilter.InvoiceStatus = []types.InvoiceStatus{types.InvoiceStatusDraft}
	drafts, err := s.InvoiceRepo.List(ctx, invoiceFilter)
	if err != nil {
		return err
	}

	slices.SortFunc(drafts, func(a, b *invoice.Invoice) int { return strings.Compare(a.ID, b.ID) })
	for _, draft := range drafts {
		if err := s.lockInvoice(ctx, draft.ID); err != nil {
			return err
		}

		paymentFilter := types.NewNoLimitPaymentFilter()
		paymentFilter.DestinationType = lo.ToPtr(string(types.PaymentDestinationTypeInvoice))
		paymentFilter.DestinationID = lo.ToPtr(draft.ID)
		payments, err := s.PaymentRepo.List(ctx, paymentFilter)
		if err != nil {
			return err
		}

		// A succeeded payment counts until the draft is finalized: it is about to finalize it, or paid part of it.
		charging, found := lo.Find(payments, func(p *payment.Payment) bool {
			return lo.Contains([]types.PaymentStatus{
				types.PaymentStatusPending,
				types.PaymentStatusProcessing,
				types.PaymentStatusSucceeded,
			}, p.PaymentStatus)
		})
		if found {
			return ierr.NewErrorf("draft invoice %s is being paid by payment %s", draft.ID, charging.ID).
				WithHint("Change the billing terms once the invoice is finalized or the payment fails").
				WithReportableDetails(map[string]any{
					"invoice_id": draft.ID,
					"payment_id": charging.ID,
				}).
				Mark(ierr.ErrInvalidOperation)
		}
	}

	return nil
}

// lockInvoice serialises starting a charge on an invoice with changes to what it bills. It must run inside a
// transaction.
func (s *invoiceService) lockInvoice(ctx context.Context, id string) error {
	if err := s.DB.LockWithWait(ctx, postgres.LockRequest{Key: "invoice:" + id}); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to lock invoice").
			Mark(ierr.ErrInternal)
	}

	return nil
}

// staleDraftError refuses a draft that bills the given instalments, which are no longer scheduled.
func staleDraftError(invoiceID string, instalmentIDs []string, hint string) error {
	return ierr.WithError(errStaleDraft).
		WithHint(hint).
		WithReportableDetails(map[string]any{
			"invoice_id":     invoiceID,
			"instalment_ids": instalmentIDs,
		}).
		Mark(ierr.ErrInvalidOperation)
}

// billedInstalmentIDs returns the ids of the instalments the invoice's lines bill.
func (s *invoiceService) billedInstalmentIDs(ctx context.Context, invoiceID string) ([]string, error) {
	lines, err := s.InvoiceLineItemRepo.ListByInvoiceID(ctx, invoiceID)
	if err != nil {
		return nil, err
	}

	return lo.FilterMap(lines, func(line *invoice.InvoiceLineItem, _ int) (string, bool) {
		return lo.FromPtr(line.BillingInstalmentID), line.BillingInstalmentID != nil
	}), nil
}

// rescheduleInstalments puts a voided invoice's instalments back on schedule in the void transaction, under their
// commitments' locks so an edit sizing its plan by the invoiced instalments never runs halfway through it.
func (s *invoiceService) rescheduleInstalments(ctx context.Context, invoiceID string) error {
	ids, err := s.billedInstalmentIDs(ctx, invoiceID)
	if err != nil || len(ids) == 0 {
		return err
	}

	billed, err := s.BillingInstalmentRepo.List(ctx, &billingterm.InstalmentFilter{IDs: ids})
	if err != nil {
		return err
	}

	owners := lo.Uniq(lo.Map(billed, func(inst *billingterm.Instalment, _ int) string { return inst.OwnerID }))
	slices.Sort(owners)
	commitments := NewCommitmentService(s.ServiceParams).(*commitmentService)
	for _, id := range owners {
		if err := commitments.lockCommitment(ctx, id); err != nil {
			return err
		}
	}

	rescheduled, err := s.BillingInstalmentRepo.Reschedule(ctx, invoiceID)
	if err != nil {
		return err
	}

	publishInstalmentEvents(ctx, s.ServiceParams, types.WebhookEventBillingInstalmentUpdated, rescheduled)
	return nil
}
