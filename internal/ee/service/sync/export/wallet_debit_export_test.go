package export

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gocarina/gocsv"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWalletDebitCSVTopUpUsedAmount(t *testing.T) {
	paid := func(v string) *decimal.Decimal { return lo.ToPtr(decimal.RequireFromString(v)) }

	tests := []struct {
		name     string
		row      wallet.WalletDebitsExportData
		expected string
	}{
		{
			name:     "share of a discounted purchase",
			row:      wallet.WalletDebitsExportData{Credits: decimal.NewFromInt(200), WalletTxnCredits: paid("500"), WalletTxnPaidAmount: paid("450")},
			expected: "180",
		},
		{
			name:     "rounded to the currency",
			row:      wallet.WalletDebitsExportData{Credits: decimal.NewFromInt(1), WalletTxnCredits: paid("3"), WalletTxnPaidAmount: paid("10")},
			expected: "3.33",
		},
		{
			name:     "free or bonus credits",
			row:      wallet.WalletDebitsExportData{Credits: decimal.NewFromInt(20), WalletTxnCredits: paid("100"), WalletTxnPaidAmount: paid("0")},
			expected: "0",
		},
		{
			name:     "credits with no paid amount",
			row:      wallet.WalletDebitsExportData{Credits: decimal.NewFromInt(10), WalletTxnCredits: paid("40")},
			expected: "",
		},
		{
			name:     "debit with no recorded batches",
			row:      wallet.WalletDebitsExportData{Credits: decimal.NewFromInt(5)},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.row.Currency = "usd"
			tt.row.TransactionReason = types.TransactionReasonInvoicePayment
			tt.row.CreatedAt = time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)

			out, err := gocsv.MarshalString([]*WalletDebitCSV{toWalletDebitCSV(&tt.row)})
			require.NoError(t, err)
			assert.Contains(t, out, ","+tt.row.Credits.String()+","+tt.expected+",2026-10-09T00:00:00Z")
		})
	}
}
