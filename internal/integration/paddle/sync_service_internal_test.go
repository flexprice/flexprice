package paddle

import (
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPaddleChargeLineItems_CollapseReportsCollapsedAndKeepsLineName(t *testing.T) {
	chargePID := "pri_new"
	creditPID := "pri_old"
	inv := &invoice.Invoice{
		AmountDue: decimal.RequireFromString("1397.68"),
		LineItems: []*invoice.InvoiceLineItem{
			{PriceID: &chargePID, DisplayName: lo.ToPtr("Team Starter"), Amount: decimal.RequireFromString("1996.68"), Currency: "USD"},
			{PriceID: &creditPID, DisplayName: lo.ToPtr("Team"), Amount: decimal.RequireFromString("-599"), Currency: "USD"},
		},
	}

	got, collapsed := paddleChargeLineItems(inv)
	require.Len(t, got, 1)
	assert.True(t, collapsed)
	assert.True(t, got[0].Amount.Equal(decimal.RequireFromString("1397.68")))
	// The line name feeds Paddle product creation, so collapse must not rewrite it.
	assert.Equal(t, "Team Starter", lo.FromPtr(got[0].DisplayName))
}

func TestPaddleChargeLineItems_NoCollapse(t *testing.T) {
	pid := "pri_a"
	inv := &invoice.Invoice{
		AmountDue: decimal.NewFromInt(100),
		LineItems: []*invoice.InvoiceLineItem{
			{PriceID: &pid, DisplayName: lo.ToPtr("Seat"), Amount: decimal.NewFromInt(100), Currency: "USD"},
		},
	}
	got, collapsed := paddleChargeLineItems(inv)
	require.Len(t, got, 1)
	assert.False(t, collapsed)
	assert.Equal(t, "Seat", lo.FromPtr(got[0].DisplayName))
}

func TestPaddleCollapsedInvoiceDisplayName(t *testing.T) {
	tests := []struct {
		name     string
		inv      *invoice.Invoice
		fallback string
		want     string
	}{
		{
			name: "metadata wins",
			inv: &invoice.Invoice{
				Metadata:      types.WithCollapsedInvoiceDisplayName(nil, "Upgrade: Team → Team Starter"),
				Description:   "ignored",
				InvoiceNumber: lo.ToPtr("INV-47"),
			},
			fallback: "Team Starter",
			want:     "Upgrade: Team → Team Starter",
		},
		{
			name: "unlabelled taxed invoice keeps the line name",
			inv: &invoice.Invoice{
				Description:   "Invoice for advance charges - subscription subs_1",
				InvoiceNumber: lo.ToPtr("INV-99"),
			},
			fallback: "QA Team",
			want:     "QA Team",
		},
		{
			name:     "falls back to line name",
			inv:      &invoice.Invoice{},
			fallback: "Seat",
			want:     "Seat",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, paddleCollapsedInvoiceDisplayName(tt.inv, tt.fallback))
		})
	}
}

func TestTruncatePaddlePriceName(t *testing.T) {
	assert.Len(t, []rune(truncatePaddlePriceName(strings.Repeat("é", 60))), paddlePriceNameMaxRunes)
	assert.Equal(t, "Seat", truncatePaddlePriceName("  Seat  "))
}

func TestPaddleChargeLineItems_NothingOwed(t *testing.T) {
	chargePID := "pri_charge"
	creditPID := "pri_credit"
	lines := []*invoice.InvoiceLineItem{
		{PriceID: &chargePID, DisplayName: lo.ToPtr("Pro"), Amount: decimal.NewFromInt(100), Currency: "USD"},
		{PriceID: &creditPID, DisplayName: lo.ToPtr("Basic"), Amount: decimal.NewFromInt(-100), Currency: "USD"},
	}

	tests := []struct {
		name      string
		amountDue decimal.Decimal
	}{
		{name: "credits cancel the charges out", amountDue: decimal.Zero},
		{name: "credits exceed the charges", amountDue: decimal.NewFromInt(-25)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, collapsed := paddleChargeLineItems(&invoice.Invoice{AmountDue: tt.amountDue, LineItems: lines})
			assert.Empty(t, got, "must not charge the gross positive lines when nothing is owed")
			assert.False(t, collapsed)
		})
	}
}
