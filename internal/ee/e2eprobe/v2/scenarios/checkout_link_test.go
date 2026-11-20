package scenarios

import (
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/probetest"
	"github.com/flexprice/go-sdk/v2/models/types"
	"github.com/shopspring/decimal"
)

// rejectUnattended answers a customer-not-present start with status, and every
// other start with a pending session.
func rejectUnattended(status int) func(string, *types.CheckoutParams) (*types.CheckoutSessionResponse, error) {
	return func(_ string, cfg *types.CheckoutParams) (*types.CheckoutSessionResponse, error) {
		if customerNotPresent(cfg) {
			return nil, rejection(status)
		}
		return probetest.PendingSession(), nil
	}
}

func TestCheckoutLink(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		setup    func(fx *paymentFixture)
		wantStep string
		// wantCancels is how many sessions the scenario cancels on success.
		wantCancels int
	}{
		{
			name:        "happy path covers invoice, top-up and auto-charge fallback links",
			provider:    "stripe",
			setup:       func(fx *paymentFixture) { fx.fc.Payment.Start = rejectUnattended(400) },
			wantCancels: 3,
		},
		{
			name:     "unattended auto-charge accepted",
			provider: "stripe",
			wantStep: "unattended_assert_rejected",
		},
		{
			name:     "unattended auto-charge fails with 5xx",
			provider: "stripe",
			setup:    func(fx *paymentFixture) { fx.fc.Payment.Start = rejectUnattended(500) },
			wantStep: "unattended_assert_rejected",
		},
		{
			name:     "pending session without URL",
			provider: "stripe",
			setup: func(fx *paymentFixture) {
				fx.fc.Payment.Start = func(string, *types.CheckoutParams) (*types.CheckoutSessionResponse, error) {
					s := probetest.PendingSession()
					s.PaymentAction = nil
					return s, nil
				}
			},
			wantStep: "invoice_link_assert_url",
		},
		{
			name:     "session not pending",
			provider: "stripe",
			setup: func(fx *paymentFixture) {
				fx.fc.Payment.Start = func(string, *types.CheckoutParams) (*types.CheckoutSessionResponse, error) {
					s := probetest.PendingSession()
					s.CheckoutStatus = types.CheckoutStatusFailed.ToPointer()
					return s, nil
				}
			},
			wantStep: "invoice_link_assert_pending",
		},
		{
			name:     "cancel does not expire session",
			provider: "stripe",
			setup:    func(fx *paymentFixture) { fx.fc.Payment.CancelStatus = types.CheckoutStatusCompleted },
			wantStep: "invoice_link_assert_expired",
		},
		{
			name:     "link top-up credits wallet before payment",
			provider: "razorpay",
			setup: func(fx *paymentFixture) {
				fx.fc.Payment.Start = func(action string, _ *types.CheckoutParams) (*types.CheckoutSessionResponse, error) {
					if action == "topup" {
						fx.addCredits(25)
					}
					return probetest.PendingSession(), nil
				}
			},
			wantStep: "topup_link_assert_no_credit",
		},
		{
			name:     "start rejected",
			provider: "chargebee",
			setup: func(fx *paymentFixture) {
				fx.fc.Payment.Start = func(string, *types.CheckoutParams) (*types.CheckoutSessionResponse, error) {
					return nil, rejection(500)
				}
			},
			wantStep: "invoice_link_start",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newPaymentFixture(t, tt.provider, nil)
			if tt.setup != nil {
				tt.setup(fx)
			}
			r := fx.run(t, fx.checkoutLink())
			if tt.wantStep != "" {
				if r.Status != e2eprobe.StatusFail || r.Step != "checkout-link" {
					t.Fatalf("want failure at step %q, got %+v", tt.wantStep, r)
				}
				if got := stepOf(r); got != tt.wantStep {
					t.Fatalf("step = %q, want %q (result: %+v)", got, tt.wantStep, r)
				}
				if !strings.HasPrefix(r.Attrs["failed_legs"], "links") {
					t.Errorf("failed_legs = %q, want the links leg first", r.Attrs["failed_legs"])
				}
				return
			}
			if r.Status != e2eprobe.StatusPass {
				t.Fatalf("result = %+v", r)
			}
			if got := len(fx.fc.Payment.Cancelled); got < tt.wantCancels {
				t.Errorf("cancelled %d sessions, want at least %d", got, tt.wantCancels)
			}
			if eph := fx.World.Ephemerals().List("customer"); len(eph) != 1 {
				t.Errorf("registered %d ephemeral customers, want the clone only", len(eph))
			}
			if r.Attrs["provider"] != tt.provider || r.Attrs["ephemeral_customer"] == "" {
				t.Errorf("attrs = %v", r.Attrs)
			}
		})
	}
}

func TestCheckoutLink_SkipsWithoutConnection(t *testing.T) {
	fx := newPaymentFixture(t, "stripe", nil)
	fx.fc.TenantOps.Connections = []e2eprobe.Connection{{ID: "c1", ProviderType: "razorpay"}}
	created := len(fx.fc.Customer.Created)
	r := fx.run(t, fx.checkoutLink())
	if r.Status != e2eprobe.StatusSkip || r.Step != "connection" || !strings.Contains(r.Reason, "stripe") {
		t.Fatalf("result = %+v", r)
	}
	if len(fx.fc.Customer.Created) != created {
		t.Errorf("a skipped run must not create customers")
	}
}

