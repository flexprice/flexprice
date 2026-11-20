package scenarios

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/scenario"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/suite"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/world"
	"github.com/flexprice/go-sdk/v2/models/dtos"
	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
	"github.com/flexprice/go-sdk/v2/models/types"
	ptr "github.com/flexprice/go-sdk/v2/types"
	"github.com/shopspring/decimal"
)

// paymentCapability mirrors types.IntegrationCapabilityType on the server.
type paymentCapability string

const (
	capPaymentLink  paymentCapability = "payment_link"
	capAutoCharge   paymentCapability = "auto_charge"
	capListMethods  paymentCapability = "list_payment_methods"
	capAddMethod    paymentCapability = "add_payment_method"
	capDeleteMethod paymentCapability = "delete_payment_method"
	capSetDefault   paymentCapability = "set_default_method"
)

const (
	paymentReturnURL   = "https://example.com/e2eprobe/payments"
	razorpayContact    = "9000090000"
	defaultPaymentPoll = 3 * time.Second
)

// gatewayCapabilities mirrors internal/ee/service/payment_provider_resolver.go: a
// listed capability is probed for success, a missing one for a clean rejection.
var gatewayCapabilities = map[string][]paymentCapability{
	"stripe":    {capPaymentLink, capAutoCharge, capListMethods, capAddMethod, capDeleteMethod, capSetDefault},
	"chargebee": {capPaymentLink, capAutoCharge, capListMethods, capAddMethod, capDeleteMethod, capSetDefault},
	"razorpay":  {capPaymentLink, capAutoCharge, capListMethods, capAddMethod},
}

// gatewayDefault fills a persona's payment block: the gateway's native currency
// and how long its webhooks take to settle a payment.
type gatewayDefault struct {
	currency string
	settle   time.Duration
}

var gatewayDefaults = map[string]gatewayDefault{
	"stripe":    {currency: "USD", settle: 90 * time.Second},
	"chargebee": {currency: "USD", settle: 5 * time.Minute},
	"razorpay":  {currency: "INR", settle: 10 * time.Minute},
}

var (
	paymentTopUpCredits  = decimal.NewFromInt(25)
	paymentInvoiceAmount = decimal.NewFromInt(15)
)

// paymentOpts is one persona's payment block with gateway defaults applied.
type paymentOpts struct {
	provider          string
	currency          string
	fixedCustomer     string
	declineLast4      string
	settleTimeout     time.Duration
	poll              time.Duration
	assertKnownIssues bool
}

func newPaymentOpts(spec suite.Payment, poll time.Duration) paymentOpts {
	def, known := gatewayDefaults[spec.Provider]
	if !known {
		def = gatewayDefault{currency: "USD", settle: 90 * time.Second}
	}
	o := paymentOpts{
		provider:          spec.Provider,
		currency:          spec.Currency,
		fixedCustomer:     spec.FixedCustomer,
		declineLast4:      spec.DeclineLast4,
		settleTimeout:     spec.SettleTimeout.Duration,
		poll:              poll,
		assertKnownIssues: spec.AssertKnownIssues,
	}
	if o.currency == "" {
		o.currency = def.currency
	}
	if o.settleTimeout <= 0 {
		o.settleTimeout = def.settle
	}
	if o.poll <= 0 {
		o.poll = defaultPaymentPoll
	}
	return o
}

// knownIssue returns why a leg is skipped on this gateway, or "" when it runs.
func (o paymentOpts) knownIssue(leg string) string {
	if o.assertKnownIssues {
		return ""
	}
	return knownIssueLegs[o.provider][leg]
}

func (o paymentOpts) supports(c paymentCapability) bool {
	for _, have := range gatewayCapabilities[o.provider] {
		if have == c {
			return true
		}
	}
	return false
}

// paymentPrelude binds the persona's payment block and checks the gateway is
// connected and capable; a non-nil result ends the run.
func paymentPrelude(ctx context.Context, sc *scenario.Context, poll time.Duration, need paymentCapability) (paymentOpts, *e2eprobe.Result) {
	spec := sc.Persona().Spec().Payment
	if spec == nil {
		r := sc.Skip("payment", "persona has no payment block")
		return paymentOpts{}, &r
	}
	opts := newPaymentOpts(*spec, poll)
	sc.Attr("provider", opts.provider)
	conns, err := sc.Client().Tenant().ListConnections(ctx)
	if err != nil {
		r := sc.Fail("connections", err)
		return opts, &r
	}
	if !hasConnection(conns, opts.provider) {
		r := sc.Skip("connection", "no "+opts.provider+" connection on the tenant")
		return opts, &r
	}
	if !opts.supports(need) {
		r := sc.Skip("capability", fmt.Sprintf("%s has no %s capability", opts.provider, need))
		return opts, &r
	}
	return opts, nil
}

