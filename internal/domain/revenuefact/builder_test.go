package revenuefact

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestNewRevert(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	seed := &RevenueFact{
		ID:                "revfact_orig",
		TenantID:          "t1",
		EnvironmentID:     "e1",
		CustomerID:        "c1",
		SubscriptionID:    "s1",
		PriceID:           lo.ToPtr("p1"),
		RevenueSource:     types.RevenueSourceUsage,
		Day:               now.AddDate(0, 0, -3),
		UsageAtListRate:   decimal.NewFromInt(100),
		TierDelta:         decimal.NewFromInt(-10),
		EntitlementAmount: decimal.NewFromInt(20),
		NetAmount:         decimal.NewFromInt(70),
		BillableQty:       decimal.NewFromInt(7000),
		EntitlementQty:    decimal.NewFromInt(2000),
		Status:            types.FactFinal,
		InvoiceID:         lo.ToPtr("inv1"),
		InvoiceLineItemID: lo.ToPtr("li1"),
		Version:           3,
	}

	rev := NewRevert(seed, now)

	// Fresh identity, contra amounts, revert markers.
	assert.NotEqual(t, seed.ID, rev.ID)
	assert.True(t, rev.IsRevert)
	assert.Equal(t, types.FactFinal, rev.Status)
	assert.EqualValues(t, 1, rev.Version)
	assert.Equal(t, now, rev.ComputedAt)
	assert.True(t, rev.NetAmount.Equal(decimal.NewFromInt(-70)))
	assert.True(t, rev.UsageAtListRate.Equal(decimal.NewFromInt(-100)))
	assert.True(t, rev.TierDelta.Equal(decimal.NewFromInt(10)))
	assert.True(t, rev.EntitlementAmount.Equal(decimal.NewFromInt(-20)))
	assert.True(t, rev.BillableQty.Equal(decimal.NewFromInt(-7000)))
	assert.True(t, rev.EntitlementQty.Equal(decimal.NewFromInt(-2000)))

	// Grain + invoice stamps carry over so the contra joins back to what it reverses.
	assert.Equal(t, seed.SubscriptionID, rev.SubscriptionID)
	assert.Equal(t, seed.PriceID, rev.PriceID)
	assert.Equal(t, seed.Day, rev.Day)
	assert.Equal(t, seed.InvoiceID, rev.InvoiceID)
	assert.Equal(t, seed.InvoiceLineItemID, rev.InvoiceLineItemID)

	// The seed must be untouched.
	assert.True(t, seed.NetAmount.Equal(decimal.NewFromInt(70)))
	assert.False(t, seed.IsRevert)
	assert.Equal(t, "revfact_orig", seed.ID)

	// Original + revert nets to zero.
	assert.True(t, seed.NetAmount.Add(rev.NetAmount).IsZero())
}

func TestRevenueFactBuilderNilSafety(t *testing.T) {
	built := NewRevenueFactBuilder(nil).WithID("x").WithStatus(types.FactFinal).Build()
	assert.Equal(t, "x", built.ID)
	assert.Equal(t, types.FactFinal, built.Status)

	var b *revenueFactBuilder
	assert.NotPanics(t, func() { _ = b.WithID("y").Build() })
}
