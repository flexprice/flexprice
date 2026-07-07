package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

const testEnvScheduleStore = "env_test_sub_schedule"

func newScheduleForStoreTest(ctx context.Context, id, subID string, scheduleType types.SubscriptionScheduleChangeType, status types.ScheduleStatus) *subscription.SubscriptionSchedule {
	now := time.Now().UTC()
	return &subscription.SubscriptionSchedule{
		ID:             id,
		SubscriptionID: subID,
		ScheduleType:   scheduleType,
		ScheduledAt:    now.Add(24 * time.Hour),
		Status:         status,
		TenantID:       types.GetTenantID(ctx),
		EnvironmentID:  testEnvScheduleStore,
		CreatedAt:      now,
		UpdatedAt:      now,
		StatusColumn:   types.StatusPublished,
	}
}

// TestInMemorySubscriptionScheduleStore_GetPendingBySubscriptionAndType verifies the store
// mirrors the real Ent repo (internal/repository/ent/subscription_schedule.go
// GetPendingBySubscriptionAndType): no pending schedule returns (nil, nil), not ErrNotFound.
func TestInMemorySubscriptionScheduleStore_GetPendingBySubscriptionAndType(t *testing.T) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), testEnvScheduleStore)

	testCases := []struct {
		name       string
		seed       []*subscription.SubscriptionSchedule
		subID      string
		queryType  types.SubscriptionScheduleChangeType
		expectedID string // empty means expect (nil, nil)
	}{
		{
			name:      "unknown_subscription_returns_nil_nil",
			seed:      nil,
			subID:     "sub_sched_unknown",
			queryType: types.SubscriptionScheduleChangeTypePlanChange,
		},
		{
			name: "no_pending_schedule_of_requested_type_returns_nil_nil",
			seed: []*subscription.SubscriptionSchedule{
				newScheduleForStoreTest(ctx, "schd_cancel_pending", "sub_sched_1", types.SubscriptionScheduleChangeTypeCancellation, types.ScheduleStatusPending),
			},
			subID:     "sub_sched_1",
			queryType: types.SubscriptionScheduleChangeTypePlanChange,
		},
		{
			name: "non_pending_schedule_of_type_returns_nil_nil",
			seed: []*subscription.SubscriptionSchedule{
				newScheduleForStoreTest(ctx, "schd_plan_executed", "sub_sched_2", types.SubscriptionScheduleChangeTypePlanChange, types.ScheduleStatusExecuted),
			},
			subID:     "sub_sched_2",
			queryType: types.SubscriptionScheduleChangeTypePlanChange,
		},
		{
			name: "pending_schedule_of_type_is_returned",
			seed: []*subscription.SubscriptionSchedule{
				newScheduleForStoreTest(ctx, "schd_plan_cancelled", "sub_sched_3", types.SubscriptionScheduleChangeTypePlanChange, types.ScheduleStatusCancelled),
				newScheduleForStoreTest(ctx, "schd_plan_pending", "sub_sched_3", types.SubscriptionScheduleChangeTypePlanChange, types.ScheduleStatusPending),
			},
			subID:      "sub_sched_3",
			queryType:  types.SubscriptionScheduleChangeTypePlanChange,
			expectedID: "schd_plan_pending",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			store := NewInMemorySubscriptionScheduleStore()
			for _, schedule := range tc.seed {
				require.NoError(t, store.Create(ctx, schedule))
			}

			got, err := store.GetPendingBySubscriptionAndType(ctx, tc.subID, tc.queryType)
			require.NoError(t, err)
			if tc.expectedID == "" {
				require.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			require.Equal(t, tc.expectedID, got.ID)
		})
	}
}
