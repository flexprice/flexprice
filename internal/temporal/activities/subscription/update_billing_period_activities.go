package subscription

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	invoiceModels "github.com/flexprice/flexprice/internal/temporal/models/invoice"
	subscriptionModels "github.com/flexprice/flexprice/internal/temporal/models/subscription"
	temporalService "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
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
	// Truncate to seconds to avoid sub-second precision issues in billing period comparisons
	sub.CurrentPeriodStart = input.PeriodStart.Truncate(time.Second)
	sub.CurrentPeriodEnd = input.PeriodEnd.Truncate(time.Second)

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
			return s.serviceParams.SubRepo.Update(ctx, sub)
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
		Success: true,
	}, nil
}
