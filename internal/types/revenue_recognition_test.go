package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLedgerAccountClassification(t *testing.T) {
	tests := []struct {
		key       LedgerAccountKey
		class     AccountClass
		side      JournalDirection
		statement StatementType
	}{
		{LedgerAccountCash, AccountClassAsset, JournalDirectionDebit, StatementTypeBalanceSheet},
		{LedgerAccountReceivable, AccountClassAsset, JournalDirectionDebit, StatementTypeBalanceSheet},
		{LedgerAccountUnbilledReceivable, AccountClassAsset, JournalDirectionDebit, StatementTypeBalanceSheet},
		{LedgerAccountDeferredRevenue, AccountClassLiability, JournalDirectionCredit, StatementTypeBalanceSheet},
		{LedgerAccountCustomerCredit, AccountClassLiability, JournalDirectionCredit, StatementTypeBalanceSheet},
		{LedgerAccountTaxPayable, AccountClassLiability, JournalDirectionCredit, StatementTypeBalanceSheet},
		{LedgerAccountRevenue, AccountClassRevenue, JournalDirectionCredit, StatementTypeIncomeStatement},
		{LedgerAccountContraRevenue, AccountClassContraRevenue, JournalDirectionDebit, StatementTypeIncomeStatement},
		{"discounts", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(string(tt.key), func(t *testing.T) {
			require.Equal(t, tt.class, tt.key.Class())
			require.Equal(t, tt.side, tt.key.NormalSide())
			require.Equal(t, tt.statement, tt.key.Statement())
		})
	}
}
