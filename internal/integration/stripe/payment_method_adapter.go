package stripe

import (
	"context"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	stripeapi "github.com/stripe/stripe-go/v82"
)

// PaymentMethodAdapter wraps Stripe to implement interfaces.PaymentMethodProvider.
type PaymentMethodAdapter struct {
	Client            *Client
	StripeCustomerSvc *CustomerService
	CustomerSvc       interfaces.CustomerService
	Logger            *logger.Logger
}

// ListSavedMethods returns the customer's usable payment methods from Stripe.
func (a *PaymentMethodAdapter) ListSavedMethods(ctx context.Context, flexCustomerID string) ([]interfaces.ProviderPaymentMethod, error) {
	if a == nil || a.Client == nil || a.CustomerSvc == nil || a.StripeCustomerSvc == nil {
		return nil, ierr.NewError("stripe payment method adapter is not configured").
			Mark(ierr.ErrInternal)
	}

	stripeCustomerID, err := a.StripeCustomerSvc.GetStripeCustomerID(ctx, flexCustomerID, a.CustomerSvc)
	if err != nil {
		if ierr.IsNotFound(err) {
			// Not yet synced to Stripe simply means nothing is saved.
			a.Logger.Info(ctx, "customer not synced to Stripe, returning empty saved methods",
				"customer_id", flexCustomerID)
			return nil, nil
		}
		a.Logger.Error(ctx, "failed to get customer for saved payment methods",
			"customer_id", flexCustomerID, "error", err)
		return nil, err
	}

	stripeClient, _, err := a.Client.GetStripeClient(ctx)
	if err != nil {
		return nil, err
	}

	// The default lives on the customer, not the method; a failed read only costs is_default.
	defaultPMID := ""
	if stripeCust, err := stripeClient.V1Customers.Retrieve(ctx, stripeCustomerID, nil); err == nil {
		defaultPMID = defaultPaymentMethodID(stripeCust)
	}

	// Cards only: the rest of this adapter assumes off-session chargeable methods, which
	// bank debits are not.
	params := &stripeapi.PaymentMethodListParams{
		Customer: stripeapi.String(stripeCustomerID),
		Type:     stripeapi.String(string(stripeapi.PaymentMethodTypeCard)),
	}

	now := time.Now()
	out := make([]interfaces.ProviderPaymentMethod, 0)
	for pm, err := range stripeClient.V1PaymentMethods.List(ctx, params) {
		if err != nil {
			a.Logger.Error(ctx, "failed listing stripe payment methods",
				"customer_id", flexCustomerID, "error", err)
			return nil, err
		}
		if pm == nil {
			continue
		}

		var card *interfaces.ProviderCardDetails
		if pm.Card != nil {
			card = &interfaces.ProviderCardDetails{
				Brand:    string(pm.Card.Brand),
				Last4:    pm.Card.Last4,
				ExpMonth: int(pm.Card.ExpMonth),
				ExpYear:  int(pm.Card.ExpYear),
			}
		}

		out = append(out, interfaces.ProviderPaymentMethod{
			GatewayMethodID:     pm.ID,
			Method:              types.PaymentMethodTypeCard,
			CreatedAt:           time.Unix(pm.Created, 0).UTC(),
			IsDefault:           defaultPMID != "" && pm.ID == defaultPMID,
			Active:              paymentMethodUsable(pm, now),
			InstantlyChargeable: true,
			Card:                card,
			GatewayAccountID:    stripeCustomerID,
		})
	}

	return out, nil
}

// DeleteSavedMethod detaches a vaulted payment method after verifying customer ownership.
func (a *PaymentMethodAdapter) DeleteSavedMethod(ctx context.Context, flexCustomerID, gatewayMethodID string) error {
	if _, err := a.validateIfMethodBelongsToCustomer(ctx, flexCustomerID, gatewayMethodID); err != nil {
		return err
	}

	stripeClient, _, err := a.Client.GetStripeClient(ctx)
	if err != nil {
		return err
	}

	_, err = stripeClient.V1PaymentMethods.Detach(ctx, gatewayMethodID, nil)
	return err
}

// SetDefaultSavedMethod sets the customer's default payment method in Stripe.
func (a *PaymentMethodAdapter) SetDefaultSavedMethod(ctx context.Context, flexCustomerID, gatewayMethodID string) error {
	stripeCustomerID, err := a.validateIfMethodBelongsToCustomer(ctx, flexCustomerID, gatewayMethodID)
	if err != nil {
		return err
	}

	stripeClient, _, err := a.Client.GetStripeClient(ctx)
	if err != nil {
		return err
	}

	params := &stripeapi.CustomerUpdateParams{
		InvoiceSettings: &stripeapi.CustomerUpdateInvoiceSettingsParams{
			DefaultPaymentMethod: stripeapi.String(gatewayMethodID),
		},
	}
	_, err = stripeClient.V1Customers.Update(ctx, stripeCustomerID, params)
	return err
}

