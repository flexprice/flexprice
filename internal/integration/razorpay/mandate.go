package razorpay

import (
	"context"
	"strings"
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

// NormalizeRazorpayToken converts a raw token object (as returned by
// Client.GetCustomerTokens) into the generic interfaces.ProviderPaymentMethod shape.
// Returns nil, nil for non-confirmed tokens so callers skip them cleanly.
func NormalizeRazorpayToken(raw map[string]interface{}) (*interfaces.ProviderPaymentMethod, error) {
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

// razorpaySubscriptionMethod maps a FlexPrice PaymentMethodType to the Razorpay
// subscription_registration "method" value. Empty input defaults to "upi".
func razorpaySubscriptionMethod(pm types.PaymentMethodType) (string, error) {
	switch pm {
	case "", types.PaymentMethodTypeUPI:
		return "upi", nil
	case types.PaymentMethodTypeCard:
		return "card", nil
	default:
		return "", ierr.NewErrorf("razorpay authorization link registration does not support method %q", pm).
			WithHint("Only UPI and Card are supported for Razorpay mandate registration").
			Mark(ierr.ErrNotImplemented)
	}
}

// CreateAuthorizationLink registers a UPI Autopay or card recurring-payment
// mandate combined with the first invoice payment.
func (a *CheckoutAdapter) CreateAuthorizationLink(
	ctx context.Context,
	req interfaces.AuthorizationLinkRequest,
) (*interfaces.CheckoutProviderResponse, error) {
	method, err := razorpaySubscriptionMethod(req.PreferredMethod)
	if err != nil {
		return nil, err
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
	// Razorpay requires a contact number for recurring/subscription-registration links.
	if c.Contact != nil && *c.Contact != "" {
		customerInfo["contact"] = *c.Contact
	}

	data := map[string]interface{}{
		"customer":     customerInfo,
		"type":         "link",
		"amount":       toPaise(req.Amount),
		"currency":     strings.ToUpper(req.Currency),
		"description":  "Subscription authorization",
		"receipt":      req.InvoiceID,
		"email_notify": true,
		"sms_notify":   true,
		"notes": map[string]interface{}{
			"flexprice_customer_id": req.CustomerID,
			"flexprice_payment_id":  req.PaymentID,
		},
	}

	subReg := map[string]interface{}{"method": method}
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
	if shortURL == "" || id == "" {
		return nil, ierr.NewError("razorpay authorization link response missing short_url or id").
			WithHint("Razorpay returned an unexpected response for the mandate registration link").
			WithReportableDetails(map[string]interface{}{
				"has_short_url": shortURL != "",
				"has_id":        id != "",
			}).
			Mark(ierr.ErrInternal)
	}

	return &interfaces.CheckoutProviderResponse{
		ProviderSessionID: id,
		NextAction:        types.PaymentAction{Type: types.PaymentActionTypePaymentLink, URL: shortURL},
	}, nil
}
