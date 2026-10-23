package invoice

import (
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSignedQuantityOnlyForUsageLines(t *testing.T) {
	usage, fixed := string(types.PRICE_TYPE_USAGE), string(types.PRICE_TYPE_FIXED)
	li := &InvoiceLineItem{PriceType: &usage, Quantity: decimal.NewFromInt(-30), Amount: decimal.NewFromInt(-30)}
	require.NoError(t, li.Validate())
	li.PriceType = &fixed
	require.Error(t, li.Validate())
}

func TestSyncsToVendors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		total   int64
		lines   []int64
		skipped bool
		want    bool
	}{
		{"positive", 50, []int64{50}, false, true},
		{"zero", 0, []int64{0}, false, true},
		{"skipped", 0, nil, true, false},
		{"negative total", -5, nil, false, false},
		{"negative line positive total", 5, []int64{50, -45}, false, false},
		{"negative line zero total", 0, []int64{50, -50}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inv := &Invoice{Total: decimal.NewFromInt(tc.total), InvoiceStatus: types.InvoiceStatusFinalized}
			if tc.skipped {
				inv.InvoiceStatus = types.InvoiceStatusSkipped
			}
			for _, amount := range tc.lines {
				inv.LineItems = append(inv.LineItems, &InvoiceLineItem{Amount: decimal.NewFromInt(amount)})
			}
			if got := inv.SyncsToVendors(); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
