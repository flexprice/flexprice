package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/proration"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type prorationService struct {
	serviceParams  ServiceParams
	invoiceService InvoiceService
	priceService   PriceService
}

// NewProrationService creates a new proration service.
func NewProrationService(
	serviceParams ServiceParams,
) proration.Service {
	return &prorationService{
		serviceParams:  serviceParams,
		invoiceService: NewInvoiceService(serviceParams),
		priceService:   NewPriceService(serviceParams),
	}
}

// CalculateProration delegates to the underlying calculator.
func (s *prorationService) CalculateProration(ctx context.Context, params proration.ProrationParams) (*proration.ProrationResult, error) {
	calculator := s.serviceParams.ProrationCalculator
	s.serviceParams.Logger.Info(ctx, "calculating proration",
		"subscription_id", params.SubscriptionID,
		"line_item_id", params.LineItemID,
		"action", string(params.Action),
	)

	result, err := calculator.Calculate(ctx, params)
	if err != nil {
		s.serviceParams.Logger.Error(ctx, "proration calculation failed",
			"error", err,
			"subscription_id", params.SubscriptionID,
			"line_item_id", params.LineItemID,
		)
		return nil, ierr.NewErrorf("proration calculation failed: %v", err).
			WithHint("Check if the subscription and line item details are valid").
			Mark(ierr.ErrSystem)
	}

	if result == nil {
		return nil, nil
	}

	s.serviceParams.Logger.Debug(ctx, "proration calculation completed",
		"subscription_id", params.SubscriptionID,
		"line_item_id", params.LineItemID,
		"net_amount", result.NetAmount.String(),
	)

	return result, nil
}

// CalculateSubscriptionCancellationProration credits each item's unused time on its own windows.
// e.g. $20 monthly item on a quarterly sub cancelled Apr 11: 13.33 + 20 + 20 = $53.33.
func (s *prorationService) CalculateSubscriptionCancellationProration(
	ctx context.Context,
	sub *subscription.Subscription,
	lineItems []*subscription.SubscriptionLineItem,
	cancellationType types.CancellationType,
	effectiveDate time.Time,
	reason string,
	behavior types.ProrationBehavior,
) (*proration.SubscriptionProrationResult, error) {
	result := &proration.SubscriptionProrationResult{
		LineItemResults:      make(map[string]*proration.ProrationResult),
		TotalProrationAmount: decimal.Zero,
		Currency:             sub.Currency,
	}

	if behavior == types.ProrationBehaviorNone || cancellationType != types.CancellationTypeImmediate {
		return result, nil
	}

	if effectiveDate.Before(sub.CurrentPeriodStart) {
		effectiveDate = sub.CurrentPeriodStart
	}
	if effectiveDate.After(sub.CurrentPeriodEnd) {
		return nil, ierr.NewError("cancellation date must be within current billing period").
			WithHintf("Period: %s to %s, Cancellation: %s",
				sub.CurrentPeriodStart.Format("2006-01-02"),
				sub.CurrentPeriodEnd.Format("2006-01-02"),
				effectiveDate.Format("2006-01-02")).
			Mark(ierr.ErrValidation)
	}

	entries := make([]LineItemProrationEntry, 0, len(lineItems))
	for _, item := range lineItems {
		if item.Status != types.StatusPublished {
			continue
		}

		p, err := s.serviceParams.PriceRepo.Get(ctx, item.PriceID)
		if err != nil {
			return nil, err
		}

		entries = append(entries, LineItemProrationEntry{
			LineItem:        item,
			Action:          types.ProrationActionCancellation,
			CurrentPrice:    p,
			CurrentQuantity: item.Quantity,
		})
	}

	summary, err := NewLineItemProrationService(s.serviceParams).Compute(ctx, LineItemProrationRequest{
		Subscription:  sub,
		Entries:       entries,
		EffectiveDate: effectiveDate,
		Behavior:      behavior,
		Reason:        reason,
	})
	if err != nil {
		return nil, err
	}

	// Window credits are merged into one credit item per line item, so the response keeps one row per item.
	creditItems := make(map[string]proration.ProrationLineItem)
	for _, credit := range summary.CreditLineItems {
		lineItemID := lo.FromPtr(credit.SubscriptionLineItemID)
		creditItem, ok := creditItems[lineItemID]
		if !ok {
			creditItem = proration.ProrationLineItem{
				Description: lo.FromPtr(credit.DisplayName),
				Amount:      decimal.Zero,
				StartDate:   effectiveDate,
				Quantity:    credit.Quantity,
				PriceID:     lo.FromPtr(credit.PriceID),
				IsCredit:    true,
			}
		}
		creditItem.Amount = creditItem.Amount.Add(credit.Amount)
		creditItem.EndDate = lo.FromPtr(credit.PeriodEnd)
		creditItems[lineItemID] = creditItem
	}

	for lineItemID, creditItem := range creditItems {
		result.LineItemResults[lineItemID] = &proration.ProrationResult{
			CreditItems:        []proration.ProrationLineItem{creditItem},
			NetAmount:          creditItem.Amount,
			Currency:           sub.Currency,
			Action:             types.ProrationActionCancellation,
			ProrationDate:      effectiveDate,
			LineItemID:         lineItemID,
			CurrentPeriodStart: sub.CurrentPeriodStart,
			CurrentPeriodEnd:   sub.CurrentPeriodEnd,
			BillingPeriod:      sub.BillingPeriod,
		}
		result.TotalProrationAmount = result.TotalProrationAmount.Add(creditItem.Amount)
	}

	return result, nil
}

