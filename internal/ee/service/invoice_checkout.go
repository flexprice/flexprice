package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// createPayGatedOneOffInvoice creates the one-off invoice as a computed DRAFT and opens a
// hosted checkout session over it. It finalizes only when the payment webhook lands.
func (s *invoiceService) createPayGatedOneOffInvoice(ctx context.Context, req dto.CreateInvoiceRequest) (*dto.InvoiceResponse, error) {
	draft, err := s.CreateEmptyDraftInvoice(ctx, req.ToDraftRequest())
	if err != nil {
		return nil, err
	}

	// A repeated idempotency key returns the same draft; a second session over it would
	// collide on CreatePaymentForCheckout's {checkout_invoice_id, gateway} key.
	existing, err := s.CheckoutSessionRepo.List(ctx, &types.CheckoutSessionFilter{
		QueryFilter:        types.NewNoLimitQueryFilter(),
		CheckoutInvoiceIDs: []string{draft.ID},
		CheckoutStatuses:   types.ActiveCheckoutStatuses(),
	})
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		return s.gatedInvoiceResponse(ctx, draft.ID, dto.ToCheckoutSessionResponse(existing[0]))
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

	// Credits are debited at compute, so the void is what returns them.
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

// gatedInvoiceResponse re-reads the invoice so taxes, customer and line items are populated.
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

// activeCheckoutSessionForInvoice returns the session gating this invoice, or nil.
// At most one is active per invoice.
func (s *invoiceService) activeCheckoutSessionForInvoice(ctx context.Context, invoiceID string) (*domainCheckout.CheckoutSession, error) {
	filter := types.NewNoLimitQueryFilter()
	filter.Limit = lo.ToPtr(1)

	sessions, err := s.CheckoutSessionRepo.List(ctx, &types.CheckoutSessionFilter{
		QueryFilter:        filter,
		CheckoutInvoiceIDs: []string{invoiceID},
		CheckoutStatuses:   types.ActiveCheckoutStatuses(),
	})
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, nil
	}
	return sessions[0], nil
}

// errInvoiceCheckoutGated rejects a manual state change while a checkout session owns the
// invoice: acting mid-session strands a live payment link over an unpayable invoice.
func errInvoiceCheckoutGated(invoiceID, operation string) error {
	return ierr.NewError("invoice is gated by an active checkout session").
		WithHintf("Cancel the checkout session before you %s this invoice", operation).
		WithReportableDetails(map[string]any{"invoice_id": invoiceID, "operation": operation}).
		Mark(ierr.ErrValidation)
}
