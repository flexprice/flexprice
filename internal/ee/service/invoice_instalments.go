package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// claimInstalments marks the instalments on a finalizing invoice as billed by it. It runs in the finalize
// transaction, so a draft carrying an instalment that is no longer scheduled fails the whole finalization.
func (s *invoiceService) claimInstalments(ctx context.Context, inv *invoice.Invoice) error {
	lines, err := s.InvoiceLineItemRepo.ListByInvoiceID(ctx, inv.ID)
	if err != nil {
		return err
	}

	ids := lo.FilterMap(lines, func(line *invoice.InvoiceLineItem, _ int) (string, bool) {
		return lo.FromPtr(line.BillingInstalmentID), line.BillingInstalmentID != nil
	})
	if len(ids) == 0 {
		return nil
	}

	claimed, err := s.BillingInstalmentRepo.Claim(ctx, inv.ID, ids)
	if err != nil {
		return err
	}

	if len(claimed) < len(ids) {
		return ierr.NewError("invoice carries instalments that changed since it was computed").
			WithHint("Recompute the invoice and finalize again").
			Mark(ierr.ErrInvalidOperation)
	}

	publishInstalmentEvents(ctx, s.ServiceParams, types.WebhookEventBillingInstalmentInvoiced, claimed)
	return nil
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