// creditBasis is what a line item was billed and already credited, falling back to its list
// total when no invoice has billed it yet.
func creditBasis(
	item *subscription.SubscriptionLineItem,
	billed map[string]*invoice.BilledAmounts,
	listTotal decimal.Decimal,
) (originalAmountPaid, previousCredits decimal.Decimal) {
	if item == nil {
		return decimal.Zero, decimal.Zero
	}

	if amounts := billed[item.ID]; amounts != nil {
		return amounts.Charged(), amounts.Credited()
	}

	return listTotal, decimal.Zero
}

// CalculateEntitlementProration calculates prorated entitlement limits for a subscription
func (s *prorationService) CalculateEntitlementProration(
	ctx context.Context,
	planID string,
	periodStart time.Time,
	periodEnd time.Time,
	prorationDate time.Time,
	customerTimezone string,
	billingCycle types.BillingCycle,
	billingAnchor time.Time,
	billingPeriod types.BillingPeriod,
	billingPeriodCount int,
) (*proration.EntitlementProrationResult, error) {
	logger := s.serviceParams.Logger.With(
		"plan_id", planID,
		"period_start", periodStart,
		"period_end", periodEnd,
		"proration_date", prorationDate,
		"billing_cycle", string(billingCycle),
	)

	logger.Info(ctx, "calculating entitlement proration")

	// Get plan entitlements
	entitlementService := NewEntitlementService(s.serviceParams)
	entitlementsResp, err := entitlementService.GetPlanEntitlements(ctx, planID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get plan entitlements").
			Mark(ierr.ErrDatabase)
	}

	// Convert to domain entitlements
	planEntitlements := make([]*entitlement.Entitlement, len(entitlementsResp.Items))
	for i, item := range entitlementsResp.Items {
		planEntitlements[i] = item.Entitlement
	}

	// Create entitlement proration calculator
	entitlementCalculator := proration.NewEntitlementProrationCalculator(
		s.serviceParams.Logger,
		s.serviceParams.ProrationCalculator,
	)

	// Calculate proration
	params := proration.EntitlementProrationParams{
		PeriodStart:        periodStart,
		PeriodEnd:          periodEnd,
		ProrationDate:      prorationDate,
		Timezone:           customerTimezone,
		BillingCycle:       billingCycle,
		BillingAnchor:      billingAnchor,
		BillingPeriod:      billingPeriod,
		BillingPeriodCount: billingPeriodCount,
		PlanEntitlements:   planEntitlements,
		Strategy:           types.StrategySecondBased, // Use second-based for precise time-based proration
	}

	result, err := entitlementCalculator.CalculateEntitlementProration(ctx, params)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to calculate entitlement proration").
			Mark(ierr.ErrSystem)
	}

	logger.Info(ctx, "entitlement proration calculated",
		"prorated_count", len(result.ProratedLimits),
		"coefficient", result.ProrationCoefficient.String())

	return result, nil
}

