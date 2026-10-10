package checks

import (
	"context"
	"testing"
)

func TestPaymentMethodProbe(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		setup    func(fx *paymentFixture)
		wantStep string
		check    func(t *testing.T, fx *paymentFixture)
	}{
		{
			name:     "stripe lists methods and issues setup and add-method links",
			provider: "stripe",
			check: func(t *testing.T, fx *paymentFixture) {
				if len(fx.fc.payments.defaults) != 0 || len(fx.fc.payments.deleted) != 0 {
					t.Errorf("defaults=%v deleted=%v, want no card changes", fx.fc.payments.defaults, fx.fc.payments.deleted)
				}
			},
		},
		{
			name:     "chargebee lists methods and issues an add-method link",
			provider: "chargebee",
		},
		{
			name:     "razorpay refuses add and delete with 4xx",
			provider: "razorpay",
			setup: func(fx *paymentFixture) {
				fx.fc.payments.portalAddErr = apiError(400)
				fx.fc.payments.portalDeleteErr = apiError(400)
			},
		},
		{
			name:     "razorpay accepting add-method is a failure",
			provider: "razorpay",
			setup: func(fx *paymentFixture) {
				fx.fc.payments.portalDeleteErr = apiError(400)
			},
			wantStep: "add_method_assert_rejected",
		},
		{
			name:     "razorpay add-method 5xx is a failure",
			provider: "razorpay",
			setup: func(fx *paymentFixture) {
				fx.fc.payments.portalAddErr = apiError(502)
			},
			wantStep: "add_method_assert_rejected",
		},
		{
			name:     "listing reports a provider error",
			provider: "stripe",
			setup: func(fx *paymentFixture) {
				fx.fc.payments.listErr = apiError(500)
			},
			wantStep: "list_methods_api",
		},
		{
			name:     "stripe setup intent without URL",
			provider: "stripe",
			setup: func(fx *paymentFixture) {
				fx.fc.payments.setupURL = ""
			},
			wantStep: "setup_link",
		},
		{
			name:     "add-method link without URL",
			provider: "chargebee",
			setup: func(fx *paymentFixture) {
				fx.fc.payments.portalAddURL = ""
			},
			wantStep: "add_method_link",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newPaymentFixture(t, tt.provider)
			if tt.setup != nil {
				tt.setup(fx)
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
				tt.check(t, fx)
			}
		})
	}
}
