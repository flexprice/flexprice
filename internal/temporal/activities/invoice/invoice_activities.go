package invoice

import (
	"context"

	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	invoiceModels "github.com/flexprice/flexprice/internal/temporal/models/invoice"
	temporalService "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/activity"
)

// InvoiceActivities contains all invoice-related activities
type InvoiceActivities struct {
	serviceParams service.ServiceParams
	logger        *logger.Logger
}

// NewInvoiceActivities creates a new InvoiceActivities instance
func NewInvoiceActivities(
	serviceParams service.ServiceParams,
	logger *logger.Logger,
) *InvoiceActivities {
	return &InvoiceActivities{
		serviceParams: serviceParams,
		logger:        logger,
	}
}

// ComputeInvoiceActivity computes an invoice (line items, coupons/taxes, or SKIPPED). Returns Skipped=true if zero-dollar.
// Invoice number is NOT assigned here — it is assigned during FinalizeInvoiceActivity.
func (s *InvoiceActivities) ComputeInvoiceActivity(
	ctx context.Context,
	input invoiceModels.ComputeInvoiceActivityInput,
) (*invoiceModels.ComputeInvoiceActivityOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)
	ctx = types.SetUserID(ctx, input.UserID)
	invoiceService := service.NewInvoiceService(s.serviceParams)
	// Pass nil for subscription invoices - coupons/taxes come from billing service
	skipped, err := invoiceService.ComputeInvoice(ctx, input.InvoiceID, nil)
	if err != nil {
		s.logger.Errorw("failed to compute invoice",
			"invoice_id", input.InvoiceID,
			"error", err)
		return nil, err
	}
	s.logger.Infow("computed invoice",
		"invoice_id", input.InvoiceID,
		"skipped", skipped)
	return &invoiceModels.ComputeInvoiceActivityOutput{
		Skipped: skipped,
	}, nil
}

// CreateDraftForCurrentSubscriptionPeriodActivity creates an idempotent subscription draft for the subscription's current period (no compute).
func (s *InvoiceActivities) CreateDraftForCurrentSubscriptionPeriodActivity(
	ctx context.Context,
	input invoiceModels.CreateDraftForCurrentSubscriptionPeriodActivityInput,
) (*invoiceModels.CreateDraftForCurrentSubscriptionPeriodActivityOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)
	ctx = types.SetUserID(ctx, input.UserID)

	sub, err := s.serviceParams.SubRepo.Get(ctx, input.SubscriptionID)
	if err != nil {
		return nil, err
	}
	periodStart := sub.CurrentPeriodStart
	periodEnd := sub.CurrentPeriodEnd
	if periodStart.IsZero() || periodEnd.IsZero() {
		return nil, ierr.NewError("subscription is missing current period bounds").
			WithHint("Set CurrentPeriodStart and CurrentPeriodEnd on the subscription").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": input.SubscriptionID,
			}).
			Mark(ierr.ErrValidation)
	}
	if periodEnd.Before(periodStart) {
		return nil, ierr.NewError("invalid subscription current period (end before start)").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": input.SubscriptionID,
			}).
			Mark(ierr.ErrValidation)
	}

	invoiceService := service.NewInvoiceService(s.serviceParams)
	draft, err := invoiceService.CreateDraftInvoiceForSubscription(
		ctx, input.SubscriptionID, periodStart, periodEnd, types.ReferencePointPeriodEnd,
	)
	if err != nil {
		s.logger.Errorw("failed to create draft invoice for subscription current period",
			"subscription_id", input.SubscriptionID,
			"error", err)
		return nil, err
	}
	return &invoiceModels.CreateDraftForCurrentSubscriptionPeriodActivityOutput{
		InvoiceID: draft.ID,
	}, nil
}

// FinalizeInvoiceActivity finalizes an invoice
func (s *InvoiceActivities) FinalizeInvoiceActivity(
	ctx context.Context,
	input invoiceModels.FinalizeInvoiceActivityInput,
) (*invoiceModels.FinalizeInvoiceActivityOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	// Set context values
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)
	ctx = types.SetUserID(ctx, input.UserID)

	invoiceService := service.NewInvoiceService(s.serviceParams)

	// Check if finalization delay has elapsed
	due, err := invoiceService.IsFinalizationDue(ctx, input.InvoiceID)
	if err != nil {
		s.logger.Errorw("failed to check finalization delay",
			"invoice_id", input.InvoiceID,
			"error", err)
		return nil, err
	}
	if !due {
		s.logger.Infow("finalization delay not yet elapsed, skipping",
			"invoice_id", input.InvoiceID)
		return &invoiceModels.FinalizeInvoiceActivityOutput{Success: true, Skipped: true}, nil
	}

	if err := invoiceService.FinalizeInvoice(ctx, input.InvoiceID); err != nil {
		s.logger.Errorw("failed to finalize invoice",
			"invoice_id", input.InvoiceID,
			"error", err)
		return nil, err
	}

	s.logger.Infow("finalized invoice successfully",
		"invoice_id", input.InvoiceID)

	return &invoiceModels.FinalizeInvoiceActivityOutput{
		Success: true,
	}, nil
}

// SyncInvoiceToVendorActivity syncs an invoice to external vendors
func (s *InvoiceActivities) SyncInvoiceToVendorActivity(
	ctx context.Context,
	input invoiceModels.SyncInvoiceActivityInput,
) (*invoiceModels.SyncInvoiceActivityOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	// Set context values
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)
	ctx = types.SetUserID(ctx, input.UserID)

	invoiceService := service.NewInvoiceService(s.serviceParams)

	if err := invoiceService.SyncInvoiceToExternalVendors(ctx, input.InvoiceID); err != nil {
		s.logger.Errorw("failed to sync invoice to external vendor",
			"invoice_id", input.InvoiceID,
			"error", err)
		return nil, err
	}

	s.logger.Infow("synced invoice to external vendor successfully",
		"invoice_id", input.InvoiceID)

	return &invoiceModels.SyncInvoiceActivityOutput{
		Success: true,
	}, nil
}

