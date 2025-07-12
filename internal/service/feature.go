package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/meter"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/samber/lo"
)

type FeatureService interface {
	CreateFeature(ctx context.Context, req dto.CreateFeatureRequest) (*dto.FeatureResponse, error)
	GetFeature(ctx context.Context, id string) (*dto.FeatureResponse, error)
	GetFeatures(ctx context.Context, filter *types.FeatureFilter) (*dto.ListFeaturesResponse, error)
	UpdateFeature(ctx context.Context, id string, req dto.UpdateFeatureRequest) (*dto.FeatureResponse, error)
	DeleteFeature(ctx context.Context, id string) error
}

type featureService struct {
	ServiceParams
}

func NewFeatureService(params ServiceParams) FeatureService {
	return &featureService{
		ServiceParams: params,
	}
}

func (s *featureService) CreateFeature(ctx context.Context, req dto.CreateFeatureRequest) (*dto.FeatureResponse, error) {
	meterService := NewMeterService(s.MeterRepo)
	err := req.Validate()
	if err != nil {
		return nil, err // Validation errors are already properly formatted in the DTO
	}

	// Validate meter existence and status for metered features
	if req.Type == types.FeatureTypeMetered {
		var meter *meter.Meter
		if req.MeterID != "" {
			meter, err = meterService.GetMeter(ctx, req.MeterID)
			if err != nil {
				return nil, err
			}
		} else {
			meter, err = meterService.CreateMeter(ctx, req.Meter)
			if err != nil {
				return nil, err
			}
			req.MeterID = meter.ID
		}

		// Validate meter status
		if meter.Status != types.StatusPublished {
			return nil, ierr.NewError("invalid meter status").
				WithHint("The metered feature must be associated with an active meter").
				Mark(ierr.ErrValidation)
		}
	}

	featureModel, err := req.ToFeature(ctx)
	if err != nil {
		return nil, err
	}

	if err := s.FeatureRepo.Create(ctx, featureModel); err != nil {
		return nil, err
	}

	// Publish webhook event
	s.publishWebhookEvent(ctx, types.WebhookEventFeatureCreated, featureModel.ID)

	return &dto.FeatureResponse{Feature: featureModel}, nil
}

func (s *featureService) GetFeature(ctx context.Context, id string) (*dto.FeatureResponse, error) {
	feature, err := s.FeatureRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	response := &dto.FeatureResponse{Feature: feature}

	// Expand meter if it exists and feature is metered
	if feature.Type == types.FeatureTypeMetered && feature.MeterID != "" {
		meter, err := s.MeterRepo.GetMeter(ctx, feature.MeterID)
		if err != nil {
			return nil, err
		}
		response.Meter = dto.ToMeterResponse(meter)
	}

	return response, nil
}

func (s *featureService) GetFeatures(ctx context.Context, filter *types.FeatureFilter) (*dto.ListFeaturesResponse, error) {
	if filter == nil {
		filter = types.NewDefaultFeatureFilter()
	}

	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewDefaultQueryFilter()
	}

	// Set default sort order if not specified
	if filter.QueryFilter.Sort == nil {
		filter.QueryFilter.Sort = lo.ToPtr("created_at")
		filter.QueryFilter.Order = lo.ToPtr("desc")
	}

	// validate filters
	if err := filter.Validate(); err != nil {
		return nil, err
	}

	features, err := s.FeatureRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	featureCount, err := s.FeatureRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	response := &dto.ListFeaturesResponse{
		Items: make([]*dto.FeatureResponse, len(features)),
	}

	// Create a map to store meters by ID for expansion
	var metersByID map[string]*meter.Meter
	if !filter.GetExpand().IsEmpty() && filter.GetExpand().Has(types.ExpandMeters) {
		// Collect meter IDs from metered features
		meterIDs := make([]string, 0)
		for _, f := range features {
			if f.Type == types.FeatureTypeMetered && f.MeterID != "" {
				meterIDs = append(meterIDs, f.MeterID)
			}
		}

		if len(meterIDs) > 0 {
			// Create a filter to fetch all meters
			meterFilter := types.NewNoLimitMeterFilter()
			meterFilter.MeterIDs = meterIDs
			meters, err := s.MeterRepo.List(ctx, meterFilter)
			if err != nil {
				return nil, err
			}

			// Create a map for quick meter lookup
			metersByID = make(map[string]*meter.Meter, len(meters))
			for _, m := range meters {
				metersByID[m.ID] = m
			}

			s.Logger.Debugw("fetched meters for features", "count", len(meters))
		}
	}

	for i, f := range features {
		response.Items[i] = &dto.FeatureResponse{Feature: f}

		// Add meter if requested and available
		if !filter.GetExpand().IsEmpty() && filter.GetExpand().Has(types.ExpandMeters) && f.Type == types.FeatureTypeMetered && f.MeterID != "" {
			if m, ok := metersByID[f.MeterID]; ok {
				response.Items[i].Meter = dto.ToMeterResponse(m)
			}
		}
	}

	response.Pagination = types.NewPaginationResponse(
		featureCount,
		filter.GetLimit(),
		filter.GetOffset(),
	)

	return response, nil
}

