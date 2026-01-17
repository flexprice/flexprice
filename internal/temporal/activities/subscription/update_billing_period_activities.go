package subscription

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	invoiceModels "github.com/flexprice/flexprice/internal/temporal/models/invoice"
	subscriptionModels "github.com/flexprice/flexprice/internal/temporal/models/subscription"
	temporalService "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type BillingActivities struct {
	subscriptionService service.SubscriptionService
	serviceParams       service.ServiceParams
	logger              *logger.Logger
}

func NewBillingActivities(
	subscriptionService service.SubscriptionService,
	serviceParams service.ServiceParams,
	logger *logger.Logger,
) *BillingActivities {
	return &BillingActivities{
		subscriptionService: subscriptionService,
		serviceParams:       serviceParams,
		logger:              logger,
	}
}

// CheckDraftSubscriptionActivity checks if the subscription is draft
func (s *BillingActivities) CheckDraftSubscriptionActivity(
	ctx context.Context,
	input subscriptionModels.CheckDraftSubscriptionActivityInput,
) (*subscriptionModels.CheckDraftSubscriptionActivityOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	// Set context values
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)
	ctx = types.SetUserID(ctx, input.UserID)

	sub, err := s.serviceParams.SubRepo.Get(ctx, input.SubscriptionID)
	if err != nil {
		return nil, err
	}

	if sub.SubscriptionStatus == types.SubscriptionStatusDraft {
		return &subscriptionModels.CheckDraftSubscriptionActivityOutput{
			IsDraft: true,
		}, nil
	}

	return &subscriptionModels.CheckDraftSubscriptionActivityOutput{
		IsDraft: false,
	}, nil
}

// CalculatePeriodsActivity calculates billing periods from the current period up to the specified time
func (s *BillingActivities) CalculatePeriodsActivity(
	ctx context.Context,
	input subscriptionModels.CalculatePeriodsActivityInput,
) (*subscriptionModels.CalculatePeriodsActivityOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	// Set context values
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)
	ctx = types.SetUserID(ctx, input.UserID)

	subscriptionService := service.NewSubscriptionService(s.serviceParams)

	periods, err := subscriptionService.CalculateBillingPeriods(ctx, input.SubscriptionID)
	if err != nil {
		return nil, err
	}

	output := &subscriptionModels.CalculatePeriodsActivityOutput{
		Periods:       periods,
		ShouldProcess: len(periods) > 1,
	}

	return output, nil
}

// CreateDraftInvoicesActivity creates draft invoices for specific billing periods
// This activity does NOT finalize, sync, or attempt payment - those are handled by ProcessInvoiceWorkflow
func (s *BillingActivities) CreateDraftInvoicesActivity(
	ctx context.Context,
	input subscriptionModels.CreateInvoicesActivityInput,
) (*subscriptionModels.CreateInvoicesActivityOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	// Set context values
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)
	ctx = types.SetUserID(ctx, input.UserID)

	subscriptionService := service.NewSubscriptionService(s.serviceParams)

	invoices := make([]string, 0)
	for _, period := range input.Periods {
		invoice, err := subscriptionService.CreateDraftInvoiceForSubscription(ctx, input.SubscriptionID, period)
		if err != nil {
			return nil, err
		}
		// Skip nil invoices (zero-amount invoices)
		if invoice != nil {
			invoices = append(invoices, invoice.ID)
		}
	}

	return &subscriptionModels.CreateInvoicesActivityOutput{
		InvoiceIDs: invoices,
	}, nil
}

