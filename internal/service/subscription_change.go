package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
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
}

type subscriptionChangeService struct {
	serviceParams       ServiceParams
	invoiceService      InvoiceService
	prorationService    proration.Service
	subscriptionService SubscriptionService
}

// NewSubscriptionChangeService creates a new subscription change service
func NewSubscriptionChangeService(serviceParams ServiceParams) SubscriptionChangeService {
	return &subscriptionChangeService{
		serviceParams:       serviceParams,
		invoiceService:      NewInvoiceService(serviceParams),
		prorationService:    NewProrationService(serviceParams),
		subscriptionService: NewSubscriptionService(serviceParams),
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

	// Validate plans are in same environment
	if currentPlan.EnvironmentID != targetPlan.EnvironmentID {
		return nil, ierr.NewError("plans must be in the same environment").
			WithHint("Cannot change between plans in different environments").
			Mark(ierr.ErrValidation)
	}

	// Validate that target plan is different from current plan
	if currentPlan.ID == targetPlan.ID {
		return nil, ierr.NewError("cannot change subscription to the same plan").
			WithHint("Target plan must be different from current plan").
			WithReportableDetails(map[string]interface{}{
				"current_plan_id": currentPlan.ID,
				"target_plan_id":  targetPlan.ID,
			}).
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

		// Validate that target plan is different from current plan
		if currentPlan.ID == targetPlan.ID {
			return ierr.NewError("cannot change subscription to the same plan").
				WithHint("Target plan must be different from current plan").
				WithReportableDetails(map[string]interface{}{
					"current_plan_id": currentPlan.ID,
					"target_plan_id":  targetPlan.ID,
				}).
				Mark(ierr.ErrValidation)
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
		// effectiveDate := time.Date(2025, 9, 15, 12, 0, 0, 0, time.UTC)

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
	// For now, we'll use a simple comparison based on plan names or could be enhanced
	// with pricing comparison logic in the future

	// If same plan, it's a lateral change (though this might not make sense)
	if currentPlan.ID == targetPlan.ID {
		return types.SubscriptionChangeTypeLateral, nil
	}

	// Get prices for both plans to compare
	priceService := NewPriceService(s.serviceParams)

	currentPricesResponse, err := priceService.GetPricesByPlanID(ctx, currentPlan.ID)
	if err != nil {
		return "", ierr.WithError(err).
			WithHint("Failed to get current plan prices").
			Mark(ierr.ErrDatabase)
	}

	targetPricesResponse, err := priceService.GetPricesByPlanID(ctx, targetPlan.ID)
	if err != nil {
		return "", ierr.WithError(err).
			WithHint("Failed to get target plan prices").
			Mark(ierr.ErrDatabase)
	}

	// Extract price objects from response
	currentPrices := make([]*price.Price, len(currentPricesResponse.Items))
	for i, item := range currentPricesResponse.Items {
		currentPrices[i] = item.Price
	}

	targetPrices := make([]*price.Price, len(targetPricesResponse.Items))
	for i, item := range targetPricesResponse.Items {
		targetPrices[i] = item.Price
	}

	// Simple comparison based on total plan value
	// In a real implementation, this would be more sophisticated
	currentValue := s.calculatePlanValue(currentPrices)
	targetValue := s.calculatePlanValue(targetPrices)

	if targetValue.GreaterThan(currentValue) {
		return types.SubscriptionChangeTypeUpgrade, nil
	} else if targetValue.LessThan(currentValue) {
		return types.SubscriptionChangeTypeDowngrade, nil
	}

	return types.SubscriptionChangeTypeLateral, nil
}

// calculatePlanValue calculates a simple total value for plan comparison
func (s *subscriptionChangeService) calculatePlanValue(prices []*price.Price) decimal.Decimal {
	total := decimal.Zero
	for _, p := range prices {
		if !p.Amount.IsZero() {
			total = total.Add(p.Amount)
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
	prorationResult, err := s.prorationService.CalculateSubscriptionCancellationProration(
		ctx,
		currentSub,
		lineItems,
		types.CancellationTypeImmediate, // Treat subscription change as immediate cancellation
		effectiveDate,
		"subscription_change_preview",
		types.ProrationBehaviorCreateProrations,
	)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to calculate cancellation proration for preview").
			Mark(ierr.ErrSystem)
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
	targetPricesResponse, err := priceService.GetPricesByPlanID(ctx, targetPlan.ID)
	if err != nil {
		return nil, err
	}

	// Extract price objects from response
	targetPrices := make([]*price.Price, len(targetPricesResponse.Items))
	for i, item := range targetPricesResponse.Items {
		targetPrices[i] = item.Price
	}

	// Calculate what the next invoice would look like with the new plan
	lineItems := []dto.InvoiceLineItemPreview{}
	subtotal := decimal.Zero

	for _, p := range targetPrices {
		if !p.Amount.IsZero() && p.Amount.GreaterThan(decimal.Zero) {
			amount := p.Amount
			description := fmt.Sprintf("%s - %s", targetPlan.Name, p.Description)
			if p.Description == "" {
				description = fmt.Sprintf("%s - Price", targetPlan.Name)
			}
			lineItems = append(lineItems, dto.InvoiceLineItemPreview{
				Description: description,
				Amount:      amount,
				Quantity:    decimal.NewFromInt(1),
				UnitPrice:   amount,
				IsProration: false,
			})
			subtotal = subtotal.Add(amount)
		}
	}

	// For simplicity, assume no taxes in preview
	taxAmount := decimal.Zero
	total := subtotal.Add(taxAmount)

	return &dto.InvoicePreview{
		Subtotal:  subtotal,
		TaxAmount: taxAmount,
		Total:     total,
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
	// Get target plan prices to determine billing cadence
	// For simplicity, we'll use the current subscription's billing info
	// In a real implementation, this would be derived from the target plan

	newPeriodStart := effectiveDate
	newBillingAnchor := currentSub.BillingAnchor

	// Adjust based on billing cycle anchor
	switch billingCycleAnchor {
	case types.BillingCycleAnchorReset:
		newBillingAnchor = effectiveDate
	case types.BillingCycleAnchorImmediate:
		newPeriodStart = effectiveDate
		newBillingAnchor = effectiveDate
	case types.BillingCycleAnchorUnchanged:
		// Keep current billing anchor
	}

	// Calculate new period end based on billing period
	newPeriodEnd := s.calculatePeriodEnd(newPeriodStart, currentSub.BillingPeriod, currentSub.BillingPeriodCount)

	return &dto.BillingCycleInfo{
		PeriodStart:        newPeriodStart,
		PeriodEnd:          newPeriodEnd,
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
	targetPlan *plan.Plan,
	changeType types.SubscriptionChangeType,
	prorationBehavior types.ProrationBehavior,
) []string {
	warnings := []string{}

	// Warning for downgrades
	if changeType == types.SubscriptionChangeTypeDowngrade {
		warnings = append(warnings, "This is a downgrade. You may lose access to certain features.")
	}

	// Warning for trial subscriptions
	if currentSub.TrialEnd != nil && currentSub.TrialEnd.After(time.Date(2025, 9, 15, 12, 0, 0, 0, time.UTC)) {
		warnings = append(warnings, "Changing plans during trial period may end your trial immediately.")
	}

	// Warning about proration
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

	// Step 1: Archive the old subscription
	subscriptionService := NewSubscriptionService(s.serviceParams)
	archivedSub, err := subscriptionService.CancelSubscription(ctx, currentSub.ID, &dto.CancelSubscriptionRequest{
		CancellationType: types.CancellationTypeImmediate,
		Reason:           "subscription_change",
	})
	if err != nil {
		return nil, err
	}

	// Step 2: Create new subscription with credit grants
	newSub, err := s.createNewSubscription(ctx, currentSub, lineItems, targetPlan, req, effectiveDate)
	if err != nil {
		return nil, err
	}

	response := &dto.SubscriptionChangeExecuteResponse{
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
	}

	return response, nil
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

	// Build create subscription request based on current subscription but with target plan's billing info
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
		EndDate:            nil,
		Metadata:           req.Metadata,
		ProrationBehavior:  req.ProrationBehavior,
		CustomerTimezone:   currentSub.CustomerTimezone,
		CommitmentAmount:   currentSub.CommitmentAmount,
		OverageFactor:      currentSub.OverageFactor,
		Workflow:           lo.ToPtr(types.TemporalSubscriptionCreationWorkflow),
	}

	// Use the existing subscription service to create the new subscription
	response, err := s.subscriptionService.CreateSubscription(ctx, createSubReq)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to create new subscription using subscription service").
			Mark(ierr.ErrDatabase)
	}

	// Get the created subscription with line items
	newSub, newLineItems, err := s.serviceParams.SubRepo.GetWithLineItems(ctx, response.Subscription.ID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve newly created subscription").
			Mark(ierr.ErrDatabase)
	}

	// Transfer line item coupons from old subscription to new subscription
	err = s.transferLineItemCoupons(ctx, currentSub.ID, newSub.ID, oldLineItems, newLineItems)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to transfer line item coupons").
			Mark(ierr.ErrDatabase)
	}

	return newSub, nil
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

// prepareProrationCreditGrantRequests prepares credit grant requests for proration credits
func (s *subscriptionChangeService) prepareProrationCreditGrantRequests(
	cancellationProrationResult *proration.SubscriptionProrationResult,
	effectiveDate time.Time,
) []dto.CreateCreditGrantRequest {
	if cancellationProrationResult == nil {
		return nil
	}

	var creditGrantRequests []dto.CreateCreditGrantRequest

	// Process each line item result to create credit grant requests for credit items
	for lineItemID, prorationResult := range cancellationProrationResult.LineItemResults {
		for _, creditItem := range prorationResult.CreditItems {
			// Only create credit grants for positive credit amounts
			creditAmount := creditItem.Amount.Abs()
			if creditAmount.IsZero() {
				continue
			}

			// Create credit grant request (subscription ID will be set by the subscription service)
			// Based on the sample API request format
			creditGrantReq := dto.CreateCreditGrantRequest{
				Name:                   fmt.Sprintf("Subscription Change Credit - %s", effectiveDate.Format("2006-01-02")),
				Scope:                  types.CreditGrantScopeSubscription,
				SubscriptionID:         lo.ToPtr(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION)),
				Credits:                creditAmount,
				Cadence:                types.CreditGrantCadenceOneTime,
				ExpirationType:         types.CreditGrantExpiryTypeNever,
				ExpirationDuration:     nil,                                               // Explicitly set to nil for NEVER expiration
				ExpirationDurationUnit: lo.ToPtr(types.CreditGrantExpiryDurationUnitDays), // Explicitly set to nil for NEVER expiration
				Priority:               lo.ToPtr(1),
				Metadata: types.Metadata{
					"subscription_change":    "true",
					"original_line_item_id":  lineItemID,
					"proration_date":         effectiveDate.Format(time.RFC3339),
					"credit_description":     creditItem.Description,
					"proration_period_start": creditItem.StartDate.Format(time.RFC3339),
					"proration_period_end":   creditItem.EndDate.Format(time.RFC3339),
				},
			}

			creditGrantRequests = append(creditGrantRequests, creditGrantReq)

			s.serviceParams.Logger.Infow("prepared proration credit grant request",
				"amount", creditAmount.String(),
				"line_item_id", lineItemID,
				"description", creditItem.Description)
		}
	}

	return creditGrantRequests
}

// generateChangeInvoiceChargesOnly generates an invoice with only positive charges (no credits)
func (s *subscriptionChangeService) generateChangeInvoiceChargesOnly(
	ctx context.Context,
	newSub *subscription.Subscription,
	cancellationProrationResult *proration.SubscriptionProrationResult,
	effectiveDate time.Time,
) (*dto.InvoiceResponse, error) {
	if cancellationProrationResult == nil {
		return nil, nil
	}

	// Calculate total charge amount
	totalChargeAmount := decimal.Zero
	lineItems := []dto.CreateInvoiceLineItemRequest{}

	// Only add charge items (positive amounts) to the invoice
	for lineItemID, prorationResult := range cancellationProrationResult.LineItemResults {
		for _, chargeItem := range prorationResult.ChargeItems {
			if chargeItem.Amount.GreaterThan(decimal.Zero) {
				displayName := chargeItem.Description
				lineItems = append(lineItems, dto.CreateInvoiceLineItemRequest{
					EntityID:    &lineItemID,
					EntityType:  lo.ToPtr("subscription_line_item"),
					DisplayName: &displayName,
					Amount:      chargeItem.Amount, // Positive charges only
					Quantity:    chargeItem.Quantity,
					PeriodStart: &chargeItem.StartDate,
					PeriodEnd:   &chargeItem.EndDate,
					Metadata: types.Metadata{
						"proration_type":        "charge",
						"subscription_change":   "true",
						"original_line_item_id": lineItemID,
					},
				})
				totalChargeAmount = totalChargeAmount.Add(chargeItem.Amount)
			}
		}
	}

	// If no charges, don't create an invoice
	if totalChargeAmount.IsZero() || len(lineItems) == 0 {
		return nil, nil
	}

	// Create invoice request for charges only
	invoiceReq := dto.CreateInvoiceRequest{
		CustomerID:     newSub.CustomerID,
		SubscriptionID: &newSub.ID,
		InvoiceType:    types.InvoiceTypeSubscription,
		BillingReason:  types.InvoiceBillingReasonSubscriptionUpdate,
		Description:    fmt.Sprintf("Subscription change charges - %s", effectiveDate.Format("2006-01-02")),
		Currency:       newSub.Currency,
		PeriodStart:    &effectiveDate,
		PeriodEnd:      &effectiveDate,
		Subtotal:       totalChargeAmount,
		AmountDue:      totalChargeAmount,
		LineItems:      lineItems,
		Metadata: types.Metadata{
			"subscription_change": "true",
			"effective_date":      effectiveDate.Format(time.RFC3339),
			"charges_only":        "true", // Indicate this invoice contains only charges
		},
	}

	// Create the invoice
	return s.invoiceService.CreateInvoice(ctx, invoiceReq)
}

func (s *subscriptionChangeService) transferCoupons(ctx context.Context, oldSubscriptionID string) ([]string, error) {
	// Create filter to get coupon associations for the subscription

	// Get coupon associations
	couponAssociations, err := s.serviceParams.CouponAssociationRepo.GetBySubscription(ctx, oldSubscriptionID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to fetch coupon associations").
			Mark(ierr.ErrDatabase)
	}

	// Convert to string format
	var couponIDs []string
	for _, association := range couponAssociations {
		couponIDs = append(couponIDs, association.CouponID)
	}

	return couponIDs, nil
}

// transferAddonAssociations fetches and transfers addon associations from old subscription
func (s *subscriptionChangeService) transferAddonAssociations(ctx context.Context, oldSubscriptionID string) ([]dto.AddAddonToSubscriptionRequest, error) {
	// Create filter to get addon associations for the subscription
	filter := types.NewAddonAssociationFilter()
	filter.EntityIDs = []string{oldSubscriptionID}
	filter.EntityType = lo.ToPtr(types.AddonAssociationEntityTypeSubscription)
	filter.AddonStatus = lo.ToPtr(string(types.AddonStatusActive))

	// Get addon associations
	addonAssociations, err := s.serviceParams.AddonAssociationRepo.List(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to fetch addon associations").
			Mark(ierr.ErrDatabase)
	}

	// Convert to AddAddonToSubscriptionRequest format
	var addons []dto.AddAddonToSubscriptionRequest
	for _, association := range addonAssociations {
		// Only transfer active addons
		if association.AddonStatus == types.AddonStatusActive {
			addon := dto.AddAddonToSubscriptionRequest{
				AddonID:   association.AddonID,
				StartDate: association.StartDate,
				EndDate:   association.EndDate,
				Metadata:  association.Metadata,
			}
			addons = append(addons, addon)
		}
	}

	s.serviceParams.Logger.Infow("transferred addon associations",
		"old_subscription_id", oldSubscriptionID,
		"addon_count", len(addons))

	return addons, nil
}

// transferTaxAssociations fetches and transfers tax associations from old subscription
func (s *subscriptionChangeService) transferTaxAssociations(ctx context.Context, oldSubscriptionID string) ([]*dto.TaxRateOverride, error) {
	// Create filter to get tax associations for the subscription
	filter := types.NewNoLimitTaxAssociationFilter()
	filter.EntityType = types.TaxRateEntityTypeSubscription
	filter.EntityID = oldSubscriptionID

	// Get tax associations using tax service
	taxService := NewTaxService(s.serviceParams)
	taxAssociationsResponse, err := taxService.ListTaxAssociations(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to fetch tax associations").
			Mark(ierr.ErrDatabase)
	}

	// Convert to TaxRateOverride format
	var taxRateOverrides []*dto.TaxRateOverride
	for _, association := range taxAssociationsResponse.Items {
		// Get the tax rate details
		taxRate, err := taxService.GetTaxRate(ctx, association.TaxRateID)
		if err != nil {
			s.serviceParams.Logger.Warnw("failed to get tax rate details, skipping",
				"tax_rate_id", association.TaxRateID,
				"error", err)
			continue
		}

		override := &dto.TaxRateOverride{
			TaxRateCode: taxRate.TaxRate.Code,
			Priority:    association.Priority,
			AutoApply:   association.AutoApply,
			Currency:    association.Currency,
			Metadata:    association.Metadata,
		}
		taxRateOverrides = append(taxRateOverrides, override)
	}

	s.serviceParams.Logger.Infow("transferred tax associations",
		"old_subscription_id", oldSubscriptionID,
		"tax_override_count", len(taxRateOverrides))

	return taxRateOverrides, nil
}

// transferLineItemCoupons transfers line item specific coupons from old subscription to new subscription
func (s *subscriptionChangeService) transferLineItemCoupons(
	ctx context.Context,
	oldSubscriptionID, newSubscriptionID string,
	oldLineItems, newLineItems []*subscription.SubscriptionLineItem,
) error {
	// Get line item coupon associations from old subscription
	couponAssociationRepo := s.serviceParams.CouponAssociationRepo
	lineItemCoupons, err := couponAssociationRepo.GetBySubscriptionForLineItems(ctx, oldSubscriptionID)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to fetch line item coupon associations").
			Mark(ierr.ErrDatabase)
	}

	if len(lineItemCoupons) == 0 {
		s.serviceParams.Logger.Infow("no line item coupons to transfer",
			"old_subscription_id", oldSubscriptionID)
		return nil
	}

	// Create mapping from price_id to line item for both old and new subscriptions
	oldPriceToLineItem := make(map[string]*subscription.SubscriptionLineItem)
	for _, lineItem := range oldLineItems {
		oldPriceToLineItem[lineItem.PriceID] = lineItem
	}

	newPriceToLineItem := make(map[string]*subscription.SubscriptionLineItem)
	for _, lineItem := range newLineItems {
		newPriceToLineItem[lineItem.PriceID] = lineItem
	}

	// Group coupons by old line item ID
	couponsByOldLineItem := make(map[string][]string)
	for _, couponAssoc := range lineItemCoupons {
		if couponAssoc.SubscriptionLineItemID != nil {
			oldLineItemID := *couponAssoc.SubscriptionLineItemID
			couponsByOldLineItem[oldLineItemID] = append(couponsByOldLineItem[oldLineItemID], couponAssoc.CouponID)
		}
	}

	// Transfer coupons to matching line items in new subscription
	couponAssociationService := NewCouponAssociationService(s.serviceParams)
	transferredCount := 0

	for oldLineItemID, couponIDs := range couponsByOldLineItem {
		// Find the old line item to get its price_id
		var oldLineItem *subscription.SubscriptionLineItem
		for _, li := range oldLineItems {
			if li.ID == oldLineItemID {
				oldLineItem = li
				break
			}
		}

		if oldLineItem == nil {
			s.serviceParams.Logger.Warnw("old line item not found, skipping coupon transfer",
				"old_line_item_id", oldLineItemID)
			continue
		}

		// Find matching line item in new subscription by price_id
		newLineItem, exists := newPriceToLineItem[oldLineItem.PriceID]
		if !exists {
			s.serviceParams.Logger.Warnw("no matching line item in new subscription, skipping coupon transfer",
				"price_id", oldLineItem.PriceID,
				"old_line_item_id", oldLineItemID)
			continue
		}

		// Apply coupons to the new line item
		err := couponAssociationService.ApplyCouponToSubscriptionLineItem(ctx, couponIDs, newSubscriptionID, newLineItem.ID)
		if err != nil {
			s.serviceParams.Logger.Errorw("failed to transfer line item coupons",
				"old_line_item_id", oldLineItemID,
				"new_line_item_id", newLineItem.ID,
				"coupon_ids", couponIDs,
				"error", err)
			return ierr.WithError(err).
				WithHint("Failed to apply coupons to new line item").
				WithReportableDetails(map[string]interface{}{
					"old_line_item_id": oldLineItemID,
					"new_line_item_id": newLineItem.ID,
					"coupon_ids":       couponIDs,
				}).
				Mark(ierr.ErrDatabase)
		}

		transferredCount += len(couponIDs)
		s.serviceParams.Logger.Infow("transferred line item coupons",
			"old_line_item_id", oldLineItemID,
			"new_line_item_id", newLineItem.ID,
			"coupon_count", len(couponIDs))
	}

	s.serviceParams.Logger.Infow("completed line item coupon transfer",
		"old_subscription_id", oldSubscriptionID,
		"new_subscription_id", newSubscriptionID,
		"total_coupons_transferred", transferredCount)

	return nil
}
