package checks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/go-sdk/v2/models/types"
	"github.com/shopspring/decimal"
)

var paymentPlanPrice = decimal.NewFromInt(10)

// PaymentAutoChargeProbe charges a fixed customer's hand-saved cards or mandates,
// with nobody on a hosted page:
//   - set_default: the default alternates between two good cards each Run;
//   - wallet_topup: the payment settles before anything reads the session (the
//     webhook path), the session completes, and credits land exactly once even
//     across repeated reconciling reads;
//   - pay_invoice: the invoice ends SUCCEEDED with the full amount paid;
//   - create_subscription: the subscription ends active;
//   - decline: the saved declining card leaves no credits and no completed session.
//
// Registered only for gateways with a fixed customer configured.
type PaymentAutoChargeProbe struct {
	client e2eprobe.Client
	reg    e2eprobe.Registry
	runID  string
	lg     *logger.Logger
	opts   PaymentProbeOpts
}

func NewPaymentAutoChargeProbe(c e2eprobe.Client, r e2eprobe.Registry, runID string, lg *logger.Logger, opts PaymentProbeOpts) *PaymentAutoChargeProbe {
	return &PaymentAutoChargeProbe{client: c, reg: r, runID: runID, lg: lg, opts: opts}
}

func (p *PaymentAutoChargeProbe) Name() string {
	return "payment-autocharge-probe-" + p.opts.Provider.Provider
}

func (p *PaymentAutoChargeProbe) Kind() e2eprobe.Kind { return e2eprobe.KindScenario }

func (p *PaymentAutoChargeProbe) Run(ctx context.Context) error {
	if p.opts.Provider.FixedCustomerExternalID == "" || !p.opts.supports(capAutoCharge) {
		return nil
	}
	f := newPaymentFlow(p.client, p.reg, p.runID, p.opts, "persistent-payment-autocharge")
	saved, err := p.prepareFixedCustomer(ctx, f)
	if err != nil {
		return err
	}

	legs := &legResults{}
	legs.runIf(len(saved.good) >= 2 && p.opts.supports(capSetDefault), "set_default", func() error {
		return p.switchDefaultCard(ctx, f, saved)
	})
	legs.run("wallet_topup", func() error { return p.walletAutoCharge(ctx, f) })

	var invoiceID string
	legs.run("pay_invoice", func() (err error) {
		invoiceID, err = p.invoiceAutoCharge(ctx, f)
		return err
	})

	var subID string
	legs.run("create_subscription", func() (err error) {
		subID, err = p.subscriptionAutoCharge(ctx, f)
		return err
	})
	legs.runIf(subID != "", "modify_subscription", func() error {
		return p.modifySubscriptionAutoCharge(ctx, f, subID)
	})
	legs.runIf(subID != "", "add_addon", func() error {
		return p.addonAutoCharge(ctx, f, subID)
	})
	legs.runKnown(p.opts.knownIssue("refund"), invoiceID != "", "refund", func() error {
		return p.refundPaidInvoice(ctx, f, invoiceID)
	})
	legs.runKnown(p.opts.knownIssue("decline"), saved.declineID != "", "decline", func() error {
		return p.chargeDecliningCard(ctx, f, saved.portalToken, saved.declineID, saved.defaultID)
	})
	// The fixed mandate customer is never deleted by the janitor, so its
	// subscriptions would otherwise pile up and renew against the mandate.
	legs.runIf(subID != "", "cancel_subscription", func() error {
		return p.cancelSubscription(ctx, f, subID)
	})
	return legs.err(f)
}

func (p *PaymentAutoChargeProbe) cancelSubscription(ctx context.Context, f *paymentFlow, subID string) error {
	_, err := p.client.Subscriptions().Cancel(ctx, subID, types.CancelSubscriptionRequest{
		CancellationType:               types.CancellationTypeImmediate,
		CancelImmediatelyInovicePolicy: types.CancelImmediatelyInvoicePolicySkip.ToPointer(),
		Reason:                         strPtr("e2eprobe payment autocharge cleanup"),
	})
	if err != nil {
		return f.fail("cancel_subscription", map[string]string{"subscription_id": subID}, "cancel subscription: %w", err)
	}
	return nil
}

