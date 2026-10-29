package ent

import (
	"context"
	"errors"
	"testing"

	walletdb "github.com/flexprice/flexprice/ent/wallet"
	"github.com/flexprice/flexprice/internal/cache"
	walletdomain "github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type walletTransactionCache struct {
	*fakeRedisCache
	deletedCtx context.Context
	deleteErr  error
	deletes    int
}

func (c *walletTransactionCache) Delete(ctx context.Context, key string) {
	c.deletedCtx = ctx
	c.deleteErr = ctx.Err()
	c.deletes++
	if c.deleteErr != nil {
		return
	}
	c.fakeRedisCache.Delete(ctx, key)
}

func TestWalletTransactionCachePostgres(t *testing.T) {
	db := newBillingPostgresFixture(t)
	ctx := billingTestContext("wallet_cache_tenant", "wallet_cache_environment")
	redis := &walletTransactionCache{fakeRedisCache: newFakeRedisCache()}
	repo := NewWalletRepository(db.client, logger.NewNoopLogger(), redis)
	w := &walletdomain.Wallet{
		ID: types.GenerateUUIDWithPrefix("wallet"), CustomerID: "wallet_cache_customer", Currency: "usd",
		Balance: decimal.NewFromInt(100), CreditBalance: decimal.NewFromInt(100),
		WalletType: types.WalletTypePrePaid, WalletStatus: types.WalletStatusActive,
		ConversionRate: decimal.NewFromInt(1), TopupConversionRate: decimal.NewFromInt(1),
		EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}
	require.NoError(t, repo.CreateWallet(ctx, w))
	key := cache.GenerateKey(ctx, cache.PrefixWallet, w.ID)
	assertCachedBalance := func(amount int64) {
		t.Helper()
		value, found := redis.Get(ctx, key)
		require.True(t, found)
		cached, ok := cache.UnmarshalCacheValue[walletdomain.Wallet](value)
		require.True(t, ok)
		require.True(t, cached.Balance.Equal(decimal.NewFromInt(amount)))
		require.True(t, cached.CreditBalance.Equal(decimal.NewFromInt(amount)))
	}
	initial, err := repo.GetWalletByID(ctx, w.ID)
	require.NoError(t, err)
	require.True(t, initial.Balance.Equal(decimal.NewFromInt(100)))
	assertCachedBalance(100)

	failure := errors.New("scheduled source cancellation failed after wallet refund")
	err = db.client.WithTx(ctx, func(tx context.Context) error {
		if err := repo.UpdateWalletBalance(tx, w.ID, decimal.NewFromInt(125), decimal.NewFromInt(125)); err != nil {
			return err
		}
		refunded, err := repo.GetWalletByID(tx, w.ID)
		if err != nil {
			return err
		}
		require.True(t, refunded.Balance.Equal(decimal.NewFromInt(125)), "transaction reads its refunded balance")
		assertCachedBalance(100)
		require.Zero(t, redis.deletes, "uncommitted mutations do not invalidate shared cache")
		outside, err := repo.GetWalletByID(ctx, w.ID)
		if err != nil {
			return err
		}
		require.True(t, outside.Balance.Equal(decimal.NewFromInt(100)), "another request sees the committed wallet")
		return failure
	})
	require.ErrorIs(t, err, failure)
	assertCachedBalance(100)
	require.Zero(t, redis.deletes)
	rolledBack, err := repo.GetWalletByID(types.WithForceWriter(ctx), w.ID)
	require.NoError(t, err)
	require.True(t, rolledBack.Balance.Equal(decimal.NewFromInt(100)))

	err = db.client.WithTx(ctx, func(tx context.Context) error {
		if err := repo.UpdateWalletBalance(tx, w.ID, decimal.NewFromInt(125), decimal.NewFromInt(125)); err != nil {
			return err
		}
		refunded, err := repo.GetWalletByID(tx, w.ID)
		if err != nil {
			return err
		}
		require.True(t, refunded.Balance.Equal(decimal.NewFromInt(125)))
		assertCachedBalance(100)
		require.Zero(t, redis.deletes)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, redis.deletes)
	require.Nil(t, db.client.TxFromContext(redis.deletedCtx), "post-commit invalidation drops the completed transaction")
	_, found := redis.Get(ctx, key)
	require.False(t, found, "commit invalidates the old balance")
	committed, err := repo.GetWalletByID(ctx, w.ID)
	require.NoError(t, err)
	require.True(t, committed.Balance.Equal(decimal.NewFromInt(125)))
	assertCachedBalance(125)

	for _, foreign := range []context.Context{
		billingTestContext("another_tenant", types.GetEnvironmentID(ctx)),
		billingTestContext(types.GetTenantID(ctx), "another_environment"),
	} {
		err := repo.UpdateWalletBalance(foreign, w.ID, decimal.NewFromInt(999), decimal.NewFromInt(999))
		require.True(t, ierr.IsNotFound(err), "a foreign wallet update cannot silently succeed: %v", err)
	}
	for _, missing := range []context.Context{nil, context.Background(), billingTestContext(types.GetTenantID(ctx), "")} {
		require.Error(t, repo.UpdateWalletBalance(missing, w.ID, decimal.NewFromInt(999), decimal.NewFromInt(999)))
	}
	committed, err = repo.GetWalletByID(types.WithForceWriter(ctx), w.ID)
	require.NoError(t, err)
	require.True(t, committed.Balance.Equal(decimal.NewFromInt(125)))
	assertCachedBalance(125)

	_, err = db.client.Writer(ctx).Wallet.Update().Where(
		walletdb.ID(w.ID), walletdb.TenantID(types.GetTenantID(ctx)), walletdb.EnvironmentID(types.GetEnvironmentID(ctx)),
	).SetBalance(decimal.NewFromInt(150)).SetCreditBalance(decimal.NewFromInt(150)).Save(ctx)
	require.NoError(t, err)
	fresh, err := repo.GetWalletByID(types.WithForceWriter(ctx), w.ID)
	require.NoError(t, err)
	require.True(t, fresh.Balance.Equal(decimal.NewFromInt(150)), "force-writer reads bypass a stale cache")
	assertCachedBalance(125)
	redis.Delete(ctx, key)
	_, err = repo.GetWalletByID(types.WithForceWriter(ctx), w.ID)
	require.NoError(t, err)
	_, found = redis.Get(ctx, key)
	require.False(t, found, "force-writer reads do not fill shared cache")

	_, err = repo.GetWalletByID(ctx, w.ID)
	require.NoError(t, err)
	require.NoError(t, repo.UpdateWalletBalance(ctx, w.ID, decimal.NewFromInt(175), decimal.NewFromInt(175)))
	_, found = redis.Get(ctx, key)
	require.False(t, found, "an update without a transaction invalidates immediately")

	_, err = repo.GetWalletByID(ctx, w.ID)
	require.NoError(t, err)
	callerCtx, cancelCaller := context.WithCancel(ctx)
	defer cancelCaller()
	err = db.client.WithTx(callerCtx, func(tx context.Context) error {
		require.True(t, types.RegisterPostCommit(tx, cancelCaller))
		return repo.UpdateWalletBalance(tx, w.ID, decimal.NewFromInt(200), decimal.NewFromInt(200))
	})
	require.NoError(t, err)
	require.ErrorIs(t, callerCtx.Err(), context.Canceled)
	require.NoError(t, redis.deleteErr, "a canceled caller cannot prevent committed balance invalidation")
	_, bounded := redis.deletedCtx.Deadline()
	require.True(t, bounded)
	_, found = redis.Get(ctx, key)
	require.False(t, found)
	committed, err = repo.GetWalletByID(ctx, w.ID)
	require.NoError(t, err)
	require.True(t, committed.Balance.Equal(decimal.NewFromInt(200)))
}
