package checks

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/go-sdk/v2/models/dtos"
	"github.com/flexprice/go-sdk/v2/models/types"
	"github.com/shopspring/decimal"
)

var (
	paymentTopUpCredits  = decimal.NewFromInt(25)
	paymentInvoiceAmount = decimal.NewFromInt(15)
)

// PaymentLinkProbe exercises every checkout that should end on a hosted page,
// on an ephemeral customer with no saved method:
//   - pay_invoice and wallet_topup via send_invoice issue a pending session with
//     a payment URL; cancelling expires it with no payment and no credits.
//   - charge_automatically with nothing saved falls back to an authorization link.
//   - the same charge with customer_not_present is refused (4xx), not left pending.
type PaymentLinkProbe struct {
	client e2eprobe.Client
	reg    e2eprobe.Registry
	runID  string
	lg     *logger.Logger
	opts   PaymentProbeOpts

	// expiryWatch is the untouched session a later Run checks was expired by the
	// cleanup job; nil when none is outstanding. Lost on restart, which only
	// delays the check by one cycle.
	mu          sync.Mutex
	expiryWatch *expiryWatch
}

// expiryWatch records an abandoned top-up session and what must hold once the
// cleanup job has expired it.
type expiryWatch struct {
	sessionID string
	paymentID string
	invoiceID string
	walletID  string
	credits   decimal.Decimal
	matureAt  time.Time
}

// expiryCleanupSlack covers the cleanup job's 30-minute cadence plus headroom.
const expiryCleanupSlack = 40 * time.Minute

func NewPaymentLinkProbe(c e2eprobe.Client, r e2eprobe.Registry, runID string, lg *logger.Logger, opts PaymentProbeOpts) *PaymentLinkProbe {
	return &PaymentLinkProbe{client: c, reg: r, runID: runID, lg: lg, opts: opts}
}

func (p *PaymentLinkProbe) Name() string        { return "payment-link-probe-" + p.opts.Provider.Provider }
func (p *PaymentLinkProbe) Kind() e2eprobe.Kind { return e2eprobe.KindScenario }

func (p *PaymentLinkProbe) Run(ctx context.Context) error {
	if !p.opts.supports(capPaymentLink) {
		return nil
	}
	expiry := newPaymentFlow(p.client, p.reg, p.runID, p.opts, "persistent-payment-expiry")
	legs := &legResults{}
	legs.run("links", func() error { return p.runLinkFlows(ctx) })
	legs.run("natural_expiry", func() error { return p.checkNaturalExpiry(ctx, expiry) })
	return legs.err(expiry)
}

// runLinkFlows covers the hosted-link flows on a fresh ephemeral customer.
func (p *PaymentLinkProbe) runLinkFlows(ctx context.Context) error {
	f := newPaymentFlow(p.client, p.reg, p.runID, p.opts, "ephemeral-payment-link")
	if err := f.createCustomer(ctx); err != nil {
		return err
	}
	if err := f.createWallet(ctx); err != nil {
		return err
	}

	if err := p.linkThenCancel(ctx, f, "invoice_link", func() (*types.CheckoutSessionResponse, error) {
		return f.startInvoiceCheckout(ctx, paymentInvoiceAmount, types.CollectionMethodSendInvoice)
	}); err != nil {
		return err
	}
	if err := p.linkThenCancel(ctx, f, "topup_link", func() (*types.CheckoutSessionResponse, error) {
		return f.startTopUp(ctx, paymentTopUpCredits, types.CollectionMethodSendInvoice, false)
	}); err != nil {
		return err
	}

	if !p.opts.supports(capAutoCharge) {
		return nil
	}
	if err := p.linkThenCancel(ctx, f, "autocharge_fallback_link", func() (*types.CheckoutSessionResponse, error) {
		return f.startTopUp(ctx, paymentTopUpCredits, types.CollectionMethodChargeAutomatically, false)
	}); err != nil {
		return err
	}
	return p.unattendedChargeRejected(ctx, f)
}

