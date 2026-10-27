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

func TestWalletDebitCSVTopUpAmountUsed(t *testing.T) {
	paid := func(v string) *decimal.Decimal { return lo.ToPtr(decimal.RequireFromString(v)) }

	tests := []struct {
		name     string
		row      wallet.WalletDebitsExportData
		expected string
	}{
		{
			name:     "share of a discounted purchase",
			row:      wallet.WalletDebitsExportData{Credits: decimal.NewFromInt(200), WalletTxnCredits: paid("500"), WalletTxnAmountPaid: paid("450")},
			expected: "180",
		},
		{
			name:     "rounded to the currency",
			row:      wallet.WalletDebitsExportData{Credits: decimal.NewFromInt(1), WalletTxnCredits: paid("3"), WalletTxnAmountPaid: paid("10")},
			expected: "3.33",
		},
		{
			name:     "free or bonus credits",
			row:      wallet.WalletDebitsExportData{Credits: decimal.NewFromInt(20), WalletTxnCredits: paid("100"), WalletTxnAmountPaid: paid("0")},
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
			tt.row.CreditBalanceBefore = decimal.NewFromInt(1000)
			tt.row.CreditBalanceAfter = tt.row.CreditBalanceBefore.Sub(tt.row.Credits)
			tt.row.WalletBalanceBefore = decimal.NewFromInt(2000)
			tt.row.WalletBalanceAfter = tt.row.CreditBalanceAfter.Mul(decimal.NewFromInt(2))
			tt.row.TopUpAmountRemainingBefore = decimal.RequireFromString("500.5049")
			tt.row.TopUpAmountRemainingAfter = decimal.RequireFromString("320.4951")

			out, err := gocsv.MarshalString([]*WalletDebitCSV{toWalletDebitCSV(&tt.row)})
			require.NoError(t, err)
			assert.Contains(t, out, ","+tt.row.Credits.String()+",1000,"+tt.row.CreditBalanceAfter.String()+",2000,"+tt.row.WalletBalanceAfter.String()+",500.5,"+tt.expected+",320.5,2026-10-09T00:00:00Z")
		})
	}
}

func TestCreditReportsCSVInvoicePeriod(t *testing.T) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	debit := toWalletDebitCSV(&wallet.WalletDebitsExportData{Currency: "usd", InvoicePeriodStart: &start, InvoicePeriodEnd: &end})
	assert.Equal(t, "2026-10-01T00:00:00Z", debit.InvoicePeriodStart)
	assert.Equal(t, "2026-11-01T00:00:00Z", debit.InvoicePeriodEnd)

	noInvoice := toWalletDebitCSV(&wallet.WalletDebitsExportData{Currency: "usd"})
	assert.Empty(t, noInvoice.InvoicePeriodStart)
	assert.Empty(t, noInvoice.InvoicePeriodEnd)

	topups := (&CreditTopupExporter{}).convertToCSVRecords([]*wallet.CreditTopupsExportData{
		{InvoicePeriodStart: &start, InvoicePeriodEnd: &end},
		{},
	})
	assert.Equal(t, "2026-10-01T00:00:00Z", topups[0].InvoicePeriodStart)
	assert.Equal(t, "2026-11-01T00:00:00Z", topups[0].InvoicePeriodEnd)
	assert.Empty(t, topups[1].InvoicePeriodStart, "a one-off top-up invoice has no period")
}
