package stripe

import (
	"context"
	"strings"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/types/integrations"
	stripeapi "github.com/stripe/stripe-go/v82"
)

// Stripe id prefixes: the prefix is what says which endpoint can read a stored handle.
const (
	paymentIntentIDPrefix   = "pi_"
	checkoutSessionIDPrefix = "cs_"
)

// CheckoutAdapter wraps Stripe payment services to implement interfaces.CheckoutProvider.
type CheckoutAdapter struct {
	Client      *Client
	PaymentSvc  *PaymentService
	CustomerSvc interfaces.CustomerService
	InvoiceSvc  interfaces.InvoiceService
	Logger      *logger.Logger
}

// CreatePaymentLink creates a hosted Stripe checkout session for one-time payment.
func (a *CheckoutAdapter) CreatePaymentLink(
	ctx context.Context,
	req interfaces.CheckoutProviderRequest,
) (*interfaces.CheckoutProviderResponse, error) {
	if a == nil || a.PaymentSvc == nil {
		return nil, ierr.NewError("stripe checkout adapter is not configured").
			Mark(ierr.ErrInternal)
	}

	linkResp, err := a.PaymentSvc.CreatePaymentLink(ctx, &CreateStripePaymentLinkRequest{
		InvoiceID:              req.InvoiceID,
		CustomerID:             req.CustomerID,
		Amount:                 req.Amount,
		Currency:               req.Currency,
		SuccessURL:             req.SuccessURL,
		CancelURL:              req.CancelURL,
		Metadata:               req.Metadata,
		SaveCardAndMakeDefault: false,
		PaymentID:              req.PaymentID,
		ExpiresAt:              req.ExpiresAt,
	}, a.CustomerSvc, a.InvoiceSvc)
	if err != nil {
		return nil, err
	}

	return &interfaces.CheckoutProviderResponse{
		ProviderSessionID: linkResp.ID,
		NextAction: types.PaymentAction{
			Type: types.PaymentActionTypePaymentLink,
			URL:  linkResp.PaymentURL,
		},
		ProviderPaymentIntentID: linkResp.PaymentIntentID,
		ExpiresAt:               linkResp.ExpiresAt,
	}, nil
}

// CreateAuthorizationLink creates a Stripe checkout session that vaults the card as the
// customer's default for future off-session charges.
func (a *CheckoutAdapter) CreateAuthorizationLink(
	ctx context.Context,
	req interfaces.AuthorizationLinkRequest,
) (*interfaces.CheckoutProviderResponse, error) {
	if a == nil || a.PaymentSvc == nil {
		return nil, ierr.NewError("stripe checkout adapter is not configured").
			Mark(ierr.ErrInternal)
	}

	linkResp, err := a.PaymentSvc.CreatePaymentLink(ctx, &CreateStripePaymentLinkRequest{
		InvoiceID:              req.InvoiceID,
		CustomerID:             req.CustomerID,
		Amount:                 req.Amount,
		Currency:               req.Currency,
		SuccessURL:             req.SuccessURL,
		CancelURL:              req.CancelURL,
		Metadata:               req.Metadata,
		SaveCardAndMakeDefault: true,
		PaymentID:              req.PaymentID,
		ExpiresAt:              req.ExpiresAt,
	}, a.CustomerSvc, a.InvoiceSvc)
	if err != nil {
		return nil, err
	}

	return &interfaces.CheckoutProviderResponse{
		ProviderSessionID: linkResp.ID,
		NextAction: types.PaymentAction{
			Type: types.PaymentActionTypePaymentLink,
			URL:  linkResp.PaymentURL,
		},
		ProviderPaymentIntentID: linkResp.PaymentIntentID,
		ExpiresAt:               linkResp.ExpiresAt,
	}, nil
}

