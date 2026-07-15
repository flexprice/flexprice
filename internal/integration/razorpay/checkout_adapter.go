package razorpay

import (
	"context"

	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
)

// CheckoutAdapter wraps PaymentService to implement interfaces.CheckoutProvider.
type CheckoutAdapter struct {
	Svc         *PaymentService
	CustomerSvc interfaces.CustomerService
	InvoiceSvc  interfaces.InvoiceService
}

func (a *CheckoutAdapter) CreatePaymentLink(
	ctx context.Context,
	req interfaces.CheckoutProviderRequest,
) (*interfaces.CheckoutProviderResponse, error) {
	r, err := a.Svc.CreatePaymentLink(ctx, &CreatePaymentLinkRequest{
		InvoiceID:  req.InvoiceID,
		CustomerID: req.CustomerID,
		Amount:     req.Amount,
		Currency:   req.Currency,
		SuccessURL: req.SuccessURL,
		CancelURL:  req.CancelURL,
		Metadata:   req.Metadata,
		PaymentID:  req.PaymentID,
	}, a.CustomerSvc, a.InvoiceSvc)
	if err != nil {
		return nil, err
	}
	return &interfaces.CheckoutProviderResponse{
		ProviderSessionID: r.ID,
		NextAction:        types.PaymentAction{Type: types.PaymentActionTypePaymentLink, URL: r.PaymentURL},
	}, nil
}