func (s *featureService) UpdateFeature(ctx context.Context, id string, req dto.UpdateFeatureRequest) (*dto.FeatureResponse, error) {
	if id == "" {
		return nil, ierr.NewError("feature ID is required").
			WithHint("Feature ID is required").
			Mark(ierr.ErrValidation)
	}

	feature, err := s.FeatureRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if req.Description != nil {
		feature.Description = *req.Description
	}
	if req.Metadata != nil {
		feature.Metadata = *req.Metadata
	}
	if req.Name != nil {
		feature.Name = *req.Name
	}

	if req.UnitSingular != nil {
		feature.UnitSingular = *req.UnitSingular
	}
	if req.UnitPlural != nil {
		feature.UnitPlural = *req.UnitPlural
	}

	if feature.Type == types.FeatureTypeMetered && feature.MeterID != "" {
		// update meter filters if provided
		meterService := NewMeterService(s.MeterRepo)
		if req.Filters != nil {
			if _, err := meterService.UpdateMeter(ctx, feature.MeterID, *req.Filters); err != nil {
				return nil, err
			}
		}
	}

	// Validate units are set together
	if (feature.UnitSingular == "" && feature.UnitPlural != "") || (feature.UnitPlural == "" && feature.UnitSingular != "") {
		return nil, ierr.NewError("unit_singular and unit_plural must be set together").
			WithHint("Unit singular and unit plural must be set together").
			Mark(ierr.ErrValidation)
	}

	if err := s.FeatureRepo.Update(ctx, feature); err != nil {
		return nil, err
	}

	// Publish webhook event
	s.publishWebhookEvent(ctx, types.WebhookEventFeatureUpdated, feature.ID)

	return &dto.FeatureResponse{Feature: feature}, nil
}

func (s *featureService) DeleteFeature(ctx context.Context, id string) error {
	if id == "" {
		return ierr.NewError("feature ID is required").
			WithHint("Feature ID is required").
			Mark(ierr.ErrValidation)
	}

	feature, err := s.FeatureRepo.Get(ctx, id)
	if err != nil {
		return ierr.NewError(fmt.Sprintf("Feature with ID %s was not found", id)).
			WithHint("The specified feature does not exist").
			Mark(ierr.ErrNotFound)
	}

	entitlementFilter := types.NewDefaultEntitlementFilter()
	entitlementFilter.QueryFilter.Limit = lo.ToPtr(1)
	entitlementFilter.QueryFilter.Status = lo.ToPtr(types.StatusPublished)
	entitlementFilter.FeatureIDs = []string{id}
	entitlements, err := s.EntitlementRepo.List(ctx, entitlementFilter)

	if err != nil {
		return err
	}
	if len(entitlements) > 0 {
		return ierr.NewError("feature is linked to some plans").
			WithHint("Feature is linked to some plans, please remove the feature from the plans first").
			Mark(ierr.ErrInvalidOperation)
	}

	if feature.Type == types.FeatureTypeMetered {
		if err := s.MeterRepo.DisableMeter(ctx, feature.MeterID); err != nil {
			return err
		}
	}

	if err := s.FeatureRepo.Delete(ctx, id); err != nil {
		return err
	}

	// Publish webhook event
	s.publishWebhookEvent(ctx, types.WebhookEventFeatureDeleted, id)

	return nil
}

func (s *featureService) publishWebhookEvent(ctx context.Context, eventName string, featureID string) {
	webhookPayload, err := json.Marshal(webhookDto.InternalFeatureEvent{
		FeatureID: featureID,
		TenantID:  types.GetTenantID(ctx),
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
