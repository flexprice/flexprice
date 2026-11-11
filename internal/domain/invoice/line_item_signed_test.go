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
