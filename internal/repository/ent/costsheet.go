// Package ent provides the implementation of the costsheet repository using Ent ORM.
// This package handles all database operations related to costsheets, including CRUD operations
// and specialized queries.
package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/costsheet"
	"github.com/flexprice/flexprice/internal/cache"
	domainCostsheet "github.com/flexprice/flexprice/internal/domain/costsheet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

// costsheetRepository implements the costsheet.Repository interface using Ent ORM.
// It provides methods for managing costsheet records in the database.
type costsheetRepository struct {
	client postgres.IClient // Database client for executing queries
	logger *logger.Logger   // Logger for tracking operations
	cache  cache.Cache      // Cache for performance optimization
}

// NewCostsheetRepository creates a new instance of the Ent-based costsheet repository.
// It initializes the repository with the provided database client, logger, and cache.
func NewCostsheetRepository(client postgres.IClient, logger *logger.Logger, cache cache.Cache) domainCostsheet.Repository {
	return &costsheetRepository{
		client: client,
		logger: logger,
		cache:  cache,
	}
}

// Create stores a new costsheet record in the database.
func (r *costsheetRepository) Create(ctx context.Context, costsheet *domainCostsheet.Costsheet) error {
	entCostsheet := r.domainToEnt(costsheet)

	createQuery := r.client.Writer(ctx).Costsheet.Create().
		SetID(entCostsheet.ID).
		SetTenantID(entCostsheet.TenantID).
		SetEnvironmentID(entCostsheet.EnvironmentID).
		SetStatus(entCostsheet.Status).
		SetCreatedAt(entCostsheet.CreatedAt).
		SetUpdatedAt(entCostsheet.UpdatedAt).
		SetNillableCreatedBy(&entCostsheet.CreatedBy).
		SetNillableUpdatedBy(&entCostsheet.UpdatedBy).
		SetName(entCostsheet.Name)

	// Set optional fields
	if entCostsheet.LookupKey != "" {
		createQuery.SetLookupKey(entCostsheet.LookupKey)
	}
	if entCostsheet.Description != "" {
		createQuery.SetDescription(entCostsheet.Description)
	}
	if entCostsheet.Metadata != nil {
		createQuery.SetMetadata(entCostsheet.Metadata)
	}

	err := createQuery.Exec(ctx)

	if err != nil {
		r.logger.Errorw("Failed to create costsheet", "error", err, "costsheet_id", costsheet.ID)
		return ierr.WithError(err).
			WithHint("Failed to create costsheet").
			Mark(ierr.ErrDatabase)
	}

	r.logger.Infow("Created costsheet", "costsheet_id", costsheet.ID, "name", costsheet.Name)
	return nil
}

// GetByID retrieves a costsheet record by its ID.
func (r *costsheetRepository) GetByID(ctx context.Context, id string) (*domainCostsheet.Costsheet, error) {
	entCostsheet, err := r.client.Reader(ctx).Costsheet.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.NewError("costsheet not found").
				WithHint("No costsheet found with the provided ID").
				WithReportableDetails(map[string]any{"id": id}).
				Mark(ierr.ErrNotFound)
		}
		r.logger.Errorw("Failed to get costsheet by ID", "error", err, "costsheet_id", id)
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve costsheet").
			Mark(ierr.ErrDatabase)
	}

	return r.entToDomain(entCostsheet), nil
}

// GetByTenantAndEnvironment retrieves costsheet records for a specific tenant and environment.
func (r *costsheetRepository) GetByTenantAndEnvironment(ctx context.Context, tenantID, environmentID string) ([]*domainCostsheet.Costsheet, error) {
	entCostsheets, err := r.client.Reader(ctx).Costsheet.Query().
		Where(
			costsheet.TenantID(tenantID),
			costsheet.EnvironmentID(environmentID),
			costsheet.Status(string(types.StatusPublished)),
		).
		Order(ent.Asc(costsheet.FieldCreatedAt)).
		All(ctx)

	if err != nil {
		r.logger.Errorw("Failed to get costsheet records by tenant and environment",
			"error", err, "tenant_id", tenantID, "environment_id", environmentID)
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve costsheet records").
			Mark(ierr.ErrDatabase)
	}

	costsheets := make([]*domainCostsheet.Costsheet, len(entCostsheets))
	for i, entCostsheet := range entCostsheets {
		costsheets[i] = r.entToDomain(entCostsheet)
	}

	return costsheets, nil
}

