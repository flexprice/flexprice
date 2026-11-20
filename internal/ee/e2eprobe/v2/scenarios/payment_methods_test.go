package scenarios

import (
	"testing"

	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2"
	"github.com/flexprice/flexprice/internal/ee/e2eprobe/v2/probetest"
)

func TestPaymentMethods(t *testing.T) {
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
				if len(fx.fc.Payment.Defaults) != 0 || len(fx.fc.Payment.Deleted) != 0 {
					t.Errorf("defaults=%v deleted=%v, want no card changes", fx.fc.Payment.Defaults, fx.fc.Payment.Deleted)
				}
				if len(fx.World.Ephemerals().List("customer")) != 1 {
					t.Errorf("want the clone registered for the janitor")
				}
			},
		},
		{
			name:     "chargebee lists methods and issues an add-method link",
			provider: "chargebee",
		},
		{
			name:     "razorpay lists methods and issues an add-method link",
			provider: "razorpay",
		},
		{
			name:     "razorpay add-method 5xx is a failure",
			provider: "razorpay",
			setup:    func(fx *paymentFixture) { fx.fc.Payment.PortalAddErr = rejection(502) },
			wantStep: "add_method_link",
		},
		{
			name:     "listing reports a provider error",
			provider: "stripe",
			setup:    func(fx *paymentFixture) { fx.fc.Payment.ListErr = rejection(500) },
			wantStep: "list_methods_api",
		},
		{
			name:     "stripe setup intent without URL",
			provider: "stripe",
			setup:    func(fx *paymentFixture) { fx.fc.Payment.SetupURL = "" },
			wantStep: "setup_link",
		},
		{
			name:     "add-method link without URL",
			provider: "chargebee",
			setup:    func(fx *paymentFixture) { fx.fc.Payment.PortalAddURL = "" },
			wantStep: "add_method_link",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newPaymentFixture(t, tt.provider, nil)
			if tt.setup != nil {
				tt.setup(fx)
			}
			r := fx.run(t, fx.paymentMethods())
			if tt.wantStep != "" {
				if r.Status != e2eprobe.StatusFail || r.Step != "payment-methods" {
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
			if tt.check != nil {
				tt.check(t, fx)
			}
		})
	}
}

func TestPaymentMethods_SkipsAndInfra(t *testing.T) {
	t.Run("no connection", func(t *testing.T) {
		fx := newPaymentFixture(t, "stripe", nil)
		fx.fc.TenantOps.Connections = nil
		if r := fx.run(t, fx.paymentMethods()); r.Status != e2eprobe.StatusSkip || r.Step != "connection" {
			t.Fatalf("result = %+v", r)
		}
	})
	t.Run("clone fails", func(t *testing.T) {
		fx := newPaymentFixture(t, "stripe", nil)
		fx.fc.Customer.CreateErr = probetest.ServerError(502)
		if r := fx.run(t, fx.paymentMethods()); r.Status != e2eprobe.StatusFail || r.Step != "clone" || !r.Infra {
			t.Fatalf("result = %+v", r)
		}
	})
}
