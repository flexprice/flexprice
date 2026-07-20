package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// MarketplaceService registers marketplace agreements, linking a Flexprice subscription, customer
// and plan to their AWS identifiers.
type MarketplaceService interface {
	RegisterAgreement(ctx context.Context, req dto.RegisterMarketplaceAgreementRequest) (*dto.RegisterMarketplaceAgreementResponse, error)
}

type marketplaceService struct {
	ServiceParams
}

// NewMarketplaceService creates a new marketplace service.
func NewMarketplaceService(params ServiceParams) MarketplaceService {
	return &marketplaceService{ServiceParams: params}
}

// RegisterAgreement validates the subscription and the agreement uniqueness rules, then creates
// the plan, subscription and customer mappings in one transaction. The plan mapping holds the
// plan-level AWS configuration (product code, concurrent-agreements flag, dimension) and is created
// once per plan; later agreements for the same plan reuse it.
func (s *marketplaceService) RegisterAgreement(ctx context.Context, req dto.RegisterMarketplaceAgreementRequest) (*dto.RegisterMarketplaceAgreementResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// req.Provider is already validated against allowedMarketplaceProviders in Validate() above.
	providerType := string(req.Provider)

	// The subscription must already exist and be active; this endpoint never creates subscriptions.
	sub, err := s.SubRepo.Get(ctx, req.SubscriptionID)
	if err != nil {
		s.Logger.Error(ctx, "marketplace agreement registration failed",
			"subscription_id", req.SubscriptionID, "customer_id", req.CustomerID, "plan_id", req.PlanID,
			"error", err, "stage", "get_subscription")
		return nil, err
	}
	if sub.SubscriptionStatus != types.SubscriptionStatusActive {
		s.Logger.Error(ctx, "marketplace agreement registration failed",
			"subscription_id", req.SubscriptionID, "customer_id", req.CustomerID, "plan_id", req.PlanID,
			"subscription_status", sub.SubscriptionStatus, "error", "subscription is not active", "stage", "validate_subscription")
		return nil, ierr.NewError("subscription is not active").
			WithHintf("Subscription %s must be active to register a marketplace agreement", req.SubscriptionID).
			WithReportableDetails(map[string]any{
				"subscription_id": req.SubscriptionID,
				"status":          sub.SubscriptionStatus,
			}).
			Mark(ierr.ErrValidation)
	}
	if sub.CustomerID != req.CustomerID {
		s.Logger.Error(ctx, "marketplace agreement registration failed",
			"subscription_id", req.SubscriptionID, "customer_id", req.CustomerID, "plan_id", req.PlanID,
			"error", "customer_id does not match subscription", "stage", "validate_customer")
		return nil, ierr.NewError("customer_id does not match subscription").
			WithHintf("Subscription %s belongs to a different customer", req.SubscriptionID).
			Mark(ierr.ErrValidation)
	}
	if sub.PlanID != req.PlanID {
		s.Logger.Error(ctx, "marketplace agreement registration failed",
			"subscription_id", req.SubscriptionID, "customer_id", req.CustomerID, "plan_id", req.PlanID,
			"error", "plan_id does not match subscription", "stage", "validate_plan")
		return nil, ierr.NewError("plan_id does not match subscription").
			WithHintf("Subscription %s belongs to a different plan", req.SubscriptionID).
			Mark(ierr.ErrValidation)
	}

	// A license_arn maps to exactly one subscription. It is stored as the subscription mapping's
	// provider_entity_id, so look it up directly by that indexed field.
	existingByLicense, err := s.EntityIntegrationMappingRepo.List(ctx, &types.EntityIntegrationMappingFilter{
		QueryFilter:       types.NewNoLimitPublishedQueryFilter(),
		EntityType:        types.IntegrationEntityTypeSubscription,
		ProviderTypes:     []string{providerType},
		ProviderEntityIDs: []string{req.LicenseArn},
	})
	if err != nil {
		s.Logger.Error(ctx, "marketplace agreement registration failed",
			"subscription_id", req.SubscriptionID, "customer_id", req.CustomerID, "plan_id", req.PlanID,
			"error", err, "stage", "list_license_mappings")
		return nil, err
	}
	if len(existingByLicense) > 0 && existingByLicense[0].EntityID != req.SubscriptionID {
		s.Logger.Error(ctx, "marketplace agreement registration failed",
			"subscription_id", req.SubscriptionID, "customer_id", req.CustomerID, "plan_id", req.PlanID,
			"existing_subscription_id", existingByLicense[0].EntityID,
			"error", "license_arn already registered to a different subscription", "stage", "validate_license_uniqueness")
		return nil, ierr.NewError("license_arn already registered").
			WithHintf("AWS license_arn %s is already registered to a different subscription", req.LicenseArn).
			Mark(ierr.ErrAlreadyExists)
	}

	// A subscription maps to at most one license_arn; it cannot be re-pointed to a different one.
	existingSubMapping, err := s.EntityIntegrationMappingRepo.List(ctx, &types.EntityIntegrationMappingFilter{
		QueryFilter:   types.NewNoLimitPublishedQueryFilter(),
		EntityType:    types.IntegrationEntityTypeSubscription,
		EntityID:      req.SubscriptionID,
		ProviderTypes: []string{providerType},
	})
	if err != nil {
		s.Logger.Error(ctx, "marketplace agreement registration failed",
			"subscription_id", req.SubscriptionID, "customer_id", req.CustomerID, "plan_id", req.PlanID,
			"error", err, "stage", "list_subscription_mappings")
		return nil, err
	}
	if len(existingSubMapping) > 0 && existingSubMapping[0].ProviderEntityID != req.LicenseArn {
		s.Logger.Error(ctx, "marketplace agreement registration failed",
			"subscription_id", req.SubscriptionID, "customer_id", req.CustomerID, "plan_id", req.PlanID,
			"error", "subscription already mapped to a different license_arn", "stage", "validate_subscription_uniqueness")
		return nil, ierr.NewError("subscription already mapped to a different license_arn").
			WithHintf("Subscription %s is already registered against a different AWS license_arn", req.SubscriptionID).
			Mark(ierr.ErrAlreadyExists)
	}

	var planMappingID, subMappingID, custMappingID string

	err = s.DB.WithTx(ctx, func(txCtx context.Context) error {
		// Plan mapping carries the plan-level AWS config (product_code + concurrent_agreements +
		// dimension). It may already exist if another agreement for the same plan was registered
		// earlier — created once, never updated.
		planMapping, txErr := s.createMappingIfAbsent(txCtx, providerType, types.IntegrationEntityTypePlan, req.PlanID, req.ProductCode, map[string]interface{}{
			"concurrent_agreements": req.ConcurrentAgreements,
			"dimension":             req.Dimension,
		})
		if txErr != nil {
			return txErr
		}
		planMappingID = planMapping.ID

		// Subscription mapping: license_arn. A new agreement is always a new subscription, so this
		// is always a fresh row.
		subMapping, txErr := s.createMappingIfAbsent(txCtx, providerType, types.IntegrationEntityTypeSubscription, req.SubscriptionID, req.LicenseArn, nil)
		if txErr != nil {
			return txErr
		}
		subMappingID = subMapping.ID

		// Customer mapping: customer_aws_account_id. May already exist if the same customer holds an
		// earlier agreement — created once, never updated.
		custMapping, txErr := s.createMappingIfAbsent(txCtx, providerType, types.IntegrationEntityTypeCustomer, req.CustomerID, req.CustomerAWSAccountID, nil)
		if txErr != nil {
			return txErr
		}
		custMappingID = custMapping.ID

		return nil
	})
	if err != nil {
		s.Logger.Error(ctx, "marketplace agreement registration failed",
			"subscription_id", req.SubscriptionID, "customer_id", req.CustomerID, "plan_id", req.PlanID,
			"error", err, "stage", "create_mappings")
		return nil, err
	}

	s.Logger.Info(ctx, "marketplace agreement registered",
		"subscription_id", req.SubscriptionID,
		"customer_id", req.CustomerID,
		"plan_id", req.PlanID,
		"provider", req.Provider,
	)

	return &dto.RegisterMarketplaceAgreementResponse{
		PlanMappingID:         planMappingID,
		SubscriptionMappingID: subMappingID,
		CustomerMappingID:     custMappingID,
		Status:                "active",
	}, nil
}

