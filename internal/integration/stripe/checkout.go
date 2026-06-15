package stripe

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/checkout"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
)

// StripeCheckoutProvider implements checkout.CheckoutProvider for Stripe.
type StripeCheckoutProvider struct {
	payment  *PaymentService
	customer interfaces.CustomerService
	invoice  interfaces.InvoiceService
}

func NewStripeCheckoutProvider(p *PaymentService, c interfaces.CustomerService, i interfaces.InvoiceService) *StripeCheckoutProvider {
	return &StripeCheckoutProvider{payment: p, customer: c, invoice: i}
}

var _ checkout.CheckoutProvider = (*StripeCheckoutProvider)(nil)

// stripeModeForObjective maps the checkout objective to the Stripe Checkout Session mode.
func stripeModeForObjective(o types.CheckoutObjective) string {
	if o == types.CheckoutObjectiveSetup {
		return "setup"
	}
	return "payment"
}

func (s *StripeCheckoutProvider) CreateCheckoutSession(ctx context.Context, req checkout.CheckoutSessionRequest) (*checkout.CheckoutSessionResponse, error) {
	switch req.Objective {
	case types.CheckoutObjectivePayment:
		return s.createPaymentSession(ctx, req)
	case types.CheckoutObjectiveSetup:
		return s.createSetupSession(ctx, req)
	default:
		return nil, ierr.NewError("unsupported checkout objective").
			WithHint("Objective must be 'payment' or 'setup'").
			WithReportableDetails(map[string]any{"objective": req.Objective}).
			Mark(ierr.ErrValidation)
	}
}

func (s *StripeCheckoutProvider) createPaymentSession(ctx context.Context, req checkout.CheckoutSessionRequest) (*checkout.CheckoutSessionResponse, error) {
	metadata := types.Metadata{}
	for k, v := range req.Metadata {
		metadata[k] = v
	}
	metadata["flexprice_checkout_id"] = req.CheckoutID

	resp, err := s.payment.CreatePaymentLink(ctx, &dto.CreateStripePaymentLinkRequest{
		InvoiceID:              req.InvoiceID,
		CustomerID:             req.CustomerID,
		Amount:                 req.Amount,
		Currency:               req.Currency,
		SuccessURL:             req.SuccessURL,
		CancelURL:              req.CancelURL,
		Metadata:               metadata,
		SaveCardAndMakeDefault: req.SaveCard,
		EnvironmentID:          types.GetEnvironmentID(ctx),
		PaymentID:              req.PaymentID,
	}, s.customer, s.invoice)
	if err != nil {
		return nil, err
	}
	return &checkout.CheckoutSessionResponse{
		SessionID:       resp.ID,
		URL:             resp.PaymentURL,
		PaymentIntentID: resp.PaymentIntentID,
		Status:          resp.Status,
	}, nil
}

// createSetupSession is implemented in Task 3 (next).
func (s *StripeCheckoutProvider) createSetupSession(ctx context.Context, req checkout.CheckoutSessionRequest) (*checkout.CheckoutSessionResponse, error) {
	return nil, ierr.NewError("setup checkout not implemented").
		WithHint("Setup-mode checkout is implemented in Task 3").
		Mark(ierr.ErrInvalidOperation)
}
