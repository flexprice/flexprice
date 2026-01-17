package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"go.uber.org/zap"
)

// SubscriptionScheduleService handles subscription schedule operations
type SubscriptionScheduleService interface {
	// SchedulePlanChange schedules a plan change at period end
	SchedulePlanChange(ctx context.Context, subscriptionID string, config *subscription.PlanChangeConfiguration) (*subscription.SubscriptionSchedule, error)

	// Cancel cancels a pending schedule
	Cancel(ctx context.Context, scheduleID string) error

	// CancelBySubscriptionAndType cancels a pending schedule by subscription ID and schedule type
	CancelBySubscriptionAndType(ctx context.Context, subscriptionID string, scheduleType types.SubscriptionScheduleChangeType) error

	// CancelPendingForSubscription cancels all pending schedules for a subscription
	CancelPendingForSubscription(ctx context.Context, subscriptionID string) error

	// Get retrieves a schedule by ID
	Get(ctx context.Context, scheduleID string) (*subscription.SubscriptionSchedule, error)

	// GetBySubscriptionID retrieves all schedules for a subscription
	GetBySubscriptionID(ctx context.Context, subscriptionID string) ([]*subscription.SubscriptionSchedule, error)

	// GetPendingBySubscriptionAndType retrieves a pending schedule by subscription ID and type
	GetPendingBySubscriptionAndType(ctx context.Context, subscriptionID string, scheduleType types.SubscriptionScheduleChangeType) (*subscription.SubscriptionSchedule, error)

	// List retrieves schedules based on filter
	List(ctx context.Context, filter *types.SubscriptionScheduleFilter) ([]*subscription.SubscriptionSchedule, error)

	// ExecuteSchedule executes a scheduled change (called by Temporal worker)
	ExecuteSchedule(ctx context.Context, scheduleID string) error

	// MarkAsExecuting updates schedule status to executing (called by Temporal worker)
	MarkAsExecuting(ctx context.Context, scheduleID string) error

	// MarkAsExecuted updates schedule status after successful execution
	MarkAsExecuted(ctx context.Context, scheduleID string, result interface{}) error

	// MarkAsFailed updates schedule status after failed execution
	MarkAsFailed(ctx context.Context, scheduleID string, errorMsg string) error
}

type subscriptionScheduleService struct {
	ServiceParams
	changeService SubscriptionChangeService
}

// NewSubscriptionScheduleService creates a new subscription schedule service
func NewSubscriptionScheduleService(
	params ServiceParams,
	changeService SubscriptionChangeService,
) SubscriptionScheduleService {
	return &subscriptionScheduleService{
		ServiceParams: params,
		changeService: changeService,
	}
}

// SchedulePlanChange schedules a plan change at period end
func (s *subscriptionScheduleService) SchedulePlanChange(
	ctx context.Context,
	subscriptionID string,
	config *subscription.PlanChangeConfiguration,
) (*subscription.SubscriptionSchedule, error) {
	logger := s.Logger.With(
		zap.String("subscription_id", subscriptionID),
		zap.String("target_plan_id", config.TargetPlanID),
	)

	// Get subscription to calculate period end
	sub, err := s.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get subscription: %w", err)
	}

	// Validate subscription is active
	if sub.SubscriptionStatus != types.SubscriptionStatusActive {
		return nil, fmt.Errorf("subscription must be active to schedule changes")
	}

	// Check for existing pending schedule
	existing, err := s.SubScheduleRepo.GetPendingBySubscriptionAndType(
		ctx,
		subscriptionID,
		types.SubscriptionScheduleChangeTypePlanChange,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to check existing schedules: %w", err)
	}
	if existing != nil {
		return nil, fmt.Errorf("a plan change is already scheduled for this subscription")
	}

	// Create schedule
	schedule := &subscription.SubscriptionSchedule{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_SCHEDULE),
		SubscriptionID: subscriptionID,
		ScheduleType:   types.SubscriptionScheduleChangeTypePlanChange,
		ScheduledAt:    sub.CurrentPeriodEnd,
		Status:         types.ScheduleStatusPending,
		TenantID:       sub.TenantID,
		EnvironmentID:  sub.EnvironmentID,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		CreatedBy:      types.GetUserID(ctx),
		UpdatedBy:      types.GetUserID(ctx),
		StatusColumn:   types.StatusPublished,
	}

	// Set configuration
	if err := schedule.SetPlanChangeConfig(config); err != nil {
		return nil, fmt.Errorf("failed to set configuration: %w", err)
	}

	// Save to database
	if err := s.SubScheduleRepo.Create(ctx, schedule); err != nil {
		return nil, fmt.Errorf("failed to create schedule: %w", err)
	}

	logger.Info("plan change scheduled in database",
		zap.String("schedule_id", schedule.ID),
		zap.Time("scheduled_at", schedule.ScheduledAt),
	)

	return schedule, nil
}

