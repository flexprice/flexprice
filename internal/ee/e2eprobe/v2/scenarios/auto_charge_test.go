package scenarios

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/probetest"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/suite"
	"github.com/flexprice/go-sdk/v2/models/types"
)

// autoChargeFixture models a gateway that settles every auto-charge: top-ups
// credit the wallet, the invoice is paid, the subscription is active, a paid
// quantity change adds a quantity-2 line item, a paid addon turns active and a
// refund settles (to the card on refunding gateways, else to the wallet). A
// top-up while the declining card is default is refused with 402.
func autoChargeFixture(t *testing.T, provider string, spec func(*suite.Payment)) *paymentFixture {
	t.Helper()
	fx := newPaymentFixture(t, provider, func(p *suite.Payment) {
		p.FixedCustomer = "e2eprobe-cust-pay-" + provider + "-cards"
		p.DeclineLast4 = "0341"
		if spec != nil {
			spec(p)
		}
	})
	pay := fx.fc.Payment
	pay.SavedMethods = []e2eprobe.SavedPaymentMethod{
		savedCard("pm_good_a", "4242", true), savedCard("pm_good_b", "4444", false), savedCard("pm_decline_c", "0341", false),
	}
	pay.Start = func(action string, cfg *types.CheckoutParams) (*types.CheckoutSessionResponse, error) {
		if action == "topup" && collectionOf(cfg) == types.CollectionMethodChargeAutomatically {
			if fx.declineIsDefault() {
				return nil, rejection(402)
			}
			fx.addCredits(25)
		}
		return probetest.PendingSession(), nil
	}
	fx.fc.Invoice.ByID["inv_2"] = types.InvoiceResponse{
		ID: probetest.Ptr("inv_2"), PaymentStatus: types.PaymentStatusSucceeded.ToPointer(), AmountPaid: probetest.Ptr("15.00"),
		LineItems: []types.InvoiceLineItemResponse{{ID: probetest.Ptr("inv_li_1")}},
	}
	fx.fc.Invoice.ByID["inv_3"] = types.InvoiceResponse{ID: probetest.Ptr("inv_3"), SubscriptionID: probetest.Ptr("sub_pay")}
	seedActiveSubscription(fx, "sub_pay")
	pay.OnModify = func(subID string, _ types.ExecuteSubscriptionModifyRequest) {
		sub := fx.fc.Subscription.Items[subID]
		ended := paymentsPlanItem()
		endedAt := testNow.Add(-time.Minute)
		ended.EndDate = &endedAt
		replaced := paymentsPlanItem()
		replaced.ID, replaced.Quantity = probetest.Ptr("li_2"), probetest.Ptr("2")
		sub.LineItems = []types.SubscriptionSubscriptionLineItem{ended, replaced}
		fx.fc.Subscription.Items[subID] = sub
	}
	pay.AddonAssociations = []types.AddonAssociationResponse{
		{ID: probetest.Ptr("assoc_1"), AddonStatus: types.AddonStatusActive.ToPointer()},
	}
	pay.OnCreditNote = func(req types.CreateCreditNoteRequest) {
		inv := fx.fc.Invoice.ByID[req.InvoiceID]
		inv.PaymentStatus = types.PaymentStatusRefunded.ToPointer()
		fx.fc.Invoice.ByID[req.InvoiceID] = inv

		refund := types.RefundResponse{
			Amount: probetest.Ptr("15.00"), RefundStatus: types.RefundStatusSucceeded.ToPointer(),
			RefundDestination: types.RefundDestinationWallet.ToPointer(),
		}
		if gatewaysRefundingToSource[provider] {
			refund.RefundDestination = types.RefundDestinationGateway.ToPointer()
			refund.GatewayRefundID = probetest.Ptr("rf_1")
		} else {
			fx.addCredits(15)
		}
		pay.Refunds = []types.RefundResponse{refund}
	}
	return fx
}

