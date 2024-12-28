package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/publisher"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type SubscriptionService interface {
	CreateSubscription(ctx context.Context, req dto.CreateSubscriptionRequest) (*dto.SubscriptionResponse, error)
	GetSubscription(ctx context.Context, id string) (*dto.SubscriptionResponse, error)
	CancelSubscription(ctx context.Context, id string, cancelAtPeriodEnd bool) error
	ListSubscriptions(ctx context.Context, filter *types.SubscriptionFilter) (*dto.ListSubscriptionsResponse, error)
	GetUsageBySubscription(ctx context.Context, req *dto.GetUsageBySubscriptionRequest) (*dto.GetUsageBySubscriptionResponse, error)
	UpdateBillingPeriods(ctx context.Context) error
}

type subscriptionService struct {
	subscriptionRepo subscription.Repository
	planRepo         plan.Repository
	priceRepo        price.Repository
	eventRepo        events.Repository
	meterRepo        meter.Repository
	customerRepo     customer.Repository
	publisher        publisher.EventPublisher
	logger           *logger.Logger
}

func NewSubscriptionService(
	subscriptionRepo subscription.Repository,
	planRepo plan.Repository,
	priceRepo price.Repository,
	eventRepo events.Repository,
	meterRepo meter.Repository,
	customerRepo customer.Repository,
	publisher publisher.EventPublisher,
	logger *logger.Logger,
) SubscriptionService {
	return &subscriptionService{
		subscriptionRepo: subscriptionRepo,
		planRepo:         planRepo,
		priceRepo:        priceRepo,
		eventRepo:        eventRepo,
		meterRepo:        meterRepo,
		publisher:        publisher,
		customerRepo:     customerRepo,
		logger:           logger,
	}
}

func (s *subscriptionService) CreateSubscription(ctx context.Context, req dto.CreateSubscriptionRequest) (*dto.SubscriptionResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	plan, err := s.planRepo.Get(ctx, req.PlanID)
	if err != nil {
		return nil, fmt.Errorf("failed to get plan: %w", err)
	}

	if plan.Status != types.StatusPublished {
		return nil, fmt.Errorf("plan is not active")
	}

	prices, err := s.priceRepo.GetByPlanID(ctx, req.PlanID)
	if err != nil {
		return nil, fmt.Errorf("failed to get prices: %w", err)
	}

	if len(prices) == 0 {
		return nil, fmt.Errorf("no prices found for plan")
	}

	subscription := req.ToSubscription(ctx)
	now := time.Now().UTC()

	// Set start date and ensure it's in UTC
	if subscription.StartDate.IsZero() {
		subscription.StartDate = now
	} else {
		subscription.StartDate = subscription.StartDate.UTC()
	}

	// Set billing anchor and ensure it's in UTC
	if subscription.BillingAnchor.IsZero() {
		subscription.BillingAnchor = subscription.StartDate
	} else {
		subscription.BillingAnchor = subscription.BillingAnchor.UTC()
		// Validate that billing anchor is not before start date
		if subscription.BillingAnchor.Before(subscription.StartDate) {
			return nil, fmt.Errorf("billing anchor cannot be before start date")
		}
	}

	if subscription.BillingPeriodCount == 0 {
		subscription.BillingPeriodCount = 1
	}

	// Calculate the first billing period end date
	nextBillingDate, err := types.NextBillingDate(subscription.StartDate, subscription.BillingAnchor, subscription.BillingPeriodCount, subscription.BillingPeriod)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate next billing date: %w", err)
	}

	subscription.CurrentPeriodStart = subscription.StartDate
	subscription.CurrentPeriodEnd = nextBillingDate
	subscription.SubscriptionStatus = types.SubscriptionStatusActive

	s.logger.Infow("creating subscription",
		"customer_id", subscription.CustomerID,
		"plan_id", subscription.PlanID,
		"start_date", subscription.StartDate,
		"billing_anchor", subscription.BillingAnchor,
		"current_period_start", subscription.CurrentPeriodStart,
		"current_period_end", subscription.CurrentPeriodEnd)

	if err := s.subscriptionRepo.Create(ctx, subscription); err != nil {
		return nil, fmt.Errorf("failed to create subscription: %w", err)
	}

	return &dto.SubscriptionResponse{Subscription: subscription}, nil
}

func (s *subscriptionService) GetSubscription(ctx context.Context, id string) (*dto.SubscriptionResponse, error) {
	subscription, err := s.subscriptionRepo.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get subscription: %w", err)
	}

	planService := NewPlanService(s.planRepo, s.priceRepo, s.logger)
	plan, err := planService.GetPlan(ctx, subscription.PlanID)
	if err != nil {
		return nil, fmt.Errorf("failed to get plan: %w", err)
	}

	return &dto.SubscriptionResponse{Subscription: subscription, Plan: plan}, nil
}

