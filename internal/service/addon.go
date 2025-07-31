package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/price"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// AddonService interface defines the business logic for addon management
type AddonService interface {
	// Addon CRUD operations
	CreateAddon(ctx context.Context, req dto.CreateAddonRequest) (*dto.CreateAddonResponse, error)
	GetAddon(ctx context.Context, id string) (*dto.AddonResponse, error)
	GetAddonByLookupKey(ctx context.Context, lookupKey string) (*dto.AddonResponse, error)
	GetAddons(ctx context.Context, filter *types.AddonFilter) (*dto.ListAddonsResponse, error)
	UpdateAddon(ctx context.Context, id string, req dto.UpdateAddonRequest) (*dto.AddonResponse, error)
	DeleteAddon(ctx context.Context, id string) error

	// Subscription addon operations
	// AddAddonToSubscription(ctx context.Context, subscriptionID string, req dto.AddAddonToSubscriptionRequest) (*dto.SubscriptionAddonResponse, error)
	// RemoveAddonFromSubscription(ctx context.Context, subscriptionID, addonID string) error
	// UpdateSubscriptionAddon(ctx context.Context, subscriptionAddonID string, req dto.UpdateSubscriptionAddonRequest) (*dto.SubscriptionAddonResponse, error)
	// GetSubscriptionAddons(ctx context.Context, subscriptionID string) (*dto.ListSubscriptionAddonsResponse, error)
	// GetSubscriptionAddon(ctx context.Context, subscriptionAddonID string) (*dto.SubscriptionAddonResponse, error)

	// // Proration and lifecycle management
	// CalculateAddonProration(ctx context.Context, subscriptionID, addonID string, changeDate time.Time) (*decimal.Decimal, error)
	// CancelSubscriptionAddon(ctx context.Context, subscriptionAddonID string, reason string) error
	// PauseSubscriptionAddon(ctx context.Context, subscriptionAddonID string) error
	// ResumeSubscriptionAddon(ctx context.Context, subscriptionAddonID string) error

	// // Validation and compatibility
	// ValidateAddonCompatibility(ctx context.Context, subscriptionID, addonID string) error
}

type addonService struct {
	ServiceParams
}

func NewAddonService(params ServiceParams) AddonService {
	return &addonService{
		ServiceParams: params,
	}
}