// savedCards are the fixed customer's hand-saved methods, split by behaviour.
type savedCards struct {
	portalToken string
	good        []string
	declineID   string
	// defaultID is the good card currently set as default.
	defaultID string
}

// prepareFixedCustomer sorts the fixed customer's saved methods into good cards
// and the declining card, and makes sure a good card is the default before any
// charge, in case an earlier Run stopped while the declining card was default.
func (p *PaymentAutoChargeProbe) prepareFixedCustomer(ctx context.Context, f *paymentFlow) (*savedCards, error) {
	if err := f.usePersistentCustomer(ctx, p.opts.Provider.FixedCustomerExternalID); err != nil {
		return nil, err
	}
	if err := f.useOrCreateWallet(ctx); err != nil {
		return nil, err
	}
	methods, err := p.client.Payments().ListSavedMethods(ctx, f.customerID, f.provider())
	if err != nil {
		return nil, f.fail("fixed_customer_mandate", nil, "list saved methods: %w", err)
	}

	saved := &savedCards{}
	defaultIsGood := false
	for _, m := range methods {
		if !m.CanAutoCharge {
			continue
		}
		if last4 := p.opts.Provider.DeclineCardLast4; last4 != "" && m.Last4() == last4 {
			saved.declineID = m.ID
			continue
		}
		saved.good = append(saved.good, m.ID)
		if m.IsDefault {
			saved.defaultID = m.ID
			defaultIsGood = true
		}
	}
	if len(saved.good) == 0 {
		return nil, f.fail("fixed_customer_mandate", nil, "fixed customer has no auto-chargeable method other than the declining card; save a card or re-authorize its mandate")
	}
	if !p.opts.supports(capSetDefault) {
		return saved, nil
	}

	saved.portalToken, err = p.client.Payments().CreatePortalSession(ctx, f.externalID)
	if err != nil {
		return nil, f.fail("portal_session", nil, "create portal session: %w", err)
	}
	if !defaultIsGood {
		if _, err := p.client.Payments().PortalSetDefaultMethod(ctx, saved.portalToken, f.provider(), saved.good[0]); err != nil {
			return nil, f.fail("fixed_customer_restore_default", map[string]string{"payment_method_id": saved.good[0]}, "make a good card default: %w", err)
		}
		saved.defaultID = saved.good[0]
	}
	return saved, nil
}

// switchDefaultCard makes the other good card the default and checks the gateway
// agrees, so successive Runs alternate between the two.
func (p *PaymentAutoChargeProbe) switchDefaultCard(ctx context.Context, f *paymentFlow, saved *savedCards) error {
	target := saved.good[0]
	for _, id := range saved.good {
		if id != saved.defaultID {
			target = id
			break
		}
	}
	ids := map[string]string{"payment_method_id": target, "previous_default": saved.defaultID}

	methods, err := p.client.Payments().PortalSetDefaultMethod(ctx, saved.portalToken, f.provider(), target)
	if err != nil {
		return f.fail("set_default", ids, "portal set default: %w", err)
	}
	if m, ok := findSavedMethod(methods, target); !ok || !m.IsDefault {
		return f.fail("set_default_assert_response", ids, "set-default response does not mark the card default")
	}
	listed, err := p.client.Payments().ListSavedMethods(ctx, f.customerID, f.provider())
	if err != nil {
		return f.fail("set_default_list", ids, "list saved methods: %w", err)
	}
	for _, m := range listed {
		if m.IsDefault != (m.ID == target) {
			return f.fail("set_default_assert_listed", ids, "after set-default, %s lists is_default=%t", m.ID, m.IsDefault)
		}
	}
	saved.defaultID = target
	return nil
}

