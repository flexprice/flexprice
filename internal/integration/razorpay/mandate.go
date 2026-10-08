package razorpay

import (
	"context"
	"encoding/json"
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

func NormalizeRazorpayToken(raw map[string]interface{}) (*interfaces.ProviderPaymentMethod, error) {
	body, err := json.Marshal(raw)
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to read Razorpay token").Mark(ierr.ErrInternal)
	}

	var token Token
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to parse Razorpay token").Mark(ierr.ErrInternal)
	}

	pm := &interfaces.ProviderPaymentMethod{
		GatewayMethodID:  token.ID,
		ProviderMetadata: map[string]string{},
		Active:           token.Status == "" || token.Status == "active",
	}

	switch token.Method {
	case "upi":
		pm.Method = types.PaymentMethodTypeUPI
		pm.UPI = token.upiDetails()
	case "card":
		pm.Method = types.PaymentMethodTypeCard
		pm.Card = token.cardDetails()
	default:
		return nil, nil
	}

	if token.CreatedAt > 0 {
		pm.CreatedAt = time.Unix(token.CreatedAt, 0).UTC()
	}
	if token.Recurring {
		pm.Recurring = token.recurringDetails()
	}

	return pm, nil
}

func (t Token) recurringDetails() *interfaces.ProviderRecurringPaymentDetails {
	status := ""
	if t.RecurringDetails != nil {
		status = t.RecurringDetails.Status
	}
	out := &interfaces.ProviderRecurringPaymentDetails{Status: toRecurringPaymentStatus(status)}

	// Razorpay declines charges on a card whose issuer no longer supports recurring,
	// even while the mandate still reads confirmed.
	if t.cardRecurringUnsupported() && out.Status == types.RecurringPaymentStatusActive {
		out.Status = types.RecurringPaymentStatusRejected
	}

	if t.MaxAmount > 0 {
		major := fromPaise(t.MaxAmount)
		out.MaxAmount = &major
	}

	if t.ExpiredAt > 0 {
		expiresAt := time.Unix(t.ExpiredAt, 0).UTC()
		out.AutoChargeableTill = &expiresAt

		if time.Now().UTC().After(expiresAt) && !lo.Contains([]types.RecurringPaymentStatus{
			types.RecurringPaymentStatusRejected, types.RecurringPaymentStatusCancelled,
		}, out.Status) {
			out.Status = types.RecurringPaymentStatusExpired
		}
	}

	return out
}

func (t Token) cardRecurringUnsupported() bool {
	return t.Card != nil && t.Card.Flows != nil && t.Card.Flows.Recurring != nil && !*t.Card.Flows.Recurring
}

func (t Token) cardDetails() *interfaces.ProviderCardDetails {
	if t.Card == nil {
		return nil
	}

	return &interfaces.ProviderCardDetails{
		Brand:    t.Card.Network,
		Last4:    t.Card.Last4,
		ExpMonth: int(t.Card.ExpiryMonth),
		ExpYear:  int(t.Card.ExpiryYear),
	}
}

func (t Token) upiDetails() *interfaces.ProviderUPIDetails {
	if t.VPA == nil || t.VPA.Username == "" || t.VPA.Handle == "" {
		return nil
	}
	return &interfaces.ProviderUPIDetails{VPA: t.VPA.Username + "@" + t.VPA.Handle}
}

func toRecurringPaymentStatus(status string) types.RecurringPaymentStatus {
	switch status {
	case "initiated":
		return types.RecurringPaymentStatusPending
	case "confirmed":
		return types.RecurringPaymentStatusActive
	case "paused":
		return types.RecurringPaymentStatusPaused
	case "rejected":
		return types.RecurringPaymentStatusRejected
	case "cancelled":
		return types.RecurringPaymentStatusCancelled
	default:
		return types.RecurringPaymentStatusUnknown
	}
}

