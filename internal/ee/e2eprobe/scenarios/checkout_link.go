package scenarios

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/scenario"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/suite"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/world"
	"github.com/flexprice/go-sdk/v2/models/dtos"
	"github.com/flexprice/go-sdk/v2/models/types"
	"github.com/shopspring/decimal"
)

// registerPayments adds the payment scenarios, which bind to payer personas.
func registerPayments(cat *scenario.Catalog) {
	payment := []e2eprobe.Trait{e2eprobe.TraitPayment}
	cat.Register(suite.ScenarioInfo{Name: "checkout-link", Kind: e2eprobe.KindFlow, Traits: payment, Every: 30 * time.Minute, Summary: "hosted checkout links are issued for invoices and top-ups, expire on cancel, and an unattended auto-charge without a method is refused"},
		func(d scenario.Deps) scenario.Scenario { return newCheckoutLink(d) })
	cat.Register(suite.ScenarioInfo{Name: "payment-methods", Kind: e2eprobe.KindFlow, Traits: payment, Every: time.Hour, Summary: "saved methods are listed, the default can be switched and the declining card neither completes nor credits"},
		func(d scenario.Deps) scenario.Scenario { return newPaymentMethods(d) })
	cat.Register(suite.ScenarioInfo{Name: "auto-charge", Kind: e2eprobe.KindFlow, Traits: payment, Every: 6 * time.Hour, Summary: "auto-charged top-ups, invoice payments and subscription changes settle via webhook and credit exactly once"},
		func(d scenario.Deps) scenario.Scenario { return newAutoCharge(d) })
}

