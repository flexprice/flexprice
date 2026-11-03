package service

import (
	"context"
	"errors"
	"slices"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/billingterm"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// errStaleInstalments is the cause of a finalization that found instalments on the invoice no longer scheduled.
var errStaleInstalments = errors.New("invoice carries instalments that changed since it was computed")

// finalizeDraft finalizes the draft, recomputing a subscription draft first if it misses a due instalment and again if
// it bills one no longer scheduled. A manually edited draft is never recomputed and keeps the stale error.
func (s *invoiceService) finalizeDraft(ctx context.Context, inv *invoice.Invoice, source dto.InvoiceStateChangeSource) error {
	recomputable := inv.InvoiceType == types.InvoiceTypeSubscription && inv.SubscriptionID != nil && !inv.IsManuallyEdited
	recompute := func(reason string, instalmentIDs []string) error {
		s.Logger.Info(ctx, "invoice.instalments_recomputed",
			"operation", "invoice.instalments_recomputed",
			"reason", reason,
			"invoice_id", inv.ID,
			"instalment_ids", instalmentIDs,
		)

		recomputed, _, err := s.ComputeInvoice(ctx, inv.ID, &dto.InvoiceComputeRequest{InvoiceStateChangeSource: source})
		if err != nil {
			return err
		}

		*inv = *recomputed
		return nil
	}

	if recomputable {
		missing, err := s.missingInstalments(ctx, inv)
		if err != nil {
			return err
		}

		if len(missing) > 0 {
			if err := recompute("due instalments missing from the draft", missing); err != nil {
				return err
			}
		}
	}

	err := s.performFinalizeInvoiceActions(ctx, inv, source.SourceID())
	if !errors.Is(err, errStaleInstalments) || !recomputable {
		return err
	}

	if err := recompute("instalments changed since the draft was computed", nil); err != nil {
		return err
	}

	return s.performFinalizeInvoiceActions(ctx, inv, source.SourceID())
}

// missingInstalments returns the ids of the scheduled instalments due within a subscription draft that it does not
// bill, such as a milestone triggered after the draft was computed.
func (s *invoiceService) missingInstalments(ctx context.Context, inv *invoice.Invoice) ([]string, error) {
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

	due, err := dueInstalments(ctx, s.ServiceParams, *inv.SubscriptionID, billAt)
	if err != nil || len(due) == 0 {
		return nil, err
	}

	billed, err := s.billedInstalmentIDs(ctx, inv.ID)
	if err != nil {
		return nil, err
	}

	return lo.Without(lo.Map(due, func(inst *billingterm.Instalment, _ int) string { return inst.ID }), billed...), nil
}

// claimInstalments marks the scheduled instalments on a finalizing invoice as billed by it, in the caller's
// finalize transaction, and returns the ids of those it could not claim because they are no longer scheduled.
func (s *invoiceService) claimInstalments(ctx context.Context, inv *invoice.Invoice) (unclaimed []string, err error) {
	ids, err := s.billedInstalmentIDs(ctx, inv.ID)
	if err != nil || len(ids) == 0 {
		return nil, err
	}

	claimed, err := s.BillingInstalmentRepo.Claim(ctx, inv.ID, ids)
	if err != nil {
		return nil, err
	}

	publishInstalmentEvents(ctx, s.ServiceParams, types.WebhookEventBillingInstalmentInvoiced, claimed)
	claimedIDs := lo.Map(claimed, func(inst *billingterm.Instalment, _ int) string { return inst.ID })
	return lo.Without(ids, claimedIDs...), nil
}

// requireFreshInstalments refuses to charge a draft that bills instalments no longer scheduled: paying it in full
// finalizes it without a recompute, so it would bill them twice.
func (s *invoiceService) requireFreshInstalments(ctx context.Context, inv *invoice.Invoice) error {
	if inv.InvoiceStatus != types.InvoiceStatusDraft {
		return nil
	}

	stale, err := s.staleInstalments(ctx, inv)
	if err != nil || len(stale) == 0 {
		return err
	}

	return ierr.WithError(errStaleInstalments).
		WithHint("Recompute the invoice before paying it").
		WithReportableDetails(map[string]any{
			"invoice_id":     inv.ID,
			"instalment_ids": stale,
		}).
		Mark(ierr.ErrInvalidOperation)
}

// staleInstalments returns the ids of the instalments a draft bills that are no longer scheduled.
func (s *invoiceService) staleInstalments(ctx context.Context, inv *invoice.Invoice) ([]string, error) {
	ids, err := s.billedInstalmentIDs(ctx, inv.ID)
	if err != nil || len(ids) == 0 {
		return nil, err
	}

	scheduled, err := s.BillingInstalmentRepo.List(ctx, &billingterm.InstalmentFilter{
		IDs:      ids,
		Statuses: []types.InstalmentStatus{types.InstalmentStatusScheduled},
	})
	if err != nil {
		return nil, err
	}

	return lo.Without(ids, lo.Map(scheduled, func(inst *billingterm.Instalment, _ int) string { return inst.ID })...), nil
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
