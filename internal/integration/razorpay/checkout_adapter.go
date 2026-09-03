package razorpay

import (
	"context"
	"strings"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/types/integrations"
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
		ExpiresAt:  req.ExpiresAt,
	}, a.CustomerSvc, a.InvoiceSvc)
	if err != nil {
		return nil, err
	}
	return &interfaces.CheckoutProviderResponse{
		ProviderSessionID: r.ID,
		ExpiresAt:         r.ExpiresAt,
		NextAction:        types.PaymentAction{Type: types.PaymentActionTypePaymentLink, URL: r.PaymentURL},
	}, nil
}

// Razorpay object id prefixes. Which handle a checkout holds depends on how it was
// created — send_invoice gives plink_, an unsaved mandate gives inv_, a saved-token
// charge gives pay_ (and order_ when a retry reused an existing order). Each is
// answered by a different endpoint, which is why routing lives here.
const (
	paymentLinkPrefix = "plink_"
	invoicePrefix     = "inv_"
	orderPrefix       = "order_"
	paymentPrefix     = "pay_"
)

// FetchPaymentState implements interfaces.CheckoutProvider. pay_ is the source of
// truth; the other handles are scaffolding that exists only until someone pays, so a
// known payment id is always preferred.
func (a *CheckoutAdapter) FetchPaymentState(
	ctx context.Context,
	req interfaces.PaymentStateRequest,
) (*interfaces.PaymentState, error) {
	if a == nil || a.Svc == nil {
		return nil, ierr.NewError("razorpay checkout adapter is not configured").
			Mark(ierr.ErrNotImplemented)
	}

	if paymentID := req.GatewayPaymentID; paymentID != "" {
		rawStatus, err := a.Svc.GetPaymentStatus(ctx, paymentID)
		if err != nil {
			return nil, err
		}
		status, err := integrations.RazorpayPaymentStatus(rawStatus).ToFlexpricePaymentStatus()
		if err != nil {
			// "created" and "authorized" are in-flight, not outcomes.
			a.Svc.logger.Debug(ctx, "razorpay payment status is not an outcome",
				"razorpay_payment_id", paymentID, "status", rawStatus)
			return &interfaces.PaymentState{GatewayPaymentID: paymentID}, nil
		}
		return &interfaces.PaymentState{Status: status, GatewayPaymentID: paymentID}, nil
	}

	handle := req.GatewayTrackingID
	switch {
	case strings.HasPrefix(handle, paymentPrefix):
		// A saved-token charge with no order records the payment id as its only handle.
		return a.FetchPaymentState(ctx, interfaces.PaymentStateRequest{GatewayPaymentID: handle})

	case strings.HasPrefix(handle, paymentLinkPrefix):
		link, err := a.Svc.GetPaymentLinkStatus(ctx, handle)
		if err != nil {
			return nil, err
		}
		status, err := integrations.RazorpayPaymentLinkStatus(link.Status).ToFlexpricePaymentStatus()
		if err != nil {
			return nil, err
		}
		return &interfaces.PaymentState{Status: status, GatewayPaymentID: link.RazorpayPaymentID}, nil

	case strings.HasPrefix(handle, invoicePrefix):
		inv, err := a.Svc.GetInvoiceStatus(ctx, handle)
		if err != nil {
			return nil, err
		}
		status, err := integrations.RazorpayInvoiceStatus(inv.Status).ToFlexpricePaymentStatus()
		if err != nil {
			return nil, err
		}
		return &interfaces.PaymentState{Status: status, GatewayPaymentID: inv.RazorpayPaymentID}, nil

	case strings.HasPrefix(handle, orderPrefix):
		order, err := a.Svc.GetOrderStatus(ctx, handle)
		if err != nil {
			return nil, err
		}
		status, err := integrations.RazorpayOrderStatus(order.Status).ToFlexpricePaymentStatus()
		if err != nil {
			return nil, err
		}
		return &interfaces.PaymentState{Status: status, GatewayPaymentID: order.RazorpayPaymentID}, nil

	default:
		// Never guess an endpoint from an id we do not recognise.
		return nil, ierr.NewError("unrecognised razorpay checkout handle").
			WithHint("The stored gateway tracking ID does not match any known Razorpay object").
			WithReportableDetails(map[string]interface{}{"gateway_tracking_id": handle}).
			Mark(ierr.ErrValidation)
	}
}
