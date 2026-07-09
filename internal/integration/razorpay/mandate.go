package razorpay

import (
	"context"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// toPaise converts a major-unit decimal (rupees) to paise for Razorpay API calls.
func toPaise(major decimal.Decimal) int64 {
	return major.Mul(decimal.NewFromInt(100)).IntPart()
}

// fromPaise converts a paise float64 (as returned by Razorpay) to a major-unit decimal.
func fromPaise(paise float64) decimal.Decimal {
	return decimal.NewFromFloat(paise).Div(decimal.NewFromInt(100))
}

// normalizeRazorpayToken converts a raw token object (as returned by
// Token.All/Client.GetCustomerTokens) into the generic interfaces.ProviderPaymentMethod
// shape. Returns nil, nil for non-confirmed tokens so callers skip them cleanly.
func normalizeRazorpayToken(raw map[string]interface{}) (*interfaces.ProviderPaymentMethod, error) {
	// Only confirmed tokens are usable; skip anything else.
	details, _ := raw["recurring_details"].(map[string]interface{})
	if status, _ := details["status"].(string); status != "confirmed" {
		return nil, nil
	}

	pm := &interfaces.ProviderPaymentMethod{
		GatewayMethodID:  lo.ValueOr(raw, "id", "").(string),
		ProviderMetadata: map[string]string{},
	}

	method, _ := raw["method"].(string)
	switch method {
	case "upi":
		pm.Method = types.PaymentMethodTypeUPI
	case "card":
		pm.Method = types.PaymentMethodTypeCard
	}

	if paise, ok := raw["max_amount"].(float64); ok && paise > 0 {
		major := fromPaise(paise)
		pm.MaxAmount = &major
	}
	if expiredAtUnix, ok := raw["expired_at"].(float64); ok && expiredAtUnix > 0 {
		t := time.Unix(int64(expiredAtUnix), 0).UTC()
		pm.ExpiresAt = &t
	}
	if createdAtUnix, ok := raw["created_at"].(float64); ok {
		pm.CreatedAt = time.Unix(int64(createdAtUnix), 0).UTC()
	}

	return pm, nil
}

// SelectUsableToken applies the deterministic selection algorithm: filter for
// confirmed, non-expired, matching-method, under-ceiling; pick the newest.
// Exported because both the checkout-time dedup check and the auto-charge path
// need the exact same logic.
func SelectUsableToken(
	methods []*interfaces.ProviderPaymentMethod,
	preferredMethod types.PaymentMethodType,
	invoiceTotal decimal.Decimal,
) (*interfaces.ProviderPaymentMethod, bool) {
	now := time.Now().UTC()
	usable := lo.Filter(methods, func(pm *interfaces.ProviderPaymentMethod, _ int) bool {
		return pm.Method == preferredMethod &&
			(pm.ExpiresAt == nil || !now.After(*pm.ExpiresAt)) &&
			(pm.MaxAmount == nil || !pm.MaxAmount.LessThan(invoiceTotal))
	})
	if len(usable) == 0 {
		return nil, false
	}
	return lo.MaxBy(usable, func(a, b *interfaces.ProviderPaymentMethod) bool {
		return a.CreatedAt.After(b.CreatedAt)
	}), true
}

// ListSavedPaymentMethods resolves the Razorpay customer_id, fetches tokens
// live via Token.All, and normalizes them.
func (a *CheckoutAdapter) ListSavedPaymentMethods(
	ctx context.Context,
	req interfaces.ListSavedPaymentMethodsRequest,
) ([]*interfaces.ProviderPaymentMethod, error) {
	razorpayCustomerID, err := a.Svc.customerSvc.GetRazorpayCustomerID(ctx, req.CustomerID)
	if err != nil {
		return nil, err
	}

	rawTokens, err := a.Svc.client.GetCustomerTokens(ctx, razorpayCustomerID)
	if err != nil {
		return nil, err
	}

	return lo.FilterMap(rawTokens, func(raw map[string]interface{}, _ int) (*interfaces.ProviderPaymentMethod, bool) {
		pm, err := normalizeRazorpayToken(raw)
		return pm, err == nil && pm != nil
	}), nil
}

// CreateAuthorizationLink registers a UPI Autopay mandate combined with the
// first invoice payment.
func (a *CheckoutAdapter) CreateAuthorizationLink(
	ctx context.Context,
	req interfaces.AuthorizationLinkRequest,
) (*interfaces.CheckoutProviderResponse, error) {
	if req.PreferredMethod != "" && req.PreferredMethod != types.PaymentMethodTypeUPI {
		return nil, ierr.NewErrorf("razorpay authorization link registration does not support method %q", req.PreferredMethod).
			WithHint("Only UPI is supported for Razorpay mandate registration in v1").
			Mark(ierr.ErrNotImplemented)
	}

	customerResp, err := a.CustomerSvc.GetCustomer(ctx, req.CustomerID)
	if err != nil {
		return nil, err
	}
	c := customerResp.Customer

	customerInfo := map[string]interface{}{
		"name": c.Name,
	}
	if c.Email != "" {
		customerInfo["email"] = c.Email
	}
	// Note: contact/phone not available in FlexPrice customer model (see payment.go's
	// CreatePaymentLink for the same caveat). VERIFY AT IMPLEMENTATION TIME: unlike
	// CreatePaymentLink's endpoint, docs/prds/razorpau-runbook.md's tested example
	// payloads for subscription_registration/auth_links DO include "contact" — its
	// customer-dedup-on-contact/email claim was specifically confirmed with contact
	// present. Behavior with email-only is unverified; test against Razorpay test mode
	// before relying on this in production.

	data := map[string]interface{}{
		"customer":     customerInfo,
		"type":         "link",
		"amount":       toPaise(req.Amount),
		"currency":     req.Currency,
		"description":  "Subscription authorization",
		"receipt":      req.InvoiceID,
		"email_notify": true,
		"sms_notify":   true,
		"notes": map[string]interface{}{
			"flexprice_customer_id": req.CustomerID,
			"flexprice_payment_id":  req.PaymentID,
		},
	}

	subReg := map[string]interface{}{"method": "upi"}
	if req.MaxAmount != nil {
		subReg["max_amount"] = toPaise(*req.MaxAmount)
	}
	if req.ExpiresAt != nil {
		subReg["expire_at"] = req.ExpiresAt.Unix()
	}
	data["subscription_registration"] = subReg

	result, err := a.Svc.client.CreateAuthorizationLink(ctx, data)
	if err != nil {
		return nil, err
	}

	shortURL, _ := result["short_url"].(string)
	id, _ := result["id"].(string)
	return &interfaces.CheckoutProviderResponse{
		ProviderSessionID: id,
		NextAction:        types.PaymentAction{Type: types.PaymentActionTypePaymentLink, URL: shortURL},
	}, nil
}

// ChargeSavedPaymentMethod charges a specific token via Order + recurring
// Payment. Returns "processing" — the actual capture/failure outcome only
// arrives via webhook (payment.captured/failed); it must NEVER be reported as
// a final success here.
func (a *CheckoutAdapter) ChargeSavedPaymentMethod(
	ctx context.Context,
	req interfaces.ChargeSavedPaymentMethodRequest,
) (*interfaces.ChargeResult, error) {
	razorpayCustomerID, err := a.Svc.customerSvc.GetRazorpayCustomerID(ctx, req.CustomerID)
	if err != nil {
		return nil, err
	}

	amountPaise := toPaise(req.Amount)

	order, err := a.Svc.client.CreateOrder(ctx, map[string]interface{}{
		"amount":          amountPaise,
		"currency":        req.Currency,
		"payment_capture": true,
		"notes": map[string]interface{}{
			"flexprice_invoice_id": req.InvoiceID,
			"flexprice_payment_id": req.PaymentID,
		},
	})
	if err != nil {
		return nil, err
	}
	orderID, _ := order["id"].(string)

	customerResp, err := a.CustomerSvc.GetCustomer(ctx, req.CustomerID)
	if err != nil {
		return nil, err
	}
	c := customerResp.Customer

	paymentData := map[string]interface{}{
		"amount":      amountPaise,
		"currency":    req.Currency,
		"order_id":    orderID,
		"customer_id": razorpayCustomerID,
		"token":       req.GatewayMethodID,
		"recurring":   true,
		"description": "Auto-charge for invoice " + req.InvoiceID,
		"notes": map[string]interface{}{
			"flexprice_invoice_id": req.InvoiceID,
			"flexprice_payment_id": req.PaymentID,
		},
	}
	if c.Email != "" {
		paymentData["email"] = c.Email
	}

	payment, err := a.Svc.client.CreateRecurringPayment(ctx, paymentData)
	if err != nil {
		return nil, err
	}

	paymentID, _ := payment["id"].(string)
	return &interfaces.ChargeResult{
		ProviderPaymentIntentID: paymentID,
	}, nil
}