// walletAutoCharge separates the webhook path from the reconcile path: the
// payment is read first (no reconcile), then the session (which reconciles).
func (p *PaymentAutoChargeProbe) walletAutoCharge(ctx context.Context, f *paymentFlow) error {
	before, err := f.credits(ctx)
	if err != nil {
		return err
	}
	session, err := f.startTopUp(ctx, paymentTopUpCredits, types.CollectionMethodChargeAutomatically, false)
	if err != nil {
		return f.fail("topup_autocharge_start", nil, "start auto-charged top-up: %w", err)
	}
	sessionID := derefStr(session.ID)
	paymentID := derefStr(session.CheckoutPaymentID)
	ids := map[string]string{"checkout_session_id": sessionID, "payment_id": paymentID}
	if paymentID == "" {
		return f.fail("topup_autocharge_start", ids, "session has no checkout_payment_id")
	}

	status, err := f.waitPaymentSettled(ctx, "topup_autocharge_webhook", paymentID)
	if err != nil {
		return err
	}
	if status != types.PaymentStatusSucceeded {
		return f.fail("topup_autocharge_webhook", ids, "payment settled as %s, want SUCCEEDED", status)
	}

	final, err := f.waitTerminal(ctx, "topup_autocharge_session", sessionID)
	if err != nil {
		return err
	}
	if err := f.expectCompleted("topup_autocharge_session", final); err != nil {
		return err
	}

	want := before.Add(paymentTopUpCredits)
	if err := f.expectCredits(ctx, "topup_autocharge_credits", want); err != nil {
		return err
	}
	for i := 0; i < 3; i++ {
		if _, err := p.client.Payments().GetCheckoutSession(ctx, sessionID); err != nil {
			return f.fail("topup_autocharge_reread", ids, "re-read session: %w", err)
		}
	}
	return f.expectCredits(ctx, "topup_autocharge_credits_once", want)
}

// invoiceAutoCharge returns the paid invoice so the refund leg can credit it.
func (p *PaymentAutoChargeProbe) invoiceAutoCharge(ctx context.Context, f *paymentFlow) (string, error) {
	session, err := f.startInvoiceCheckout(ctx, paymentInvoiceAmount, types.CollectionMethodChargeAutomatically)
	if err != nil {
		return "", f.fail("invoice_autocharge_start", nil, "start auto-charged invoice: %w", err)
	}

	final, err := f.waitTerminal(ctx, "invoice_autocharge_session", derefStr(session.ID))
	if err != nil {
		return "", err
	}
	if err := f.expectCompleted("invoice_autocharge_session", final); err != nil {
		return "", err
	}
	invoiceID := derefStr(final.CheckoutInvoiceID)
	inv, err := p.client.Invoices().Get(ctx, invoiceID)
	if err != nil {
		return "", f.fail("invoice_autocharge_read", map[string]string{"invoice_id": invoiceID}, "get invoice: %w", err)
	}
	if inv == nil || inv.InvoiceResponse == nil {
		return "", f.fail("invoice_autocharge_read", map[string]string{"invoice_id": invoiceID}, "get invoice: empty response")
	}
	ids := map[string]string{"invoice_id": invoiceID}
	if inv.InvoiceResponse.PaymentStatus == nil || *inv.InvoiceResponse.PaymentStatus != types.PaymentStatusSucceeded {
		ids["payment_status"] = fmt.Sprint(derefPaymentStatus(inv.InvoiceResponse.PaymentStatus))
		return "", f.fail("invoice_autocharge_assert_paid", ids, "invoice payment_status is %s, want SUCCEEDED", ids["payment_status"])
	}
	paid, _ := decimal.NewFromString(derefStr(inv.InvoiceResponse.AmountPaid))
	if !paid.Equal(paymentInvoiceAmount) {
		ids["amount_paid"] = paid.String()
		return "", f.fail("invoice_autocharge_assert_amount", ids, "invoice amount_paid %s, want %s", paid, paymentInvoiceAmount)
	}
	return invoiceID, nil
}

