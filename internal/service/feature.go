package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
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
	repo      feature.Repository
	meterRepo meter.Repository
	logger    *logger.Logger
}

func NewFeatureService(repo feature.Repository, meterRepo meter.Repository, logger *logger.Logger) FeatureService {
	return &featureService{
		repo:      repo,
		meterRepo: meterRepo,
		logger:    logger,
	}
}

func (s *featureService) CreateFeature(ctx context.Context, req dto.CreateFeatureRequest) (*dto.FeatureResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err // Validation errors are already properly formatted in the DTO
	}

	// Validate meter existence and status for metered features
	if req.Type == types.FeatureTypeMetered {
		meter, err := s.meterRepo.GetMeter(ctx, req.MeterID)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHintf("Could not find meter for the feature").
				Mark(ierr.ErrValidation)
		}
		if meter.Status != types.StatusPublished {
			return nil, ierr.NewError("invalid meter status").
				WithHint("The meter must be in active").
				Mark(ierr.ErrValidation)
		}
	}

	featureModel, err := req.ToFeature(ctx)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to process feature data").
			Mark(ierr.ErrValidation)
	}

	if err := s.repo.Create(ctx, featureModel); err != nil {
		return nil, err // Repository errors are already properly formatted
	}

	return &dto.FeatureResponse{Feature: featureModel}, nil
}

func (s *featureService) GetFeature(ctx context.Context, id string) (*dto.FeatureResponse, error) {
	if id == "" {
		return nil, ierr.NewError("feature ID is required").
			WithHint("Feature ID is required").
			Mark(ierr.ErrValidation)
	}

	feature, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err // Repository errors are already properly formatted
	}

	response := &dto.FeatureResponse{Feature: feature}

	// Expand meter if it exists and feature is metered
	if feature.Type == types.FeatureTypeMetered && feature.MeterID != "" {
		meter, err := s.meterRepo.GetMeter(ctx, feature.MeterID)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHintf("Failed to get meter %s for feature %s", feature.MeterID, feature.ID).
				Mark(ierr.ErrSystem)
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

	features, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	featureCount, err := s.repo.Count(ctx, filter)
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
			meters, err := s.meterRepo.List(ctx, meterFilter)
			if err != nil {
				return nil, ierr.WithError(err).
					WithHint("Failed to fetch meters for features").
					Mark(ierr.ErrSystem)
			}

			// Create a map for quick meter lookup
			metersByID = make(map[string]*meter.Meter, len(meters))
			for _, m := range meters {
				metersByID[m.ID] = m
			}

			s.logger.Debugw("fetched meters for features", "count", len(meters))
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

	feature, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err // Repository errors are already properly formatted
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

	// Validate units are set together
	if (feature.UnitSingular == "" && feature.UnitPlural != "") || (feature.UnitPlural == "" && feature.UnitSingular != "") {
		return nil, ierr.NewError("unit_singular and unit_plural must be set together").
			WithHint("Unit singular and unit plural must be set together").
			Mark(ierr.ErrValidation)
	}

	if err := s.repo.Update(ctx, feature); err != nil {
		return nil, err // Repository errors are already properly formatted
	}

	return &dto.FeatureResponse{Feature: feature}, nil
}

func (s *featureService) DeleteFeature(ctx context.Context, id string) error {
	if id == "" {
		return ierr.NewError("feature ID is required").
			WithHint("Feature ID is required").
			Mark(ierr.ErrValidation)
	}

	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	return nil
}
