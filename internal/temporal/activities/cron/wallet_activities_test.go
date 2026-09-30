package cron

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func TestOrderByExpiry(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	credit := func(id string, expiry, created time.Time) *dto.WalletTransactionResponse {
		return &dto.WalletTransactionResponse{Transaction: &wallet.Transaction{
			ID:         id,
			ExpiryDate: lo.ToPtr(expiry),
			BaseModel:  types.BaseModel{CreatedAt: created},
		}}
	}
	// Listed newest first, as the repository returns them.
	txs := []*dto.WalletTransactionResponse{
		credit("late", base.AddDate(0, 0, 20), base.AddDate(0, 0, 12)),
		credit("tie_b", base.AddDate(0, 0, 10), base.AddDate(0, 0, 2)),
		credit("tie_a", base.AddDate(0, 0, 10), base.AddDate(0, 0, 2)),
		credit("early", base.AddDate(0, 0, 10), base),
	}

	orderByExpiry(txs)

	require.Equal(t, []string{"early", "tie_a", "tie_b", "late"},
		lo.Map(txs, func(tx *dto.WalletTransactionResponse, _ int) string { return tx.ID }))
}