// subscriptionAutoCharge returns the activated subscription for the modify and addon legs.
func (p *PaymentAutoChargeProbe) subscriptionAutoCharge(ctx context.Context, f *paymentFlow) (string, error) {
	planID, err := p.ensurePlan(ctx, f)
	if err != nil {
		return "", err
	}
	cfg := f.checkoutParams(types.CollectionMethodChargeAutomatically, false)
	resp, err := p.client.Payments().CreateCheckoutSession(ctx, types.CreateCheckoutSessionRequest{
		CustomerExternalID: f.externalID,
		Action:             types.CheckoutActionCreateSubscription,
		Configuration: &types.CheckoutConfiguration{
			CreateSubscriptionParams: &types.CreateSubscriptionParams{
				PlanID:        strPtr(planID),
				Currency:      strPtr(f.opts.Provider.Currency),
				BillingPeriod: types.BillingPeriodMonthly.ToPointer(),
			},
		},
		PaymentProvider:       cfg.PaymentProvider,
		PaymentProviderConfig: cfg.PaymentProviderConfig,
		SuccessURL:            cfg.SuccessURL,
		FailureURL:            cfg.FailureURL,
		CancelURL:             cfg.CancelURL,
		Metadata:              cfg.Metadata,
	})
	if err != nil {
		return "", f.fail("subscription_autocharge_start", map[string]string{"plan_id": planID}, "create checkout session: %w", err)
	}
	if resp == nil || resp.CheckoutSessionResponse == nil {
		return "", f.fail("subscription_autocharge_start", map[string]string{"plan_id": planID}, "create checkout session: empty response")
	}
	sessionID := derefStr(resp.CheckoutSessionResponse.ID)

	final, err := f.waitTerminal(ctx, "subscription_autocharge_session", sessionID)
	if err != nil {
		return "", err
	}
	if err := f.expectCompleted("subscription_autocharge_session", final); err != nil {
		return "", err
	}
	invoiceID := derefStr(final.CheckoutInvoiceID)
	inv, err := p.client.Invoices().Get(ctx, invoiceID)
	if err != nil {
		return "", f.fail("subscription_autocharge_read_invoice", map[string]string{"invoice_id": invoiceID}, "get checkout invoice: %w", err)
	}
	if inv == nil || inv.InvoiceResponse == nil {
		return "", f.fail("subscription_autocharge_read_invoice", map[string]string{"invoice_id": invoiceID}, "get checkout invoice: empty response")
	}
	subID := derefStr(inv.InvoiceResponse.SubscriptionID)
	if subID == "" {
		return "", f.fail("subscription_autocharge_read_invoice", map[string]string{"invoice_id": invoiceID}, "checkout invoice has no subscription_id")
	}
	p.reg.RegisterEphemeral("subscription", subID, time.Now().UTC())

	sub, err := p.client.Subscriptions().Get(ctx, subID)
	if err != nil {
		return "", f.fail("subscription_autocharge_read_sub", map[string]string{"subscription_id": subID}, "get subscription: %w", err)
	}
	if status := observedSubStatus(sub); status != string(types.SubscriptionStatusActive) {
		return "", f.fail("subscription_autocharge_assert_active", map[string]string{"subscription_id": subID, "subscription_status": status},
			"subscription is %s, want active", status)
	}
	return subID, nil
}

