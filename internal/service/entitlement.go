package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addon"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/samber/lo"
)

// EntitlementService defines the interface for entitlement operations
type EntitlementService interface {
	CreateEntitlement(ctx context.Context, req dto.CreateEntitlementRequest) (*dto.EntitlementResponse, error)
	CreateBulkEntitlement(ctx context.Context, req dto.CreateBulkEntitlementRequest) (*dto.CreateBulkEntitlementResponse, error)
	GetEntitlement(ctx context.Context, id string) (*dto.EntitlementResponse, error)
	ListEntitlements(ctx context.Context, filter *types.EntitlementFilter) (*dto.ListEntitlementsResponse, error)
	UpdateEntitlement(ctx context.Context, id string, req dto.UpdateEntitlementRequest) (*dto.EntitlementResponse, error)
	DeleteEntitlement(ctx context.Context, id string) error
	GetPlanEntitlements(ctx context.Context, planID string) (*dto.ListEntitlementsResponse, error)
	GetPlanFeatureEntitlements(ctx context.Context, planID, featureID string) (*dto.ListEntitlementsResponse, error)
	GetAddonEntitlements(ctx context.Context, addonID string) (*dto.ListEntitlementsResponse, error)
}

type entitlementService struct {
	ServiceParams
}

func NewEntitlementService(params ServiceParams) EntitlementService {
	return &entitlementService{
		ServiceParams: params,
	}
}

