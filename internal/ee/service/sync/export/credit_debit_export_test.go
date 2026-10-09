package export

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestCreditDebitCSVRecognizedAmount(t *testing.T) {
	paid := func(v string) decimal.NullDecimal { return decimal.NewNullDecimal(decimal.RequireFromString(v)) }

	tests := []struct {
		name     string
		row      wallet.CreditDebitsExportData
		expected string
	}{
		{
			name:     "share of a discounted purchase",
			row:      wallet.CreditDebitsExportData{Credits: decimal.NewFromInt(200), BatchCredits: paid("500"), BatchPaidAmount: paid("450")},
			expected: "180",
		},
		{
			name:     "rounded to the currency",
			row:      wallet.CreditDebitsExportData{Credits: decimal.NewFromInt(1), BatchCredits: paid("3"), BatchPaidAmount: paid("10")},
			expected: "3.33",
		},
		{
			name:     "free or bonus credits",
			row:      wallet.CreditDebitsExportData{Credits: decimal.NewFromInt(20), BatchCredits: paid("100"), BatchPaidAmount: paid("0")},
			expected: "0",
		},
		{
			name:     "credits with no paid amount",
			row:      wallet.CreditDebitsExportData{Credits: decimal.NewFromInt(10), BatchCredits: paid("40")},
			expected: "",
		},
		{
			name:     "debit with no recorded batches",
			row:      wallet.CreditDebitsExportData{Credits: decimal.NewFromInt(5)},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.row.Currency = "usd"
			tt.row.TransactionReason = types.TransactionReasonInvoicePayment
			tt.row.CreatedAt = time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)

			record := toCreditDebitCSV(&tt.row)
			assert.Equal(t, tt.expected, record.RecognizedAmount)
			assert.Equal(t, tt.row.Credits.String(), record.Credits)
			assert.Equal(t, "2026-10-09T00:00:00Z", record.CreatedAt)
		})
	}
}