// AttemptInvoicePaymentActivity attempts to collect payment for an invoice
func (s *InvoiceActivities) AttemptInvoicePaymentActivity(
	ctx context.Context,
	input invoiceModels.PaymentActivityInput,
) (*invoiceModels.PaymentActivityOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	// Set context values
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)
	ctx = types.SetUserID(ctx, input.UserID)

	invoiceService := service.NewInvoiceService(s.serviceParams)

	if err := invoiceService.AttemptPayment(ctx, input.InvoiceID); err != nil {
		s.logger.Errorw("failed to attempt payment for invoice",
			"invoice_id", input.InvoiceID,
			"error", err)
		return nil, err
	}

	s.logger.Infow("attempted payment for invoice successfully",
		"invoice_id", input.InvoiceID)

	return &invoiceModels.PaymentActivityOutput{
		Success: true,
	}, nil
}

// RecalculateInvoiceActivity recalculates a voided subscription invoice by creating a replacement invoice (same billing period).
func (s *InvoiceActivities) RecalculateInvoiceActivity(
	ctx context.Context,
	input invoiceModels.RecalculateInvoiceActivityInput,
) (*invoiceModels.RecalculateInvoiceActivityOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)
	ctx = types.SetUserID(ctx, input.UserID)

	invSvc := service.NewInvoiceService(s.serviceParams)

	newInv, err := invSvc.RecalculateInvoice(ctx, input.InvoiceID)
	if err != nil {
		s.logger.Errorw("failed to recalculate invoice",
			"invoice_id", input.InvoiceID,
			"error", err)
		return nil, err
	}

	outID := input.InvoiceID
	if newInv != nil {
		outID = newInv.ID
	}
	s.logger.Infow("recalculated invoice successfully",
		"invoice_id", input.InvoiceID,
		"new_invoice_id", outID)

	return &invoiceModels.RecalculateInvoiceActivityOutput{
		Success:   true,
		InvoiceID: outID,
	}, nil
}

// FinalizeDueDraftsActivity scans all draft invoices across tenants, checks which are due
// for finalization, and fires FinalizeDraftInvoiceWorkflow for each. Same pattern as
// ScheduleBillingActivity: single activity does scan + fire-and-forget workflow starts.
func (s *InvoiceActivities) FinalizeDueDraftsActivity(
	ctx context.Context,
	input invoiceModels.FinalizeDueDraftsActivityInput,
) (*invoiceModels.ScheduleDraftFinalizationWorkflowResult, error) {
	logger := activity.GetLogger(ctx)
	result := &invoiceModels.ScheduleDraftFinalizationWorkflowResult{}

	batchSize := input.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}

	// Read max workflows per cron run from app config
	maxPerRun := 500
	if cfg, err := config.NewConfig(); err == nil && cfg.Temporal.MaxWorkflowsPerCronRun > 0 {
		maxPerRun = cfg.Temporal.MaxWorkflowsPerCronRun
	}

	invoiceService := service.NewInvoiceService(s.serviceParams)
	temporalSvc := temporalService.GetGlobalTemporalService()

	offset := 0
	capReached := false
	for !capReached {
		drafts, err := invoiceService.ListAllTenantDraftInvoices(ctx, batchSize, offset)
		if err != nil {
			logger.Error("Failed to list draft invoices", "offset", offset, "error", err)
			return result, err
		}
		if len(drafts) == 0 {
			break
		}

		for _, inv := range drafts {
			if result.FinalizedCount >= maxPerRun {
				logger.Info("Reached max workflows per cron run, remaining processed next cycle",
					"max", maxPerRun, "triggered", result.FinalizedCount)
				capReached = true
				break
			}

			result.TotalProcessed++

			invCtx := types.SetTenantID(ctx, inv.TenantID)
			invCtx = types.SetEnvironmentID(invCtx, inv.EnvironmentID)
			invCtx = types.SetUserID(invCtx, inv.CreatedBy)

			due, err := invoiceService.IsFinalizationDue(invCtx, inv.ID)
			if err != nil {
				logger.Error("Failed to check finalization due", "invoice_id", inv.ID, "error", err)
				result.FailedCount++
				continue
			}
			if !due {
				result.SkippedCount++
				continue
			}

			// Fire FinalizeDraftInvoiceWorkflow — same pattern as ScheduleBillingActivity
			_, err = temporalSvc.ExecuteWorkflow(
				invCtx,
				types.TemporalFinalizeDraftInvoiceWorkflow,
				invoiceModels.ProcessInvoiceWorkflowInput{
					InvoiceID:     inv.ID,
					TenantID:      inv.TenantID,
					EnvironmentID: inv.EnvironmentID,
					UserID:        inv.CreatedBy,
				},
			)
			if err != nil {
				logger.Error("Failed to trigger FinalizeDraftInvoiceWorkflow",
					"invoice_id", inv.ID, "error", err)
				result.FailedCount++
				continue
			}
			result.FinalizedCount++
		}

		if len(drafts) < batchSize {
			break
		}
		offset += batchSize
	}

	logger.Info("Completed finalize due drafts",
		"total_processed", result.TotalProcessed,
		"finalized", result.FinalizedCount,
		"skipped", result.SkippedCount,
		"failed", result.FailedCount)

	return result, nil
}
