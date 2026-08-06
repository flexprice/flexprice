package dto

import (
	"testing"

	"github.com/shopspring/decimal"
)

// Near half-up boundaries, float64 round-trips can flip the final cent.
// AmountDecimal must preserve the exact decimal used for billing.
func TestSubscriptionUsageByMetersResponse_AmountDecimalPreservesRoundingBoundary(t *testing.T) {
	boundary := decimal.RequireFromString("0.014999999999999999")

	viaFloat := decimal.NewFromFloat(boundary.InexactFloat64()).Round(2)
	if !viaFloat.Equal(decimal.RequireFromString("0.02")) {
		t.Fatalf("precondition: expected float round-trip to flip to 0.02, got %s", viaFloat)
	}

	charge := &SubscriptionUsageByMetersResponse{}
	charge.SetAmountDecimal(boundary)
	got := charge.AmountDecimal().Round(2)
	want := decimal.RequireFromString("0.01")
	if !got.Equal(want) {
		t.Fatalf("AmountDecimal().Round(2)=%s, want %s (float Amount=%v)", got, want, charge.Amount)
	}
}

func TestSubscriptionUsageByMetersResponse_QuantityDecimalPreservesHighPrecision(t *testing.T) {
	q := decimal.RequireFromString("1234567.890123456789")
	viaFloat := decimal.NewFromFloat(q.InexactFloat64())
	if viaFloat.Equal(q) {
		t.Fatalf("precondition: expected quantity float round-trip loss")
	}

	charge := &SubscriptionUsageByMetersResponse{}
	charge.SetQuantityDecimal(q)
	if !charge.QuantityDecimal().Equal(q) {
		t.Fatalf("QuantityDecimal()=%s, want %s", charge.QuantityDecimal(), q)
	}
}
