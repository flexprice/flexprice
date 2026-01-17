package ent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/subscriptionschedule"
	domainSub "github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type subscriptionScheduleRepository struct {
	client postgres.IClient
	logger *logger.Logger
}

// NewSubscriptionScheduleRepository creates a new subscription schedule repository
func NewSubscriptionScheduleRepository(client postgres.IClient, logger *logger.Logger) domainSub.SubscriptionScheduleRepository {
	return &subscriptionScheduleRepository{
		client: client,
		logger: logger,
	}
}

// Create creates a new subscription schedule
func (r *subscriptionScheduleRepository) Create(ctx context.Context, schedule *domainSub.SubscriptionSchedule) error {
	client := r.client.Writer(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_schedule", "create", map[string]interface{}{
		"schedule_id":     schedule.ID,
		"subscription_id": schedule.SubscriptionID,
		"schedule_type":   schedule.ScheduleType,
	})
	defer FinishSpan(span)

	// Convert JSONB to map
	var configMap map[string]interface{}
	if err := json.Unmarshal(schedule.Configuration, &configMap); err != nil {
		SetSpanError(span, err)
		return fmt.Errorf("failed to parse configuration: %w", err)
	}

	builder := client.SubscriptionSchedule.Create().
		SetID(schedule.ID).
		SetSubscriptionID(schedule.SubscriptionID).
		SetScheduleType(schedule.ScheduleType).
		SetScheduledAt(schedule.ScheduledAt).
		SetStatus(string(schedule.Status)).
		SetConfiguration(configMap).
		SetTenantID(schedule.TenantID).
		SetEnvironmentID(schedule.EnvironmentID).
		SetCreatedBy(schedule.CreatedBy).
		SetUpdatedBy(schedule.UpdatedBy)

	// Set optional fields
	if schedule.ExecutedAt != nil {
		builder.SetExecutedAt(*schedule.ExecutedAt)
	}

	if schedule.ExecutionResult != nil {
		var resultMap map[string]interface{}
		if err := json.Unmarshal(schedule.ExecutionResult, &resultMap); err == nil {
			builder.SetExecutionResult(resultMap)
		}
	}

	if schedule.ErrorMessage != nil {
		builder.SetErrorMessage(*schedule.ErrorMessage)
	}

	if schedule.Metadata != nil {
		builder.SetMetadata(schedule.Metadata)
	}

	_, err := builder.Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return fmt.Errorf("failed to create subscription schedule: %w", err)
	}

	SetSpanSuccess(span)
	return nil
}

// Get retrieves a subscription schedule by ID
func (r *subscriptionScheduleRepository) Get(ctx context.Context, id string) (*domainSub.SubscriptionSchedule, error) {
	client := r.client.Reader(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_schedule", "get", map[string]interface{}{
		"schedule_id": id,
	})
	defer FinishSpan(span)

	entity, err := client.SubscriptionSchedule.
		Query().
		Where(subscriptionschedule.IDEQ(id)).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("subscription schedule not found: %w", err)
		}
		return nil, fmt.Errorf("failed to get subscription schedule: %w", err)
	}

	SetSpanSuccess(span)
	return domainSub.SubscriptionScheduleFromEnt(entity), nil
}

// Update updates an existing subscription schedule
func (r *subscriptionScheduleRepository) Update(ctx context.Context, schedule *domainSub.SubscriptionSchedule) error {
	client := r.client.Writer(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_schedule", "update", map[string]interface{}{
		"schedule_id": schedule.ID,
		"status":      schedule.Status,
	})
	defer FinishSpan(span)

	// Convert JSONB to map
	var configMap map[string]interface{}
	if err := json.Unmarshal(schedule.Configuration, &configMap); err != nil {
		SetSpanError(span, err)
		return fmt.Errorf("failed to parse configuration: %w", err)
	}

	builder := client.SubscriptionSchedule.
		UpdateOneID(schedule.ID).
		SetScheduleType(schedule.ScheduleType).
		SetScheduledAt(schedule.ScheduledAt).
		SetStatus(string(schedule.Status)).
		SetConfiguration(configMap).
		SetUpdatedBy(schedule.UpdatedBy)

	// Set optional fields
	if schedule.ExecutedAt != nil {
		builder.SetExecutedAt(*schedule.ExecutedAt)
	} else {
		builder.ClearExecutedAt()
	}

	if schedule.CancelledAt != nil {
		builder.SetCancelledAt(*schedule.CancelledAt)
	} else {
		builder.ClearCancelledAt()
	}

	if schedule.ExecutionResult != nil {
		var resultMap map[string]interface{}
		if err := json.Unmarshal(schedule.ExecutionResult, &resultMap); err == nil {
			builder.SetExecutionResult(resultMap)
		}
	} else {
		builder.ClearExecutionResult()
	}

	if schedule.ErrorMessage != nil {
		builder.SetErrorMessage(*schedule.ErrorMessage)
	} else {
		builder.ClearErrorMessage()
	}

	if schedule.Metadata != nil {
		builder.SetMetadata(schedule.Metadata)
	}

	_, err := builder.Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return fmt.Errorf("subscription schedule not found: %w", err)
		}
		return fmt.Errorf("failed to update subscription schedule: %w", err)
	}

	SetSpanSuccess(span)
	return nil
}

