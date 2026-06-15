package stripe

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/checkout"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stripe/stripe-go/v82"
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

// createSetupSession creates a Stripe Checkout Session in "setup" mode
// (SetupIntent-backed). It captures a card without charging the customer; the
// resulting checkout.session.completed webhook resolves the flexprice checkout
// via the flexprice_checkout_id metadata.
func (s *StripeCheckoutProvider) createSetupSession(ctx context.Context, req checkout.CheckoutSessionRequest) (*checkout.CheckoutSessionResponse, error) {
	// Acquire the Stripe client (same pattern as PaymentService.CreatePaymentLink, payment.go:63).
	stripeClient, _, err := s.payment.client.GetStripeClient(ctx)
	if err != nil {
		return nil, err
	}

	// Resolve the Stripe customer id from the flexprice customer id
	// (same pattern as payment.go:127-146).
	customerResp, err := s.payment.customerSvc.EnsureCustomerSyncedToStripe(ctx, req.CustomerID, s.customer)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to sync customer to Stripe").
			WithReportableDetails(map[string]interface{}{
				"customer_id": req.CustomerID,
			}).
			Mark(ierr.ErrValidation)
	}

	stripeCustomerID, exists := customerResp.Customer.Metadata["stripe_customer_id"]
	if !exists || stripeCustomerID == "" {
		return nil, ierr.NewError("customer does not have Stripe customer ID after sync").
			WithHint("Failed to sync customer to Stripe").
			WithReportableDetails(map[string]interface{}{
				"customer_id": req.CustomerID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Build session metadata; flexprice_checkout_id is required so the
	// checkout.session.completed webhook can resolve the checkout by id.
	metadata := map[string]string{}
	for k, v := range req.Metadata {
		metadata[k] = v
	}
	metadata["flexprice_checkout_id"] = req.CheckoutID

	// Setup mode differs from payment mode: no LineItems and no PaymentIntentData.
	params := &stripe.CheckoutSessionCreateParams{
		Mode:       stripe.String(stripeModeForObjective(req.Objective)),
		Customer:   stripe.String(stripeCustomerID),
		SuccessURL: stripe.String(req.SuccessURL),
		CancelURL:  stripe.String(req.CancelURL),
		Metadata:   metadata,
	}

	session, err := stripeClient.V1CheckoutSessions.Create(ctx, params)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to create Stripe setup checkout session").
			Mark(ierr.ErrSystem)
	}

	return &checkout.CheckoutSessionResponse{
		SessionID: session.ID,
		URL:       session.URL,
		Status:    string(session.Status),
	}, nil
}
