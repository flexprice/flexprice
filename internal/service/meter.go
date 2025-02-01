package service

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/meter"
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
}

func NewMeterService(meterRepo meter.Repository) MeterService {
	return &meterService{meterRepo: meterRepo}
}

func (s *meterService) CreateMeter(ctx context.Context, req *dto.CreateMeterRequest) (*meter.Meter, error) {
	if req == nil {
		return nil, fmt.Errorf("meter cannot be nil")
	}

	if req.EventName == "" {
		return nil, fmt.Errorf("event_name is required")
	}

	meter := req.ToMeter(types.GetTenantID(ctx), types.GetUserID(ctx))

	if err := meter.Validate(); err != nil {
		return nil, fmt.Errorf("validate meter: %w", err)
	}

	if err := s.meterRepo.CreateMeter(ctx, meter); err != nil {
		return nil, fmt.Errorf("create meter: %w", err)
	}

	return meter, nil
}

func (s *meterService) GetMeter(ctx context.Context, id string) (*meter.Meter, error) {
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}
	return s.meterRepo.GetMeter(ctx, id)
}

func (s *meterService) GetMeters(ctx context.Context, filter *types.MeterFilter) (*dto.ListMetersResponse, error) {
	if filter == nil {
		filter = types.NewMeterFilter()
	}

	if err := filter.Validate(); err != nil {
		return nil, fmt.Errorf("invalid filter: %w", err)
	}

	meters, err := s.meterRepo.List(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to list meters: %w", err)
	}

	count, err := s.meterRepo.Count(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to count meters: %w", err)
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
		return nil, fmt.Errorf("failed to list meters: %w", err)
	}

	count, err := s.meterRepo.Count(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to count meters: %w", err)
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
		return fmt.Errorf("id is required")
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
		return nil, fmt.Errorf("id is required")
	}

	if len(filters) == 0 {
		return nil, fmt.Errorf("filters cannot be empty")
	}

	// Fetch the existing meter
	existingMeter, err := s.meterRepo.GetMeter(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("fetch meter: %w", err)
	}

	// Merge filters
	mergedFilters := mergeFilters(existingMeter.Filters, filters)

	// Update only the filters field in the database
	if err := s.meterRepo.UpdateMeter(ctx, id, mergedFilters); err != nil {
		return nil, fmt.Errorf("update filters: %w", err)
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
