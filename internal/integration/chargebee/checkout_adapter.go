package chargebee

import (
	"context"
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
	CustomerSvc ChargebeeCustomerService
	InvoiceSvc  ChargebeeInvoiceService
	Logger      *logger.Logger
}

// checkoutRequest normalizes the two interface requests the hosted page serves.
type checkoutRequest struct {
	flexCustomerID string
	flexPaymentID  string
	flexInvoiceID  string
	currency       string
	amount         decimal.Decimal
	successURL     string
	lineItems      []interfaces.CheckoutLineItem
}

func (a *CheckoutAdapter) CreatePaymentLink(
	ctx context.Context,
	req interfaces.CheckoutProviderRequest,
) (*interfaces.CheckoutProviderResponse, error) {
	return a.hostedCheckout(ctx, checkoutRequest{
		flexCustomerID: req.CustomerID,
		flexPaymentID:  req.PaymentID,
		flexInvoiceID:  req.InvoiceID,
		currency:       req.Currency,
		amount:         req.Amount,
		successURL:     req.SuccessURL,
		lineItems:      req.LineItems,
	})
}

func (a *CheckoutAdapter) CreateAuthorizationLink(
	ctx context.Context,
	req interfaces.AuthorizationLinkRequest,
) (*interfaces.CheckoutProviderResponse, error) {
	return a.hostedCheckout(ctx, checkoutRequest{
		flexCustomerID: req.CustomerID,
		flexPaymentID:  req.PaymentID,
		flexInvoiceID:  req.InvoiceID,
		currency:       req.Currency,
		amount:         req.Amount,
		successURL:     req.SuccessURL,
		lineItems:      req.LineItems,
	})
}

