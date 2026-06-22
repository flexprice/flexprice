package checkout

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
)

func TestCheckout_StatusHelpers(t *testing.T) {
	tests := []struct {
		name         string
		status       types.CheckoutStatus
		wantPending  bool
		wantTerminal bool
	}{
		{"pending", types.CheckoutStatusPending, true, false},
		{"completed", types.CheckoutStatusCompleted, false, true},
		{"expired", types.CheckoutStatusExpired, false, true},
		{"cancelled", types.CheckoutStatusCancelled, false, true},
		{"failed", types.CheckoutStatusFailed, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Checkout{Status: tt.status}
			if got := c.IsPending(); got != tt.wantPending {
				t.Fatalf("IsPending() = %v, want %v", got, tt.wantPending)
			}
			if got := c.IsTerminal(); got != tt.wantTerminal {
				t.Fatalf("IsTerminal() = %v, want %v", got, tt.wantTerminal)
			}
		})
	}
}

func TestCheckout_GetConfigurationMap_Nil(t *testing.T) {
	c := &Checkout{}
	m, err := c.GetConfigurationMap()
	if err != nil {
		t.Fatalf("GetConfigurationMap: %v", err)
	}
	if m != nil {
		t.Fatalf("expected nil, got %+v", m)
	}
}
