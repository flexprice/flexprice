package service

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// EntitlementService defines the interface for entitlement operations
type EntitlementService interface {
	CreateEntitlement(ctx context.Context, req dto.CreateEntitlementRequest) (*dto.EntitlementResponse, error)
	GetEntitlement(ctx context.Context, id string) (*dto.EntitlementResponse, error)
	ListEntitlements(ctx context.Context, filter *types.EntitlementFilter) (*dto.ListEntitlementsResponse, error)
	UpdateEntitlement(ctx context.Context, id string, req dto.UpdateEntitlementRequest) (*dto.EntitlementResponse, error)
	DeleteEntitlement(ctx context.Context, id string) error
	GetPlanEntitlements(ctx context.Context, planID string) (*dto.ListEntitlementsResponse, error)
	GetPlanFeatureEntitlements(ctx context.Context, planID, featureID string) (*dto.ListEntitlementsResponse, error)
}

type entitlementService struct {
	repo        entitlement.Repository
	planRepo    plan.Repository
	featureRepo feature.Repository
	log         *logger.Logger
}

func NewEntitlementService(
	repo entitlement.Repository,
	planRepo plan.Repository,
	featureRepo feature.Repository,
	log *logger.Logger,
) EntitlementService {
	return &entitlementService{
		repo:        repo,
		planRepo:    planRepo,
		featureRepo: featureRepo,
		log:         log,
	}
}

func (s *entitlementService) CreateEntitlement(ctx context.Context, req dto.CreateEntitlementRequest) (*dto.EntitlementResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	if req.PlanID == "" {
		return nil, fmt.Errorf("plan_id is required")
	}

	// Validate plan exists
	plan, err := s.planRepo.Get(ctx, req.PlanID)
	if err != nil {
		return nil, fmt.Errorf("plan not found: %w", err)
	}

	// Validate feature exists
	feature, err := s.featureRepo.Get(ctx, req.FeatureID)
	if err != nil {
		return nil, fmt.Errorf("feature not found: %w", err)
	}

	// Validate feature type matches
	if feature.Type != req.FeatureType {
		return nil, fmt.Errorf("feature type mismatch: expected %s, got %s", feature.Type, req.FeatureType)
	}

	// Create entitlement
	e := req.ToEntitlement(ctx)

	result, err := s.repo.Create(ctx, e)
	if err != nil {
		return nil, fmt.Errorf("failed to create entitlement: %w", err)
	}

	response := &dto.EntitlementResponse{Entitlement: result}

	// Add expanded fields
	response.Feature = &dto.FeatureResponse{Feature: feature}
	response.Plan = &dto.PlanResponse{Plan: plan}

	return response, nil
}

func (s *entitlementService) GetEntitlement(ctx context.Context, id string) (*dto.EntitlementResponse, error) {
	result, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get entitlement: %w", err)
	}

	response := &dto.EntitlementResponse{Entitlement: result}

	// Add expanded fields
	feature, err := s.featureRepo.Get(ctx, result.FeatureID)
	if err != nil {
		return nil, fmt.Errorf("failed to get feature: %w", err)
	}
	response.Feature = &dto.FeatureResponse{Feature: feature}

	plan, err := s.planRepo.Get(ctx, result.PlanID)
	if err != nil {
		return nil, fmt.Errorf("failed to get plan: %w", err)
	}
	response.Plan = &dto.PlanResponse{Plan: plan}

	return response, nil
}