func (a *CheckoutAdapter) hostedCheckout(
	ctx context.Context,
	req checkoutRequest,
) (*interfaces.CheckoutProviderResponse, error) {
	cust, err := a.CustomerSvc.EnsureCustomerSyncedToChargebee(ctx, req.flexCustomerID)
	if err != nil {
		return nil, err
	}
	cbCustomerID := cust.Metadata["chargebee_customer_id"]
	if cbCustomerID == "" {
		if cbCustomerID, err = a.CustomerSvc.GetChargebeeCustomerID(ctx, req.flexCustomerID); err != nil {
			return nil, err
		}
	}

	cfg, err := a.Client.GetChargebeeConfig(ctx)
	if err != nil {
		return nil, err
	}

	charges, err := a.getLineItems(ctx, req.lineItems, req.amount, req.currency, req.flexInvoiceID)
	if err != nil {
		return nil, err
	}

	page, err := a.Client.CreateHostedCheckoutPage(ctx, HostedCheckoutPageRequest{
		ChargebeeCustomerID: cbCustomerID,
		Currency:            req.currency,
		Charges:             charges,
		RedirectURL:         req.successURL,
		GatewayAccountID:    cfg.GatewayAccountID,
		InvoiceNote:         PaymentNote(req.flexPaymentID),
	})
	if err != nil {
		return nil, err
	}
	if page.Url == "" {
		return nil, missingPayload("hosted page url")
	}

	a.Logger.Info(ctx, "created chargebee hosted checkout page",
		"hosted_page_id", page.Id,
		"flexprice_invoice_id", req.flexInvoiceID,
		"flexprice_payment_id", req.flexPaymentID,
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

	charges, err := a.getLineItems(ctx, req.LineItems, req.Amount, req.Currency, req.InvoiceID)
	if err != nil {
		return nil, false, err
	}

	inv, err := a.Client.CreateAdHocInvoice(ctx, AdHocInvoiceRequest{
		ChargebeeCustomerID: cbCustomerID,
		Currency:            req.Currency,
		Charges:             charges,
		InvoiceNote:         PaymentNote(req.PaymentID),
		IdempotencyKey:      idempotencyScoped(req.PaymentID, "invoice"),
		AutoCollect:         true,
		CustomerPresent:     req.CustomerPresent,
	})
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

	settled, pending, failed := lastLinkedPayment(inv)
	switch {
	case inv.Status == invoiceEnum.StatusPaid || settled != nil:
		a.Logger.Info(ctx, "chargebee collected the invoice on creation",
			"chargebee_invoice_id", inv.Id, "invoice_id", req.InvoiceID, "status", inv.Status)
		return autoCollectResponse(inv, settled), true, nil

	// ACH and SEPA sit in_progress for days. The collection is live, so the invoice
	// must not be voided; the session stays PENDING for the webhook to settle.
	case pending != nil:
		a.Logger.Info(ctx, "chargebee auto-charge: collection in progress",
			"customer_id", req.CustomerID, "invoice_id", req.InvoiceID,
			"chargebee_invoice_id", inv.Id, "transaction_status", pending.TxnStatus)
		return autoCollectResponse(inv, pending), true, nil

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

func autoCollectResponse(
	inv *invoiceModel.Invoice,
	txn *invoiceModel.LinkedPayment,
) *interfaces.CheckoutProviderResponse {
	meta := map[string]string{"chargebee_invoice_id": inv.Id}
	resp := &interfaces.CheckoutProviderResponse{
		ProviderSessionID: inv.Id,
		ProviderMetadata:  meta,
	}

	if txn != nil {
		resp.ProviderPaymentIntentID = txn.TxnId
		meta["transaction_status"] = string(txn.TxnStatus)
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

func lastLinkedPayment(inv *invoiceModel.Invoice) (settled, pending, failed *invoiceModel.LinkedPayment) {
	for _, p := range inv.LinkedPayments {
		if p == nil {
			continue
		}
		switch classifyTransaction(p.TxnStatus) {
		case transactionSettled:
			settled = p
		case transactionPending:
			pending = p
		case transactionFailed:
			failed = p
		}
	}
	return settled, pending, failed
}

func (a *CheckoutAdapter) getLineItems(
	ctx context.Context,
	lineItems []interfaces.CheckoutLineItem,
	amount decimal.Decimal,
	currency, flexInvoiceID string,
) ([]AdHocCharge, error) {
	if len(lineItems) == 0 {
		return nil, ierr.NewError("invoice has no chargeable line items").
			WithHint("The invoice being charged has no line items to itemise").
			WithReportableDetails(map[string]any{"invoice_id": flexInvoiceID}).
			Mark(ierr.ErrValidation)
	}

	total := amountToMinorUnits(amount, currency)
	var sum int64
	charges := make([]AdHocCharge, 0, len(lineItems))
	for _, li := range lineItems {
		minor := amountToMinorUnits(li.Amount, currency)
		sum += minor
		charges = append(charges, AdHocCharge{
			AmountMinor: minor,
			Description: li.Description,
			PeriodStart: li.PeriodStart,
			PeriodEnd:   li.PeriodEnd,
		})
	}

	// Chargebee collects the sum of the lines, not the total we hand it, so a drift
	// here is a customer charged an amount our payment record disagrees with.
	//
	// KNOWN GAP — the comparison below is against the payment amount, which is the
	// wrong base, and a drift only logs rather than stopping the charge.
	//
	// Line item amounts sum to Subtotal, while the payment amount is
	// Subtotal - TotalDiscount + TotalTax - prepaid credits - adjustments. Tax is
	// deliberately not sent: as with Zoho, the tenant configures it on the provider
	// and Chargebee recomputes it over these charges, so an itemised invoice is
	// correct precisely when sum(lines) == amount_due - total_tax. Comparing against
	// the payment amount instead makes this fire on every taxed invoice, which buries
	// the cases that are genuinely wrong:
	//
	//   - discounts, prepaid credits and adjustments reduce amount_due without
	//     touching line item amounts, so Chargebee collects more than is owed.
	//   - per-line rounding to minor units can drift a unit from the once-rounded
	//     total even when nothing else applies.
	//
	// Fixing it means comparing against amount_due - total_tax and falling back to a
	// single charge for the full payment amount when they disagree. That fallback
	// must be sent with Taxable false, since the amount already includes our tax and
	// Chargebee would otherwise tax it a second time.
	if sum != total {
		a.Logger.Error(ctx, "chargebee line items do not sum to the payment amount",
			"error", "line item sum differs from payment amount",
			"flexprice_invoice_id", flexInvoiceID,
			"line_item_count", len(lineItems),
			"line_item_sum_minor", sum,
			"payment_amount_minor", total,
			"currency", currency)
	}
	return charges, nil
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
// in_progress for days and settle normally, so pending keeps the invoice alive and
// the caller waits for the webhook.
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

// FetchPaymentState implements interfaces.CheckoutProvider. Not implemented —
// Chargebee checkouts reconcile by webhook only, so a stale session waits for the
// expiry sweep.
//
// To enable polling: interpret ProviderSessionID (a hosted page id, or an invoice id
// on the authorization path), fetch that object, map its status onto
// types.PaymentStatus (empty for non-terminal), and return the settling transaction id
// as GatewayPaymentID. Nothing outside this adapter changes.
func (a *CheckoutAdapter) FetchPaymentState(
	ctx context.Context,
	req interfaces.PaymentStateRequest,
) (*interfaces.PaymentState, error) {
	return nil, ierr.NewError("chargebee does not support checkout payment state reads").
		WithHint("Chargebee checkout sessions are reconciled by webhook only").
		Mark(ierr.ErrNotImplemented)
}