// checkoutLink runs every checkout that should end on a hosted page against an
// ephemeral customer with no saved method: invoice and top-up links via send_invoice,
// the auto-charge fallback link, the unattended-charge rejection and the cleanup job's expiry.
type checkoutLink struct {
	poll time.Duration

	// expiryWatch is the untouched session a later run checks the cleanup job expired;
	// lost on restart, which only delays the check by one cycle.
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

func newCheckoutLink(scenario.Deps) *checkoutLink { return &checkoutLink{poll: defaultPaymentPoll} }

func (*checkoutLink) Name() string               { return "checkout-link" }
func (*checkoutLink) Kind() e2eprobe.Kind        { return e2eprobe.KindFlow }
func (*checkoutLink) Requires() []e2eprobe.Trait { return []e2eprobe.Trait{e2eprobe.TraitPayment} }

func (p *checkoutLink) Run(ctx context.Context, sc *scenario.Context) e2eprobe.Result {
	opts, stop := paymentPrelude(ctx, sc, p.poll, capPaymentLink)
	if stop != nil {
		return *stop
	}
	eph, err := sc.Clone(ctx)
	if err != nil {
		return sc.Fail("clone", err)
	}
	sc.Attr("ephemeral_customer", eph.ExternalID())
	legs := &legResults{}
	legs.run("links", func() error { return p.runLinkFlows(ctx, sc, opts, eph) })
	legs.run("natural_expiry", func() error { return p.checkNaturalExpiry(ctx, sc, opts) })
	return legs.result(sc, "checkout-link")
}

// runLinkFlows covers the hosted-link flows on the fresh ephemeral customer.
func (p *checkoutLink) runLinkFlows(ctx context.Context, sc *scenario.Context, opts paymentOpts, eph *world.Ephemeral) error {
	f := newPaymentFlow(sc, opts, "link")
	if err := f.useEphemeral(ctx, eph); err != nil {
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

	if !opts.supports(capAutoCharge) {
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
func (p *checkoutLink) linkThenCancel(ctx context.Context, f *paymentFlow, flow string, start func() (*types.CheckoutSessionResponse, error)) error {
	before, err := f.credits(ctx)
	if err != nil {
		return err
	}
	session, err := start()
	if err != nil {
		return f.fail(flow+"_start", nil, "start checkout: %w", err)
	}
	sessionID := text(session.ID)
	defer f.cancelQuietly(sessionID)
	ids := map[string]string{"checkout_session_id": sessionID, "checkout_status": sessionStatus(session)}

	if sessionStatus(session) != string(types.CheckoutStatusPending) {
		return f.fail(flow+"_assert_pending", ids, "session is %q, want pending", sessionStatus(session))
	}
	if session.PaymentAction == nil || text(session.PaymentAction.URL) == "" {
		return f.fail(flow+"_assert_url", ids, "pending session has no payment_action.url")
	}

	cancelled, err := f.client.Payments().CancelCheckoutSession(ctx, sessionID)
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
func (p *checkoutLink) unattendedChargeRejected(ctx context.Context, f *paymentFlow) error {
	before, err := f.credits(ctx)
	if err != nil {
		return err
	}
	session, err := f.startTopUp(ctx, paymentTopUpCredits, types.CollectionMethodChargeAutomatically, true)
	if err == nil {
		f.cancelQuietly(text(session.ID))
		return f.fail("unattended_assert_rejected", map[string]string{"checkout_session_id": text(session.ID), "checkout_status": sessionStatus(session)},
			"unattended auto-charge with no saved method was accepted")
	}
	if !isClientRejection(err) {
		return f.fail("unattended_assert_rejected", nil, "want a 4xx rejection: %w", err)
	}
	return f.expectCredits(ctx, "unattended_assert_no_credit", before)
}

// checkNaturalExpiry verifies a matured watch, then abandons a fresh top-up for a later
// run; payment and invoice are read before the session so the signal stays on the cleanup job.
func (p *checkoutLink) checkNaturalExpiry(ctx context.Context, sc *scenario.Context, opts paymentOpts) error {
	f := newPaymentFlow(sc, opts, "expiry")
	p.mu.Lock()
	watch := p.expiryWatch
	p.mu.Unlock()

	if err := f.usePersistentCustomer(ctx, fmt.Sprintf("e2eprobe-cust-pay-%s-expiry", f.provider())); err != nil {
		return err
	}
	sc.Attr("expiry_customer", f.externalID)
	if watch != nil {
		if sc.Now().Before(watch.matureAt) {
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

func (p *checkoutLink) startExpiryWatch(ctx context.Context, f *paymentFlow) error {
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
		f.cancelQuietly(text(session.ID))
		return f.fail("expiry_start", map[string]string{"checkout_session_id": text(session.ID)}, "session has no expires_at")
	}
	p.mu.Lock()
	p.expiryWatch = &expiryWatch{
		sessionID: text(session.ID),
		paymentID: text(session.CheckoutPaymentID),
		invoiceID: text(session.CheckoutInvoiceID),
		walletID:  f.walletID,
		credits:   credits,
		matureAt:  session.ExpiresAt.Add(expiryCleanupSlack),
	}
	p.mu.Unlock()
	return nil
}

// verifyExpired asserts the cleanup job failed the top-up, removed its draft
// invoice and payment, credited nothing, and marked the session expired.
func (p *checkoutLink) verifyExpired(ctx context.Context, f *paymentFlow, w *expiryWatch) error {
	ids := map[string]string{"checkout_session_id": w.sessionID, "payment_id": w.paymentID, "invoice_id": w.invoiceID}
	f.walletID = w.walletID

	pays, err := f.client.Payments().ListPayments(ctx, dtos.ListPaymentsRequest{PaymentIds: []string{w.paymentID}})
	if err != nil {
		return f.fail("expiry_read_payment", ids, "list payments: %w", err)
	}
	if pays != nil && pays.ListPaymentsResponse != nil {
		for _, pay := range pays.ListPaymentsResponse.Items {
			if text(pay.ID) == w.paymentID && derefPaymentStatus(pay.PaymentStatus) == types.PaymentStatusSucceeded {
				return f.fail("expiry_assert_unpaid", ids, "abandoned session's payment is SUCCEEDED")
			}
		}
	}

	inv, err := f.client.Invoices().Get(ctx, w.invoiceID)
	switch {
	case err != nil && !world.IsNotFound(err):
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

	resp, err := f.client.Payments().GetCheckoutSession(ctx, w.sessionID)
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