// linkThenCancel asserts the flow issues a pending session with a payment URL,
// then that cancelling expires it without a payment or any credit.
func (p *PaymentLinkProbe) linkThenCancel(ctx context.Context, f *paymentFlow, flow string, start func() (*types.CheckoutSessionResponse, error)) error {
	before, err := f.credits(ctx)
	if err != nil {
		return err
	}
	session, err := start()
	if err != nil {
		return f.fail(flow+"_start", nil, "start checkout: %w", err)
	}
	sessionID := derefStr(session.ID)
	defer f.cancelQuietly(sessionID)
	ids := map[string]string{"checkout_session_id": sessionID, "checkout_status": sessionStatus(session)}

	if sessionStatus(session) != string(types.CheckoutStatusPending) {
		return f.fail(flow+"_assert_pending", ids, "session is %q, want pending", sessionStatus(session))
	}
	if session.PaymentAction == nil || derefStr(session.PaymentAction.URL) == "" {
		return f.fail(flow+"_assert_url", ids, "pending session has no payment_action.url")
	}

	cancelled, err := p.client.Payments().CancelCheckoutSession(ctx, sessionID)
	if err != nil {
		return f.fail(flow+"_cancel", ids, "cancel session: %w", err)
	}
	if cancelled == nil || cancelled.CheckoutSessionResponse == nil {
		return f.fail(flow+"_cancel", ids, "cancel session: empty response")
	}
	final := cancelled.CheckoutSessionResponse
	ids["checkout_status"] = sessionStatus(final)
	if sessionStatus(final) != string(types.CheckoutStatusExpired) {
		return f.fail(flow+"_assert_expired", ids, "cancelled session is %q, want expired", sessionStatus(final))
	}
	if final.Payment != nil && final.Payment.Status != nil && *final.Payment.Status == types.PaymentStatusSucceeded {
		return f.fail(flow+"_assert_unpaid", ids, "cancelled session's payment is SUCCEEDED")
	}
	return f.expectCredits(ctx, flow+"_assert_no_credit", before)
}

// unattendedChargeRejected asserts a customer-not-present charge with nothing
// saved is refused outright rather than parked behind a link nobody will open.
func (p *PaymentLinkProbe) unattendedChargeRejected(ctx context.Context, f *paymentFlow) error {
	before, err := f.credits(ctx)
	if err != nil {
		return err
	}
	session, err := f.startTopUp(ctx, paymentTopUpCredits, types.CollectionMethodChargeAutomatically, true)
	if err == nil {
		f.cancelQuietly(derefStr(session.ID))
		return f.fail("unattended_assert_rejected", map[string]string{"checkout_session_id": derefStr(session.ID), "checkout_status": sessionStatus(session)},
			"unattended auto-charge with no saved method was accepted")
	}
	if !isClientRejection(err) {
		return f.fail("unattended_assert_rejected", nil, "want a 4xx rejection: %w", err)
	}
	return f.expectCredits(ctx, "unattended_assert_no_credit", before)
}

// checkNaturalExpiry verifies a matured watch, then leaves a fresh top-up session
// untouched for a later Run. The checks read the payment and invoice before the
// session: a session read never expires anything, but asserting the cleanup's
// effects first keeps the signal on the job itself.
func (p *PaymentLinkProbe) checkNaturalExpiry(ctx context.Context, f *paymentFlow) error {
	p.mu.Lock()
	watch := p.expiryWatch
	p.mu.Unlock()

	if err := f.usePersistentCustomer(ctx, fmt.Sprintf("e2eprobe-cust-pay-%s-expiry", f.provider())); err != nil {
		return err
	}
	if watch != nil {
		if time.Now().Before(watch.matureAt) {
			return nil
		}
		p.mu.Lock()
		p.expiryWatch = nil
		p.mu.Unlock()
		if err := p.verifyExpired(ctx, f, watch); err != nil {
			return err
		}
	}
	return p.startExpiryWatch(ctx, f)
}

