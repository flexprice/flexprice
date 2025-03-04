package service

import (
	"context"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type SubscriptionService interface {
	CreateSubscription(ctx context.Context, req dto.CreateSubscriptionRequest) (*dto.SubscriptionResponse, error)
	GetSubscription(ctx context.Context, id string) (*dto.SubscriptionResponse, error)
	CancelSubscription(ctx context.Context, id string, cancelAtPeriodEnd bool) error
	ListSubscriptions(ctx context.Context, filter *types.SubscriptionFilter) (*dto.ListSubscriptionsResponse, error)
	GetUsageBySubscription(ctx context.Context, req *dto.GetUsageBySubscriptionRequest) (*dto.GetUsageBySubscriptionResponse, error)
	UpdateBillingPeriods(ctx context.Context) (*dto.SubscriptionUpdatePeriodResponse, error)
}

type subscriptionService struct {
	ServiceParams
}

func NewSubscriptionService(params ServiceParams) SubscriptionService {
	return &subscriptionService{
		ServiceParams: params,
	}
}
func (s *subscriptionService) CreateSubscription(ctx context.Context, req dto.CreateSubscriptionRequest) (*dto.SubscriptionResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Check if customer exists
	customer, err := s.CustomerRepo.Get(ctx, req.CustomerID)
	if err != nil {
		return nil, err
	}

	if customer.Status != types.StatusPublished {
		return nil, ierr.NewError("customer is not active").
			WithHint("The customer must be active to create a subscription").
			WithReportableDetails(map[string]interface{}{
				"customer_id": req.CustomerID,
				"status":      customer.Status,
			}).
			Mark(ierr.ErrValidation)
	}

	plan, err := s.PlanRepo.Get(ctx, req.PlanID)
	if err != nil {
		return nil, err
	}

	if plan.Status != types.StatusPublished {
		return nil, ierr.NewError("plan is not active").
			WithHint("The plan must be active to create a subscription").
			WithReportableDetails(map[string]interface{}{
				"plan_id": req.PlanID,
				"status":  plan.Status,
			}).
			Mark(ierr.ErrValidation)
	}

	priceService := NewPriceService(s.PriceRepo, s.MeterRepo, s.Logger)
	priceFilter := types.NewNoLimitPriceFilter().
		WithPlanIDs([]string{plan.ID}).
		WithExpand(string(types.ExpandMeters))
	pricesResponse, err := priceService.GetPrices(ctx, priceFilter)
	if err != nil {
		return nil, err
	}

	if len(pricesResponse.Items) == 0 {
		return nil, ierr.NewError("no prices found for plan").
			WithHint("The plan must have at least one price to create a subscription").
			WithReportableDetails(map[string]interface{}{
				"plan_id": req.PlanID,
			}).
			Mark(ierr.ErrValidation)
	}

	prices := make([]price.Price, len(pricesResponse.Items))
	for i, p := range pricesResponse.Items {
		prices[i] = *p.Price
	}

	priceMap := make(map[string]*dto.PriceResponse, len(prices))
	for _, p := range pricesResponse.Items {
		priceMap[p.Price.ID] = p
	}

	sub := req.ToSubscription(ctx)

	// Filter prices for subscription that are valid for the plan
	validPrices := filterValidPricesForSubscription(pricesResponse.Items, sub)
	if len(validPrices) == 0 {
		return nil, ierr.NewError("no valid prices found for subscription").
			WithHint("No prices match the subscription criteria").
			WithReportableDetails(map[string]interface{}{
				"plan_id":         req.PlanID,
				"billing_period":  sub.BillingPeriod,
				"billing_cadence": sub.BillingCadence,
			}).
			Mark(ierr.ErrValidation)
	}

	now := time.Now().UTC()

	// Set start date and ensure it's in UTC
	if sub.StartDate.IsZero() {
		sub.StartDate = now
	} else {
		sub.StartDate = sub.StartDate.UTC()
	}

	// Set billing anchor and ensure it's in UTC
	if sub.BillingAnchor.IsZero() {
		sub.BillingAnchor = sub.StartDate
	} else {
		sub.BillingAnchor = sub.BillingAnchor.UTC()
		// Validate that billing anchor is not before start date
		if sub.BillingAnchor.Before(sub.StartDate) {
			return nil, ierr.NewError("billing anchor cannot be before start date").
				WithHint("The billing anchor must be on or after the start date").
				WithReportableDetails(map[string]interface{}{
					"start_date":     sub.StartDate,
					"billing_anchor": sub.BillingAnchor,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	if sub.BillingPeriodCount == 0 {
		sub.BillingPeriodCount = 1
	}

	// Calculate the first billing period end date
	nextBillingDate, err := types.NextBillingDate(sub.StartDate, sub.BillingAnchor, sub.BillingPeriodCount, sub.BillingPeriod)
	if err != nil {
		return nil, err
	}

	sub.CurrentPeriodStart = sub.StartDate
	sub.CurrentPeriodEnd = nextBillingDate
	sub.SubscriptionStatus = types.SubscriptionStatusActive

	// Convert line items
	lineItems := make([]*subscription.SubscriptionLineItem, 0, len(validPrices))
	for _, price := range validPrices {
		lineItems = append(lineItems, &subscription.SubscriptionLineItem{
			PriceID:       price.ID,
			EnvironmentID: types.GetEnvironmentID(ctx),
			BaseModel:     types.GetDefaultBaseModel(ctx),
		})
	}

	// Convert line items
	for _, item := range lineItems {
		price, ok := priceMap[item.PriceID]
		if !ok {
			return nil, ierr.NewError("failed to get price %s: price not found").
				WithHint("Ensure all prices are valid and available").
				WithReportableDetails(map[string]interface{}{
					"price_id": item.PriceID,
				}).
				Mark(ierr.ErrDatabase)
		}

		if price.Type == types.PRICE_TYPE_USAGE && price.Meter != nil {
			item.MeterID = price.Meter.ID
			item.MeterDisplayName = price.Meter.Name
			item.DisplayName = price.Meter.Name
			item.Quantity = decimal.Zero
		} else {
			item.DisplayName = plan.Name
			if item.Quantity.IsZero() {
				item.Quantity = decimal.NewFromInt(1)
			}
		}

		if item.ID == "" {
			item.ID = types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM)
		}

		item.SubscriptionID = sub.ID
		item.PriceType = price.Type
		item.PlanID = plan.ID
		item.PlanDisplayName = plan.Name
		item.CustomerID = sub.CustomerID
		item.Currency = sub.Currency
		item.BillingPeriod = sub.BillingPeriod
		item.StartDate = sub.StartDate
		if sub.EndDate != nil {
			item.EndDate = *sub.EndDate
		}
	}
	sub.LineItems = lineItems

	s.Logger.Infow("creating subscription",
		"customer_id", sub.CustomerID,
		"plan_id", sub.PlanID,
		"start_date", sub.StartDate,
		"billing_anchor", sub.BillingAnchor,
		"current_period_start", sub.CurrentPeriodStart,
		"current_period_end", sub.CurrentPeriodEnd,
		"valid_prices", len(validPrices),
		"num_line_items", len(sub.LineItems))

	// Create subscription with line items
	if err := s.SubRepo.CreateWithLineItems(ctx, sub, sub.LineItems); err != nil {
		return nil, err
	}

	response := &dto.SubscriptionResponse{Subscription: sub}
	return response, nil
}

func (s *subscriptionService) GetSubscription(ctx context.Context, id string) (*dto.SubscriptionResponse, error) {
	// Get subscription with line items
	subscription, _, err := s.SubRepo.GetWithLineItems(ctx, id)
	if err != nil {
		return nil, err
	}

	response := &dto.SubscriptionResponse{Subscription: subscription}

	// expand plan
	planService := NewPlanService(s.DB, s.PlanRepo, s.PriceRepo, s.MeterRepo, s.EntitlementRepo, s.FeatureRepo, s.Logger)

	plan, err := planService.GetPlan(ctx, subscription.PlanID)
	if err != nil {
		return nil, err
	}
	response.Plan = plan

	// expand customer
	customerService := NewCustomerService(s.CustomerRepo)
	customer, err := customerService.GetCustomer(ctx, subscription.CustomerID)
	if err != nil {
		return nil, err
	}
	response.Customer = customer
	return response, nil
}

func (s *subscriptionService) CancelSubscription(ctx context.Context, id string, cancelAtPeriodEnd bool) error {
	subscription, _, err := s.SubRepo.GetWithLineItems(ctx, id)
	if err != nil {
		return err
	}

	if subscription.SubscriptionStatus == types.SubscriptionStatusCancelled {
		return ierr.NewError("subscription is already cancelled").
			WithHint("The subscription is already cancelled").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": id,
			}).
			Mark(ierr.ErrValidation)
	}

	now := time.Now().UTC()
	subscription.SubscriptionStatus = types.SubscriptionStatusCancelled
	subscription.CancelledAt = &now
	subscription.CancelAtPeriodEnd = cancelAtPeriodEnd

	if err := s.SubRepo.Update(ctx, subscription); err != nil {
		return err
	}

	return nil
}

func (s *subscriptionService) ListSubscriptions(ctx context.Context, filter *types.SubscriptionFilter) (*dto.ListSubscriptionsResponse, error) {
	planService := NewPlanService(s.DB, s.PlanRepo, s.PriceRepo, s.MeterRepo, s.EntitlementRepo, s.FeatureRepo, s.Logger)

	subscriptions, err := s.SubRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	count, err := s.SubRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	response := &dto.ListSubscriptionsResponse{
		Items: make([]*dto.SubscriptionResponse, len(subscriptions)),
		Pagination: types.NewPaginationResponse(
			count,
			filter.GetLimit(),
			filter.GetOffset(),
		),
	}

	// Collect unique plan IDs
	planIDMap := make(map[string]*dto.PlanResponse, 0)
	for _, sub := range subscriptions {
		planIDMap[sub.PlanID] = nil
	}

	// Get plans in bulk
	planFilter := types.NewNoLimitPlanFilter()
	planFilter.PlanIDs = lo.Keys(planIDMap)
	planResponse, err := planService.GetPlans(ctx, planFilter)
	if err != nil {
		return nil, err
	}

	// Build plan map for quick lookup
	for _, plan := range planResponse.Items {
		planIDMap[plan.Plan.ID] = plan
	}

	// Build response with plans
	for i, sub := range subscriptions {
		response.Items[i] = &dto.SubscriptionResponse{
			Subscription: sub,
			Plan:         planIDMap[sub.PlanID],
		}
	}

	return response, nil
}

func (s *subscriptionService) GetUsageBySubscription(ctx context.Context, req *dto.GetUsageBySubscriptionRequest) (*dto.GetUsageBySubscriptionResponse, error) {
	response := &dto.GetUsageBySubscriptionResponse{}

	eventService := NewEventService(s.EventRepo, s.MeterRepo, s.EventPublisher, s.Logger)
	priceService := NewPriceService(s.PriceRepo, s.MeterRepo, s.Logger)

	// Get subscription with line items
	subscription, lineItems, err := s.SubRepo.GetWithLineItems(ctx, req.SubscriptionID)
	if err != nil {
		return nil, err
	}

	// Get customer
	customer, err := s.CustomerRepo.Get(ctx, subscription.CustomerID)
	if err != nil {
		return nil, err
	}

	usageStartTime := req.StartTime
	if usageStartTime.IsZero() {
		usageStartTime = subscription.CurrentPeriodStart
	}

	usageEndTime := req.EndTime
	if usageEndTime.IsZero() {
		usageEndTime = subscription.CurrentPeriodEnd
	}

	if req.LifetimeUsage {
		usageStartTime = time.Time{}
		usageEndTime = time.Now().UTC()
	}

	// Maintain meter order as they first appear in line items
	meterOrder := []string{}
	seenMeters := make(map[string]bool)
	meterPrices := make(map[string][]*price.Price)

	// Collect all price IDs
	priceIDs := make([]string, 0, len(lineItems))
	for _, item := range lineItems {
		if item.PriceType != types.PRICE_TYPE_USAGE {
			continue
		}
		if item.MeterID == "" {
			continue
		}
		priceIDs = append(priceIDs, item.PriceID)
	}

	// Fetch all prices in one call
	priceFilter := types.NewNoLimitPriceFilter()
	priceFilter.PriceIDs = priceIDs
	prices, err := s.PriceRepo.List(ctx, priceFilter)
	if err != nil {
		return nil, err
	}

	// Build price map for quick lookup
	priceMap := make(map[string]*price.Price, len(prices))
	for _, p := range prices {
		priceMap[p.ID] = p
	}

	// Build meterPrices from line items
	for _, item := range lineItems {
		if item.PriceType != types.PRICE_TYPE_USAGE {
			continue
		}
		if item.MeterID == "" {
			continue
		}
		meterID := item.MeterID
		if !seenMeters[meterID] {
			meterOrder = append(meterOrder, meterID)
			seenMeters[meterID] = true
		}

		// Get price details from map
		price, ok := priceMap[item.PriceID]
		if !ok {
			return nil, ierr.NewError("failed to get price %s: price not found").
				WithHint("Ensure all prices are valid and available").
				WithReportableDetails(map[string]interface{}{
					"price_id": item.PriceID,
				}).
				Mark(ierr.ErrDatabase)
		}
		meterPrices[meterID] = append(meterPrices[meterID], price)
	}

	// Pre-fetch all meter display names
	meterDisplayNames := make(map[string]string)
	for _, meterID := range meterOrder {
		meterDisplayNames[meterID] = getMeterDisplayName(ctx, s.MeterRepo, meterID, meterDisplayNames)
	}

	totalCost := decimal.Zero

	s.Logger.Debugw("calculating usage for subscription",
		"subscription_id", req.SubscriptionID,
		"start_time", usageStartTime,
		"end_time", usageEndTime,
		"num_meters", len(meterOrder))

	for _, meterID := range meterOrder {
		meterPriceGroup := meterPrices[meterID]

		// Sort prices by filter count (stable order)
		sort.Slice(meterPriceGroup, func(i, j int) bool {
			return len(meterPriceGroup[i].FilterValues) > len(meterPriceGroup[j].FilterValues)
		})

		type filterGroup struct {
			ID           string
			Priority     int
			FilterValues map[string][]string
		}

		filterGroups := make([]filterGroup, 0, len(meterPriceGroup))
		for _, price := range meterPriceGroup {
			filterGroups = append(filterGroups, filterGroup{
				ID:           price.ID,
				Priority:     calculatePriority(price.FilterValues),
				FilterValues: price.FilterValues,
			})
		}

		// Sort filter groups by priority and ID
		sort.SliceStable(filterGroups, func(i, j int) bool {
			pi := calculatePriority(filterGroups[i].FilterValues)
			pj := calculatePriority(filterGroups[j].FilterValues)
			if pi != pj {
				return pi > pj
			}
			return filterGroups[i].ID < filterGroups[j].ID
		})

		filterGroupsMap := make(map[string]map[string][]string)
		for _, group := range filterGroups {
			if len(group.FilterValues) == 0 {
				filterGroupsMap[group.ID] = map[string][]string{}
			} else {
				filterGroupsMap[group.ID] = group.FilterValues
			}
		}

		usages, err := eventService.GetUsageByMeterWithFilters(ctx, &dto.GetUsageByMeterRequest{
			MeterID:            meterID,
			ExternalCustomerID: customer.ExternalID,
			StartTime:          usageStartTime,
			EndTime:            usageEndTime,
		}, filterGroupsMap)
		if err != nil {
			return nil, err
		}

		// Append charges in the same order as meterPriceGroup
		for _, price := range meterPriceGroup {
			var quantity decimal.Decimal
			var matchingUsage *events.AggregationResult
			for _, usage := range usages {
				if fgID, ok := usage.Metadata["filter_group_id"]; ok && fgID == price.ID {
					matchingUsage = usage
					break
				}
			}

			if matchingUsage != nil {
				quantity = matchingUsage.Value
				cost := priceService.CalculateCost(ctx, price, quantity)
				totalCost = totalCost.Add(cost)

				s.Logger.Debugw("calculated usage for meter",
					"meter_id", meterID,
					"quantity", quantity,
					"cost", cost,
					"total_cost", totalCost,
					"meter_display_name", meterDisplayNames[meterID],
					"subscription_id", req.SubscriptionID,
					"usage", matchingUsage,
					"price", price,
					"filter_values", price.FilterValues,
				)

				filteredUsageCharge := createChargeResponse(
					price,
					quantity,
					cost,
					meterDisplayNames[meterID],
				)

				if filteredUsageCharge == nil {
					continue
				}

				if filteredUsageCharge.Quantity > 0 && filteredUsageCharge.Amount > 0 {
					response.Charges = append(response.Charges, filteredUsageCharge)
				}
			}
		}
	}

	response.StartTime = usageStartTime
	response.EndTime = usageEndTime
	response.Amount = price.FormatAmountToFloat64WithPrecision(totalCost, subscription.Currency)
	response.Currency = subscription.Currency
	response.DisplayAmount = price.GetDisplayAmountWithPrecision(totalCost, subscription.Currency)

	return response, nil
}

// UpdateBillingPeriods updates the current billing periods for all active subscriptions
// This should be run every 15 minutes to ensure billing periods are up to date
func (s *subscriptionService) UpdateBillingPeriods(ctx context.Context) (*dto.SubscriptionUpdatePeriodResponse, error) {
	const batchSize = 100
	now := time.Now().UTC()

	s.Logger.Infow("starting billing period updates",
		"current_time", now)

	response := &dto.SubscriptionUpdatePeriodResponse{
		Items:        make([]*dto.SubscriptionUpdatePeriodResponseItem, 0),
		TotalFailed:  0,
		TotalSuccess: 0,
		StartAt:      now,
	}

	offset := 0
	for {
		filter := &types.SubscriptionFilter{
			QueryFilter: &types.QueryFilter{
				Limit:  lo.ToPtr(batchSize),
				Offset: lo.ToPtr(offset),
				Status: lo.ToPtr(types.StatusPublished),
			},
			SubscriptionStatus: []types.SubscriptionStatus{types.SubscriptionStatusActive},
			TimeRangeFilter: &types.TimeRangeFilter{
				EndTime: &now,
			},
		}

		subs, err := s.SubRepo.ListAllTenant(ctx, filter)
		if err != nil {
			return response, err
		}

		s.Logger.Infow("processing subscription batch",
			"batch_size", len(subs),
			"offset", offset)

		if len(subs) == 0 {
			break // No more subscriptions to process
		}

		// Process each subscription in the batch
		for _, sub := range subs {
			// update context to include the tenant id
			ctx = context.WithValue(ctx, types.CtxTenantID, sub.TenantID)
			item := &dto.SubscriptionUpdatePeriodResponseItem{
				SubscriptionID: sub.ID,
				PeriodStart:    sub.CurrentPeriodStart,
				PeriodEnd:      sub.CurrentPeriodEnd,
			}
			err = s.processSubscriptionPeriod(ctx, sub, now)
			if err != nil {
				s.Logger.Errorw("failed to process subscription period",
					"subscription_id", sub.ID,
					"error", err)

				response.TotalFailed++
				item.Error = err.Error()
			} else {
				item.Success = true
				response.TotalSuccess++
			}

			response.Items = append(response.Items, item)
		}

		offset += len(subs)
		if len(subs) < batchSize {
			break // No more subscriptions to fetch
		}
	}

	return response, nil
}

/// Helpers

func (s *subscriptionService) processSubscriptionPeriod(ctx context.Context, sub *subscription.Subscription, now time.Time) error {
	// Initialize services
	invoiceService := NewInvoiceService(s.ServiceParams)

	currentStart := sub.CurrentPeriodStart
	currentEnd := sub.CurrentPeriodEnd

	// Start with current period
	var periods []struct {
		start time.Time
		end   time.Time
	}
	periods = append(periods, struct {
		start time.Time
		end   time.Time
	}{
		start: currentStart,
		end:   currentEnd,
	})

	for currentEnd.Before(now) {
		nextStart := currentEnd
		nextEnd, err := types.NextBillingDate(nextStart, sub.BillingAnchor, sub.BillingPeriodCount, sub.BillingPeriod)
		if err != nil {
			s.Logger.Errorw("failed to calculate next billing date",
				"subscription_id", sub.ID,
				"current_end", currentEnd,
				"process_up_to", now,
				"error", err)
			return err
		}

		periods = append(periods, struct {
			start time.Time
			end   time.Time
		}{
			start: nextStart,
			end:   nextEnd,
		})

		currentEnd = nextEnd
	}

	if len(periods) == 1 {
		s.Logger.Debugw("no transitions needed for subscription",
			"subscription_id", sub.ID,
			"current_period_start", sub.CurrentPeriodStart,
			"current_period_end", sub.CurrentPeriodEnd,
			"process_up_to", now)
		return nil
	}

	// Use db's WithTx for atomic operations
	err := s.DB.WithTx(ctx, func(ctx context.Context) error {
		// Process all periods except the last one (which becomes the new current period)
		for i := 0; i < len(periods)-1; i++ {
			period := periods[i]

			// Create and finalize invoice for this period
			inv, err := invoiceService.CreateSubscriptionInvoice(ctx, &dto.CreateSubscriptionInvoiceRequest{
				SubscriptionID: sub.ID,
				PeriodStart:    period.start,
				PeriodEnd:      period.end,
			})
			if err != nil {
				return err
			}

			if err := invoiceService.FinalizeInvoice(ctx, inv.ID); err != nil {
				return err
			}

			s.Logger.Infow("created invoice for period",
				"subscription_id", sub.ID,
				"invoice_id", inv.ID,
				"period_start", period.start,
				"period_end", period.end,
				"period_index", i)

			// Check for cancellation at this period end
			if sub.CancelAtPeriodEnd && sub.CancelAt != nil && !sub.CancelAt.After(period.end) {
				sub.SubscriptionStatus = types.SubscriptionStatusCancelled
				sub.CancelledAt = sub.CancelAt
				break
			}
		}

		// Update to the new current period (last period)
		newPeriod := periods[len(periods)-1]
		sub.CurrentPeriodStart = newPeriod.start
		sub.CurrentPeriodEnd = newPeriod.end

		// Final cancellation check
		if sub.CancelAtPeriodEnd && sub.CancelAt != nil && !sub.CancelAt.After(newPeriod.end) {
			sub.SubscriptionStatus = types.SubscriptionStatusCancelled
			sub.CancelledAt = sub.CancelAt
		}

		if err := s.SubRepo.Update(ctx, sub); err != nil {
			return err
		}

		s.Logger.Infow("completed subscription period processing",
			"subscription_id", sub.ID,
			"original_period_start", periods[0].start,
			"original_period_end", periods[0].end,
			"new_period_start", sub.CurrentPeriodStart,
			"new_period_end", sub.CurrentPeriodEnd,
			"process_up_to", now,
			"periods_processed", len(periods)-1)

		return nil
	})

	if err != nil {
		s.Logger.Errorw("failed to process subscription period",
			"subscription_id", sub.ID,
			"error", err)
		return err
	}

	return nil
}

func createChargeResponse(priceObj *price.Price, quantity decimal.Decimal, cost decimal.Decimal, meterDisplayName string) *dto.SubscriptionUsageByMetersResponse {
	finalAmount := price.FormatAmountToFloat64WithPrecision(cost, priceObj.Currency)
	if finalAmount <= 0 {
		return nil
	}

	return &dto.SubscriptionUsageByMetersResponse{
		Amount:           finalAmount,
		Currency:         priceObj.Currency,
		DisplayAmount:    price.GetDisplayAmountWithPrecision(cost, priceObj.Currency),
		Quantity:         quantity.InexactFloat64(),
		FilterValues:     priceObj.FilterValues,
		MeterDisplayName: meterDisplayName,
		Price:            priceObj,
	}
}

func getMeterDisplayName(ctx context.Context, meterRepo meter.Repository, meterID string, cache map[string]string) string {
	if name, ok := cache[meterID]; ok {
		return name
	}

	m, err := meterRepo.GetMeter(ctx, meterID)
	if err != nil {
		return meterID
	}

	displayName := m.Name
	if displayName == "" {
		displayName = m.EventName
	}
	cache[meterID] = displayName
	return displayName
}

func filterValidPricesForSubscription(prices []*dto.PriceResponse, subscriptionObj *subscription.Subscription) []*dto.PriceResponse {
	var validPrices []*dto.PriceResponse
	for _, p := range prices {
		if types.IsMatchingCurrency(p.Price.Currency, subscriptionObj.Currency) &&
			p.Price.BillingPeriod == subscriptionObj.BillingPeriod &&
			p.Price.BillingPeriodCount == subscriptionObj.BillingPeriodCount {
			validPrices = append(validPrices, p)
		}
	}
	return validPrices
}

func calculatePriority(filterValues map[string][]string) int {
	priority := 0
	for _, values := range filterValues {
		priority += len(values)
	}
	priority += len(filterValues) * 10
	return priority
}
