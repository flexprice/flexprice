package stripe

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stripe/stripe-go/v82"
)

type ServiceDependencies = interfaces.ServiceDependencies

type StripePlanService interface {
	CreatePlan(ctx context.Context, planID string, service *ServiceDependencies) (string, error)
	UpdatePlan(ctx context.Context, stripeProductID string, services *ServiceDependencies) (*dto.PlanResponse, error)
	DeletePlan(ctx context.Context, stripeProductID string, services *ServiceDependencies) error
}

type stripePlanService struct {
	client *Client
	logger *logger.Logger
}

func NewStripePlanService(client *Client, logger *logger.Logger) *stripePlanService {
	return &stripePlanService{
		client: client,
		logger: logger,
	}
}

// fetchStripeProduct retrieves a product from Stripe
func (s *stripePlanService) fetchStripeProduct(ctx context.Context, productID string) (*stripe.Product, error) {

	stripeClient, _, err := s.client.GetStripeClient(ctx)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get Stripe client").
			Mark(ierr.ErrSystem)
	}

	// Retrieve the product from Stripe
	product, err := stripeClient.V1Products.Retrieve(ctx, productID, nil)
	if err != nil {
		s.logger.Errorw("failed to retrieve product from Stripe",
			"error", err,
			"product_id", productID,
		)
		return nil, ierr.NewError("failed to retrieve product from Stripe").
			WithHint("Could not fetch product information from Stripe").
			WithReportableDetails(map[string]interface{}{
				"product_id": productID,
				"error":      err.Error(),
			}).
			Mark(ierr.ErrSystem)
	}

	return product, nil
}

func (s *stripePlanService) CreatePlan(ctx context.Context, planID string, services *ServiceDependencies) (string, error) {
	var planIDResult string

	err := services.DB.WithTx(ctx, func(txCtx context.Context) error {
		// Check if the plan already exists in Entity Mapping
		filter := &types.EntityIntegrationMappingFilter{
			EntityType:        types.IntegrationEntityTypePlan,
			ProviderTypes:     []string{"stripe"},
			ProviderEntityIDs: []string{planID},
		}

		existingMappings, err := services.EntityIntegrationMappingService.GetEntityIntegrationMappings(txCtx, filter)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to check for existing plan mapping").
				Mark(ierr.ErrInternal)
		}

		// If yes: return the plan ID
		if len(existingMappings.Items) > 0 {
			existingMapping := existingMappings.Items[0]
			planIDResult = existingMapping.EntityID
			return nil
		}

		// Fetch the Product from Stripe
		stripeProduct, err := s.fetchStripeProduct(txCtx, planID)
		if err != nil {
			return err
		}

		// Create a plan with Stripe product data
		createPlanReq := dto.CreatePlanRequest{
			Name:        stripeProduct.Name,
			Description: stripeProduct.Description,
			LookupKey:   planID,
		}

		// Validate the request
		if err := createPlanReq.Validate(); err != nil {
			return ierr.WithError(err).
				WithHint("Invalid plan data for Stripe plan creation").
				Mark(ierr.ErrValidation)
		}

		// Create the plan using the plan service
		createPlanResp, err := services.PlanService.CreatePlan(txCtx, createPlanReq)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to create plan").
				Mark(ierr.ErrInternal)
		}

		// Create entity mapping for plan
		entityMappingReq := dto.CreateEntityIntegrationMappingRequest{
			EntityID:         createPlanResp.ID,
			EntityType:       types.IntegrationEntityTypePlan,
			ProviderType:     "stripe",
			ProviderEntityID: planID,
			Metadata: map[string]interface{}{
				"created_via":    "stripe_plan_service",
				"stripe_plan_id": planID,
			},
		}

		// Validate the mapping request
		if err := entityMappingReq.Validate(); err != nil {
			return ierr.WithError(err).
				WithHint("Invalid entity mapping data").
				Mark(ierr.ErrValidation)
		}

		// Create the entity integration mapping
		entityMappingService := services.EntityIntegrationMappingService
		_, err = entityMappingService.CreateEntityIntegrationMapping(txCtx, entityMappingReq)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to create entity integration mapping").
				Mark(ierr.ErrInternal)
		}

		planIDResult = createPlanResp.ID
		return nil
	})

	if err != nil {
		return "", err
	}

	return planIDResult, nil
}