// CalculateAdditiveEntitlementProration calculates combined entitlement limits
// when changing plans mid-period. It adds the remaining entitlement from the old plan
// to the prorated entitlement from the new plan.
func (s *prorationService) CalculateAdditiveEntitlementProration(
	ctx context.Context,
	oldPlanID string,
	newPlanID string,
	oldPeriodStart time.Time,
	oldPeriodEnd time.Time,
	changeDate time.Time,
	customerTimezone string,
	billingCycle types.BillingCycle,
	billingAnchor time.Time,
	billingPeriod types.BillingPeriod,
	billingPeriodCount int,
) (*proration.EntitlementProrationResult, error) {
	logger := s.serviceParams.Logger.With(
		"old_plan_id", oldPlanID,
		"new_plan_id", newPlanID,
		"change_date", changeDate,
		"billing_cycle", string(billingCycle),
	)

	logger.Info(ctx, "calculating additive entitlement proration for plan change")

	// Determine the period end to use for proration based on billing cycle
	var periodEnd time.Time
	if billingCycle == types.BillingCycleCalendar {
		// For calendar billing, use calendar period end
		// CalculateCalendarBillingAnchor returns the START of the NEXT period,
		// which is the END of the current period
		periodEnd = types.CalculateCalendarBillingAnchor(changeDate, billingPeriod, customerTimezone)
		logger.Debug(ctx, "using calendar period end for proration",
			"period_end", periodEnd)
	} else {
		// For anniversary billing, use subscription period end
		periodEnd = oldPeriodEnd
		logger.Debug(ctx, "using subscription period end for proration",
			"period_end", periodEnd)
	}

	// Step 1: Calculate remaining entitlement from old plan
	logger.Debug(ctx, "calculating remaining entitlement from old plan")
	oldProration, err := s.CalculateEntitlementProration(
		ctx, oldPlanID,
		oldPeriodStart, periodEnd,
		changeDate, // Proration date is change date
		customerTimezone, billingCycle, billingAnchor,
		billingPeriod, billingPeriodCount,
	)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to calculate old plan remaining entitlement").
			Mark(ierr.ErrSystem)
	}

	// Step 2: Calculate prorated entitlement for new plan
	logger.Debug(ctx, "calculating prorated entitlement for new plan")
	newProration, err := s.CalculateEntitlementProration(
		ctx, newPlanID,
		oldPeriodStart, periodEnd,
		changeDate, // Same proration date
		customerTimezone, billingCycle, billingAnchor,
		billingPeriod, billingPeriodCount,
	)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to calculate new plan prorated entitlement").
			Mark(ierr.ErrSystem)
	}

	// Step 3: Combine the limits
	logger.Info(ctx, "proration coefficient calculated",
		"coefficient", oldProration.ProrationCoefficient.String(),
		"total_days", oldProration.TotalDays,
		"remaining_days", oldProration.RemainingDays,
		"period_start", oldPeriodStart,
		"period_end", periodEnd,
		"change_date", changeDate)

	combinedResult := &proration.EntitlementProrationResult{
		ProratedLimits:       make(map[string]int64),
		EntitlementDetails:   []proration.EntitlementProrationDetail{},
		ProrationCoefficient: oldProration.ProrationCoefficient, // Same for both
		PeriodStart:          oldPeriodStart,
		PeriodEnd:            periodEnd,
		ProrationDate:        changeDate,
		TotalDays:            oldProration.TotalDays,
		RemainingDays:        oldProration.RemainingDays,
		IsAdditive:           true,
		OldPlanContribution:  make(map[string]proration.AdditiveProrationDetail),
		NewPlanContribution:  make(map[string]proration.AdditiveProrationDetail),
	}

	// Track all unique feature IDs
	featureIDs := make(map[string]bool)
	for featureID := range oldProration.ProratedLimits {
		featureIDs[featureID] = true
	}
	for featureID := range newProration.ProratedLimits {
		featureIDs[featureID] = true
	}

	// Combine limits for each feature
	for featureID := range featureIDs {
		oldLimit := oldProration.ProratedLimits[featureID]
		newLimit := newProration.ProratedLimits[featureID]
		combinedLimit := oldLimit + newLimit

		combinedResult.ProratedLimits[featureID] = combinedLimit

		// Find original limits from detail arrays
		var oldOriginal, newOriginal int64
		var oldParentID, newParentID string
		var usageResetPeriod types.EntitlementUsageResetPeriod

		for _, detail := range oldProration.EntitlementDetails {
			if detail.FeatureID == featureID {
				oldOriginal = detail.OriginalLimit
				oldParentID = detail.ParentID
				usageResetPeriod = detail.UsageResetPeriod
				break
			}
		}

		for _, detail := range newProration.EntitlementDetails {
			if detail.FeatureID == featureID {
				newOriginal = detail.OriginalLimit
				newParentID = detail.ParentID
				usageResetPeriod = detail.UsageResetPeriod
				break
			}
		}

		// Store contribution details
		if oldLimit > 0 {
			combinedResult.OldPlanContribution[featureID] = proration.AdditiveProrationDetail{
				PlanID:        oldPlanID,
				OriginalLimit: oldOriginal,
				ProratedLimit: oldLimit,
				Coefficient:   oldProration.ProrationCoefficient,
			}
		}

		if newLimit > 0 {
			combinedResult.NewPlanContribution[featureID] = proration.AdditiveProrationDetail{
				PlanID:        newPlanID,
				OriginalLimit: newOriginal,
				ProratedLimit: newLimit,
				Coefficient:   newProration.ProrationCoefficient,
			}
		}

		// Create combined entitlement detail
		// Use new plan's parent ID if available, otherwise old plan's
		parentID := newParentID
		if parentID == "" {
			parentID = oldParentID
		}

		combinedResult.EntitlementDetails = append(combinedResult.EntitlementDetails, proration.EntitlementProrationDetail{
			FeatureID:        featureID,
			OriginalLimit:    oldOriginal + newOriginal, // Combined original
			ProratedLimit:    combinedLimit,
			Coefficient:      oldProration.ProrationCoefficient,
			ParentID:         parentID,
			UsageResetPeriod: usageResetPeriod,
		})

		logger.Info(ctx, "combined entitlement for feature",
			"feature_id", featureID,
			"old_plan_original_limit", oldOriginal,
			"old_plan_prorated_limit", oldLimit,
			"new_plan_original_limit", newOriginal,
			"new_plan_prorated_limit", newLimit,
			"combined_limit", combinedLimit,
			"coefficient", oldProration.ProrationCoefficient.String())

		logger.Debug(ctx, "combined entitlement limits",
			"feature_id", featureID,
			"old_limit", oldLimit,
			"new_limit", newLimit,
			"combined_limit", combinedLimit)
	}

	logger.Info(ctx, "additive entitlement proration calculation completed",
		"total_features", len(combinedResult.ProratedLimits),
		"coefficient", combinedResult.ProrationCoefficient.String(),
		"old_plan_features", len(oldProration.ProratedLimits),
		"new_plan_features", len(newProration.ProratedLimits))

	return combinedResult, nil
}

