//go:build pgintegration

package ent

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/ent/wallettransaction"
	"github.com/flexprice/flexprice/internal/config"
	walletdomain "github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// The in-memory store mirrors these writes; only Postgres proves the jsonb round-trip and that
// an update without a source leaves the stored one in place. Run with:
//
//	go test -tags pgintegration ./internal/repository/ent -run TestWalletLedgerFacts
func TestWalletLedgerFacts(t *testing.T) {
	cfg, err := config.NewConfig()
	require.NoError(t, err)
	log, err := logger.NewLogger(cfg)
	require.NoError(t, err)
	clients, err := postgres.NewEntClients(cfg, log)
	require.NoError(t, err)
	client := postgres.NewClient(clients, log, nil)

	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), "env_ledger_test")
	repo := NewWalletRepository(client, log, nil)

	runID := types.GenerateUUIDWithPrefix("ledger")
	base := types.GetDefaultBaseModel(ctx)
	w := &walletdomain.Wallet{
		ID:                  "wallet_" + runID,
		CustomerID:          "cust_" + runID,
		Currency:            "usd",
		WalletType:          types.WalletTypePrePaid,
		WalletStatus:        types.WalletStatusActive,
		ConversionRate:      decimal.NewFromInt(1),
		TopupConversionRate: decimal.NewFromInt(1),
		EnvironmentID:       types.GetEnvironmentID(ctx),
		BaseModel:           base,
	}
	require.NoError(t, repo.CreateWallet(ctx, w))

	newTx := func(id string, txType types.TransactionType, status types.TransactionStatus, credits int64) *walletdomain.Transaction {
		return &walletdomain.Transaction{
			ID:                id,
			WalletID:          w.ID,
			CustomerID:        w.CustomerID,
			Type:              txType,
			Amount:            decimal.NewFromInt(credits),
			CreditAmount:      decimal.NewFromInt(credits),
			CreditsAvailable:  decimal.NewFromInt(credits),
			TxStatus:          status,
			TransactionReason: types.TransactionReasonPurchasedCreditInvoiced,
			Currency:          w.Currency,
			EnvironmentID:     types.GetEnvironmentID(ctx),
			BaseModel:         base,
		}
	}

	t.Run("source is written at completion and kept by later updates", func(t *testing.T) {
		purchase := newTx("wtx_purchase_"+runID, types.TransactionTypeCredit, types.TransactionStatusPending, 100)
		require.NoError(t, repo.CreateTransaction(ctx, purchase))
		require.Empty(t, purchase.SourceType)

		purchase.TxStatus = types.TransactionStatusCompleted
		purchase.SourceType = types.WalletTxSourceTypeInvoice
		purchase.SourceID = "inv_first_" + runID
		purchase.UpdatedAt = time.Now().UTC()
		require.NoError(t, repo.UpdateTransaction(ctx, purchase))

		purchase.SourceType = ""
		purchase.SourceID = ""
		require.NoError(t, repo.UpdateTransaction(ctx, purchase))

		got, err := repo.GetTransactionByID(ctx, purchase.ID)
		require.NoError(t, err)
		require.Equal(t, types.TransactionStatusCompleted, got.TxStatus)
		require.Equal(t, types.WalletTxSourceTypeInvoice, got.SourceType)
		require.Equal(t, "inv_first_"+runID, got.SourceID, "an update without a source leaves it in place")
	})

	t.Run("consumption breakdown round-trips through jsonb", func(t *testing.T) {
		first := newTx("wtx_batch_a_"+runID, types.TransactionTypeCredit, types.TransactionStatusCompleted, 50)
		second := newTx("wtx_batch_b_"+runID, types.TransactionTypeCredit, types.TransactionStatusCompleted, 30)
		require.NoError(t, repo.CreateTransaction(ctx, first))
		require.NoError(t, repo.CreateTransaction(ctx, second))

		consumed, err := repo.ConsumeCredits(ctx, []*walletdomain.Transaction{first, second}, decimal.RequireFromString("70.5"))
		require.NoError(t, err)
		require.Equal(t, []types.WalletTxConsumption{
			{CreditTransactionID: first.ID, Credits: decimal.NewFromInt(50)},
			{CreditTransactionID: second.ID, Credits: decimal.RequireFromString("20.5")},
		}, consumed)

		debit := newTx("wtx_debit_"+runID, types.TransactionTypeDebit, types.TransactionStatusCompleted, 0)
		debit.CreditAmount = decimal.RequireFromString("70.5")
		debit.CreditsAvailable = decimal.Zero
		debit.ConsumptionBreakdown = consumed
		debit.SourceType = types.WalletTxSourceTypeInvoice
		debit.SourceID = "inv_debit_" + runID
		require.NoError(t, repo.CreateTransaction(ctx, debit))

		got, err := repo.GetTransactionByID(ctx, debit.ID)
		require.NoError(t, err)
		require.Len(t, got.ConsumptionBreakdown, 2)

		// A credit carries no breakdown: the column is SQL NULL, not a JSON null.
		withoutBreakdown, err := client.Reader(ctx).WalletTransaction.Query().
			Where(wallettransaction.ID(first.ID), wallettransaction.ConsumptionBreakdownIsNil()).
			Count(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, withoutBreakdown)
		require.True(t, got.ConsumptionBreakdown[1].Credits.Equal(decimal.RequireFromString("20.5")))
		require.Equal(t, "inv_debit_"+runID, got.SourceID)

		left, err := repo.GetTransactionByID(ctx, second.ID)
		require.NoError(t, err)
		require.True(t, left.CreditsAvailable.Equal(decimal.RequireFromString("9.5")))
	})
}