// TryAutoChargingSavedMethod attempts an off-session charge; charged=false if the
// customer has no usable saved payment method.
func (a *CheckoutAdapter) TryAutoChargingSavedMethod(
	ctx context.Context,
	req interfaces.AuthorizationLinkRequest,
) (*interfaces.CheckoutProviderResponse, bool, error) {
	if a == nil || a.PaymentSvc == nil || a.CustomerSvc == nil || req.CustomerID == "" {
		return nil, false, nil
	}

	methods, err := a.PaymentSvc.GetCustomerPaymentMethods(ctx, &dto.GetCustomerPaymentMethodsRequest{
		CustomerID: req.CustomerID,
	}, a.CustomerSvc)
	if err != nil {
		a.Logger.Info(ctx, "stripe auto-charge: failed to get customer payment methods, falling back to auth link",
			"customer_id", req.CustomerID, "error", err)
		return nil, false, nil
	}

	if len(methods) == 0 {
		return nil, false, nil
	}

	// The customer's default in Stripe wins over list order; without one, the first card.
	pmID := methods[0].ID
	if stripeCust, err := a.PaymentSvc.customerSvc.RetrieveStripeCustomer(ctx, req.CustomerID, a.CustomerSvc); err == nil {
		if defaultID := defaultPaymentMethodID(stripeCust); defaultID != "" {
			pmID = defaultID
		}
	}

	chargeResp, err := a.PaymentSvc.ChargeSavedPaymentMethod(ctx, &ChargeSavedPaymentMethodRequest{
		CustomerID:      req.CustomerID,
		InvoiceID:       req.InvoiceID,
		PaymentMethodID: pmID,
		Amount:          req.Amount,
		Currency:        req.Currency,
		PaymentID:       req.PaymentID,
	}, a.CustomerSvc, a.InvoiceSvc)
	if err != nil {
		return nil, false, err
	}

	return &interfaces.CheckoutProviderResponse{
		ProviderSessionID:       chargeResp.ID,
		ProviderPaymentIntentID: chargeResp.ID,
	}, true, nil
}

// HasAutoChargeableMethod checks if the customer has any usable saved payment methods on Stripe.
func (a *CheckoutAdapter) HasAutoChargeableMethod(ctx context.Context, req interfaces.HasAutoChargeableMethodRequest) (bool, error) {
	if a == nil || a.PaymentSvc == nil || a.CustomerSvc == nil || req.CustomerID == "" {
		return false, nil
	}
	return a.PaymentSvc.HasSavedPaymentMethods(ctx, req.CustomerID, a.CustomerSvc)
}

// FetchPaymentState reads payment state given gateway tracking/payment handles. The
// session (cs_) is read first when present: read alone, its intent looks declined
// (requires_payment_method) until the customer actually pays.
func (a *CheckoutAdapter) FetchPaymentState(
	ctx context.Context,
	req interfaces.PaymentStateRequest,
) (*interfaces.PaymentState, error) {
	if a == nil || a.Client == nil {
		return nil, ierr.NewError("stripe checkout adapter is not configured").
			Mark(ierr.ErrNotImplemented)
	}

	paymentID := req.GatewayPaymentID
	trackingID := req.GatewayTrackingID
	if paymentID == "" && trackingID == "" {
		return nil, nil
	}

	stripeClient, _, err := a.Client.GetStripeClient(ctx)
	if err != nil {
		return nil, err
	}

	switch {
	case strings.HasPrefix(trackingID, checkoutSessionIDPrefix):
		return a.checkoutSessionPaymentState(ctx, stripeClient, trackingID)
	case strings.HasPrefix(paymentID, paymentIntentIDPrefix):
		return a.paymentIntentPaymentState(ctx, stripeClient, paymentID)
	default:
		// Never guess an endpoint from an id we do not recognise.
		return nil, ierr.NewError("unrecognised stripe checkout handle").
			WithHint("The stored gateway ids do not match any Stripe object this adapter can read").
			WithReportableDetails(map[string]interface{}{
				"gateway_payment_id":  paymentID,
				"gateway_tracking_id": trackingID,
			}).
			Mark(ierr.ErrValidation)
	}
}

func (a *CheckoutAdapter) paymentIntentPaymentState(ctx context.Context, stripeClient *stripeapi.Client, paymentIntentID string) (*interfaces.PaymentState, error) {
	pi, err := stripeClient.V1PaymentIntents.Retrieve(ctx, paymentIntentID, nil)
	if err != nil {
		return nil, err
	}

	status, err := integrations.StripePaymentStatus(pi.Status).ToFlexpricePaymentStatus()
	if err != nil {
		return nil, err
	}

	return &interfaces.PaymentState{
		Status:           status,
		GatewayPaymentID: pi.ID,
	}, nil
}

func (a *CheckoutAdapter) checkoutSessionPaymentState(ctx context.Context, stripeClient *stripeapi.Client, sessionID string) (*interfaces.PaymentState, error) {
	session, err := stripeClient.V1CheckoutSessions.Retrieve(ctx, sessionID, nil)
	if err != nil {
		return nil, err
	}

	status, err := integrations.StripeCheckoutSessionStatus(session.Status).
		ToFlexpricePaymentStatus(integrations.StripeCheckoutPaymentStatus(session.PaymentStatus))
	if err != nil {
		return nil, err
	}

	piID := ""
	if session.PaymentIntent != nil {
		piID = session.PaymentIntent.ID
	}

	return &interfaces.PaymentState{
		Status:           status,
		GatewayPaymentID: piID,
	}, nil
}
