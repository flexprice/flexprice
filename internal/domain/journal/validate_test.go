package journal

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type testLine struct {
	account   types.LedgerAccountKey
	direction types.JournalDirection
	amount    decimal.Decimal
}

func (l testLine) Account() types.LedgerAccountKey   { return l.account }
func (l testLine) Direction() types.JournalDirection { return l.direction }
func (l testLine) Amount() decimal.Decimal           { return l.amount }

func dr(account types.LedgerAccountKey, amount string) testLine {
	return testLine{account, types.JournalDirectionDebit, decimal.RequireFromString(amount)}
}

func cr(account types.LedgerAccountKey, amount string) testLine {
	return testLine{account, types.JournalDirectionCredit, decimal.RequireFromString(amount)}
}

func TestValidateEntry(t *testing.T) {
	tests := []struct {
		name    string
		lines   []testLine
		wantErr bool
	}{
		{name: "invoice finalized with tax", lines: []testLine{dr(types.LedgerAccountReceivable, "118"), cr(types.LedgerAccountDeferredRevenue, "100"), cr(types.LedgerAccountTaxPayable, "18")}},
		{name: "recognition", lines: []testLine{dr(types.LedgerAccountUnbilledReceivable, "99.51"), cr(types.LedgerAccountRevenue, "99.51")}},
		{name: "unbalanced", lines: []testLine{dr(types.LedgerAccountReceivable, "118"), cr(types.LedgerAccountDeferredRevenue, "100")}, wantErr: true},
		{name: "debits only", lines: []testLine{dr(types.LedgerAccountReceivable, "10"), dr(types.LedgerAccountCash, "10")}, wantErr: true},
		{name: "empty", wantErr: true},
		{name: "zero amount", lines: []testLine{dr(types.LedgerAccountReceivable, "0"), cr(types.LedgerAccountRevenue, "0")}, wantErr: true},
		{name: "negative amount", lines: []testLine{dr(types.LedgerAccountReceivable, "-5"), cr(types.LedgerAccountRevenue, "-5")}, wantErr: true},
		{name: "unknown account", lines: []testLine{dr("discounts", "5"), cr(types.LedgerAccountRevenue, "5")}, wantErr: true},
		{name: "unknown direction", lines: []testLine{{types.LedgerAccountReceivable, "both", decimal.NewFromInt(5)}, cr(types.LedgerAccountRevenue, "5")}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEntry(tt.lines)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
