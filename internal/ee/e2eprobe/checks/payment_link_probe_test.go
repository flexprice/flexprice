package checks

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe"
	"github.com/shopspring/decimal"

	"github.com/flexprice/go-sdk/v2/models/types"
)

// rejectUnattended answers a customer-not-present start with status, and every
// other start with a pending session.
func rejectUnattended(status int) func(string, *types.CheckoutParams) (*types.CheckoutSessionResponse, error) {
	return func(_ string, cfg *types.CheckoutParams) (*types.CheckoutSessionResponse, error) {
		if customerNotPresent(cfg) {
			return nil, apiError(status)
		}
		return fakePendingSession(), nil
	}
}

func TestPaymentLinkProbe(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		setup    func(fx *paymentFixture)
		wantStep string
		// wantCancels is how many sessions the probe cancels on success.
		wantCancels int
	}{
		{
			name:        "happy path covers invoice, top-up and auto-charge fallback links",
			provider:    "stripe",
			setup:       func(fx *paymentFixture) { fx.fc.payments.start = rejectUnattended(400) },
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
			setup:    func(fx *paymentFixture) { fx.fc.payments.start = rejectUnattended(500) },
			wantStep: "unattended_assert_rejected",
		},
		{
			name:     "pending session without URL",
			provider: "stripe",
			setup: func(fx *paymentFixture) {
				fx.fc.payments.start = func(string, *types.CheckoutParams) (*types.CheckoutSessionResponse, error) {
					s := fakePendingSession()
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
				fx.fc.payments.start = func(string, *types.CheckoutParams) (*types.CheckoutSessionResponse, error) {
					s := fakePendingSession()
					s.CheckoutStatus = types.CheckoutStatusFailed.ToPointer()
					return s, nil
				}
			},
			wantStep: "invoice_link_assert_pending",
		},
		{
			name:     "cancel does not expire session",
			provider: "stripe",
			setup:    func(fx *paymentFixture) { fx.fc.payments.cancelStatus = types.CheckoutStatusCompleted },
			wantStep: "invoice_link_assert_expired",
		},
		{
			name:     "link top-up credits wallet before payment",
			provider: "razorpay",
			setup: func(fx *paymentFixture) {
				fx.fc.payments.start = func(action string, cfg *types.CheckoutParams) (*types.CheckoutSessionResponse, error) {
					if action == "topup" {
						fx.addCredits(25)
					}
					return fakePendingSession(), nil
				}
			},
			wantStep: "topup_link_assert_no_credit",
		},
		{
			name:     "start rejected",
			provider: "chargebee",
			setup: func(fx *paymentFixture) {
				fx.fc.payments.start = func(string, *types.CheckoutParams) (*types.CheckoutSessionResponse, error) {
					return nil, apiError(500)
				}
			},
			wantStep: "invoice_link_start",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newPaymentFixture(t, tt.provider)
			if tt.setup != nil {
				tt.setup(fx)
			}
			err := NewPaymentLinkProbe(fx.fc, fx.reg, "run1", nil, fx.opts).Run(context.Background())
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
			if got := len(fx.fc.payments.cancelled); got < tt.wantCancels {
				t.Errorf("cancelled %d sessions, want at least %d", got, tt.wantCancels)
			}
			if eph := fx.reg.Ephemerals("customer"); len(eph) != 1 {
				t.Errorf("registered %d ephemeral customers, want 1", len(eph))
			}
		})
	}
}

func TestPaymentLinkProbe_UnknownGatewaySkips(t *testing.T) {
	fx := newPaymentFixture(t, "nomod")
	if err := NewPaymentLinkProbe(fx.fc, fx.reg, "run1", nil, fx.opts).Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(fx.fc.customers.created) != 0 {
		t.Errorf("created %d customers for an unsupported gateway, want 0", len(fx.fc.customers.created))
	}
}

func TestPaymentLinkProbe_NaturalExpiry(t *testing.T) {
	matured := func(fx *paymentFixture) *expiryWatch {
		return &expiryWatch{sessionID: "cs_old", paymentID: "pay_old", invoiceID: "inv_old", walletID: "wallet_1",
			credits: decimal.Zero, matureAt: time.Now().Add(-time.Minute)}
	}
	expiredSession := func(fx *paymentFixture, status types.CheckoutStatus) {
		fx.fc.payments.paymentStatus = types.PaymentStatusFailed
		fx.fc.payments.sessions = map[string]*types.CheckoutSessionResponse{"cs_old": {ID: strPtr("cs_old")}}
		fx.fc.payments.sessionOnGet = func(s *types.CheckoutSessionResponse) *types.CheckoutSessionResponse {
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
				fx.fc.payments.paymentStatus = types.PaymentStatusSucceeded
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
				fx.fc.invoices.getByID = map[string]types.InvoiceResponse{"inv_old": {ID: strPtr("inv_old"), InvoiceStatus: types.InvoiceStatusDraft.ToPointer()}}
			},
			wantStep: "expiry_assert_invoice_removed",
		},
		{
			name: "draft invoice soft-deleted by cleanup",
			setup: func(fx *paymentFixture) {
				expiredSession(fx, types.CheckoutStatusExpired)
				fx.fc.invoices.getByID = map[string]types.InvoiceResponse{"inv_old": {ID: strPtr("inv_old"),
					InvoiceStatus: types.InvoiceStatusDraft.ToPointer(), Status: types.StatusDeleted.ToPointer()}}
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
			fx := newPaymentFixture(t, "stripe")
			fx.fc.payments.start = rejectUnattended(400)
			fx.fc.invoices.getErr = nil
			tt.setup(fx)
			probe := NewPaymentLinkProbe(fx.fc, fx.reg, "run1", nil, fx.opts)
			probe.expiryWatch = matured(fx)

			err := probe.Run(context.Background())
			if tt.wantStep != "" {
				if got := e2eprobe.AttributesFrom(err)["leg.natural_expiry.step"]; got != tt.wantStep {
					t.Fatalf("natural_expiry step = %q, want %q (err: %v)", got, tt.wantStep, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if probe.expiryWatch == nil || probe.expiryWatch.sessionID == "cs_old" {
				t.Errorf("want a fresh watch after verifying the matured one, got %+v", probe.expiryWatch)
			}
		})
	}
}

func TestPaymentLinkProbe_ImmatureWatchIsLeftAlone(t *testing.T) {
	fx := newPaymentFixture(t, "stripe")
	fx.fc.payments.start = rejectUnattended(400)
	probe := NewPaymentLinkProbe(fx.fc, fx.reg, "run1", nil, fx.opts)
	pending := &expiryWatch{sessionID: "cs_pending", matureAt: time.Now().Add(time.Hour)}
	probe.expiryWatch = pending
	if err := probe.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if probe.expiryWatch != pending {
		t.Errorf("immature watch was replaced: %+v", probe.expiryWatch)
	}
}