// Cancel cancels a pending schedule
func (s *subscriptionScheduleService) Cancel(ctx context.Context, scheduleID string) error {
	// Execute in a transaction to ensure atomicity
	return s.DB.WithTx(ctx, func(txCtx context.Context) error {
		schedule, err := s.SubScheduleRepo.Get(txCtx, scheduleID)
		if err != nil {
			return fmt.Errorf("failed to get schedule: %w", err)
		}

		if !schedule.CanBeCancelled() {
			return fmt.Errorf("schedule cannot be cancelled (status: %s)", schedule.Status)
		}

		// Restore subscription state based on schedule type
		if err := s.restoreSubscriptionState(txCtx, schedule); err != nil {
			s.Logger.Error("failed to restore subscription state",
				zap.String("schedule_id", scheduleID),
				zap.Error(err),
			)
			return fmt.Errorf("failed to restore subscription state: %w", err)
		}

		now := time.Now()
		schedule.Status = types.ScheduleStatusCancelled
		schedule.CancelledAt = &now
		schedule.UpdatedAt = now
		schedule.UpdatedBy = types.GetUserID(txCtx)

		if err := s.SubScheduleRepo.Update(txCtx, schedule); err != nil {
			return fmt.Errorf("failed to cancel schedule: %w", err)
		}

		s.Logger.Info("schedule cancelled in database",
			zap.String("schedule_id", scheduleID),
			zap.String("subscription_id", schedule.SubscriptionID),
			zap.String("schedule_type", string(schedule.ScheduleType)),
		)

		return nil
	})
}

// CancelBySubscriptionAndType cancels a pending schedule by subscription ID and schedule type
func (s *subscriptionScheduleService) CancelBySubscriptionAndType(
	ctx context.Context,
	subscriptionID string,
	scheduleType types.SubscriptionScheduleChangeType,
) error {
	// Get the pending schedule for this subscription and type
	schedule, err := s.SubScheduleRepo.GetPendingBySubscriptionAndType(ctx, subscriptionID, scheduleType)
	if err != nil {
		return fmt.Errorf("failed to get pending schedule: %w", err)
	}

	if schedule == nil {
		return fmt.Errorf("no pending %s schedule found for subscription %s", scheduleType, subscriptionID)
	}

	// Use the existing Cancel method
	return s.Cancel(ctx, schedule.ID)
}

// CancelPendingForSubscription cancels all pending schedules for a subscription
func (s *subscriptionScheduleService) CancelPendingForSubscription(ctx context.Context, subscriptionID string) error {
	schedules, err := s.SubScheduleRepo.GetBySubscriptionID(ctx, subscriptionID)
	if err != nil {
		return fmt.Errorf("failed to get schedules: %w", err)
	}

	for _, schedule := range schedules {
		if schedule.CanBeCancelled() {
			if err := s.Cancel(ctx, schedule.ID); err != nil {
				s.Logger.Warn("failed to cancel schedule",
					zap.String("schedule_id", schedule.ID),
					zap.Error(err),
				)
			}
		}
	}

	return nil
}

// Get retrieves a schedule by ID
func (s *subscriptionScheduleService) Get(ctx context.Context, scheduleID string) (*subscription.SubscriptionSchedule, error) {
	return s.SubScheduleRepo.Get(ctx, scheduleID)
}

// GetBySubscriptionID retrieves all schedules for a subscription
func (s *subscriptionScheduleService) GetBySubscriptionID(ctx context.Context, subscriptionID string) ([]*subscription.SubscriptionSchedule, error) {
	return s.SubScheduleRepo.GetBySubscriptionID(ctx, subscriptionID)
}

// GetPendingBySubscriptionAndType retrieves a pending schedule by subscription ID and type
func (s *subscriptionScheduleService) GetPendingBySubscriptionAndType(
	ctx context.Context,
	subscriptionID string,
	scheduleType types.SubscriptionScheduleChangeType,
) (*subscription.SubscriptionSchedule, error) {
	return s.SubScheduleRepo.GetPendingBySubscriptionAndType(ctx, subscriptionID, scheduleType)
}

// List retrieves schedules based on filter
func (s *subscriptionScheduleService) List(ctx context.Context, filter *types.SubscriptionScheduleFilter) ([]*subscription.SubscriptionSchedule, error) {
	return s.SubScheduleRepo.List(ctx, filter)
}

