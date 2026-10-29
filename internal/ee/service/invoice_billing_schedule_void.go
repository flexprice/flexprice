package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/ee/service/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
)

func (s *invoiceService) withBillingScheduleVoid(ctx context.Context, inv *invoice.Invoice, apply func(context.Context) error) error {
	if s.BillingTerms == nil {
		if hasBillingScheduleLines(inv) {
			return ierr.NewError("billing terms service is unavailable").
				WithHint("Scheduled invoice sources must be canceled atomically with the invoice").Mark(ierr.ErrServiceUnavailable)
		}
		return apply(ctx)
	}
	req := billingterms.InvoiceVoidRequest{InvoiceID: inv.ID}
	for _, line := range inv.LineItems {
		if line != nil && line.BillingScheduleItemID != nil {
			req.Sources = append(req.Sources, billingterms.InvoiceSource{ItemID: *line.BillingScheduleItemID, InvoiceLineItemID: line.ID})
		}
	}
	return s.BillingTerms.WithInvoiceVoid(ctx, req, apply)
}