// List retrieves costsheet records based on the provided filter.
func (r *costsheetRepository) List(ctx context.Context, filter *domainCostsheet.Filter) ([]*domainCostsheet.Costsheet, error) {
	query := r.client.Reader(ctx).Costsheet.Query()

	// Apply tenant and environment from context
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	if tenantID != "" {
		query = query.Where(costsheet.TenantID(tenantID))
	}
	if environmentID != "" {
		query = query.Where(costsheet.EnvironmentID(environmentID))
	}

	// Apply status filter if provided
	if filter.GetStatus() != "" {
		query = query.Where(costsheet.Status(filter.GetStatus()))
	}

	// Apply costsheet IDs filter
	if len(filter.CostsheetIDs) > 0 {
		query = query.Where(costsheet.IDIn(filter.CostsheetIDs...))
	}

	// Apply name filter
	if filter.Name != "" {
		query = query.Where(costsheet.Name(filter.Name))
	}

	// Apply lookup key filter
	if filter.LookupKey != "" {
		query = query.Where(costsheet.LookupKey(filter.LookupKey))
	}

	// Apply time range filter
	if filter.TimeRangeFilter != nil {
		if filter.TimeRangeFilter.StartTime != nil {
			query = query.Where(costsheet.CreatedAtGTE(*filter.TimeRangeFilter.StartTime))
		}
		if filter.TimeRangeFilter.EndTime != nil {
			query = query.Where(costsheet.CreatedAtLTE(*filter.TimeRangeFilter.EndTime))
		}
	}

	// Apply pagination
	if !filter.IsUnlimited() {
		query = query.Limit(filter.GetLimit()).Offset(filter.GetOffset())
	}

	// Apply sorting
	order := ent.Asc(costsheet.FieldCreatedAt)
	if filter.GetSort() != "" {
		switch filter.GetSort() {
		case "name":
			if filter.GetOrder() == "desc" {
				order = ent.Desc(costsheet.FieldName)
			} else {
				order = ent.Asc(costsheet.FieldName)
			}
		case "created_at":
			if filter.GetOrder() == "desc" {
				order = ent.Desc(costsheet.FieldCreatedAt)
			} else {
				order = ent.Asc(costsheet.FieldCreatedAt)
			}
		case "updated_at":
			if filter.GetOrder() == "desc" {
				order = ent.Desc(costsheet.FieldUpdatedAt)
			} else {
				order = ent.Asc(costsheet.FieldUpdatedAt)
			}
		}
	}
	query = query.Order(order)

	entCostsheets, err := query.All(ctx)
	if err != nil {
		r.logger.Errorw("Failed to list costsheet records", "error", err)
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve costsheet records").
			Mark(ierr.ErrDatabase)
	}

	costsheets := make([]*domainCostsheet.Costsheet, len(entCostsheets))
	for i, entCostsheet := range entCostsheets {
		costsheets[i] = r.entToDomain(entCostsheet)
	}

	return costsheets, nil
}