// UpdateCurrentPeriodActivity updates the subscription to the new current period
func (s *BillingActivities) UpdateCurrentPeriodActivity(
	ctx context.Context,
	input subscriptionModels.UpdateSubscriptionPeriodActivityInput,
) (*subscriptionModels.UpdateSubscriptionPeriodActivityOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	// Set context values
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)
	ctx = types.SetUserID(ctx, input.UserID)

	// Get the subscription
	sub, err := s.serviceParams.SubRepo.Get(ctx, input.SubscriptionID)
	if err != nil {
		return nil, err
	}

	// Update to the new current period
	sub.CurrentPeriodStart = input.PeriodStart
	sub.CurrentPeriodEnd = input.PeriodEnd

	// Update the subscription
	if err := s.serviceParams.SubRepo.Update(ctx, sub); err != nil {
		s.logger.Errorw("failed to update subscription period",
			"subscription_id", sub.ID,
			"new_period_start", input.PeriodStart,
			"new_period_end", input.PeriodEnd,
			"error", err)
		return nil, err
	}

	s.logger.Infow("updated subscription period",
		"subscription_id", sub.ID,
		"new_period_start", input.PeriodStart,
		"new_period_end", input.PeriodEnd)

	return &subscriptionModels.UpdateSubscriptionPeriodActivityOutput{
		Success: true,
	}, nil
}

// TriggerInvoiceWorkflowActivity triggers invoice workflows for each invoice (fire-and-forget)
// If triggering fails for any invoice, it logs the error and continues with the rest
func (s *BillingActivities) TriggerInvoiceWorkflowActivity(
	ctx context.Context,
	input invoiceModels.TriggerInvoiceWorkflowActivityInput,
) (*invoiceModels.TriggerInvoiceWorkflowActivityOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	// Set context values
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)
	ctx = types.SetUserID(ctx, input.UserID)

	temporalSvc := temporalService.GetGlobalTemporalService()

	output := &invoiceModels.TriggerInvoiceWorkflowActivityOutput{
		TriggeredCount: 0,
		FailedCount:    0,
		FailedInvoices: make([]string, 0),
	}

	for _, invoiceID := range input.InvoiceIDs {
		_, err := temporalSvc.ExecuteWorkflow(
			ctx,
			types.TemporalProcessInvoiceWorkflow,
			invoiceModels.ProcessInvoiceWorkflowInput{
				InvoiceID:     invoiceID,
				TenantID:      input.TenantID,
				EnvironmentID: input.EnvironmentID,
				UserID:        input.UserID,
			},
		)
		if err != nil {
			s.logger.Errorw("failed to trigger invoice workflow",
				"invoice_id", invoiceID,
				"error", err)
			output.FailedCount++
			output.FailedInvoices = append(output.FailedInvoices, invoiceID)
			// Continue with other invoices - don't fail the entire activity
			continue
		}

		s.logger.Infow("triggered invoice workflow",
			"invoice_id", invoiceID)
		output.TriggeredCount++
	}

	return output, nil
}

// CheckCancellationActivity checks if a subscription should be cancelled and performs the cancellation
func (s *BillingActivities) CheckCancellationActivity(
	ctx context.Context,
	input subscriptionModels.CheckSubscriptionCancellationActivityInput,
) (*subscriptionModels.CheckSubscriptionCancellationActivityOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	// Set context values
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)
	ctx = types.SetUserID(ctx, input.UserID)

	sub, err := s.serviceParams.SubRepo.Get(ctx, input.SubscriptionID)
	if err != nil {
		return nil, err
	}

	shouldCancel := false
	var cancelledAt *time.Time

	// Check for cancellation at period end
	if sub.CancelAtPeriodEnd && sub.CancelAt != nil && !sub.CancelAt.After(input.Period.End) {
		shouldCancel = true
		cancelledAt = sub.CancelAt
		s.logger.Infow("subscription should be cancelled at period end",
			"subscription_id", sub.ID,
			"period_end", input.Period.End,
			"cancel_at", *sub.CancelAt)
	}

	// Check if period end matches the subscription end date
	if sub.EndDate != nil && input.Period.End.Equal(*sub.EndDate) {
		shouldCancel = true
		cancelledAt = sub.EndDate
		s.logger.Infow("subscription reached end date",
			"subscription_id", sub.ID,
			"period_end", input.Period.End,
			"end_date", *sub.EndDate)
	}

	// Perform cancellation if required
	if shouldCancel {
		sub.SubscriptionStatus = types.SubscriptionStatusCancelled
		sub.CancelledAt = cancelledAt

		err := s.serviceParams.DB.WithTx(ctx, func(ctx context.Context) error {
			// Update subscription
			if err := s.serviceParams.SubRepo.Update(ctx, sub); err != nil {
				return err
			}

			// Update the cancellation schedule status to executed
			subscriptionService := service.NewSubscriptionService(s.serviceParams)
			if err := subscriptionService.MarkCancellationScheduleAsExecuted(ctx, sub.ID); err != nil {
				s.logger.Errorw("failed to mark cancellation schedule as executed",
					"subscription_id", sub.ID,
					"error", err)
				// Don't fail the transaction, just log the error
			}

			return nil
		})
		if err != nil {
			s.logger.Errorw("failed to cancel subscription",
				"subscription_id", sub.ID,
				"error", err)
			return nil, err
		}

		s.logger.Infow("subscription cancelled successfully",
			"subscription_id", sub.ID,
			"cancelled_at", *cancelledAt)
	}

	return &subscriptionModels.CheckSubscriptionCancellationActivityOutput{
		IsCancelled: shouldCancel,
		Success:     true,
	}, nil
}

