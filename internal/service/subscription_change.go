package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/proration"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

// SubscriptionChangeService handles subscription plan changes (upgrades/downgrades)
type SubscriptionChangeService interface {
	// PreviewSubscriptionChange shows the impact of changing subscription plan
	PreviewSubscriptionChange(ctx context.Context, subscriptionID string, req dto.SubscriptionChangeRequest) (*dto.SubscriptionChangePreviewResponse, error)

	// ExecuteSubscriptionChange performs the actual subscription plan change
	ExecuteSubscriptionChange(ctx context.Context, subscriptionID string, req dto.SubscriptionChangeRequest) (*dto.SubscriptionChangeExecuteResponse, error)

	// ExecuteSubscriptionChangeInternal executes a subscription change immediately (used by scheduled execution)
	ExecuteSubscriptionChangeInternal(ctx context.Context, subscriptionID string, req dto.SubscriptionChangeRequest) (*dto.SubscriptionChangeExecuteResponse, error)
}

type subscriptionChangeService struct {
	serviceParams ServiceParams
}

// NewSubscriptionChangeService creates a new subscription change service
func NewSubscriptionChangeService(serviceParams ServiceParams) SubscriptionChangeService {
	return &subscriptionChangeService{
		serviceParams: serviceParams,
	}
}

// PreviewSubscriptionChange shows the impact of changing subscription plan
func (s *subscriptionChangeService) PreviewSubscriptionChange(
	ctx context.Context,
	subscriptionID string,
	req dto.SubscriptionChangeRequest,
) (*dto.SubscriptionChangePreviewResponse, error) {
	logger := s.serviceParams.Logger.With(
		zap.String("subscription_id", subscriptionID),
		zap.String("target_plan_id", req.TargetPlanID),
	)

	logger.Info("previewing subscription change")

	// Validate the request
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Get current subscription with line items
	currentSub, lineItems, err := s.serviceParams.SubRepo.GetWithLineItems(ctx, subscriptionID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve subscription").
			Mark(ierr.ErrDatabase)
	}

	// Get current plan
	currentPlan, err := s.serviceParams.PlanRepo.Get(ctx, currentSub.PlanID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve current plan").
			Mark(ierr.ErrDatabase)
	}

	// Get target plan
	targetPlan, err := s.serviceParams.PlanRepo.Get(ctx, req.TargetPlanID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve target plan").
			Mark(ierr.ErrDatabase)
	}

	// Validate that target plan is different from current plan
	if currentPlan.ID == targetPlan.ID {
		return nil, ierr.NewError("cannot change subscription to the same plan").
			WithHint("Target plan must be different from current plan").
			Mark(ierr.ErrValidation)
	}

	// Check if subscription is in a valid state for changes
	if err := s.validateSubscriptionForChange(currentSub); err != nil {
		return nil, err
	}

	// Determine change type and validate it's allowed
	changeType, err := s.determineChangeType(ctx, currentPlan, targetPlan)
	if err != nil {
		return nil, err
	}

	// Calculate effective date
	effectiveDate := time.Now()

	// Calculate proration if needed
	var prorationDetails *dto.ProrationDetails
	if req.ProrationBehavior == types.ProrationBehaviorCreateProrations {
		prorationDetails, err = s.calculateProrationPreview(ctx, currentSub, lineItems, targetPlan, effectiveDate)
		if err != nil {
			logger.Error("failed to calculate proration preview", zap.Error(err))
			return nil, err
		}
	}

	// Calculate next invoice preview
	nextInvoice, err := s.calculateNextInvoicePreview(ctx, currentSub, targetPlan, effectiveDate)
	if err != nil {
		logger.Error("failed to calculate next invoice preview", zap.Error(err))
		return nil, err
	}

	// Calculate new billing cycle
	newBillingCycle, err := s.calculateNewBillingCycle(currentSub, targetPlan, types.BillingCycleAnchorUnchanged, effectiveDate)
	if err != nil {
		logger.Error("failed to calculate new billing cycle", zap.Error(err))
		return nil, err
	}

	// Generate warnings
	warnings := s.generateWarnings(currentSub, targetPlan, changeType, req.ProrationBehavior)

	response := &dto.SubscriptionChangePreviewResponse{
		SubscriptionID: subscriptionID,
		CurrentPlan: dto.PlanSummary{
			ID:          currentPlan.ID,
			Name:        currentPlan.Name,
			LookupKey:   currentPlan.LookupKey,
			Description: currentPlan.Description,
		},
		TargetPlan: dto.PlanSummary{
			ID:          targetPlan.ID,
			Name:        targetPlan.Name,
			LookupKey:   targetPlan.LookupKey,
			Description: targetPlan.Description,
		},
		ChangeType:         changeType,
		ProrationDetails:   prorationDetails,
		NextInvoicePreview: nextInvoice,
		EffectiveDate:      effectiveDate,
		NewBillingCycle:    *newBillingCycle,
		Warnings:           warnings,
		Metadata:           req.Metadata,
	}

	logger.Info("subscription change preview completed successfully")
	return response, nil
}

