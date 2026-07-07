package testutil

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests pin the no-aliasing guarantee of the wallet store: real
// repositories materialize a fresh row per read, so a caller mutating a
// returned record (or the fixture it passed to Create) must never edit the
// stored state without an explicit Update call.

func walletFixture(id string) *wallet.Wallet {
	return &wallet.Wallet{
		ID:            id,
		CustomerID:    "cust_1",
		Currency:      "usd",
		Balance:       decimal.NewFromInt(100),
		CreditBalance: decimal.NewFromInt(100),
		WalletStatus:  types.WalletStatusActive,
		Metadata:      types.Metadata{"tier": "gold"},
		Config: types.WalletConfig{
			AllowedPriceTypes: []types.WalletConfigPriceType{types.WalletConfigPriceTypeAll},
		},
		ConversionRate: decimal.NewFromInt(1),
		BaseModel:      types.GetDefaultBaseModel(SetupContext()),
	}
}

func TestInMemoryWalletStoreDoesNotAliasStoredRecords(t *testing.T) {
	ctx := SetupContext()

	t.Run("mutating_the_created_fixture_does_not_edit_stored_wallet", func(t *testing.T) {
		store := NewInMemoryWalletStore()
		w := walletFixture("wallet_alias_create")
		require.NoError(t, store.CreateWallet(ctx, w))

		w.CreditBalance = w.CreditBalance.Add(decimal.NewFromInt(999))
		w.Metadata["tier"] = "mutated"
		w.Config.AllowedPriceTypes[0] = types.WalletConfigPriceType("mutated")

		stored, err := store.GetWalletByID(ctx, "wallet_alias_create")
		require.NoError(t, err)
		assert.True(t, stored.CreditBalance.Equal(decimal.NewFromInt(100)),
			"expected 100, got %s", stored.CreditBalance)
		assert.Equal(t, "gold", stored.Metadata["tier"])
		assert.Equal(t, types.WalletConfigPriceTypeAll, stored.Config.AllowedPriceTypes[0])
	})

	t.Run("mutating_a_read_result_does_not_edit_stored_wallet", func(t *testing.T) {
		store := NewInMemoryWalletStore()
		require.NoError(t, store.CreateWallet(ctx, walletFixture("wallet_alias_get")))

		first, err := store.GetWalletByID(ctx, "wallet_alias_get")
		require.NoError(t, err)
		first.CreditBalance = first.CreditBalance.Add(decimal.NewFromInt(999))
		first.Metadata["tier"] = "mutated"

		second, err := store.GetWalletByID(ctx, "wallet_alias_get")
		require.NoError(t, err)
		assert.True(t, second.CreditBalance.Equal(decimal.NewFromInt(100)),
			"expected 100, got %s", second.CreditBalance)
		assert.Equal(t, "gold", second.Metadata["tier"])
	})

	t.Run("mutating_a_read_transaction_does_not_edit_stored_transaction", func(t *testing.T) {
		store := NewInMemoryWalletStore()
		expiry := time.Now().UTC().Add(24 * time.Hour)
		txn := &wallet.Transaction{
			ID:               "txn_alias",
			WalletID:         "wallet_1",
			Type:             types.TransactionTypeCredit,
			Amount:           decimal.NewFromInt(10),
			CreditAmount:     decimal.NewFromInt(10),
			CreditsAvailable: decimal.NewFromInt(10),
			TxStatus:         types.TransactionStatusCompleted,
			ExpiryDate:       lo.ToPtr(expiry),
			Metadata:         types.Metadata{"source": "test"},
			BaseModel:        types.GetDefaultBaseModel(ctx),
		}
		require.NoError(t, store.CreateTransaction(ctx, txn))

		read, err := store.GetTransactionByID(ctx, "txn_alias")
		require.NoError(t, err)
		read.CreditsAvailable = decimal.Zero
		*read.ExpiryDate = read.ExpiryDate.Add(-48 * time.Hour)
		read.Metadata["source"] = "mutated"

		stored, err := store.GetTransactionByID(ctx, "txn_alias")
		require.NoError(t, err)
		assert.True(t, stored.CreditsAvailable.Equal(decimal.NewFromInt(10)))
		assert.True(t, stored.ExpiryDate.Equal(expiry))
		assert.Equal(t, "test", stored.Metadata["source"])
	})
}
