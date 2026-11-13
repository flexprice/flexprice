package razorpay

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/clock"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/payment"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration/payments"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
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
	case "emandate":
		pm.Method = types.PaymentMethodTypeENACH
		pm.BankAccount = token.bankAccountDetails()
	default:
		return nil, nil
	}

	if token.CreatedAt > 0 {
		pm.CreatedAt = time.Unix(token.CreatedAt, 0).UTC()
	}
	// Rejected and cancelled mandates have recurring=false but still carry their status.
	if token.Recurring || token.RecurringDetails != nil && lo.Contains([]string{"rejected", "cancelled"}, token.RecurringDetails.Status) {
		pm.Recurring = token.recurringDetails()
	}

	return pm, nil
}

func (t Token) recurringDetails() *interfaces.ProviderRecurringPaymentDetails {
	status, failureReason := "", ""
	if t.RecurringDetails != nil {
		status, failureReason = t.RecurringDetails.Status, t.RecurringDetails.FailureReason
	}
	out := &interfaces.ProviderRecurringPaymentDetails{Status: toRecurringPaymentStatus(status), FailureReason: failureReason}

	if t.MaxAmount > 0 {
		major := fromPaise(t.MaxAmount)
		out.MaxAmount = &major
	}

	if t.ExpiredAt > 0 {
		expiresAt := time.Unix(t.ExpiredAt, 0).UTC()
		out.AutoChargeableTill = &expiresAt

		if clock.Now().After(expiresAt) && !lo.Contains([]types.RecurringPaymentStatus{
			types.RecurringPaymentStatusRejected, types.RecurringPaymentStatusCancelled,
		}, out.Status) {
			out.Status = types.RecurringPaymentStatusExpired
		}
	}

	return out
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

func (t Token) bankAccountDetails() *interfaces.ProviderBankAccountDetails {
	if t.BankDetails == nil {
		return nil
	}

	accountNumber := t.BankDetails.AccountNumber
	return &interfaces.ProviderBankAccountDetails{
		Bank:        t.Bank,
		IFSC:        t.BankDetails.IFSC,
		Last4:       accountNumber[max(0, len(accountNumber)-4):],
		AccountType: t.BankDetails.AccountType,
		AuthType:    t.AuthType,
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
	now := clock.Now()
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

// NoUsableTokenError reports that the customer has no mandate that can pay p.
func NoUsableTokenError(p *payment.Payment, customerID string) error {
	return ierr.NewError("no usable razorpay token for payment method").
		WithHintf("No active %s mandate covering this amount was found at Razorpay, or the customer is not synced to Razorpay. Collect one first, or use payment_method_type PAYMENT_LINK.", p.PaymentMethodType).
		WithReportableDetails(map[string]interface{}{
			"payment_id":          p.ID,
			"invoice_id":          p.DestinationID,
			"customer_id":         customerID,
			"payment_method_type": p.PaymentMethodType,
			"amount":              p.Amount.String(),
		}).
		Mark(ierr.ErrValidation)
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

// linkCustomer is the customer block of a registration link; Razorpay matches its customer on these details.
func linkCustomer(c *customer.Customer) *CustomerInfo {
	return &CustomerInfo{Name: c.Name, Email: c.Email, Contact: lo.FromPtr(c.Contact)}
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

	linkReq := AuthorizationLinkRequest{
		Customer:    linkCustomer(c),
		Amount:      toPaise(req.Amount),
		Currency:    strings.ToUpper(req.Currency),
		Description: "Subscription authorization",
		Receipt:     req.InvoiceID,
		EmailNotify: true,
		SMSNotify:   true,
		Notes: map[string]interface{}{
			"flexprice_customer_id": req.CustomerID,
			"flexprice_payment_id":  req.PaymentID,
		},
		// as_presented allows another debit in the same period, still capped by max_amount.
		// A fixed frequency rejects a second charge until that window ends.
		SubscriptionRegistration: SubscriptionRegistration{Method: method, Frequency: "as_presented"},
	}
	if req.ExpiresAt != nil {
		linkReq.ExpireBy = req.ExpiresAt.Unix()
	}
	if req.MaxAmount != nil {
		linkReq.SubscriptionRegistration.MaxAmount = toPaise(*req.MaxAmount)
	}

	link, err := createCustomerLink(ctx, a.Svc.client, a.Svc.customerSvc, c, linkReq)
	if err != nil {
		return nil, err
	}

	return &interfaces.CheckoutProviderResponse{
		ProviderSessionID: link.ID,
		NextAction:        types.PaymentAction{Type: types.PaymentActionTypePaymentLink, URL: link.ShortURL},
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

	lock, err := payments.LockInvoiceCharge(ctx, a.Svc.locker, a.Svc.invoiceSyncSvc.paymentRepo, custResp.Customer.EnvironmentID, req.InvoiceID, req.PaymentID)
	if err != nil {
		return nil, false, err
	}
	defer func() {
		if releaseErr := lock.Release(ctx); releaseErr != nil {
			a.Svc.logger.Error(ctx, "failed to release auto-charge lock", "invoice_id", req.InvoiceID, "error", releaseErr)
		}
	}()

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
		PaymentMethod:           result.Method,
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

// createCustomerLink creates a registration link only when Razorpay will attach it to the customer's own
// Razorpay customer; one it still attaches elsewhere is cancelled.
func createCustomerLink(ctx context.Context, client RazorpayClient, customerSvc RazorpayCustomerService, c *customer.Customer, req AuthorizationLinkRequest) (*AuthorizationLinkResponse, error) {
	razorpayCustomerID, err := customerSvc.VerifyLinkCustomer(ctx, c)
	if err != nil {
		return nil, err
	}

	link, err := client.CreateAuthorizationLink(ctx, req)
	if err != nil {
		return nil, err
	}
	if link.CustomerID == razorpayCustomerID {
		return link, nil
	}

	if _, err := client.CancelInvoice(context.WithoutCancel(ctx), link.ID); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Razorpay attached the link to another customer and it couldn't be cancelled").
			WithReportableDetails(map[string]interface{}{"customer_id": c.ID, "link_id": link.ID}).
			Mark(ierr.ErrInternal)
	}
	return nil, errLinkCustomerMismatch(c.ID, razorpayCustomerID)
}
