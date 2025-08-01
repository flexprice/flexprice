package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addon"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// AddonLifecycleService handles all addon lifecycle operations
type AddonLifecycleService interface {
	// Add addon to subscription
	AddAddonToSubscription(ctx context.Context, subscriptionID string, req *dto.SubscriptionAddonRequest) (*addon.SubscriptionAddon, error)

	// Remove addon from subscription
	RemoveAddonFromSubscription(ctx context.Context, subscriptionID, addonID string, reason string) error

	// Pause addon
	PauseAddon(ctx context.Context, subscriptionID, addonID string, reason string) error

	// Resume addon
	ResumeAddon(ctx context.Context, subscriptionID, addonID string) error

	// Update addon quantity
	UpdateAddonQuantity(ctx context.Context, subscriptionID, addonID string, newQuantity int) error

	// Update addon price
	UpdateAddonPrice(ctx context.Context, subscriptionID, addonID string, newPriceID string) error

	// Get subscription addons
	GetSubscriptionAddons(ctx context.Context, subscriptionID string) ([]*addon.SubscriptionAddon, error)

	// Get addon usage
	GetAddonUsage(ctx context.Context, subscriptionID, addonID string, startTime, endTime time.Time) (*dto.AddonUsageResponse, error)

	// Calculate addon charges for billing period
	CalculateAddonCharges(ctx context.Context, subscriptionID string, periodStart, periodEnd time.Time) ([]dto.CreateInvoiceLineItemRequest, decimal.Decimal, error)
}

type addonLifecycleService struct {
	ServiceParams
}

func NewAddonLifecycleService(params ServiceParams) AddonLifecycleService {
	return &addonLifecycleService{
		ServiceParams: params,
	}
}

// AddAddonToSubscription adds an addon to a subscription
func (s *addonLifecycleService) AddAddonToSubscription(
	ctx context.Context,
	subscriptionID string,
	req *dto.SubscriptionAddonRequest,
) (*addon.SubscriptionAddon, error) {
	// Validate request
	if err := req.Validate(); err != nil {
		return nil, ierr.WithError(err).Mark(ierr.ErrValidation)
	}

	// Check if subscription exists and is active
	subscription, err := s.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Subscription not found").
			Mark(ierr.ErrNotFound)
	}

	if subscription.SubscriptionStatus != types.SubscriptionStatusActive {
		return nil, ierr.NewError("subscription is not active").
			WithHint("Cannot add addon to inactive subscription").
			Mark(ierr.ErrValidation)
	}

	// Check if addon exists and is active
	addonService := NewAddonService(s.ServiceParams)
	_, err = addonService.GetAddon(ctx, req.AddonID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Addon not found or not active").
			Mark(ierr.ErrNotFound)
	}

	// Check if price exists
	priceService := NewPriceService(s.PriceRepo, s.MeterRepo, s.Logger)
	_, err = priceService.GetPrice(ctx, req.PriceID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Price not found").
			Mark(ierr.ErrNotFound)
	}

	// Check if addon is already added to subscription
	existingAddons, err := s.AddonRepo.GetSubscriptionAddons(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	for _, existingAddon := range existingAddons {
		if existingAddon.AddonID == req.AddonID && existingAddon.AddonStatus == types.AddonStatusActive {
			return nil, ierr.NewError("addon already added to subscription").
				WithHint("Addon is already active on this subscription").
				Mark(ierr.ErrAlreadyExists)
		}
	}

	// Create subscription addon
	subscriptionAddon := req.ToDomain(ctx, subscriptionID)

	err = s.AddonRepo.CreateSubscriptionAddon(ctx, subscriptionAddon)
	if err != nil {
		return nil, err
	}

	s.Logger.Infow("added addon to subscription",
		"subscription_id", subscriptionID,
		"addon_id", req.AddonID,
		"price_id", req.PriceID)

	return subscriptionAddon, nil
}

