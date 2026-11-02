package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/billingterm"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

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