// ExecuteSubscriptionChange performs the actual subscription plan change
func (s *subscriptionChangeService) ExecuteSubscriptionChange(
	ctx context.Context,
	subscriptionID string,
	req dto.SubscriptionChangeRequest,
) (*dto.SubscriptionChangeExecuteResponse, error) {
	logger := s.serviceParams.Logger.With(
		zap.String("subscription_id", subscriptionID),
		zap.String("target_plan_id", req.TargetPlanID),
	)

	logger.Info("executing subscription change")

	// Validate the request
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// BRANCH: Determine execution timing
	// - If change_at is "period_end": schedule for end of billing period
	// - If change_at is "immediate" or not provided: execute immediately
	if req.ChangeAt != nil && *req.ChangeAt == types.ScheduleTypePeriodEnd {
		return s.scheduleChangeForPeriodEnd(ctx, subscriptionID, req)
	}

	// IMMEDIATE EXECUTION PATH (when change_at is "immediate")
	return s.ExecuteSubscriptionChangeInternal(ctx, subscriptionID, req)
}

// ExecuteSubscriptionChangeInternal executes a subscription change immediately
// This is the core execution logic used by both immediate API calls and scheduled execution
func (s *subscriptionChangeService) ExecuteSubscriptionChangeInternal(
	ctx context.Context,
	subscriptionID string,
	req dto.SubscriptionChangeRequest,
) (*dto.SubscriptionChangeExecuteResponse, error) {
	logger := s.serviceParams.Logger.With(
		zap.String("subscription_id", subscriptionID),
		zap.String("target_plan_id", req.TargetPlanID),
	)

	var response *dto.SubscriptionChangeExecuteResponse

	// Execute the change within a transaction
	err := s.serviceParams.DB.WithTx(ctx, func(txCtx context.Context) error {
		// Get current subscription with line items
		currentSub, lineItems, err := s.serviceParams.SubRepo.GetWithLineItems(txCtx, subscriptionID)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to retrieve subscription").
				Mark(ierr.ErrDatabase)
		}

		// Get current and target plans
		currentPlan, err := s.serviceParams.PlanRepo.Get(txCtx, currentSub.PlanID)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to retrieve current plan").
				Mark(ierr.ErrDatabase)
		}

		targetPlan, err := s.serviceParams.PlanRepo.Get(txCtx, req.TargetPlanID)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to retrieve target plan").
				Mark(ierr.ErrDatabase)
		}

		// Validate the change
		if err := s.validateSubscriptionForChange(currentSub); err != nil {
			return err
		}

		// Determine change type
		changeType, err := s.determineChangeType(txCtx, currentPlan, targetPlan)
		if err != nil {
			return err
		}

		// Calculate effective date
		effectiveDate := time.Now()

		// Execute the change based on type
		result, err := s.executeChange(txCtx, currentSub, lineItems, targetPlan, changeType, req, effectiveDate)
		if err != nil {
			return err
		}

		response = result
		return nil
	})

	if err != nil {
		logger.Error("failed to execute subscription change", zap.Error(err))
		return nil, err
	}

	logger.Info("subscription change executed successfully",
		zap.String("old_subscription_id", response.OldSubscription.ID),
		zap.String("new_subscription_id", response.NewSubscription.ID),
	)

	return response, nil
}