// ProcessPendingPlanChangesActivity processes any pending plan change schedules for a subscription
func (s *BillingActivities) ProcessPendingPlanChangesActivity(
	ctx context.Context,
	input subscriptionModels.ProcessPendingPlanChangesActivityInput,
) (*subscriptionModels.ProcessPendingPlanChangesActivityOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	// Set context values
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)
	ctx = types.SetUserID(ctx, input.UserID)

	// Get the subscription
	sub, err := s.serviceParams.SubRepo.Get(ctx, input.SubscriptionID)
	if err != nil {
		s.logger.Errorw("failed to get subscription",
			"subscription_id", input.SubscriptionID,
			"error", err)
		return nil, err
	}

	// Only process if subscription is active
	if sub.SubscriptionStatus != types.SubscriptionStatusActive {
		s.logger.Infow("subscription not active, skipping plan change processing",
			"subscription_id", sub.ID,
			"status", sub.SubscriptionStatus)
		return &subscriptionModels.ProcessPendingPlanChangesActivityOutput{
			Success:    true,
			WasChanged: false,
		}, nil
	}

	// Check if there's a pending plan change schedule
	schedule, err := s.serviceParams.SubScheduleRepo.GetPendingBySubscriptionAndType(
		ctx,
		sub.ID,
		types.SubscriptionScheduleChangeTypePlanChange,
	)
	if err != nil {
		s.logger.Errorw("failed to check for pending plan change",
			"subscription_id", sub.ID,
			"error", err)
		return nil, err
	}

	// No pending schedule, nothing to do
	if schedule == nil {
		s.logger.Infow("no pending plan change found",
			"subscription_id", sub.ID)
		return &subscriptionModels.ProcessPendingPlanChangesActivityOutput{
			Success:    true,
			WasChanged: false,
		}, nil
	}

	// Guard: Check if schedule is due (scheduled_at <= now)
	if schedule.ScheduledAt.After(time.Now()) {
		s.logger.Infow("schedule not yet due, skipping execution",
			"schedule_id", schedule.ID,
			"subscription_id", sub.ID,
			"scheduled_at", schedule.ScheduledAt,
			"current_time", time.Now())
		return &subscriptionModels.ProcessPendingPlanChangesActivityOutput{
			Success:    true,
			WasChanged: false,
		}, nil
	}

	s.logger.Infow("found pending plan change schedule, executing",
		"schedule_id", schedule.ID,
		"subscription_id", sub.ID,
		"scheduled_at", schedule.ScheduledAt)

	// Execute the plan change using the subscription service
	subscriptionService := service.NewSubscriptionService(s.serviceParams)
	changeService := service.NewSubscriptionChangeService(s.serviceParams)

	// Execute the scheduled plan change
	err = s.executeScheduledPlanChange(ctx, schedule, changeService, subscriptionService)
	if err != nil {
		s.logger.Errorw("failed to execute scheduled plan change",
			"schedule_id", schedule.ID,
			"subscription_id", sub.ID,
			"error", err)
		return nil, err
	}

	s.logger.Infow("successfully executed plan change at period end",
		"schedule_id", schedule.ID,
		"subscription_id", sub.ID)

	return &subscriptionModels.ProcessPendingPlanChangesActivityOutput{
		Success:    true,
		WasChanged: true,
	}, nil
}

