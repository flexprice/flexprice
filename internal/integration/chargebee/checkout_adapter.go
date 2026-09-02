package chargebee

import (
	"context"
	"fmt"
	"time"

	invoiceModel "github.com/chargebee/chargebee-go/v3/models/invoice"
	invoiceEnum "github.com/chargebee/chargebee-go/v3/models/invoice/enum"
	transactionEnum "github.com/chargebee/chargebee-go/v3/models/transaction/enum"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type CheckoutAdapter struct {
	Client      ChargebeeClient
	CustomerSvc *CustomerService
	InvoiceSvc  *InvoiceService
	Logger      *logger.Logger
}

// CreatePaymentLink returns a Chargebee-hosted checkout page for an exact ad-hoc
// amount.
func (a *CheckoutAdapter) CreatePaymentLink(
	ctx context.Context,
	req interfaces.CheckoutProviderRequest,
) (*interfaces.CheckoutProviderResponse, error) {
	return a.hostedCheckout(ctx, req.CustomerID, req.PaymentID, req.InvoiceID, req.Currency, req.Amount, req.SuccessURL)
}

// CreateAuthorizationLink is the same hosted page.
func (a *CheckoutAdapter) CreateAuthorizationLink(
	ctx context.Context,
	req interfaces.AuthorizationLinkRequest,
) (*interfaces.CheckoutProviderResponse, error) {
	return a.hostedCheckout(ctx, req.CustomerID, req.PaymentID, req.InvoiceID, req.Currency, req.Amount, req.SuccessURL)
}

func (a *CheckoutAdapter) hostedCheckout(
	ctx context.Context,
	flexCustomerID, flexPaymentID, flexInvoiceID, currency string,
	amount decimal.Decimal,
	successURL string,
) (*interfaces.CheckoutProviderResponse, error) {
	cust, err := a.CustomerSvc.EnsureCustomerSyncedToChargebee(ctx, flexCustomerID)
	if err != nil {
		return nil, err
	}
	cbCustomerID := cust.Metadata["chargebee_customer_id"]
	if cbCustomerID == "" {
		if cbCustomerID, err = a.CustomerSvc.GetChargebeeCustomerID(ctx, flexCustomerID); err != nil {
			return nil, err
		}
	}

	cfg, err := a.Client.GetChargebeeConfig(ctx)
	if err != nil {
		return nil, err
	}

	page, err := a.Client.CreateCheckoutOneTimePage(
		ctx,
		cbCustomerID,
		currency,
		amountToMinorUnits(amount, currency),
		fmt.Sprintf("Flexprice invoice %s", flexInvoiceID),
		successURL,
		cfg.GatewayAccountID,
		flexPaymentID,
	)
	if err != nil {
		return nil, err
	}
	if page.Url == "" {
		return nil, missingPayload("hosted page url")
	}

	a.Logger.Info(ctx, "created chargebee hosted checkout page",
		"hosted_page_id", page.Id,
		"flexprice_invoice_id", flexInvoiceID,
		"flexprice_payment_id", flexPaymentID,
		"expires_at", page.ExpiresAt)

	resp := &interfaces.CheckoutProviderResponse{
		ProviderSessionID: page.Id,
		NextAction: types.PaymentAction{
			Type: types.PaymentActionTypePaymentLink,
			URL:  page.Url,
		},
		ProviderMetadata: map[string]string{
			"chargebee_customer_id": cbCustomerID,
			"chargebee_hosted_page": page.Id,
		},
	}
	if page.ExpiresAt > 0 {
		resp.ExpiresAt = lo.ToPtr(time.Unix(page.ExpiresAt, 0).UTC())
	}
	return resp, nil
}

// TryAutoChargingSavedMethod charges the customer's stored card off-session.
//
// Unattended, creating the invoice with auto-collection on charges it in the same
// call, which is also how Chargebee books it as merchant-initiated. Attended, the
// charge has to be its own call: only collect_payment can declare it
// customer-initiated.
func (a *CheckoutAdapter) TryAutoChargingSavedMethod(
	ctx context.Context,
	req interfaces.AuthorizationLinkRequest,
) (*interfaces.CheckoutProviderResponse, bool, error) {
	cust, err := a.CustomerSvc.EnsureCustomerSyncedToChargebee(ctx, req.CustomerID)
	if err != nil {
		return nil, false, err
	}
	cbCustomerID := cust.Metadata["chargebee_customer_id"]
	if cbCustomerID == "" {
		if cbCustomerID, err = a.CustomerSvc.GetChargebeeCustomerID(ctx, req.CustomerID); err != nil {
			return nil, false, err
		}
	}

	inv, err := a.Client.CreateAdHocInvoice(
		ctx,
		cbCustomerID,
		req.Currency,
		amountToMinorUnits(req.Amount, req.Currency),
		fmt.Sprintf("Flexprice invoice %s", req.InvoiceID),
		req.PaymentID,
		idempotencyScoped(req.PaymentID, "invoice"),
		true,
		req.CustomerPresent,
	)
	if err != nil {
		// Collecting on creation makes a customer with no card fail the create itself,
		// so no invoice exists to abandon and there is nothing to charge.
		if IsNoPaymentMethod(err) {
			a.Logger.Info(ctx, "chargebee auto-charge: no card on file to collect against",
				"customer_id", req.CustomerID, "invoice_id", req.InvoiceID)
			return nil, false, nil
		}
		return nil, false, err
	}
	if inv.Id == "" {
		return nil, false, missingPayload("invoice id")
	}

	// The payment_succeeded webhook finds its way back through this mapping.
	if err := a.InvoiceSvc.LinkInvoiceMapping(ctx, req.InvoiceID, inv.Id); err != nil {
		a.Logger.Error(ctx, "failed to map chargebee invoice",
			"error", err, "flexprice_invoice_id", req.InvoiceID, "chargebee_invoice_id", inv.Id)
	}

	settled, failed := lastLinkedPayment(inv)
	switch {
	case inv.Status == invoiceEnum.StatusPaid || settled != nil:
		return a.responseFromAutoCollect(ctx, req, inv, settled), true, nil

	case failed != nil:
		a.voidAbandonedInvoice(ctx, inv.Id, "off-session charge declined")
		return nil, false, ierr.NewError("chargebee off-session charge did not succeed").
			WithHintf("The saved payment method was declined (transaction status %s)", failed.TxnStatus).
			WithReportableDetails(map[string]any{
				"transaction_status":   failed.TxnStatus,
				"chargebee_invoice_id": inv.Id,
				"invoice_id":           req.InvoiceID,
			}).
			Mark(ierr.ErrHTTPClient)

	default:
		a.Logger.Info(ctx, "chargebee auto-charge: invoice created but nothing was collected",
			"customer_id", req.CustomerID, "invoice_id", req.InvoiceID,
			"chargebee_invoice_id", inv.Id, "status", inv.Status)
		a.voidAbandonedInvoice(ctx, inv.Id, "nothing collected on the mirrored invoice")
		return nil, false, nil
	}
}

func (a *CheckoutAdapter) responseFromAutoCollect(
	ctx context.Context,
	req interfaces.AuthorizationLinkRequest,
	inv *invoiceModel.Invoice,
	settled *invoiceModel.LinkedPayment,
) *interfaces.CheckoutProviderResponse {
	a.Logger.Info(ctx, "chargebee collected the invoice on creation",
		"chargebee_invoice_id", inv.Id,
		"invoice_id", req.InvoiceID,
		"status", inv.Status)

	meta := map[string]string{"chargebee_invoice_id": inv.Id}
	resp := &interfaces.CheckoutProviderResponse{
		ProviderSessionID: inv.Id,
		ProviderMetadata:  meta,
	}
	if settled != nil {
		resp.ProviderPaymentIntentID = settled.TxnId
		meta["transaction_status"] = string(settled.TxnStatus)
	}
	return resp
}

// voidAbandonedInvoice closes a mirror nobody will pay. Best effort: the caller is
// already falling back, and a failure here only leaves an unpaid invoice behind.
func (a *CheckoutAdapter) voidAbandonedInvoice(ctx context.Context, chargebeeInvoiceID, reason string) {
	if err := a.Client.VoidInvoice(ctx, chargebeeInvoiceID, reason); err != nil {
		a.Logger.Error(ctx, "failed to void abandoned chargebee invoice",
			"error", err, "chargebee_invoice_id", chargebeeInvoiceID, "reason", reason)
	}
}

func lastLinkedPayment(inv *invoiceModel.Invoice) (settled, failed *invoiceModel.LinkedPayment) {
	for _, p := range inv.LinkedPayments {
		if p == nil {
			continue
		}
		switch classifyTransaction(p.TxnStatus) {
		case transactionSettled:
			settled = p
		case transactionFailed:
			failed = p
		}
	}
	return settled, failed
}

// amountToMinorUnits shifts the decimal to the currency's minor unit. Going via
// float64 (as convertAmountToSmallestUnit does) can misround a value money must
// carry exactly.
func amountToMinorUnits(amount decimal.Decimal, currency string) int64 {
	return amount.Shift(types.GetCurrencyPrecision(currency)).Round(0).IntPart()
}

// idempotencyScoped namespaces an idempotency key per Chargebee operation.
// Chargebee treats the key as global, so the same key on two different endpoints
// is rejected with 422 unable_to_process_request.
func idempotencyScoped(key, op string) string {
	if key == "" {
		return ""
	}
	return key + ":" + op
}

// transactionOutcome is the three-way reading of a Chargebee transaction status.
// Treating everything non-success as failure is wrong: ACH and SEPA sit in
// in_progress for days and settle normally.
type transactionOutcome int

const (
	transactionSettled transactionOutcome = iota
	transactionPending
	transactionFailed
)

func classifyTransaction(status transactionEnum.Status) transactionOutcome {
	switch status {
	case transactionEnum.StatusSuccess:
		return transactionSettled
	case transactionEnum.StatusFailure, transactionEnum.StatusLateFailure,
		transactionEnum.StatusTimeout, transactionEnum.StatusVoided:
		return transactionFailed
	default:
		// in_progress, needs_attention, and anything Chargebee adds later: the
		// webhook settles it, and session expiry (plus the late-payment refund)
		// bounds the wait.
		return transactionPending
	}
}