// paymentsPlanItem is the open in-advance plan line item of a fixture subscription.
func paymentsPlanItem() types.SubscriptionSubscriptionLineItem {
	return types.SubscriptionSubscriptionLineItem{
		EndDate: &time.Time{}, // the API reports an open line item with a zero end_date
		ID:      probetest.Ptr("li_1"), PriceID: probetest.Ptr("price_pay"), Quantity: probetest.Ptr("1"),
		PriceType: types.PriceTypeFixed.ToPointer(), InvoiceCadence: types.InvoiceCadenceAdvance.ToPointer(),
	}
}

// seedActiveSubscription stores id as the active subscription a checkout invoice names.
func seedActiveSubscription(fx *paymentFixture, id string) {
	fx.fc.Subscription.Items[id] = types.SubscriptionResponse{
		ID: probetest.Ptr(id), SubscriptionStatus: types.SubscriptionStatusActive.ToPointer(),
		LineItems: []types.SubscriptionSubscriptionLineItem{paymentsPlanItem()},
	}
}

// declineIsDefault reports whether the last set-default call chose the declining card.
func (fx *paymentFixture) declineIsDefault() bool {
	defaults := fx.fc.Payment.Defaults
	return len(defaults) > 0 && strings.Contains(defaults[len(defaults)-1], "decline")
}

// savedCard is a hand-saved card on the fixed customer's listing.
func savedCard(id, last4 string, isDefault bool) e2eprobe.SavedPaymentMethod {
	m := e2eprobe.SavedPaymentMethod{ID: id, Status: "active", CanAutoCharge: true, IsDefault: isDefault}
	m.Card = &struct {
		Last4 string `json:"last4"`
	}{Last4: last4}
	return m
}

// paymentsPlanID is the per-currency plan the auto-charge scenario subscribes to.
func paymentsPlanID(fx *paymentFixture) string {
	for _, p := range fx.fc.Plan.Items {
		if p.LookupKey != nil && *p.LookupKey == "e2eprobe_payments_plan_usd" && p.ID != nil {
			return *p.ID
		}
	}
	return ""
}

// paymentsPrices are the prices created on the payments plan and addon.
func paymentsPrices(fx *paymentFixture) []types.CreatePriceRequest {
	owners := map[string]bool{paymentsPlanID(fx): true, fx.fc.Payment.Addons["e2eprobe_payments_addon_usd"]: true}
	var out []types.CreatePriceRequest
	for _, p := range fx.fc.Price.Created {
		if owners[p.EntityID] {
			out = append(out, p)
		}
	}
	return out
}