// ExecuteSchedule executes a scheduled change (called by Temporal worker)
func (s *subscriptionScheduleService) ExecuteSchedule(ctx context.Context, scheduleID string) error {
	// Mark as executing
	if err := s.MarkAsExecuting(ctx, scheduleID); err != nil {
		return err
	}

	schedule, err := s.SubScheduleRepo.Get(ctx, scheduleID)
	if err != nil {
		return fmt.Errorf("failed to get schedule: %w", err)
	}

	// Validate it's still executing
	if schedule.Status != types.ScheduleStatusExecuting {
		return fmt.Errorf("schedule is not executing (status: %s)", schedule.Status)
	}

	// Execute based on type
	var executionError error
	var result interface{}

	switch schedule.ScheduleType {
	case types.SubscriptionScheduleChangeTypePlanChange:
		result, executionError = s.executePlanChange(ctx, schedule)
	default:
		executionError = fmt.Errorf("unsupported schedule type: %s", schedule.ScheduleType)
	}

	// Update status based on result
	if executionError != nil {
		if err := s.MarkAsFailed(ctx, scheduleID, executionError.Error()); err != nil {
			return fmt.Errorf("failed to mark as failed: %w (original error: %v)", err, executionError)
		}
		return executionError
	}

	// Mark as executed
	if err := s.MarkAsExecuted(ctx, scheduleID, result); err != nil {
		return fmt.Errorf("failed to mark as executed: %w", err)
	}

	return nil
}

// executePlanChange executes a plan change schedule
func (s *subscriptionScheduleService) executePlanChange(
	ctx context.Context,
	schedule *subscription.SubscriptionSchedule,
) (*subscription.PlanChangeResult, error) {
	config, err := schedule.GetPlanChangeConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to parse plan change configuration: %w", err)
	}

	// Get current subscription
	sub, err := s.SubRepo.Get(ctx, schedule.SubscriptionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get subscription: %w", err)
	}

	// Validate subscription state
	if sub.SubscriptionStatus != types.SubscriptionStatusActive {
		return nil, fmt.Errorf("subscription is not active (status: %s)", sub.SubscriptionStatus)
	}

	// Block plan changes if subscription is cancelled or scheduled for cancellation
	// Check all cancellation indicators:
	// - CancelledAt: subscription is already cancelled
	// - CancelAtPeriodEnd: cancellation scheduled for period end
	// - CancelAt: cancellation scheduled for a specific date
	if sub.CancelAtPeriodEnd || sub.CancelledAt != nil || sub.CancelAt != nil {
		return nil, fmt.Errorf("subscription is cancelled or scheduled for cancellation")
	}

	if sub.PlanID == config.TargetPlanID {
		return nil, fmt.Errorf("subscription is already on target plan %s", config.TargetPlanID)
	}

	s.Logger.Info("executing plan change",
		zap.String("schedule_id", schedule.ID),
		zap.String("subscription_id", schedule.SubscriptionID),
		zap.String("from_plan", sub.PlanID),
		zap.String("to_plan", config.TargetPlanID),
	)

	// Build change request from configuration
	changeRequest := dto.SubscriptionChangeRequest{
		TargetPlanID:       config.TargetPlanID,
		ProrationBehavior:  config.ProrationBehavior,
		BillingCadence:     config.BillingCadence,
		BillingPeriod:      config.BillingPeriod,
		BillingPeriodCount: config.BillingPeriodCount,
		BillingCycle:       config.BillingCycle,
		Metadata:           config.ChangeMetadata,
	}

	// Execute the change using the injected change service
	changeResponse, err := s.changeService.ExecuteSubscriptionChangeInternal(ctx, schedule.SubscriptionID, changeRequest)
	if err != nil {
		s.Logger.Error("failed to execute subscription change",
			zap.String("schedule_id", schedule.ID),
			zap.String("subscription_id", schedule.SubscriptionID),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to execute subscription change: %w", err)
	}

	// Build result
	result := &subscription.PlanChangeResult{
		OldSubscriptionID: schedule.SubscriptionID,
		NewSubscriptionID: changeResponse.NewSubscription.ID,
		ChangeType:        string(changeResponse.ChangeType),
		EffectiveDate:     time.Now(),
	}

	s.Logger.Info("plan change executed successfully",
		zap.String("schedule_id", schedule.ID),
		zap.String("subscription_id", schedule.SubscriptionID),
	)

	return result, nil
}

