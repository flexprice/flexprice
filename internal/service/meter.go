package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/meter"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

type MeterService interface {
	CreateMeter(ctx context.Context, req *dto.CreateMeterRequest) (*meter.Meter, error)
	GetMeter(ctx context.Context, id string) (*meter.Meter, error)
	GetMeters(ctx context.Context, filter *types.MeterFilter) (*dto.ListMetersResponse, error)
	GetAllMeters(ctx context.Context) (*dto.ListMetersResponse, error)
	DisableMeter(ctx context.Context, id string) error
	UpdateMeter(ctx context.Context, id string, filters []meter.Filter) (*meter.Meter, error)
}

type meterService struct {
	meterRepo meter.Repository
	params    *ServiceParams // Optional for sync functionality
}

func NewMeterService(meterRepo meter.Repository) MeterService {
	return &meterService{meterRepo: meterRepo}
}

func NewMeterServiceWithParams(params ServiceParams) MeterService {
	return &meterService{meterRepo: params.MeterRepo, params: &params}
}

func (s *meterService) CreateMeter(ctx context.Context, req *dto.CreateMeterRequest) (*meter.Meter, error) {
	if req == nil {
		return nil, ierr.NewError("meter cannot be nil").
			WithHint("Meter cannot be nil").
			Mark(ierr.ErrValidation)
	}

	if req.EventName == "" {
		return nil, ierr.NewError("event_name is required").
			WithHint("Event name is required").
			Mark(ierr.ErrValidation)
	}

	meter := req.ToMeter(types.GetTenantID(ctx), types.GetUserID(ctx))
	meter.EnvironmentID = types.GetEnvironmentID(ctx)

	if err := meter.Validate(); err != nil {
		return nil, err
	}

	if err := s.meterRepo.CreateMeter(ctx, meter); err != nil {
		return nil, err
	}

	// Sync meter to external providers asynchronously if params are available
	if s.params != nil {
		// Create a detached context that preserves tenant/environment info but won't be canceled
		syncCtx := context.WithValue(context.Background(), types.CtxTenantID, types.GetTenantID(ctx))
		syncCtx = context.WithValue(syncCtx, types.CtxEnvironmentID, types.GetEnvironmentID(ctx))
		syncCtx = context.WithValue(syncCtx, types.CtxUserID, types.GetUserID(ctx))

		go func(detachedCtx context.Context, meterID string) {
			integrationService := NewIntegrationService(*s.params)

			if err := integrationService.SyncEntityToProviders(detachedCtx, types.IntegrationEntityTypeMeter, meterID); err != nil {
				s.params.Logger.Errorw("failed to sync meter to providers",
					"meter_id", meterID,
					"error", err)
			}
		}(syncCtx, meter.ID)
	}

	return meter, nil
}

func (s *meterService) GetMeter(ctx context.Context, id string) (*meter.Meter, error) {
	if id == "" {
		return nil, ierr.NewError("id is required").
			WithHint("Id is required").
			Mark(ierr.ErrValidation)
	}
	return s.meterRepo.GetMeter(ctx, id)
}

func (s *meterService) GetMeters(ctx context.Context, filter *types.MeterFilter) (*dto.ListMetersResponse, error) {
	if filter == nil {
		filter = types.NewMeterFilter()
	}

	if err := filter.Validate(); err != nil {
		return nil, err
	}

	meters, err := s.meterRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	count, err := s.meterRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	response := &dto.ListMetersResponse{
		Items:      make([]*dto.MeterResponse, len(meters)),
		Pagination: types.NewPaginationResponse(count, filter.GetLimit(), filter.GetOffset()),
	}

	for i, meter := range meters {
		response.Items[i] = dto.ToMeterResponse(meter)
	}

	return response, nil
}

func (s *meterService) GetAllMeters(ctx context.Context) (*dto.ListMetersResponse, error) {
	filter := types.NewNoLimitMeterFilter()
	meters, err := s.meterRepo.ListAll(ctx, filter)
	if err != nil {
		return nil, err
	}

	count, err := s.meterRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	response := &dto.ListMetersResponse{
		Items:      make([]*dto.MeterResponse, len(meters)),
		Pagination: types.NewPaginationResponse(count, filter.GetLimit(), filter.GetOffset()),
	}

	for i, meter := range meters {
		response.Items[i] = dto.ToMeterResponse(meter)
	}
	return response, nil
}

func (s *meterService) DisableMeter(ctx context.Context, id string) error {
	if id == "" {
		return ierr.NewError("id is required").
			WithHint("Id is required").
			Mark(ierr.ErrValidation)
	}
	return s.meterRepo.DisableMeter(ctx, id)
}

// contains checks if a slice contains a specific value
func contains(slice []string, value string) bool {
	for _, v := range slice {
		if v == value {
			return true
		}
	}
	return false
}

func (s *meterService) UpdateMeter(ctx context.Context, id string, filters []meter.Filter) (*meter.Meter, error) {
	// Validate input
	if id == "" {
		return nil, ierr.NewError("id is required").
			WithHint("Id is required").
			Mark(ierr.ErrValidation)
	}

	if len(filters) == 0 {
		return nil, ierr.NewError("filters cannot be empty").
			WithHint("Filters cannot be empty").
			Mark(ierr.ErrValidation)
	}

	// Fetch the existing meter
	existingMeter, err := s.meterRepo.GetMeter(ctx, id)
	if err != nil {
		return nil, err
	}

	// Merge filters
	mergedFilters := mergeFilters(existingMeter.Filters, filters)

	// Update only the filters field in the database
	if err := s.meterRepo.UpdateMeter(ctx, id, mergedFilters); err != nil {
		return nil, err
	}

	// Return the updated meter object
	existingMeter.Filters = mergedFilters
	return existingMeter, nil
}

// mergeFilters combines existing filters with new filters, ensuring no duplicates
func mergeFilters(existingFilters, newFilters []meter.Filter) []meter.Filter {
	filterMap := make(map[string][]string)

	// Add existing filters to the map
	for _, f := range existingFilters {
		filterMap[f.Key] = f.Values
	}

	// Merge new filters into the map
	for _, newFilter := range newFilters {
		if _, exists := filterMap[newFilter.Key]; !exists {
			filterMap[newFilter.Key] = []string{}
		}
		for _, value := range newFilter.Values {
			if !contains(filterMap[newFilter.Key], value) {
				filterMap[newFilter.Key] = append(filterMap[newFilter.Key], value)
			}
		}
	}

	// Convert the map back to a slice of filters
	mergedFilters := make([]meter.Filter, 0, len(filterMap))
	for key, values := range filterMap {
		mergedFilters = append(mergedFilters, meter.Filter{
			Key:    key,
			Values: values,
		})
	}

	return mergedFilters
}