// scheduleChangeForPeriodEnd schedules a plan change to execute at period end
// This creates a database entry in subscription_schedules table
// The actual execution happens during period processing (cron or temporal workflow)
func (s *subscriptionChangeService) scheduleChangeForPeriodEnd(
	ctx context.Context,
	subscriptionID string,
	req dto.SubscriptionChangeRequest,
) (*dto.SubscriptionChangeExecuteResponse, error) {
	logger := s.serviceParams.Logger.With(
		zap.String("subscription_id", subscriptionID),
		zap.String("target_plan_id", req.TargetPlanID),
		zap.String("change_at", string(*req.ChangeAt)),
	)

	logger.Info("scheduling subscription change for period end")

	// Get subscription to calculate period end
	sub, err := s.serviceParams.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve subscription").
			Mark(ierr.ErrDatabase)
	}

	// Validate subscription is active
	if sub.SubscriptionStatus != types.SubscriptionStatusActive {
		return nil, ierr.NewError("subscription must be active to schedule changes").
			WithHint("Only active subscriptions can have scheduled plan changes").
			Mark(ierr.ErrValidation)
	}

	// Check if subscription is scheduled for cancellation at period end
	// If so, user must cancel the cancellation schedule first
	cancelSchedule, err := s.serviceParams.SubScheduleRepo.GetPendingBySubscriptionAndType(
		ctx,
		subscriptionID,
		types.SubscriptionScheduleChangeTypeCancellation,
	)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to check for existing cancellation schedule").
			Mark(ierr.ErrDatabase)
	}
	if cancelSchedule != nil {
		return nil, ierr.NewError("subscription is scheduled for cancellation at period end").
			WithHint("Cancel the pending cancellation schedule before scheduling a plan change").
			WithReportableDetails(map[string]any{
				"cancellation_schedule_id": cancelSchedule.ID,
				"scheduled_at":             cancelSchedule.ScheduledAt,
			}).
			Mark(ierr.ErrValidation)
	}

	// Check for existing pending plan change schedule
	existing, err := s.serviceParams.SubScheduleRepo.GetPendingBySubscriptionAndType(
		ctx,
		subscriptionID,
		types.SubscriptionScheduleChangeTypePlanChange,
	)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to check for existing schedules").
			Mark(ierr.ErrDatabase)
	}
	if existing != nil {
		return nil, ierr.NewError("a plan change is already scheduled for this subscription").
			WithHint("Cancel the existing scheduled plan change before creating a new one").
			WithReportableDetails(map[string]any{
				"existing_schedule_id": existing.ID,
				"scheduled_at":         existing.ScheduledAt,
			}).
			Mark(ierr.ErrValidation)
	}

	// Create configuration from request
	config := &subscription.PlanChangeConfiguration{
		TargetPlanID:       req.TargetPlanID,
		ProrationBehavior:  req.ProrationBehavior,
		BillingCadence:     req.BillingCadence,
		BillingPeriod:      req.BillingPeriod,
		BillingPeriodCount: req.BillingPeriodCount,
		BillingCycle:       req.BillingCycle,
		ChangeMetadata:     req.Metadata,
	}

	// Create the schedule entry
	schedule := &subscription.SubscriptionSchedule{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_SCHEDULE),
		SubscriptionID: subscriptionID,
		ScheduleType:   types.SubscriptionScheduleChangeTypePlanChange,
		ScheduledAt:    sub.CurrentPeriodEnd,
		Status:         types.ScheduleStatusPending,
		TenantID:       sub.TenantID,
		EnvironmentID:  sub.EnvironmentID,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
		CreatedBy:      types.GetUserID(ctx),
		UpdatedBy:      types.GetUserID(ctx),
		StatusColumn:   types.StatusPublished,
	}

	// Set configuration
	if err := schedule.SetPlanChangeConfig(config); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to serialize plan change configuration").
			Mark(ierr.ErrInternal)
	}

	// Save to database
	if err := s.serviceParams.SubScheduleRepo.Create(ctx, schedule); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to create schedule entry").
			Mark(ierr.ErrDatabase)
	}

	logger.Info("subscription change scheduled successfully",
		zap.String("schedule_id", schedule.ID),
		zap.Time("scheduled_at", schedule.ScheduledAt),
	)

	// Return response indicating the change was scheduled
	response := &dto.SubscriptionChangeExecuteResponse{
		IsScheduled:   true,
		ScheduleID:    &schedule.ID,
		ScheduledAt:   &schedule.ScheduledAt,
		ChangeType:    types.SubscriptionChangeTypeUpgrade, // Will be determined at execution time
		EffectiveDate: schedule.ScheduledAt,                // Scheduled execution time
	}

	return response, nil
}