// Count returns the total number of costsheet records matching the filter.
func (r *costsheetRepository) Count(ctx context.Context, filter *domainCostsheet.Filter) (int64, error) {
	query := r.client.Reader(ctx).Costsheet.Query()

	// Apply tenant and environment from context
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	if tenantID != "" {
		query = query.Where(costsheet.TenantID(tenantID))
	}
	if environmentID != "" {
		query = query.Where(costsheet.EnvironmentID(environmentID))
	}

	// Apply status filter if provided
	if filter.GetStatus() != "" {
		query = query.Where(costsheet.Status(filter.GetStatus()))
	}

	// Apply costsheet IDs filter
	if len(filter.CostsheetIDs) > 0 {
		query = query.Where(costsheet.IDIn(filter.CostsheetIDs...))
	}

	// Apply name filter
	if filter.Name != "" {
		query = query.Where(costsheet.Name(filter.Name))
	}

	// Apply lookup key filter
	if filter.LookupKey != "" {
		query = query.Where(costsheet.LookupKey(filter.LookupKey))
	}

	// Apply time range filter
	if filter.TimeRangeFilter != nil {
		if filter.TimeRangeFilter.StartTime != nil {
			query = query.Where(costsheet.CreatedAtGTE(*filter.TimeRangeFilter.StartTime))
		}
		if filter.TimeRangeFilter.EndTime != nil {
			query = query.Where(costsheet.CreatedAtLTE(*filter.TimeRangeFilter.EndTime))
		}
	}

	count, err := query.Count(ctx)
	if err != nil {
		r.logger.Errorw("Failed to count costsheet records", "error", err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count costsheet records").
			Mark(ierr.ErrDatabase)
	}

	return int64(count), nil
}

// Update updates an existing costsheet record.
func (r *costsheetRepository) Update(ctx context.Context, costsheet *domainCostsheet.Costsheet) error {
	entCostsheet := r.domainToEnt(costsheet)

	updateQuery := r.client.Writer(ctx).Costsheet.UpdateOneID(entCostsheet.ID).
		SetName(entCostsheet.Name).
		SetStatus(entCostsheet.Status).
		SetUpdatedAt(entCostsheet.UpdatedAt).
		SetNillableUpdatedBy(&entCostsheet.UpdatedBy)

	// Update optional fields
	if entCostsheet.LookupKey != "" {
		updateQuery.SetLookupKey(entCostsheet.LookupKey)
	}
	if entCostsheet.Description != "" {
		updateQuery.SetDescription(entCostsheet.Description)
	}
	if entCostsheet.Metadata != nil {
		updateQuery.SetMetadata(entCostsheet.Metadata)
	}

	err := updateQuery.Exec(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return ierr.NewError("costsheet not found").
				WithHint("No costsheet found with the provided ID").
				WithReportableDetails(map[string]any{"id": costsheet.ID}).
				Mark(ierr.ErrNotFound)
		}
		r.logger.Errorw("Failed to update costsheet", "error", err, "costsheet_id", costsheet.ID)
		return ierr.WithError(err).
			WithHint("Failed to update costsheet").
			Mark(ierr.ErrDatabase)
	}

	r.logger.Infow("Updated costsheet", "costsheet_id", costsheet.ID, "name", costsheet.Name)
	return nil
}

// Delete soft deletes a costsheet record by setting its status to deleted.
func (r *costsheetRepository) Delete(ctx context.Context, id string) error {
	err := r.client.Writer(ctx).Costsheet.UpdateOneID(id).
		SetStatus(string(types.StatusDeleted)).
		SetUpdatedAt(time.Now().UTC()).
		Exec(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return ierr.NewError("costsheet not found").
				WithHint("No costsheet found with the provided ID").
				WithReportableDetails(map[string]any{"id": id}).
				Mark(ierr.ErrNotFound)
		}
		r.logger.Errorw("Failed to delete costsheet", "error", err, "costsheet_id", id)
		return ierr.WithError(err).
			WithHint("Failed to delete costsheet").
			Mark(ierr.ErrDatabase)
	}

	r.logger.Infow("Deleted costsheet", "costsheet_id", id)
	return nil
}

// GetByName retrieves a costsheet record by name within a tenant and environment.
func (r *costsheetRepository) GetByName(ctx context.Context, tenantID, environmentID, name string) (*domainCostsheet.Costsheet, error) {
	entCostsheet, err := r.client.Reader(ctx).Costsheet.Query().
		Where(
			costsheet.TenantID(tenantID),
			costsheet.EnvironmentID(environmentID),
			costsheet.Name(name),
			costsheet.Status(string(types.StatusPublished)),
		).
		First(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.NewError("costsheet not found").
				WithHint("No costsheet found with the provided name").
				WithReportableDetails(map[string]any{
					"name":           name,
					"tenant_id":      tenantID,
					"environment_id": environmentID,
				}).
				Mark(ierr.ErrNotFound)
		}
		r.logger.Errorw("Failed to get costsheet by name",
			"error", err, "name", name, "tenant_id", tenantID, "environment_id", environmentID)
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve costsheet by name").
			Mark(ierr.ErrDatabase)
	}

	return r.entToDomain(entCostsheet), nil
}

