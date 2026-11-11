package dto

import (
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSignedInvoiceRequest(t *testing.T) {
	req := CreateInvoiceRequest{
		CustomerID: "customer", InvoiceType: types.InvoiceTypeOneOff, Currency: "usd",
		Subtotal: decimal.NewFromInt(-30), Total: decimal.NewFromInt(-30), AmountDue: decimal.Zero,
		LineItems: []CreateInvoiceLineItemRequest{{Amount: decimal.NewFromInt(-30), Quantity: decimal.NewFromInt(-30), PriceType: lo.ToPtr(string(types.PRICE_TYPE_USAGE))}},
	}
	require.NoError(t, req.Validate())
	req.AmountDue = decimal.NewFromInt(-30)
	require.Error(t, req.Validate(), "collection amount must stay nonnegative")
	req.AmountDue = decimal.Zero
	req.LineItems[0].PriceType = lo.ToPtr(string(types.PRICE_TYPE_FIXED))
	require.Error(t, req.Validate(), "negative quantities are only supported for usage")
}
