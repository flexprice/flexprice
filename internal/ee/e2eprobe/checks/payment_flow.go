package checks

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/go-sdk/v2/models/dtos"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
	"github.com/flexprice/go-sdk/v2/models/types"
	"github.com/shopspring/decimal"
)

// paymentCapability mirrors types.IntegrationCapabilityType on the server.
type paymentCapability string

const (
	capPaymentLink   paymentCapability = "payment_link"
	capAutoCharge    paymentCapability = "auto_charge"
	capListMethods   paymentCapability = "list_payment_methods"
	capAddMethod     paymentCapability = "add_payment_method"
	capDeleteMethod  paymentCapability = "delete_payment_method"
	capSetDefault    paymentCapability = "set_default_method"
	paymentReturnURL                   = "https://example.com/e2eprobe/payments"
)

// gatewayCapabilities mirrors gatewayCapabilities in
// internal/ee/service/payment_provider_resolver.go. A capability listed here is
// probed for success; one missing is probed for a clean rejection.
var gatewayCapabilities = map[string][]paymentCapability{
	"stripe":    {capPaymentLink, capAutoCharge, capListMethods, capAddMethod, capDeleteMethod, capSetDefault},
	"chargebee": {capPaymentLink, capAutoCharge, capListMethods, capAddMethod, capDeleteMethod, capSetDefault},
	"razorpay":  {capPaymentLink, capAutoCharge, capListMethods},
}

// PaymentProbeOpts configures one gateway's payment probes.
type PaymentProbeOpts struct {
	Provider e2eprobe.PaymentProviderConfig
	// SettleTimeout bounds every wait for a payment or session to settle.
	SettleTimeout time.Duration
	// PollInterval spaces settle polls; tests shorten it.
	PollInterval time.Duration
	// AssertKnownIssues runs legs listed in knownIssueLegs instead of skipping them.
	AssertKnownIssues bool
}

// knownIssue returns why a leg is skipped on this gateway, or "" when it runs.
func (o PaymentProbeOpts) knownIssue(leg string) string {
	if o.AssertKnownIssues {
		return ""
	}
	return knownIssueLegs[o.Provider.Provider][leg]
}

func (o PaymentProbeOpts) supports(c paymentCapability) bool {
	for _, have := range gatewayCapabilities[o.Provider.Provider] {
		if have == c {
			return true
		}
	}
	return false
}

func (o PaymentProbeOpts) pollInterval() time.Duration {
	if o.PollInterval > 0 {
		return o.PollInterval
	}
	return 3 * time.Second
}

func (o PaymentProbeOpts) settleTimeout() time.Duration {
	if o.SettleTimeout > 0 {
		return o.SettleTimeout
	}
	return 90 * time.Second
}

// paymentFlow carries one Run's ephemeral customer and the helpers every
// payment probe shares. Every error it returns carries the provider tag.
type paymentFlow struct {
	client e2eprobe.Client
	reg    e2eprobe.Registry
	runID  string
	opts   PaymentProbeOpts
	role   string

	externalID string
	customerID string
	walletID   string
}

func newPaymentFlow(c e2eprobe.Client, r e2eprobe.Registry, runID string, opts PaymentProbeOpts, role string) *paymentFlow {
	return &paymentFlow{client: c, reg: r, runID: runID, opts: opts, role: role}
}

func (f *paymentFlow) provider() string { return f.opts.Provider.Provider }

// fail wraps err with the step, the provider and whatever ids are known so far.
func (f *paymentFlow) fail(step string, extra map[string]string, format string, args ...any) error {
	attrs := map[string]string{
		"step":                 step,
		"provider":             f.provider(),
		"external_customer_id": f.externalID,
	}
	if f.customerID != "" {
		attrs["customer_id"] = f.customerID
	}
	for k, v := range extra {
		attrs[k] = v
	}
	return e2eprobe.Errorf(attrs, format, args...)
}