// RemoveAddonFromSubscription removes an addon from a subscription
func (s *addonLifecycleService) RemoveAddonFromSubscription(
	ctx context.Context,
	subscriptionID, addonID string,
	reason string,
) error {
	// Get subscription addon
	subscriptionAddons, err := s.AddonRepo.GetSubscriptionAddons(ctx, subscriptionID)
	if err != nil {
		return err
	}

	var targetAddon *addon.SubscriptionAddon
	for _, sa := range subscriptionAddons {
		if sa.AddonID == addonID && sa.AddonStatus == types.AddonStatusActive {
			targetAddon = sa
			break
		}
	}

	if targetAddon == nil {
		return ierr.NewError("addon not found on subscription").
			WithHint("Addon is not active on this subscription").
			Mark(ierr.ErrNotFound)
	}

	// Update addon status to cancelled
	now := time.Now()
	targetAddon.AddonStatus = types.AddonStatusCancelled
	targetAddon.CancellationReason = reason
	targetAddon.CancelledAt = &now
	targetAddon.EndDate = &now

	err = s.AddonRepo.UpdateSubscriptionAddon(ctx, targetAddon)
	if err != nil {
		return err
	}

	s.Logger.Infow("removed addon from subscription",
		"subscription_id", subscriptionID,
		"addon_id", addonID,
		"reason", reason)

	return nil
}

// PauseAddon pauses an addon on a subscription
func (s *addonLifecycleService) PauseAddon(
	ctx context.Context,
	subscriptionID, addonID string,
	reason string,
) error {
	// Get subscription addon
	subscriptionAddons, err := s.AddonRepo.GetSubscriptionAddons(ctx, subscriptionID)
	if err != nil {
		return err
	}

	var targetAddon *addon.SubscriptionAddon
	for _, sa := range subscriptionAddons {
		if sa.AddonID == addonID && sa.AddonStatus == types.AddonStatusActive {
			targetAddon = sa
			break
		}
	}

	if targetAddon == nil {
		return ierr.NewError("addon not found on subscription").
			WithHint("Addon is not active on this subscription").
			Mark(ierr.ErrNotFound)
	}

	// Update addon status to paused
	targetAddon.AddonStatus = types.AddonStatusPaused
	targetAddon.CancellationReason = reason

	err = s.AddonRepo.UpdateSubscriptionAddon(ctx, targetAddon)
	if err != nil {
		return err
	}

	s.Logger.Infow("paused addon on subscription",
		"subscription_id", subscriptionID,
		"addon_id", addonID,
		"reason", reason)

	return nil
}

// ResumeAddon resumes a paused addon on a subscription
func (s *addonLifecycleService) ResumeAddon(
	ctx context.Context,
	subscriptionID, addonID string,
) error {
	// Get subscription addon
	subscriptionAddons, err := s.AddonRepo.GetSubscriptionAddons(ctx, subscriptionID)
	if err != nil {
		return err
	}

	var targetAddon *addon.SubscriptionAddon
	for _, sa := range subscriptionAddons {
		if sa.AddonID == addonID && sa.AddonStatus == types.AddonStatusPaused {
			targetAddon = sa
			break
		}
	}

	if targetAddon == nil {
		return ierr.NewError("addon not found or not paused").
			WithHint("Addon is not paused on this subscription").
			Mark(ierr.ErrNotFound)
	}

	// Update addon status to active
	targetAddon.AddonStatus = types.AddonStatusActive
	targetAddon.CancellationReason = ""

	err = s.AddonRepo.UpdateSubscriptionAddon(ctx, targetAddon)
	if err != nil {
		return err
	}

	s.Logger.Infow("resumed addon on subscription",
		"subscription_id", subscriptionID,
		"addon_id", addonID)

	return nil
}

