package checkout

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
)

func TestCheckout_StatusHelpers(t *testing.T) {
	c := &Checkout{Status: types.CheckoutStatusPending}
	if !c.IsPending() {
		t.Fatal("expected IsPending true")
	}
	if c.IsTerminal() {
		t.Fatal("expected IsTerminal false for pending")
	}
	c.Status = types.CheckoutStatusCompleted
	if c.IsPending() {
		t.Fatal("expected IsPending false for completed")
	}
	if !c.IsTerminal() {
		t.Fatal("expected IsTerminal true for completed")
	}
}

func TestCheckout_ConfigurationRoundTrip(t *testing.T) {
	c := &Checkout{}
	if err := c.SetConfiguration(&CheckoutConfiguration{SaveCard: true}); err != nil {
		t.Fatalf("SetConfiguration: %v", err)
	}
	got, err := c.GetConfiguration()
	if err != nil {
		t.Fatalf("GetConfiguration: %v", err)
	}
	if got == nil || !got.SaveCard {
		t.Fatalf("expected SaveCard true, got %+v", got)
	}
}

func TestCheckout_GetConfiguration_NilWhenEmpty(t *testing.T) {
	c := &Checkout{}
	got, err := c.GetConfiguration()
	if err != nil {
		t.Fatalf("GetConfiguration: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil configuration, got %+v", got)
	}
}