// validateSubscriptionForChange checks if subscription can be changed
func (s *subscriptionChangeService) validateSubscriptionForChange(sub *subscription.Subscription) error {
	// Check subscription status
	switch sub.SubscriptionStatus {
	case types.SubscriptionStatusActive, types.SubscriptionStatusTrialing:
		// These are valid states for changes
	case types.SubscriptionStatusPaused:
		return ierr.NewError("cannot change paused subscription").
			WithHint("Resume the subscription before changing plans").
			Mark(ierr.ErrValidation)
	case types.SubscriptionStatusCancelled:
		return ierr.NewError("cannot change cancelled subscription").
			WithHint("Cancelled subscriptions cannot be changed").
			Mark(ierr.ErrValidation)
	default:
		return ierr.NewError("subscription not in valid state for changes").
			WithHint("Subscription must be active or trialing").
			WithReportableDetails(map[string]any{
				"current_status": sub.SubscriptionStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

// determineChangeType determines if this is an upgrade, downgrade, or lateral change
func (s *subscriptionChangeService) determineChangeType(
	ctx context.Context,
	currentPlan *plan.Plan,
	targetPlan *plan.Plan,
) (types.SubscriptionChangeType, error) {
	// Get prices for both plans to compare
	priceService := NewPriceService(s.serviceParams)

	currentPricesResponse, err := priceService.GetPricesByPlanID(ctx, dto.GetPricesByPlanRequest{
		PlanID:       currentPlan.ID,
		AllowExpired: false,
	})
	if err != nil {
		return "", err
	}

	targetPricesResponse, err := priceService.GetPricesByPlanID(ctx, dto.GetPricesByPlanRequest{
		PlanID:       targetPlan.ID,
		AllowExpired: false,
	})
	if err != nil {
		return "", err
	}

	// Calculate plan values for comparison
	currentValue := s.calculatePlanValue(currentPricesResponse.Items)
	targetValue := s.calculatePlanValue(targetPricesResponse.Items)

	if targetValue.GreaterThan(currentValue) {
		return types.SubscriptionChangeTypeUpgrade, nil
	} else if targetValue.LessThan(currentValue) {
		return types.SubscriptionChangeTypeDowngrade, nil
	}

	return types.SubscriptionChangeTypeLateral, nil
}

// calculatePlanValue calculates a simple total value for plan comparison
func (s *subscriptionChangeService) calculatePlanValue(priceResponses []*dto.PriceResponse) decimal.Decimal {
	total := decimal.Zero
	for _, priceResp := range priceResponses {
		if !priceResp.Price.Amount.IsZero() {
			total = total.Add(priceResp.Price.Amount)
		}
	}
	return total
}

// calculateProrationPreview calculates proration amounts for preview using cancellation proration method
func (s *subscriptionChangeService) calculateProrationPreview(
	ctx context.Context,
	currentSub *subscription.Subscription,
	lineItems []*subscription.SubscriptionLineItem,
	targetPlan *plan.Plan,
	effectiveDate time.Time,
) (*dto.ProrationDetails, error) {
	// Use the same cancellation proration method for consistency
	prorationService := NewProrationService(s.serviceParams)
	prorationResult, err := prorationService.CalculateSubscriptionCancellationProration(
		ctx,
		currentSub,
		lineItems,
		types.CancellationTypeImmediate,
		effectiveDate,
		"subscription_change_preview",
		types.ProrationBehaviorCreateProrations,
	)
	if err != nil {
		return nil, err
	}

	// Convert to DTO format
	return s.convertCancellationProrationToDetails(prorationResult, effectiveDate, currentSub), nil
}

// calculateNextInvoicePreview calculates how the next regular invoice would be affected
func (s *subscriptionChangeService) calculateNextInvoicePreview(
	ctx context.Context,
	currentSub *subscription.Subscription,
	targetPlan *plan.Plan,
	effectiveDate time.Time,
) (*dto.InvoicePreview, error) {
	// Get target plan prices
	priceService := NewPriceService(s.serviceParams)
	targetPricesResponse, err := priceService.GetPricesByPlanID(ctx, dto.GetPricesByPlanRequest{
		PlanID:       targetPlan.ID,
		AllowExpired: false,
	})
	if err != nil {
		return nil, err
	}

	// Calculate what the next invoice would look like with the new plan
	lineItems := []dto.InvoiceLineItemPreview{}
	subtotal := decimal.Zero

	for _, priceResp := range targetPricesResponse.Items {
		p := priceResp.Price
		if !p.Amount.IsZero() && p.Amount.GreaterThan(decimal.Zero) {
			description := fmt.Sprintf("%s - %s", targetPlan.Name, p.Description)
			if p.Description == "" {
				description = fmt.Sprintf("%s - Price", targetPlan.Name)
			}
			lineItems = append(lineItems, dto.InvoiceLineItemPreview{
				Description: description,
				Amount:      p.Amount,
				Quantity:    decimal.NewFromInt(1),
				UnitPrice:   p.Amount,
				IsProration: false,
			})
			subtotal = subtotal.Add(p.Amount)
		}
	}

	return &dto.InvoicePreview{
		Subtotal:  subtotal,
		TaxAmount: decimal.Zero,
		Total:     subtotal,
		Currency:  currentSub.Currency,
		LineItems: lineItems,
	}, nil
}

// calculateNewBillingCycle calculates the new billing cycle information
func (s *subscriptionChangeService) calculateNewBillingCycle(
	currentSub *subscription.Subscription,
	targetPlan *plan.Plan,
	billingCycleAnchor types.BillingCycleAnchor,
	effectiveDate time.Time,
) (*dto.BillingCycleInfo, error) {
	newPeriodStart := effectiveDate
	newBillingAnchor := currentSub.BillingAnchor

	// Adjust based on billing cycle anchor
	switch billingCycleAnchor {
	case types.BillingCycleAnchorReset, types.BillingCycleAnchorImmediate:
		newBillingAnchor = effectiveDate
		newPeriodStart = effectiveDate
	case types.BillingCycleAnchorUnchanged:
		// Keep current billing anchor
	}

	return &dto.BillingCycleInfo{
		PeriodStart:        newPeriodStart,
		PeriodEnd:          s.calculatePeriodEnd(newPeriodStart, currentSub.BillingPeriod, currentSub.BillingPeriodCount),
		BillingAnchor:      newBillingAnchor,
		BillingCadence:     currentSub.BillingCadence,
		BillingPeriod:      currentSub.BillingPeriod,
		BillingPeriodCount: currentSub.BillingPeriodCount,
	}, nil
}

// calculatePeriodEnd calculates the end of a billing period
func (s *subscriptionChangeService) calculatePeriodEnd(
	start time.Time,
	period types.BillingPeriod,
	count int,
) time.Time {
	switch period {
	case types.BILLING_PERIOD_DAILY:
		return start.AddDate(0, 0, count)
	case types.BILLING_PERIOD_WEEKLY:
		return start.AddDate(0, 0, count*7)
	case types.BILLING_PERIOD_MONTHLY:
		return start.AddDate(0, count, 0)
	case types.BILLING_PERIOD_ANNUAL:
		return start.AddDate(count, 0, 0)
	case types.BILLING_PERIOD_QUARTER:
		return start.AddDate(0, count*3, 0)
	case types.BILLING_PERIOD_HALF_YEAR:
		return start.AddDate(0, count*6, 0)
	default:
		return start.AddDate(0, 1, 0) // Default to 1 month
	}
}

// generateWarnings generates warnings about the subscription change
func (s *subscriptionChangeService) generateWarnings(
	currentSub *subscription.Subscription,
	_ *plan.Plan,
	changeType types.SubscriptionChangeType,
	prorationBehavior types.ProrationBehavior,
) []string {
	var warnings []string

	if changeType == types.SubscriptionChangeTypeDowngrade {
		warnings = append(warnings, "This is a downgrade. You may lose access to certain features.")
	}

	if currentSub.TrialEnd != nil && currentSub.TrialEnd.After(time.Now()) {
		warnings = append(warnings, "Changing plans during trial period may end your trial immediately.")
	}

	if prorationBehavior == types.ProrationBehaviorCreateProrations {
		warnings = append(warnings, "Proration charges or credits will be applied to your next invoice.")
	}

	return warnings
}

// executeChange performs the actual subscription change
func (s *subscriptionChangeService) executeChange(
	ctx context.Context,
	currentSub *subscription.Subscription,
	lineItems []*subscription.SubscriptionLineItem,
	targetPlan *plan.Plan,
	changeType types.SubscriptionChangeType,
	req dto.SubscriptionChangeRequest,
	effectiveDate time.Time,
) (*dto.SubscriptionChangeExecuteResponse, error) {
	// Cancel the old subscription
	subscriptionService := NewSubscriptionService(s.serviceParams)
	archivedSub, err := subscriptionService.CancelSubscription(ctx, currentSub.ID, &dto.CancelSubscriptionRequest{
		CancellationType: types.CancellationTypeImmediate,
		Reason:           "subscription_change",
	})
	if err != nil {
		return nil, err
	}

	// Create new subscription
	newSub, err := s.createNewSubscription(ctx, currentSub, lineItems, targetPlan, req, effectiveDate)
	if err != nil {
		return nil, err
	}

	return &dto.SubscriptionChangeExecuteResponse{
		OldSubscription: dto.SubscriptionSummary{
			ID:     archivedSub.SubscriptionID,
			Status: archivedSub.Status,
		},
		NewSubscription: dto.SubscriptionSummary{
			ID:                 newSub.ID,
			Status:             newSub.SubscriptionStatus,
			PlanID:             newSub.PlanID,
			CurrentPeriodStart: newSub.CurrentPeriodStart,
			CurrentPeriodEnd:   newSub.CurrentPeriodEnd,
			BillingAnchor:      newSub.BillingAnchor,
			CreatedAt:          newSub.CreatedAt,
		},
		ChangeType:    changeType,
		EffectiveDate: effectiveDate,
		Metadata:      req.Metadata,
	}, nil
}

// createNewSubscription creates a new subscription with the target plan using the existing subscription service
func (s *subscriptionChangeService) createNewSubscription(
	ctx context.Context,
	currentSub *subscription.Subscription,
	oldLineItems []*subscription.SubscriptionLineItem,
	targetPlan *plan.Plan,
	req dto.SubscriptionChangeRequest,
	effectiveDate time.Time,
) (*subscription.Subscription, error) {
	// Create new subscription request
	createSubReq := dto.CreateSubscriptionRequest{
		CustomerID:         currentSub.CustomerID,
		PlanID:             targetPlan.ID,
		Currency:           currentSub.Currency,
		LookupKey:          currentSub.LookupKey,
		BillingCadence:     req.BillingCadence,
		BillingPeriod:      req.BillingPeriod,
		BillingPeriodCount: req.BillingPeriodCount,
		BillingCycle:       req.BillingCycle,
		BillingAnchor:      &currentSub.BillingAnchor,
		StartDate:          &effectiveDate,
		Metadata:           req.Metadata,
		ProrationBehavior:  req.ProrationBehavior,
		CustomerTimezone:   currentSub.CustomerTimezone,
		CommitmentAmount:   currentSub.CommitmentAmount,
		OverageFactor:      currentSub.OverageFactor,
		Workflow:           lo.ToPtr(types.TemporalSubscriptionCreationWorkflow),
	}

	subscriptionService := NewSubscriptionService(s.serviceParams)
	response, err := subscriptionService.CreateSubscription(ctx, createSubReq)
	if err != nil {
		return nil, err
	}

	// Get the created subscription with line items
	newSub, newLineItems, err := s.serviceParams.SubRepo.GetWithLineItems(ctx, response.Subscription.ID)
	if err != nil {
		return nil, err
	}

	// Handle entitlement proration for subscription changes
	// This handles both anniversary and calendar billing cycles
	s.serviceParams.Logger.Infow("checking entitlement proration condition",
		"req_proration_behavior", req.ProrationBehavior,
		"expected_value", types.ProrationBehaviorCreateProrations,
		"will_execute", req.ProrationBehavior == types.ProrationBehaviorCreateProrations,
		"old_subscription_id", currentSub.ID,
		"new_subscription_id", newSub.ID)

	if req.ProrationBehavior == types.ProrationBehaviorCreateProrations {
		if err := s.handleSubscriptionChangeEntitlementProration(
			ctx,
			currentSub,
			newSub,
			targetPlan,
			effectiveDate,
		); err != nil {
			// Log error but don't fail the change
			s.serviceParams.Logger.Errorw("failed to create prorated entitlements for plan change",
				"error", err,
				"old_subscription_id", currentSub.ID,
				"new_subscription_id", newSub.ID)
		}
	}

	// Transfer line item coupons
	if err := s.transferLineItemCoupons(ctx, currentSub.ID, newSub, oldLineItems, newLineItems); err != nil {
		return nil, err
	}

	return newSub, nil
}

// handleSubscriptionChangeEntitlementProration handles entitlement proration for subscription plan changes
func (s *subscriptionChangeService) handleSubscriptionChangeEntitlementProration(
	ctx context.Context,
	oldSub *subscription.Subscription,
	newSub *subscription.Subscription,
	targetPlan *plan.Plan,
	effectiveDate time.Time,
) error {
	s.serviceParams.Logger.Infow("handling entitlement proration for subscription change",
		"old_subscription_id", oldSub.ID,
		"new_subscription_id", newSub.ID,
		"target_plan_id", targetPlan.ID,
		"billing_cycle", newSub.BillingCycle,
		"effective_date", effectiveDate)

	// Get proration service
	prorationService := NewProrationService(s.serviceParams)

	// Calculate ADDITIVE entitlement proration (old remaining + new prorated)
	// This ensures customers don't lose unused entitlements when changing plans
	prorationResult, err := prorationService.CalculateAdditiveEntitlementProration(
		ctx,
		oldSub.PlanID, // Old plan ID
		targetPlan.ID, // New plan ID
		oldSub.CurrentPeriodStart,
		oldSub.CurrentPeriodEnd,
		effectiveDate, // Change date
		newSub.CustomerTimezone,
		newSub.BillingCycle,
		newSub.BillingAnchor,
		newSub.BillingPeriod,
		newSub.BillingPeriodCount,
	)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to calculate additive entitlement proration for plan change").
			Mark(ierr.ErrSystem)
	}

	// Create prorated entitlements for the new subscription
	if err = prorationService.CreateProratedEntitlements(ctx, newSub.ID, prorationResult, effectiveDate, newSub.CurrentPeriodEnd); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to create prorated entitlements for plan change").
			Mark(ierr.ErrSystem)
	}

	s.serviceParams.Logger.Infow("additive entitlement proration completed for subscription change",
		"new_subscription_id", newSub.ID,
		"prorated_count", len(prorationResult.ProratedLimits),
		"coefficient", prorationResult.ProrationCoefficient.String(),
		"is_additive", prorationResult.IsAdditive,
		"old_plan_features", len(prorationResult.OldPlanContribution),
		"new_plan_features", len(prorationResult.NewPlanContribution))

	return nil
}

// convertCancellationProrationToDetails converts cancellation proration result to DTO format
func (s *subscriptionChangeService) convertCancellationProrationToDetails(
	prorationResult *proration.SubscriptionProrationResult,
	effectiveDate time.Time,
	currentSub *subscription.Subscription,
) *dto.ProrationDetails {
	if prorationResult == nil {
		return nil
	}

	// Calculate totals from line item results
	creditAmount := decimal.Zero
	chargeAmount := decimal.Zero

	for _, lineResult := range prorationResult.LineItemResults {
		for _, creditItem := range lineResult.CreditItems {
			creditAmount = creditAmount.Add(creditItem.Amount.Abs()) // Ensure positive for credit amount
		}
		for _, chargeItem := range lineResult.ChargeItems {
			chargeAmount = chargeAmount.Add(chargeItem.Amount)
		}
	}

	// Net amount is the total proration amount (negative for credits, positive for charges)
	netAmount := prorationResult.TotalProrationAmount

	// Calculate days
	daysUsed := int(effectiveDate.Sub(currentSub.CurrentPeriodStart).Hours() / 24)
	daysRemaining := int(currentSub.CurrentPeriodEnd.Sub(effectiveDate).Hours() / 24)
	if daysRemaining < 0 {
		daysRemaining = 0
	}

	return &dto.ProrationDetails{
		CreditAmount:       creditAmount,
		CreditDescription:  fmt.Sprintf("Credit for unused time on %s plan", currentSub.PlanID),
		ChargeAmount:       chargeAmount,
		ChargeDescription:  fmt.Sprintf("Charge for plan change from %s", effectiveDate.Format("2006-01-02")),
		NetAmount:          netAmount,
		ProrationDate:      effectiveDate,
		CurrentPeriodStart: currentSub.CurrentPeriodStart,
		CurrentPeriodEnd:   currentSub.CurrentPeriodEnd,
		DaysUsed:           daysUsed,
		DaysRemaining:      daysRemaining,
		Currency:           currentSub.Currency,
	}
}

// transferLineItemCoupons transfers line item specific coupons from old subscription to new subscription
func (s *subscriptionChangeService) transferLineItemCoupons(
	ctx context.Context,
	oldSubscriptionID string,
	newSubscription *subscription.Subscription,
	oldLineItems, newLineItems []*subscription.SubscriptionLineItem,
) error {
	// Get active coupon associations from old subscription
	oldSub, err := s.serviceParams.SubRepo.Get(ctx, oldSubscriptionID)
	if err != nil {
		return err
	}

	filter := types.NewCouponAssociationFilter()
	filter.SubscriptionIDs = []string{oldSub.ID}
	filter.ActiveOnly = true
	filter.PeriodStart = &oldSub.CurrentPeriodStart
	filter.PeriodEnd = &oldSub.CurrentPeriodEnd

	lineItemCoupons, err := s.serviceParams.CouponAssociationRepo.List(ctx, filter)
	if err != nil {
		return err
	}

	if len(lineItemCoupons) == 0 {
		return nil
	}

	// Create price mapping for efficient lookup
	priceToNewLineItem := make(map[string]*subscription.SubscriptionLineItem)
	for _, lineItem := range newLineItems {
		priceToNewLineItem[lineItem.PriceID] = lineItem
	}

	// Group coupons by line item and transfer
	couponService := NewCouponAssociationService(s.serviceParams)

	for _, couponAssoc := range lineItemCoupons {
		if couponAssoc.SubscriptionLineItemID == nil {
			continue
		}

		// Find old line item
		var oldLineItem *subscription.SubscriptionLineItem
		for _, li := range oldLineItems {
			if li.ID == *couponAssoc.SubscriptionLineItemID {
				oldLineItem = li
				break
			}
		}

		if oldLineItem == nil || priceToNewLineItem[oldLineItem.PriceID] == nil {
			continue
		}

		// Transfer coupon to new subscription
		couponRequest := []dto.SubscriptionCouponRequest{{
			CouponID:   couponAssoc.CouponID,
			LineItemID: &oldLineItem.ID,
			StartDate:  couponAssoc.StartDate,
			EndDate:    couponAssoc.EndDate,
		}}

		if err := couponService.ApplyCouponsToSubscription(ctx, newSubscription, couponRequest); err != nil {
			s.serviceParams.Logger.Errorw("failed to transfer coupon", "error", err)
			continue
		}
	}

	return nil
}
