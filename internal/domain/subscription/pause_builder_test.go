package subscription

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionPauseBuilderCopiesResumption(t *testing.T) {
	start := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	original := &SubscriptionPause{ID: "pause", SubscriptionID: "subscription", PauseStart: start, PauseStatus: types.PauseStatusActive, Metadata: types.Metadata{"existing": "kept"}}
	metadata := types.Metadata{"resumed": "true"}
	resumed := NewSubscriptionPauseBuilder(original).WithStatus(types.PauseStatusCompleted).
		WithResumption(start.Add(time.Hour), types.ResumeModeImmediate).WithUpdatedBy("actor").WithMetadata(metadata).Build()
	metadata["resumed"] = "changed"
	resumed.Metadata["existing"] = "changed"
	require.Equal(t, types.PauseStatusActive, original.PauseStatus)
	require.Nil(t, original.ResumedAt)
	require.Equal(t, "kept", original.Metadata["existing"])
	require.Equal(t, "true", resumed.Metadata["resumed"])
	require.Equal(t, start.Add(time.Hour), *resumed.ResumedAt)
	require.Equal(t, types.ResumeModeImmediate, resumed.ResumeMode)
	require.Equal(t, "actor", resumed.UpdatedBy)
	require.Equal(t, original.ID, resumed.ID)
	require.Equal(t, original.SubscriptionID, resumed.SubscriptionID)
	require.Equal(t, original.PauseStart, resumed.PauseStart)

	var absent *subscriptionPauseBuilder
	require.Nil(t, absent.WithStatus(types.PauseStatusCompleted).WithResumption(start, types.ResumeModeImmediate).WithUpdatedBy("actor").WithMetadata(metadata).Build())
	require.NotNil(t, NewSubscriptionPauseBuilder(nil).Build())
}

func TestSubscriptionBuilderLifecycleFields(t *testing.T) {
	start := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	id := "pause"
	original := &Subscription{ID: "subscription", SubscriptionStatus: types.SubscriptionStatusActive}
	paused := NewSubscriptionBuilder(original).WithPauseStatus(types.PauseStatusActive).WithActivePauseID(&id).
		WithCurrentPeriodStart(start).WithProrationBehavior(types.ProrationBehaviorNone).Build()
	id = "changed"
	require.Nil(t, original.ActivePauseID)
	require.Equal(t, "pause", *paused.ActivePauseID)
	require.Equal(t, types.PauseStatusActive, paused.PauseStatus)
	require.Equal(t, start, paused.CurrentPeriodStart)
	require.Equal(t, types.ProrationBehaviorNone, paused.ProrationBehavior)
	require.Nil(t, NewSubscriptionBuilder(paused).WithActivePauseID(nil).Build().ActivePauseID)
	var absent *subscriptionBuilder
	require.Nil(t, absent.WithCurrentPeriodStart(start).WithPauseStatus(types.PauseStatusActive).WithActivePauseID(&id).WithProrationBehavior(types.ProrationBehaviorNone).Build())
}

func TestSubscriptionBuilderMembershipAndCancellation(t *testing.T) {
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	original := &Subscription{ID: "sub", SubscriptionType: types.SubscriptionTypeStandalone, CurrentPeriodStart: now}
	cancelled := now.Add(time.Hour)
	updated := NewSubscriptionBuilder(original).WithSubscriptionType(types.SubscriptionTypeParent).WithCancelledAt(&cancelled).Build()
	cancelled = cancelled.Add(time.Hour)
	require.Equal(t, types.SubscriptionTypeStandalone, original.SubscriptionType)
	require.Nil(t, original.CancelledAt)
	require.Equal(t, types.SubscriptionTypeParent, updated.SubscriptionType)
	require.Equal(t, now.Add(time.Hour), *updated.CancelledAt)
	require.Equal(t, original.CurrentPeriodStart, updated.CurrentPeriodStart)
	var absent *subscriptionBuilder
	require.Nil(t, absent.WithSubscriptionType(types.SubscriptionTypeParent).WithCancelledAt(&cancelled).Build())
	require.NotNil(t, NewSubscriptionBuilder(nil).WithSubscriptionType(types.SubscriptionTypeParent).Build())
}
