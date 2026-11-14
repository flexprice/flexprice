package razorpay

import (
	"context"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"time"

	"github.com/flexprice/flexprice/internal/clock"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/samber/lo"
)

const enachSetupLinkExpiry = 7 * 24 * time.Hour

type PaymentMethodAdapter struct {
	client          RazorpayClient
	customerSvc     RazorpayCustomerService
	flexCustomerSvc interfaces.CustomerService
}

func NewPaymentMethodAdapter(client RazorpayClient, customerSvc RazorpayCustomerService, flexCustomerSvc interfaces.CustomerService) *PaymentMethodAdapter {
	return &PaymentMethodAdapter{client: client, customerSvc: customerSvc, flexCustomerSvc: flexCustomerSvc}
}

func (a *PaymentMethodAdapter) ListSavedMethods(ctx context.Context, customerID string) ([]interfaces.ProviderPaymentMethod, error) {
	_, tokens, err := a.customerSvc.ListCustomerTokens(ctx, customerID)
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
	c, err := a.customerSvc.EnsureCustomerSyncedToRazorpay(ctx, req.CustomerID, a.flexCustomerSvc)
	if err != nil {
		return nil, err
	}
	if missing := missingMandateContact(c); missing != "" {
		return nil, ierr.NewErrorf("customer has no %s", missing).
			WithHintf("Add %s to the customer. Razorpay needs an email and a phone number to set up a mandate.", missing).
			WithReportableDetails(map[string]interface{}{"customer_id": req.CustomerID}).
			Mark(ierr.ErrValidation)
	}

	linkReq := AuthorizationLinkRequest{
		Customer:                 linkCustomer(c),
		Currency:                 "INR",
		Description:              "Bank account mandate (eNACH)",
		ExpireBy:                 clock.Now().Add(enachSetupLinkExpiry).Unix(),
		Notes:                    map[string]interface{}{"flexprice_customer_id": req.CustomerID},
		SubscriptionRegistration: SubscriptionRegistration{Method: "emandate"},
	}
	if req.MaxAmount != nil {
		linkReq.SubscriptionRegistration.MaxAmount = toPaise(*req.MaxAmount)
	}

	link, err := createCustomerLink(ctx, a.client, a.customerSvc, c, linkReq)
	if err != nil {
		return nil, err
	}
	if err := a.customerSvc.SaveSetupLink(ctx, req.CustomerID, link.ID); err != nil {
		return nil, err
	}
	return link.toSetupLink(), nil
}

// missingMandateContact names what the customer lacks for a mandate: "an email", "a phone number", or both.
func missingMandateContact(c *customer.Customer) string {
	switch noEmail, noPhone := c.Email == "", lo.FromPtr(c.Contact) == ""; {
	case noEmail && noPhone:
		return "an email and a phone number"
	case noEmail:
		return "an email"
	case noPhone:
		return "a phone number"
	default:
		return ""
	}
}

func errMandateManagedAtCheckout() error {
	return ierr.NewError("razorpay saved payment methods cannot be managed here").
		WithHint("Razorpay mandates are set up at checkout").
		Mark(ierr.ErrValidation)
}