func (s *entitlementService) ListEntitlements(ctx context.Context, filter *types.EntitlementFilter) (*dto.ListEntitlementsResponse, error) {
	if filter == nil {
		filter = types.NewDefaultEntitlementFilter()
	}

	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewDefaultQueryFilter()
	}

	// Set default sort order if not specified
	if filter.QueryFilter.Sort == nil {
		filter.QueryFilter.Sort = lo.ToPtr("created_at")
		filter.QueryFilter.Order = lo.ToPtr("desc")
	}

	entitlements, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to list entitlements: %w", err)
	}

	count, err := s.repo.Count(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to count entitlements: %w", err)
	}

	response := &dto.ListEntitlementsResponse{
		Items: make([]*dto.EntitlementResponse, len(entitlements)),
	}

	// Create maps to store expanded data
	var featuresByID map[string]*feature.Feature
	var plansByID map[string]*plan.Plan

	if !filter.GetExpand().IsEmpty() {
		if filter.GetExpand().Has(types.ExpandFeatures) {
			// Collect feature IDs
			featureIDs := lo.Map(entitlements, func(e *entitlement.Entitlement, _ int) string {
				return e.FeatureID
			})

			if len(featureIDs) > 0 {
				featureFilter := types.NewNoLimitFeatureFilter()
				featureFilter.FeatureIDs = featureIDs
				features, err := s.featureRepo.List(ctx, featureFilter)
				if err != nil {
					return nil, fmt.Errorf("failed to fetch features: %w", err)
				}

				featuresByID = make(map[string]*feature.Feature, len(features))
				for _, f := range features {
					featuresByID[f.ID] = f
				}

				s.log.Debugw("fetched features for entitlements", "count", len(features))
			}
		}

		if filter.GetExpand().Has(types.ExpandPlans) {
			// Collect plan IDs
			planIDs := lo.Map(entitlements, func(e *entitlement.Entitlement, _ int) string {
				return e.PlanID
			})

			if len(planIDs) > 0 {
				planFilter := types.NewNoLimitPlanFilter()
				planFilter.PlanIDs = planIDs
				plans, err := s.planRepo.List(ctx, planFilter)
				if err != nil {
					return nil, fmt.Errorf("failed to fetch plans: %w", err)
				}

				plansByID = make(map[string]*plan.Plan, len(plans))
				for _, p := range plans {
					plansByID[p.ID] = p
				}

				s.log.Debugw("fetched plans for entitlements", "count", len(plans))
			}
		}
	}

	for i, e := range entitlements {
		response.Items[i] = &dto.EntitlementResponse{Entitlement: e}

		// Add expanded feature if requested and available
		if !filter.GetExpand().IsEmpty() && filter.GetExpand().Has(types.ExpandFeatures) {
			if f, ok := featuresByID[e.FeatureID]; ok {
				response.Items[i].Feature = &dto.FeatureResponse{Feature: f}
			}
		}

		// Add expanded plan if requested and available
		if !filter.GetExpand().IsEmpty() && filter.GetExpand().Has(types.ExpandPlans) {
			if p, ok := plansByID[e.PlanID]; ok {
				response.Items[i].Plan = &dto.PlanResponse{Plan: p}
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
	existing, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("entitlement not found: %w", err)
	}

	// Update fields
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
		return nil, fmt.Errorf("invalid entitlement: %w", err)
	}

	result, err := s.repo.Update(ctx, existing)
	if err != nil {
		return nil, fmt.Errorf("failed to update entitlement: %w", err)
	}

	response := &dto.EntitlementResponse{Entitlement: result}
	return response, nil
}

func (s *entitlementService) DeleteEntitlement(ctx context.Context, id string) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("failed to delete entitlement: %w", err)
	}
	return nil
}

func (s *entitlementService) GetPlanEntitlements(ctx context.Context, planID string) (*dto.ListEntitlementsResponse, error) {
	// Create a filter for the plan's entitlements
	filter := types.NewNoLimitEntitlementFilter()
	filter.WithPlanIDs([]string{planID})
	filter.WithStatus(types.StatusPublished)
	filter.WithExpand(string(types.ExpandFeatures))

	// Use the standard list function to get the entitlements with expansion
	return s.ListEntitlements(ctx, filter)
}

func (s *entitlementService) GetPlanFeatureEntitlements(ctx context.Context, planID, featureID string) (*dto.ListEntitlementsResponse, error) {
	// Create a filter for the feature's entitlements
	filter := types.NewNoLimitEntitlementFilter()
	filter.WithPlanIDs([]string{planID})
	filter.WithFeatureID(featureID)
	filter.WithStatus(types.StatusPublished)

	// Use the standard list function to get the entitlements with expansion
	return s.ListEntitlements(ctx, filter)
}