func (s *subscriptionService) CancelSubscription(ctx context.Context, id string, cancelAtPeriodEnd bool) error {
	subscription, err := s.subscriptionRepo.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get subscription: %w", err)
	}

	now := time.Now().UTC()
	subscription.SubscriptionStatus = types.SubscriptionStatusCancelled
	subscription.CancelledAt = &now
	subscription.CancelAtPeriodEnd = cancelAtPeriodEnd

	if err := s.subscriptionRepo.Update(ctx, subscription); err != nil {
		return fmt.Errorf("failed to cancel subscription: %w", err)
	}

	return nil
}

func (s *subscriptionService) ListSubscriptions(ctx context.Context, filter *types.SubscriptionFilter) (*dto.ListSubscriptionsResponse, error) {
	if filter.Limit == 0 {
		filter.Limit = 10
	}

	subscriptions, err := s.subscriptionRepo.List(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to list subscriptions: %w", err)
	}

	response := &dto.ListSubscriptionsResponse{
		Subscriptions: make([]*dto.SubscriptionResponse, len(subscriptions)),
		Total:         len(subscriptions),
		Offset:        filter.Offset,
		Limit:         filter.Limit,
	}

	for i, sub := range subscriptions {
		plan, err := s.planRepo.Get(ctx, sub.PlanID)
		if err != nil {
			return nil, fmt.Errorf("failed to get plan: %w", err)
		}

		response.Subscriptions[i] = &dto.SubscriptionResponse{
			Subscription: sub,
			Plan: &dto.PlanResponse{
				Plan: plan,
			},
		}
	}

	return response, nil
}

