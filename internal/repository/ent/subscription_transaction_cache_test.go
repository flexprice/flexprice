package ent

import (
	"context"
	"errors"
	"testing"

	domainSub "github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionTransactionReadsBypassCache(t *testing.T) {
	ctx, database, repo := newSubscriptionTestRepository(t)
	sub := subscriptionTestFixture(ctx, "sub_transaction_cache", types.SubscriptionStatusActive, nil, nil)
	require.NoError(t, repo.Create(ctx, sub))
	_, err := repo.Get(ctx, sub.ID)
	require.NoError(t, err)
	require.Equal(t, types.SubscriptionStatusActive, repo.GetCache(ctx, sub.ID).SubscriptionStatus)
	rollback := errors.New("rollback paused state")
	err = database.WithTx(ctx, func(tx context.Context) error {
		paused := domainSub.NewSubscriptionBuilder(sub).WithStatus(types.SubscriptionStatusPaused).Build()
		if err := repo.Update(tx, paused); err != nil {
			return err
		}
		fresh, err := repo.Get(tx, sub.ID)
		if err != nil {
			return err
		}
		require.Equal(t, types.SubscriptionStatusPaused, fresh.SubscriptionStatus)
		require.Equal(t, types.SubscriptionStatusActive, repo.GetCache(ctx, sub.ID).SubscriptionStatus, "uncommitted state is never cached")
		return rollback
	})
	require.ErrorIs(t, err, rollback)
	fresh, err := repo.Get(types.WithForceWriter(ctx), sub.ID)
	require.NoError(t, err)
	require.Equal(t, types.SubscriptionStatusActive, fresh.SubscriptionStatus)
	t.Run("writer-pinned reads bypass a populated cache", func(t *testing.T) {
		repo.SetCache(ctx, sub)
		pinned := types.WithWriterPinning(ctx)
		require.NotNil(t, repo.GetCache(pinned, sub.ID), "installing a pin alone does not bypass the cache")
		types.PinWriter(pinned)
		require.Nil(t, repo.GetCache(pinned, sub.ID))
		require.NotNil(t, repo.GetCache(ctx, sub.ID), "another request can still use the cached subscription")
	})
}