// chargeDecliningCard makes the declining card the default and asserts the charge
// neither completes nor credits, then restores goodMethodID as default.
func (p *PaymentAutoChargeProbe) chargeDecliningCard(ctx context.Context, f *paymentFlow, token, declineID, goodMethodID string) error {
	if !p.opts.supports(capSetDefault) {
		return nil
	}
	if _, err := p.client.Payments().PortalSetDefaultMethod(ctx, token, f.provider(), declineID); err != nil {
		return f.fail("decline_set_default", map[string]string{"payment_method_id": declineID}, "portal set default: %w", err)
	}
	defer func() {
		_, _ = p.client.Payments().PortalSetDefaultMethod(context.Background(), token, f.provider(), goodMethodID)
	}()

	before, err := f.credits(ctx)
	if err != nil {
		return err
	}
	session, err := f.startTopUp(ctx, paymentTopUpCredits, types.CollectionMethodChargeAutomatically, false)
	switch {
	case err != nil && !isClientRejection(err):
		return f.fail("decline_assert_rejected", map[string]string{"payment_method_id": declineID}, "declined charge must be a clean 4xx or a failed session: %w", err)
	case err == nil:
		sessionID := derefStr(session.ID)
		final, err := f.waitTerminal(ctx, "decline_session", sessionID)
		if err != nil {
			return err
		}
		if sessionStatus(final) == string(types.CheckoutStatusCompleted) {
			return f.fail("decline_assert_not_completed", map[string]string{"checkout_session_id": sessionID, "payment_method_id": declineID},
				"session completed on a declining card")
		}
	}
	return f.expectCredits(ctx, "decline_assert_no_credit", before)
}

// ensurePlan returns the per-currency payments plan, creating it with a single
// in-advance fixed price so a new subscription's first invoice is non-zero.
func (p *PaymentAutoChargeProbe) ensurePlan(ctx context.Context, f *paymentFlow) (string, error) {
	lookupKey := "e2eprobe_payments_plan_" + strings.ToLower(f.opts.Provider.Currency)
	ids := map[string]string{"plan_lookup_key": lookupKey}

	resp, err := p.client.Plans().Query(ctx, types.PlanFilter{LookupKey: strPtr(lookupKey)})
	if err != nil {
		return "", f.fail("ensure_plan", ids, "query plans: %w", err)
	}
	planID := ""
	if resp != nil && resp.ListPlansResponse != nil && len(resp.ListPlansResponse.Items) > 0 {
		planID = derefStr(resp.ListPlansResponse.Items[0].ID)
	}
	if planID == "" {
		created, err := p.client.Plans().Create(ctx, types.CreatePlanRequest{
			Name:        "E2EProbe Payments Plan " + f.opts.Provider.Currency,
			LookupKey:   strPtr(lookupKey),
			Description: strPtr("In-advance plan the e2eprobe payment probes subscribe to"),
			Metadata:    map[string]string{"e2eprobe": "true", "e2eprobe_role": "seed"},
		})
		if err != nil {
			return "", f.fail("ensure_plan", ids, "create plan: %w", err)
		}
		if created == nil || created.PlanResponse == nil || created.PlanResponse.ID == nil {
			return "", f.fail("ensure_plan", ids, "create plan: empty response")
		}
		planID = *created.PlanResponse.ID
	}
	ids["plan_id"] = planID

	prices, err := p.client.Prices().Query(ctx, planPriceQueryFilter(planID))
	if err != nil {
		return "", f.fail("ensure_plan_price", ids, "query plan prices: %w", err)
	}
	if prices != nil && prices.ListPricesResponse != nil && len(prices.ListPricesResponse.Items) > 0 {
		return planID, nil
	}
	if _, err := p.client.Prices().Create(ctx, types.CreatePriceRequest{
		EntityID:           planID,
		EntityType:         types.PriceEntityTypePlan,
		Type:               types.PriceTypeFixed,
		BillingModel:       types.BillingModelFlatFee,
		BillingPeriod:      types.BillingPeriodMonthly,
		BillingPeriodCount: int64Ptr(1),
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		PriceUnitType:      types.PriceUnitTypeFiat,
		Amount:             strPtr(paymentPlanPrice.StringFixed(2)),
		Currency:           f.opts.Provider.Currency,
		DisplayName:        strPtr("E2EProbe Payments Platform Fee"),
	}); err != nil {
		return "", f.fail("ensure_plan_price", ids, "create plan price: %w", err)
	}
	return planID, nil
}

func derefPaymentStatus(s *types.PaymentStatus) types.PaymentStatus {
	if s == nil {
		return ""
	}
	return *s
}