// CreateProratedEntitlements creates subscription-scoped entitlement overrides
func (s *prorationService) CreateProratedEntitlements(
	ctx context.Context,
	subscriptionID string,
	prorationResult *proration.EntitlementProrationResult,
	startDate time.Time,
	endDate time.Time,
) error {
	logger := s.serviceParams.Logger.With(
		"subscription_id", subscriptionID,
		"entitlements_count", len(prorationResult.ProratedLimits),
	)

	logger.Info(ctx, "creating prorated entitlements")

	if len(prorationResult.ProratedLimits) == 0 {
		logger.Info(ctx, "no entitlements to prorate")
		return nil
	}

	entitlementService := NewEntitlementService(s.serviceParams)
	createdCount := 0
	var errors []error

	// Create subscription-scoped entitlement for each prorated limit
	for _, detail := range prorationResult.EntitlementDetails {
		logger.Debug(ctx, "creating prorated entitlement",
			"feature_id", detail.FeatureID,
			"original_limit", detail.OriginalLimit,
			"prorated_limit", detail.ProratedLimit)

		// Create subscription-scoped entitlement override
		_, err := entitlementService.CreateEntitlement(ctx, dto.CreateEntitlementRequest{
			EntityType:          types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION,
			EntityID:            subscriptionID,
			FeatureID:           detail.FeatureID,
			FeatureType:         types.FeatureTypeMetered,
			UsageLimit:          &detail.ProratedLimit,
			UsageResetPeriod:    detail.UsageResetPeriod,
			ParentEntitlementID: &detail.ParentID,
			IsEnabled:           true,
			StartDate:           &startDate,
			EndDate:             &endDate,
		})

		if err != nil {
			logger.Error(ctx, "failed to create prorated entitlement",
				"feature_id", detail.FeatureID,
				"error", err)
			errors = append(errors, err)
			continue
		}

		createdCount++
	}

	if len(errors) > 0 {
		logger.Info(context.Background(), "some prorated entitlements failed to create",
			"created", createdCount,
			"failed", len(errors))
		// Return error if all failed
		if createdCount == 0 {
			return ierr.NewErrorf("failed to create all prorated entitlements: %v", errors).
				WithHint("Check entitlement creation errors").
				Mark(ierr.ErrSystem)
		}
	}

	logger.Info(ctx, "prorated entitlements created",
		"created_count", createdCount)

	return nil
}
