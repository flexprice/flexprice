package checks

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/flexprice/go-sdk/v2/models/types"
)

// autoChargeFixture models a gateway that settles every auto-charge: top-ups
// credit the wallet, the invoice is paid, the subscription is active, a paid
// quantity change adds a quantity-2 line item, a paid addon turns active and a
// refund settles (to the card on refunding gateways, else to the wallet). A
// top-up while the declining card is default is refused with 402.
func autoChargeFixture(t *testing.T, provider string) (*paymentFixture, *fakeGatewayDriver) {
	t.Helper()
	fx := newPaymentFixture(t, provider, nil)
	driver := &fakeGatewayDriver{payments: &fx.fc.payments}
	fx.opts.Driver = driver

	fx.fc.payments.start = func(action string, cfg *types.CheckoutParams) (*types.CheckoutSessionResponse, error) {
		if action == "topup" && collectionOf(cfg) == types.CollectionMethodChargeAutomatically {
			defaults := fx.fc.payments.defaults
			if len(defaults) > 0 && strings.Contains(defaults[len(defaults)-1], string(TestCardDecline)) {
				return nil, apiError(402)
			}
			fx.addCredits(25)
		}
		return fakePendingSession(), nil
	}
	fx.fc.invoices.getByID = map[string]types.InvoiceResponse{
		"inv_2": {
			ID: strPtr("inv_2"), PaymentStatus: types.PaymentStatusSucceeded.ToPointer(), AmountPaid: strPtr("15.00"),
			LineItems: []types.InvoiceLineItemResponse{{ID: strPtr("inv_li_1")}},
		},
		"inv_3": {ID: strPtr("inv_3"), SubscriptionID: strPtr("sub_1")},
	}
	planItem := types.SubscriptionSubscriptionLineItem{
		EndDate: &time.Time{}, // the API reports an open line item with a zero end_date
		ID:      strPtr("li_1"), PriceID: strPtr("price_1"), Quantity: strPtr("1"),
		PriceType: types.PriceTypeFixed.ToPointer(), InvoiceCadence: types.InvoiceCadenceAdvance.ToPointer(),
	}
	fx.fc.subs.subs = map[string]types.SubscriptionResponse{
		"sub_1": {ID: strPtr("sub_1"), SubscriptionStatus: types.SubscriptionStatusActive.ToPointer(), LineItems: []types.SubscriptionSubscriptionLineItem{planItem}},
	}
	fx.fc.payments.onModify = func(subID string, _ types.ExecuteSubscriptionModifyRequest) {
		fx.fc.subs.mu.Lock()
		defer fx.fc.subs.mu.Unlock()
		sub := fx.fc.subs.subs[subID]
		ended := planItem
		endedAt := time.Now().Add(-time.Minute)
		ended.EndDate = &endedAt
		replaced := planItem
		replaced.ID, replaced.Quantity = strPtr("li_2"), strPtr("2")
		sub.LineItems = []types.SubscriptionSubscriptionLineItem{ended, replaced}
		fx.fc.subs.subs[subID] = sub
	}
	fx.fc.payments.addonAssociations = []types.AddonAssociationResponse{
		{ID: strPtr("assoc_1"), AddonStatus: types.AddonStatusActive.ToPointer()},
	}
	fx.fc.payments.onCreditNote = func(req types.CreateCreditNoteRequest) {
		fx.fc.invoices.mu.Lock()
		inv := fx.fc.invoices.getByID[req.InvoiceID]
		inv.PaymentStatus = types.PaymentStatusRefunded.ToPointer()
		fx.fc.invoices.getByID[req.InvoiceID] = inv
		fx.fc.invoices.mu.Unlock()

		refund := types.RefundResponse{
			Amount: strPtr("15.00"), RefundStatus: types.RefundStatusSucceeded.ToPointer(),
			RefundDestination: types.RefundDestinationWallet.ToPointer(),
		}
		if gatewaysRefundingToSource[provider] {
			refund.RefundDestination = types.RefundDestinationGateway.ToPointer()
			refund.GatewayRefundID = strPtr("rf_1")
		} else {
			fx.addCredits(15)
		}
		fx.fc.payments.refunds = []types.RefundResponse{refund}
	}
	return fx, driver
}

