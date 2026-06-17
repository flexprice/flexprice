package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
)

// TestSubscriptionSchedule_DomainHelpers tests the helper methods on SubscriptionSchedule
func TestSubscriptionSchedule_DomainHelpers(t *testing.T) {
	t.Run("IsPending returns true for pending status", func(t *testing.T) {
		schedule := &subscription.SubscriptionSchedule{
			Status: types.ScheduleStatusPending,
		}
		assert.True(t, schedule.IsPending())
	})

	t.Run("IsPending returns false for non-pending status", func(t *testing.T) {
		schedule := &subscription.SubscriptionSchedule{
			Status: types.ScheduleStatusExecuted,
		}
		assert.False(t, schedule.IsPending())
	})

	t.Run("CanBeCancelled returns true for future pending schedule", func(t *testing.T) {
		schedule := &subscription.SubscriptionSchedule{
			Status:      types.ScheduleStatusPending,
			ScheduledAt: time.Now().Add(24 * time.Hour),
		}
		assert.True(t, schedule.CanBeCancelled())
	})

	t.Run("CanBeCancelled returns false for executed schedule", func(t *testing.T) {
		schedule := &subscription.SubscriptionSchedule{
			Status:      types.ScheduleStatusExecuted,
			ScheduledAt: time.Now().Add(24 * time.Hour),
		}
		assert.False(t, schedule.CanBeCancelled())
	})

	t.Run("CanBeCancelled returns false for expired pending schedule", func(t *testing.T) {
		schedule := &subscription.SubscriptionSchedule{
			Status:      types.ScheduleStatusPending,
			ScheduledAt: time.Now().Add(-24 * time.Hour),
		}
		assert.False(t, schedule.CanBeCancelled())
	})

	t.Run("IsExpired returns true for past scheduled time", func(t *testing.T) {
		schedule := &subscription.SubscriptionSchedule{
			Status:      types.ScheduleStatusPending,
			ScheduledAt: time.Now().Add(-1 * time.Hour),
		}
		assert.True(t, schedule.IsExpired())
	})

	t.Run("IsExpired returns false for future scheduled time", func(t *testing.T) {
		schedule := &subscription.SubscriptionSchedule{
			ScheduledAt: time.Now().Add(24 * time.Hour),
		}
		assert.False(t, schedule.IsExpired())
	})

	t.Run("DaysUntilExecution calculates correctly", func(t *testing.T) {
		schedule := &subscription.SubscriptionSchedule{
			Status:      types.ScheduleStatusPending,
			ScheduledAt: time.Now().Add(48 * time.Hour),
		}
		days := schedule.DaysUntilExecution()
		assert.True(t, days >= 1 && days <= 2) // Account for rounding
	})

	t.Run("DaysUntilExecution returns 0 for non-pending", func(t *testing.T) {
		now := time.Now()
		schedule := &subscription.SubscriptionSchedule{
			Status:      types.ScheduleStatusExecuted,
			ScheduledAt: time.Now().Add(48 * time.Hour),
			ExecutedAt:  &now,
		}
		assert.Equal(t, 0, schedule.DaysUntilExecution())
	})
}

// TestPlanChangeConfiguration_JSONB tests marshaling and unmarshaling
func TestPlanChangeConfiguration_JSONB(t *testing.T) {
	t.Run("SetPlanChangeConfig and GetPlanChangeConfig roundtrip", func(t *testing.T) {
		schedule := &subscription.SubscriptionSchedule{
			ID:           "schd_test",
			ScheduleType: types.SubscriptionScheduleChangeTypePlanChange,
		}

		config := &subscription.PlanChangeConfiguration{
			TargetPlanID:       "plan_123",
			ProrationBehavior:  types.ProrationBehaviorCreateProrations,
			BillingCadence:     "RECURRING",
			BillingPeriod:      "MONTHLY",
			BillingPeriodCount: 1,
			BillingCycle:       "anniversary",
			ChangeMetadata: map[string]string{
				"reason": "customer_request",
			},
		}

		err := schedule.SetPlanChangeConfig(config)
		assert.NoError(t, err)
		assert.NotNil(t, schedule.Configuration)

		retrieved, err := schedule.GetPlanChangeConfig()
		assert.NoError(t, err)
		assert.Equal(t, config.TargetPlanID, retrieved.TargetPlanID)
		assert.Equal(t, config.ProrationBehavior, retrieved.ProrationBehavior)
		assert.Equal(t, config.ChangeMetadata["reason"], retrieved.ChangeMetadata["reason"])
	})

	t.Run("GetPlanChangeConfig fails for wrong schedule type", func(t *testing.T) {
		schedule := &subscription.SubscriptionSchedule{
			ID:           "schd_test",
			ScheduleType: "addon_change", // Wrong type
		}

		_, err := schedule.GetPlanChangeConfig()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid schedule type for this operation")
	})
}

