package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestAdvanceBillingPeriodPreservesPauseAndRejectsBackwardReplay(t *testing.T) {
	fixture, svc, sub := newLifecycleFixture(t)
	ctx := fixture.GetContext()
	paused := subscription.NewSubscriptionBuilder(sub).WithStatus(types.SubscriptionStatusPaused).WithPauseStatus(types.PauseStatusActive).Build()
	require.NoError(t, svc.SubRepo.Update(ctx, paused))
	oldPeriod := dto.Period{Start: sub.CurrentPeriodStart, End: sub.CurrentPeriodEnd}
	next := dto.Period{Start: oldPeriod.End, End: oldPeriod.End.AddDate(0, 1, 0)}
	updated, err := svc.AdvanceBillingPeriod(ctx, sub.ID, next)
	require.NoError(t, err)
	require.Len(t, updated, 1)
	require.Equal(t, types.SubscriptionStatusPaused, updated[0].SubscriptionStatus)
	require.Equal(t, types.PauseStatusActive, updated[0].PauseStatus)
	require.True(t, updated[0].CurrentPeriodStart.Equal(next.Start))
	_, err = svc.AdvanceBillingPeriod(ctx, sub.ID, oldPeriod)
	require.NoError(t, err)
	stored, err := svc.SubRepo.Get(ctx, sub.ID)
	require.NoError(t, err)
	require.True(t, stored.CurrentPeriodStart.Equal(next.Start))
	require.Equal(t, types.SubscriptionStatusPaused, stored.SubscriptionStatus)
}

func TestLegacyPeriodUsesFreshPausedState(t *testing.T) {
	fixture, svc, stale := newLifecycleFixture(t)
	ctx := fixture.GetContext()
	paused := subscription.NewSubscriptionBuilder(stale).WithStatus(types.SubscriptionStatusPaused).Build()
	require.NoError(t, svc.SubRepo.Update(ctx, paused))
	require.NoError(t, svc.processSubscriptionPeriod(ctx, stale, stale.CurrentPeriodEnd.AddDate(0, 1, 0)))
	stored, err := svc.SubRepo.Get(ctx, stale.ID)
	require.NoError(t, err)
	require.Equal(t, types.SubscriptionStatusPaused, stored.SubscriptionStatus)
	require.True(t, stored.CurrentPeriodStart.Equal(stale.CurrentPeriodStart))
	invoices, err := svc.InvoiceRepo.List(ctx, types.NewNoLimitInvoiceFilter())
	require.NoError(t, err)
	require.Empty(t, invoices)
}