func (p *PaymentLinkProbe) startExpiryWatch(ctx context.Context, f *paymentFlow) error {
	if err := f.useOrCreateWallet(ctx); err != nil {
		return err
	}
	credits, err := f.credits(ctx)
	if err != nil {
		return err
	}
	session, err := f.startTopUp(ctx, paymentTopUpCredits, types.CollectionMethodSendInvoice, false)
	if err != nil {
		return f.fail("expiry_start", nil, "start abandoned top-up: %w", err)
	}
	if session.ExpiresAt == nil {
		f.cancelQuietly(derefStr(session.ID))
		return f.fail("expiry_start", map[string]string{"checkout_session_id": derefStr(session.ID)}, "session has no expires_at")
	}
	p.mu.Lock()
	p.expiryWatch = &expiryWatch{
		sessionID: derefStr(session.ID),
		paymentID: derefStr(session.CheckoutPaymentID),
		invoiceID: derefStr(session.CheckoutInvoiceID),
		walletID:  f.walletID,
		credits:   credits,
		matureAt:  session.ExpiresAt.Add(expiryCleanupSlack),
	}
	p.mu.Unlock()
	return nil
}

// verifyExpired asserts the cleanup job failed the top-up, removed its draft
// invoice and payment, credited nothing, and marked the session expired.
func (p *PaymentLinkProbe) verifyExpired(ctx context.Context, f *paymentFlow, w *expiryWatch) error {
	ids := map[string]string{"checkout_session_id": w.sessionID, "payment_id": w.paymentID, "invoice_id": w.invoiceID}
	f.walletID = w.walletID

	pays, err := p.client.Payments().ListPayments(ctx, dtos.ListPaymentsRequest{PaymentIds: []string{w.paymentID}})
	if err != nil {
		return f.fail("expiry_read_payment", ids, "list payments: %w", err)
	}
	if pays != nil && pays.ListPaymentsResponse != nil {
		for _, pay := range pays.ListPaymentsResponse.Items {
			if derefStr(pay.ID) == w.paymentID && derefPaymentStatus(pay.PaymentStatus) == types.PaymentStatusSucceeded {
				return f.fail("expiry_assert_unpaid", ids, "abandoned session's payment is SUCCEEDED")
			}
		}
	}

	inv, err := p.client.Invoices().Get(ctx, w.invoiceID)
	switch {
	case err != nil && !isNotFound(err):
		return f.fail("expiry_read_invoice", ids, "get invoice: %w", err)
	case err == nil && inv != nil && inv.InvoiceResponse != nil && !isRowRemoved(inv.InvoiceResponse.Status):
		status := inv.InvoiceResponse.InvoiceStatus
		if status == nil || *status != types.InvoiceStatusVoided {
			ids["invoice_status"] = fmt.Sprint(derefInvoiceStatus(status))
			return f.fail("expiry_assert_invoice_removed", ids, "abandoned session's invoice is still %s", ids["invoice_status"])
		}
	}

	if err := f.expectCredits(ctx, "expiry_assert_no_credit", w.credits); err != nil {
		return err
	}

	resp, err := p.client.Payments().GetCheckoutSession(ctx, w.sessionID)
	if err != nil {
		return f.fail("expiry_read_session", ids, "get checkout session: %w", err)
	}
	if resp == nil || resp.CheckoutSessionResponse == nil || sessionStatus(resp.CheckoutSessionResponse) != string(types.CheckoutStatusExpired) {
		status := ""
		if resp != nil {
			status = sessionStatus(resp.CheckoutSessionResponse)
		}
		ids["checkout_status"] = status
		return f.fail("expiry_assert_expired", ids, "abandoned session is %q past its expiry plus a cleanup cycle, want expired", status)
	}
	return nil
}

// isRowRemoved reports a soft-deleted or archived row, which GET by ID still returns.
func isRowRemoved(s *types.Status) bool {
	return s != nil && (*s == types.StatusDeleted || *s == types.StatusArchived)
}

func derefInvoiceStatus(s *types.InvoiceStatus) types.InvoiceStatus {
	if s == nil {
		return ""
	}
	return *s
}