func (s *entitlementService) CreateEntitlement(ctx context.Context, req dto.CreateEntitlementRequest) (*dto.EntitlementResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Support both plan_id (legacy) and entity_type/entity_id
	var entityID string
	var entityType types.EntitlementEntityType

	if req.EntityType != "" && req.EntityID != "" {
		// New way: using entity_type and entity_id
		entityID = req.EntityID
		entityType = req.EntityType
	} else if req.PlanID != "" {
		// Legacy way: using plan_id
		entityID = req.PlanID
		entityType = types.ENTITLEMENT_ENTITY_TYPE_PLAN
	} else {
		return nil, ierr.NewError("either entity_type/entity_id or plan_id is required").
			WithHint("Please provide entity_type and entity_id, or plan_id for backward compatibility").
			Mark(ierr.ErrValidation)
	}

	// Validate entity exists based on entity type
	var entity interface{}
	var err error

	switch entityType {
	case types.ENTITLEMENT_ENTITY_TYPE_PLAN:
		entity, err = s.PlanRepo.Get(ctx, entityID)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("Plan not found").
				WithReportableDetails(map[string]interface{}{
					"plan_id": entityID,
				}).
				Mark(ierr.ErrNotFound)
		}
	case types.ENTITLEMENT_ENTITY_TYPE_ADDON:
		entity, err = s.AddonRepo.GetByID(ctx, entityID)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("Addon not found").
				WithReportableDetails(map[string]interface{}{
					"addon_id": entityID,
				}).
				Mark(ierr.ErrNotFound)
		}
	case types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION:
		// For subscription-scoped entitlements, validate subscription exists
		entity, err = s.SubRepo.Get(ctx, entityID)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("Subscription not found").
				WithReportableDetails(map[string]interface{}{
					"subscription_id": entityID,
				}).
				Mark(ierr.ErrNotFound)
		}
		s.Logger.Infow("creating subscription-scoped entitlement",
			"subscription_id", entityID,
			"feature_id", req.FeatureID,
			"parent_entitlement_id", req.ParentEntitlementID)
	default:
		return nil, ierr.NewError("unsupported entity type").
			WithHint("Only PLAN, ADDON, and SUBSCRIPTION entity types are supported").
			WithReportableDetails(map[string]interface{}{
				"entity_type": entityType,
			}).
			Mark(ierr.ErrValidation)
	}

	// Validate feature exists
	feature, err := s.FeatureRepo.Get(ctx, req.FeatureID)
	if err != nil {
		return nil, err
	}

	// Validate feature type matches
	if feature.Type != req.FeatureType {
		return nil, ierr.NewError("feature type mismatch").
			WithHint(fmt.Sprintf("Expected %s, got %s", feature.Type, req.FeatureType)).
			WithReportableDetails(map[string]interface{}{
				"expected": feature.Type,
				"actual":   req.FeatureType,
			}).
			Mark(ierr.ErrValidation)
	}

	// For metered features, check if it's a bucketed max meter
	if feature.Type == types.FeatureTypeMetered {
		meter, err := s.MeterRepo.GetMeter(ctx, feature.MeterID)
		if err != nil {
			return nil, err
		}

		// Entitlements are restricted for bucketed max meters
		if meter.IsBucketedMaxMeter() {
			return nil, ierr.NewError("entitlements not supported for bucketed max meters").
				WithHint("Bucketed max meters process each bucket independently and cannot have entitlements").
				WithReportableDetails(map[string]interface{}{
					"meter_id":     meter.ID,
					"bucket_size":  meter.Aggregation.BucketSize,
					"feature_type": req.FeatureType,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	// Create entitlement
	e := req.ToEntitlement(ctx)
	// Ensure entity type and ID are set correctly
	e.EntityType = entityType
	e.EntityID = entityID

	result, err := s.EntitlementRepo.Create(ctx, e)
	if err != nil {
		return nil, err
	}

	response := &dto.EntitlementResponse{Entitlement: result}

	// Add expanded fields
	response.Feature = &dto.FeatureResponse{Feature: feature}

	// Add entity-specific response based on entity type
	switch entityType {
	case types.ENTITLEMENT_ENTITY_TYPE_PLAN:
		response.Plan = &dto.PlanResponse{Plan: entity.(*plan.Plan)}
		response.PlanID = entityID
	case types.ENTITLEMENT_ENTITY_TYPE_ADDON:
		response.Addon = &dto.AddonResponse{Addon: entity.(*addon.Addon)}
		response.PlanID = entityID // Keep for backward compatibility
	case types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION:
		// For subscription entitlements, we don't add the full subscription object to avoid circular deps
		s.Logger.Infow("subscription-scoped entitlement created",
			"entitlement_id", result.ID,
			"subscription_id", entityID)
	}

	// Publish webhook event
	s.publishWebhookEvent(ctx, types.WebhookEventEntitlementCreated, result.ID)

	return response, nil
}

func (s *entitlementService) CreateBulkEntitlement(ctx context.Context, req dto.CreateBulkEntitlementRequest) (*dto.CreateBulkEntitlementResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	var response *dto.CreateBulkEntitlementResponse

	// Use transaction to ensure all entitlements are created or none
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		response = &dto.CreateBulkEntitlementResponse{
			Items: make([]*dto.EntitlementResponse, 0),
		}

		// Pre-validate all plans/addons/subscriptions and features to ensure they exist
		planIDs := make(map[string]bool)
		addonIDs := make(map[string]bool)
		subscriptionIDs := make(map[string]bool)
		featureIDs := make(map[string]bool)

		for _, entReq := range req.Items {
			// Support both legacy plan_id and new entity_type/entity_id
			var entityType types.EntitlementEntityType
			var entityID string

			if entReq.EntityType != "" && entReq.EntityID != "" {
				entityType = entReq.EntityType
				entityID = entReq.EntityID
			} else if entReq.PlanID != "" {
				entityType = types.ENTITLEMENT_ENTITY_TYPE_PLAN
				entityID = entReq.PlanID
			}

			// Collect entity IDs by type
			switch entityType {
			case types.ENTITLEMENT_ENTITY_TYPE_PLAN:
				planIDs[entityID] = true
			case types.ENTITLEMENT_ENTITY_TYPE_ADDON:
				addonIDs[entityID] = true
			case types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION:
				subscriptionIDs[entityID] = true
			}

			featureIDs[entReq.FeatureID] = true
		}

		// Validate all plans exist
		for planID := range planIDs {
			_, err := s.PlanRepo.Get(txCtx, planID)
			if err != nil {
				return ierr.WithError(err).
					WithHint(fmt.Sprintf("Plan with ID %s not found", planID)).
					WithReportableDetails(map[string]interface{}{
						"plan_id": planID,
					}).
					Mark(ierr.ErrValidation)
			}
		}

		// Validate all addons exist
		for addonID := range addonIDs {
			_, err := s.AddonRepo.GetByID(txCtx, addonID)
			if err != nil {
				return ierr.WithError(err).
					WithHint(fmt.Sprintf("Addon with ID %s not found", addonID)).
					WithReportableDetails(map[string]interface{}{
						"addon_id": addonID,
					}).
					Mark(ierr.ErrValidation)
			}
		}

		// Validate all subscriptions exist
		for subscriptionID := range subscriptionIDs {
			_, err := s.SubRepo.Get(txCtx, subscriptionID)
			if err != nil {
				return ierr.WithError(err).
					WithHint(fmt.Sprintf("Subscription with ID %s not found", subscriptionID)).
					WithReportableDetails(map[string]interface{}{
						"subscription_id": subscriptionID,
					}).
					Mark(ierr.ErrValidation)
			}
		}

		// Validate all features exist and get them for later use
		featuresByID := make(map[string]*feature.Feature)
		for featureID := range featureIDs {
			feature, err := s.FeatureRepo.Get(txCtx, featureID)
			if err != nil {
				return ierr.WithError(err).
					WithHint(fmt.Sprintf("Feature with ID %s not found", featureID)).
					WithReportableDetails(map[string]interface{}{
						"feature_id": featureID,
					}).
					Mark(ierr.ErrValidation)
			}
			featuresByID[featureID] = feature
		}

		// Create entitlements in bulk
		entitlements := make([]*entitlement.Entitlement, len(req.Items))
		for i, entReq := range req.Items {
			// Validate feature type matches
			feature := featuresByID[entReq.FeatureID]
			if feature.Type != entReq.FeatureType {
				return ierr.NewError("feature type mismatch").
					WithHint(fmt.Sprintf("Expected %s, got %s", feature.Type, entReq.FeatureType)).
					WithReportableDetails(map[string]interface{}{
						"expected":   feature.Type,
						"actual":     entReq.FeatureType,
						"feature_id": entReq.FeatureID,
						"index":      i,
					}).
					Mark(ierr.ErrValidation)
			}

			// For metered features, check if it's a bucketed max meter
			if feature.Type == types.FeatureTypeMetered {
				meter, err := s.MeterRepo.GetMeter(txCtx, feature.MeterID)
				if err != nil {
					return err
				}

				// Bucketed max meters cannot have entitlements
				if meter.IsBucketedMaxMeter() {
					return ierr.NewError("entitlements not supported for bucketed max meters").
						WithHint("Bucketed max meters process each bucket independently and cannot have entitlements").
						WithReportableDetails(map[string]interface{}{
							"meter_id":     meter.ID,
							"bucket_size":  meter.Aggregation.BucketSize,
							"feature_type": entReq.FeatureType,
							"index":        i,
						}).
						Mark(ierr.ErrValidation)
				}
			}

			ent := entReq.ToEntitlement(txCtx)
			entitlements[i] = ent
		}

		// Create entitlements in bulk
		createdEntitlements, err := s.EntitlementRepo.CreateBulk(txCtx, entitlements)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to create entitlements in bulk").
				Mark(ierr.ErrDatabase)
		}

		// Build response with expanded fields
		for _, ent := range createdEntitlements {
			entResp := &dto.EntitlementResponse{Entitlement: ent}

			// TODO: !REMOVE after migration
			if ent.EntityType == types.ENTITLEMENT_ENTITY_TYPE_PLAN {
				entResp.PlanID = ent.EntityID
			}

			// Add expanded feature
			if feature, ok := featuresByID[ent.FeatureID]; ok {
				entResp.Feature = &dto.FeatureResponse{Feature: feature}
			}

			// Add expanded entity based on entity type
			switch ent.EntityType {
			case types.ENTITLEMENT_ENTITY_TYPE_PLAN:
				plan, err := s.PlanRepo.Get(txCtx, ent.EntityID)
				if err != nil {
					return ierr.WithError(err).
						WithHint("Failed to fetch plan for entitlement").
						WithReportableDetails(map[string]interface{}{
							"plan_id": ent.EntityID,
						}).
						Mark(ierr.ErrDatabase)
				}
				entResp.Plan = &dto.PlanResponse{Plan: plan}
			case types.ENTITLEMENT_ENTITY_TYPE_ADDON:
				addon, err := s.AddonRepo.GetByID(txCtx, ent.EntityID)
				if err != nil {
					return ierr.WithError(err).
						WithHint("Failed to fetch addon for entitlement").
						WithReportableDetails(map[string]interface{}{
							"addon_id": ent.EntityID,
						}).
						Mark(ierr.ErrDatabase)
				}
				entResp.Addon = &dto.AddonResponse{Addon: addon}
			case types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION:
				// For subscription entitlements, we don't add the full subscription object to avoid circular deps
				s.Logger.Infow("subscription-scoped entitlement created in bulk",
					"entitlement_id", ent.ID,
					"subscription_id", ent.EntityID)
			}

			response.Items = append(response.Items, entResp)

			// Publish webhook event for each created entitlement
			s.publishWebhookEvent(txCtx, types.WebhookEventEntitlementCreated, ent.ID)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return response, nil
}

func (s *entitlementService) GetEntitlement(ctx context.Context, id string) (*dto.EntitlementResponse, error) {

	result, err := s.EntitlementRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	response := &dto.EntitlementResponse{Entitlement: result}

	// Add expanded fields
	feature, err := s.FeatureRepo.Get(ctx, result.FeatureID)
	if err != nil {
		return nil, err
	}
	response.Feature = &dto.FeatureResponse{Feature: feature}

	if result.EntityType == types.ENTITLEMENT_ENTITY_TYPE_PLAN {
		// TODO: !REMOVE after migration
		response.PlanID = result.EntityID

		// Keep this for backwards compatibility
		plan, err := s.PlanRepo.Get(ctx, result.EntityID)
		if err != nil {
			return nil, err
		}
		response.Plan = &dto.PlanResponse{Plan: plan}
	}

	// TODO: Implement the same for addon as we have for plan

	return response, nil
}

func (s *entitlementService) ListEntitlements(ctx context.Context, filter *types.EntitlementFilter) (*dto.ListEntitlementsResponse, error) {
	if filter == nil {
		filter = types.NewDefaultEntitlementFilter()
	}

	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewDefaultQueryFilter()
	}

	if filter.GetLimit() == 0 {
		filter.Limit = lo.ToPtr(types.GetDefaultFilter().Limit)
	}

	// Set default sort order if not specified
	if filter.QueryFilter.Sort == nil {
		filter.QueryFilter.Sort = lo.ToPtr("created_at")
		filter.QueryFilter.Order = lo.ToPtr("desc")
	}

	entitlements, err := s.EntitlementRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	count, err := s.EntitlementRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	response := &dto.ListEntitlementsResponse{
		Items: make([]*dto.EntitlementResponse, len(entitlements)),
	}

	// Create maps to store expanded data
	var featuresByID map[string]*feature.Feature
	var plansByID map[string]*plan.Plan
	var metersByID map[string]*meter.Meter

	if !filter.GetExpand().IsEmpty() {
		if filter.GetExpand().Has(types.ExpandFeatures) {
			// Collect feature IDs
			featureIDs := lo.Map(entitlements, func(e *entitlement.Entitlement, _ int) string {
				return e.FeatureID
			})

			if len(featureIDs) > 0 {
				featureFilter := types.NewNoLimitFeatureFilter()
				featureFilter.FeatureIDs = featureIDs
				features, err := s.FeatureRepo.List(ctx, featureFilter)
				if err != nil {
					return nil, err
				}

				featuresByID = make(map[string]*feature.Feature, len(features))
				for _, f := range features {
					featuresByID[f.ID] = f
				}

				s.Logger.Debugw("fetched features for entitlements", "count", len(features))
			}
		}

		if filter.GetExpand().Has(types.ExpandMeters) {
			// Collect meter IDs
			meterIDs := []string{}
			for _, f := range featuresByID {
				meterIDs = append(meterIDs, f.MeterID)
			}

			if len(meterIDs) > 0 {
				meterFilter := types.NewNoLimitMeterFilter()
				meterFilter.MeterIDs = meterIDs
				meters, err := s.MeterRepo.List(ctx, meterFilter)
				if err != nil {
					return nil, err
				}

				metersByID = make(map[string]*meter.Meter, len(meters))
				for _, m := range meters {
					metersByID[m.ID] = m
				}

				s.Logger.Debugw("fetched meters for entitlements", "count", len(meters))
			}
		}

		if filter.GetExpand().Has(types.ExpandPlans) {
			// Collect entity IDs for plans
			entityIDs := lo.Map(entitlements, func(e *entitlement.Entitlement, _ int) string {
				return e.EntityID
			})

			if len(entityIDs) > 0 {
				planFilter := types.NewNoLimitPlanFilter()
				planFilter.PlanIDs = entityIDs
				plans, err := s.PlanRepo.List(ctx, planFilter)
				if err != nil {
					return nil, err
				}

				plansByID = make(map[string]*plan.Plan, len(plans))
				for _, p := range plans {
					plansByID[p.ID] = p
				}

				s.Logger.Debugw("fetched plans for entitlements", "count", len(plans))
			}
		}
	}

	for i, e := range entitlements {
		response.Items[i] = &dto.EntitlementResponse{Entitlement: e}

		// TODO: !REMOVE after migration
		if e.EntityType == types.ENTITLEMENT_ENTITY_TYPE_PLAN {
			response.Items[i].PlanID = e.EntityID
		}

		// Add expanded feature if requested and available
		if !filter.GetExpand().IsEmpty() && filter.GetExpand().Has(types.ExpandFeatures) {
			if f, ok := featuresByID[e.FeatureID]; ok {
				response.Items[i].Feature = &dto.FeatureResponse{Feature: f}
				// Add expanded meter if requested and available
				if filter.GetExpand().Has(types.ExpandMeters) {
					if m, ok := metersByID[f.MeterID]; ok {
						response.Items[i].Feature.Meter = dto.ToMeterResponse(m)
					}
				}
			}
		}

		// Add expanded plan if requested and available
		if !filter.GetExpand().IsEmpty() && filter.GetExpand().Has(types.ExpandPlans) && e.EntityType == types.ENTITLEMENT_ENTITY_TYPE_PLAN {

			if p, ok := plansByID[e.EntityID]; ok {
				response.Items[i].Plan = &dto.PlanResponse{Plan: p}
				// TODO: !REMOVE after migration
				response.Items[i].PlanID = e.EntityID
			}

		}
	}

	response.Pagination = types.NewPaginationResponse(
		count,
		filter.GetLimit(),
		filter.GetOffset(),
	)

	return response, nil
}

func (s *entitlementService) UpdateEntitlement(ctx context.Context, id string, req dto.UpdateEntitlementRequest) (*dto.EntitlementResponse, error) {
	existing, err := s.EntitlementRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Update fields if provided
	if req.IsEnabled != nil {
		existing.IsEnabled = *req.IsEnabled
	}
	if req.UsageLimit != nil {
		// If usage limit is 0, set to nil
		// TODO: This is a workaround and might need to be revisited
		if *req.UsageLimit == 0 {
			existing.UsageLimit = nil
		} else {
			existing.UsageLimit = req.UsageLimit
		}
	}
	if req.UsageResetPeriod != "" {
		existing.UsageResetPeriod = req.UsageResetPeriod
	}
	if req.IsSoftLimit != nil {
		existing.IsSoftLimit = *req.IsSoftLimit
	}
	if req.StaticValue != "" {
		existing.StaticValue = req.StaticValue
	}

	// Validate updated entitlement
	if err := existing.Validate(); err != nil {
		return nil, err
	}

	result, err := s.EntitlementRepo.Update(ctx, existing)
	if err != nil {
		return nil, err
	}

	response := &dto.EntitlementResponse{Entitlement: result}

	// TODO: !REMOVE after migration
	if result.EntityType == types.ENTITLEMENT_ENTITY_TYPE_PLAN {
		response.PlanID = result.EntityID
	}

	// Publish webhook event
	s.publishWebhookEvent(ctx, types.WebhookEventEntitlementUpdated, result.ID)

	return response, nil
}

func (s *entitlementService) DeleteEntitlement(ctx context.Context, id string) error {

	err := s.EntitlementRepo.Delete(ctx, id)
	if err != nil {
		return err
	}

	// Publish webhook event after successful deletion
	s.publishWebhookEvent(ctx, types.WebhookEventEntitlementDeleted, id)

	return nil
}

func (s *entitlementService) GetPlanEntitlements(ctx context.Context, planID string) (*dto.ListEntitlementsResponse, error) {
	// Create a filter for the plan's entitlements
	filter := types.NewNoLimitEntitlementFilter()
	filter.WithEntityIDs([]string{planID})
	filter.WithEntityType(types.ENTITLEMENT_ENTITY_TYPE_PLAN)
	filter.WithStatus(types.StatusPublished)
	filter.WithExpand(fmt.Sprintf("%s,%s", types.ExpandFeatures, types.ExpandMeters))

	// Use the standard list function to get the entitlements with expansion
	return s.ListEntitlements(ctx, filter)
}

func (s *entitlementService) GetPlanFeatureEntitlements(ctx context.Context, planID, featureID string) (*dto.ListEntitlementsResponse, error) {
	// Create a filter for the plan's entitlements for a specific feature
	filter := types.NewNoLimitEntitlementFilter()
	filter.WithEntityIDs([]string{planID})
	filter.WithEntityType(types.ENTITLEMENT_ENTITY_TYPE_PLAN)
	filter.WithFeatureID(featureID)
	filter.WithStatus(types.StatusPublished)
	filter.WithExpand(fmt.Sprintf("%s,%s", types.ExpandFeatures, types.ExpandMeters))

	// Use the standard list function to get the entitlements with expansion
	return s.ListEntitlements(ctx, filter)
}

func (s *entitlementService) GetAddonEntitlements(ctx context.Context, addonID string) (*dto.ListEntitlementsResponse, error) {
	// Create a filter for the addon's entitlements
	filter := types.NewNoLimitEntitlementFilter()
	filter.WithEntityIDs([]string{addonID})
	filter.WithEntityType(types.ENTITLEMENT_ENTITY_TYPE_ADDON)
	filter.WithStatus(types.StatusPublished)
	filter.WithExpand(fmt.Sprintf("%s,%s", types.ExpandFeatures, types.ExpandMeters))

	// Use the standard list function to get the entitlements with expansion
	return s.ListEntitlements(ctx, filter)
}

func (s *entitlementService) publishWebhookEvent(ctx context.Context, eventName string, entitlementID string) {
	webhookPayload, err := json.Marshal(webhookDto.InternalEntitlementEvent{
		EntitlementID: entitlementID,
		TenantID:      types.GetTenantID(ctx),
	})
	if err != nil {
		s.Logger.Errorw("failed to marshal webhook payload", "error", err)
		return
	}

	webhookEvent := &types.WebhookEvent{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WEBHOOK_EVENT),
		EventName:     eventName,
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
		UserID:        types.GetUserID(ctx),
		Timestamp:     time.Now().UTC(),
		Payload:       json.RawMessage(webhookPayload),
	}
	if err := s.WebhookPublisher.PublishWebhook(ctx, webhookEvent); err != nil {
		s.Logger.Errorf("failed to publish %s event: %v", webhookEvent.EventName, err)
	}
}