// Delete soft deletes a subscription schedule by ID
func (r *subscriptionScheduleRepository) Delete(ctx context.Context, id string) error {
	client := r.client.Writer(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_schedule", "delete", map[string]interface{}{
		"schedule_id": id,
	})
	defer FinishSpan(span)

	// Soft delete by updating status to cancelled
	err := client.SubscriptionSchedule.
		UpdateOneID(id).
		SetStatus(string(types.ScheduleStatusCancelled)).
		Exec(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return fmt.Errorf("subscription schedule not found: %w", err)
		}
		return fmt.Errorf("failed to delete subscription schedule: %w", err)
	}

	SetSpanSuccess(span)
	return nil
}

// GetBySubscriptionID retrieves all schedules for a given subscription ID
func (r *subscriptionScheduleRepository) GetBySubscriptionID(ctx context.Context, subscriptionID string) ([]*domainSub.SubscriptionSchedule, error) {
	client := r.client.Reader(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_schedule", "get_by_subscription", map[string]interface{}{
		"subscription_id": subscriptionID,
	})
	defer FinishSpan(span)

	entities, err := client.SubscriptionSchedule.
		Query().
		Where(subscriptionschedule.SubscriptionIDEQ(subscriptionID)).
		Order(ent.Desc(subscriptionschedule.FieldCreatedAt)).
		All(ctx)

	if err != nil {
		SetSpanError(span, err)
		return nil, fmt.Errorf("failed to get subscription schedules: %w", err)
	}

	SetSpanSuccess(span)
	return domainSub.SubscriptionScheduleListFromEnt(entities), nil
}

// GetPendingBySubscriptionAndType retrieves a pending schedule of a specific type for a subscription
func (r *subscriptionScheduleRepository) GetPendingBySubscriptionAndType(
	ctx context.Context,
	subscriptionID string,
	scheduleType types.SubscriptionScheduleChangeType,
) (*domainSub.SubscriptionSchedule, error) {
	client := r.client.Reader(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_schedule", "get_pending", map[string]interface{}{
		"subscription_id": subscriptionID,
		"schedule_type":   scheduleType,
	})
	defer FinishSpan(span)

	entity, err := client.SubscriptionSchedule.
		Query().
		Where(
			subscriptionschedule.SubscriptionIDEQ(subscriptionID),
			subscriptionschedule.ScheduleTypeEQ(scheduleType),
			subscriptionschedule.StatusEQ(string(types.ScheduleStatusPending)),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			SetSpanSuccess(span)
			return nil, nil // No pending schedule found is not an error
		}
		SetSpanError(span, err)
		return nil, fmt.Errorf("failed to get pending subscription schedule: %w", err)
	}

	SetSpanSuccess(span)
	return domainSub.SubscriptionScheduleFromEnt(entity), nil
}

// List retrieves subscription schedules based on a filter
func (r *subscriptionScheduleRepository) List(ctx context.Context, filter *types.SubscriptionScheduleFilter) ([]*domainSub.SubscriptionSchedule, error) {
	client := r.client.Reader(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_schedule", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	query := client.SubscriptionSchedule.Query()

	// Apply filters
	query = r.applyFilters(query, filter)

	// Apply pagination
	if filter.QueryFilter != nil {
		query = query.
			Limit(filter.GetLimit()).
			Offset(filter.GetOffset())
	}

	// Apply ordering
	query = query.Order(ent.Desc(subscriptionschedule.FieldCreatedAt))

	entities, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, fmt.Errorf("failed to list subscription schedules: %w", err)
	}

	SetSpanSuccess(span)
	return domainSub.SubscriptionScheduleListFromEnt(entities), nil
}

// Count returns the count of subscription schedules matching the filter
func (r *subscriptionScheduleRepository) Count(ctx context.Context, filter *types.SubscriptionScheduleFilter) (int, error) {
	client := r.client.Reader(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_schedule", "count", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	query := client.SubscriptionSchedule.Query()

	// Apply filters
	query = r.applyFilters(query, filter)

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, fmt.Errorf("failed to count subscription schedules: %w", err)
	}

	SetSpanSuccess(span)
	return count, nil
}

// applyFilters applies filter conditions to the query
func (r *subscriptionScheduleRepository) applyFilters(
	query *ent.SubscriptionScheduleQuery,
	filter *types.SubscriptionScheduleFilter,
) *ent.SubscriptionScheduleQuery {
	if filter == nil {
		return query
	}

	// Filter by schedule IDs
	if len(filter.ScheduleIDs) > 0 {
		query = query.Where(subscriptionschedule.IDIn(filter.ScheduleIDs...))
	}

	// Filter by subscription IDs
	if len(filter.SubscriptionIDs) > 0 {
		query = query.Where(subscriptionschedule.SubscriptionIDIn(filter.SubscriptionIDs...))
	}

	// Filter by schedule types
	if len(filter.ScheduleType) > 0 {
		query = query.Where(subscriptionschedule.ScheduleTypeIn(filter.ScheduleType...))
	}

	// Filter by schedule statuses
	if len(filter.ScheduleStatus) > 0 {
		statuses := make([]string, len(filter.ScheduleStatus))
		for i, status := range filter.ScheduleStatus {
			statuses[i] = string(status)
		}
		query = query.Where(subscriptionschedule.StatusIn(statuses...))
	}

	// Filter by scheduled_at range
	if filter.ScheduledAtStart != nil {
		query = query.Where(subscriptionschedule.ScheduledAtGTE(*filter.ScheduledAtStart))
	}
	if filter.ScheduledAtEnd != nil {
		query = query.Where(subscriptionschedule.ScheduledAtLTE(*filter.ScheduledAtEnd))
	}

	// Filter pending only
	if filter.PendingOnly {
		query = query.Where(subscriptionschedule.StatusEQ(string(types.ScheduleStatusPending)))
	}

	return query
}