func TestPaymentAutoChargeProbe(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(fx *paymentFixture, d *fakeGatewayDriver)
		wantStep string
	}{
		{name: "happy path charges top-up, invoice and subscription, refuses decline"},
		{
			name:  "no declining card configured skips the decline leg",
			setup: func(_ *paymentFixture, d *fakeGatewayDriver) { d.declineUnsupported = true },
		},
		{
			name: "webhook never settles the payment",
			setup: func(fx *paymentFixture, _ *fakeGatewayDriver) {
				fx.fc.payments.paymentStatus = types.PaymentStatusPending
			},
			wantStep: "topup_autocharge_webhook",
		},
		{
			name: "payment settles as failed",
			setup: func(fx *paymentFixture, _ *fakeGatewayDriver) {
				fx.fc.payments.paymentStatus = types.PaymentStatusFailed
			},
			wantStep: "topup_autocharge_webhook",
		},
		{
			name: "session never completes",
			setup: func(fx *paymentFixture, _ *fakeGatewayDriver) {
				fx.fc.payments.sessionOnGet = func(s *types.CheckoutSessionResponse) *types.CheckoutSessionResponse {
					out := *s
					out.CheckoutStatus = types.CheckoutStatusFailed.ToPointer()
					out.Terminal = boolPtr(true)
					return &out
				}
			},
			wantStep: "topup_autocharge_session",
		},
		{
			name: "top-up credits twice",
			setup: func(fx *paymentFixture, _ *fakeGatewayDriver) {
				inner := fx.fc.payments.start
				fx.fc.payments.start = func(action string, cfg *types.CheckoutParams) (*types.CheckoutSessionResponse, error) {
					if action == "topup" {
						fx.addCredits(25)
					}
					return inner(action, cfg)
				}
			},
			wantStep: "topup_autocharge_credits",
		},
		{
			name: "invoice left unpaid",
			setup: func(fx *paymentFixture, _ *fakeGatewayDriver) {
				fx.fc.invoices.getByID["inv_2"] = types.InvoiceResponse{ID: strPtr("inv_2"), PaymentStatus: types.PaymentStatusPending.ToPointer()}
			},
			wantStep: "invoice_autocharge_assert_paid",
		},
		{
			name: "invoice partially paid",
			setup: func(fx *paymentFixture, _ *fakeGatewayDriver) {
				fx.fc.invoices.getByID["inv_2"] = types.InvoiceResponse{ID: strPtr("inv_2"), PaymentStatus: types.PaymentStatusSucceeded.ToPointer(), AmountPaid: strPtr("5.00")}
			},
			wantStep: "invoice_autocharge_assert_amount",
		},
		{
			name: "subscription not active",
			setup: func(fx *paymentFixture, _ *fakeGatewayDriver) {
				fx.fc.subs.subs["sub_1"] = types.SubscriptionResponse{ID: strPtr("sub_1"), SubscriptionStatus: types.SubscriptionStatusIncomplete.ToPointer()}
			},
			wantStep: "subscription_autocharge_assert_active",
		},
		{
			name: "quantity change applied without checkout",
			setup: func(fx *paymentFixture, _ *fakeGatewayDriver) {
				fx.fc.payments.ungated = map[string]bool{"modify": true}
			},
			wantStep: "modify_autocharge_assert_gated",
		},
		{
			name:     "paid quantity change not applied",
			setup:    func(fx *paymentFixture, _ *fakeGatewayDriver) { fx.fc.payments.onModify = nil },
			wantStep: "modify_autocharge_assert_quantity",
		},
		{
			name: "addon attached without checkout",
			setup: func(fx *paymentFixture, _ *fakeGatewayDriver) {
				fx.fc.payments.ungated = map[string]bool{"addon": true}
			},
			wantStep: "addon_autocharge_assert_gated",
		},
		{
			name:     "paid addon never activates",
			setup:    func(fx *paymentFixture, _ *fakeGatewayDriver) { fx.fc.payments.addonAssociations = nil },
			wantStep: "addon_autocharge_assert_active",
		},
		{
			name: "refund leaves invoice unrefunded",
			setup: func(fx *paymentFixture, _ *fakeGatewayDriver) {
				fx.fc.payments.onCreditNote = func(types.CreateCreditNoteRequest) {}
			},
			wantStep: "refund_assert_invoice_refunded",
		},
		{
			name: "refund stuck processing",
			setup: func(fx *paymentFixture, _ *fakeGatewayDriver) {
				inner := fx.fc.payments.onCreditNote
				fx.fc.payments.onCreditNote = func(req types.CreateCreditNoteRequest) {
					inner(req)
					fx.fc.payments.refunds[0].RefundStatus = types.RefundStatusProcessing.ToPointer()
				}
			},
			wantStep: "refund_assert_settles",
		},
		{
			name: "wallet refund credits nothing",
			setup: func(fx *paymentFixture, _ *fakeGatewayDriver) {
				inner := fx.fc.payments.onCreditNote
				fx.fc.payments.onCreditNote = func(req types.CreateCreditNoteRequest) {
					inner(req)
					fx.addCredits(-15)
				}
			},
			wantStep: "refund_assert_wallet_credit",
		},
		{
			name: "declining card still completes",
			setup: func(fx *paymentFixture, _ *fakeGatewayDriver) {
				inner := fx.fc.payments.start
				fx.fc.payments.start = func(action string, cfg *types.CheckoutParams) (*types.CheckoutSessionResponse, error) {
					defaults := fx.fc.payments.defaults
					if len(defaults) > 0 && strings.Contains(defaults[len(defaults)-1], string(TestCardDecline)) {
						return fakePendingSession(), nil
					}
					return inner(action, cfg)
				}
			},
			wantStep: "decline_assert_not_completed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx, driver := autoChargeFixture(t, "stripe")
			if tt.setup != nil {
				tt.setup(fx, driver)
			}
			err := NewPaymentAutoChargeProbe(fx.fc, fx.reg, "run1", nil, fx.opts).Run(context.Background())
			if tt.wantStep != "" {
				if err == nil {
					t.Fatalf("want failure at step %q, got nil", tt.wantStep)
				}
				if got := stepOf(err); got != tt.wantStep {
					t.Fatalf("step = %q, want %q (err: %v)", got, tt.wantStep, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if subs := fx.reg.Ephemerals("subscription"); len(subs) != 1 || subs[0].ID != "sub_1" {
				t.Errorf("ephemeral subscriptions = %v, want [sub_1]", subs)
			}
		})
	}
}

func TestPaymentAutoChargeProbe_ChargebeeRefundsToCard(t *testing.T) {
	fx, _ := autoChargeFixture(t, "chargebee")
	fx.opts.AssertKnownIssues = true
	if err := NewPaymentAutoChargeProbe(fx.fc, fx.reg, "run1", nil, fx.opts).Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(fx.fc.payments.creditNotes) != 1 || *fx.fc.payments.creditNotes[0].RefundTarget != types.RefundTargetBackToSource {
		t.Fatalf("credit notes = %+v, want one back-to-source refund", fx.fc.payments.creditNotes)
	}

	// A refund that falls back to the wallet on a gateway that refunds is a failure.
	fx, _ = autoChargeFixture(t, "chargebee")
	fx.opts.AssertKnownIssues = true
	inner := fx.fc.payments.onCreditNote
	fx.fc.payments.onCreditNote = func(req types.CreateCreditNoteRequest) {
		inner(req)
		fx.fc.payments.refunds[0].RefundDestination = types.RefundDestinationWallet.ToPointer()
	}
	err := NewPaymentAutoChargeProbe(fx.fc, fx.reg, "run1", nil, fx.opts).Run(context.Background())
	if got := stepOf(err); got != "refund_assert_to_source" {
		t.Fatalf("step = %q, want refund_assert_to_source (err: %v)", got, err)
	}
}

func TestPaymentAutoChargeProbe_SkipsKnownIssueLegs(t *testing.T) {
	fx, _ := autoChargeFixture(t, "chargebee")
	inner := fx.fc.payments.onCreditNote
	fx.fc.payments.onCreditNote = func(req types.CreateCreditNoteRequest) {
		inner(req)
		fx.fc.payments.refunds[0].RefundDestination = types.RefundDestinationWallet.ToPointer()
	}
	if err := NewPaymentAutoChargeProbe(fx.fc, fx.reg, "run1", nil, fx.opts).Run(context.Background()); err != nil {
		t.Fatalf("known-issue legs must not fail the run: %v", err)
	}
	if len(fx.fc.payments.creditNotes) != 0 {
		t.Errorf("refund leg ran despite its known issue: %+v", fx.fc.payments.creditNotes)
	}
}

func TestPaymentAutoChargeProbe_TopUpSupersedesPendingSession(t *testing.T) {
	fx, _ := autoChargeFixture(t, "stripe")
	inner := fx.fc.payments.start
	var policy *types.OnExistingEntityPolicy
	fx.fc.payments.start = func(action string, cfg *types.CheckoutParams) (*types.CheckoutSessionResponse, error) {
		if action == "topup" && cfg.EntityCreationOptions != nil && cfg.EntityCreationOptions.EntityCreationConflictPolicies != nil {
			policy = cfg.EntityCreationOptions.EntityCreationConflictPolicies.OnExistingEntity
		}
		return inner(action, cfg)
	}
	if err := NewPaymentAutoChargeProbe(fx.fc, fx.reg, "run1", nil, fx.opts).Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if policy == nil || *policy != types.OnExistingEntityPolicySupersede {
		t.Errorf("top-up on_existing_entity = %v, want supersede", policy)
	}
}

func TestPaymentAutoChargeProbe_EnsuresInAdvancePlan(t *testing.T) {
	fx, _ := autoChargeFixture(t, "stripe")
	if err := NewPaymentAutoChargeProbe(fx.fc, fx.reg, "run1", nil, fx.opts).Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(fx.fc.plans.created) != 1 || derefStr(fx.fc.plans.created[0].LookupKey) != "e2eprobe_payments_plan_usd" {
		t.Fatalf("plans created = %+v, want one e2eprobe_payments_plan_usd", fx.fc.plans.created)
	}
	if len(fx.fc.prices.created) != 2 {
		t.Fatalf("prices created = %+v, want the plan price and the addon price", fx.fc.prices.created)
	}
	for _, p := range fx.fc.prices.created {
		if p.InvoiceCadence != types.InvoiceCadenceAdvance {
			t.Errorf("price for %s is %s, want ADVANCE", p.EntityID, p.InvoiceCadence)
		}
	}

	// A second run reuses the plan and its price. Its invoice and subscription
	// sessions are the 2nd and 3rd it opens.
	n := fx.fc.payments.nextID
	fx.fc.invoices.getByID[fmt.Sprintf("inv_%d", n+2)] = types.InvoiceResponse{
		ID: strPtr("inv_paid"), PaymentStatus: types.PaymentStatusSucceeded.ToPointer(), AmountPaid: strPtr("15.00"),
		LineItems: []types.InvoiceLineItemResponse{{ID: strPtr("inv_li_2")}},
	}
	fx.fc.invoices.getByID[fmt.Sprintf("inv_%d", n+3)] = fx.fc.invoices.getByID["inv_3"]
	fx.fc.payments.defaults = nil
	if err := NewPaymentAutoChargeProbe(fx.fc, fx.reg, "run2", nil, fx.opts).Run(context.Background()); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if len(fx.fc.plans.created) != 1 || len(fx.fc.prices.created) != 2 || len(fx.fc.payments.addons) != 1 {
		t.Errorf("second run created plans=%d prices=%d addons=%d, want no new ones",
			len(fx.fc.plans.created), len(fx.fc.prices.created), len(fx.fc.payments.addons))
	}
}

func TestPaymentAutoChargeProbe_SkipsWithoutDriver(t *testing.T) {
	fx := newPaymentFixture(t, "razorpay", nil)
	if err := NewPaymentAutoChargeProbe(fx.fc, fx.reg, "run1", nil, fx.opts).Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(fx.fc.customers.created) != 0 {
		t.Errorf("created %d customers without a driver, want 0", len(fx.fc.customers.created))
	}
}

func TestPaymentAutoChargeProbe_LegsRunIndependently(t *testing.T) {
	fx, _ := autoChargeFixture(t, "stripe")
	fx.fc.subs.subs["sub_1"] = types.SubscriptionResponse{ID: strPtr("sub_1"), SubscriptionStatus: types.SubscriptionStatusIncomplete.ToPointer()}
	inner := fx.fc.payments.start
	fx.fc.payments.start = func(action string, cfg *types.CheckoutParams) (*types.CheckoutSessionResponse, error) {
		defaults := fx.fc.payments.defaults
		if len(defaults) > 0 && strings.Contains(defaults[len(defaults)-1], string(TestCardDecline)) {
			return fakePendingSession(), nil
		}
		return inner(action, cfg)
	}

	err := NewPaymentAutoChargeProbe(fx.fc, fx.reg, "run1", nil, fx.opts).Run(context.Background())
	attrs := e2eprobe.AttributesFrom(err)
	if attrs["failed_legs"] != "create_subscription,decline" {
		t.Fatalf("failed_legs = %q, want create_subscription,decline (err: %v)", attrs["failed_legs"], err)
	}
	if !strings.Contains(attrs["skipped_legs"], "modify_subscription") || !strings.Contains(attrs["skipped_legs"], "add_addon") {
		t.Errorf("skipped_legs = %q, want modify_subscription and add_addon", attrs["skipped_legs"])
	}
	if attrs["leg.decline.step"] != "decline_assert_not_completed" {
		t.Errorf("leg.decline.step = %q", attrs["leg.decline.step"])
	}
	if len(fx.fc.payments.creditNotes) != 1 {
		t.Errorf("refund leg did not run despite an unrelated failure")
	}
}

func TestPaymentAutoChargeProbe_FixedMandateCustomer(t *testing.T) {
	setup := func(t *testing.T) *paymentFixture {
		fx, _ := autoChargeFixture(t, "razorpay")
		fx.opts.Driver = nil
		fx.opts.Provider.FixedCustomerExternalID = "e2eprobe-cust-pay-razorpay-mandate"
		return fx
	}

	t.Run("charges the authorized mandate", func(t *testing.T) {
		fx := setup(t)
		fx.fc.payments.savedMethods = []e2eprobe.SavedPaymentMethod{{ID: "token_1", Status: "active", CanAutoCharge: true}}
		if err := NewPaymentAutoChargeProbe(fx.fc, fx.reg, "run1", nil, fx.opts).Run(context.Background()); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if len(fx.fc.customers.created) != 1 || fx.fc.customers.created[0].ExternalID != "e2eprobe-cust-pay-razorpay-mandate" {
			t.Errorf("customers created = %+v, want only the fixed customer", fx.fc.customers.created)
		}
		if len(fx.reg.Ephemerals("customer")) != 0 {
			t.Errorf("fixed customer must not be registered for janitor cleanup")
		}
		if len(fx.fc.subs.cancelled) != 1 {
			t.Errorf("subscriptions cancelled = %v, want the run's subscription cancelled", fx.fc.subs.cancelled)
		}
	})

	t.Run("stale pending top-up fails fast", func(t *testing.T) {
		fx := setup(t)
		fx.fc.payments.savedMethods = []e2eprobe.SavedPaymentMethod{{ID: "token_1", Status: "active", CanAutoCharge: true}}
		inner := fx.fc.payments.start
		fx.fc.payments.start = func(action string, cfg *types.CheckoutParams) (*types.CheckoutSessionResponse, error) {
			if action != "topup" {
				return inner(action, cfg)
			}
			return &types.CheckoutSessionResponse{
				ID:                strPtr("cs_stale"),
				CheckoutPaymentID: strPtr("pay_stale"),
				CheckoutStatus:    types.CheckoutStatusPending.ToPointer(),
				EntityCreationResult: &types.EntityCreationResult{
					EntityID: strPtr("cs_stale"),
					Status:   types.EntityCreationStatusFailedAlreadyExists.ToPointer(),
				},
			}, nil
		}
		err := NewPaymentAutoChargeProbe(fx.fc, fx.reg, "run1", nil, fx.opts).Run(context.Background())
		attrs := e2eprobe.AttributesFrom(err)
		if attrs["leg.wallet_topup.step"] != "topup_autocharge_start" {
			t.Fatalf("leg.wallet_topup.step = %q, want topup_autocharge_start (err: %v)", attrs["leg.wallet_topup.step"], err)
		}
		if !strings.Contains(err.Error(), "cs_stale") {
			t.Errorf("error should name the blocking session: %v", err)
		}
	})

	t.Run("missing mandate fails clearly", func(t *testing.T) {
		fx := setup(t)
		err := NewPaymentAutoChargeProbe(fx.fc, fx.reg, "run1", nil, fx.opts).Run(context.Background())
		if got := stepOf(err); got != "fixed_customer_mandate" {
			t.Fatalf("step = %q, want fixed_customer_mandate (err: %v)", got, err)
		}
	})
}

// savedCard is a hand-saved card on the fixed customer's listing.
func savedCard(id, last4 string, isDefault bool) e2eprobe.SavedPaymentMethod {
	m := e2eprobe.SavedPaymentMethod{ID: id, Status: "active", CanAutoCharge: true, IsDefault: isDefault}
	m.Card = &struct {
		Last4 string `json:"last4"`
	}{Last4: last4}
	return m
}

func TestPaymentAutoChargeProbe_FixedSavedCards(t *testing.T) {
	setup := func(t *testing.T, cards ...e2eprobe.SavedPaymentMethod) *paymentFixture {
		fx, _ := autoChargeFixture(t, "stripe")
		fx.opts.Driver = nil
		fx.opts.Provider.FixedCustomerExternalID = "e2eprobe-cust-pay-stripe-cards"
		fx.opts.Provider.DeclineCardLast4 = "0341"
		fx.fc.payments.savedMethods = cards
		return fx
	}

	t.Run("switches default between good cards and charges the declining card", func(t *testing.T) {
		fx := setup(t, savedCard("pm_good_a", "4242", true), savedCard("pm_good_b", "4444", false), savedCard("pm_decline_c", "0341", false))
		if err := NewPaymentAutoChargeProbe(fx.fc, fx.reg, "run1", nil, fx.opts).Run(context.Background()); err != nil {
			t.Fatalf("Run: %v", err)
		}
		want := []string{"pm_good_b", "pm_decline_c", "pm_good_b"}
		if got := fx.fc.payments.defaults; strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("default changes = %v, want %v (switch, decline, restore)", got, want)
		}
	})

	t.Run("next run switches back", func(t *testing.T) {
		fx := setup(t, savedCard("pm_good_a", "4242", false), savedCard("pm_good_b", "4444", true), savedCard("pm_decline_c", "0341", false))
		if err := NewPaymentAutoChargeProbe(fx.fc, fx.reg, "run1", nil, fx.opts).Run(context.Background()); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if got := fx.fc.payments.defaults; len(got) == 0 || got[0] != "pm_good_a" {
			t.Errorf("default changes = %v, want the first switch to pm_good_a", got)
		}
	})

	t.Run("declining card left as default is replaced before charging", func(t *testing.T) {
		fx := setup(t, savedCard("pm_good_a", "4242", false), savedCard("pm_good_b", "4444", false), savedCard("pm_decline_c", "0341", true))
		if err := NewPaymentAutoChargeProbe(fx.fc, fx.reg, "run1", nil, fx.opts).Run(context.Background()); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if got := fx.fc.payments.defaults; len(got) == 0 || got[0] != "pm_good_a" {
			t.Errorf("default changes = %v, want a good card restored first", got)
		}
	})

	t.Run("one good card skips set_default", func(t *testing.T) {
		fx := setup(t, savedCard("pm_good_a", "4242", true), savedCard("pm_decline_c", "0341", false))
		if err := NewPaymentAutoChargeProbe(fx.fc, fx.reg, "run1", nil, fx.opts).Run(context.Background()); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if got := fx.fc.payments.defaults; strings.Join(got, ",") != "pm_decline_c,pm_good_a" {
			t.Errorf("default changes = %v, want only the decline leg's switch and restore", got)
		}
	})

	t.Run("no declining card configured skips the decline leg", func(t *testing.T) {
		fx := setup(t, savedCard("pm_good_a", "4242", true), savedCard("pm_good_b", "4444", false))
		fx.opts.Provider.DeclineCardLast4 = ""
		if err := NewPaymentAutoChargeProbe(fx.fc, fx.reg, "run1", nil, fx.opts).Run(context.Background()); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if got := fx.fc.payments.defaults; strings.Join(got, ",") != "pm_good_b" {
			t.Errorf("default changes = %v, want only the set_default switch", got)
		}
	})

	t.Run("only the declining card saved", func(t *testing.T) {
		fx := setup(t, savedCard("pm_decline_c", "0341", true))
		err := NewPaymentAutoChargeProbe(fx.fc, fx.reg, "run1", nil, fx.opts).Run(context.Background())
		if got := stepOf(err); got != "fixed_customer_mandate" {
			t.Fatalf("step = %q, want fixed_customer_mandate (err: %v)", got, err)
		}
	})

	t.Run("set-default not reflected in the listing", func(t *testing.T) {
		fx := setup(t, savedCard("pm_good_a", "4242", true), savedCard("pm_good_b", "4444", false), savedCard("pm_decline_c", "0341", false))
		fx.fc.payments.ignoreSetDefault = true
		err := NewPaymentAutoChargeProbe(fx.fc, fx.reg, "run1", nil, fx.opts).Run(context.Background())
		if got := e2eprobe.AttributesFrom(err)["leg.set_default.step"]; got != "set_default_assert_response" {
			t.Fatalf("set_default step = %q, want set_default_assert_response (err: %v)", got, err)
		}
	})
}
