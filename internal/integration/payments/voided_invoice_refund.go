package payments

import (
	"context"

	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// LateCaptureRefunder is implemented by each gateway's PaymentService.
type LateCaptureRefunder interface {
	RefundLateCapturedPayment(ctx context.Context, flexpricePaymentID, gatewayPaymentID string, paymentService interfaces.PaymentService) error
}

// RefundIfInvoiceVoidedParams are the inputs to RefundIfInvoiceVoided.
type RefundIfInvoiceVoidedParams struct {
	PaymentService     interfaces.PaymentService
	InvoiceService     interfaces.InvoiceService
	Refunder           LateCaptureRefunder
	FlexpricePaymentID string
	GatewayPaymentID   string
	Logger             *logger.Logger
}

// RefundIfInvoiceVoided refunds a payment whose invoice was voided, instead of
// settling it. handled=true means the caller must not settle.
func RefundIfInvoiceVoided(ctx context.Context, p RefundIfInvoiceVoidedParams) (handled bool) {
	pay, err := p.PaymentService.GetPayment(ctx, p.FlexpricePaymentID)
	if err != nil || pay == nil || pay.DestinationType != types.PaymentDestinationTypeInvoice {
		return false
	}

	switch pay.PaymentStatus {
	case types.PaymentStatusSucceeded, types.PaymentStatusOverpaid, types.PaymentStatusPartiallyRefunded:
		return false
	}

	inv, err := p.InvoiceService.GetInvoice(ctx, pay.DestinationID)
	if err != nil || inv == nil || inv.InvoiceStatus != types.InvoiceStatusVoided {
		return false
	}

	p.Logger.Info(ctx, "payment captured on voided invoice, refunding",
		"flexprice_payment_id", p.FlexpricePaymentID,
		"gateway_payment_id", p.GatewayPaymentID,
		"invoice_id", pay.DestinationID)

	if err := p.Refunder.RefundLateCapturedPayment(ctx, p.FlexpricePaymentID, p.GatewayPaymentID, p.PaymentService); err != nil {
		p.Logger.Error(ctx, "failed to refund payment on voided invoice, manual reconciliation required",
			"error", err,
			"flexprice_payment_id", p.FlexpricePaymentID,
			"gateway_payment_id", p.GatewayPaymentID,
			"invoice_id", pay.DestinationID)
	}
	return true
}
