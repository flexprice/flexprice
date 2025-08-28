package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// AddSubscriptionLineItem adds a new line item to an existing subscription
func (s *subscriptionService) AddSubscriptionLineItem(ctx context.Context, subscriptionID string, req dto.CreateSubscriptionLineItemRequest) (*dto.SubscriptionLineItemResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Get the subscription
	sub, err := s.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	// Validate subscription status
	if sub.SubscriptionStatus != types.SubscriptionStatusActive {
		return nil, ierr.NewError("subscription is not active").
			WithHint("Only active subscriptions can have line items added").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": subscriptionID,
				"status":          sub.SubscriptionStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	// Initialize line item params
	params := dto.LineItemParams{
		Subscription: sub,
	}

	// Get entity details and price with expanded data
	switch {
	case req.PriceID != "":
		// Get base price to determine entity type
		price, err := s.PriceRepo.Get(ctx, req.PriceID)
		if err != nil {
			return nil, err
		}

		if price.EntityType == types.PRICE_ENTITY_TYPE_PLAN {
			planService := NewPlanService(s.ServiceParams)
			planResponse, err := planService.GetPlan(ctx, price.EntityID)
			if err != nil {
				return nil, err
			}
			params.Plan = planResponse
			params.EntityType = types.SubscriptionLineItemEntitiyTypePlan
		} else {
			addonService := NewAddonService(s.ServiceParams)
			addonResponse, err := addonService.GetAddon(ctx, price.EntityID)
			if err != nil {
				return nil, err
			}
			params.Addon = addonResponse
			params.EntityType = types.SubscriptionLineItemEntitiyTypeAddon

		}
	}

	// Create the line item
	lineItem := req.ToSubscriptionLineItem(ctx, params)

	if err := s.LineItemRepo.Create(ctx, lineItem); err != nil {
		return nil, err
	}

	return &dto.SubscriptionLineItemResponse{SubscriptionLineItem: lineItem}, nil
}

// DeleteSubscriptionLineItem marks a line item as deleted by setting its end date
func (s *subscriptionService) DeleteSubscriptionLineItem(ctx context.Context, lineItemID string, req dto.DeleteSubscriptionLineItemRequest) (*dto.SubscriptionLineItemResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Get the line item
	lineItem, err := s.LineItemRepo.Get(ctx, lineItemID)
	if err != nil {
		return nil, err
	}

	// Set end date and update
	lineItem.EndDate = req.EndDate.UTC()
	if lineItem.EndDate.IsZero() {
		lineItem.EndDate = time.Now().UTC()
	}

	if err := s.LineItemRepo.Update(ctx, lineItem); err != nil {
		return nil, err
	}

	return &dto.SubscriptionLineItemResponse{SubscriptionLineItem: lineItem}, nil
}
