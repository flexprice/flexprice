package checks

import (
	"context"
	"testing"
)

func TestPaymentMethodProbe(t *testing.T) {
	tests := []struct {
		name       string
		provider   string
		withDriver bool
		setup      func(fx *paymentFixture, d *fakeGatewayDriver)
		wantStep   string
		check      func(t *testing.T, fx *paymentFixture, d *fakeGatewayDriver)
	}{
		{
			name:       "stripe vaulted card is listed, made default and deleted",
			provider:   "stripe",
			withDriver: true,
			check: func(t *testing.T, fx *paymentFixture, d *fakeGatewayDriver) {
				if len(d.attached) != 1 {
					t.Fatalf("attached %d cards, want 1", len(d.attached))
				}
				if len(fx.fc.payments.defaults) != 1 || len(fx.fc.payments.deleted) != 1 {
					t.Errorf("defaults=%v deleted=%v, want one of each", fx.fc.payments.defaults, fx.fc.payments.deleted)
				}
			},
		},
		{
			name:     "without gateway credentials only links are checked",
			provider: "chargebee",
			check: func(t *testing.T, fx *paymentFixture, _ *fakeGatewayDriver) {
				if len(fx.fc.payments.deleted) != 0 {
					t.Errorf("deleted %v without a driver", fx.fc.payments.deleted)
				}
			},
		},
		{
			name:     "razorpay refuses add and delete with 4xx",
			provider: "razorpay",
			setup: func(fx *paymentFixture, _ *fakeGatewayDriver) {
				fx.fc.payments.portalAddErr = apiError(400)
				fx.fc.payments.portalDeleteErr = apiError(400)
			},
		},
		{
			name:     "razorpay accepting add-method is a failure",
			provider: "razorpay",
			setup: func(fx *paymentFixture, _ *fakeGatewayDriver) {
				fx.fc.payments.portalDeleteErr = apiError(400)
			},
			wantStep: "add_method_assert_rejected",
		},
		{
			name:     "razorpay add-method 5xx is a failure",
			provider: "razorpay",
			setup: func(fx *paymentFixture, _ *fakeGatewayDriver) {
				fx.fc.payments.portalAddErr = apiError(502)
			},
			wantStep: "add_method_assert_rejected",
		},
		{
			name:     "listing reports a provider error",
			provider: "stripe",
			setup: func(fx *paymentFixture, _ *fakeGatewayDriver) {
				fx.fc.payments.listErr = apiError(500)
			},
			wantStep: "list_methods_api",
		},
		{
			name:     "stripe setup intent without URL",
			provider: "stripe",
			setup: func(fx *paymentFixture, _ *fakeGatewayDriver) {
				fx.fc.payments.setupURL = ""
			},
			wantStep: "setup_link",
		},
		{
			name:       "vaulted card not auto-chargeable",
			provider:   "stripe",
			withDriver: true,
			setup: func(_ *paymentFixture, d *fakeGatewayDriver) {
				d.notAutoChargeable = true
			},
			wantStep: "assert_auto_chargeable",
		},
		{
			name:       "vaulting fails at the gateway",
			provider:   "stripe",
			withDriver: true,
			setup: func(_ *paymentFixture, d *fakeGatewayDriver) {
				d.attachErr = apiError(402)
			},
			wantStep: "vault_card",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newPaymentFixture(t, tt.provider, nil)
			driver := &fakeGatewayDriver{payments: &fx.fc.payments}
			if tt.withDriver {
				fx.opts.Driver = driver
			}
			if tt.setup != nil {
				tt.setup(fx, driver)
			}
			err := NewPaymentMethodProbe(fx.fc, fx.reg, "run1", nil, fx.opts).Run(context.Background())
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
			if tt.check != nil {
				tt.check(t, fx, driver)
			}
		})
	}
}
