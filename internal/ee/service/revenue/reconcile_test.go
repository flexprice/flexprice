package revenue

import (
	"testing"

	"github.com/flexprice/flexprice/internal/domain/revenuefact"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func usageRow(usageAtListRate, tierDelta, entitlementAmount, lineDiscount, invoiceDiscount, netAmount string) *revenuefact.RevenueFact {
	return &revenuefact.RevenueFact{
		RevenueSource:     types.RevenueSourceUsage,
		UsageAtListRate:   decimal.RequireFromString(usageAtListRate),
		TierDelta:         decimal.RequireFromString(tierDelta),
		EntitlementAmount: decimal.RequireFromString(entitlementAmount),
		LineDiscount:      decimal.RequireFromString(lineDiscount),
		InvoiceDiscount:   decimal.RequireFromString(invoiceDiscount),
		NetAmount:         decimal.RequireFromString(netAmount),
	}
}

func fixedRow(netAmount string) *revenuefact.RevenueFact {
	return &revenuefact.RevenueFact{
		RevenueSource: types.RevenueSourceFixed,
		NetAmount:     decimal.RequireFromString(netAmount),
	}
}

func TestReconcileRow(t *testing.T) {
	tests := []struct {
		name       string
		row        *revenuefact.RevenueFact
		wantOK     bool
		wantResSet bool // whether we assert an exact nonzero residual
		wantRes    decimal.Decimal
	}{
		{
			name:   "usage row matches: 100 + 10 - 5 - 2 - 1 = 102",
			row:    usageRow("100", "10", "5", "2", "1", "102"),
			wantOK: true,
		},
		{
			name:   "usage row matches with zero discounts",
			row:    usageRow("50", "0", "0", "0", "0", "50"),
			wantOK: true,
		},
		{
			name:       "usage row mismatch",
			row:        usageRow("100", "10", "5", "2", "1", "999"),
			wantOK:     false,
			wantResSet: true,
			wantRes:    decimal.RequireFromString("897"), // 999 - 102
		},
		{
			name:   "fixed row always ok, residual zero",
			row:    fixedRow("500"),
			wantOK: true,
		},
		{
			name:   "commitment_trueup row always ok, residual zero",
			row:    &revenuefact.RevenueFact{RevenueSource: types.RevenueSourceCommitmentTrueup, NetAmount: decimal.RequireFromString("1000")},
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			residual, ok := reconcileRow(tt.row)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantResSet {
				assert.True(t, residual.Equal(tt.wantRes), "expected residual %s, got %s", tt.wantRes, residual)
			} else {
				assert.True(t, residual.Abs().LessThanOrEqual(reconcileEpsilon), "expected residual ~0, got %s", residual)
			}
		})
	}
}

func TestReconcileLineItem(t *testing.T) {
	rows := []*revenuefact.RevenueFact{
		{NetAmount: decimal.RequireFromString("30")},
		{NetAmount: decimal.RequireFromString("40")},
		{NetAmount: decimal.RequireFromString("30")},
	}

	t.Run("matches engine amount", func(t *testing.T) {
		residual, ok := reconcileLineItem(rows, decimal.RequireFromString("100"))
		assert.True(t, ok)
		assert.True(t, residual.Abs().LessThanOrEqual(reconcileEpsilon))
	})

	t.Run("mismatch vs engine amount", func(t *testing.T) {
		residual, ok := reconcileLineItem(rows, decimal.RequireFromString("150"))
		assert.False(t, ok)
		assert.True(t, residual.Equal(decimal.RequireFromString("-50")))
	})

	t.Run("empty rows vs zero engine amount", func(t *testing.T) {
		residual, ok := reconcileLineItem(nil, decimal.Zero)
		assert.True(t, ok)
		assert.True(t, residual.IsZero())
	})
}

func TestReconcileInvoice(t *testing.T) {
	rows := []*revenuefact.RevenueFact{
		{NetAmount: decimal.RequireFromString("100")},
		{NetAmount: decimal.RequireFromString("200")},
	}

	t.Run("matches subtotal minus discount", func(t *testing.T) {
		residual, ok := reconcileInvoice(rows, decimal.RequireFromString("300"))
		assert.True(t, ok)
		assert.True(t, residual.Abs().LessThanOrEqual(reconcileEpsilon))
	})

	t.Run("mismatch vs subtotal minus discount", func(t *testing.T) {
		residual, ok := reconcileInvoice(rows, decimal.RequireFromString("250"))
		assert.False(t, ok)
		assert.True(t, residual.Equal(decimal.RequireFromString("50")))
	})
}

// TestReconcileRow_ChecksEveryDecomposedRow: the identity is claimed by every
// decomposed row, not just usage ones — the commitment split maintains it for
// overage rows too, and leaving them unchecked meant claiming exactness we
// never verified. Money-only rows (a true-up, or an overage reported as an
// amount with no units behind it) carry the charge whole and are exempt.
func TestReconcileRow_ChecksEveryDecomposedRow(t *testing.T) {
	marginalOverage := func(listRate, tierDelta, net string) *revenuefact.RevenueFact {
		return &revenuefact.RevenueFact{
			RevenueSource:     types.RevenueSourceOverage,
			DecompositionMode: types.Marginal,
			UsageAtListRate:   decimal.RequireFromString(listRate),
			TierDelta:         decimal.RequireFromString(tierDelta),
			BillableQty:       decimal.NewFromInt(10),
			NetAmount:         decimal.RequireFromString(net),
		}
	}

	// 20 list + 20 tier (the 2x overage factor) = 40 billed.
	_, ok := reconcileRow(marginalOverage("20", "20", "40"))
	assert.True(t, ok, "a consistent overage row reconciles")

	residual, ok := reconcileRow(marginalOverage("20", "20", "35"))
	assert.False(t, ok, "a broken overage row must now be caught")
	assert.Equal(t, "-5", residual.String())

	// A per-bucket true-up is money with no usage behind it: nothing to check.
	trueUp := &revenuefact.RevenueFact{
		RevenueSource:     types.RevenueSourceCommitmentTrueup,
		DecompositionMode: types.Marginal,
		NetAmount:         decimal.NewFromInt(50),
	}
	_, ok = reconcileRow(trueUp)
	assert.True(t, ok, "money-only rows carry the charge whole")
	assert.False(t, carriesDecomposition(trueUp))
}