// createMappingIfAbsent creates an entity_integration_mapping row for (entityType, entityID,
// providerType) if one doesn't already exist, and returns the existing row otherwise. It never
// updates an existing mapping — agreement registration only ever creates mappings; a plan or
// customer mapping shared across agreements is left exactly as first written.
func (s *marketplaceService) createMappingIfAbsent(
	ctx context.Context,
	providerType string,
	entityType types.IntegrationEntityType,
	entityID string,
	providerEntityID string,
	metadata map[string]interface{},
) (*entityintegrationmapping.EntityIntegrationMapping, error) {
	existing, err := s.EntityIntegrationMappingRepo.List(ctx, &types.EntityIntegrationMappingFilter{
		QueryFilter:   types.NewNoLimitPublishedQueryFilter(),
		EntityID:      entityID,
		EntityType:    entityType,
		ProviderTypes: []string{providerType},
	})
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		return existing[0], nil
	}

	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityID:         entityID,
		EntityType:       entityType,
		ProviderType:     providerType,
		ProviderEntityID: providerEntityID,
		Metadata:         metadata,
		EnvironmentID:    types.GetEnvironmentID(ctx),
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
	if err := entityintegrationmapping.Validate(mapping); err != nil {
		return nil, err
	}
	if err := s.EntityIntegrationMappingRepo.Create(ctx, mapping); err != nil {
		return nil, err
	}
	return mapping, nil
}
