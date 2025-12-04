package subscription

import (
	"context"

	"github.com/flexprice/flexprice/internal/integration/stripe"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	subscriptionModels "github.com/flexprice/flexprice/internal/temporal/models/subscription"
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

// CheckPauseActivity checks and handles subscription pause status
// It activates scheduled pauses, handles auto-resume, and returns whether processing should continue
func (s *BillingActivities) CheckPauseActivity(
	ctx context.Context,
	input subscriptionModels.CheckSubscriptionPauseStatusActivityInput,
) (*subscriptionModels.CheckSubscriptionPauseStatusActivityOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	// Set context values
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	// Get the subscription
	sub, err := s.serviceParams.SubRepo.Get(ctx, input.SubscriptionID)
	if err != nil {
		return nil, err
	}

	now := input.CurrentTime
	output := &subscriptionModels.CheckSubscriptionPauseStatusActivityOutput{
		ShouldSkipProcessing: false,
		IsPaused:             false,
		WasResumed:           false,
	}

	// Skip processing for already paused subscriptions (unless we need to check for auto-resume)
	if sub.SubscriptionStatus == types.SubscriptionStatusPaused && sub.ActivePauseID == nil {
		output.ShouldSkipProcessing = true
		output.IsPaused = true
		s.logger.Infow("skipping period processing for paused subscription",
			"subscription_id", sub.ID)
		return output, nil
	}

	// Check for scheduled pauses that should be activated
	if sub.PauseStatus == types.PauseStatusScheduled && sub.ActivePauseID != nil {
		pause, err := s.serviceParams.SubRepo.GetPause(ctx, *sub.ActivePauseID)
		if err != nil {
			return nil, err
		}

		// If this is a period-end pause and we're at period end, activate it
		if pause.PauseMode == types.PauseModePeriodEnd && !now.Before(sub.CurrentPeriodEnd) {
			sub.SubscriptionStatus = types.SubscriptionStatusPaused
			pause.PauseStatus = types.PauseStatusActive

			// Update the subscription and pause
			if err := s.serviceParams.SubRepo.Update(ctx, sub); err != nil {
				return nil, err
			}

			if err := s.serviceParams.SubRepo.UpdatePause(ctx, pause); err != nil {
				return nil, err
			}

			s.logger.Infow("activated period-end pause",
				"subscription_id", sub.ID,
				"pause_id", pause.ID)

			output.ShouldSkipProcessing = true
			output.IsPaused = true
			output.PauseID = &pause.ID
			return output, nil
		}

		// If this is a scheduled pause and we've reached the start date, activate it
		if pause.PauseMode == types.PauseModeScheduled && !now.Before(pause.PauseStart) {
			sub.SubscriptionStatus = types.SubscriptionStatusPaused
			pause.PauseStatus = types.PauseStatusActive

			// Update the subscription and pause
			if err := s.serviceParams.SubRepo.Update(ctx, sub); err != nil {
				return nil, err
			}

			if err := s.serviceParams.SubRepo.UpdatePause(ctx, pause); err != nil {
				return nil, err
			}

			s.logger.Infow("activated scheduled pause",
				"subscription_id", sub.ID,
				"pause_id", pause.ID)

			output.ShouldSkipProcessing = true
			output.IsPaused = true
			output.PauseID = &pause.ID
			return output, nil
		}
	}

	// Check for auto-resume based on pause end date
	if sub.SubscriptionStatus == types.SubscriptionStatusPaused && sub.ActivePauseID != nil {
		pause, err := s.serviceParams.SubRepo.GetPause(ctx, *sub.ActivePauseID)
		if err != nil {
			return nil, err
		}

		// If this pause has an end date and we've reached it, auto-resume
		if pause.PauseEnd != nil && !now.Before(*pause.PauseEnd) {
			// Calculate the pause duration
			pauseDuration := now.Sub(pause.PauseStart)

			// Update the pause record
			pause.PauseStatus = types.PauseStatusCompleted
			pause.ResumedAt = &now

			// Update the subscription
			sub.SubscriptionStatus = types.SubscriptionStatusActive
			sub.PauseStatus = types.PauseStatusNone
			sub.ActivePauseID = nil

			// Adjust the billing period by the pause duration
			sub.CurrentPeriodEnd = sub.CurrentPeriodEnd.Add(pauseDuration)

			// Update the subscription and pause
			if err := s.serviceParams.SubRepo.Update(ctx, sub); err != nil {
				return nil, err
			}

			if err := s.serviceParams.SubRepo.UpdatePause(ctx, pause); err != nil {
				return nil, err
			}

			s.logger.Infow("auto-resumed subscription",
				"subscription_id", sub.ID,
				"pause_id", pause.ID,
				"pause_duration", pauseDuration)

			output.WasResumed = true
			output.IsPaused = false
			output.ShouldSkipProcessing = false
			updatedPeriodEnd := sub.CurrentPeriodEnd
			output.UpdatedPeriodEnd = &updatedPeriodEnd
			output.PauseID = &pause.ID
			// Continue with normal processing
			return output, nil
		} else {
			// Still paused, skip processing
			output.ShouldSkipProcessing = true
			output.IsPaused = true
			output.PauseID = &pause.ID
			s.logger.Infow("skipping period processing for paused subscription",
				"subscription_id", sub.ID)
			return output, nil
		}
	}

	// No pause-related actions needed, continue with normal processing
	return output, nil
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

	// Get the subscription
	sub, err := s.serviceParams.SubRepo.Get(ctx, input.SubscriptionID)
	if err != nil {
		return nil, err
	}

	now := input.CurrentTime
	currentStart := sub.CurrentPeriodStart
	currentEnd := sub.CurrentPeriodEnd

	// Start with current period
	periods := []subscriptionModels.BillingPeriod{
		{
			Start: currentStart,
			End:   currentEnd,
		},
	}

	reachedEndDate := false
	hasMorePeriods := false

	// Generate periods but respect subscription end date
	for currentEnd.Before(now) {
		nextStart := currentEnd
		nextEnd, err := types.NextBillingDate(nextStart, sub.BillingAnchor, sub.BillingPeriodCount, sub.BillingPeriod, sub.EndDate)
		if err != nil {
			s.logger.Errorw("failed to calculate next billing date",
				"subscription_id", sub.ID,
				"current_end", currentEnd,
				"process_up_to", now,
				"error", err)
			return nil, err
		}

		// In case of end date reached or next end is equal to current end, we break the loop
		// nextEnd will be equal to currentEnd in case of end date reached
		if nextEnd.Equal(currentEnd) {
			s.logger.Infow("stopped period generation - reached subscription end date",
				"subscription_id", sub.ID,
				"end_date", sub.EndDate,
				"final_period_end", currentEnd)
			reachedEndDate = true
			break
		}

		periods = append(periods, subscriptionModels.BillingPeriod{
			Start: nextStart,
			End:   nextEnd,
		})

		currentEnd = nextEnd
	}

	// Check if there are more periods beyond the last calculated period
	if !reachedEndDate {
		// Try to calculate one more period to see if there are more
		nextEnd, err := types.NextBillingDate(currentEnd, sub.BillingAnchor, sub.BillingPeriodCount, sub.BillingPeriod, sub.EndDate)
		if err == nil && !nextEnd.Equal(currentEnd) {
			hasMorePeriods = true
		}
	}

	output := &subscriptionModels.CalculatePeriodsActivityOutput{
		Periods:        periods,
		HasMorePeriods: hasMorePeriods,
		ReachedEndDate: reachedEndDate,
	}

	if len(periods) > 0 {
		lastPeriod := periods[len(periods)-1]
		output.FinalPeriodEnd = &lastPeriod.End
	}

	if len(periods) == 1 {
		s.logger.Debugw("no transitions needed for subscription",
			"subscription_id", sub.ID,
			"current_period_start", sub.CurrentPeriodStart,
			"current_period_end", sub.CurrentPeriodEnd,
			"process_up_to", now)
	}

	return output, nil
}

// ProcessPeriodsActivity processes all billing periods by calling CreateInvoicesActivity for each period
// and checking for cancellation after each period
func (s *BillingActivities) ProcessPeriodsActivity(
	ctx context.Context,
	input subscriptionModels.ProcessPeriodsActivityInput,
) (*subscriptionModels.ProcessPeriodsActivityOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	// Set context values
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	// Get the subscription
	sub, err := s.serviceParams.SubRepo.Get(ctx, input.SubscriptionID)
	if err != nil {
		return nil, err
	}

	invoiceIDs := []string{}

	// Process all periods except the last one (which becomes the new current period)
	for i := 0; i < len(input.Periods)-1; i++ {
		period := input.Periods[i]

		// Create invoice for this period using CreateInvoicesActivity
		createInvoiceInput := subscriptionModels.CreateInvoicesActivityInput{
			SubscriptionID: input.SubscriptionID,
			TenantID:       input.TenantID,
			EnvironmentID:  input.EnvironmentID,
			PeriodStart:    period.Start,
			PeriodEnd:      period.End,
		}

		createInvoiceOutput, err := s.CreateInvoicesActivity(ctx, createInvoiceInput)
		if err != nil {
			s.logger.Errorw("failed to create invoice for period",
				"subscription_id", sub.ID,
				"period_start", period.Start,
				"period_end", period.End,
				"period_index", i,
				"error", err)
			return nil, err
		}

		// Refresh subscription to get latest state
		sub, err = s.serviceParams.SubRepo.Get(ctx, input.SubscriptionID)
		if err != nil {
			return nil, err
		}

		// Check for cancellation at this period end
		if sub.CancelAtPeriodEnd && sub.CancelAt != nil && !sub.CancelAt.After(period.End) {
			s.logger.Infow("subscription should be cancelled at period end",
				"subscription_id", sub.ID,
				"period_end", period.End,
				"cancel_at", *sub.CancelAt)
			// Update subscription status to cancelled
			sub.SubscriptionStatus = types.SubscriptionStatusCancelled
			sub.EndDate = sub.CancelAt
			if err := s.serviceParams.SubRepo.Update(ctx, sub); err != nil {
				return nil, err
			}
			// Break out of loop - no more periods to process
			break
		}

		// Check if this period end matches the subscription end date
		if sub.EndDate != nil && period.End.Equal(*sub.EndDate) {
			s.logger.Infow("subscription should be cancelled (end date reached)",
				"subscription_id", sub.ID,
				"period_end", period.End,
				"end_date", *sub.EndDate)
			// Update subscription status to cancelled
			sub.SubscriptionStatus = types.SubscriptionStatusCancelled
			sub.CancelledAt = sub.EndDate
			if err := s.serviceParams.SubRepo.Update(ctx, sub); err != nil {
				return nil, err
			}
			// Break out of loop - no more periods to process
			break
		}

		// Add invoice ID if invoice was created
		if createInvoiceOutput.InvoiceCreated && createInvoiceOutput.InvoiceID != nil {
			invoiceIDs = append(invoiceIDs, *createInvoiceOutput.InvoiceID)
			s.logger.Infow("created invoice for period",
				"subscription_id", sub.ID,
				"invoice_id", *createInvoiceOutput.InvoiceID,
				"period_start", period.Start,
				"period_end", period.End,
				"period_index", i)
		} else {
			s.logger.Debugw("no invoice created for period (zero amount)",
				"subscription_id", sub.ID,
				"period_start", period.Start,
				"period_end", period.End,
				"period_index", i)
		}
	}

	return &subscriptionModels.ProcessPeriodsActivityOutput{
		InvoiceIDs:       invoiceIDs,
		PeriodsProcessed: len(input.Periods) - 1,
	}, nil
}

// CreateInvoicesActivity creates and finalizes an invoice for a specific billing period
// This activity does NOT sync to external vendors or attempt payment - those are handled separately
func (s *BillingActivities) CreateInvoicesActivity(
	ctx context.Context,
	input subscriptionModels.CreateInvoicesActivityInput,
) (*subscriptionModels.CreateInvoicesActivityOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	// Set context values
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	// Get the subscription
	sub, err := s.serviceParams.SubRepo.Get(ctx, input.SubscriptionID)
	if err != nil {
		return nil, err
	}

	// Initialize services
	invoiceService := service.NewInvoiceService(s.serviceParams)
	billingService := service.NewBillingService(s.serviceParams)

	// Get subscription with line items
	subWithLineItems, _, err := s.serviceParams.SubRepo.GetWithLineItems(ctx, input.SubscriptionID)
	if err != nil {
		return nil, err
	}

	// Prepare invoice request using billing service
	invoiceReq, err := billingService.PrepareSubscriptionInvoiceRequest(ctx,
		subWithLineItems,
		input.PeriodStart,
		input.PeriodEnd,
		types.ReferencePointPeriodEnd,
	)
	if err != nil {
		s.logger.Errorw("failed to prepare invoice request",
			"subscription_id", sub.ID,
			"period_start", input.PeriodStart,
			"period_end", input.PeriodEnd,
			"error", err)
		return nil, err
	}

	// Check if the invoice is zero amount
	if invoiceReq.Subtotal.IsZero() {
		s.logger.Debugw("no invoice created (zero amount)",
			"subscription_id", sub.ID,
			"period_start", input.PeriodStart,
			"period_end", input.PeriodEnd)
		return &subscriptionModels.CreateInvoicesActivityOutput{
			InvoiceCreated: false,
		}, nil
	}

	// Create the invoice (this creates it as draft)
	inv, err := invoiceService.CreateInvoice(ctx, *invoiceReq)
	if err != nil {
		s.logger.Errorw("failed to create invoice",
			"subscription_id", sub.ID,
			"period_start", input.PeriodStart,
			"period_end", input.PeriodEnd,
			"error", err)
		return nil, err
	}

	// Finalize the invoice (without syncing or payment attempts)
	if err := invoiceService.FinalizeInvoice(ctx, inv.ID); err != nil {
		s.logger.Errorw("failed to finalize invoice",
			"subscription_id", sub.ID,
			"invoice_id", inv.ID,
			"period_start", input.PeriodStart,
			"period_end", input.PeriodEnd,
			"error", err)
		return nil, err
	}

	// Get the finalized invoice
	finalizedInv, err := invoiceService.GetInvoice(ctx, inv.ID)
	if err != nil {
		s.logger.Errorw("failed to get finalized invoice",
			"subscription_id", sub.ID,
			"invoice_id", inv.ID,
			"error", err)
		return nil, err
	}

	s.logger.Infow("created and finalized invoice",
		"subscription_id", sub.ID,
		"invoice_id", finalizedInv.ID,
		"period_start", input.PeriodStart,
		"period_end", input.PeriodEnd)

	return &subscriptionModels.CreateInvoicesActivityOutput{
		InvoiceCreated: true,
		InvoiceID:      &finalizedInv.ID,
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

// SyncInvoiceActivity syncs an invoice to external vendors (Stripe, etc.)
func (s *BillingActivities) SyncInvoiceActivity(
	ctx context.Context,
	input subscriptionModels.SyncInvoiceToExternalVendorActivityInput,
) (*subscriptionModels.SyncInvoiceToExternalVendorActivityOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	// Set context values
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	// Get the subscription
	sub, err := s.serviceParams.SubRepo.Get(ctx, input.SubscriptionID)
	if err != nil {
		return nil, err
	}

	// Check if Stripe connection exists and invoice sync is enabled
	conn, err := s.serviceParams.ConnectionRepo.GetByProvider(ctx, types.SecretProviderStripe)
	if err != nil || conn == nil {
		s.logger.Debugw("Stripe connection not available, skipping invoice sync",
			"invoice_id", input.InvoiceID,
			"error", err)
		return &subscriptionModels.SyncInvoiceToExternalVendorActivityOutput{
			Success: true, // Not an error, just skip sync
		}, nil
	}

	// Check if invoice sync is enabled for this connection
	if !conn.IsInvoiceOutboundEnabled() {
		s.logger.Debugw("invoice sync disabled for Stripe connection, skipping invoice sync",
			"invoice_id", input.InvoiceID,
			"connection_id", conn.ID)
		return &subscriptionModels.SyncInvoiceToExternalVendorActivityOutput{
			Success: true, // Not an error, just skip sync
		}, nil
	}

	// Get Stripe integration
	stripeIntegration, err := s.serviceParams.IntegrationFactory.GetStripeIntegration(ctx)
	if err != nil {
		s.logger.Errorw("failed to get Stripe integration, skipping invoice sync",
			"invoice_id", input.InvoiceID,
			"error", err)
		return &subscriptionModels.SyncInvoiceToExternalVendorActivityOutput{
			Success: true, // Don't fail the entire process, just skip invoice sync
		}, nil
	}

	// Ensure customer is synced to Stripe before syncing invoice
	customerService := service.NewCustomerService(s.serviceParams)
	_, err = stripeIntegration.CustomerSvc.EnsureCustomerSyncedToStripe(ctx, sub.CustomerID, customerService)
	if err != nil {
		s.logger.Errorw("failed to ensure customer is synced to Stripe, skipping invoice sync",
			"invoice_id", input.InvoiceID,
			"customer_id", sub.CustomerID,
			"error", err)
		return &subscriptionModels.SyncInvoiceToExternalVendorActivityOutput{
			Success: true, // Don't fail the entire process, just skip invoice sync
		}, nil
	}

	// Determine collection method from subscription
	collectionMethod := types.CollectionMethod(sub.CollectionMethod)

	// Create sync request
	syncRequest := stripe.StripeInvoiceSyncRequest{
		InvoiceID:        input.InvoiceID,
		CollectionMethod: string(collectionMethod),
	}

	// Perform the sync
	_, err = stripeIntegration.InvoiceSyncSvc.SyncInvoiceToStripe(ctx, syncRequest, customerService)
	if err != nil {
		s.logger.Errorw("failed to sync invoice to Stripe",
			"invoice_id", input.InvoiceID,
			"subscription_id", input.SubscriptionID,
			"error", err)
		// Don't fail the entire process, just log the error
		return &subscriptionModels.SyncInvoiceToExternalVendorActivityOutput{
			Success: false,
		}, nil
	}

	s.logger.Infow("successfully synced invoice to Stripe",
		"invoice_id", input.InvoiceID,
		"subscription_id", input.SubscriptionID)

	return &subscriptionModels.SyncInvoiceToExternalVendorActivityOutput{
		Success: true,
	}, nil
}

// AttemptPaymentActivity attempts to collect payment for an invoice
func (s *BillingActivities) AttemptPaymentActivity(
	ctx context.Context,
	input subscriptionModels.AttemptPaymentActivityInput,
) (*subscriptionModels.AttemptPaymentActivityOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	// Set context values
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	// Initialize invoice service
	invoiceService := service.NewInvoiceService(s.serviceParams)

	// Attempt payment
	if err := invoiceService.AttemptPayment(ctx, input.InvoiceID); err != nil {
		s.logger.Errorw("failed to attempt payment",
			"invoice_id", input.InvoiceID,
			"subscription_id", input.SubscriptionID,
			"error", err)
		return nil, err
	}

	s.logger.Infow("payment attempt completed",
		"invoice_id", input.InvoiceID,
		"subscription_id", input.SubscriptionID)

	return &subscriptionModels.AttemptPaymentActivityOutput{
		Success: true,
	}, nil
}

// CheckCancellationActivity checks if a subscription should be cancelled
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

	// Get the subscription
	sub, err := s.serviceParams.SubRepo.Get(ctx, input.SubscriptionID)
	if err != nil {
		return nil, err
	}

	output := &subscriptionModels.CheckSubscriptionCancellationActivityOutput{
		ShouldCancel: false,
	}

	// Check for cancellation at period end
	if sub.CancelAtPeriodEnd && sub.CancelAt != nil && !sub.CancelAt.After(input.PeriodEnd) {
		output.ShouldCancel = true
		output.CancelledAt = sub.CancelAt
		s.logger.Infow("subscription should be cancelled at period end",
			"subscription_id", sub.ID,
			"period_end", input.PeriodEnd,
			"cancel_at", *sub.CancelAt)
		return output, nil
	}

	// Check if period end matches the subscription end date
	if sub.EndDate != nil && input.PeriodEnd.Equal(*sub.EndDate) {
		output.ShouldCancel = true
		output.CancelledAt = sub.EndDate
		s.logger.Infow("subscription should be cancelled (end date reached)",
			"subscription_id", sub.ID,
			"period_end", input.PeriodEnd,
			"end_date", *sub.EndDate)
		return output, nil
	}

	return output, nil
}