func TestCheckoutLink_UnknownGatewaySkips(t *testing.T) {
	fx := newPaymentFixture(t, "nomod", nil)
	created := len(fx.fc.Customer.Created)
	r := fx.run(t, fx.checkoutLink())
	if r.Status != e2eprobe.StatusSkip || r.Step != "capability" {
		t.Fatalf("result = %+v", r)
	}
	if len(fx.fc.Customer.Created) != created {
		t.Errorf("created customers for an unsupported gateway")
	}
}

func TestCheckoutLink_InfraFailures(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(fx *paymentFixture)
		wantStep string
	}{
		{"connections unreadable", func(fx *paymentFixture) { fx.fc.TenantOps.ConnectionsErr = probetest.ServerError(503) }, "connections"},
		{"clone fails", func(fx *paymentFixture) { fx.fc.Customer.CreateErr = probetest.ServerError(500) }, "clone"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newPaymentFixture(t, "stripe", nil)
			tt.setup(fx)
			r := fx.run(t, fx.checkoutLink())
			if r.Status != e2eprobe.StatusFail || r.Step != tt.wantStep || !r.Infra {
				t.Fatalf("result = %+v", r)
			}
		})
	}
}

func TestCheckoutLink_NaturalExpiry(t *testing.T) {
	matured := func(fx *paymentFixture) *expiryWatch {
		return &expiryWatch{sessionID: "cs_old", paymentID: "pay_old", invoiceID: "inv_old", walletID: "wallet_1",
			credits: decimal.Zero, matureAt: fx.now.Add(-time.Minute)}
	}
	expiredSession := func(fx *paymentFixture, status types.CheckoutStatus) {
		fx.fc.Payment.PaymentStatus = types.PaymentStatusFailed
		fx.fc.Payment.Sessions = map[string]*types.CheckoutSessionResponse{"cs_old": {ID: probetest.Ptr("cs_old")}}
		fx.fc.Payment.SessionOnGet = func(s *types.CheckoutSessionResponse) *types.CheckoutSessionResponse {
			out := *s
			out.CheckoutStatus = status.ToPointer()
			return &out
		}
	}
	tests := []struct {
		name     string
		setup    func(fx *paymentFixture)
		wantStep string
	}{
		{
			name:  "cleanup expired the abandoned session",
			setup: func(fx *paymentFixture) { expiredSession(fx, types.CheckoutStatusExpired) },
		},
		{
			name: "abandoned session's payment succeeded",
			setup: func(fx *paymentFixture) {
				expiredSession(fx, types.CheckoutStatusExpired)
				fx.fc.Payment.PaymentStatus = types.PaymentStatusSucceeded
			},
			wantStep: "expiry_assert_unpaid",
		},
		{
			name:     "session still pending past expiry",
			setup:    func(fx *paymentFixture) { expiredSession(fx, types.CheckoutStatusPending) },
			wantStep: "expiry_assert_expired",
		},
		{
			name: "draft invoice left behind",
			setup: func(fx *paymentFixture) {
				expiredSession(fx, types.CheckoutStatusExpired)
				fx.fc.Invoice.ByID["inv_old"] = types.InvoiceResponse{ID: probetest.Ptr("inv_old"), InvoiceStatus: types.InvoiceStatusDraft.ToPointer()}
			},
			wantStep: "expiry_assert_invoice_removed",
		},
		{
			name: "draft invoice soft-deleted by cleanup",
			setup: func(fx *paymentFixture) {
				expiredSession(fx, types.CheckoutStatusExpired)
				fx.fc.Invoice.ByID["inv_old"] = types.InvoiceResponse{ID: probetest.Ptr("inv_old"),
					InvoiceStatus: types.InvoiceStatusDraft.ToPointer(), Status: types.StatusDeleted.ToPointer()}
			},
		},
		{
			name: "abandoned top-up credited the wallet",
			setup: func(fx *paymentFixture) {
				expiredSession(fx, types.CheckoutStatusExpired)
				fx.addCredits(25)
			},
			wantStep: "expiry_assert_no_credit",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newPaymentFixture(t, "stripe", nil)
			fx.fc.Payment.Start = rejectUnattended(400)
			tt.setup(fx)
			s := fx.checkoutLink()
			s.expiryWatch = matured(fx)

			r := fx.run(t, s)
			if tt.wantStep != "" {
				if got := r.Attrs["leg.natural_expiry.step"]; got != tt.wantStep || r.Status != e2eprobe.StatusFail {
					t.Fatalf("natural_expiry step = %q, want %q (result: %+v)", got, tt.wantStep, r)
				}
				return
			}
			if r.Status != e2eprobe.StatusPass {
				t.Fatalf("result = %+v", r)
			}
			if s.expiryWatch == nil || s.expiryWatch.sessionID == "cs_old" {
				t.Errorf("want a fresh watch after verifying the matured one, got %+v", s.expiryWatch)
			}
			if len(fx.World.Ephemerals().List("customer")) != 1 {
				t.Errorf("the expiry customer must not be registered for the janitor")
			}
		})
	}
}

func TestCheckoutLink_ImmatureWatchIsLeftAlone(t *testing.T) {
	fx := newPaymentFixture(t, "stripe", nil)
	fx.fc.Payment.Start = rejectUnattended(400)
	s := fx.checkoutLink()
	pending := &expiryWatch{sessionID: "cs_pending", matureAt: fx.now.Add(time.Hour)}
	s.expiryWatch = pending
	if r := fx.run(t, s); r.Status != e2eprobe.StatusPass {
		t.Fatalf("result = %+v", r)
	}
	if s.expiryWatch != pending {
		t.Errorf("immature watch was replaced: %+v", s.expiryWatch)
	}
}
