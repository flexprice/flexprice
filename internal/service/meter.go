package service

import (
	"context"
	"time"

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
}

func NewMeterService(meterRepo meter.Repository) MeterService {
	return &meterService{meterRepo: meterRepo}
}

func (s *meterService) CreateMeter(ctx context.Context, req *dto.CreateMeterRequest) (*meter.Meter, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Create meter from request
	meter := &meter.Meter{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_METER),
		Name:          req.Name,
		EventName:     req.EventName,
		Aggregation:   req.Aggregation,
		Filters:       req.Filters,
		ResetUsage:    req.ResetUsage,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel: types.BaseModel{
			TenantID:  types.GetTenantID(ctx),
			Status:    types.StatusPublished,
			CreatedBy: types.GetUserID(ctx),
			UpdatedBy: types.GetUserID(ctx),
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		},
	}

	if err := s.meterRepo.CreateMeter(ctx, meter); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to create meter").
			WithReportableDetails(map[string]interface{}{
				"meter_name": req.Name,
				"event_name": req.EventName,
			}).
			Mark(ierr.ErrDatabase)
	}

	return meter, nil
}

func (s *meterService) GetMeter(ctx context.Context, id string) (*meter.Meter, error) {
	if id == "" {
		return nil, ierr.NewError("meter ID is required").
			WithHint("Please provide a valid meter ID").
			Mark(ierr.ErrValidation)
	}

	meter, err := s.meterRepo.GetMeter(ctx, id)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve meter").
			WithReportableDetails(map[string]interface{}{
				"meter_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	return meter, nil
}

func (s *meterService) GetMeters(ctx context.Context, filter *types.MeterFilter) (*dto.ListMetersResponse, error) {
	if filter == nil {
		filter = types.NewMeterFilter()
	}

	if err := filter.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation)
	}

	meters, err := s.meterRepo.List(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve meters").
			Mark(ierr.ErrDatabase)
	}

	count, err := s.meterRepo.Count(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to count meters").
			Mark(ierr.ErrDatabase)
	}

	response := &dto.ListMetersResponse{
		Items: make([]*dto.MeterResponse, len(meters)),
		Pagination: types.PaginationResponse{
			Total:  count,
			Limit:  filter.GetLimit(),
			Offset: filter.GetOffset(),
		},
	}

	for i, m := range meters {
		response.Items[i] = &dto.MeterResponse{
			ID:            m.ID,
			Name:          m.Name,
			EventName:     m.EventName,
			Aggregation:   m.Aggregation,
			Filters:       m.Filters,
			ResetUsage:    m.ResetUsage,
			EnvironmentID: m.EnvironmentID,
			TenantID:      m.TenantID,
			Status:        m.Status,
			CreatedAt:     m.CreatedAt,
			UpdatedAt:     m.UpdatedAt,
		}
	}

	return response, nil
}

func (s *meterService) GetAllMeters(ctx context.Context) (*dto.ListMetersResponse, error) {
	filter := types.NewNoLimitMeterFilter()

	meters, err := s.meterRepo.ListAll(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve all meters").
			Mark(ierr.ErrDatabase)
	}

	response := &dto.ListMetersResponse{
		Items: make([]*dto.MeterResponse, len(meters)),
		Pagination: types.PaginationResponse{
			Total:  len(meters),
			Limit:  len(meters),
			Offset: 0,
		},
	}

	for i, m := range meters {
		response.Items[i] = &dto.MeterResponse{
			ID:            m.ID,
			Name:          m.Name,
			EventName:     m.EventName,
			Aggregation:   m.Aggregation,
			Filters:       m.Filters,
			ResetUsage:    m.ResetUsage,
			EnvironmentID: m.EnvironmentID,
			TenantID:      m.TenantID,
			Status:        m.Status,
			CreatedAt:     m.CreatedAt,
			UpdatedAt:     m.UpdatedAt,
		}
	}

	return response, nil
}

func (s *meterService) DisableMeter(ctx context.Context, id string) error {
	if id == "" {
		return ierr.NewError("meter ID is required").
			WithHint("Please provide a valid meter ID").
			Mark(ierr.ErrValidation)
	}

	// Check if meter exists
	_, err := s.meterRepo.GetMeter(ctx, id)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to retrieve meter").
			WithReportableDetails(map[string]interface{}{
				"meter_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Disable meter
	if err := s.meterRepo.DisableMeter(ctx, id); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to disable meter").
			WithReportableDetails(map[string]interface{}{
				"meter_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	return nil
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
			WithHint("Meter ID is required").
			Mark(ierr.ErrValidation)
	}

	if len(filters) == 0 {
		return nil, ierr.NewError("filters cannot be empty").
			WithHint("At least one filter must be provided").
			Mark(ierr.ErrValidation)
	}

	// Fetch the existing meter
	existingMeter, err := s.meterRepo.GetMeter(ctx, id)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve meter").
			WithReportableDetails(map[string]interface{}{
				"meter_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Merge filters
	mergedFilters := mergeFilters(existingMeter.Filters, filters)

	// Update only the filters field in the database
	if err := s.meterRepo.UpdateMeter(ctx, id, mergedFilters); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to update meter filters").
			WithReportableDetails(map[string]interface{}{
				"meter_id": id,
			}).
			Mark(ierr.ErrDatabase)
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
