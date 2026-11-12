package razorpay

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/clock"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/samber/lo"
)

const enachSetupLinkExpiry = 7 * 24 * time.Hour

type PaymentMethodAdapter struct {
	Client          RazorpayClient
	CustomerSvc     RazorpayCustomerService
	FlexCustomerSvc interfaces.CustomerService
}

func (a *PaymentMethodAdapter) ListSavedMethods(ctx context.Context, customerID string) ([]interfaces.ProviderPaymentMethod, error) {
	_, tokens, err := a.CustomerSvc.ListCustomerTokens(ctx, customerID)
	if err != nil {
		if ierr.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	return lo.FilterMap(tokens, func(t *interfaces.ProviderPaymentMethod, _ int) (interfaces.ProviderPaymentMethod, bool) {
		if t == nil {
			return interfaces.ProviderPaymentMethod{}, false
		}

		return *t, true
	}), nil
}

func (a *PaymentMethodAdapter) DeleteSavedMethod(ctx context.Context, customerID, methodID string) error {
	return errMandateManagedAtCheckout()
}

func (a *PaymentMethodAdapter) SetDefaultSavedMethod(ctx context.Context, customerID, methodID string) error {
	return errMandateManagedAtCheckout()
}

// CreateSetupLink returns a ₹0 eNACH registration link; Razorpay doesn't redirect these, so ReturnURL is unused.
func (a *PaymentMethodAdapter) CreateSetupLink(ctx context.Context, req interfaces.SetupLinkRequest) (*interfaces.SetupLinkResponse, error) {
	c, err := a.CustomerSvc.EnsureCustomerSyncedToRazorpay(ctx, req.CustomerID, a.FlexCustomerSvc)
	if err != nil {
		return nil, err
	}
	if c.Email == "" || lo.FromPtr(c.Contact) == "" {
		return nil, ierr.NewError("customer needs an email and a phone number to set up a bank mandate").
			WithHint("Add an email and a phone number to the customer; Razorpay requires both for eNACH").
			WithReportableDetails(map[string]interface{}{"customer_id": req.CustomerID}).
			Mark(ierr.ErrValidation)
	}

	linkReq := AuthorizationLinkRequest{
		Customer:                 linkCustomer(c),
		Currency:                 "INR",
		Description:              "Bank account mandate (eNACH)",
		ExpireBy:                 clock.Now().Add(enachSetupLinkExpiry).Unix(),
		EmailNotify:              true,
		SMSNotify:                true,
		SubscriptionRegistration: SubscriptionRegistration{Method: "emandate"},
	}
	if req.MaxAmount != nil {
		linkReq.SubscriptionRegistration.MaxAmount = toPaise(*req.MaxAmount)
	}

	link, err := a.Client.CreateAuthorizationLink(ctx, linkReq)
	if err != nil {
		return nil, err
	}
	if err := a.CustomerSvc.SaveSetupLink(ctx, req.CustomerID, link.ID); err != nil {
		return nil, err
	}
	return link.toSetupLink(), nil
}

func errMandateManagedAtCheckout() error {
	return ierr.NewError("razorpay saved payment methods cannot be managed here").
		WithHint("Razorpay mandates are set up at checkout").
		Mark(ierr.ErrValidation)
}