func hasConnection(conns []e2eprobe.Connection, provider string) bool {
	for _, c := range conns {
		if c.ProviderType == provider {
			return true
		}
	}
	return false
}

// paymentFlow carries one run's customer and the helpers every payment scenario
// shares. Every error it returns carries the step and the provider.
type paymentFlow struct {
	client  e2eprobe.Client
	runID   string
	persona string
	now     func() time.Time
	opts    paymentOpts
	role    string

	externalID string
	customerID string
	walletID   string
}

func newPaymentFlow(sc *scenario.Context, opts paymentOpts, role string) *paymentFlow {
	return &paymentFlow{client: sc.Client(), runID: sc.RunID(), persona: sc.Persona().Name(), now: sc.Now, opts: opts, role: role}
}

func (f *paymentFlow) provider() string { return f.opts.provider }

// fail wraps err with the step, the provider and whatever ids are known so far.
func (f *paymentFlow) fail(step string, extra map[string]string, format string, args ...any) error {
	attrs := map[string]string{
		"step":     step,
		"provider": f.provider(),
		"customer": f.externalID,
	}
	if f.customerID != "" {
		attrs["customer_id"] = f.customerID
	}
	for k, v := range extra {
		attrs[k] = v
	}
	return e2eprobe.Errorf(attrs, format, args...)
}

// useEphemeral works on a customer Clone stamped, adding the contact details the
// gateway copy needs: a short email (Chargebee's limits) and Razorpay's phone number.
func (f *paymentFlow) useEphemeral(ctx context.Context, eph *world.Ephemeral) error {
	f.externalID, f.customerID = eph.ExternalID(), eph.CustomerID()
	if f.customerID == "" {
		return f.fail("clone", nil, "ephemeral customer %s has no id", f.externalID)
	}
	req := types.UpdateCustomerRequest{
		Email: ptr.String(fmt.Sprintf("e2eprobe-%s-%s@example.com", f.role, strconv.FormatInt(f.now().UnixNano(), 36))),
	}
	if f.provider() == "razorpay" {
		req.Contact = ptr.String(razorpayContact)
	}
	if _, err := f.client.Customers().Update(ctx, req, ptr.String(f.customerID), nil); err != nil {
		return f.fail("customer_contact", nil, "set customer contact: %w", err)
	}
	return nil
}

// usePersistentCustomer works on a long-lived customer instead of a fresh
// ephemeral one, creating it on first use. It is never registered for the janitor.
func (f *paymentFlow) usePersistentCustomer(ctx context.Context, externalID string) error {
	f.externalID = externalID
	got, err := f.client.Customers().GetByExternalID(ctx, externalID)
	if err != nil && !world.IsNotFound(err) {
		return f.fail("get_persistent_customer", nil, "get customer: %w", err)
	}
	if err == nil && got != nil && got.CustomerResponse != nil && got.CustomerResponse.ID != nil {
		f.customerID = *got.CustomerResponse.ID
		return nil
	}
	req := types.CreateCustomerRequest{
		ExternalID: externalID,
		Name:       "E2EProbe Payments " + f.provider(),
		Email:      ptr.String(fmt.Sprintf("e2eprobe-%s-%s@example.com", f.role, f.provider())),
		Metadata: map[string]string{
			"e2eprobe":         "true",
			"e2eprobe_cohort":  world.PersistentCohort,
			"e2eprobe_persona": f.persona,
			"e2eprobe_role":    "payment-" + f.role,
			"e2eprobe_run_id":  f.runID,
		},
	}
	if f.provider() == "razorpay" {
		req.Contact = ptr.String(razorpayContact)
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
			if w.ID != nil && strings.EqualFold(text(w.Currency), f.opts.currency) {
				f.walletID = *w.ID
				return nil
			}
		}
	}
	return f.createWallet(ctx)
}