// CreateSetupLink creates a hosted Stripe Checkout session in setup mode for adding a card.
func (a *PaymentMethodAdapter) CreateSetupLink(ctx context.Context, req interfaces.SetupLinkRequest) (*interfaces.SetupLinkResponse, error) {
	if a == nil || a.Client == nil || a.StripeCustomerSvc == nil || a.CustomerSvc == nil {
		return nil, ierr.NewError("stripe payment method adapter is not configured").
			Mark(ierr.ErrInternal)
	}

	custResp, err := a.StripeCustomerSvc.EnsureCustomerSyncedToStripe(ctx, req.CustomerID, a.CustomerSvc)
	if err != nil {
		return nil, err
	}
	stripeCustomerID := custResp.Customer.Metadata[stripeCustomerIDMetadataKey]

	stripeClient, _, err := a.Client.GetStripeClient(ctx)
	if err != nil {
		return nil, err
	}

	metadata := map[string]string{
		"customer_id":           req.CustomerID,
		"flexprice_customer_id": req.CustomerID,
		"set_default":           "true",
	}

	// Cards only, matching what ListSavedMethods reports back.
	params := &stripeapi.CheckoutSessionCreateParams{
		Customer:           stripeapi.String(stripeCustomerID),
		Mode:               stripeapi.String(string(stripeapi.CheckoutSessionModeSetup)),
		PaymentMethodTypes: stripeapi.StringSlice([]string{string(stripeapi.PaymentMethodTypeCard)}),
		SuccessURL:         stripeapi.String(req.ReturnURL),
		CancelURL:          stripeapi.String(req.ReturnURL),
		Metadata:           metadata,
		SetupIntentData: &stripeapi.CheckoutSessionCreateSetupIntentDataParams{
			Metadata: metadata,
		},
	}

	session, err := stripeClient.V1CheckoutSessions.Create(ctx, params)
	if err != nil {
		return nil, ierr.NewError("failed to create Stripe setup session").
			WithHint("Could not create Stripe checkout session for setup").
			Mark(ierr.ErrSystem)
	}

	resp := &interfaces.SetupLinkResponse{
		URL:               session.URL,
		ProviderSessionID: session.ID,
	}
	if session.ExpiresAt > 0 {
		expiresAt := time.Unix(session.ExpiresAt, 0).UTC()
		resp.ExpiresAt = &expiresAt
	}
	return resp, nil
}

// validateIfMethodBelongsToCustomer returns the customer's Stripe account id after
// confirming the method is vaulted under it.
func (a *PaymentMethodAdapter) validateIfMethodBelongsToCustomer(ctx context.Context, flexCustomerID, gatewayMethodID string) (string, error) {
	if gatewayMethodID == "" {
		return "", ierr.NewError("payment method id is required").
			WithHint("Specify which saved payment method to act on").
			Mark(ierr.ErrValidation)
	}

	if a == nil || a.Client == nil || a.StripeCustomerSvc == nil || a.CustomerSvc == nil {
		return "", ierr.NewError("stripe payment method adapter is not configured").
			Mark(ierr.ErrInternal)
	}

	stripeCustomerID, err := a.StripeCustomerSvc.GetStripeCustomerID(ctx, flexCustomerID, a.CustomerSvc)
	if err != nil {
		return "", err
	}

	stripeClient, _, err := a.Client.GetStripeClient(ctx)
	if err != nil {
		return "", err
	}

	pm, err := stripeClient.V1PaymentMethods.Retrieve(ctx, gatewayMethodID, nil)
	if err != nil {
		if stripeErr, ok := err.(*stripeapi.Error); ok && (stripeErr.HTTPStatusCode == 404 || stripeErr.Code == "resource_missing") {
			return "", ierr.NewError("payment method not found").
				WithHint("The specified payment method does not exist in Stripe").
				Mark(ierr.ErrNotFound)
		}
		return "", err
	}

	if pm.Customer == nil || pm.Customer.ID != stripeCustomerID {
		return "", ierr.NewError("payment method does not belong to customer").
			WithHint("The specified payment method is vaulted under a different customer").
			Mark(ierr.ErrPermissionDenied)
	}

	return stripeCustomerID, nil
}

// paymentMethodUsable reports "active": Stripe has no status field, so this just checks
// a card isn't past its expiry month. Non-card methods have no expiry and are always usable.
func paymentMethodUsable(pm *stripeapi.PaymentMethod, now time.Time) bool {
	if pm == nil {
		return false
	}
	if pm.Card == nil || pm.Card.ExpYear == 0 || pm.Card.ExpMonth == 0 {
		return true
	}

	now = now.UTC()
	year, month := int64(now.Year()), int64(now.Month())
	if pm.Card.ExpYear != year {
		return pm.Card.ExpYear > year
	}
	return pm.Card.ExpMonth >= month
}