// UpdateAddonQuantity updates the quantity of an addon
func (s *addonLifecycleService) UpdateAddonQuantity(
	ctx context.Context,
	subscriptionID, addonID string,
	newQuantity int,
) error {
	if newQuantity <= 0 {
		return ierr.NewError("quantity must be greater than 0").
			Mark(ierr.ErrValidation)
	}

	// Get subscription addon
	subscriptionAddons, err := s.AddonRepo.GetSubscriptionAddons(ctx, subscriptionID)
	if err != nil {
		return err
	}

	var targetAddon *addon.SubscriptionAddon
	for _, sa := range subscriptionAddons {
		if sa.AddonID == addonID && sa.AddonStatus == types.AddonStatusActive {
			targetAddon = sa
			break
		}
	}

	if targetAddon == nil {
		return ierr.NewError("addon not found on subscription").
			WithHint("Addon is not active on this subscription").
			Mark(ierr.ErrNotFound)
	}

	// Update quantity
	targetAddon.Quantity = newQuantity

	err = s.AddonRepo.UpdateSubscriptionAddon(ctx, targetAddon)
	if err != nil {
		return err
	}

	s.Logger.Infow("updated addon quantity",
		"subscription_id", subscriptionID,
		"addon_id", addonID,
		"new_quantity", newQuantity)

	return nil
}

// UpdateAddonPrice updates the price of an addon
func (s *addonLifecycleService) UpdateAddonPrice(
	ctx context.Context,
	subscriptionID, addonID string,
	newPriceID string,
) error {
	// Check if new price exists
	priceService := NewPriceService(s.PriceRepo, s.MeterRepo, s.Logger)
	_, err := priceService.GetPrice(ctx, newPriceID)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Price not found").
			Mark(ierr.ErrNotFound)
	}

	// Get subscription addon
	subscriptionAddons, err := s.AddonRepo.GetSubscriptionAddons(ctx, subscriptionID)
	if err != nil {
		return err
	}

	var targetAddon *addon.SubscriptionAddon
	for _, sa := range subscriptionAddons {
		if sa.AddonID == addonID && sa.AddonStatus == types.AddonStatusActive {
			targetAddon = sa
			break
		}
	}

	if targetAddon == nil {
		return ierr.NewError("addon not found on subscription").
			WithHint("Addon is not active on this subscription").
			Mark(ierr.ErrNotFound)
	}

	// Update price
	targetAddon.PriceID = newPriceID

	err = s.AddonRepo.UpdateSubscriptionAddon(ctx, targetAddon)
	if err != nil {
		return err
	}

	s.Logger.Infow("updated addon price",
		"subscription_id", subscriptionID,
		"addon_id", addonID,
		"new_price_id", newPriceID)

	return nil
}

// GetSubscriptionAddons gets all addons for a subscription
func (s *addonLifecycleService) GetSubscriptionAddons(
	ctx context.Context,
	subscriptionID string,
) ([]*addon.SubscriptionAddon, error) {
	return s.AddonRepo.GetSubscriptionAddons(ctx, subscriptionID)
}