// createCustomer creates and registers the ephemeral customer the Run works on.
func (f *paymentFlow) createCustomer(ctx context.Context) error {
	now := time.Now().UTC()
	// The role keeps ids distinct when several payment probes start in the same tick.
	f.externalID = fmt.Sprintf("e2eprobe-cust-eph-%s-%s-%d", strings.TrimPrefix(f.role, "ephemeral-"), f.provider(), now.UnixNano())
	req := types.CreateCustomerRequest{
		ExternalID: f.externalID,
		Name:       "E2EProbe Ephemeral Payments " + f.provider(),
		// Chargebee caps emails at 70 characters and rejects long digit runs as card
		// numbers, so the email carries a short base-36 suffix instead of the external id.
		Email: strPtr(fmt.Sprintf("e2eprobe-%s-%s@example.com", strings.TrimPrefix(f.role, "ephemeral-payment-"), strconv.FormatInt(now.UnixNano(), 36))),
		Metadata: map[string]string{
			"e2eprobe":        "true",
			"e2eprobe_cohort": "ephemeral",
			"e2eprobe_role":   f.role,
			"e2eprobe_run_id": f.runID,
		},
	}
	if f.provider() == "razorpay" {
		req.Contact = strPtr("9000090000")
	}
	resp, err := f.client.Customers().Create(ctx, req)
	if err != nil {
		return f.fail("create_customer", nil, "create customer: %w", err)
	}
	if resp == nil || resp.CustomerResponse == nil || resp.CustomerResponse.ID == nil {
		return f.fail("create_customer", nil, "create customer: empty response")
	}
	f.customerID = *resp.CustomerResponse.ID
	f.reg.RegisterEphemeral("customer", f.externalID, now)
	return nil
}

// usePersistentCustomer works on a long-lived customer instead of a fresh
// ephemeral one, creating it on first use. It is not registered with the janitor.
func (f *paymentFlow) usePersistentCustomer(ctx context.Context, externalID string) error {
	f.externalID = externalID
	got, err := f.client.Customers().GetByExternalID(ctx, externalID)
	if err != nil && !isNotFound(err) {
		return f.fail("get_persistent_customer", nil, "get customer: %w", err)
	}
	if err == nil && got != nil && got.CustomerResponse != nil && got.CustomerResponse.ID != nil {
		f.customerID = *got.CustomerResponse.ID
		return nil
	}
	req := types.CreateCustomerRequest{
		ExternalID: externalID,
		Name:       "E2EProbe Payments " + f.provider(),
		Email:      strPtr(fmt.Sprintf("e2eprobe-%s-%s@example.com", strings.TrimPrefix(f.role, "persistent-payment-"), f.provider())),
		Metadata: map[string]string{
			"e2eprobe":        "true",
			"e2eprobe_cohort": "persistent",
			"e2eprobe_role":   f.role,
		},
	}
	if f.provider() == "razorpay" {
		req.Contact = strPtr("9000090000")
	}
	resp, err := f.client.Customers().Create(ctx, req)
	if err != nil {
		return f.fail("create_persistent_customer", nil, "create customer: %w", err)
	}
	if resp == nil || resp.CustomerResponse == nil || resp.CustomerResponse.ID == nil {
		return f.fail("create_persistent_customer", nil, "create customer: empty response")
	}
	f.customerID = *resp.CustomerResponse.ID
	return nil
}

// useOrCreateWallet reuses the customer's wallet in the probe currency, creating one if absent.
func (f *paymentFlow) useOrCreateWallet(ctx context.Context) error {
	resp, err := f.client.Wallets().GetWalletsByCustomerID(ctx, f.customerID)
	if err != nil {
		return f.fail("get_wallets", nil, "list customer wallets: %w", err)
	}
	if resp != nil {
		for _, w := range resp.WalletResponses {
			if w.ID != nil && strings.EqualFold(derefStr(w.Currency), f.opts.Provider.Currency) {
				f.walletID = *w.ID
				return nil
			}
		}
	}
	return f.createWallet(ctx)
}

func (f *paymentFlow) createWallet(ctx context.Context) error {
	resp, err := f.client.Wallets().Create(ctx, types.CreateWalletRequest{
		CustomerID: strPtr(f.customerID),
		Currency:   f.opts.Provider.Currency,
		Metadata:   map[string]string{"e2eprobe": "true", "e2eprobe_role": f.role},
	})
	if err != nil {
		return f.fail("create_wallet", nil, "create wallet: %w", err)
	}
	if resp == nil || resp.WalletResponse == nil || resp.WalletResponse.ID == nil {
		return f.fail("create_wallet", nil, "create wallet: empty response")
	}
	f.walletID = *resp.WalletResponse.ID
	return nil
}

func (f *paymentFlow) credits(ctx context.Context) (decimal.Decimal, error) {
	resp, err := f.client.Wallets().GetBalance(ctx, f.walletID)
	if err != nil {
		return decimal.Zero, f.fail("read_wallet", map[string]string{"wallet_id": f.walletID}, "read wallet balance: %w", err)
	}
	if resp == nil || resp.WalletBalanceResponse == nil || resp.WalletBalanceResponse.CreditBalance == nil {
		return decimal.Zero, nil
	}
	bal, err := decimal.NewFromString(*resp.WalletBalanceResponse.CreditBalance)
	if err != nil {
		return decimal.Zero, f.fail("read_wallet", map[string]string{"wallet_id": f.walletID}, "parse credit_balance %q: %w", *resp.WalletBalanceResponse.CreditBalance, err)
	}
	return bal, nil
}

