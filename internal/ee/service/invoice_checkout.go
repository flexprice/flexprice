package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// createPayGatedOneOffInvoice creates the one-off invoice as a computed DRAFT and opens a
// hosted checkout session over it. The invoice finalizes only when the payment webhook lands.
func (s *invoiceService) createPayGatedOneOffInvoice(ctx context.Context, req dto.CreateInvoiceRequest) (*dto.InvoiceResponse, error) {
	inv, skipped, err := s.CreateComputedDraftInvoice(ctx, req)
	if err != nil {
		return nil, err
	}

	if skipped {
		s.archiveGatedDraft(ctx, inv.ID)
		return nil, ierr.NewError("checkout requires a non-zero invoice").
			WithHint("The invoice computed to zero and cannot be gated behind a payment").
			Mark(ierr.ErrValidation)
	}

	// Credits are debited at compute, so voiding before archiving is what returns them.
	if inv.AmountDue.LessThanOrEqual(decimal.Zero) {
		if _, err := s.VoidInvoice(ctx, inv.ID, dto.InvoiceVoidRequest{}); err != nil {
			s.Logger.Error(ctx, "failed to void fully-credited gated invoice",
				"error", err, "invoice_id", inv.ID)
		}
		s.archiveGatedDraft(ctx, inv.ID)
		return nil, ierr.NewError("checkout requires a non-zero invoice; prepaid credits cover the full amount").
			WithHint("Remove the checkout object to issue this invoice without a payment link").
			WithReportableDetails(map[string]any{"amount_due": inv.AmountDue.String()}).
			Mark(ierr.ErrValidation)
	}

	// An idempotency key returns the same draft, and CreatePaymentForCheckout keys off
	// {checkout_invoice_id, gateway}, so a second session over it would collide.
	existing, err := s.CheckoutSessionRepo.List(ctx, &types.CheckoutSessionFilter{
		QueryFilter:        types.NewNoLimitQueryFilter(),
		CheckoutInvoiceIDs: []string{inv.ID},
		CheckoutStatuses:   types.ActiveCheckoutStatuses(),
	})
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		return s.gatedInvoiceResponse(ctx, inv.ID, dto.ToCheckoutSessionResponse(existing[0]))
	}

	sessionResp, err := NewCheckoutSessionService(s.ServiceParams).StartPayFirstCheckoutSession(ctx, &dto.PayFirstCheckoutRequest{
		CustomerID: inv.CustomerID,
		Action:     types.CheckoutActionPayInvoice,
		Configuration: types.CheckoutConfiguration{
			PayInvoiceParams: &types.PayInvoiceParams{InvoiceID: inv.ID},
		},
		DraftInvoice: &inv.Invoice,
		Checkout:     req.Checkout,
	})
	if err != nil {
		return nil, err
	}

	return s.gatedInvoiceResponse(ctx, inv.ID, sessionResp)
}

// gatedInvoiceResponse re-reads the invoice so taxes, customer and line items are populated
// as they are on the ungated path.
func (s *invoiceService) gatedInvoiceResponse(ctx context.Context, invoiceID string, session *dto.CheckoutSessionResponse) (*dto.InvoiceResponse, error) {
	resp, err := s.GetInvoice(ctx, invoiceID)
	if err != nil {
		return nil, err
	}
	
	return resp.WithCheckoutSession(session), nil
}

func (s *invoiceService) archiveGatedDraft(ctx context.Context, invoiceID string) {
	if err := s.InvoiceRepo.Delete(ctx, invoiceID); err != nil {
		s.Logger.Error(ctx, "failed to archive gated draft invoice",
			"error", err, "invoice_id", invoiceID)
	}
}