// executeScheduledPlanChange executes a scheduled plan change
func (s *BillingActivities) executeScheduledPlanChange(
	ctx context.Context,
	schedule *subscription.SubscriptionSchedule,
	changeService service.SubscriptionChangeService,
	subscriptionService service.SubscriptionService,
) error {
	// Get the plan change configuration
	config, err := schedule.GetPlanChangeConfig()
	if err != nil {
		return fmt.Errorf("failed to parse plan change configuration: %w", err)
	}

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

	// Execute the change
	response, err := changeService.ExecuteSubscriptionChangeInternal(ctx, schedule.SubscriptionID, changeRequest)
	if err != nil {
		// Mark schedule as failed
		schedule.Status = types.ScheduleStatusFailed
		schedule.ExecutedAt = lo.ToPtr(time.Now().UTC())
		schedule.ErrorMessage = lo.ToPtr(err.Error())
		if updateErr := s.serviceParams.SubScheduleRepo.Update(ctx, schedule); updateErr != nil {
			s.logger.Errorw("failed to update schedule status to failed",
				"schedule_id", schedule.ID,
				"subscription_id", schedule.SubscriptionID,
				"original_error", err,
				"update_error", updateErr)
		}
		return err
	}

	// Validate response is not nil and has required fields
	if response == nil {
		err := fmt.Errorf("subscription change returned nil response")
		schedule.Status = types.ScheduleStatusFailed
		schedule.ExecutedAt = lo.ToPtr(time.Now().UTC())
		schedule.ErrorMessage = lo.ToPtr(err.Error())
		if updateErr := s.serviceParams.SubScheduleRepo.Update(ctx, schedule); updateErr != nil {
			s.logger.Errorw("failed to update schedule status to failed",
				"schedule_id", schedule.ID,
				"subscription_id", schedule.SubscriptionID,
				"update_error", updateErr)
		}
		return err
	}

	if response.OldSubscription.ID == "" || response.NewSubscription.ID == "" {
		err := fmt.Errorf("subscription change response missing required subscription IDs")
		schedule.Status = types.ScheduleStatusFailed
		schedule.ExecutedAt = lo.ToPtr(time.Now().UTC())
		schedule.ErrorMessage = lo.ToPtr(err.Error())
		if updateErr := s.serviceParams.SubScheduleRepo.Update(ctx, schedule); updateErr != nil {
			s.logger.Errorw("failed to update schedule status to failed",
				"schedule_id", schedule.ID,
				"subscription_id", schedule.SubscriptionID,
				"update_error", updateErr)
		}
		return err
	}

	// Mark schedule as completed
	schedule.Status = types.ScheduleStatusExecuted
	schedule.ExecutedAt = lo.ToPtr(time.Now().UTC())

	// Set execution result
	result := &subscription.PlanChangeResult{
		OldSubscriptionID: response.OldSubscription.ID,
		NewSubscriptionID: response.NewSubscription.ID,
		ChangeType:        string(response.ChangeType),
		EffectiveDate:     response.EffectiveDate,
	}
	if err := schedule.SetPlanChangeResult(result); err != nil {
		s.logger.Errorw("failed to set plan change result", "error", err)
	}

	if err := s.serviceParams.SubScheduleRepo.Update(ctx, schedule); err != nil {
		s.logger.Errorw("failed to update schedule status", "error", err)
		return err
	}

	return nil
}
