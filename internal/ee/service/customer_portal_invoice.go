package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// PayInvoice collects against an invoice that already exists. Deliberately not a
// checkout session: nothing here is created that would need rolling back, and a
// session would put a real customer invoice inside machinery that archives the
// invoices it owns.
func (s *customerPortalService) PayInvoice(ctx context.Context, invoiceID string, req *dto.PortalPayInvoiceRequest) (*dto.PortalPayInvoiceResponse, error) {
	if req == nil {
		return nil, ierr.NewError("request is required").Mark(ierr.ErrValidation)
	}

	customerID, err := s.portalCustomerID(ctx)
	if err != nil {
		return nil, err
	}

	// GetInvoice is the ownership check; it is already scoped to the portal customer.
	inv, err := s.GetInvoice(ctx, invoiceID)
	if err != nil {
		return nil, err
	}
	if err := NewInvoiceService(s.ServiceParams).CheckInvoicePayable(ctx, inv); err != nil {
		return nil, err
	}

	gateway, err := NewPaymentProviderResolver(s.ServiceParams).
		ResolveProvider(ctx, customerID, types.IntegrationCapabilityPaymentLink, lo.FromPtr(req.PaymentProvider))
	if err != nil {
		return nil, err
	}

	payReq := &dto.CreatePaymentRequest{
		IdempotencyKey:    lo.FromPtr(req.IdempotencyKey),
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     inv.ID,
		PaymentMethodType: types.PaymentMethodTypePaymentLink,
		PaymentGateway:    lo.ToPtr(gateway),
		Amount:            inv.AmountRemaining,
		Currency:          inv.Currency,
		SuccessURL:        lo.FromPtr(req.SuccessURL),
		CancelURL:         lo.FromPtr(req.CancelURL),
		ProcessPayment:    true,
	}

	payResp, err := s.paymentService.GetOrCreateInvoicePaymentLink(ctx, payReq)
	if err != nil {
		return nil, err
	}

	return &dto.PortalPayInvoiceResponse{
		PaymentID:     payResp.ID,
		InvoiceID:     inv.ID,
		Status:        payResp.PaymentStatus,
		Amount:        payResp.Amount,
		Currency:      payResp.Currency,
		PaymentAction: paymentActionFrom(payResp),
	}, nil
}
