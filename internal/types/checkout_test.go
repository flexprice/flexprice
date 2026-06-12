package types

import "testing"

func TestCheckoutEnums_Validate(t *testing.T) {
	tests := []struct {
		name    string
		fn      func() error
		wantErr bool
	}{
		{"valid entity type", func() error { return CheckoutEntityTypeSubscription.Validate() }, false},
		{"invalid entity type", func() error { return CheckoutEntityType("nope").Validate() }, true},
		{"valid type", func() error { return CheckoutTypeSubscriptionCreation.Validate() }, false},
		{"invalid type", func() error { return CheckoutType("nope").Validate() }, true},
		{"valid objective", func() error { return CheckoutObjectivePayment.Validate() }, false},
		{"invalid objective", func() error { return CheckoutObjective("nope").Validate() }, true},
		{"valid status", func() error { return CheckoutStatusPending.Validate() }, false},
		{"invalid status", func() error { return CheckoutStatus("nope").Validate() }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fn()
			if (err != nil) != tt.wantErr {
				t.Fatalf("got err=%v, wantErr=%v", err, tt.wantErr)
			}
		})
	}
}