// CreateAddon creates a new addon with associated prices and entitlements
func (s *addonService) CreateAddon(ctx context.Context, req dto.CreateAddonRequest) (*dto.CreateAddonResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Convert request to domain model
	domainAddon := req.ToAddon(ctx)
	s.Logger.Infof("domainAddon: %+v", domainAddon)

	// Start a transaction to create addon, prices, and entitlements
	err := s.DB.WithTx(ctx, func(ctx context.Context) error {
		// 1. Create the addon
		if err := s.AddonRepo.Create(ctx, domainAddon); err != nil {
			return err
		}

		// 2. Create prices in bulk if present
		if len(req.Prices) > 0 {
			prices := make([]*price.Price, len(req.Prices))
			for i, priceReq := range req.Prices {
				price, err := priceReq.ToPrice(ctx)
				if err != nil {
					return ierr.WithError(err).
						WithHint("Failed to create price").
						Mark(ierr.ErrValidation)
				}
				price.AddonID = lo.ToPtr(domainAddon.ID)
				prices[i] = price
			}

			// Create prices in bulk
			if err := s.PriceRepo.CreateBulk(ctx, prices); err != nil {
				return err
			}
		}

		// 3. Create entitlements in bulk if present
		if len(req.Entitlements) > 0 {
			entitlements := make([]*entitlement.Entitlement, len(req.Entitlements))
			for i, entReq := range req.Entitlements {
				ent := entReq.ToEntitlement(ctx, lo.ToPtr(domainAddon.ID))
				ent.AddonID = lo.ToPtr(domainAddon.ID)
				entitlements[i] = ent
			}

			// Create entitlements in bulk
			if _, err := s.EntitlementRepo.CreateBulk(ctx, entitlements); err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Return response
	response := &dto.AddonResponse{}
	return &dto.CreateAddonResponse{
		AddonResponse: response.FromDomain(domainAddon),
	}, nil
}

// GetAddon retrieves an addon by ID
func (s *addonService) GetAddon(ctx context.Context, id string) (*dto.AddonResponse, error) {
	if id == "" {
		return nil, ierr.NewError("addon ID is required").
			WithHint("Please provide a valid addon ID").
			Mark(ierr.ErrValidation)
	}

	domainAddon, err := s.AddonRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	response := &dto.AddonResponse{}
	return response.FromDomain(domainAddon), nil
}

// GetAddonByLookupKey retrieves an addon by lookup key
func (s *addonService) GetAddonByLookupKey(ctx context.Context, lookupKey string) (*dto.AddonResponse, error) {
	if lookupKey == "" {
		return nil, ierr.NewError("lookup key is required").
			WithHint("Please provide a valid lookup key").
			Mark(ierr.ErrValidation)
	}

	domainAddon, err := s.AddonRepo.GetByLookupKey(ctx, lookupKey)
	if err != nil {
		return nil, err
	}

	response := &dto.AddonResponse{}
	return response.FromDomain(domainAddon), nil
}

// GetAddons lists addons with filtering
func (s *addonService) GetAddons(ctx context.Context, filter *types.AddonFilter) (*dto.ListAddonsResponse, error) {
	if filter == nil {
		filter = types.NewAddonFilter()
	}

	if err := filter.Validate(); err != nil {
		return nil, err
	}

	result, err := s.AddonRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	count, err := s.AddonRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	response := &dto.ListAddonsResponse{
		Items: dto.AddonDomainToResponses(result),
		Pagination: types.NewPaginationResponse(
			count,
			filter.GetLimit(),
			filter.GetOffset(),
		),
	}

	return response, nil
}

// UpdateAddon updates an existing addon
func (s *addonService) UpdateAddon(ctx context.Context, id string, req dto.UpdateAddonRequest) (*dto.AddonResponse, error) {
	if id == "" {
		return nil, ierr.NewError("addon ID is required").
			WithHint("Please provide a valid addon ID").
			Mark(ierr.ErrValidation)
	}

	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Get existing addon
	domainAddon, err := s.AddonRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Apply updates
	if req.Name != nil {
		domainAddon.Name = *req.Name
	}
	if req.Description != nil {
		domainAddon.Description = *req.Description
	}
	if req.Metadata != nil {
		domainAddon.Metadata = req.Metadata
	}

	// Update addon
	if err := s.AddonRepo.Update(ctx, domainAddon); err != nil {
		return nil, err
	}

	response := &dto.AddonResponse{}
	return response.FromDomain(domainAddon), nil
}

// DeleteAddon soft deletes an addon
func (s *addonService) DeleteAddon(ctx context.Context, id string) error {
	if id == "" {
		return ierr.NewError("addon ID is required").
			WithHint("Please provide a valid addon ID").
			Mark(ierr.ErrValidation)
	}

	// Check if addon exists
	_, err := s.AddonRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	// // Check if addon is in use by any subscriptions
	// subscriptionAddons, err := s.AddonRepo.GetSubscriptionAddons(ctx, id)
	// if err != nil {
	// 	return ierr.WithError(err).
	// 		WithHint("Failed to check addon usage").
	// 		WithReportableDetails(map[string]interface{}{
	// 			"addon_id": id,
	// 		}).
	// 		Mark(ierr.ErrSystem)
	// }

	// activeAddons := lo.Filter(subscriptionAddons, func(sa *addon.SubscriptionAddon, _ int) bool {
	// 	return sa.IsActive()
	// })

	// if len(activeAddons) > 0 {
	// 	return ierr.NewError("addon is in use by active subscriptions").
	// 		WithHint("Cancel all active subscription addons before deleting").
	// 		WithReportableDetails(map[string]interface{}{
	// 			"addon_id":             id,
	// 			"active_subscriptions": len(activeAddons),
	// 		}).
	// 		Mark(ierr.ErrValidation)
	// }

	// Soft delete the addon
	if err := s.AddonRepo.Delete(ctx, id); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to delete addon").
			WithReportableDetails(map[string]interface{}{
				"addon_id": id,
			}).
			Mark(ierr.ErrSystem)
	}

	return nil
}

// // AddAddonToSubscription adds an addon to a subscription
// func (s *addonService) AddAddonToSubscription(ctx context.Context, subscriptionID string, req dto.AddAddonToSubscriptionRequest) (*dto.SubscriptionAddonResponse, error) {
// 	if subscriptionID == "" {
// 		return nil, ierr.NewError("subscription ID is required").
// 			WithHint("Please provide a valid subscription ID").
// 			Mark(ierr.ErrValidation)
// 	}

// 	if err := req.Validate(); err != nil {
// 		return nil, ierr.WithError(err).
// 			WithHint("Please check the addon data").
// 			Mark(ierr.ErrValidation)
// 	}

// 	// Validate addon compatibility
// 	if err := s.ValidateAddonCompatibility(ctx, subscriptionID, req.AddonID); err != nil {
// 		return nil, err
// 	}

// 	// Check if this is a single-instance addon and already exists
// 	domainAddon, err := s.AddonRepo.GetByID(ctx, req.AddonID)
// 	if err != nil {
// 		return nil, ierr.WithError(err).
// 			WithHint("Addon not found").
// 			WithReportableDetails(map[string]interface{}{
// 				"addon_id": req.AddonID,
// 			}).
// 			Mark(ierr.ErrNotFound)
// 	}

// 	if domainAddon.Type == types.AddonTypeSingleInstance {
// 		existingAddons, err := s.AddonRepo.GetSubscriptionAddons(ctx, subscriptionID)
// 		if err != nil {
// 			return nil, ierr.WithError(err).
// 				WithHint("Failed to check existing addons").
// 				WithReportableDetails(map[string]interface{}{
// 					"subscription_id": subscriptionID,
// 				}).
// 				Mark(ierr.ErrSystem)
// 		}

// 		for _, existing := range existingAddons {
// 			if existing.AddonID == req.AddonID && existing.AddonStatus == types.AddonStatusActive {
// 				return nil, ierr.NewError("single-instance addon already exists on subscription").
// 					WithHint("This addon can only be added once per subscription").
// 					WithReportableDetails(map[string]interface{}{
// 						"subscription_id": subscriptionID,
// 						"addon_id":        req.AddonID,
// 					}).
// 					Mark(ierr.ErrValidation)
// 			}
// 		}
// 	}

// 	// Convert to domain model
// 	subscriptionAddon := req.ToDomain(ctx, subscriptionID)
// 	if subscriptionAddon == nil {
// 		return nil, ierr.NewError("failed to convert request to domain").
// 			WithHint("Invalid addon data provided").
// 			Mark(ierr.ErrSystem)
// 	}

// 	// Calculate proration if needed
// 	if req.ProrationBehavior == types.ProrationBehaviorCreateProrations {
// 		proratedAmount, err := s.CalculateAddonProration(ctx, subscriptionID, req.AddonID, *subscriptionAddon.StartDate)
// 		if err != nil {
// 			return nil, err
// 		}
// 		subscriptionAddon.ProratedAmount = proratedAmount
// 	}

// 	// Create subscription addon
// 	if err := s.AddonRepo.CreateSubscriptionAddon(ctx, subscriptionAddon); err != nil {
// 		return nil, ierr.WithError(err).
// 			WithHint("Failed to add addon to subscription").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_id": subscriptionID,
// 				"addon_id":        req.AddonID,
// 			}).
// 			Mark(ierr.ErrSystem)
// 	}

// 	response := &dto.SubscriptionAddonResponse{}
// 	return response.FromDomain(subscriptionAddon), nil
// }

// // RemoveAddonFromSubscription removes an addon from a subscription
// func (s *addonService) RemoveAddonFromSubscription(ctx context.Context, subscriptionID, addonID string) error {
// 	if subscriptionID == "" {
// 		return ierr.NewError("subscription ID is required").
// 			WithHint("Please provide a valid subscription ID").
// 			Mark(ierr.ErrValidation)
// 	}

// 	if addonID == "" {
// 		return ierr.NewError("addon ID is required").
// 			WithHint("Please provide a valid addon ID").
// 			Mark(ierr.ErrValidation)
// 	}

// 	// Find the subscription addon
// 	subscriptionAddons, err := s.AddonRepo.GetSubscriptionAddons(ctx, subscriptionID)
// 	if err != nil {
// 		return ierr.WithError(err).
// 			WithHint("Failed to retrieve subscription addons").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_id": subscriptionID,
// 			}).
// 			Mark(ierr.ErrSystem)
// 	}

// 	var targetAddon *addon.SubscriptionAddon
// 	for _, sa := range subscriptionAddons {
// 		if sa.AddonID == addonID && sa.AddonStatus == types.AddonStatusActive {
// 			targetAddon = sa
// 			break
// 		}
// 	}

// 	if targetAddon == nil {
// 		return ierr.NewError("addon not found on subscription").
// 			WithHint("The addon is not active on this subscription").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_id": subscriptionID,
// 				"addon_id":        addonID,
// 			}).
// 			Mark(ierr.ErrNotFound)
// 	}

// 	// Cancel the addon
// 	return s.CancelSubscriptionAddon(ctx, targetAddon.ID, "removed_by_user")
// }

// // UpdateSubscriptionAddon updates a subscription addon
// func (s *addonService) UpdateSubscriptionAddon(ctx context.Context, subscriptionAddonID string, req dto.UpdateSubscriptionAddonRequest) (*dto.SubscriptionAddonResponse, error) {
// 	if subscriptionAddonID == "" {
// 		return nil, ierr.NewError("subscription addon ID is required").
// 			WithHint("Please provide a valid subscription addon ID").
// 			Mark(ierr.ErrValidation)
// 	}

// 	if err := req.Validate(); err != nil {
// 		return nil, ierr.WithError(err).
// 			WithHint("Please check the update data").
// 			Mark(ierr.ErrValidation)
// 	}

// 	// Get existing subscription addon
// 	subscriptionAddon, err := s.AddonRepo.GetSubscriptionAddonByID(ctx, subscriptionAddonID)
// 	if err != nil {
// 		return nil, ierr.WithError(err).
// 			WithHint("Subscription addon not found").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_addon_id": subscriptionAddonID,
// 			}).
// 			Mark(ierr.ErrNotFound)
// 	}

// 	// Apply updates
// 	if req.Quantity != nil {
// 		subscriptionAddon.Quantity = *req.Quantity
// 	}
// 	if req.PriceID != nil {
// 		subscriptionAddon.PriceID = *req.PriceID
// 	}
// 	if req.ProrationBehavior != nil {
// 		subscriptionAddon.ProrationBehavior = *req.ProrationBehavior
// 	}
// 	if req.Metadata != nil {
// 		subscriptionAddon.Metadata = req.Metadata
// 	}

// 	// Update subscription addon
// 	if err := s.AddonRepo.UpdateSubscriptionAddon(ctx, subscriptionAddon); err != nil {
// 		return nil, ierr.WithError(err).
// 			WithHint("Failed to update subscription addon").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_addon_id": subscriptionAddonID,
// 			}).
// 			Mark(ierr.ErrSystem)
// 	}

// 	response := &dto.SubscriptionAddonResponse{}
// 	return response.FromDomain(subscriptionAddon), nil
// }

// // GetSubscriptionAddons retrieves all addons for a subscription
// func (s *addonService) GetSubscriptionAddons(ctx context.Context, subscriptionID string) (*dto.ListSubscriptionAddonsResponse, error) {
// 	if subscriptionID == "" {
// 		return nil, ierr.NewError("subscription ID is required").
// 			WithHint("Please provide a valid subscription ID").
// 			Mark(ierr.ErrValidation)
// 	}

// 	subscriptionAddons, err := s.AddonRepo.GetSubscriptionAddons(ctx, subscriptionID)
// 	if err != nil {
// 		return nil, ierr.WithError(err).
// 			WithHint("Failed to retrieve subscription addons").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_id": subscriptionID,
// 			}).
// 			Mark(ierr.ErrSystem)
// 	}

// 	return &dto.ListSubscriptionAddonsResponse{
// 		Items: dto.SubscriptionAddonDomainToResponses(subscriptionAddons),
// 	}, nil
// }

// // GetSubscriptionAddon retrieves a specific subscription addon
// func (s *addonService) GetSubscriptionAddon(ctx context.Context, subscriptionAddonID string) (*dto.SubscriptionAddonResponse, error) {
// 	if subscriptionAddonID == "" {
// 		return nil, ierr.NewError("subscription addon ID is required").
// 			WithHint("Please provide a valid subscription addon ID").
// 			Mark(ierr.ErrValidation)
// 	}

// 	subscriptionAddon, err := s.AddonRepo.GetSubscriptionAddonByID(ctx, subscriptionAddonID)
// 	if err != nil {
// 		return nil, ierr.WithError(err).
// 			WithHint("Subscription addon not found").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_addon_id": subscriptionAddonID,
// 			}).
// 			Mark(ierr.ErrNotFound)
// 	}

// 	response := &dto.SubscriptionAddonResponse{}
// 	return response.FromDomain(subscriptionAddon), nil
// }

// // CalculateAddonProration calculates the prorated amount for an addon change
// func (s *addonService) CalculateAddonProration(ctx context.Context, subscriptionID, addonID string, changeDate time.Time) (*decimal.Decimal, error) {
// 	if subscriptionID == "" {
// 		return nil, ierr.NewError("subscription ID is required").
// 			WithHint("Please provide a valid subscription ID").
// 			Mark(ierr.ErrValidation)
// 	}

// 	if addonID == "" {
// 		return nil, ierr.NewError("addon ID is required").
// 			WithHint("Please provide a valid addon ID").
// 			Mark(ierr.ErrValidation)
// 	}

// 	// Get subscription details
// 	sub, err := s.SubRepo.Get(ctx, subscriptionID)
// 	if err != nil {
// 		return nil, ierr.WithError(err).
// 			WithHint("Subscription not found").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_id": subscriptionID,
// 			}).
// 			Mark(ierr.ErrNotFound)
// 	}

// 	// Get addon price
// 	_, err = s.AddonRepo.GetByID(ctx, addonID)
// 	if err != nil {
// 		return nil, ierr.WithError(err).
// 			WithHint("Addon not found").
// 			WithReportableDetails(map[string]interface{}{
// 				"addon_id": addonID,
// 			}).
// 			Mark(ierr.ErrNotFound)
// 	}

// 	// Get addon prices (we'll use the first fixed price for proration)
// 	filter := types.NewPriceFilter()
// 	filter.Filters = []*types.FilterCondition{
// 		{Field: "addon_id", Operator: types.FilterOperatorEQ, Value: &addonID},
// 		{Field: "type", Operator: types.FilterOperatorEQ, Value: &types.PriceTypeFixed},
// 	}

// 	pricesResult, err := s.PriceRepo.List(ctx, filter)
// 	if err != nil {
// 		return nil, ierr.WithError(err).
// 			WithHint("Failed to retrieve addon prices").
// 			WithReportableDetails(map[string]interface{}{
// 				"addon_id": addonID,
// 			}).
// 			Mark(ierr.ErrSystem)
// 	}

// 	if len(pricesResult.Items) == 0 {
// 		// No fixed price found, return zero proration
// 		zero := decimal.Zero
// 		return &zero, nil
// 	}

// 	price := pricesResult.Items[0]

// 	// Calculate days remaining in current period
// 	periodStart := sub.CurrentPeriodStart
// 	periodEnd := sub.CurrentPeriodEnd
// 	totalDays := int(periodEnd.Sub(periodStart).Hours() / 24)
// 	remainingDays := int(periodEnd.Sub(changeDate).Hours() / 24)

// 	if remainingDays <= 0 {
// 		zero := decimal.Zero
// 		return &zero, nil
// 	}

// 	// Calculate prorated amount
// 	dailyRate := price.Amount.Div(decimal.NewFromInt(int64(totalDays)))
// 	proratedAmount := dailyRate.Mul(decimal.NewFromInt(int64(remainingDays)))

// 	return &proratedAmount, nil
// }

// // CancelSubscriptionAddon cancels a subscription addon
// func (s *addonService) CancelSubscriptionAddon(ctx context.Context, subscriptionAddonID, reason string) error {
// 	if subscriptionAddonID == "" {
// 		return ierr.NewError("subscription addon ID is required").
// 			WithHint("Please provide a valid subscription addon ID").
// 			Mark(ierr.ErrValidation)
// 	}

// 	subscriptionAddon, err := s.AddonRepo.GetSubscriptionAddonByID(ctx, subscriptionAddonID)
// 	if err != nil {
// 		return ierr.WithError(err).
// 			WithHint("Subscription addon not found").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_addon_id": subscriptionAddonID,
// 			}).
// 			Mark(ierr.ErrNotFound)
// 	}

// 	// Update status to cancelled
// 	now := time.Now()
// 	subscriptionAddon.AddonStatus = types.AddonStatusCancelled
// 	subscriptionAddon.CancellationReason = reason
// 	subscriptionAddon.CancelledAt = &now
// 	subscriptionAddon.EndDate = &now

// 	if err := s.AddonRepo.UpdateSubscriptionAddon(ctx, subscriptionAddon); err != nil {
// 		return ierr.WithError(err).
// 			WithHint("Failed to cancel subscription addon").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_addon_id": subscriptionAddonID,
// 			}).
// 			Mark(ierr.ErrSystem)
// 	}

// 	return nil
// }

// // PauseSubscriptionAddon pauses a subscription addon
// func (s *addonService) PauseSubscriptionAddon(ctx context.Context, subscriptionAddonID string) error {
// 	if subscriptionAddonID == "" {
// 		return ierr.NewError("subscription addon ID is required").
// 			WithHint("Please provide a valid subscription addon ID").
// 			Mark(ierr.ErrValidation)
// 	}

// 	subscriptionAddon, err := s.AddonRepo.GetSubscriptionAddonByID(ctx, subscriptionAddonID)
// 	if err != nil {
// 		return ierr.WithError(err).
// 			WithHint("Subscription addon not found").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_addon_id": subscriptionAddonID,
// 			}).
// 			Mark(ierr.ErrNotFound)
// 	}

// 	subscriptionAddon.AddonStatus = types.AddonStatusPaused
// 	if err := s.AddonRepo.UpdateSubscriptionAddon(ctx, subscriptionAddon); err != nil {
// 		return ierr.WithError(err).
// 			WithHint("Failed to pause subscription addon").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_addon_id": subscriptionAddonID,
// 			}).
// 			Mark(ierr.ErrSystem)
// 	}

// 	return nil
// }

// // ResumeSubscriptionAddon resumes a paused subscription addon
// func (s *addonService) ResumeSubscriptionAddon(ctx context.Context, subscriptionAddonID string) error {
// 	if subscriptionAddonID == "" {
// 		return ierr.NewError("subscription addon ID is required").
// 			WithHint("Please provide a valid subscription addon ID").
// 			Mark(ierr.ErrValidation)
// 	}

// 	subscriptionAddon, err := s.AddonRepo.GetSubscriptionAddonByID(ctx, subscriptionAddonID)
// 	if err != nil {
// 		return ierr.WithError(err).
// 			WithHint("Subscription addon not found").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_addon_id": subscriptionAddonID,
// 			}).
// 			Mark(ierr.ErrNotFound)
// 	}

// 	subscriptionAddon.AddonStatus = types.AddonStatusActive
// 	if err := s.AddonRepo.UpdateSubscriptionAddon(ctx, subscriptionAddon); err != nil {
// 		return ierr.WithError(err).
// 			WithHint("Failed to resume subscription addon").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_addon_id": subscriptionAddonID,
// 			}).
// 			Mark(ierr.ErrSystem)
// 	}

// 	return nil
// }

// // ValidateAddonCompatibility validates if an addon can be added to a subscription
// func (s *addonService) ValidateAddonCompatibility(ctx context.Context, subscriptionID, addonID string) error {
// 	if subscriptionID == "" {
// 		return ierr.NewError("subscription ID is required").
// 			WithHint("Please provide a valid subscription ID").
// 			Mark(ierr.ErrValidation)
// 	}

// 	if addonID == "" {
// 		return ierr.NewError("addon ID is required").
// 			WithHint("Please provide a valid addon ID").
// 			Mark(ierr.ErrValidation)
// 	}

// 	// Get subscription
// 	sub, err := s.SubRepo.GetByID(ctx, subscriptionID)
// 	if err != nil {
// 		return ierr.WithError(err).
// 			WithHint("Subscription not found").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_id": subscriptionID,
// 			}).
// 			Mark(ierr.ErrNotFound)
// 	}

// 	// Get addon
// 	domainAddon, err := s.AddonRepo.GetByID(ctx, addonID)
// 	if err != nil {
// 		return ierr.WithError(err).
// 			WithHint("Addon not found").
// 			WithReportableDetails(map[string]interface{}{
// 				"addon_id": addonID,
// 			}).
// 			Mark(ierr.ErrNotFound)
// 	}

// 	// Check if subscription is in a valid state for addon changes
// 	if sub.SubscriptionStatus != types.SubscriptionStatusActive &&
// 		sub.SubscriptionStatus != types.SubscriptionStatusTrialing {
// 		return ierr.NewError("subscription is not in a valid state for addon changes").
// 			WithHint("Subscription must be active or trialing").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_id":     subscriptionID,
// 				"subscription_status": sub.SubscriptionStatus,
// 			}).
// 			Mark(ierr.ErrInvalidOperation)
// 	}

// 	// Get addon prices
// 	filter := types.NewPriceFilter()

// 	pricesResult, err := s.PriceRepo.List(ctx, filter)
// 	if err != nil {
// 		return ierr.WithError(err).
// 			WithHint("Failed to retrieve addon prices").
// 			WithReportableDetails(map[string]interface{}{
// 				"addon_id": addonID,
// 			}).
// 			Mark(ierr.ErrSystem)
// 	}

// 	// Check currency compatibility
// 	for _, price := range pricesResult {
// 		if price.Currency != sub.Currency {
// 			return ierr.NewError("addon price currencxy does not match subscription currency").
// 				WithHint("All addon prices must use the same currency as the subscription").
// 				WithReportableDetails(map[string]interface{}{
// 					"subscription_id":       subscriptionID,
// 					"subscription_currency": sub.Currency,
// 					"addon_currency":        price.Currency,
// 				}).
// 				Mark(ierr.ErrValidation)
// 		}
// 	}

// 	// Check billing period compatibility (addon should align with subscription billing)
// 	for _, price := range pricesResult.Items {
// 		if price.BillingPeriod != sub.BillingPeriod {
// 			return ierr.NewError("addon billing period does not match subscription billing period").
// 				WithHint("Addon billing period must match subscription billing period").
// 				WithReportableDetails(map[string]interface{}{
// 					"subscription_id":             subscriptionID,
// 					"subscription_billing_period": sub.BillingPeriod,
// 					"addon_billing_period":        price.BillingPeriod,
// 				}).
// 				Mark(ierr.ErrValidation)
// 		}
// 	}

// 	return nil
// }
