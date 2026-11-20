package scenarios

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/scenario"
	"github.com/flexprice/go-sdk/v2/models/dtos"
	"github.com/flexprice/go-sdk/v2/models/types"
	ptr "github.com/flexprice/go-sdk/v2/types"
	"github.com/shopspring/decimal"
)

var paymentPlanPrice = decimal.NewFromInt(10)

// autoCharge charges a fixed customer's hand-saved cards with nobody on a hosted page:
// set_default, wallet_topup, pay_invoice, create_subscription, modify_subscription,
// add_addon, refund, decline and cancel_subscription, each an independent leg.
type autoCharge struct {
	poll time.Duration
}

func newAutoCharge(scenario.Deps) *autoCharge { return &autoCharge{poll: defaultPaymentPoll} }

func (*autoCharge) Name() string               { return "auto-charge" }
func (*autoCharge) Kind() e2eprobe.Kind        { return e2eprobe.KindFlow }
func (*autoCharge) Requires() []e2eprobe.Trait { return []e2eprobe.Trait{e2eprobe.TraitPayment} }

func (p *autoCharge) Run(ctx context.Context, sc *scenario.Context) e2eprobe.Result {
	opts, stop := paymentPrelude(ctx, sc, p.poll, capAutoCharge)
	if stop != nil {
		return *stop
	}
	if opts.fixedCustomer == "" {
		return sc.Skip("fixed_customer", "persona names no fixed customer with saved cards")
	}
	if gap := sc.Persona().Gap(e2eprobe.TraitPayment); gap != "" {
		return sc.Skip("fixed_customer", gap)
	}
	sc.Attr("fixed_customer", opts.fixedCustomer)
	f := newPaymentFlow(sc, opts, "autocharge")
	saved, err := p.prepareFixedCustomer(ctx, f)
	if err != nil {
		return sc.Fail(stepFrom(err, "fixed_customer"), err)
	}

	legs := &legResults{}
	legs.runIf(len(saved.good) >= 2 && opts.supports(capSetDefault), "set_default", func() error {
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
	legs.runKnown(opts.knownIssue("refund"), invoiceID != "", "refund", func() error {
		return p.refundPaidInvoice(ctx, f, invoiceID)
	})
	legs.runKnown(opts.knownIssue("decline"), saved.declineID != "", "decline", func() error {
		return p.chargeDecliningCard(ctx, f, saved.portalToken, saved.declineID, saved.defaultID)
	})
	// The fixed customer is never deleted by the janitor, so its subscriptions
	// would otherwise pile up and renew against the saved card.
	legs.runIf(subID != "", "cancel_subscription", func() error {
		return p.cancelSubscription(ctx, f, subID)
	})
	return legs.result(sc, "auto-charge")
}

func (p *autoCharge) cancelSubscription(ctx context.Context, f *paymentFlow, subID string) error {
	_, err := f.client.Subscriptions().Cancel(ctx, subID, types.CancelSubscriptionRequest{
		CancellationType:               types.CancellationTypeImmediate,
		CancelImmediatelyInvoicePolicy: types.CancelImmediatelyInvoicePolicySkip.ToPointer(),
		Reason:                         ptr.String("e2eprobe payment autocharge cleanup"),
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

// prepareFixedCustomer splits the fixed customer's saved methods into good cards and
// the declining card, restoring a good default in case an earlier run stopped mid-decline.
func (p *autoCharge) prepareFixedCustomer(ctx context.Context, f *paymentFlow) (*savedCards, error) {
	if err := f.usePersistentCustomer(ctx, f.opts.fixedCustomer); err != nil {
		return nil, err
	}
	if err := f.useOrCreateWallet(ctx); err != nil {
		return nil, err
	}
	methods, err := f.client.Payments().ListSavedMethods(ctx, f.customerID, f.provider())
	if err != nil {
		return nil, f.fail("fixed_customer_mandate", nil, "list saved methods: %w", err)
	}

	saved := &savedCards{}
	defaultIsGood := false
	for _, m := range methods {
		if !m.CanAutoCharge {
			continue
		}
		if last4 := f.opts.declineLast4; last4 != "" && m.Last4() == last4 {
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
	if !f.opts.supports(capSetDefault) {
		return saved, nil
	}

	saved.portalToken, err = f.client.Payments().CreatePortalSession(ctx, f.externalID)
	if err != nil {
		return nil, f.fail("portal_session", nil, "create portal session: %w", err)
	}
	if !defaultIsGood {
		if _, err := f.client.Payments().PortalSetDefaultMethod(ctx, saved.portalToken, f.provider(), saved.good[0]); err != nil {
			return nil, f.fail("fixed_customer_restore_default", map[string]string{"payment_method_id": saved.good[0]}, "make a good card default: %w", err)
		}
		saved.defaultID = saved.good[0]
	}
	return saved, nil
}

// switchDefaultCard makes the other good card the default and checks the gateway
// agrees, so successive runs alternate between the two.
func (p *autoCharge) switchDefaultCard(ctx context.Context, f *paymentFlow, saved *savedCards) error {
	target := saved.good[0]
	for _, id := range saved.good {
		if id != saved.defaultID {
			target = id
			break
		}
	}
	ids := map[string]string{"payment_method_id": target, "previous_default": saved.defaultID}

	methods, err := f.client.Payments().PortalSetDefaultMethod(ctx, saved.portalToken, f.provider(), target)
	if err != nil {
		return f.fail("set_default", ids, "portal set default: %w", err)
	}
	if m, ok := findSavedMethod(methods, target); !ok || !m.IsDefault {
		return f.fail("set_default_assert_response", ids, "set-default response does not mark the card default")
	}
	listed, err := f.client.Payments().ListSavedMethods(ctx, f.customerID, f.provider())
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
func (p *autoCharge) walletAutoCharge(ctx context.Context, f *paymentFlow) error {
	before, err := f.credits(ctx)
	if err != nil {
		return err
	}
	session, err := f.startTopUp(ctx, paymentTopUpCredits, types.CollectionMethodChargeAutomatically, false)
	if err != nil {
		return f.fail("topup_autocharge_start", nil, "start auto-charged top-up: %w", err)
	}
	sessionID := text(session.ID)
	paymentID := text(session.CheckoutPaymentID)
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
		if _, err := f.client.Payments().GetCheckoutSession(ctx, sessionID); err != nil {
			return f.fail("topup_autocharge_reread", ids, "re-read session: %w", err)
		}
	}
	return f.expectCredits(ctx, "topup_autocharge_credits_once", want)
}

// invoiceAutoCharge returns the paid invoice so the refund leg can credit it.
func (p *autoCharge) invoiceAutoCharge(ctx context.Context, f *paymentFlow) (string, error) {
	session, err := f.startInvoiceCheckout(ctx, paymentInvoiceAmount, types.CollectionMethodChargeAutomatically)
	if err != nil {
		return "", f.fail("invoice_autocharge_start", nil, "start auto-charged invoice: %w", err)
	}

	final, err := f.waitTerminal(ctx, "invoice_autocharge_session", text(session.ID))
	if err != nil {
		return "", err
	}
	if err := f.expectCompleted("invoice_autocharge_session", final); err != nil {
		return "", err
	}
	invoiceID := text(final.CheckoutInvoiceID)
	ids := map[string]string{"invoice_id": invoiceID}
	inv, err := f.client.Invoices().Get(ctx, invoiceID)
	if err != nil {
		return "", f.fail("invoice_autocharge_read", ids, "get invoice: %w", err)
	}
	if inv == nil || inv.InvoiceResponse == nil {
		return "", f.fail("invoice_autocharge_read", ids, "get invoice: empty response")
	}
	if inv.InvoiceResponse.PaymentStatus == nil || *inv.InvoiceResponse.PaymentStatus != types.PaymentStatusSucceeded {
		ids["payment_status"] = fmt.Sprint(derefPaymentStatus(inv.InvoiceResponse.PaymentStatus))
		return "", f.fail("invoice_autocharge_assert_paid", ids, "invoice payment_status is %s, want SUCCEEDED", ids["payment_status"])
	}
	paid, _ := decimal.NewFromString(text(inv.InvoiceResponse.AmountPaid))
	if !paid.Equal(paymentInvoiceAmount) {
		ids["amount_paid"] = paid.String()
		return "", f.fail("invoice_autocharge_assert_amount", ids, "invoice amount_paid %s, want %s", paid, paymentInvoiceAmount)
	}
	return invoiceID, nil
}

// subscriptionAutoCharge returns the activated subscription for the modify and addon legs.
func (p *autoCharge) subscriptionAutoCharge(ctx context.Context, f *paymentFlow) (string, error) {
	planID, err := p.ensurePlan(ctx, f)
	if err != nil {
		return "", err
	}
	cfg := f.checkoutParams(types.CollectionMethodChargeAutomatically, false)
	resp, err := f.client.Payments().CreateCheckoutSession(ctx, types.CreateCheckoutSessionRequest{
		CustomerExternalID: f.externalID,
		Action:             types.CheckoutActionCreateSubscription,
		Configuration: &types.CheckoutConfiguration{
			CreateSubscriptionParams: &types.CreateSubscriptionParams{
				PlanID:        ptr.String(planID),
				Currency:      ptr.String(f.opts.currency),
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
	sessionID := text(resp.CheckoutSessionResponse.ID)

	final, err := f.waitTerminal(ctx, "subscription_autocharge_session", sessionID)
	if err != nil {
		return "", err
	}
	if err := f.expectCompleted("subscription_autocharge_session", final); err != nil {
		return "", err
	}
	invoiceID := text(final.CheckoutInvoiceID)
	ids := map[string]string{"invoice_id": invoiceID}
	inv, err := f.client.Invoices().Get(ctx, invoiceID)
	if err != nil {
		return "", f.fail("subscription_autocharge_read_invoice", ids, "get checkout invoice: %w", err)
	}
	if inv == nil || inv.InvoiceResponse == nil {
		return "", f.fail("subscription_autocharge_read_invoice", ids, "get checkout invoice: empty response")
	}
	subID := text(inv.InvoiceResponse.SubscriptionID)
	if subID == "" {
		return "", f.fail("subscription_autocharge_read_invoice", ids, "checkout invoice has no subscription_id")
	}

	sub, err := f.client.Subscriptions().Get(ctx, subID)
	if err != nil {
		return "", f.fail("subscription_autocharge_read_sub", map[string]string{"subscription_id": subID}, "get subscription: %w", err)
	}
	if status := subscriptionStatus(sub); status != string(types.SubscriptionStatusActive) {
		return "", f.fail("subscription_autocharge_assert_active", map[string]string{"subscription_id": subID, "subscription_status": status},
			"subscription is %s, want active", status)
	}
	return subID, nil
}

// chargeDecliningCard makes the declining card the default and asserts the charge
// neither completes nor credits, then restores goodMethodID as default.
func (p *autoCharge) chargeDecliningCard(ctx context.Context, f *paymentFlow, token, declineID, goodMethodID string) error {
	if !f.opts.supports(capSetDefault) {
		return nil
	}
	if _, err := f.client.Payments().PortalSetDefaultMethod(ctx, token, f.provider(), declineID); err != nil {
		return f.fail("decline_set_default", map[string]string{"payment_method_id": declineID}, "portal set default: %w", err)
	}
	defer func() {
		_, _ = f.client.Payments().PortalSetDefaultMethod(context.Background(), token, f.provider(), goodMethodID)
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
		sessionID := text(session.ID)
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
func (p *autoCharge) ensurePlan(ctx context.Context, f *paymentFlow) (string, error) {
	lookupKey := "e2eprobe_payments_plan_" + strings.ToLower(f.opts.currency)
	ids := map[string]string{"plan_lookup_key": lookupKey}

	resp, err := f.client.Plans().Query(ctx, types.PlanFilter{LookupKey: ptr.String(lookupKey)})
	if err != nil {
		return "", f.fail("ensure_plan", ids, "query plans: %w", err)
	}
	planID := ""
	if resp != nil && resp.ListPlansResponse != nil && len(resp.ListPlansResponse.Items) > 0 {
		planID = text(resp.ListPlansResponse.Items[0].ID)
	}
	if planID == "" {
		created, err := f.client.Plans().Create(ctx, types.CreatePlanRequest{
			Name:        "E2EProbe Payments Plan " + f.opts.currency,
			LookupKey:   ptr.String(lookupKey),
			Description: ptr.String("In-advance plan the e2eprobe payment scenarios subscribe to"),
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

	prices, err := f.client.Prices().Query(ctx, paymentPlanPrices(planID))
	if err != nil {
		return "", f.fail("ensure_plan_price", ids, "query plan prices: %w", err)
	}
	if prices != nil && prices.ListPricesResponse != nil && len(prices.ListPricesResponse.Items) > 0 {
		return planID, nil
	}
	if _, err := f.client.Prices().Create(ctx, types.CreatePriceRequest{
		EntityID:           planID,
		EntityType:         types.PriceEntityTypePlan,
		Type:               types.PriceTypeFixed,
		BillingModel:       types.BillingModelFlatFee,
		BillingPeriod:      types.BillingPeriodMonthly,
		BillingPeriodCount: ptr.Int64(1),
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		PriceUnitType:      types.PriceUnitTypeFiat,
		Amount:             ptr.String(paymentPlanPrice.StringFixed(2)),
		Currency:           f.opts.currency,
		DisplayName:        ptr.String("E2EProbe Payments Platform Fee"),
	}); err != nil {
		return "", f.fail("ensure_plan_price", ids, "create plan price: %w", err)
	}
	return planID, nil
}

// paymentPlanPrices lists the published prices on a plan.
func paymentPlanPrices(planID string) types.PriceFilter {
	entityType := types.PriceEntityTypePlan
	return types.PriceFilter{
		EntityIds:  []string{planID},
		EntityType: &entityType,
		Status:     types.StatusPublished.ToPointer(),
		Limit:      ptr.Int64(1000),
	}
}

// subscriptionStatus is the observed status, "unknown" when the response lacks one.
func subscriptionStatus(resp *dtos.GetSubscriptionResponse) string {
	if resp == nil || resp.SubscriptionResponse == nil || resp.SubscriptionResponse.SubscriptionStatus == nil {
		return "unknown"
	}
	return string(*resp.SubscriptionResponse.SubscriptionStatus)
}

func derefPaymentStatus(s *types.PaymentStatus) types.PaymentStatus {
	if s == nil {
		return ""
	}
	return *s
}
