package ent

import (
	"context"
	"time"

	eent "github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/subscriptionschedule"
	"github.com/flexprice/flexprice/ent/subscriptionschedulephase"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

// SubscriptionScheduleRepository implements the repository for subscription schedules
type SubscriptionScheduleRepository struct {
	Client    postgres.IClient
	Logger    *logger.Logger
	Cache     cache.Cache
	queryOpts SubscriptionScheduleQueryOptions
}

// NewSubscriptionScheduleRepository creates a new subscription schedule repository
func NewSubscriptionScheduleRepository(client postgres.IClient, logger *logger.Logger, cache cache.Cache) subscription.SubscriptionScheduleRepository {
	return &SubscriptionScheduleRepository{
		Client:    client,
		Logger:    logger,
		Cache:     cache,
		queryOpts: SubscriptionScheduleQueryOptions{},
	}
}

// Create creates a new subscription schedule
func (r *SubscriptionScheduleRepository) Create(ctx context.Context, schedule *subscription.SubscriptionSchedule) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_schedule", "create", map[string]interface{}{
		"subscription_id": schedule.SubscriptionID,
		"tenant_id":       schedule.TenantID,
	})
	defer FinishSpan(span)

	client := r.Client.Querier(ctx)

	r.Logger.Debugw("creating subscription schedule",
		"subscription_id", schedule.SubscriptionID,
		"tenant_id", schedule.TenantID,
	)

	_, err := client.SubscriptionSchedule.Create().
		SetID(schedule.ID).
		SetSubscriptionID(schedule.SubscriptionID).
		SetScheduleStatus(schedule.ScheduleStatus).
		SetCurrentPhaseIndex(schedule.CurrentPhaseIndex).
		SetEndBehavior(schedule.EndBehavior).
		SetStartDate(schedule.StartDate).
		SetMetadata(schedule.Metadata).
		SetEnvironmentID(schedule.EnvironmentID).
		SetTenantID(schedule.TenantID).
		SetCreatedBy(types.GetUserID(ctx)).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to create subscription schedule").
			WithReportableDetails(map[string]interface{}{
				"schedule_id":     schedule.ID,
				"subscription_id": schedule.SubscriptionID,
			}).
			Mark(ierr.ErrDatabase)
	}

	return nil
}