func (f *paymentFlow) createWallet(ctx context.Context) error {
	resp, err := f.client.Wallets().Create(ctx, types.CreateWalletRequest{
		CustomerID: ptr.String(f.customerID),
		Currency:   f.opts.currency,
		Metadata:   map[string]string{"e2eprobe": "true", "e2eprobe_role": "payment-" + f.role, "e2eprobe_run_id": f.runID},
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
	if world.IsNotFound(err) {
		// A wallet created a moment ago can 404 on its first read.
		if err = f.pause(ctx, f.opts.poll); err != nil {
			return decimal.Zero, err
		}
		resp, err = f.client.Wallets().GetBalance(ctx, f.walletID)
	}
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
		cfg.CustomerNotPresent = ptr.Bool(true)
	}
	return &types.CheckoutParams{
		PaymentProvider:       types.CheckoutPaymentProvider(f.provider()),
		PaymentProviderConfig: cfg,
		SuccessURL:            ptr.String(paymentReturnURL),
		FailureURL:            ptr.String(paymentReturnURL),
		CancelURL:             ptr.String(paymentReturnURL),
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
		CreditsToAdd:      ptr.String(credits.StringFixed(2)),
		TransactionReason: types.TransactionReasonPurchasedCreditInvoiced,
		Description:       ptr.String("e2eprobe payments top-up"),
		// The server's derived key dedupes identical top-ups within a minute; each flow is its own top-up.
		IdempotencyKey: ptr.String(fmt.Sprintf("e2eprobe-topup-%s-%d", f.runID, f.now().UnixNano())),
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
			f.walletID, text(r.EntityID), text(session.CheckoutPaymentID))
	}
	return session, nil
}

// startInvoiceCheckout creates a one-off invoice gated on checkout and returns its session.
func (f *paymentFlow) startInvoiceCheckout(ctx context.Context, amount decimal.Decimal, collection types.CollectionMethod) (*types.CheckoutSessionResponse, error) {
	amt := amount.StringFixed(2)
	resp, err := f.client.Payments().CreateCheckoutInvoice(ctx, types.CreateInvoiceRequest{
		CustomerID:    f.customerID,
		Currency:      f.opts.currency,
		InvoiceType:   types.InvoiceTypeOneOff.ToPointer(),
		BillingReason: types.InvoiceBillingReasonManual.ToPointer(),
		AmountDue:     amt,
		Subtotal:      amt,
		Total:         amt,
		Description:   ptr.String("e2eprobe payments one-off invoice"),
		LineItems: []types.CreateInvoiceLineItemRequest{{
			DisplayName: ptr.String("E2EProbe one-off charge"),
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
	settle, cancel := context.WithTimeout(ctx, f.opts.settleTimeout)
	defer cancel()
	for {
		resp, err := f.client.Payments().GetCheckoutSession(ctx, sessionID)
		if err != nil {
			return nil, f.fail(step, map[string]string{"checkout_session_id": sessionID}, "get checkout session: %w", err)
		}
		var s *types.CheckoutSessionResponse
		if resp != nil {
			s = resp.CheckoutSessionResponse
		}
		if s != nil && s.Terminal != nil && *s.Terminal {
			return s, nil
		}
		if err := f.pause(settle, f.opts.poll); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			f.cancelQuietly(sessionID)
			return nil, f.fail(step, map[string]string{"checkout_session_id": sessionID, "checkout_status": sessionStatus(s)},
				"session not terminal after %s", f.opts.settleTimeout)
		}
	}
}

// waitPaymentSettled polls the payment through reads that never reconcile with
// the gateway, so only an inbound webhook can move it. Returns the terminal status.
func (f *paymentFlow) waitPaymentSettled(ctx context.Context, step, paymentID string) (types.PaymentStatus, error) {
	settle, cancel := context.WithTimeout(ctx, f.opts.settleTimeout)
	defer cancel()
	var status types.PaymentStatus
	for {
		resp, err := f.client.Payments().ListPayments(ctx, dtos.ListPaymentsRequest{PaymentIds: []string{paymentID}})
		if err != nil {
			return "", f.fail(step, map[string]string{"payment_id": paymentID}, "list payments: %w", err)
		}
		if resp != nil && resp.ListPaymentsResponse != nil {
			for _, p := range resp.ListPaymentsResponse.Items {
				if text(p.ID) == paymentID && p.PaymentStatus != nil {
					status = *p.PaymentStatus
				}
			}
		}
		if status == types.PaymentStatusSucceeded || status == types.PaymentStatusFailed {
			return status, nil
		}
		if err := f.pause(settle, f.opts.poll); err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return status, f.fail(step, map[string]string{"payment_id": paymentID, "payment_status": string(status)},
				"payment still %q after %s: the %s webhook was not processed", status, f.opts.settleTimeout, f.provider())
		}
	}
}

// pause waits for d or until ctx ends, returning ctx's error in that case.
func (f *paymentFlow) pause(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// cancelQuietly expires a hosted-link session a failed run left pending. Never use
// it on an auto-charged session: that orphans money the gateway already captured.
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
		"checkout_session_id": text(s.ID),
		"checkout_status":     sessionStatus(s),
		"failure_reason":      text(s.FailureReason),
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

// text is the string a response pointer holds, "" when absent.
func text(p *string) string {
	if p == nil {
		return ""
	}
	return *p
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
