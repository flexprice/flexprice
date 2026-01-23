package service

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/planpricesync"
	domainPrice "github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type PlanService = interfaces.PlanService

type planService struct {
	ServiceParams
}

func NewPlanService(
	params ServiceParams,
) PlanService {
	return &planService{
		ServiceParams: params,
	}
}

func (s *planService) CreatePlan(ctx context.Context, req dto.CreatePlanRequest) (*dto.CreatePlanResponse, error) {
	// Validate request
	if err := req.Validate(); err != nil {
		return nil, err
	}

	plan := req.ToPlan(ctx)

	if err := s.PlanRepo.Create(ctx, plan); err != nil {
		return nil, err
	}

	return &dto.CreatePlanResponse{Plan: plan}, nil
}

func (s *planService) GetPlan(ctx context.Context, id string) (*dto.PlanResponse, error) {
	if id == "" {
		return nil, ierr.NewError("plan ID is required").
			WithHint("Please provide a valid plan ID").
			Mark(ierr.ErrValidation)
	}

	plan, err := s.PlanRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	priceService := NewPriceService(s.ServiceParams)
	entitlementService := NewEntitlementService(s.ServiceParams)

	pricesResponse, err := priceService.GetPricesByPlanID(ctx, dto.GetPricesByPlanRequest{
		PlanID:       plan.ID,
		AllowExpired: true,
	})
	if err != nil {
		s.Logger.Errorw("failed to fetch prices for plan", "plan_id", plan.ID, "error", err)
		return nil, err
	}

	entitlements, err := entitlementService.GetPlanEntitlements(ctx, plan.ID)
	if err != nil {
		s.Logger.Errorw("failed to fetch entitlements for plan", "plan_id", plan.ID, "error", err)
		return nil, err
	}

	creditGrants, err := NewCreditGrantService(s.ServiceParams).GetCreditGrantsByPlan(ctx, plan.ID)
	if err != nil {
		s.Logger.Errorw("failed to fetch credit grants for plan", "plan_id", plan.ID, "error", err)
		return nil, err
	}

	response := &dto.PlanResponse{
		Plan:         plan,
		Prices:       pricesResponse.Items,
		Entitlements: entitlements.Items,
		CreditGrants: creditGrants.Items,
	}
	return response, nil
}

