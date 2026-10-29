package journal

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func testLine(account types.LedgerAccountKey, direction types.JournalDirection, amount string) *Line {
	return NewLineBuilder(nil).WithAccount(account).WithDirection(direction).WithAmount(decimal.RequireFromString(amount)).Build()
}

func dr(account types.LedgerAccountKey, amount string) *Line {
	return testLine(account, types.JournalDirectionDebit, amount)
}

func cr(account types.LedgerAccountKey, amount string) *Line {
	return testLine(account, types.JournalDirectionCredit, amount)
}

func TestValidateEntry(t *testing.T) {
	tests := []struct {
		name    string
		lines   []*Line
		wantErr bool
	}{
		{name: "invoice finalized with tax", lines: []*Line{dr(types.LedgerAccountReceivable, "118"), cr(types.LedgerAccountDeferredRevenue, "100"), cr(types.LedgerAccountTaxPayable, "18")}},
		{name: "recognition", lines: []*Line{dr(types.LedgerAccountUnbilledReceivable, "99.51"), cr(types.LedgerAccountRevenue, "99.51")}},
		{name: "unbalanced", lines: []*Line{dr(types.LedgerAccountReceivable, "118"), cr(types.LedgerAccountDeferredRevenue, "100")}, wantErr: true},
		{name: "debits only", lines: []*Line{dr(types.LedgerAccountReceivable, "10"), dr(types.LedgerAccountCash, "10")}, wantErr: true},
		{name: "empty", wantErr: true},
		{name: "zero amount", lines: []*Line{dr(types.LedgerAccountReceivable, "0"), cr(types.LedgerAccountRevenue, "0")}, wantErr: true},
		{name: "negative amount", lines: []*Line{dr(types.LedgerAccountReceivable, "-5"), cr(types.LedgerAccountRevenue, "-5")}, wantErr: true},
		{name: "unknown account", lines: []*Line{dr("discounts", "5"), cr(types.LedgerAccountRevenue, "5")}, wantErr: true},
		{name: "unknown direction", lines: []*Line{testLine(types.LedgerAccountReceivable, "both", "5"), cr(types.LedgerAccountRevenue, "5")}, wantErr: true},
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
