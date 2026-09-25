package stripe

import (
	"context"
	"strings"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	stripeapi "github.com/stripe/stripe-go/v82"
)

// CheckoutAdapter wraps Stripe payment services to implement interfaces.CheckoutProvider.
type CheckoutAdapter struct {
	Client      *Client
	PaymentSvc  *PaymentService
	CustomerSvc interfaces.CustomerService
	InvoiceSvc  interfaces.InvoiceService
	Logger      *logger.Logger
}

var _ interfaces.CheckoutProvider = (*CheckoutAdapter)(nil)

// CreatePaymentLink creates a hosted Stripe checkout session for one-time payment.
func (a *CheckoutAdapter) CreatePaymentLink(
	ctx context.Context,
	req interfaces.CheckoutProviderRequest,
) (*interfaces.CheckoutProviderResponse, error) {
	if a == nil || a.PaymentSvc == nil {
		return nil, ierr.NewError("stripe checkout adapter is not configured").
			Mark(ierr.ErrInternal)
	}

	linkResp, err := a.PaymentSvc.CreatePaymentLink(ctx, &dto.CreateStripePaymentLinkRequest{
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

// CreateAuthorizationLink creates a Stripe checkout session configured to save the card
// and make it the default payment method for future off-session charges.
func (a *CheckoutAdapter) CreateAuthorizationLink(
	ctx context.Context,
	req interfaces.AuthorizationLinkRequest,
) (*interfaces.CheckoutProviderResponse, error) {
	if a == nil || a.PaymentSvc == nil {
		return nil, ierr.NewError("stripe checkout adapter is not configured").
			Mark(ierr.ErrInternal)
	}

	linkResp, err := a.PaymentSvc.CreatePaymentLink(ctx, &dto.CreateStripePaymentLinkRequest{
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

// TryAutoChargingSavedMethod attempts an off-session charge against a customer's stored card.
// Returns charged=false if the customer has no usable payment method on file.
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

	pmID := methods[0].ID
	if stripeCust, err := a.getStripeCustomer(ctx, req.CustomerID); err == nil &&
		stripeCust != nil &&
		stripeCust.InvoiceSettings != nil &&
		stripeCust.InvoiceSettings.DefaultPaymentMethod != nil &&
		stripeCust.InvoiceSettings.DefaultPaymentMethod.ID != "" {
		pmID = stripeCust.InvoiceSettings.DefaultPaymentMethod.ID
	}

	chargeResp, err := a.PaymentSvc.ChargeSavedPaymentMethod(ctx, &dto.ChargeSavedPaymentMethodRequest{
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

// FetchPaymentState reads payment state from Stripe given gateway tracking/payment handles.
func (a *CheckoutAdapter) FetchPaymentState(
	ctx context.Context,
	req interfaces.PaymentStateRequest,
) (*interfaces.PaymentState, error) {
	if a == nil || a.Client == nil {
		return nil, ierr.NewError("stripe checkout adapter is not configured").
			Mark(ierr.ErrNotImplemented)
	}

	if req.GatewayPaymentID == "" && req.GatewayTrackingID == "" {
		return nil, nil
	}

	stripeClient, _, err := a.Client.GetStripeClient(ctx)
	if err != nil {
		return nil, err
	}

	// 1. GatewayPaymentID preferred (e.g. pi_...)
	paymentID := req.GatewayPaymentID
	if paymentID != "" && strings.HasPrefix(paymentID, "pi_") {
		pi, err := stripeClient.V1PaymentIntents.Retrieve(ctx, paymentID, nil)
		if err != nil {
			return nil, err
		}
		var status types.PaymentStatus
		switch pi.Status {
		case stripeapi.PaymentIntentStatusSucceeded:
			status = types.PaymentStatusSucceeded
		case stripeapi.PaymentIntentStatusCanceled:
			status = types.PaymentStatusFailed
		default:
			status = ""
		}
		return &interfaces.PaymentState{
			Status:           status,
			GatewayPaymentID: pi.ID,
		}, nil
	}

	// 2. GatewayTrackingID (cs_... or pi_...)
	handle := req.GatewayTrackingID
	switch {
	case strings.HasPrefix(handle, "pi_"):
		return a.FetchPaymentState(ctx, interfaces.PaymentStateRequest{GatewayPaymentID: handle})

	case strings.HasPrefix(handle, "cs_"):
		session, err := stripeClient.V1CheckoutSessions.Retrieve(ctx, handle, nil)
		if err != nil {
			return nil, err
		}
		var status types.PaymentStatus
		piID := ""
		if session.PaymentIntent != nil {
			piID = session.PaymentIntent.ID
		}
		switch session.Status {
		case stripeapi.CheckoutSessionStatusComplete:
			status = types.PaymentStatusSucceeded
		case stripeapi.CheckoutSessionStatusExpired:
			status = types.PaymentStatusFailed
		default:
			status = ""
		}
		return &interfaces.PaymentState{
			Status:           status,
			GatewayPaymentID: piID,
		}, nil

	default:
		return nil, nil
	}
}

func (a *CheckoutAdapter) getStripeCustomer(ctx context.Context, flexCustomerID string) (*stripeapi.Customer, error) {
	ourCustResp, err := a.CustomerSvc.GetCustomer(ctx, flexCustomerID)
	if err != nil || ourCustResp == nil || ourCustResp.Customer == nil {
		return nil, err
	}

	stripeCustomerID := ourCustResp.Customer.Metadata["stripe_customer_id"]
	if stripeCustomerID == "" {
		return nil, ierr.NewError("customer not synced to Stripe").Mark(ierr.ErrNotFound)
	}

	stripeClient, _, err := a.Client.GetStripeClient(ctx)
	if err != nil {
		return nil, err
	}

	return stripeClient.V1Customers.Retrieve(ctx, stripeCustomerID, nil)
}