// GetByLookupKey retrieves a costsheet record by lookup key within a tenant and environment.
func (r *costsheetRepository) GetByLookupKey(ctx context.Context, tenantID, environmentID, lookupKey string) (*domainCostsheet.Costsheet, error) {
	entCostsheet, err := r.client.Reader(ctx).Costsheet.Query().
		Where(
			costsheet.TenantID(tenantID),
			costsheet.EnvironmentID(environmentID),
			costsheet.LookupKey(lookupKey),
			costsheet.Status(string(types.StatusPublished)),
		).
		First(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.NewError("costsheet not found").
				WithHint("No costsheet found with the provided lookup key").
				WithReportableDetails(map[string]any{
					"lookup_key":     lookupKey,
					"tenant_id":      tenantID,
					"environment_id": environmentID,
				}).
				Mark(ierr.ErrNotFound)
		}
		r.logger.Errorw("Failed to get costsheet by lookup key",
			"error", err, "lookup_key", lookupKey, "tenant_id", tenantID, "environment_id", environmentID)
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve costsheet by lookup key").
			Mark(ierr.ErrDatabase)
	}

	return r.entToDomain(entCostsheet), nil
}

// ListByStatus retrieves costsheet records filtered by status.
func (r *costsheetRepository) ListByStatus(ctx context.Context, tenantID, environmentID string, status types.Status) ([]*domainCostsheet.Costsheet, error) {
	entCostsheets, err := r.client.Reader(ctx).Costsheet.Query().
		Where(
			costsheet.TenantID(tenantID),
			costsheet.EnvironmentID(environmentID),
			costsheet.Status(string(status)),
		).
		Order(ent.Asc(costsheet.FieldCreatedAt)).
		All(ctx)

	if err != nil {
		r.logger.Errorw("Failed to get costsheet records by status",
			"error", err, "tenant_id", tenantID, "environment_id", environmentID, "status", status)
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve costsheet records by status").
			Mark(ierr.ErrDatabase)
	}

	costsheets := make([]*domainCostsheet.Costsheet, len(entCostsheets))
	for i, entCostsheet := range entCostsheets {
		costsheets[i] = r.entToDomain(entCostsheet)
	}

	return costsheets, nil
}

// domainToEnt converts a domain costsheet to an Ent costsheet.
func (r *costsheetRepository) domainToEnt(costsheet *domainCostsheet.Costsheet) *ent.Costsheet {
	return &ent.Costsheet{
		ID:            costsheet.ID,
		TenantID:      costsheet.TenantID,
		EnvironmentID: costsheet.EnvironmentID,
		Status:        string(costsheet.Status),
		CreatedAt:     costsheet.CreatedAt,
		UpdatedAt:     costsheet.UpdatedAt,
		CreatedBy:     costsheet.CreatedBy,
		UpdatedBy:     costsheet.UpdatedBy,
		Name:          costsheet.Name,
		LookupKey:     costsheet.LookupKey,
		Description:   costsheet.Description,
		Metadata:      costsheet.Metadata,
	}
}

// entToDomain converts an Ent costsheet to a domain costsheet.
func (r *costsheetRepository) entToDomain(entCostsheet *ent.Costsheet) *domainCostsheet.Costsheet {
	return &domainCostsheet.Costsheet{
		ID:            entCostsheet.ID,
		Name:          entCostsheet.Name,
		LookupKey:     entCostsheet.LookupKey,
		Description:   entCostsheet.Description,
		Metadata:      entCostsheet.Metadata,
		EnvironmentID: entCostsheet.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  entCostsheet.TenantID,
			Status:    types.Status(entCostsheet.Status),
			CreatedAt: entCostsheet.CreatedAt,
			UpdatedAt: entCostsheet.UpdatedAt,
			CreatedBy: entCostsheet.CreatedBy,
			UpdatedBy: entCostsheet.UpdatedBy,
		},
	}
}