// MarkAsExecuting updates schedule status to executing (called by Temporal worker)
func (s *subscriptionScheduleService) MarkAsExecuting(ctx context.Context, scheduleID string) error {
	schedule, err := s.SubScheduleRepo.Get(ctx, scheduleID)
	if err != nil {
		return fmt.Errorf("failed to get schedule: %w", err)
	}

	if schedule.Status != types.ScheduleStatusPending {
		return fmt.Errorf("schedule is not pending (status: %s)", schedule.Status)
	}

	schedule.Status = types.ScheduleStatusExecuting
	schedule.UpdatedAt = time.Now()

	return s.SubScheduleRepo.Update(ctx, schedule)
}

// MarkAsExecuted updates schedule status after successful execution
func (s *subscriptionScheduleService) MarkAsExecuted(ctx context.Context, scheduleID string, result interface{}) error {
	schedule, err := s.SubScheduleRepo.Get(ctx, scheduleID)
	if err != nil {
		return fmt.Errorf("failed to get schedule: %w", err)
	}

	now := time.Now()
	schedule.Status = types.ScheduleStatusExecuted
	schedule.ExecutedAt = &now
	schedule.UpdatedAt = now

	// Store result based on type
	if schedule.ScheduleType == types.SubscriptionScheduleChangeTypePlanChange {
		if planResult, ok := result.(*subscription.PlanChangeResult); ok {
			if err := schedule.SetPlanChangeResult(planResult); err != nil {
				s.Logger.Warn("failed to store execution result", zap.Error(err))
			}
		}
	}

	// Update database first
	if err := s.SubScheduleRepo.Update(ctx, schedule); err != nil {
		return err
	}

	return nil
}

// MarkAsFailed updates schedule status after failed execution
func (s *subscriptionScheduleService) MarkAsFailed(ctx context.Context, scheduleID string, errorMsg string) error {
	schedule, err := s.SubScheduleRepo.Get(ctx, scheduleID)
	if err != nil {
		return fmt.Errorf("failed to get schedule: %w", err)
	}

	now := time.Now()
	schedule.Status = types.ScheduleStatusFailed
	schedule.ExecutedAt = &now
	schedule.ErrorMessage = &errorMsg
	schedule.UpdatedAt = now

	// Update database first
	if err := s.SubScheduleRepo.Update(ctx, schedule); err != nil {
		return err
	}

	return nil
}

// restoreSubscriptionState restores subscription to its pre-schedule state
func (s *subscriptionScheduleService) restoreSubscriptionState(
	ctx context.Context,
	schedule *subscription.SubscriptionSchedule,
) error {
	switch schedule.ScheduleType {
	case types.SubscriptionScheduleChangeTypePlanChange:
		// For plan change: just cancel schedule, subscription remains unchanged
		s.Logger.Info("plan change schedule cancelled, no state restoration needed",
			zap.String("schedule_id", schedule.ID),
		)
		return nil

	case types.SubscriptionScheduleChangeTypeCancellation:
		// For cancellation: restore subscription fields that were changed
		return s.restoreCancellationState(ctx, schedule)

	default:
		// Other types: no restoration needed
		return nil
	}
}

// restoreCancellationState restores subscription state when a cancellation schedule is cancelled
func (s *subscriptionScheduleService) restoreCancellationState(
	ctx context.Context,
	schedule *subscription.SubscriptionSchedule,
) error {
	// Parse the cancellation configuration to get original state
	config, err := schedule.GetCancellationConfig()
	if err != nil {
		return fmt.Errorf("failed to parse cancellation configuration: %w", err)
	}

	// Enrich context with tenant and environment from the schedule
	ctx = types.SetTenantID(ctx, schedule.TenantID)
	ctx = types.SetEnvironmentID(ctx, schedule.EnvironmentID)

	// Get the subscription
	sub, err := s.SubRepo.Get(ctx, schedule.SubscriptionID)
	if err != nil {
		return fmt.Errorf("failed to get subscription: %w", err)
	}

	s.Logger.Info("restoring subscription state after cancellation schedule cancellation",
		zap.String("schedule_id", schedule.ID),
		zap.String("subscription_id", sub.ID),
		zap.Bool("current_cancel_at_period_end", sub.CancelAtPeriodEnd),
	)

	// Restore the original state
	sub.CancelAtPeriodEnd = config.OriginalCancelAtPeriodEnd
	sub.CancelAt = config.OriginalCancelAt
	sub.EndDate = config.OriginalEndDate

	// Update the subscription
	if err := s.SubRepo.Update(ctx, sub); err != nil {
		return fmt.Errorf("failed to restore subscription state: %w", err)
	}

	s.Logger.Info("subscription state restored successfully",
		zap.String("schedule_id", schedule.ID),
		zap.String("subscription_id", sub.ID),
		zap.Bool("restored_cancel_at_period_end", sub.CancelAtPeriodEnd),
	)

	return nil
}
