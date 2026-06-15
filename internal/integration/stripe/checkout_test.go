package stripe

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
)

func TestStripeMode_FromObjective(t *testing.T) {
	if got := stripeModeForObjective(types.CheckoutObjectivePayment); got != "payment" {
		t.Fatalf("payment objective -> %q, want payment", got)
	}
	if got := stripeModeForObjective(types.CheckoutObjectiveSetup); got != "setup" {
		t.Fatalf("setup objective -> %q, want setup", got)
	}
}
