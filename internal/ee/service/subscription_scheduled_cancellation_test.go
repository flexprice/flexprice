package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

type cancellationRaceRepository struct {
	subscription.Repository
	beforeLock func(context.Context, string)
}

func (r *cancellationRaceRepository) GetForUpdateNowait(ctx context.Context, id string) (*subscription.Subscription, error) {
	if r.beforeLock != nil {
		hook := r.beforeLock
		r.beforeLock = nil
		hook(ctx, id)
	}
	return r.Repository.GetForUpdateNowait(ctx, id)
}

func TestScheduledCancellationUsesFreshState(t *testing.T) {
	for _, scenario := range []string{"revoked", "rescheduled", "already cancelled", "not yet due", "cancel and replay"} {
		t.Run(scenario, func(t *testing.T) {
			fixture, svc, sub := newLifecycleFixture(t)
			ctx := fixture.GetContext()
			end := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
			scheduled := subscription.NewSubscriptionBuilder(sub).WithEndDate(&end).Build()
			require.NoError(t, svc.SubRepo.Update(ctx, scheduled))
			repo := svc.SubRepo
			svc.SubRepo = &cancellationRaceRepository{Repository: repo, beforeLock: func(tx context.Context, id string) {
				current, err := repo.Get(tx, id)
				require.NoError(t, err)
				builder := subscription.NewSubscriptionBuilder(current)
				switch scenario {
				case "revoked":
					builder.WithEndDate(nil)
				case "rescheduled", "not yet due":
					builder.WithEndDate(lo.ToPtr(time.Now().UTC().Add(time.Hour)))
				case "already cancelled":
					builder.WithStatus(types.SubscriptionStatusCancelled).WithCancelledAt(lo.ToPtr(end.Add(-time.Minute)))
				}
				require.NoError(t, repo.Update(tx, builder.Build()))
			}}
			periodEnd := end
			if scenario == "not yet due" {
				periodEnd = time.Now().UTC().Add(time.Hour)
				svc.SubRepo = repo
				require.NoError(t, repo.Update(ctx, subscription.NewSubscriptionBuilder(scheduled).WithEndDate(&periodEnd).Build()))
			}
			cancelled, err := svc.CompleteScheduledCancellation(ctx, sub.ID, periodEnd)
			require.NoError(t, err)
			stored, err := repo.Get(ctx, sub.ID)
			require.NoError(t, err)
			events := fixture.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher)
			switch scenario {
			case "already cancelled":
				require.True(t, cancelled)
				require.Equal(t, end.Add(-time.Minute), *stored.CancelledAt)
				require.Empty(t, events.Events())
			case "cancel and replay":
				require.True(t, cancelled)
				require.Equal(t, types.SubscriptionStatusCancelled, stored.SubscriptionStatus)
				require.Equal(t, end, *stored.CancelledAt)
				require.Len(t, events.Events(), 2)
				cancelled, err = svc.CompleteScheduledCancellation(ctx, sub.ID, end)
				require.NoError(t, err)
				require.True(t, cancelled)
				require.Len(t, events.Events(), 2)
			default:
				require.False(t, cancelled)
				require.Equal(t, types.SubscriptionStatusActive, stored.SubscriptionStatus)
				require.Nil(t, stored.CancelledAt)
				require.Empty(t, events.Events())
			}
		})
	}
}

func TestScheduledCancellationCompletesInheritedPauses(t *testing.T) {
	fixture, svc, sub := newLifecycleFixture(t)
	ctx := fixture.GetContext()
	end := time.Now().UTC().Add(-time.Hour)
	parent := subscription.NewSubscriptionBuilder(sub).WithSubscriptionType(types.SubscriptionTypeParent).WithEndDate(&end).Build()
	require.NoError(t, svc.SubRepo.Update(ctx, parent))
	child := subscription.NewSubscriptionBuilder(parent).WithSubscriptionType(types.SubscriptionTypeInherited).
		WithParentSubscriptionID(&parent.ID).WithStatus(types.SubscriptionStatusPaused).WithPauseStatus(types.PauseStatusActive).
		WithActivePauseID(lo.ToPtr("child_pause")).Build()
	child.ID = "inherited_child"
	require.NoError(t, svc.SubRepo.Create(ctx, child))
	pause := &subscription.SubscriptionPause{
		ID: "child_pause", SubscriptionID: child.ID, PauseStatus: types.PauseStatusActive,
		EnvironmentID: parent.EnvironmentID, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	require.NoError(t, svc.SubRepo.CreatePause(ctx, pause))
	grouped := subscription.NewSubscriptionBuilder(parent).WithSubscriptionType(types.SubscriptionTypeGroupedInvoicing).WithParentSubscriptionID(&parent.ID).Build()
	grouped.ID = "grouped_child"
	require.NoError(t, svc.SubRepo.Create(ctx, grouped))
	completed, err := svc.CompleteScheduledCancellation(ctx, parent.ID, end)
	require.NoError(t, err)
	require.True(t, completed)
	for _, id := range []string{parent.ID, child.ID} {
		current, err := svc.SubRepo.Get(ctx, id)
		require.NoError(t, err)
		require.Equal(t, types.SubscriptionStatusCancelled, current.SubscriptionStatus)
		require.Equal(t, end, *current.CancelledAt)
		require.Equal(t, types.PauseStatusNone, current.PauseStatus)
		require.Nil(t, current.ActivePauseID)
	}
	current, err := svc.SubRepo.Get(ctx, grouped.ID)
	require.NoError(t, err)
	require.Equal(t, types.SubscriptionStatusActive, current.SubscriptionStatus)
	pause, err = svc.SubRepo.GetPause(ctx, pause.ID)
	require.NoError(t, err)
	require.Equal(t, types.PauseStatusCompleted, pause.PauseStatus)
}
