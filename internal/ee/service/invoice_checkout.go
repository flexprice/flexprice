package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// startCheckoutOnOneOffInvoice creates the invoice as a computed DRAFT behind a checkout
// session. It finalizes only when the payment webhook lands.
func (s *invoiceService) startCheckoutOnOneOffInvoice(ctx context.Context, req dto.CreateInvoiceRequest) (*dto.InvoiceResponse, error) {
	req.SourceType = types.InvoiceSourceTypeCheckout

	draft, err := s.CreateEmptyDraftInvoice(ctx, req.ToDraftRequest())
	if err != nil {
		return nil, err
	}

	// A repeated idempotency key returns the same draft; a second session over it would
	// collide on the payment's {checkout_invoice_id, gateway} key.
	gatedSession, _, err := s.isInvoiceGatedOnCheckout(ctx, &draft.Invoice, "")
	if err != nil {
		return nil, err
	}
	if gatedSession != nil {
		return s.gatedInvoiceResponse(ctx, draft.ID, dto.ToCheckoutSessionResponse(gatedSession))
	}

	computeReq := req.ToComputeRequest()
	inv, skipped, err := s.ComputeInvoice(ctx, draft.ID, &computeReq)
	if err != nil {
		return nil, err
	}

	if skipped {
		s.archiveGatedDraft(ctx, inv.ID)
		return nil, ierr.NewError("checkout requires a non-zero invoice").
			WithHint("The invoice computed to zero and cannot be gated behind a payment").
			Mark(ierr.ErrValidation)
	}

	// Credits are debited at compute; the void is what returns them.
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

	checkoutSvc := NewCheckoutSessionService(s.ServiceParams)
	sessionResp, err := checkoutSvc.StartPayFirstCheckoutSession(ctx, &dto.PayFirstCheckoutRequest{
		CustomerID: inv.CustomerID,
		Action:     types.CheckoutActionPayInvoice,
		Configuration: types.CheckoutConfiguration{
			PayInvoiceParams: &types.PayInvoiceParams{InvoiceID: inv.ID},
		},
		DraftInvoice: inv,
		Checkout:     req.Checkout,
	})
	if err != nil {
		return nil, err
	}

	return s.gatedInvoiceResponse(ctx, inv.ID, sessionResp)
}

// gatedInvoiceResponse re-reads so taxes, customer and line items are populated.
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

// isInvoiceGatedOnCheckout reports the session gating this invoice and whether it is the caller's own.
// source_type short-circuits the query for invoices no checkout created. Callers admit the
// owning session: it finalizes and voids while still non-terminal.
func (s *invoiceService) isInvoiceGatedOnCheckout(ctx context.Context, inv *invoice.Invoice, callerSessionID string) (*domainCheckout.CheckoutSession, bool, error) {
	if inv == nil || inv.SourceType != types.InvoiceSourceTypeCheckout {
		return nil, false, nil
	}

	gatedSession, err := s.CheckoutSessionRepo.GetByCheckoutInvoiceID(ctx, inv.ID)
	if err != nil || gatedSession == nil {
		return nil, false, err
	}

	return gatedSession, gatedSession.ID == callerSessionID, nil
}

func errInvoiceCheckoutGated(invoiceID, operation string) error {
	return ierr.NewError("invoice is gated by an active checkout session").
		WithHintf("Cancel the checkout session before you %s this invoice", operation).
		WithReportableDetails(map[string]any{"invoice_id": invoiceID, "operation": operation}).
		Mark(ierr.ErrValidation)
}
