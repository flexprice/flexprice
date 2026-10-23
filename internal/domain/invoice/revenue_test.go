package invoice

import (
	"testing"

	"github.com/flexprice/flexprice/internal/domain/taxapplied"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestInvoice_LineNets(t *testing.T) {
	d := decimal.RequireFromString
	line := func(id, amount, lineDiscount, invoiceDiscount string) *InvoiceLineItem {
		return &InvoiceLineItem{ID: id, Amount: d(amount), LineItemDiscount: d(lineDiscount), InvoiceLevelDiscount: d(invoiceDiscount)}
	}
	tax := func(entityID, amount string, behavior types.TaxBehavior) *taxapplied.TaxApplied {
		return &taxapplied.TaxApplied{EntityType: types.TaxRateEntityTypeInvoice, EntityID: entityID, TaxAmount: d(amount), TaxBehavior: behavior}
	}

	tests := []struct {
		name  string
		lines []*InvoiceLineItem
		taxes []*taxapplied.TaxApplied
		want  map[string]string
	}{
		{
			name:  "discounts come off each line",
			lines: []*InvoiceLineItem{line("a", "100", "10", "5")},
			want:  map[string]string{"a": "85"},
		},
		{
			name:  "inclusive tax is shared pro rata by net",
			lines: []*InvoiceLineItem{line("a", "60", "0", "0"), line("b", "40", "0", "0")},
			taxes: []*taxapplied.TaxApplied{tax("inv_1", "12", types.TaxBehaviorInclusive)},
			want:  map[string]string{"a": "52.8", "b": "35.2"},
		},
		{
			name:  "rounding residual lands on the largest line",
			lines: []*InvoiceLineItem{line("a", "1", "0", "0"), line("b", "2", "0", "0"), line("c", "1", "0", "0")},
			taxes: []*taxapplied.TaxApplied{tax("inv_1", "1", types.TaxBehaviorInclusive)},
			want:  map[string]string{"a": "0.75", "b": "1.50", "c": "0.75"},
		},
		{
			name:  "equal lines put the residual on the smallest ID, whatever the order",
			lines: []*InvoiceLineItem{line("c", "1", "0", "0"), line("a", "1", "0", "0"), line("b", "1", "0", "0")},
			taxes: []*taxapplied.TaxApplied{tax("inv_1", "1", types.TaxBehaviorInclusive)},
			want:  map[string]string{"a": "0.66", "b": "0.67", "c": "0.67"},
		},
		{
			name:  "a credit line takes a negative share",
			lines: []*InvoiceLineItem{line("a", "100", "0", "0"), line("b", "-20", "0", "0")},
			taxes: []*taxapplied.TaxApplied{tax("inv_1", "8", types.TaxBehaviorInclusive)},
			want:  map[string]string{"a": "90", "b": "-18"},
		},
		{
			name:  "a zero line takes no share",
			lines: []*InvoiceLineItem{line("a", "50", "0", "0"), line("b", "0", "0", "0")},
			taxes: []*taxapplied.TaxApplied{tax("inv_1", "5", types.TaxBehaviorInclusive)},
			want:  map[string]string{"a": "45", "b": "0"},
		},
		{
			name:  "exclusive, reversed and other invoices' taxes are ignored",
			lines: []*InvoiceLineItem{line("a", "100", "0", "0")},
			taxes: []*taxapplied.TaxApplied{
				tax("inv_1", "18", types.TaxBehaviorExclusive),
				tax("inv_2", "9", types.TaxBehaviorInclusive),
				{EntityType: types.TaxRateEntityTypeInvoice, EntityID: "inv_1", TaxAmount: d("7"), TaxBehavior: types.TaxBehaviorInclusive, TaxTransactionType: types.TaxTransactionTypeReversal},
			},
			want: map[string]string{"a": "100"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := &Invoice{ID: "inv_1", Currency: "usd", LineItems: tt.lines}

			got := inv.LineNets(tt.taxes)

			require.Len(t, got, len(tt.want))
			for id, want := range tt.want {
				require.True(t, d(want).Equal(got[id]), "line %s: want %s, got %s", id, want, got[id])
			}
		})
	}
}