func (s *stripePlanService) UpdatePlan(ctx context.Context, stripeProductID string, services *ServiceDependencies) (*dto.PlanResponse, error) {
	if stripeProductID == "" {
		return nil, ierr.NewError("stripe product ID is required").
			WithHint("Stripe product ID is required").
			Mark(ierr.ErrValidation)
	}

	var planResponse *dto.PlanResponse

	err := services.DB.WithTx(ctx, func(txCtx context.Context) error {
		// Find the FlexPrice plan ID by looking up the entity mapping with Stripe product ID
		filter := &types.EntityIntegrationMappingFilter{
			ProviderEntityIDs: []string{stripeProductID},
			EntityType:        types.IntegrationEntityTypePlan,
			ProviderTypes:     []string{"stripe"},
		}

		mappings, err := services.EntityIntegrationMappingService.GetEntityIntegrationMappings(txCtx, filter)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to find plan mapping for Stripe product").
				Mark(ierr.ErrInternal)
		}

		if len(mappings.Items) == 0 {
			return ierr.NewError("no FlexPrice plan found for Stripe product").
				WithHint("No FlexPrice plan is mapped to this Stripe product").
				WithReportableDetails(map[string]interface{}{
					"stripe_product_id": stripeProductID,
				}).
				Mark(ierr.ErrNotFound)
		}

		// Get the FlexPrice plan ID from the mapping
		flexPricePlanID := mappings.Items[0].EntityID

		stripeProduct, err := s.fetchStripeProduct(txCtx, stripeProductID)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to fetch updated product data from Stripe").
				Mark(ierr.ErrSystem)
		}

		// Create update request with Stripe data
		req := dto.UpdatePlanRequest{}

		// Update the request with Stripe data to ensure consistency
		if stripeProduct.Name != "" {
			req.Name = &stripeProduct.Name
		}
		if stripeProduct.Description != "" {
			req.Description = &stripeProduct.Description
		}

		// Use the regular plan service to update the FlexPrice plan
		planResponse, err = services.PlanService.UpdatePlan(txCtx, flexPricePlanID, req)
		return err
	})

	if err != nil {
		return nil, err
	}

	return planResponse, nil
}

func (s *stripePlanService) DeletePlan(ctx context.Context, stripeProductID string, services *ServiceDependencies) error {
	if stripeProductID == "" {
		return ierr.NewError("stripe product ID is required").
			WithHint("Stripe product ID is required").
			Mark(ierr.ErrValidation)
	}

	return services.DB.WithTx(ctx, func(txCtx context.Context) error {
		// Find the FlexPrice plan ID by looking up the entity mapping with Stripe product ID
		filter := &types.EntityIntegrationMappingFilter{
			ProviderEntityIDs: []string{stripeProductID},
			EntityType:        types.IntegrationEntityTypePlan,
			ProviderTypes:     []string{"stripe"},
		}

		mappings, err := services.EntityIntegrationMappingService.GetEntityIntegrationMappings(txCtx, filter)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to find plan mapping for Stripe product").
				Mark(ierr.ErrInternal)
		}

		if len(mappings.Items) == 0 {
			return ierr.NewError("no FlexPrice plan found for Stripe product").
				WithHint("No FlexPrice plan is mapped to this Stripe product").
				WithReportableDetails(map[string]interface{}{
					"stripe_product_id": stripeProductID,
				}).
				Mark(ierr.ErrNotFound)
		}

		// Get the FlexPrice plan ID from the mapping
		flexPricePlanID := mappings.Items[0].EntityID

		// Use the regular plan service to delete the FlexPrice plan first
		if err := services.PlanService.DeletePlan(txCtx, flexPricePlanID); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to delete FlexPrice plan").
				Mark(ierr.ErrInternal)
		}

		// Clean up entity integration mappings
		for _, mapping := range mappings.Items {
			if err := services.EntityIntegrationMappingService.DeleteEntityIntegrationMapping(txCtx, mapping.ID); err != nil {
				s.logger.Errorw("failed to delete entity integration mapping",
					"error", err,
					"mapping_id", mapping.ID,
					"plan_id", flexPricePlanID,
					"stripe_product_id", stripeProductID)
				// Continue cleanup even if one mapping deletion fails
			}
		}
		return nil
	})
}
