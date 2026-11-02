package service

import (
	"context"
	"errors"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/billingterm"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// errStaleInstalments is the cause of a finalization that found instalments on the invoice no longer scheduled.
var errStaleInstalments = errors.New("invoice carries instalments that changed since it was computed")

// finalizeDraft finalizes the draft. A subscription draft carrying instalments that are no longer scheduled is
// recomputed and finalized once more; a manually edited one is never recomputed, so it keeps the error.
func (s *invoiceService) finalizeDraft(ctx context.Context, inv *invoice.Invoice, source dto.InvoiceStateChangeSource) error {
	err := s.performFinalizeInvoiceActions(ctx, inv, source.SourceID())
	if !errors.Is(err, errStaleInstalments) {
		return err
	}

	if inv.InvoiceType != types.InvoiceTypeSubscription || inv.SubscriptionID == nil || inv.IsManuallyEdited {
		return err
	}

	s.Logger.Info(ctx, "recomputing a draft whose instalments changed since it was computed",
		"invoice_id", inv.ID)

	recomputed, _, err := s.ComputeInvoice(ctx, inv.ID, &dto.InvoiceComputeRequest{InvoiceStateChangeSource: source})
	if err != nil {
		return err
	}

	*inv = *recomputed
	return s.performFinalizeInvoiceActions(ctx, inv, source.SourceID())
}

// claimInstalments marks the scheduled instalments on a finalizing invoice as billed by it, in the caller's
// finalize transaction, and returns the ids of those it could not claim because they are no longer scheduled.
func (s *invoiceService) claimInstalments(ctx context.Context, inv *invoice.Invoice) (unclaimed []string, err error) {
	lines, err := s.InvoiceLineItemRepo.ListByInvoiceID(ctx, inv.ID)
	if err != nil {
		return nil, err
	}

	ids := lo.FilterMap(lines, func(line *invoice.InvoiceLineItem, _ int) (string, bool) {
		return lo.FromPtr(line.BillingInstalmentID), line.BillingInstalmentID != nil
	})
	if len(ids) == 0 {
		return nil, nil
	}

	claimed, err := s.BillingInstalmentRepo.Claim(ctx, inv.ID, ids)
	if err != nil {
		return nil, err
	}

	publishInstalmentEvents(ctx, s.ServiceParams, types.WebhookEventBillingInstalmentInvoiced, claimed)
	claimedIDs := lo.Map(claimed, func(inst *billingterm.Instalment, _ int) string { return inst.ID })
	return lo.Without(ids, claimedIDs...), nil
}

// rescheduleInstalments puts a voided invoice's instalments back on schedule. It runs in the void transaction.
func (s *invoiceService) rescheduleInstalments(ctx context.Context, invoiceID string) error {
	rescheduled, err := s.BillingInstalmentRepo.Reschedule(ctx, invoiceID)
	if err != nil {
		return err
	}

	publishInstalmentEvents(ctx, s.ServiceParams, types.WebhookEventBillingInstalmentUpdated, rescheduled)
	return nil
}