func TestAutoCharge(t *testing.T) {
	tests := []struct {
		name     string
		spec     func(*suite.Payment)
		setup    func(fx *paymentFixture)
		wantStep string
	}{
		{name: "happy path charges top-up, invoice and subscription, refuses decline"},
		{
			name: "no declining card configured skips the decline leg",
			spec: func(p *suite.Payment) { p.DeclineLast4 = "" },
		},
		{
			name:     "webhook never settles the payment",
			setup:    func(fx *paymentFixture) { fx.fc.Payment.PaymentStatus = types.PaymentStatusPending },
			wantStep: "topup_autocharge_webhook",
		},
		{
			name:     "payment settles as failed",
			setup:    func(fx *paymentFixture) { fx.fc.Payment.PaymentStatus = types.PaymentStatusFailed },
			wantStep: "topup_autocharge_webhook",
		},
		{
			name: "session never completes",
			setup: func(fx *paymentFixture) {
				fx.fc.Payment.SessionOnGet = func(s *types.CheckoutSessionResponse) *types.CheckoutSessionResponse {
					out := *s
					out.CheckoutStatus = types.CheckoutStatusFailed.ToPointer()
					out.Terminal = probetest.Ptr(true)
					return &out
				}
			},
			wantStep: "topup_autocharge_session",
		},
		{
			name: "top-up credits twice",
			setup: func(fx *paymentFixture) {
				inner := fx.fc.Payment.Start
				fx.fc.Payment.Start = func(action string, cfg *types.CheckoutParams) (*types.CheckoutSessionResponse, error) {
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
			setup: func(fx *paymentFixture) {
				fx.fc.Invoice.ByID["inv_2"] = types.InvoiceResponse{ID: probetest.Ptr("inv_2"), PaymentStatus: types.PaymentStatusPending.ToPointer()}
			},
			wantStep: "invoice_autocharge_assert_paid",
		},
		{
			name: "invoice partially paid",
			setup: func(fx *paymentFixture) {
				fx.fc.Invoice.ByID["inv_2"] = types.InvoiceResponse{ID: probetest.Ptr("inv_2"), PaymentStatus: types.PaymentStatusSucceeded.ToPointer(), AmountPaid: probetest.Ptr("5.00")}
			},
			wantStep: "invoice_autocharge_assert_amount",
		},
		{
			name: "subscription not active",
			setup: func(fx *paymentFixture) {
				fx.fc.Subscription.Items["sub_pay"] = types.SubscriptionResponse{ID: probetest.Ptr("sub_pay"), SubscriptionStatus: types.SubscriptionStatusIncomplete.ToPointer()}
			},
			wantStep: "subscription_autocharge_assert_active",
		},
		{
			name:     "quantity change applied without checkout",
			setup:    func(fx *paymentFixture) { fx.fc.Payment.Ungated = map[string]bool{"modify": true} },
			wantStep: "modify_autocharge_assert_gated",
		},
		{
			name:     "paid quantity change not applied",
			setup:    func(fx *paymentFixture) { fx.fc.Payment.OnModify = nil },
			wantStep: "modify_autocharge_assert_quantity",
		},
		{
			name:     "addon attached without checkout",
			setup:    func(fx *paymentFixture) { fx.fc.Payment.Ungated = map[string]bool{"addon": true} },
			wantStep: "addon_autocharge_assert_gated",
		},
		{
			name:     "paid addon never activates",
			setup:    func(fx *paymentFixture) { fx.fc.Payment.AddonAssociations = nil },
			wantStep: "addon_autocharge_assert_active",
		},
		{
			name:     "refund leaves invoice unrefunded",
			setup:    func(fx *paymentFixture) { fx.fc.Payment.OnCreditNote = func(types.CreateCreditNoteRequest) {} },
			wantStep: "refund_assert_invoice_refunded",
		},
		{
			name: "refund stuck processing",
			setup: func(fx *paymentFixture) {
				inner := fx.fc.Payment.OnCreditNote
				fx.fc.Payment.OnCreditNote = func(req types.CreateCreditNoteRequest) {
					inner(req)
					fx.fc.Payment.Refunds[0].RefundStatus = types.RefundStatusProcessing.ToPointer()
				}
			},
			wantStep: "refund_assert_settles",
		},
		{
			name: "wallet refund credits nothing",
			setup: func(fx *paymentFixture) {
				inner := fx.fc.Payment.OnCreditNote
				fx.fc.Payment.OnCreditNote = func(req types.CreateCreditNoteRequest) {
					inner(req)
					fx.addCredits(-15)
				}
			},
			wantStep: "refund_assert_wallet_credit",
		},
		{
			name: "declining card still completes",
			setup: func(fx *paymentFixture) {
				inner := fx.fc.Payment.Start
				fx.fc.Payment.Start = func(action string, cfg *types.CheckoutParams) (*types.CheckoutSessionResponse, error) {
					if fx.declineIsDefault() {
						return probetest.PendingSession(), nil
					}
					return inner(action, cfg)
				}
			},
			wantStep: "decline_assert_not_completed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := autoChargeFixture(t, "stripe", tt.spec)
			if tt.setup != nil {
				tt.setup(fx)
			}
			r := fx.run(t, fx.autoCharge())
			if tt.wantStep != "" {
				if r.Status != e2eprobe.StatusFail || r.Step != "auto-charge" {
					t.Fatalf("want failure at step %q, got %+v", tt.wantStep, r)
				}
				if got := stepOf(r); got != tt.wantStep {
					t.Fatalf("step = %q, want %q (result: %+v)", got, tt.wantStep, r)
				}
				return
			}
			if r.Status != e2eprobe.StatusPass {
				t.Fatalf("result = %+v", r)
			}
			if cancelled := fx.fc.Subscription.Cancelled; len(cancelled) != 1 || cancelled[0] != "sub_pay" {
				t.Errorf("cancelled subscriptions = %v, want [sub_pay]", cancelled)
			}
		})
	}
}

func TestAutoCharge_ChargebeeRefundsToCard(t *testing.T) {
	assertKnown := func(p *suite.Payment) { p.AssertKnownIssues = true }
	fx := autoChargeFixture(t, "chargebee", assertKnown)
	if r := fx.run(t, fx.autoCharge()); r.Status != e2eprobe.StatusPass {
		t.Fatalf("result = %+v", r)
	}
	if len(fx.fc.Payment.CreditNotes) != 1 || *fx.fc.Payment.CreditNotes[0].RefundTarget != types.RefundTargetBackToSource {
		t.Fatalf("credit notes = %+v, want one back-to-source refund", fx.fc.Payment.CreditNotes)
	}

	// A refund that falls back to the wallet on a gateway that refunds is a failure.
	fx = autoChargeFixture(t, "chargebee", assertKnown)
	inner := fx.fc.Payment.OnCreditNote
	fx.fc.Payment.OnCreditNote = func(req types.CreateCreditNoteRequest) {
		inner(req)
		fx.fc.Payment.Refunds[0].RefundDestination = types.RefundDestinationWallet.ToPointer()
	}
	r := fx.run(t, fx.autoCharge())
	if got := stepOf(r); got != "refund_assert_to_source" {
		t.Fatalf("step = %q, want refund_assert_to_source (result: %+v)", got, r)
	}
}

func TestAutoCharge_SkipsKnownIssueLegs(t *testing.T) {
	fx := autoChargeFixture(t, "chargebee", nil)
	inner := fx.fc.Payment.OnCreditNote
	fx.fc.Payment.OnCreditNote = func(req types.CreateCreditNoteRequest) {
		inner(req)
		fx.fc.Payment.Refunds[0].RefundDestination = types.RefundDestinationWallet.ToPointer()
	}
	if r := fx.run(t, fx.autoCharge()); r.Status != e2eprobe.StatusPass {
		t.Fatalf("known-issue legs must not fail the run: %+v", r)
	}
	if len(fx.fc.Payment.CreditNotes) != 0 {
		t.Errorf("refund leg ran despite its known issue: %+v", fx.fc.Payment.CreditNotes)
	}
}

func TestAutoCharge_TopUpSupersedesPendingSession(t *testing.T) {
	fx := autoChargeFixture(t, "stripe", nil)
	inner := fx.fc.Payment.Start
	var policy *types.OnExistingEntityPolicy
	fx.fc.Payment.Start = func(action string, cfg *types.CheckoutParams) (*types.CheckoutSessionResponse, error) {
		if action == "topup" && cfg.EntityCreationOptions != nil && cfg.EntityCreationOptions.EntityCreationConflictPolicies != nil {
			policy = cfg.EntityCreationOptions.EntityCreationConflictPolicies.OnExistingEntity
		}
		return inner(action, cfg)
	}
	if r := fx.run(t, fx.autoCharge()); r.Status != e2eprobe.StatusPass {
		t.Fatalf("result = %+v", r)
	}
	if policy == nil || *policy != types.OnExistingEntityPolicySupersede {
		t.Errorf("top-up on_existing_entity = %v, want supersede", policy)
	}
}

func TestAutoCharge_EnsuresInAdvancePlan(t *testing.T) {
	fx := autoChargeFixture(t, "stripe", nil)
	if r := fx.run(t, fx.autoCharge()); r.Status != e2eprobe.StatusPass {
		t.Fatalf("result = %+v", r)
	}
	if paymentsPlanID(fx) == "" {
		t.Fatalf("plans = %+v, want one e2eprobe_payments_plan_usd", fx.fc.Plan.Items)
	}
	prices := paymentsPrices(fx)
	if len(prices) != 2 {
		t.Fatalf("prices created = %+v, want the plan price and the addon price", prices)
	}
	for _, p := range prices {
		if p.InvoiceCadence != types.InvoiceCadenceAdvance {
			t.Errorf("price for %s is %s, want ADVANCE", p.EntityID, p.InvoiceCadence)
		}
	}

	// A second run reuses the plan and its price. Its invoice and subscription
	// sessions are the 2nd and 3rd it opens, and the server would hand it a new
	// subscription since the first run cancelled its own.
	plans, n := len(fx.fc.Plan.Created), len(fx.fc.Payment.Sessions)
	fx.fc.Invoice.ByID[fmt.Sprintf("inv_%d", n+2)] = types.InvoiceResponse{
		ID: probetest.Ptr("inv_paid"), PaymentStatus: types.PaymentStatusSucceeded.ToPointer(), AmountPaid: probetest.Ptr("15.00"),
		LineItems: []types.InvoiceLineItemResponse{{ID: probetest.Ptr("inv_li_2")}},
	}
	fx.fc.Invoice.ByID[fmt.Sprintf("inv_%d", n+3)] = types.InvoiceResponse{ID: probetest.Ptr("inv_sub_2"), SubscriptionID: probetest.Ptr("sub_pay_2")}
	seedActiveSubscription(fx, "sub_pay_2")
	fx.fc.Payment.Defaults = nil
	if r := fx.run(t, fx.autoCharge()); r.Status != e2eprobe.StatusPass {
		t.Fatalf("second run = %+v", r)
	}
	if len(fx.fc.Plan.Created) != plans || len(paymentsPrices(fx)) != 2 || len(fx.fc.Payment.Addons) != 1 {
		t.Errorf("second run created plans=%d prices=%d addons=%d, want no new ones",
			len(fx.fc.Plan.Created)-plans, len(paymentsPrices(fx)), len(fx.fc.Payment.Addons))
	}
}

func TestAutoCharge_SkipsWithoutFixedCustomer(t *testing.T) {
	t.Run("persona names no fixed customer", func(t *testing.T) {
		fx := newPaymentFixture(t, "razorpay", nil)
		created := len(fx.fc.Customer.Created)
		r := fx.run(t, fx.autoCharge())
		if r.Status != e2eprobe.StatusSkip || r.Step != "fixed_customer" {
			t.Fatalf("result = %+v", r)
		}
		if len(fx.fc.Customer.Created) != created {
			t.Errorf("created customers without a fixed customer")
		}
	})
	t.Run("fixed customer missing on the tenant", func(t *testing.T) {
		d := newTestDeps(t)
		d.fc.TenantOps.Connections = []e2eprobe.Connection{{ID: "c1", ProviderType: "stripe"}}
		r := run(t, d, newAutoCharge(d.Deps), "payer-stripe")
		if r.Status != e2eprobe.StatusSkip || r.Step != "fixed_customer" || !strings.Contains(r.Reason, "e2eprobe-cust-pay-stripe-cards") {
			t.Fatalf("result = %+v", r)
		}
	})
	t.Run("no connection", func(t *testing.T) {
		fx := autoChargeFixture(t, "stripe", nil)
		fx.fc.TenantOps.Connections = nil
		if r := fx.run(t, fx.autoCharge()); r.Status != e2eprobe.StatusSkip || r.Step != "connection" {
			t.Fatalf("result = %+v", r)
		}
	})
}

func TestAutoCharge_LegsRunIndependently(t *testing.T) {
	fx := autoChargeFixture(t, "stripe", nil)
	fx.fc.Subscription.Items["sub_pay"] = types.SubscriptionResponse{ID: probetest.Ptr("sub_pay"), SubscriptionStatus: types.SubscriptionStatusIncomplete.ToPointer()}
	inner := fx.fc.Payment.Start
	fx.fc.Payment.Start = func(action string, cfg *types.CheckoutParams) (*types.CheckoutSessionResponse, error) {
		if fx.declineIsDefault() {
			return probetest.PendingSession(), nil
		}
		return inner(action, cfg)
	}

	r := fx.run(t, fx.autoCharge())
	if r.Attrs["failed_legs"] != "create_subscription,decline" {
		t.Fatalf("failed_legs = %q, want create_subscription,decline (result: %+v)", r.Attrs["failed_legs"], r)
	}
	if r.Attrs["leg.decline.step"] != "decline_assert_not_completed" || len(r.Mismatches) != 2 {
		t.Errorf("result = %+v", r)
	}
	if len(fx.fc.Payment.CreditNotes) != 1 {
		t.Errorf("refund leg did not run despite an unrelated failure")
	}
}

func TestAutoCharge_FixedMandateCustomer(t *testing.T) {
	setup := func(t *testing.T) *paymentFixture {
		return autoChargeFixture(t, "razorpay", func(p *suite.Payment) { p.FixedCustomer = "e2eprobe-cust-pay-razorpay-mandate" })
	}

	t.Run("charges the authorized mandate", func(t *testing.T) {
		fx := setup(t)
		fx.fc.Payment.SavedMethods = []e2eprobe.SavedPaymentMethod{{ID: "token_1", Status: "active", CanAutoCharge: true}}
		created := len(fx.fc.Customer.Created)
		if r := fx.run(t, fx.autoCharge()); r.Status != e2eprobe.StatusPass {
			t.Fatalf("result = %+v", r)
		}
		if len(fx.fc.Customer.Created) != created {
			t.Errorf("customers created = %+v, want none beyond the fixed customer", fx.fc.Customer.Created[created:])
		}
		if len(fx.World.Ephemerals().List("customer")) != 0 {
			t.Errorf("fixed customer must not be registered for janitor cleanup")
		}
		if len(fx.fc.Subscription.Cancelled) != 1 {
			t.Errorf("subscriptions cancelled = %v, want the run's subscription cancelled", fx.fc.Subscription.Cancelled)
		}
		if len(fx.fc.Payment.Defaults) != 0 {
			t.Errorf("a gateway without set-default must not switch cards: %v", fx.fc.Payment.Defaults)
		}
	})

	t.Run("stale pending top-up fails fast", func(t *testing.T) {
		fx := setup(t)
		fx.fc.Payment.SavedMethods = []e2eprobe.SavedPaymentMethod{{ID: "token_1", Status: "active", CanAutoCharge: true}}
		inner := fx.fc.Payment.Start
		fx.fc.Payment.Start = func(action string, cfg *types.CheckoutParams) (*types.CheckoutSessionResponse, error) {
			if action != "topup" {
				return inner(action, cfg)
			}
			return &types.CheckoutSessionResponse{
				ID:                probetest.Ptr("cs_stale"),
				CheckoutPaymentID: probetest.Ptr("pay_stale"),
				CheckoutStatus:    types.CheckoutStatusPending.ToPointer(),
				EntityCreationResult: &types.EntityCreationResult{
					EntityID: probetest.Ptr("cs_stale"),
					Status:   types.EntityCreationStatusFailedAlreadyExists.ToPointer(),
				},
			}, nil
		}
		r := fx.run(t, fx.autoCharge())
		if r.Attrs["leg.wallet_topup.step"] != "topup_autocharge_start" {
			t.Fatalf("leg.wallet_topup.step = %q, want topup_autocharge_start (result: %+v)", r.Attrs["leg.wallet_topup.step"], r)
		}
		if !strings.Contains(strings.Join(r.Mismatches, "\n"), "cs_stale") {
			t.Errorf("the mismatch should name the blocking session: %v", r.Mismatches)
		}
	})

	t.Run("missing mandate fails clearly", func(t *testing.T) {
		fx := setup(t)
		fx.fc.Payment.SavedMethods = nil
		r := fx.run(t, fx.autoCharge())
		if r.Status != e2eprobe.StatusFail || r.Step != "fixed_customer_mandate" || r.Infra {
			t.Fatalf("result = %+v", r)
		}
	})
}

func TestAutoCharge_FixedSavedCards(t *testing.T) {
	setup := func(t *testing.T, spec func(*suite.Payment), cards ...e2eprobe.SavedPaymentMethod) *paymentFixture {
		fx := autoChargeFixture(t, "stripe", spec)
		fx.fc.Payment.SavedMethods = cards
		return fx
	}

	t.Run("switches default between good cards and charges the declining card", func(t *testing.T) {
		fx := setup(t, nil, savedCard("pm_good_a", "4242", true), savedCard("pm_good_b", "4444", false), savedCard("pm_decline_c", "0341", false))
		if r := fx.run(t, fx.autoCharge()); r.Status != e2eprobe.StatusPass {
			t.Fatalf("result = %+v", r)
		}
		want := []string{"pm_good_b", "pm_decline_c", "pm_good_b"}
		if got := fx.fc.Payment.Defaults; strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("default changes = %v, want %v (switch, decline, restore)", got, want)
		}
	})

	t.Run("next run switches back", func(t *testing.T) {
		fx := setup(t, nil, savedCard("pm_good_a", "4242", false), savedCard("pm_good_b", "4444", true), savedCard("pm_decline_c", "0341", false))
		if r := fx.run(t, fx.autoCharge()); r.Status != e2eprobe.StatusPass {
			t.Fatalf("result = %+v", r)
		}
		if got := fx.fc.Payment.Defaults; len(got) == 0 || got[0] != "pm_good_a" {
			t.Errorf("default changes = %v, want the first switch to pm_good_a", got)
		}
	})

	t.Run("declining card left as default is replaced before charging", func(t *testing.T) {
		fx := setup(t, nil, savedCard("pm_good_a", "4242", false), savedCard("pm_good_b", "4444", false), savedCard("pm_decline_c", "0341", true))
		if r := fx.run(t, fx.autoCharge()); r.Status != e2eprobe.StatusPass {
			t.Fatalf("result = %+v", r)
		}
		if got := fx.fc.Payment.Defaults; len(got) == 0 || got[0] != "pm_good_a" {
			t.Errorf("default changes = %v, want a good card restored first", got)
		}
	})

	t.Run("one good card skips set_default", func(t *testing.T) {
		fx := setup(t, nil, savedCard("pm_good_a", "4242", true), savedCard("pm_decline_c", "0341", false))
		if r := fx.run(t, fx.autoCharge()); r.Status != e2eprobe.StatusPass {
			t.Fatalf("result = %+v", r)
		}
		if got := fx.fc.Payment.Defaults; strings.Join(got, ",") != "pm_decline_c,pm_good_a" {
			t.Errorf("default changes = %v, want only the decline leg's switch and restore", got)
		}
	})

	t.Run("no declining card configured skips the decline leg", func(t *testing.T) {
		noDecline := func(p *suite.Payment) { p.DeclineLast4 = "" }
		fx := setup(t, noDecline, savedCard("pm_good_a", "4242", true), savedCard("pm_good_b", "4444", false))
		if r := fx.run(t, fx.autoCharge()); r.Status != e2eprobe.StatusPass {
			t.Fatalf("result = %+v", r)
		}
		if got := fx.fc.Payment.Defaults; strings.Join(got, ",") != "pm_good_b" {
			t.Errorf("default changes = %v, want only the set_default switch", got)
		}
	})

	t.Run("only the declining card saved", func(t *testing.T) {
		fx := setup(t, nil, savedCard("pm_decline_c", "0341", true))
		r := fx.run(t, fx.autoCharge())
		if r.Status != e2eprobe.StatusFail || r.Step != "fixed_customer_mandate" {
			t.Fatalf("result = %+v", r)
		}
	})

	t.Run("set-default not reflected in the listing", func(t *testing.T) {
		fx := setup(t, nil, savedCard("pm_good_a", "4242", true), savedCard("pm_good_b", "4444", false), savedCard("pm_decline_c", "0341", false))
		fx.fc.Payment.IgnoreSetDefault = true
		r := fx.run(t, fx.autoCharge())
		if got := r.Attrs["leg.set_default.step"]; got != "set_default_assert_response" {
			t.Fatalf("set_default step = %q, want set_default_assert_response (result: %+v)", got, r)
		}
	})
}
