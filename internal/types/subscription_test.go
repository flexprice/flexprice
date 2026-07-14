package types

import "testing"

func TestValidateCollectionMethodAndPaymentBehavior(t *testing.T) {
	tests := []struct {
		name             string
		collectionMethod CollectionMethod
		paymentBehavior  PaymentBehavior
		wantErr          bool
	}{
		{"charge_automatically + allow_incomplete", CollectionMethodChargeAutomatically, PaymentBehaviorAllowIncomplete, false},
		{"charge_automatically + error_if_incomplete", CollectionMethodChargeAutomatically, PaymentBehaviorErrorIfIncomplete, false},
		{"charge_automatically + default_active", CollectionMethodChargeAutomatically, PaymentBehaviorDefaultActive, false},
		{"charge_automatically + default_incomplete is invalid", CollectionMethodChargeAutomatically, PaymentBehaviorDefaultIncomplete, true},
		{"send_invoice + default_active", CollectionMethodSendInvoice, PaymentBehaviorDefaultActive, false},
		{"send_invoice + default_incomplete", CollectionMethodSendInvoice, PaymentBehaviorDefaultIncomplete, false},
		{"send_invoice + allow_incomplete is invalid", CollectionMethodSendInvoice, PaymentBehaviorAllowIncomplete, true},
		{"send_invoice + error_if_incomplete is invalid", CollectionMethodSendInvoice, PaymentBehaviorErrorIfIncomplete, true},
		{"unsupported collection method", CollectionMethod("bogus"), PaymentBehaviorDefaultActive, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCollectionMethodAndPaymentBehavior(tt.collectionMethod, tt.paymentBehavior)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}

func TestNormalizeCollectionMethodAndPaymentBehavior(t *testing.T) {
	t.Run("legacy default_incomplete rewritten", func(t *testing.T) {
		cm, pb := NormalizeCollectionMethodAndPaymentBehavior("default_incomplete", "")
		if cm != CollectionMethodChargeAutomatically {
			t.Fatalf("expected charge_automatically, got %s", cm)
		}
		if pb != PaymentBehaviorAllowIncomplete {
			t.Fatalf("expected allow_incomplete, got %s", pb)
		}
	})

	t.Run("normal values pass through unchanged", func(t *testing.T) {
		cm, pb := NormalizeCollectionMethodAndPaymentBehavior("send_invoice", "default_active")
		if cm != CollectionMethodSendInvoice {
			t.Fatalf("expected send_invoice, got %s", cm)
		}
		if pb != PaymentBehaviorDefaultActive {
			t.Fatalf("expected default_active, got %s", pb)
		}
	})
}