func (s *subscriptionService) GetUsageBySubscription(ctx context.Context, req *dto.GetUsageBySubscriptionRequest) (*dto.GetUsageBySubscriptionResponse, error) {
	response := &dto.GetUsageBySubscriptionResponse{}

	eventService := NewEventService(s.eventRepo, s.meterRepo, s.publisher, s.logger)
	priceService := NewPriceService(s.priceRepo, s.logger)

	subscriptionResponse, err := s.GetSubscription(ctx, req.SubscriptionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get subscription: %w", err)
	}

	subscription := subscriptionResponse.Subscription
	plan := subscriptionResponse.Plan
	pricesResponse := plan.Prices

	// Filter only the eligible prices
	pricesResponse = filterValidPricesForSubscription(pricesResponse, subscription)

	customer, err := s.customerRepo.Get(ctx, subscription.CustomerID)
	if err != nil {
		return nil, fmt.Errorf("failed to get customer: %w", err)
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

	// Maintain meter order as they first appear in pricesResponse
	meterOrder := []string{}
	seenMeters := make(map[string]bool)
	meterPrices := make(map[string][]dto.PriceResponse)

	// Build meterPrices in the order of appearance in pricesResponse
	for _, priceResponse := range pricesResponse {
		if priceResponse.Price.Type != types.PRICE_TYPE_USAGE {
			continue
		}
		meterID := priceResponse.Price.MeterID
		if !seenMeters[meterID] {
			meterOrder = append(meterOrder, meterID)
			seenMeters[meterID] = true
		}
		meterPrices[meterID] = append(meterPrices[meterID], priceResponse)
	}

	// Pre-fetch all meter display names
	meterDisplayNames := make(map[string]string)
	for _, meterID := range meterOrder {
		meterDisplayNames[meterID] = getMeterDisplayName(ctx, s.meterRepo, meterID, meterDisplayNames)
	}

	totalCost := decimal.Zero

	s.logger.Debugw("calculating usage for subscription",
		"subscription_id", req.SubscriptionID,
		"start_time", usageStartTime,
		"end_time", usageEndTime,
		"num_prices", len(pricesResponse))

	for _, meterID := range meterOrder {
		meterPriceGroup := meterPrices[meterID]

		// Sort prices by filter count (stable order)
		sort.Slice(meterPriceGroup, func(i, j int) bool {
			return len(meterPriceGroup[i].Price.FilterValues) > len(meterPriceGroup[j].Price.FilterValues)
		})

		type filterGroup struct {
			ID           string
			Priority     int
			FilterValues map[string][]string
		}

		filterGroups := make([]filterGroup, 0, len(meterPriceGroup))
		for _, price := range meterPriceGroup {
			filterGroups = append(filterGroups, filterGroup{
				ID:           price.Price.ID,
				Priority:     calculatePriority(price.Price.FilterValues),
				FilterValues: price.Price.FilterValues,
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
			return nil, fmt.Errorf("failed to get usage for meter %s: %w", meterID, err)
		}

		// Append charges in the same order as meterPriceGroup
		for _, priceResponse := range meterPriceGroup {
			var quantity decimal.Decimal
			var matchingUsage *events.AggregationResult
			for _, usage := range usages {
				if fgID, ok := usage.Metadata["filter_group_id"]; ok && fgID == priceResponse.Price.ID {
					matchingUsage = usage
					break
				}
			}

			if matchingUsage != nil {
				quantity = matchingUsage.Value
				cost := priceService.CalculateCost(ctx, priceResponse.Price, quantity)
				totalCost = totalCost.Add(cost)

				s.logger.Debugw("calculated usage for meter",
					"meter_id", meterID,
					"quantity", quantity,
					"cost", cost,
					"total_cost", totalCost,
					"meter_display_name", meterDisplayNames[meterID],
					"subscription_id", req.SubscriptionID,
					"usage", matchingUsage,
					"price", priceResponse.Price,
					"filter_values", priceResponse.Price.FilterValues,
				)

				filteredUsageCharge := createChargeResponse(
					priceResponse.Price,
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
func (s *subscriptionService) UpdateBillingPeriods(ctx context.Context) error {
	const batchSize = 100
	now := time.Now().UTC()

	s.logger.Infow("starting billing period updates",
		"current_time", now)

	offset := 0
	for {
		filter := &types.SubscriptionFilter{
			Filter: types.Filter{
				Limit:  batchSize,
				Offset: offset,
			},
			SubscriptionStatus:     types.SubscriptionStatusActive,
			Status:                 types.StatusPublished,
			CurrentPeriodEndBefore: &now,
		}

		subs, err := s.subscriptionRepo.List(ctx, filter)
		if err != nil {
			return fmt.Errorf("failed to list subscriptions: %w", err)
		}

		s.logger.Infow("processing subscription batch",
			"batch_size", len(subs),
			"offset", offset)

		if len(subs) == 0 {
			break // No more subscriptions to process
		}

		// Process each subscription in the batch
		for _, sub := range subs {
			if err := s.processSubscriptionPeriod(ctx, sub, now); err != nil {
				s.logger.Errorw("failed to process subscription period",
					"subscription_id", sub.ID,
					"error", err)
				// Continue processing other subscriptions
				continue
			}
		}

		offset += len(subs)
		if len(subs) < batchSize {
			break // No more subscriptions to fetch
		}
	}

	return nil
}

// processSubscriptionPeriod handles the period transitions for a single subscription
func (s *subscriptionService) processSubscriptionPeriod(ctx context.Context, sub *subscription.Subscription, now time.Time) error {
	originalStart := sub.CurrentPeriodStart
	originalEnd := sub.CurrentPeriodEnd

	currentStart := sub.CurrentPeriodStart
	currentEnd := sub.CurrentPeriodEnd
	var transitions []struct {
		start time.Time
		end   time.Time
	}

	// Calculate all transitions up to the next hour boundary
	// This ensures we have a stable window for processing regardless of when the cron runs
	for currentEnd.Before(now) {
		nextStart := currentEnd
		nextEnd, err := types.NextBillingDate(nextStart, sub.BillingAnchor, sub.BillingPeriodCount, sub.BillingPeriod)
		if err != nil {
			s.logger.Errorw("failed to calculate next billing date",
				"subscription_id", sub.ID,
				"current_start", currentStart,
				"current_end", currentEnd,
				"process_up_to", now,
				"error", err)
			return err
		}

		transitions = append(transitions, struct {
			start time.Time
			end   time.Time
		}{
			start: nextStart,
			end:   nextEnd,
		})

		currentStart = nextStart
		currentEnd = nextEnd
	}

	if len(transitions) == 0 {
		s.logger.Debugw("no transitions needed for subscription",
			"subscription_id", sub.ID,
			"current_period_start", sub.CurrentPeriodStart,
			"current_period_end", sub.CurrentPeriodEnd,
			"process_up_to", now)
		return nil
	}

	// Update to the latest period
	lastTransition := transitions[len(transitions)-1]
	sub.CurrentPeriodStart = lastTransition.start
	sub.CurrentPeriodEnd = lastTransition.end

	// Handle subscription cancellation at period end
	if sub.CancelAtPeriodEnd && sub.CancelAt != nil {
		for _, t := range transitions {
			if !sub.CancelAt.After(t.end) {
				sub.SubscriptionStatus = types.SubscriptionStatusCancelled
				sub.CancelledAt = sub.CancelAt
				break
			}
		}
	}

	if err := s.subscriptionRepo.Update(ctx, sub); err != nil {
		return fmt.Errorf("failed to update subscription: %w", err)
	}

	s.logger.Infow("updated subscription billing period",
		"subscription_id", sub.ID,
		"previous_period_start", originalStart,
		"previous_period_end", originalEnd,
		"new_period_start", sub.CurrentPeriodStart,
		"new_period_end", sub.CurrentPeriodEnd,
		"process_up_to", now,
		"transitions_count", len(transitions))

	return nil
}

/// Helpers

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

func filterValidPricesForSubscription(prices []dto.PriceResponse, subscriptionObj *subscription.Subscription) []dto.PriceResponse {
	var validPrices []dto.PriceResponse
	for _, p := range prices {
		if p.Price.Currency == subscriptionObj.Currency &&
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
