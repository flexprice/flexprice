package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/kafka"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

type SubscriptionService interface {
	CreateSubscription(ctx context.Context, req dto.CreateSubscriptionRequest) (*dto.SubscriptionResponse, error)
	GetSubscription(ctx context.Context, id string) (*dto.SubscriptionResponse, error)
	CancelSubscription(ctx context.Context, id string, cancelAtPeriodEnd bool) error
	ListSubscriptions(ctx context.Context, filter *types.SubscriptionFilter) (*dto.ListSubscriptionsResponse, error)
	GetUsageBySubscription(ctx context.Context, req *dto.GetUsageBySubscriptionRequest) (*dto.GetUsageBySubscriptionResponse, error)
}

type subscriptionService struct {
	subscriptionRepo subscription.Repository
	planRepo         plan.Repository
	priceRepo        price.Repository
	producer         kafka.MessageProducer
	eventRepo        events.Repository
	meterRepo        meter.Repository
	customerRepo     customer.Repository
	logger           *logger.Logger
}

func NewSubscriptionService(
	subscriptionRepo subscription.Repository,
	planRepo plan.Repository,
	priceRepo price.Repository,
	producer kafka.MessageProducer,
	eventRepo events.Repository,
	meterRepo meter.Repository,
	customerRepo customer.Repository,
	logger *logger.Logger,
) SubscriptionService {
	return &subscriptionService{
		subscriptionRepo: subscriptionRepo,
		planRepo:         planRepo,
		priceRepo:        priceRepo,
		producer:         producer,
		eventRepo:        eventRepo,
		meterRepo:        meterRepo,
		customerRepo:     customerRepo,
		logger:           logger,
	}
}

// CreateSubscription creates a new subscription
// TODO: Add validations and trial logic
func (s *subscriptionService) CreateSubscription(ctx context.Context, req dto.CreateSubscriptionRequest) (*dto.SubscriptionResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	subscription := req.ToSubscription(ctx)
	now := time.Now().UTC()
	if subscription.StartDate.IsZero() {
		subscription.StartDate = now
	}

	if subscription.BillingAnchor.IsZero() {
		subscription.BillingAnchor = subscription.StartDate
	}

	if subscription.BillingPeriodUnit == 0 {
		subscription.BillingPeriodUnit = 1
	}

	nextBillingDate, err := types.NextBillingDate(subscription.StartDate, subscription.BillingPeriodUnit, subscription.BillingPeriod)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate next billing date: %w", err)
	}

	subscription.CurrentPeriodStart = subscription.StartDate
	subscription.CurrentPeriodEnd = nextBillingDate

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

	// TODO: Think of a better way to handle this initialization of other services
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
		response.Subscriptions[i] = &dto.SubscriptionResponse{
			Subscription: sub,
		}
	}

	return response, nil
}

func (s *subscriptionService) GetUsageBySubscription(ctx context.Context, req *dto.GetUsageBySubscriptionRequest) (*dto.GetUsageBySubscriptionResponse, error) {
	response := &dto.GetUsageBySubscriptionResponse{}

	eventService := NewEventService(s.producer, s.eventRepo, s.meterRepo, s.logger)
	priceService := NewPriceService(s.priceRepo, s.logger)

	subscriptionResponse, err := s.GetSubscription(ctx, req.SubscriptionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get subscription: %w", err)
	}

	subscription := subscriptionResponse.Subscription
	plan := subscriptionResponse.Plan
	pricesResponse := plan.Prices

	customer, err := s.customerRepo.Get(ctx, subscription.CustomerID)
	if err != nil {
		return nil, fmt.Errorf("failed to get customer: %w", err)
	}

	usageStartTime := subscription.CurrentPeriodStart
	usageEndTime := subscription.CurrentPeriodEnd

	totalCost := uint64(0)
	for _, priceResponse := range pricesResponse {
		priceObj := priceResponse.Price
		usage, err := eventService.GetUsageByMeter(ctx, &dto.GetUsageByMeterRequest{
			MeterID:            priceObj.MeterID,
			ExternalCustomerID: customer.ExternalID,
			StartTime:          usageStartTime,
			EndTime:            usageEndTime,
			Filters:            priceObj.FilterValues,
		})

		if err != nil {
			return nil, fmt.Errorf("failed to get usage: %w", err)
		}

		s.logger.Debugf(
			"calculated meter usage for price %s meter %s event: %s value: %v",
			priceObj.ID, priceObj.MeterID, usage.EventName, usage.Value,
		)

		// calculate cost for the usage
		cost := priceService.CalculateCost(ctx, priceObj, usage)
		totalCost += cost

		var quantity float64
		switch v := usage.Value.(type) {
		case float64:
			quantity = v
		case uint64:
			quantity = float64(v)
		}

		response.Charges = append(response.Charges, &dto.SubscriptionUsageByMetersResponse{
			Amount:        price.GetAmountInDollars(cost),
			Currency:      priceObj.Currency,
			DisplayAmount: price.GetDisplayAmount(cost, priceObj.Currency),
			Quantity:      quantity,
			Price:         &priceResponse,
			FilterValues:  priceObj.FilterValues,
		})

	}

	response.StartTime = usageStartTime
	response.EndTime = usageEndTime
	response.Amount = price.GetAmountInDollars(totalCost)
	response.Currency = "USD"
	response.DisplayAmount = price.GetDisplayAmount(totalCost, "USD")

	return response, nil
}
