package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
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
	if err := validateInvoiceIsPayable(inv); err != nil {
		return nil, err
	}
	if err := s.shouldAllowToPayInvoice(ctx, inv); err != nil {
		return nil, err
	}

	gateway, err := NewPaymentProviderResolver(s.ServiceParams).
		ResolveProvider(ctx, customerID, types.IntegrationCapabilityPaymentLink, lo.FromPtr(req.PaymentProvider))
	if err != nil {
		return nil, err
	}

	// A live link for this invoice is reusable: without this a second tap creates a
	// second full-amount link, and two links can both be paid.
	if existing, err := s.livePaymentLink(ctx, inv.ID, gateway); err != nil {
		return nil, err
	} else if existing != nil {
		return existing, nil
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

	payResp, err := NewPaymentService(s.ServiceParams).CreatePayment(ctx, payReq)
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

// livePaymentLink returns the payment for an unexpired link already issued for this
// invoice on this gateway, or nil when there is none to reuse. A link whose payment
// carries no URL is not reusable — there would be nothing to send the customer to.
func (s *customerPortalService) livePaymentLink(
	ctx context.Context,
	invoiceID string,
	gateway types.PaymentGatewayType,
) (*dto.PortalPayInvoiceResponse, error) {
	filter := types.NewNoLimitPaymentFilter()
	filter.DestinationType = lo.ToPtr(string(types.PaymentDestinationTypeInvoice))
	filter.DestinationID = lo.ToPtr(invoiceID)
	filter.PaymentMethodType = lo.ToPtr(string(types.PaymentMethodTypePaymentLink))
	filter.PaymentStatus = lo.ToPtr(string(types.PaymentStatusPending))
	filter.PaymentGateway = lo.ToPtr(string(gateway))
	filter.Limit = lo.ToPtr(1)

	payments, err := NewPaymentService(s.ServiceParams).ListPayments(ctx, filter)
	if err != nil {
		return nil, err
	}
	if payments == nil || len(payments.Items) == 0 {
		return nil, nil
	}

	latest := payments.Items[0]
	action := paymentActionFrom(latest)
	if action == nil {
		return nil, nil
	}

	return &dto.PortalPayInvoiceResponse{
		PaymentID:     latest.ID,
		InvoiceID:     invoiceID,
		Status:        latest.PaymentStatus,
		Amount:        latest.Amount,
		Currency:      latest.Currency,
		PaymentAction: action,
	}, nil
}

func validateInvoiceIsPayable(inv *dto.InvoiceResponse) error {
	if inv.InvoiceStatus == types.InvoiceStatusDraft {
		return ierr.NewError("invoice is not finalized").
			WithHint("This invoice cannot be paid yet").
			Mark(ierr.ErrInvalidOperation)
	}
	if inv.InvoiceStatus == types.InvoiceStatusVoided {
		return ierr.NewError("invoice is voided").
			WithHint("This invoice cannot be paid").
			Mark(ierr.ErrInvalidOperation)
	}

	switch inv.PaymentStatus {
	case types.PaymentStatusPending, types.PaymentStatusFailed:
	default:
		return ierr.NewError("invoice is not awaiting payment").
			WithHintf("This invoice is %s", inv.PaymentStatus).
			WithReportableDetails(map[string]any{"payment_status": inv.PaymentStatus}).
			Mark(ierr.ErrInvalidOperation)
	}

	if inv.AmountRemaining.LessThanOrEqual(decimal.Zero) {
		return ierr.NewError("invoice has nothing left to pay").
			WithHint("This invoice is already settled").
			Mark(ierr.ErrInvalidOperation)
	}

	return nil
}

// shouldAllowToPayInvoice blocks only the case that actually breaks: an
// invoice whose payment is supposed to activate a subscription still sitting
// unactivated. The link path settles the invoice but skips
// HandleIncompleteSubscriptionPayment, so the money would be taken and the
// subscription left behind.
//
// Deliberately narrow. An already-active subscription makes that hook a no-op
// (ActivateIncompleteSubscription returns early unless the status is incomplete),
// so those invoices stay payable, as do renewals and one-off invoices.
//
// TODO: delete once ReconcileInvoicePayment delegates to ReconcilePaymentStatus.
func (s *customerPortalService) shouldAllowToPayInvoice(ctx context.Context, inv *dto.InvoiceResponse) error {
	reason := types.InvoiceBillingReason(inv.BillingReason)
	if inv.SubscriptionID == nil || !reason.IsFirstSubscriptionOpenInvoiceReason() {
		return nil
	}

	sub, err := s.SubRepo.Get(ctx, lo.FromPtr(inv.SubscriptionID))
	if err != nil {
		return err
	}

	activationPending := sub.SubscriptionStatus == types.SubscriptionStatusIncomplete
	if reason == types.InvoiceBillingReasonSubscriptionTrialEnd {
		activationPending = sub.SubscriptionStatus != types.SubscriptionStatusActive
	}
	if !activationPending {
		return nil
	}

	return ierr.NewError("this invoice cannot be paid from the portal yet").
		WithHint("Contact support to activate this subscription").
		WithReportableDetails(map[string]any{
			"subscription_id":     lo.FromPtr(inv.SubscriptionID),
			"subscription_status": sub.SubscriptionStatus,
		}).
		Mark(ierr.ErrInvalidOperation)
}

func paymentActionFrom(payResp *dto.PaymentResponse) *types.PaymentAction {
	if payResp.GatewayMetadata == nil {
		return nil
	}
	url := payResp.GatewayMetadata["payment_url"]
	if url == "" {
		return nil
	}
	return &types.PaymentAction{Type: types.PaymentActionTypePaymentLink, URL: url}
}