func (f *paymentFlow) expectCredits(ctx context.Context, step string, want decimal.Decimal) error {
	got, err := f.credits(ctx)
	if err != nil {
		return err
	}
	if !got.Equal(want) {
		return f.fail(step, map[string]string{"wallet_id": f.walletID, "credit_balance": got.String(), "want": want.String()},
			"wallet credit_balance %s, want %s", got, want)
	}
	return nil
}

func (f *paymentFlow) checkoutParams(collection types.CollectionMethod, customerNotPresent bool) *types.CheckoutParams {
	cfg := &types.CheckoutPaymentProviderConfig{CollectionMethod: collection.ToPointer()}
	if customerNotPresent {
		cfg.CustomerNotPresent = boolPtr(true)
	}
	return &types.CheckoutParams{
		PaymentProvider:       types.CheckoutPaymentProvider(f.provider()),
		PaymentProviderConfig: cfg,
		SuccessURL:            strPtr(paymentReturnURL),
		FailureURL:            strPtr(paymentReturnURL),
		CancelURL:             strPtr(paymentReturnURL),
		Metadata:              map[string]string{"e2eprobe": "true", "e2eprobe_run_id": f.runID},
	}
}

// startTopUp starts a pay-first wallet top-up and returns its checkout session.
// The raw error is returned unwrapped so callers can assert on rejections.
func (f *paymentFlow) startTopUp(ctx context.Context, credits decimal.Decimal, collection types.CollectionMethod, customerNotPresent bool) (*types.CheckoutSessionResponse, error) {
	// Supersede a leftover pending top-up (e.g. one whose webhook was missed) so it
	// cannot block a persistent customer's wallet on every later run.
	checkout := f.checkoutParams(collection, customerNotPresent)
	checkout.EntityCreationOptions = &types.EntityCreationOptions{
		EntityCreationConflictPolicies: &types.EntityCreationConflictPolicies{
			OnExistingEntity: types.OnExistingEntityPolicySupersede.ToPointer(),
		},
	}
	resp, err := f.client.Wallets().TopUp(ctx, f.walletID, types.TopUpWalletRequest{
		CreditsToAdd:      strPtr(credits.StringFixed(2)),
		TransactionReason: types.TransactionReasonPurchasedCreditInvoiced,
		Description:       strPtr("e2eprobe payments top-up"),
		// The server's derived key dedupes identical top-ups within a minute; each flow is its own top-up.
		IdempotencyKey: strPtr(fmt.Sprintf("e2eprobe-topup-%s-%d", f.runID, time.Now().UnixNano())),
		Checkout:       checkout,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.TopUpWalletResponse == nil || resp.TopUpWalletResponse.CheckoutSession == nil {
		return nil, errors.New("top-up response has no checkout_session")
	}
	// The server hands back the wallet's in-flight session instead of starting a new one.
	session := resp.TopUpWalletResponse.CheckoutSession
	if r := session.EntityCreationResult; r != nil && r.Status != nil && *r.Status == types.EntityCreationStatusFailedAlreadyExists {
		return nil, fmt.Errorf("wallet %s is blocked by pending checkout session %s (payment %s)",
			f.walletID, derefStr(r.EntityID), derefStr(session.CheckoutPaymentID))
	}
	return session, nil
}

// startInvoiceCheckout creates a one-off invoice gated on checkout and returns its session.
func (f *paymentFlow) startInvoiceCheckout(ctx context.Context, amount decimal.Decimal, collection types.CollectionMethod) (*types.CheckoutSessionResponse, error) {
	amt := amount.StringFixed(2)
	resp, err := f.client.Payments().CreateCheckoutInvoice(ctx, types.CreateInvoiceRequest{
		CustomerID:    f.customerID,
		Currency:      f.opts.Provider.Currency,
		InvoiceType:   types.InvoiceTypeOneOff.ToPointer(),
		BillingReason: types.InvoiceBillingReasonManual.ToPointer(),
		AmountDue:     amt,
		Subtotal:      amt,
		Total:         amt,
		Description:   strPtr("e2eprobe payments one-off invoice"),
		LineItems: []types.CreateInvoiceLineItemRequest{{
			DisplayName: strPtr("E2EProbe one-off charge"),
			Amount:      amt,
			Quantity:    "1",
		}},
		Checkout: f.checkoutParams(collection, false),
	})
	if err != nil {
		return nil, err
	}
	if resp == nil || resp.InvoiceResponse == nil || resp.InvoiceResponse.CheckoutSession == nil {
		return nil, errors.New("invoice response has no checkout_session")
	}
	return resp.InvoiceResponse.CheckoutSession, nil
}

// waitTerminal polls the session until it ends. Each read reconciles with the gateway.
func (f *paymentFlow) waitTerminal(ctx context.Context, step, sessionID string) (*types.CheckoutSessionResponse, error) {
	deadline := time.Now().Add(f.opts.settleTimeout())
	for {
		resp, err := f.client.Payments().GetCheckoutSession(ctx, sessionID)
		if err != nil {
			return nil, f.fail(step, map[string]string{"checkout_session_id": sessionID}, "get checkout session: %w", err)
		}
		if resp != nil && resp.CheckoutSessionResponse != nil {
			s := resp.CheckoutSessionResponse
			if s.Terminal != nil && *s.Terminal {
				return s, nil
			}
			if time.Now().After(deadline) {
				return nil, f.fail(step, map[string]string{"checkout_session_id": sessionID, "checkout_status": sessionStatus(s)},
					"session not terminal after %s", f.opts.settleTimeout())
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(f.opts.pollInterval()):
		}
	}
}

// waitPaymentSettled polls the payment through reads that never reconcile with
// the gateway, so only an inbound webhook can move it. Returns the terminal status.
func (f *paymentFlow) waitPaymentSettled(ctx context.Context, step, paymentID string) (types.PaymentStatus, error) {
	deadline := time.Now().Add(f.opts.settleTimeout())
	var status types.PaymentStatus
	for {
		resp, err := f.client.Payments().ListPayments(ctx, dtos.ListPaymentsRequest{PaymentIds: []string{paymentID}})
		if err != nil {
			return "", f.fail(step, map[string]string{"payment_id": paymentID}, "list payments: %w", err)
		}
		if resp != nil && resp.ListPaymentsResponse != nil {
			for _, p := range resp.ListPaymentsResponse.Items {
				if derefStr(p.ID) == paymentID && p.PaymentStatus != nil {
					status = *p.PaymentStatus
				}
			}
		}
		if status == types.PaymentStatusSucceeded || status == types.PaymentStatusFailed {
			return status, nil
		}
		if time.Now().After(deadline) {
			return status, f.fail(step, map[string]string{"payment_id": paymentID, "payment_status": string(status)},
				"payment still %q after %s: the %s webhook was not processed", status, f.opts.settleTimeout(), f.provider())
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(f.opts.pollInterval()):
		}
	}
}

// cancelQuietly expires a hosted-link session left pending by a failed Run.
// Never use it on an auto-charged session: cancelling tears down the invoice and
// payment without asking the gateway, which orphans money already captured.
func (f *paymentFlow) cancelQuietly(sessionID string) {
	if sessionID == "" {
		return
	}
	_, _ = f.client.Payments().CancelCheckoutSession(context.Background(), sessionID)
}

// expectCompleted fails unless the session ended completed.
func (f *paymentFlow) expectCompleted(step string, s *types.CheckoutSessionResponse) error {
	if sessionStatus(s) == string(types.CheckoutStatusCompleted) {
		return nil
	}
	return f.fail(step, map[string]string{
		"checkout_session_id": derefStr(s.ID),
		"checkout_status":     sessionStatus(s),
		"failure_reason":      derefStr(s.FailureReason),
	}, "session ended %s, want completed", sessionStatus(s))
}

func findSavedMethod(methods []e2eprobe.SavedPaymentMethod, id string) (e2eprobe.SavedPaymentMethod, bool) {
	for _, m := range methods {
		if m.ID == id {
			return m, true
		}
	}
	return e2eprobe.SavedPaymentMethod{}, false
}

func sessionStatus(s *types.CheckoutSessionResponse) string {
	if s == nil || s.CheckoutStatus == nil {
		return ""
	}
	return string(*s.CheckoutStatus)
}

// httpStatus extracts the HTTP status from an SDK or raw-HTTP error, 0 if none.
func httpStatus(err error) int {
	var api *sdkerrors.APIError
	if errors.As(err, &api) && api != nil {
		return api.StatusCode
	}
	var er *sdkerrors.ErrorResponse
	if errors.As(err, &er) && er != nil {
		if er.HTTPStatusCode != nil {
			return int(*er.HTTPStatusCode)
		}
		if er.HTTPMeta.Response != nil {
			return er.HTTPMeta.Response.StatusCode
		}
	}
	return 0
}

// isClientRejection reports a clean 4xx refusal. A 5xx or transport error is a
// server fault, not a rejection.
func isClientRejection(err error) bool {
	status := httpStatus(err)
	return status >= 400 && status < 500
}
