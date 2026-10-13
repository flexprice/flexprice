package razorpay

import (
	"context"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/samber/lo"
)

type PaymentMethodAdapter struct {
	CustomerSvc RazorpayCustomerService
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

func (a *PaymentMethodAdapter) CreateSetupLink(ctx context.Context, req interfaces.SetupLinkRequest) (*interfaces.SetupLinkResponse, error) {
	return nil, errMandateManagedAtCheckout()
}

func errMandateManagedAtCheckout() error {
	return ierr.NewError("razorpay saved payment methods cannot be managed here").
		WithHint("Razorpay mandates are set up at checkout").
		Mark(ierr.ErrValidation)
}
