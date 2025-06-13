// Package ent provides the implementation of the costsheet repository using Ent ORM.
// This package handles all database operations related to costsheets, including CRUD operations
// and specialized queries.
package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/costsheet"
	domainCostsheet "github.com/flexprice/flexprice/internal/domain/costsheet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

// costsheetRepository implements the costsheet.Repository interface using Ent ORM.
// It provides methods for managing costsheet records in the database.
type costsheetRepository struct {
	client    postgres.IClient      // Database client for executing queries
	logger    *logger.Logger        // Logger for tracking operations
	queryOpts CostsheetQueryOptions // Query options for filtering and sorting
}

// NewCostsheetRepository creates a new instance of the Ent-based costsheet repository.
// It initializes the repository with the provided database client and logger.
func NewCostsheetRepository(client postgres.IClient, logger *logger.Logger) domainCostsheet.Repository {
	return &costsheetRepository{
		client:    client,
		logger:    logger,
		queryOpts: CostsheetQueryOptions{},
	}
}

// Create stores a new costsheet record in the database.
// It handles the following:
// - Creates a new costsheet with the provided data
// - Sets environment ID from context
// - Handles constraint violations and other database errors
// - Returns appropriate domain errors for different failure scenarios
func (r *costsheetRepository) Create(ctx context.Context, cs *domainCostsheet.Costsheet) error {
	// Get database client from context
	client := r.client.Querier(ctx)

	// Start tracing span for monitoring and debugging
	span := StartRepositorySpan(ctx, "costsheet", "create", map[string]interface{}{
		"meter_id": cs.MeterID,
		"price_id": cs.PriceID,
	})
	defer FinishSpan(span)

	// Get environment ID from context for multi-tenant support
	// environmentID := types.GetEnvironmentID(ctx)
	tenantID, environmentID := domainCostsheet.GetTenantAndEnvFromContext(ctx)

	// Create costsheet record using Ent client
	_, err := client.Costsheet.Create().
		SetID(cs.ID).
		SetTenantID(tenantID).
		SetMeterID(cs.MeterID).
		SetPriceID(cs.PriceID).
		SetStatus(string(cs.Status)).
		SetCreatedAt(cs.CreatedAt).
		SetUpdatedAt(cs.UpdatedAt).
		SetCreatedBy(cs.CreatedBy).
		SetUpdatedBy(cs.UpdatedBy).
		SetEnvironmentID(environmentID).
		Save(ctx)

	// Handle potential errors
	if err != nil {
		SetSpanError(span, err)
		// Handle unique constraint violations
		if ent.IsConstraintError(err) {
			return ierr.WithError(err).
				WithMessage("costsheet already exists").
				WithHint("A costsheet with this meter and price combination already exists").
				WithReportableDetails(map[string]any{
					"meter_id": cs.MeterID,
					"price_id": cs.PriceID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		// Handle other database errors
		return ierr.WithError(err).
			WithMessage("failed to create costsheet").
			WithHint("Failed to create costsheet").
			WithReportableDetails(map[string]any{
				"meter_id": cs.MeterID,
				"price_id": cs.PriceID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

// Get retrieves a costsheet by its ID.
// It includes tenant and environment filtering for multi-tenant support.
// Returns domain errors for not found and database operation failures.
func (r *costsheetRepository) Get(ctx context.Context, id string) (*domainCostsheet.Costsheet, error) {
	client := r.client.Querier(ctx)

	// Start tracing span
	span := StartRepositorySpan(ctx, "costsheet", "get", map[string]interface{}{
		"costsheet_id": id,
	})
	defer FinishSpan(span)

	// Query costsheet with tenant and environment filters
	cs, err := client.Costsheet.Query().
		Where(
			costsheet.ID(id),
			costsheet.TenantID(types.GetTenantID(ctx)),
			costsheet.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	// Handle potential errors
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithMessage("costsheet not found").
				WithHint("Costsheet not found").
				WithReportableDetails(map[string]any{
					"costsheet_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithMessage("failed to get costsheet").
			WithHint("Failed to retrieve costsheet").
			WithReportableDetails(map[string]any{
				"costsheet_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return fromEnt(cs), nil
}

// Update modifies an existing costsheet record.
// It updates the costsheet with new values while maintaining tenant and environment isolation.
// Handles various error scenarios including not found and constraint violations.
func (r *costsheetRepository) Update(ctx context.Context, cs *domainCostsheet.Costsheet) error {
	client := r.client.Querier(ctx)

	// Start tracing span
	span := StartRepositorySpan(ctx, "costsheet", "update", map[string]interface{}{
		"costsheet_id": cs.ID,
	})
	defer FinishSpan(span)

	// Update costsheet with new values
	_, err := client.Costsheet.Update().
		Where(
			costsheet.ID(cs.ID),
			costsheet.TenantID(cs.TenantID),
			costsheet.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetMeterID(cs.MeterID).
		SetPriceID(cs.PriceID).
		SetStatus(string(cs.Status)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	// Handle potential errors
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithMessage("costsheet not found").
				WithHint("Costsheet not found").
				WithReportableDetails(map[string]any{
					"costsheet_id": cs.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		if ent.IsConstraintError(err) {
			return ierr.WithError(err).
				WithMessage("costsheet already exists").
				WithHint("A costsheet with this meter and price combination already exists").
				WithReportableDetails(map[string]any{
					"meter_id": cs.MeterID,
					"price_id": cs.PriceID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithMessage("failed to update costsheet").
			WithHint("Failed to update costsheet").
			WithReportableDetails(map[string]any{
				"costsheet_id": cs.ID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

// Delete performs a soft delete of a costsheet by marking it as archived.
// It maintains data history while preventing further use of the costsheet.
func (r *costsheetRepository) Delete(ctx context.Context, id string) error {
	client := r.client.Querier(ctx)

	// Start tracing span
	span := StartRepositorySpan(ctx, "costsheet", "delete", map[string]interface{}{
		"costsheet_id": id,
	})
	defer FinishSpan(span)

	// Soft delete by updating status to archived
	_, err := client.Costsheet.Update().
		Where(
			costsheet.ID(id),
			costsheet.TenantID(types.GetTenantID(ctx)),
			costsheet.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	// Handle potential errors
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithMessage("costsheet not found").
				WithHint("Costsheet not found").
				WithReportableDetails(map[string]any{
					"costsheet_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithMessage("failed to delete costsheet").
			WithHint("Failed to delete costsheet").
			WithReportableDetails(map[string]any{
				"costsheet_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

// List retrieves costsheets based on the provided filter criteria.
// Supports pagination, sorting, and various filtering options.
func (r *costsheetRepository) List(ctx context.Context, filter *domainCostsheet.Filter) ([]*domainCostsheet.Costsheet, error) {
	client := r.client.Querier(ctx)

	// Start tracing span
	span := StartRepositorySpan(ctx, "costsheet", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	// Initialize query
	query := client.Costsheet.Query()

	// Apply base filters (tenant, environment, etc.)
	query = ApplyQueryOptions(ctx, query, filter.QueryFilter, r.queryOpts)

	// Apply costsheet-specific filters
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	// Execute query
	costsheets, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithMessage("failed to list costsheets").
			WithHint("Could not retrieve costsheets list").
			WithReportableDetails(map[string]any{
				"tenant_id": types.GetTenantID(ctx),
				"filter":    filter,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Convert Ent models to domain models
	result := make([]*domainCostsheet.Costsheet, len(costsheets))
	for i, cs := range costsheets {
		result[i] = fromEnt(cs)
	}

	SetSpanSuccess(span)
	return result, nil
}

// GetByMeterAndPrice retrieves a costsheet by meter ID and price ID.
// Only returns published costsheets for the given meter and price combination.
func (r *costsheetRepository) GetByMeterAndPrice(ctx context.Context, meterID, priceID string) (*domainCostsheet.Costsheet, error) {
	client := r.client.Querier(ctx)

	// Start tracing span
	span := StartRepositorySpan(ctx, "costsheet", "get_by_meter_and_price", map[string]interface{}{
		"meter_id": meterID,
		"price_id": priceID,
	})
	defer FinishSpan(span)

	// Query costsheet with specific filters
	cs, err := client.Costsheet.Query().
		Where(
			costsheet.MeterID(meterID),
			costsheet.PriceID(priceID),
			costsheet.TenantID(types.GetTenantID(ctx)),
			costsheet.EnvironmentID(types.GetEnvironmentID(ctx)),
			costsheet.Status(string(types.StatusPublished)),
		).
		Only(ctx)

	// Handle potential errors
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithMessage("costsheet not found").
				WithHint("No published costsheet found for the given meter and price").
				WithReportableDetails(map[string]any{
					"meter_id": meterID,
					"price_id": priceID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithMessage("failed to get costsheet").
			WithHint("Failed to retrieve costsheet by meter and price").
			WithReportableDetails(map[string]any{
				"meter_id": meterID,
				"price_id": priceID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return fromEnt(cs), nil
}

// Query option types and methods for costsheet queries
type CostsheetQuery = *ent.CostsheetQuery

// CostsheetQueryOptions implements BaseQueryOptions for costsheet queries
// Provides methods for applying various query filters and options
type CostsheetQueryOptions struct{}

// ApplyTenantFilter adds tenant ID filter to the query
func (o CostsheetQueryOptions) ApplyTenantFilter(ctx context.Context, query CostsheetQuery) CostsheetQuery {
	return query.Where(costsheet.TenantID(types.GetTenantID(ctx)))
}

// ApplyEnvironmentFilter adds environment ID filter to the query
func (o CostsheetQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query CostsheetQuery) CostsheetQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(costsheet.EnvironmentID(environmentID))
	}
	return query
}

// ApplyStatusFilter adds status filter to the query
// Excludes deleted records by default if no status is specified
func (o CostsheetQueryOptions) ApplyStatusFilter(query CostsheetQuery, status string) CostsheetQuery {
	if status == "" {
		return query.Where(costsheet.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(costsheet.Status(status))
}

// ApplySortFilter adds sorting to the query based on field and order
func (o CostsheetQueryOptions) ApplySortFilter(query CostsheetQuery, field string, order string) CostsheetQuery {
	orderFunc := ent.Desc
	if order == types.OrderAsc {
		orderFunc = ent.Asc
	}
	return query.Order(orderFunc(o.GetFieldName(field)))
}

// ApplyPaginationFilter adds pagination to the query
func (o CostsheetQueryOptions) ApplyPaginationFilter(query CostsheetQuery, limit int, offset int) CostsheetQuery {
	query = query.Limit(limit)
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

// GetFieldName maps domain field names to database column names
func (o CostsheetQueryOptions) GetFieldName(field string) string {
	switch field {
	case "created_at":
		return costsheet.FieldCreatedAt
	case "updated_at":
		return costsheet.FieldUpdatedAt
	default:
		return field
	}
}

// applyEntityQueryOptions applies costsheet-specific filters to the query
func (o CostsheetQueryOptions) applyEntityQueryOptions(_ context.Context, f *domainCostsheet.Filter, query CostsheetQuery) CostsheetQuery {
	if f == nil {
		return query
	}

	// Apply ID filters
	if len(f.CostsheetIDs) > 0 {
		query = query.Where(costsheet.IDIn(f.CostsheetIDs...))
	}

	// Apply meter ID filters
	if len(f.MeterIDs) > 0 {
		query = query.Where(costsheet.MeterIDIn(f.MeterIDs...))
	}

	// Apply price ID filters
	if len(f.PriceIDs) > 0 {
		query = query.Where(costsheet.PriceIDIn(f.PriceIDs...))
	}

	// Apply status filter
	if f.Status != "" {
		query = query.Where(costsheet.Status(string(f.Status)))
	}

	// Apply time range filters
	if f.TimeRangeFilter != nil {
		if f.TimeRangeFilter.StartTime != nil {
			query = query.Where(costsheet.CreatedAtGTE(*f.TimeRangeFilter.StartTime))
		}
		if f.TimeRangeFilter.EndTime != nil {
			query = query.Where(costsheet.CreatedAtLTE(*f.TimeRangeFilter.EndTime))
		}
	}

	return query
}

// fromEnt converts an Ent costsheet model to a domain costsheet model
// Maps database fields to domain model fields
func fromEnt(cs *ent.Costsheet) *domainCostsheet.Costsheet {
	return &domainCostsheet.Costsheet{
		ID:      cs.ID,
		MeterID: cs.MeterID,
		PriceID: cs.PriceID,
		BaseModel: types.BaseModel{
			TenantID:  cs.TenantID,
			Status:    types.Status(cs.Status),
			CreatedAt: cs.CreatedAt,
			UpdatedAt: cs.UpdatedAt,
			CreatedBy: cs.CreatedBy,
			UpdatedBy: cs.UpdatedBy,
		},
	}
}