// SelectUsableToken applies the deterministic selection algorithm: filter for
// active, confirmed, non-expired, matching-method, under-ceiling; pick the newest.
// Exported because both the checkout-time dedup check and the auto-charge path
// need the exact same logic.
func SelectUsableToken(
	methods []*interfaces.ProviderPaymentMethod,
	preferredMethod types.PaymentMethodType,
	invoiceTotal decimal.Decimal,
) (*interfaces.ProviderPaymentMethod, bool) {
	now := time.Now().UTC()
	usable := lo.Filter(methods, func(pm *interfaces.ProviderPaymentMethod, _ int) bool {
		expiresAt, maxAmount := pm.RecurringAutoChargeableTill(), pm.RecurringMaxAmount()

		return pm.Active &&
			pm.RecurringStatus() == types.RecurringPaymentStatusActive &&
			pm.Method == preferredMethod &&
			(expiresAt == nil || !now.After(*expiresAt)) &&
			(maxAmount == nil || !maxAmount.LessThan(invoiceTotal))
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

	c, err := a.Svc.customerSvc.EnsureCustomerSyncedToRazorpay(ctx, req.CustomerID, a.CustomerSvc)
	if err != nil {
		return nil, err
	}

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
	if req.ExpiresAt != nil {
		data["expire_by"] = req.ExpiresAt.Unix()
	}

	// as_presented allows another debit in the same period, still capped by max_amount.
	// A fixed frequency rejects a second charge until that window ends.
	subReg := map[string]interface{}{
		"method":    method,
		"frequency": "as_presented",
	}
	if req.MaxAmount != nil {
		subReg["max_amount"] = toPaise(*req.MaxAmount)
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

// TryAutoChargingSavedMethod implements interfaces.CheckoutProvider by delegating
// to PaymentService.ChargeSavedToken and mapping into CheckoutProviderResponse.
func (a *CheckoutAdapter) TryAutoChargingSavedMethod(
	ctx context.Context,
	req interfaces.AuthorizationLinkRequest,
) (*interfaces.CheckoutProviderResponse, bool, error) {
	if a == nil || a.Svc == nil || a.CustomerSvc == nil {
		return nil, false, nil
	}

	custResp, err := a.CustomerSvc.GetCustomer(ctx, req.CustomerID)
	if err != nil || custResp == nil || custResp.Customer == nil {
		a.Svc.logger.Info(ctx, "checkout auto-charge: failed to load customer, falling back to auth link",
			"customer_id", req.CustomerID, "error", err)
		return nil, false, nil
	}

	result, charged, err := a.Svc.ChargeSavedToken(ctx, ChargeSavedTokenRequest{
		Customer:           custResp.Customer,
		InvoiceID:          req.InvoiceID,
		Amount:             req.Amount,
		Currency:           req.Currency,
		FlexPricePaymentID: req.PaymentID,
		PreferredMethod:    req.PreferredMethod,
	})
	if err != nil {
		return nil, false, err
	}
	if !charged || result == nil {
		return nil, false, nil
	}

	sessionID := result.RazorpayOrderID
	if sessionID == "" {
		sessionID = result.RazorpayPaymentID
	}
	return &interfaces.CheckoutProviderResponse{
		ProviderSessionID:       sessionID,
		ProviderPaymentIntentID: result.RazorpayPaymentID,
		// No NextAction — off-session charge; completion via payment webhook.
	}, true, nil
}

// HasAutoChargeableMethod implements interfaces.CheckoutProvider by checking
// if the customer has any active, non-expired recurring mandate tokens on Razorpay
// that can cover the requested amount (or any active token if amount is nil).
func (a *CheckoutAdapter) HasAutoChargeableMethod(ctx context.Context, req interfaces.HasAutoChargeableMethodRequest) (bool, error) {
	if a == nil || a.Svc == nil || a.Svc.customerSvc == nil || req.CustomerID == "" {
		return false, nil
	}

	_, tokens, err := a.Svc.customerSvc.ListCustomerTokens(ctx, req.CustomerID)
	if err != nil {
		if ierr.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	amount := decimal.Zero
	if req.Amount != nil {
		amount = *req.Amount
	}
	_, ok := selectAutoChargeToken(tokens, "", amount)
	return ok, nil
}
