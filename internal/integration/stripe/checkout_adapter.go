package stripe

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// CheckoutAdapter wraps PaymentService to implement interfaces.CheckoutProvider.
type CheckoutAdapter struct {
	Svc *PaymentService
}

func (a *CheckoutAdapter) CreatePaymentLink(
	ctx context.Context,
	req interfaces.CheckoutProviderRequest,
	customerSvc interfaces.CustomerService,
	invoiceSvc interfaces.InvoiceService,
) (*interfaces.CheckoutProviderResponse, error) {
	amount, err := decimal.NewFromString(req.Amount)
	if err != nil {
		return nil, err
	}
	r, err := a.Svc.CreatePaymentLink(ctx, &dto.CreateStripePaymentLinkRequest{
		InvoiceID:     req.InvoiceID,
		CustomerID:    req.CustomerID,
		Amount:        amount,
		Currency:      req.Currency,
		SuccessURL:    req.SuccessURL,
		CancelURL:     req.CancelURL,
		Metadata:      req.Metadata,
		EnvironmentID: req.EnvironmentID,
		PaymentID:     req.PaymentID,
	}, customerSvc, invoiceSvc)
	if err != nil {
		return nil, err
	}
	return &interfaces.CheckoutProviderResponse{
		ProviderSessionID:       r.ID,
		NextAction:              types.PaymentAction{Type: types.PaymentActionTypeCheckoutURL, URL: r.PaymentURL},
		ProviderPaymentIntentID: r.PaymentIntentID,
	}, nil
}