// TestPlanChangeResult_JSONB tests execution result marshaling
func TestPlanChangeResult_JSONB(t *testing.T) {
	t.Run("SetPlanChangeResult and GetPlanChangeResult roundtrip", func(t *testing.T) {
		schedule := &subscription.SubscriptionSchedule{
			ID:           "schd_test",
			ScheduleType: types.SubscriptionScheduleChangeTypePlanChange,
		}

		result := &subscription.PlanChangeResult{
			OldSubscriptionID: "sub_old",
			NewSubscriptionID: "sub_new",
			ChangeType:        "upgrade",
			EffectiveDate:     time.Now(),
		}

		err := schedule.SetPlanChangeResult(result)
		assert.NoError(t, err)
		assert.NotNil(t, schedule.ExecutionResult)

		retrieved, err := schedule.GetPlanChangeResult()
		assert.NoError(t, err)
		assert.Equal(t, result.OldSubscriptionID, retrieved.OldSubscriptionID)
		assert.Equal(t, result.NewSubscriptionID, retrieved.NewSubscriptionID)
		assert.Equal(t, result.ChangeType, retrieved.ChangeType)
	})

	t.Run("GetPlanChangeResult returns nil for no result", func(t *testing.T) {
		schedule := &subscription.SubscriptionSchedule{
			ID:              "schd_test",
			ScheduleType:    types.SubscriptionScheduleChangeTypePlanChange,
			ExecutionResult: nil,
		}

		result, err := schedule.GetPlanChangeResult()
		assert.NoError(t, err)
		assert.Nil(t, result)
	})
}

// TestScheduleStatusEnum tests the ScheduleStatus enum
func TestScheduleStatusEnum(t *testing.T) {
	statuses := []types.ScheduleStatus{
		types.ScheduleStatusPending,
		types.ScheduleStatusExecuting,
		types.ScheduleStatusExecuted,
		types.ScheduleStatusCancelled,
		types.ScheduleStatusFailed,
	}

	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			assert.NotEmpty(t, string(status))
		})
	}
}

// TestSubscriptionScheduleChangeType tests the change type enum
func TestSubscriptionScheduleChangeType(t *testing.T) {
	changeType := types.SubscriptionScheduleChangeTypePlanChange
	assert.Equal(t, "plan_change", string(changeType))
}

// TestScheduleConfiguration_JSONMarshaling ensures config can be marshaled
func TestScheduleConfiguration_JSONMarshaling(t *testing.T) {
	config := &subscription.PlanChangeConfiguration{
		TargetPlanID:       "plan_abc",
		ProrationBehavior:  types.ProrationBehaviorCreateProrations,
		BillingCadence:     "RECURRING",
		BillingPeriod:      "MONTHLY",
		BillingPeriodCount: 1,
		BillingCycle:       "anniversary",
	}

	data, err := json.Marshal(config)
	assert.NoError(t, err)
	assert.NotNil(t, data)

	var unmarshaled subscription.PlanChangeConfiguration
	err = json.Unmarshal(data, &unmarshaled)
	assert.NoError(t, err)
	assert.Equal(t, config.TargetPlanID, unmarshaled.TargetPlanID)
	assert.Equal(t, config.ProrationBehavior, unmarshaled.ProrationBehavior)
}