func (s *planService) GetPlans(ctx context.Context, filter *types.PlanFilter) (*dto.ListPlansResponse, error) {
	if filter == nil {
		filter = types.NewPlanFilter()
	}

	if err := filter.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation)
	}

	// Fetch plans
	plans, err := s.PlanRepo.List(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve plans").
			Mark(ierr.ErrDatabase)
	}

	// Get count
	count, err := s.PlanRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Build response
	response := &dto.ListPlansResponse{
		Items: make([]*dto.PlanResponse, len(plans)),
		Pagination: types.NewPaginationResponse(
			count,
			filter.GetLimit(),
			filter.GetOffset(),
		),
	}

	if len(plans) == 0 {
		return response, nil
	}

	for i, plan := range plans {
		response.Items[i] = &dto.PlanResponse{Plan: plan}
	}

	// Expand entitlements and prices if requested
	planIDs := lo.Map(plans, func(plan *plan.Plan, _ int) string {
		return plan.ID
	})

	// Create maps for storing expanded data
	pricesByPlanID := make(map[string][]*dto.PriceResponse)
	entitlementsByPlanID := make(map[string][]*dto.EntitlementResponse)
	creditGrantsByPlanID := make(map[string][]*dto.CreditGrantResponse)

	priceService := NewPriceService(s.ServiceParams)
	entitlementService := NewEntitlementService(s.ServiceParams)

	// If prices or entitlements expansion is requested, fetch them in bulk
	// Fetch prices if requested
	if filter.GetExpand().Has(types.ExpandPrices) {
		priceFilter := types.NewNoLimitPriceFilter().
			WithEntityIDs(planIDs).
			WithStatus(types.StatusPublished).
			WithEntityType(types.PRICE_ENTITY_TYPE_PLAN)

		// Build expand string for nested expansions in prices
		var expandFields []string

		// If meters should be expanded, propagate the expansion to prices
		if filter.GetExpand().Has(types.ExpandMeters) {
			expandFields = append(expandFields, string(types.ExpandMeters))
		}

		// If price units should be expanded (root level or nested under prices), propagate to prices
		if filter.GetExpand().Has(types.ExpandPriceUnit) || filter.GetExpand().GetNested(types.ExpandPrices).Has(types.ExpandPriceUnit) {
			expandFields = append(expandFields, string(types.ExpandPriceUnit))
		}

		// Set expand string if any expansions are requested
		if len(expandFields) > 0 {
			priceFilter = priceFilter.WithExpand(strings.Join(expandFields, ","))
		}

		prices, err := priceService.GetPrices(ctx, priceFilter)
		if err != nil {
			return nil, err
		}

		for _, p := range prices.Items {
			pricesByPlanID[p.EntityID] = append(pricesByPlanID[p.EntityID], p)
		}
	}

	// Fetch entitlements if requested
	if filter.GetExpand().Has(types.ExpandEntitlements) {
		entFilter := types.NewNoLimitEntitlementFilter().
			WithEntityIDs(planIDs).
			WithStatus(types.StatusPublished)

		// If features should be expanded, propagate the expansion to entitlements
		if filter.GetExpand().Has(types.ExpandFeatures) {
			entFilter = entFilter.WithExpand(string(types.ExpandFeatures))
		}

		// Apply the exact same sort order as plans
		if filter.Sort != nil {
			entFilter.Sort = append(entFilter.Sort, filter.Sort...)
		}

		entitlements, err := entitlementService.ListEntitlements(ctx, entFilter)
		if err != nil {
			return nil, err
		}

		for _, e := range entitlements.Items {
			entitlementsByPlanID[e.Entitlement.EntityID] = append(entitlementsByPlanID[e.Entitlement.EntityID], e)
		}
	}

	// Fetch credit grants if requested
	if filter.GetExpand().Has(types.ExpandCreditGrant) {

		for _, planID := range planIDs {
			creditGrants, err := s.CreditGrantRepo.GetByPlan(ctx, planID)
			if err != nil {
				return nil, err
			}

			for _, cg := range creditGrants {
				creditGrantsByPlanID[lo.FromPtr(cg.PlanID)] = append(creditGrantsByPlanID[lo.FromPtr(cg.PlanID)], &dto.CreditGrantResponse{CreditGrant: cg})
			}
		}
	}

	// Build response with expanded fields
	for i, plan := range plans {

		// Add prices if available
		if prices, ok := pricesByPlanID[plan.ID]; ok {
			response.Items[i].Prices = prices
		}

		// Add entitlements if available
		if entitlements, ok := entitlementsByPlanID[plan.ID]; ok {
			response.Items[i].Entitlements = entitlements
		}

		// Add credit grants if available
		if creditGrants, ok := creditGrantsByPlanID[plan.ID]; ok {
			response.Items[i].CreditGrants = creditGrants
		}
	}

	return response, nil
}

func (s *planService) UpdatePlan(ctx context.Context, id string, req dto.UpdatePlanRequest) (*dto.PlanResponse, error) {
	if id == "" {
		return nil, ierr.NewError("plan ID is required").
			WithHint("Plan ID is required").
			Mark(ierr.ErrValidation)
	}

	// Get the existing plan
	planResponse, err := s.GetPlan(ctx, id)
	if err != nil {
		return nil, err
	}

	plan := planResponse.Plan

	// Update plan fields if provided
	if req.Name != nil {
		plan.Name = *req.Name
	}
	if req.Description != nil {
		plan.Description = *req.Description
	}
	if req.LookupKey != nil {
		plan.LookupKey = *req.LookupKey
	}
	if req.Metadata != nil {
		plan.Metadata = req.Metadata
	}
	if req.DisplayOrder != nil {
		plan.DisplayOrder = req.DisplayOrder
	}

	// Start a transaction for updating plan
	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		// Update the plan
		if err := s.PlanRepo.Update(ctx, plan); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return s.GetPlan(ctx, id)
}

