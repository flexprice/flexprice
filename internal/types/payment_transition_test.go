package types

import "testing"

func TestPaymentStatusValidateTransitionTo(t *testing.T) {
	tests := []struct {
		name    string
		from    PaymentStatus
		to      PaymentStatus
		wantErr bool
	}{
		// A settled payment must not be walked backwards through the update API.
		{"succeeded cannot be reverted to failed", PaymentStatusSucceeded, PaymentStatusFailed, true},
		{"succeeded cannot be reverted to pending", PaymentStatusSucceeded, PaymentStatusPending, true},
		{"succeeded cannot be reverted to processing", PaymentStatusSucceeded, PaymentStatusProcessing, true},

		// Terminal statuses stay sealed.
		{"failed is terminal", PaymentStatusFailed, PaymentStatusSucceeded, true},
		{"failed cannot retry via pending", PaymentStatusFailed, PaymentStatusPending, true},
		{"refunded is terminal", PaymentStatusRefunded, PaymentStatusSucceeded, true},
		{"voided is terminal", PaymentStatusVoided, PaymentStatusSucceeded, true},

		// Legitimate gateway lifecycle.
		{"initiated to pending", PaymentStatusInitiated, PaymentStatusPending, false},
		{"pending to processing", PaymentStatusPending, PaymentStatusProcessing, false},
		{"processing to succeeded", PaymentStatusProcessing, PaymentStatusSucceeded, false},
		{"pending to failed", PaymentStatusPending, PaymentStatusFailed, false},

		// External gateways and backfills create with ProcessPayment=false,
		// leaving the payment INITIATED, then record the outcome in one update.
		{"initiated to succeeded for external gateways", PaymentStatusInitiated, PaymentStatusSucceeded, false},
		{"initiated to overpaid for external gateways", PaymentStatusInitiated, PaymentStatusOverpaid, false},

		// Post-settlement money movement.
		{"succeeded to refunded", PaymentStatusSucceeded, PaymentStatusRefunded, false},
		{"succeeded to partially refunded", PaymentStatusSucceeded, PaymentStatusPartiallyRefunded, false},
		{"succeeded to voided", PaymentStatusSucceeded, PaymentStatusVoided, false},
		{"partially refunded to refunded", PaymentStatusPartiallyRefunded, PaymentStatusRefunded, false},

		// Self-transitions are no-ops, not errors.
		{"succeeded to succeeded", PaymentStatusSucceeded, PaymentStatusSucceeded, false},
		{"failed to failed", PaymentStatusFailed, PaymentStatusFailed, false},

		{"rejects unknown target status", PaymentStatusPending, PaymentStatus("NOT_A_STATUS"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.from.ValidateTransitionTo(tt.to)
			if tt.wantErr && err == nil {
				t.Fatalf("expected %s -> %s to be rejected, got nil error", tt.from, tt.to)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected %s -> %s to be allowed, got: %v", tt.from, tt.to, err)
			}
		})
	}
}

func TestPaymentStatusIsDeletable(t *testing.T) {
	tests := []struct {
		status PaymentStatus
		want   bool
	}{
		// Settled or in-flight money movement must survive for reconciliation:
		// deleting such a payment hides it while the money it records still moved.
		{PaymentStatusSucceeded, false},
		{PaymentStatusOverpaid, false},
		{PaymentStatusProcessing, false},
		{PaymentStatusRefunded, false},
		{PaymentStatusPartiallyRefunded, false},
		{PaymentStatusVoided, false},

		// PENDING means the gateway accepted the payment and the outcome is
		// still to come, so the record must survive.
		{PaymentStatusPending, false},

		// Never charged: not sent to a gateway, or sent and definitively failed.
		{PaymentStatusInitiated, true},
		{PaymentStatusFailed, true},
	}

	for _, tt := range tests {
		t.Run(tt.status.String(), func(t *testing.T) {
			if got := tt.status.IsDeletable(); got != tt.want {
				t.Fatalf("IsDeletable() for %s = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}
