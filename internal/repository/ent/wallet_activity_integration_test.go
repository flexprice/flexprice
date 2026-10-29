package ent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/flexprice/flexprice/internal/activity"
	walletdomain "github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type walletActivitySink struct {
	mu      sync.Mutex
	batches []activity.Batch
}

func (s *walletActivitySink) Publish(batch activity.Batch) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.batches = append(s.batches, batch)
}

func (s *walletActivitySink) entries() []activity.Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	var entries []activity.Entry
	for _, batch := range s.batches {
		entries = append(entries, batch.Entries...)
	}
	return entries
}

func TestWalletBalanceActivityPostgres(t *testing.T) {
	db := newBillingPostgresFixture(t)
	ctx := billingTestContext("wallet_activity_tenant", "wallet_activity_environment")
	repo := NewWalletRepository(db.client, logger.NewNoopLogger(), newFakeRedisCache())
	w := &walletdomain.Wallet{
		ID: types.GenerateUUIDWithPrefix("wallet"), CustomerID: "wallet_activity_customer", Currency: "usd",
		Balance: decimal.NewFromInt(100), CreditBalance: decimal.NewFromInt(100),
		WalletType: types.WalletTypePrePaid, WalletStatus: types.WalletStatusActive,
		ConversionRate: decimal.NewFromInt(1), TopupConversionRate: decimal.NewFromInt(1),
		EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}
	require.NoError(t, repo.CreateWallet(ctx, w))
	sink := &walletActivitySink{}
	db.client.Writer(ctx).Use(activity.Hook(activity.Registrations(), sink))
	assertEntry := func(entry activity.Entry, balance, credits int64) {
		t.Helper()
		require.Equal(t, w.ID, entry.EntityID)
		require.Equal(t, types.SystemEntityTypeWallet, entry.EntityType)
		require.Equal(t, activity.Action("wallet.updated"), entry.Action)
		require.Equal(t, types.GetTenantID(ctx), entry.TenantID)
		require.Equal(t, types.GetEnvironmentID(ctx), entry.EnvironmentID)
		var state walletdomain.Wallet
		require.NoError(t, json.Unmarshal(entry.State, &state))
		require.True(t, state.Balance.Equal(decimal.NewFromInt(balance)))
		require.True(t, state.CreditBalance.Equal(decimal.NewFromInt(credits)))
	}
	require.NoError(t, repo.UpdateWalletBalance(ctx, w.ID, decimal.NewFromInt(90), decimal.NewFromInt(80)))
	require.Len(t, sink.entries(), 1)
	assertEntry(sink.entries()[0], 90, 80)

	failure := errors.New("invoice void failed after refund")
	err := db.client.WithTx(ctx, func(tx context.Context) error {
		require.NoError(t, repo.UpdateWalletBalance(tx, w.ID, decimal.NewFromInt(120), decimal.NewFromInt(110)))
		require.Len(t, sink.entries(), 1, "uncommitted activity stays buffered")
		return failure
	})
	require.ErrorIs(t, err, failure)
	require.Len(t, sink.entries(), 1, "rollback does not publish the refunded balance")

	err = db.client.WithTx(ctx, func(tx context.Context) error {
		require.NoError(t, repo.UpdateWalletBalance(tx, w.ID, decimal.NewFromInt(120), decimal.NewFromInt(110)))
		require.Len(t, sink.entries(), 1)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, sink.entries(), 2)
	assertEntry(sink.entries()[1], 120, 110)
	for _, foreign := range []context.Context{
		billingTestContext("another_tenant", types.GetEnvironmentID(ctx)),
		billingTestContext(types.GetTenantID(ctx), "another_environment"),
	} {
		err := repo.UpdateWalletBalance(foreign, w.ID, decimal.NewFromInt(999), decimal.NewFromInt(999))
		require.True(t, ierr.IsNotFound(err))
	}
	require.Len(t, sink.entries(), 2, "failed scoped updates do not emit activity")
}