func (s *planService) DeletePlan(ctx context.Context, id string) error {

	if id == "" {
		return ierr.NewError("plan ID is required").
			WithHint("Plan ID is required").
			Mark(ierr.ErrValidation)
	}

	// check if plan exists
	plan, err := s.PlanRepo.Get(ctx, id)
	if err != nil {
		return err
	}

	subscriptionFilters := types.NewDefaultQueryFilter()
	subscriptionFilters.Status = lo.ToPtr(types.StatusPublished)
	subscriptionFilters.Limit = lo.ToPtr(1)
	subscriptions, err := s.SubRepo.List(ctx, &types.SubscriptionFilter{
		QueryFilter:             subscriptionFilters,
		PlanID:                  id,
		SubscriptionStatusNotIn: []types.SubscriptionStatus{types.SubscriptionStatusCancelled},
	})
	if err != nil {
		return err
	}

	if len(subscriptions) > 0 {
		return ierr.NewError("plan is still associated with subscriptions").
			WithHint("Please remove the active subscriptions before deleting this plan.").
			WithReportableDetails(map[string]interface{}{
				"plan_id": id,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	err = s.PlanRepo.Delete(ctx, plan)
	if err != nil {
		return err
	}
	return nil
}

// SyncPlanPrices synchronizes a single subscription with plan prices
//
// SyncPlanPrices - Enhanced Line Item Synchronization Logic (v3.0)
//
// This section synchronizes plan prices to subscription line items with comprehensive tracking.
// The process creates, terminates, and skips line items based on plan price states:
//
// 1. Price Eligibility:
//   - Each price must match the subscription's currency and billing period
//   - Ineligible prices are skipped (tracked as line_items_skipped_incompatible)
//
// 2. Price Lineage Tracking:
//   - ParentPriceID always points to the root plan price (P1)
//   - P1 -> P2: P2.ParentPriceID = P1
//   - P2 -> P3: P3.ParentPriceID = P1 (not P2)
//   - This enables proper override detection across price updates
//
// 3. Line Item Operations:
//   - Existing line items with expired prices -> Terminate (tracked as line_items_terminated)
//   - Existing line items with active prices -> Keep as is (tracked as line_items_skipped_already_terminated)
//   - Missing line items for active prices -> Create new (tracked as line_items_created)
//   - Missing line items for expired prices -> Skip (tracked as line_items_skipped_already_terminated)
//
// 4. Override Detection:
//   - Check if any line item traces back to the plan price using ParentPriceID
//   - If override exists, skip creating new line items (tracked as line_items_skipped_overridden)
//   - This handles complex scenarios: P1->P2->P3 where L2 uses P2 but P1 is updated to P4
//
// 5. Error Handling:
//   - Failed line item creation/termination operations are tracked as line_items_failed
//   - All operations are logged with detailed counters for transparency
//
// 6. Comprehensive Tracking:
//   - prices_processed: Total plan prices processed across all subscriptions
//   - line_items_created: New line items created for active prices
//   - line_items_terminated: Existing line items ended for expired prices
//   - line_items_skipped: Skipped operations (already terminated, overridden, incompatible)
//   - line_items_failed: Failed operations for monitoring and debugging
//
// The sync ensures subscriptions accurately reflect the current state of plan prices
// while maintaining proper billing continuity and respecting all price overrides.
// Time complexity: O(n) where n is the number of plan prices.
func (s *planService) SyncPlanPrices(ctx context.Context, planID string) (*dto.SyncPlanPricesResponse, error) {
	syncStartTime := time.Now()

	lineItemsFoundForCreation := 0
	lineItemsCreated := 0
	lineItemsTerminated := 0

	plan, err := s.PlanRepo.Get(ctx, planID)
	if err != nil {
		s.Logger.Errorw("failed to get plan for price synchronization", "plan_id", planID, "error", err)
		return nil, err
	}

	planPriceSyncParams := planpricesync.TerminateExpiredPlanPricesLineItemsParams{
		PlanID: planID,
		Limit:  1000,
	}

	terminationStartTime := time.Now()
	terminationIteration := 0
	for {
		terminationIteration++
		numTerminated, err := s.PlanPriceSyncRepo.TerminateExpiredPlanPricesLineItems(ctx, planPriceSyncParams)
		if err != nil {
			s.Logger.Errorw("failed to terminate expired plan price line items", "plan_id", planID, "error", err)
			return nil, err
		}
		lineItemsTerminated += numTerminated
		if numTerminated == 0 || numTerminated < planPriceSyncParams.Limit {
			break
		}
	}
	terminationTotalDuration := time.Since(terminationStartTime)

	creationStartTime := time.Now()
	cursorSubID := ""

	creationIteration := 0
	for {
		creationIteration++

		queryParams := planpricesync.ListPlanLineItemsToCreateParams{
			PlanID:     planID,
			Limit:      1000,
			AfterSubID: cursorSubID,
		}

		missingPairs, err := s.PlanPriceSyncRepo.ListPlanLineItemsToCreate(ctx, queryParams)
		if err != nil {
			s.Logger.Errorw("failed to list plan line items to create", "plan_id", planID, "error", err)
			return nil, err
		}

		nextSubID, err := s.PlanPriceSyncRepo.GetLastSubscriptionIDInBatch(ctx, queryParams)
		if err != nil {
			s.Logger.Errorw("failed to get last subscription ID in batch", "plan_id", planID, "error", err)
			return nil, err
		}

		if len(missingPairs) == 0 && nextSubID == nil {
			break
		}

		if len(missingPairs) == 0 {
			cursorSubID = *nextSubID
			continue
		}

		lineItemsFoundForCreation += len(missingPairs)

		priceIDs := lo.Uniq(lo.Map(missingPairs, func(pair planpricesync.PlanLineItemCreationDelta, _ int) string {
			return pair.PriceID
		}))

		subscriptionIDs := lo.Uniq(lo.Map(missingPairs, func(pair planpricesync.PlanLineItemCreationDelta, _ int) string {
			return pair.SubscriptionID
		}))

		priceFilter := types.NewNoLimitPriceFilter().
			WithPriceIDs(priceIDs).
			WithEntityType(types.PRICE_ENTITY_TYPE_PLAN).
			WithAllowExpiredPrices(true)

		prices, err := s.PriceRepo.List(ctx, priceFilter)
		if err != nil {
			s.Logger.Errorw("failed to fetch prices for line item creation", "plan_id", planID, "error", err)
			return nil, err
		}
		priceMap := lo.KeyBy(prices, func(p *domainPrice.Price) string { return p.ID })

		subFilter := types.NewNoLimitSubscriptionFilter()
		subFilter.SubscriptionIDs = subscriptionIDs
		subs, err := s.SubRepo.List(ctx, subFilter)
		if err != nil {
			s.Logger.Errorw("failed to fetch subscriptions for line item creation", "plan_id", planID, "error", err)
			return nil, err
		}
		subMap := lo.KeyBy(subs, func(s *subscription.Subscription) string { return s.ID })

		var lineItemsToCreate []*subscription.SubscriptionLineItem
		for _, pair := range missingPairs {
			price, priceFound := priceMap[pair.PriceID]
			sub, subFound := subMap[pair.SubscriptionID]

			if !priceFound || !subFound {
				return nil, ierr.NewError("price or subscription not found to create plan line item").
					WithHint("Price or subscription not found to create plan line item").
					WithReportableDetails(map[string]interface{}{
						"price_id":        pair.PriceID,
						"subscription_id": pair.SubscriptionID,
					}).
					Mark(ierr.ErrDatabase)
			}

			lineItem := createPlanLineItem(ctx, sub, price, plan)
			lineItemsToCreate = append(lineItemsToCreate, lineItem)
		}

		if len(lineItemsToCreate) > 0 {
			const bulkInsertBatchSize = 2000
			totalCreated := 0
			for i := 0; i < len(lineItemsToCreate); i += bulkInsertBatchSize {
				end := i + bulkInsertBatchSize
				if end > len(lineItemsToCreate) {
					end = len(lineItemsToCreate)
				}
				batch := lineItemsToCreate[i:end]

				err = s.SubscriptionLineItemRepo.CreateBulk(ctx, batch)
				if err != nil {
					s.Logger.Errorw("failed to create plan line items in bulk batch",
						"plan_id", planID,
						"error", err,
						"batch_start", i,
						"batch_end", end,
						"batch_count", len(batch),
						"total_count", len(lineItemsToCreate))
					return nil, err
				}
				totalCreated += len(batch)
			}

			lineItemsCreated += totalCreated
		}

		if nextSubID != nil {
			cursorSubID = *nextSubID
		}
	}
	creationTotalDuration := time.Since(creationStartTime)

	response := &dto.SyncPlanPricesResponse{
		PlanID:  planID,
		Message: "Plan prices synchronized to subscription line items successfully",
		Summary: dto.SyncPlanPricesSummary{
			LineItemsFoundForCreation: lineItemsFoundForCreation,
			LineItemsCreated:          lineItemsCreated,
			LineItemsTerminated:       lineItemsTerminated,
		},
	}
	totalSyncDuration := time.Since(syncStartTime)
	s.Logger.Infow("completed plan price synchronization",
		"plan_id", planID,
		"line_items_found_for_creation", lineItemsFoundForCreation,
		"line_items_created", lineItemsCreated,
		"line_items_terminated", lineItemsTerminated,
		"total_duration_ms", totalSyncDuration.Milliseconds(),
		"termination_duration_ms", terminationTotalDuration.Milliseconds(),
		"creation_duration_ms", creationTotalDuration.Milliseconds())
	return response, nil
}

func createPlanLineItem(
	ctx context.Context,
	sub *subscription.Subscription,
	price *domainPrice.Price,
	plan *plan.Plan,
) *subscription.SubscriptionLineItem {

	req := dto.CreateSubscriptionLineItemRequest{
		PriceID:     price.ID,
		Quantity:    decimal.Zero,
		Metadata:    price.Metadata,
		DisplayName: price.DisplayName,
	}

	lineItemParams := dto.LineItemParams{
		Subscription: &dto.SubscriptionResponse{Subscription: sub},
		Price:        &dto.PriceResponse{Price: price},
		Plan:         &dto.PlanResponse{Plan: plan},
		EntityType:   types.SubscriptionLineItemEntityTypePlan,
	}

	lineItem := req.ToSubscriptionLineItem(ctx, lineItemParams)

	return lineItem
}
