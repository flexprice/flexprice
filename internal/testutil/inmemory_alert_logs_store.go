package testutil

import (
	"context"

	"github.com/flexprice/flexprice/ent"
	domainAlertLogs "github.com/flexprice/flexprice/internal/domain/alertlogs"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// InMemoryAlertLogsStore implements an in-memory alert logs repository for testing
type InMemoryAlertLogsStore struct {
	*InMemoryStore[*domainAlertLogs.AlertLog]
}

// NewInMemoryAlertLogsStore creates a new in-memory alert logs store
func NewInMemoryAlertLogsStore() *InMemoryAlertLogsStore {
	return &InMemoryAlertLogsStore{
		InMemoryStore: NewInMemoryStore[*domainAlertLogs.AlertLog](),
	}
}

// Create creates a new alert log
func (s *InMemoryAlertLogsStore) Create(ctx context.Context, alertLog *domainAlertLogs.AlertLog) error {
	if alertLog == nil {
		return ierr.NewError("alert log cannot be nil").
			WithHint("Alert log data is required").
			Mark(ierr.ErrValidation)
	}

	// Set environment ID from context if not already set
	if alertLog.EnvironmentID == "" {
		alertLog.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	// Set tenant ID from context if not already set
	if alertLog.TenantID == "" {
		alertLog.TenantID = types.GetTenantID(ctx)
	}

	return s.InMemoryStore.Create(ctx, alertLog.ID, alertLog)
}

// Get retrieves an alert log by ID
func (s *InMemoryAlertLogsStore) Get(ctx context.Context, id string) (*domainAlertLogs.AlertLog, error) {
	return s.InMemoryStore.Get(ctx, id)
}

// List retrieves alert logs with filtering and pagination
func (s *InMemoryAlertLogsStore) List(ctx context.Context, filter *types.AlertLogFilter) ([]*domainAlertLogs.AlertLog, error) {
	return s.InMemoryStore.List(ctx, filter, alertLogFilterFn, alertLogSortFn)
}

// Count returns the number of alert logs matching the filter
func (s *InMemoryAlertLogsStore) Count(ctx context.Context, filter *types.AlertLogFilter) (int, error) {
	return s.InMemoryStore.Count(ctx, filter, alertLogFilterFn)
}

// GetLatestByEntity retrieves the latest alert log for a specific entity
func (s *InMemoryAlertLogsStore) GetLatestByEntity(ctx context.Context, entityType types.AlertEntityType, entityID string) (*domainAlertLogs.AlertLog, error) {
	filter := &types.AlertLogFilter{
		QueryFilter: types.NewNoLimitAlertLogFilter().QueryFilter,
		EntityType:  entityType,
		EntityID:    entityID,
	}

	alertLogs, err := s.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	if len(alertLogs) == 0 {
		return nil, &ent.NotFoundError{}
	}

	// Return the first one (newest due to sorting)
	return alertLogs[0], nil
}

// GetLatestByEntityAndAlertType retrieves the latest alert log for a specific entity and alert type
func (s *InMemoryAlertLogsStore) GetLatestByEntityAndAlertType(ctx context.Context, entityType types.AlertEntityType, entityID string, alertType types.AlertType) (*domainAlertLogs.AlertLog, error) {
	filter := &types.AlertLogFilter{
		QueryFilter: types.NewNoLimitAlertLogFilter().QueryFilter,
		EntityType:  entityType,
		EntityID:    entityID,
		AlertType:   alertType,
	}

	alertLogs, err := s.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	if len(alertLogs) == 0 {
		return nil, &ent.NotFoundError{}
	}

	// Return the first one (newest due to sorting)
	return alertLogs[0], nil
}

// ListByEntity retrieves alert logs for a specific entity with limit
func (s *InMemoryAlertLogsStore) ListByEntity(ctx context.Context, entityType types.AlertEntityType, entityID string, limit int) ([]*domainAlertLogs.AlertLog, error) {
	filter := &types.AlertLogFilter{
		QueryFilter: &types.QueryFilter{Limit: &limit},
		EntityType:  entityType,
		EntityID:    entityID,
	}

	return s.List(ctx, filter)
}

// ListByAlertType retrieves alert logs for a specific alert type with limit
func (s *InMemoryAlertLogsStore) ListByAlertType(ctx context.Context, alertType types.AlertType, limit int) ([]*domainAlertLogs.AlertLog, error) {
	filter := &types.AlertLogFilter{
		QueryFilter: &types.QueryFilter{Limit: &limit},
		AlertType:   alertType,
	}

	return s.List(ctx, filter)
}

// alertLogFilterFn implements filtering logic for alert logs
func alertLogFilterFn(ctx context.Context, alertLog *domainAlertLogs.AlertLog, filter interface{}) bool {
	if alertLog == nil {
		return false
	}

	f, ok := filter.(*types.AlertLogFilter)
	if !ok {
		return true // No filter applied
	}

	// Check tenant ID from context
	if tenantID, ok := ctx.Value(types.CtxTenantID).(string); ok {
		if alertLog.TenantID != tenantID {
			return false
		}
	}

	// Apply environment filter
	if !CheckEnvironmentFilter(ctx, alertLog.EnvironmentID) {
		return false
	}

	// Filter by entity type
	if f.EntityType != "" && alertLog.EntityType != f.EntityType {
		return false
	}

	// Filter by entity ID
	if f.EntityID != "" && alertLog.EntityID != f.EntityID {
		return false
	}

	// Filter by alert type
	if f.AlertType != "" && alertLog.AlertType != f.AlertType {
		return false
	}

	// Filter by alert status
	if f.AlertStatus != "" && alertLog.AlertStatus != f.AlertStatus {
		return false
	}

	// Filter by time range
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil && alertLog.CreatedAt.Before(*f.StartTime) {
			return false
		}
		if f.EndTime != nil && alertLog.CreatedAt.After(*f.EndTime) {
			return false
		}
	}

	return true
}

// alertLogSortFn implements sorting logic for alert logs (newest first)
func alertLogSortFn(i, j *domainAlertLogs.AlertLog) bool {
	if i == nil || j == nil {
		return false
	}
	return i.CreatedAt.After(j.CreatedAt)
}