// GetAddonUsage gets usage for a specific addon
func (s *addonLifecycleService) GetAddonUsage(
	ctx context.Context,
	subscriptionID, addonID string,
	startTime, endTime time.Time,
) (*dto.AddonUsageResponse, error) {
	// Get subscription addon
	subscriptionAddons, err := s.AddonRepo.GetSubscriptionAddons(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	var targetAddon *addon.SubscriptionAddon
	for _, sa := range subscriptionAddons {
		if sa.AddonID == addonID {
			targetAddon = sa
			break
		}
	}

	if targetAddon == nil {
		return nil, ierr.NewError("addon not found on subscription").
			Mark(ierr.ErrNotFound)
	}

	// Get usage from subscription service
	subscriptionService := NewSubscriptionService(s.ServiceParams)
	usage, err := subscriptionService.GetUsageBySubscription(ctx, &dto.GetUsageBySubscriptionRequest{
		SubscriptionID: subscriptionID,
		StartTime:      startTime,
		EndTime:        endTime,
	})
	if err != nil {
		return nil, err
	}

	// Filter usage for this specific addon
	var addonUsage []*dto.SubscriptionUsageByMetersResponse
	for _, charge := range usage.Charges {
		if charge.Price.ID == targetAddon.PriceID {
			addonUsage = append(addonUsage, charge)
		}
	}

	return &dto.AddonUsageResponse{
		SubscriptionID: subscriptionID,
		AddonID:        addonID,
		PriceID:        targetAddon.PriceID,
		Quantity:       targetAddon.Quantity,
		UsageLimit:     targetAddon.UsageLimit,
		Charges:        addonUsage,
		PeriodStart:    startTime,
		PeriodEnd:      endTime,
	}, nil
}

// CalculateAddonCharges calculates charges for addons in a billing period
func (s *addonLifecycleService) CalculateAddonCharges(
	ctx context.Context,
	subscriptionID string,
	periodStart, periodEnd time.Time,
) ([]dto.CreateInvoiceLineItemRequest, decimal.Decimal, error) {
	// Get subscription addons
	subscriptionAddons, err := s.AddonRepo.GetSubscriptionAddons(ctx, subscriptionID)
	if err != nil {
		return nil, decimal.Zero, err
	}

	var lineItems []dto.CreateInvoiceLineItemRequest
	totalAmount := decimal.Zero

	priceService := NewPriceService(s.PriceRepo, s.MeterRepo, s.Logger)

	for _, subscriptionAddon := range subscriptionAddons {
		// Skip inactive addons
		if subscriptionAddon.AddonStatus != types.AddonStatusActive {
			continue
		}

		// Skip addons that don't overlap with billing period
		if subscriptionAddon.EndDate != nil && subscriptionAddon.EndDate.Before(periodStart) {
			continue
		}
		if subscriptionAddon.StartDate != nil && subscriptionAddon.StartDate.After(periodEnd) {
			continue
		}

		// Get price
		price, err := priceService.GetPrice(ctx, subscriptionAddon.PriceID)
		if err != nil {
			return nil, decimal.Zero, err
		}

		// Calculate amount based on price type
		var amount decimal.Decimal
		switch price.PriceType {
		case types.PRICE_TYPE_FIXED:
			amount = priceService.CalculateCost(ctx, price.Price, subscriptionAddon.Quantity)
		case types.PRICE_TYPE_USAGE:
			// For usage-based pricing, we need to get actual usage
			usage, err := s.GetAddonUsage(ctx, subscriptionID, subscriptionAddon.AddonID, periodStart, periodEnd)
			if err != nil {
				return nil, decimal.Zero, err
			}

			// Calculate usage amount
			for _, charge := range usage.Charges {
				amount = amount.Add(decimal.NewFromFloat(charge.Amount))
			}
		default:
			return nil, decimal.Zero, ierr.NewError(fmt.Sprintf("unsupported price type: %s", price.PriceType)).
				Mark(ierr.ErrValidation)
		}

		// Create line item
		lineItem := dto.CreateInvoiceLineItemRequest{
			PriceID:     &subscriptionAddon.PriceID,
			PriceType:   &price.PriceType,
			DisplayName: &price.DisplayName,
			Amount:      amount,
			Quantity:    decimal.NewFromInt(int64(subscriptionAddon.Quantity)),
			PeriodStart: &periodStart,
			PeriodEnd:   &periodEnd,
			Metadata: types.Metadata{
				"addon_id":        subscriptionAddon.AddonID,
				"subscription_id": subscriptionID,
				"addon_quantity":  subscriptionAddon.Quantity,
				"addon_status":    string(subscriptionAddon.AddonStatus),
				"description":     fmt.Sprintf("Addon: %s", price.DisplayName),
			},
		}

		lineItems = append(lineItems, lineItem)
		totalAmount = totalAmount.Add(amount)
	}

	return lineItems, totalAmount, nil
}
