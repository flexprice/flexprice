package subscription

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// subscriptionScheduleBuilder copies an existing schedule and applies field updates.
type subscriptionScheduleBuilder struct {
	schedule *SubscriptionSchedule
}

// NewSubscriptionScheduleBuilder returns a builder seeded from an existing schedule.
func NewSubscriptionScheduleBuilder(schedule *SubscriptionSchedule) *subscriptionScheduleBuilder {
	if schedule == nil {
		return &subscriptionScheduleBuilder{schedule: &SubscriptionSchedule{}}
	}

	copied := *schedule
	if schedule.Metadata != nil {
		copied.Metadata = make(types.Metadata, len(schedule.Metadata))
		for k, v := range schedule.Metadata {
			copied.Metadata[k] = v
		}
	}

	return &subscriptionScheduleBuilder{schedule: &copied}
}

// NewPendingScheduleBuilder seeds a brand new pending schedule, filling the id,
// audit and tenancy fields from ctx so callers only supply what varies.
func NewPendingScheduleBuilder(
	ctx context.Context,
	sub *Subscription,
	scheduleType types.SubscriptionScheduleChangeType,
	scheduledAt time.Time,
) *subscriptionScheduleBuilder {
	now := time.Now().UTC()
	b := &subscriptionScheduleBuilder{schedule: &SubscriptionSchedule{
		ID:           types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_SCHEDULE),
		ScheduleType: scheduleType,
		ScheduledAt:  scheduledAt,
		Status:       types.ScheduleStatusPending,
		StatusColumn: types.StatusPublished,
		CreatedAt:    now,
		UpdatedAt:    now,
		CreatedBy:    types.GetUserID(ctx),
		UpdatedBy:    types.GetUserID(ctx),
	}}

	if sub != nil {
		b.schedule.SubscriptionID = sub.ID
		b.schedule.TenantID = sub.TenantID
		b.schedule.EnvironmentID = sub.EnvironmentID
	}

	return b
}

func (b *subscriptionScheduleBuilder) WithStatus(status types.ScheduleStatus) *subscriptionScheduleBuilder {
	if b == nil || b.schedule == nil {
		return b
	}
	b.schedule.Status = status
	return b
}

func (b *subscriptionScheduleBuilder) WithExecutedAt(executedAt time.Time) *subscriptionScheduleBuilder {
	if b == nil || b.schedule == nil {
		return b
	}
	b.schedule.ExecutedAt = &executedAt
	return b
}

func (b *subscriptionScheduleBuilder) WithErrorMessage(message string) *subscriptionScheduleBuilder {
	if b == nil || b.schedule == nil {
		return b
	}
	b.schedule.ErrorMessage = &message
	return b
}

func (b *subscriptionScheduleBuilder) WithCancelledAt(cancelledAt time.Time) *subscriptionScheduleBuilder {
	if b == nil || b.schedule == nil {
		return b
	}
	b.schedule.CancelledAt = &cancelledAt
	b.schedule.UpdatedAt = cancelledAt
	return b
}

func (b *subscriptionScheduleBuilder) WithUpdatedBy(userID string) *subscriptionScheduleBuilder {
	if b == nil || b.schedule == nil {
		return b
	}
	b.schedule.UpdatedBy = userID
	return b
}

// WithMetadataEntry sets one key, leaving the rest of the metadata intact.
func (b *subscriptionScheduleBuilder) WithMetadataEntry(key, value string) *subscriptionScheduleBuilder {
	if b == nil || b.schedule == nil {
		return b
	}
	if b.schedule.Metadata == nil {
		b.schedule.Metadata = types.Metadata{}
	}
	b.schedule.Metadata[key] = value
	return b
}

func (b *subscriptionScheduleBuilder) WithMetadata(metadata types.Metadata) *subscriptionScheduleBuilder {
	if b == nil || b.schedule == nil {
		return b
	}
	b.schedule.Metadata = metadata
	return b
}

func (b *subscriptionScheduleBuilder) Build() *SubscriptionSchedule {
	if b == nil {
		return nil
	}
	return b.schedule
}