// Get retrieves a subscription schedule by ID
func (r *SubscriptionScheduleRepository) Get(ctx context.Context, id string) (*subscription.SubscriptionSchedule, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_schedule", "get", map[string]interface{}{
		"schedule_id": id,
		"tenant_id":   types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	client := r.Client.Querier(ctx)

	r.Logger.Debugw("getting subscription schedule",
		"schedule_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	schedule, err := client.SubscriptionSchedule.
		Query().
		Where(
			subscriptionschedule.ID(id),
			subscriptionschedule.TenantID(types.GetTenantID(ctx)),
			subscriptionschedule.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		First(ctx)

	if err != nil {
		SetSpanError(span, err)
		if eent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Subscription schedule not found").
				WithReportableDetails(map[string]interface{}{
					"schedule_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get subscription schedule").
			WithReportableDetails(map[string]interface{}{
				"schedule_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Get all phases for the schedule
	phases, err := r.ListPhases(ctx, id)
	if err != nil {
		SetSpanError(span, err)
		return nil, err
	}

	result := subscription.GetSubscriptionScheduleFromEnt(schedule)
	result.Phases = phases
	return result, nil
}

// GetBySubscriptionID gets a schedule for a subscription if it exists
func (r *SubscriptionScheduleRepository) GetBySubscriptionID(ctx context.Context, subscriptionID string) (*subscription.SubscriptionSchedule, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_schedule", "get_by_subscription_id", map[string]interface{}{
		"subscription_id": subscriptionID,
		"tenant_id":       types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	client := r.Client.Querier(ctx)

	r.Logger.Debugw("getting subscription schedule by subscription ID",
		"subscription_id", subscriptionID,
		"tenant_id", types.GetTenantID(ctx),
	)

	schedule, err := client.SubscriptionSchedule.
		Query().
		Where(
			subscriptionschedule.SubscriptionID(subscriptionID),
			subscriptionschedule.TenantID(types.GetTenantID(ctx)),
			subscriptionschedule.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		First(ctx)

	if err != nil {
		SetSpanError(span, err)
		if eent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("No schedule found for subscription").
				WithReportableDetails(map[string]interface{}{
					"subscription_id": subscriptionID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get subscription schedule").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": subscriptionID,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Get all phases for the schedule
	phases, err := r.ListPhases(ctx, schedule.ID)
	if err != nil {
		SetSpanError(span, err)
		return nil, err
	}

	result := subscription.GetSubscriptionScheduleFromEnt(schedule)
	result.Phases = phases
	return result, nil
}

// Update updates a subscription schedule
func (r *SubscriptionScheduleRepository) Update(ctx context.Context, schedule *subscription.SubscriptionSchedule) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_schedule", "update", map[string]interface{}{
		"schedule_id":     schedule.ID,
		"subscription_id": schedule.SubscriptionID,
		"tenant_id":       schedule.TenantID,
	})
	defer FinishSpan(span)

	client := r.Client.Querier(ctx)

	r.Logger.Debugw("updating subscription schedule",
		"schedule_id", schedule.ID,
		"tenant_id", schedule.TenantID,
	)

	builder := client.SubscriptionSchedule.UpdateOneID(schedule.ID).
		Where(
			subscriptionschedule.TenantID(schedule.TenantID),
			subscriptionschedule.EnvironmentID(schedule.EnvironmentID),
		).
		SetScheduleStatus(schedule.ScheduleStatus).
		SetCurrentPhaseIndex(schedule.CurrentPhaseIndex).
		SetEndBehavior(schedule.EndBehavior).
		SetStartDate(schedule.StartDate).
		SetMetadata(schedule.Metadata).
		SetUpdatedBy(types.GetUserID(ctx)).
		SetUpdatedAt(time.Now().UTC())

	_, err := builder.Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		if eent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Subscription schedule not found for update").
				WithReportableDetails(map[string]interface{}{
					"schedule_id": schedule.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update subscription schedule").
			WithReportableDetails(map[string]interface{}{
				"schedule_id": schedule.ID,
			}).
			Mark(ierr.ErrDatabase)
	}
	return nil
}

// Delete deletes a subscription schedule
func (r *SubscriptionScheduleRepository) Delete(ctx context.Context, id string) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_schedule", "delete", map[string]interface{}{
		"schedule_id": id,
		"tenant_id":   types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	client := r.Client.Querier(ctx)

	r.Logger.Debugw("deleting subscription schedule",
		"schedule_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	// Proceed with deletion
	_, err := client.SubscriptionSchedule.
		Delete().
		Where(
			subscriptionschedule.ID(id),
			subscriptionschedule.TenantID(types.GetTenantID(ctx)),
			subscriptionschedule.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Exec(ctx)

	if err != nil {
		SetSpanError(span, err)
		if eent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Subscription schedule not found for deletion").
				WithReportableDetails(map[string]interface{}{
					"schedule_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete subscription schedule").
			WithReportableDetails(map[string]interface{}{
				"schedule_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	return nil
}

// ListPhases lists all phases for a subscription schedule
func (r *SubscriptionScheduleRepository) ListPhases(ctx context.Context, scheduleID string) ([]*subscription.SchedulePhase, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_schedule_phase", "list", map[string]interface{}{
		"schedule_id": scheduleID,
		"tenant_id":   types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	client := r.Client.Querier(ctx)

	r.Logger.Debugw("listing subscription schedule phases",
		"schedule_id", scheduleID,
		"tenant_id", types.GetTenantID(ctx),
	)

	phases, err := client.SubscriptionSchedulePhase.
		Query().
		Where(
			subscriptionschedulephase.ScheduleID(scheduleID),
			subscriptionschedulephase.TenantID(types.GetTenantID(ctx)),
			subscriptionschedulephase.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Order(eent.Asc(subscriptionschedulephase.FieldPhaseIndex)).
		All(ctx)

	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list subscription schedule phases").
			WithReportableDetails(map[string]interface{}{
				"schedule_id": scheduleID,
			}).
			Mark(ierr.ErrDatabase)
	}

	result := subscription.GetSchedulePhasesFromEnt(phases)
	return result, nil
}

// CreatePhase creates a new subscription schedule phase
func (r *SubscriptionScheduleRepository) CreatePhase(ctx context.Context, phase *subscription.SchedulePhase) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_schedule_phase", "create", map[string]interface{}{
		"phase_id":    phase.ID,
		"schedule_id": phase.ScheduleID,
		"tenant_id":   phase.TenantID,
	})
	defer FinishSpan(span)

	client := r.Client.Querier(ctx)

	r.Logger.Debugw("creating subscription schedule phase",
		"phase_id", phase.ID,
		"schedule_id", phase.ScheduleID,
		"tenant_id", phase.TenantID,
	)

	_, err := client.SubscriptionSchedulePhase.Create().
		SetID(phase.ID).
		SetScheduleID(phase.ScheduleID).
		SetPhaseIndex(phase.PhaseIndex).
		SetStartDate(phase.StartDate).
		SetNillableEndDate(phase.EndDate).
		SetNillableCommitmentAmount(phase.CommitmentAmount).
		SetNillableOverageFactor(phase.OverageFactor).
		SetLineItems(phase.LineItems).
		SetCreditGrants(phase.CreditGrants).
		SetMetadata(phase.Metadata).
		SetEnvironmentID(phase.EnvironmentID).
		SetTenantID(phase.TenantID).
		SetCreatedBy(types.GetUserID(ctx)).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to create subscription schedule phase").
			WithReportableDetails(map[string]interface{}{
				"phase_id":    phase.ID,
				"schedule_id": phase.ScheduleID,
			}).
			Mark(ierr.ErrDatabase)
	}
	return nil
}

// GetPhase gets a subscription schedule phase by ID
func (r *SubscriptionScheduleRepository) GetPhase(ctx context.Context, id string) (*subscription.SchedulePhase, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_schedule_phase", "get", map[string]interface{}{
		"phase_id":  id,
		"tenant_id": types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	client := r.Client.Querier(ctx)

	r.Logger.Debugw("getting subscription schedule phase",
		"phase_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	phase, err := client.SubscriptionSchedulePhase.
		Query().
		Where(
			subscriptionschedulephase.ID(id),
			subscriptionschedulephase.TenantID(types.GetTenantID(ctx)),
			subscriptionschedulephase.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		First(ctx)

	if err != nil {
		SetSpanError(span, err)
		if eent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Subscription schedule phase not found").
				WithReportableDetails(map[string]interface{}{
					"phase_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get subscription schedule phase").
			WithReportableDetails(map[string]interface{}{
				"phase_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	return subscription.GetSchedulePhaseFromEnt(phase), nil
}

// UpdatePhase updates a subscription schedule phase
func (r *SubscriptionScheduleRepository) UpdatePhase(ctx context.Context, phase *subscription.SchedulePhase) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_schedule_phase", "update", map[string]interface{}{
		"phase_id":    phase.ID,
		"schedule_id": phase.ScheduleID,
		"tenant_id":   phase.TenantID,
	})
	defer FinishSpan(span)

	client := r.Client.Querier(ctx)

	r.Logger.Debugw("updating subscription schedule phase",
		"phase_id", phase.ID,
		"schedule_id", phase.ScheduleID,
		"tenant_id", phase.TenantID,
	)

	builder := client.SubscriptionSchedulePhase.UpdateOneID(phase.ID).
		Where(
			subscriptionschedulephase.TenantID(phase.TenantID),
			subscriptionschedulephase.EnvironmentID(phase.EnvironmentID),
		).
		SetPhaseIndex(phase.PhaseIndex).
		SetStartDate(phase.StartDate).
		SetNillableEndDate(phase.EndDate).
		SetNillableCommitmentAmount(phase.CommitmentAmount).
		SetNillableOverageFactor(phase.OverageFactor).
		SetLineItems(phase.LineItems).
		SetCreditGrants(phase.CreditGrants).
		SetMetadata(phase.Metadata).
		SetUpdatedBy(types.GetUserID(ctx)).
		SetUpdatedAt(time.Now().UTC())

	_, err := builder.Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		if eent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Subscription schedule phase not found for update").
				WithReportableDetails(map[string]interface{}{
					"phase_id": phase.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update subscription schedule phase").
			WithReportableDetails(map[string]interface{}{
				"phase_id": phase.ID,
			}).
			Mark(ierr.ErrDatabase)
	}
	return nil
}

// DeletePhase deletes a subscription schedule phase
func (r *SubscriptionScheduleRepository) DeletePhase(ctx context.Context, id string) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_schedule_phase", "delete", map[string]interface{}{
		"phase_id":  id,
		"tenant_id": types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	client := r.Client.Querier(ctx)

	r.Logger.Debugw("deleting subscription schedule phase",
		"phase_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	_, err := client.SubscriptionSchedulePhase.
		Delete().
		Where(
			subscriptionschedulephase.ID(id),
			subscriptionschedulephase.TenantID(types.GetTenantID(ctx)),
			subscriptionschedulephase.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Exec(ctx)

	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to delete subscription schedule phase").
			WithReportableDetails(map[string]interface{}{
				"phase_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}
	return nil
}

// CreateWithPhases creates a schedule with all its phases in one transaction
func (r *SubscriptionScheduleRepository) CreateWithPhases(ctx context.Context, schedule *subscription.SubscriptionSchedule, phases []*subscription.SchedulePhase) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_schedule", "create_with_phases", map[string]interface{}{
		"schedule_id":     schedule.ID,
		"subscription_id": schedule.SubscriptionID,
		"tenant_id":       schedule.TenantID,
		"phase_count":     len(phases),
	})
	defer FinishSpan(span)

	r.Logger.Debugw("creating subscription schedule with phases",
		"schedule_id", schedule.ID,
		"subscription_id", schedule.SubscriptionID,
		"tenant_id", schedule.TenantID,
		"phase_count", len(phases),
	)

	// Use the transaction functionality from the client
	err := r.Client.WithTx(ctx, func(txCtx context.Context) error {
		txClient := r.Client.Querier(txCtx)

		// Create the schedule
		_, err := txClient.SubscriptionSchedule.Create().
			SetID(schedule.ID).
			SetSubscriptionID(schedule.SubscriptionID).
			SetScheduleStatus(schedule.ScheduleStatus).
			SetCurrentPhaseIndex(schedule.CurrentPhaseIndex).
			SetEndBehavior(schedule.EndBehavior).
			SetStartDate(schedule.StartDate).
			SetMetadata(schedule.Metadata).
			SetEnvironmentID(schedule.EnvironmentID).
			SetTenantID(schedule.TenantID).
			SetCreatedBy(types.GetUserID(txCtx)).
			SetUpdatedBy(types.GetUserID(txCtx)).
			Save(txCtx)

		if err != nil {
			r.Logger.Errorw("failed to create subscription schedule in transaction",
				"error", err,
				"schedule_id", schedule.ID,
				"subscription_id", schedule.SubscriptionID)
			return ierr.WithError(err).
				WithHint("Failed to create subscription schedule in transaction").
				WithReportableDetails(map[string]interface{}{
					"schedule_id":     schedule.ID,
					"subscription_id": schedule.SubscriptionID,
				}).
				Mark(ierr.ErrDatabase)
		}

		// Create all phases
		for _, phase := range phases {
			// Create the phase
			_, err = txClient.SubscriptionSchedulePhase.Create().
				SetID(phase.ID).
				SetScheduleID(phase.ScheduleID).
				SetPhaseIndex(phase.PhaseIndex).
				SetStartDate(phase.StartDate).
				SetNillableEndDate(phase.EndDate).
				SetNillableCommitmentAmount(phase.CommitmentAmount).
				SetNillableOverageFactor(phase.OverageFactor).
				SetLineItems(phase.LineItems).
				SetCreditGrants(phase.CreditGrants).
				SetMetadata(phase.Metadata).
				SetEnvironmentID(phase.EnvironmentID).
				SetTenantID(phase.TenantID).
				SetCreatedBy(types.GetUserID(txCtx)).
				SetUpdatedBy(types.GetUserID(txCtx)).
				Save(txCtx)

			if err != nil {
				r.Logger.Errorw("failed to create phase in transaction",
					"error", err,
					"phase_id", phase.ID,
					"schedule_id", phase.ScheduleID)
				return ierr.WithError(err).
					WithHint("Failed to create phase in transaction").
					WithReportableDetails(map[string]interface{}{
						"phase_id":    phase.ID,
						"schedule_id": phase.ScheduleID,
					}).
					Mark(ierr.ErrDatabase)
			}
		}

		return nil
	})

	if err != nil {
		SetSpanError(span, err)
		return err
	}

	return nil
}

// Query options for subscription schedule

// SubscriptionScheduleQuery type alias for better readability
type SubscriptionScheduleQuery = *eent.SubscriptionScheduleQuery

// SubscriptionScheduleQueryOptions implements BaseQueryOptions for subscription schedule queries
type SubscriptionScheduleQueryOptions struct{}

func (o SubscriptionScheduleQueryOptions) ApplyTenantFilter(ctx context.Context, query SubscriptionScheduleQuery) SubscriptionScheduleQuery {
	return query.Where(subscriptionschedule.TenantID(types.GetTenantID(ctx)))
}

func (o SubscriptionScheduleQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query SubscriptionScheduleQuery) SubscriptionScheduleQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(subscriptionschedule.EnvironmentID(environmentID))
	}
	return query
}
