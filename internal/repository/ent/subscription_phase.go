package ent

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/subscriptionphase"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type subscriptionPhaseRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts SubscriptionPhaseQueryOptions
	cache     cache.Cache
}

// NewSubscriptionPhaseRepository creates a new subscription phase repository
func NewSubscriptionPhaseRepository(client postgres.IClient, log *logger.Logger, cache cache.Cache) subscription.SubscriptionPhaseRepository {
	return &subscriptionPhaseRepository{
		client:    client,
		log:       log,
		queryOpts: SubscriptionPhaseQueryOptions{},
		cache:     cache,
	}
}

// Create creates a new subscription phase
func (r *subscriptionPhaseRepository) Create(ctx context.Context, phase *subscription.SubscriptionPhase) error {
	client := r.client.Writer(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_phase", "create", map[string]interface{}{
		"phase_id":        phase.ID,
		"subscription_id": phase.SubscriptionID,
		"tenant_id":       phase.TenantID,
	})
	defer FinishSpan(span)

	r.log.Debugw("creating subscription phase",
		"phase_id", phase.ID,
		"subscription_id", phase.SubscriptionID,
		"tenant_id", phase.TenantID,
	)

	// Set environment ID from context if not already set
	if phase.EnvironmentID == "" {
		phase.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	entPhase, err := client.SubscriptionPhase.Create().
		SetID(phase.ID).
		SetTenantID(phase.TenantID).
		SetSubscriptionID(phase.SubscriptionID).
		SetStartDate(phase.StartDate).
		SetNillableEndDate(phase.EndDate).
		SetStatus(string(phase.Status)).
		SetCreatedAt(phase.CreatedAt).
		SetUpdatedAt(phase.UpdatedAt).
		SetCreatedBy(phase.CreatedBy).
		SetUpdatedBy(phase.UpdatedBy).
		SetEnvironmentID(phase.EnvironmentID).
		SetMetadata(phase.Metadata).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsConstraintError(err) {
			return ierr.WithError(err).
				WithHint("A subscription phase with this configuration already exists").
				WithReportableDetails(map[string]interface{}{
					"subscription_id": phase.SubscriptionID,
					"start_date":      phase.StartDate,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to create subscription phase").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": phase.SubscriptionID,
				"phase_id":        phase.ID,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Update the input phase with created data
	*phase = *subscription.SubscriptionPhaseFromEnt(entPhase)
	SetSpanSuccess(span)
	return nil
}

// CreateBulk creates multiple subscription phases in bulk
func (r *subscriptionPhaseRepository) CreateBulk(ctx context.Context, phases []*subscription.SubscriptionPhase) error {
	client := r.client.Writer(ctx)

	span := StartRepositorySpan(ctx, "subscription_phase", "create_bulk", map[string]interface{}{
		"count": len(phases),
	})
	defer FinishSpan(span)

	if len(phases) == 0 {
		return nil
	}

	builders := make([]*ent.SubscriptionPhaseCreate, 0, len(phases))
	for _, phase := range phases {
		// Set environment ID from context if not already set
		if phase.EnvironmentID == "" {
			phase.EnvironmentID = types.GetEnvironmentID(ctx)
		}

		builder := client.SubscriptionPhase.Create().
			SetID(phase.ID).
			SetTenantID(phase.TenantID).
			SetSubscriptionID(phase.SubscriptionID).
			SetStartDate(phase.StartDate).
			SetNillableEndDate(phase.EndDate).
			SetStatus(string(phase.Status)).
			SetCreatedAt(phase.CreatedAt).
			SetUpdatedAt(phase.UpdatedAt).
			SetCreatedBy(phase.CreatedBy).
			SetUpdatedBy(phase.UpdatedBy).
			SetEnvironmentID(phase.EnvironmentID).
			SetMetadata(phase.Metadata)

		builders = append(builders, builder)
	}

	entPhases, err := client.SubscriptionPhase.CreateBulk(builders...).Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to create subscription phases in bulk").
			Mark(ierr.ErrDatabase)
	}

	// Update input phases with created data
	for i, entPhase := range entPhases {
		if i < len(phases) {
			*phases[i] = *subscription.SubscriptionPhaseFromEnt(entPhase)
		}
	}

	SetSpanSuccess(span)
	return nil
}

// Get retrieves a subscription phase by ID
func (r *subscriptionPhaseRepository) Get(ctx context.Context, id string) (*subscription.SubscriptionPhase, error) {
	client := r.client.Reader(ctx)

	span := StartRepositorySpan(ctx, "subscription_phase", "get", map[string]interface{}{
		"phase_id": id,
	})
	defer FinishSpan(span)

	entPhase, err := client.SubscriptionPhase.Query().
		Where(
			subscriptionphase.ID(id),
			subscriptionphase.TenantID(types.GetTenantID(ctx)),
			subscriptionphase.EnvironmentID(types.GetEnvironmentID(ctx)),
			subscriptionphase.StatusNotIn(string(types.StatusDeleted)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.NewError("subscription phase not found").
				WithHint("The subscription phase with the given ID does not exist").
				WithReportableDetails(map[string]interface{}{
					"phase_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get subscription phase").
			WithReportableDetails(map[string]interface{}{
				"phase_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return subscription.SubscriptionPhaseFromEnt(entPhase), nil
}

// Update updates an existing subscription phase
func (r *subscriptionPhaseRepository) Update(ctx context.Context, phase *subscription.SubscriptionPhase) error {
	client := r.client.Writer(ctx)

	span := StartRepositorySpan(ctx, "subscription_phase", "update", map[string]interface{}{
		"phase_id":        phase.ID,
		"subscription_id": phase.SubscriptionID,
	})
	defer FinishSpan(span)

	entPhase, err := client.SubscriptionPhase.UpdateOneID(phase.ID).
		Where(
			subscriptionphase.TenantID(types.GetTenantID(ctx)),
			subscriptionphase.EnvironmentID(types.GetEnvironmentID(ctx)),
			subscriptionphase.StatusNotIn(string(types.StatusDeleted)),
		).
		SetNillableEndDate(phase.EndDate).
		SetStatus(string(phase.Status)).
		SetUpdatedAt(phase.UpdatedAt).
		SetUpdatedBy(phase.UpdatedBy).
		SetMetadata(phase.Metadata).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.NewError("subscription phase not found").
				WithHint("The subscription phase with the given ID does not exist").
				WithReportableDetails(map[string]interface{}{
					"phase_id": phase.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update subscription phase").
			WithReportableDetails(map[string]interface{}{
				"phase_id": phase.ID,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Update the input phase with updated data
	*phase = *subscription.SubscriptionPhaseFromEnt(entPhase)
	SetSpanSuccess(span)
	return nil
}

// Delete deletes a subscription phase by ID (soft delete by setting status to archived)
func (r *subscriptionPhaseRepository) Delete(ctx context.Context, id string) error {
	client := r.client.Writer(ctx)

	span := StartRepositorySpan(ctx, "subscription_phase", "delete", map[string]interface{}{
		"phase_id": id,
	})
	defer FinishSpan(span)

	err := client.SubscriptionPhase.UpdateOneID(id).
		Where(
			subscriptionphase.TenantID(types.GetTenantID(ctx)),
			subscriptionphase.EnvironmentID(types.GetEnvironmentID(ctx)),
			subscriptionphase.StatusNotIn(string(types.StatusDeleted)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Exec(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.NewError("subscription phase not found").
				WithHint("The subscription phase with the given ID does not exist").
				WithReportableDetails(map[string]interface{}{
					"phase_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete subscription phase").
			WithReportableDetails(map[string]interface{}{
				"phase_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

// List retrieves subscription phases based on filter
func (r *subscriptionPhaseRepository) List(ctx context.Context, filter *types.SubscriptionPhaseFilter) ([]*subscription.SubscriptionPhase, error) {
	r.log.Debugw("listing subscription phases", "filter", filter)

	if filter == nil {
		filter = &types.SubscriptionPhaseFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_phase", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	if err := filter.Validate(); err != nil {
		SetSpanError(span, err)
		return nil, fmt.Errorf("invalid filter: %w", err)
	}

	client := r.client.Reader(ctx)
	query := client.SubscriptionPhase.Query()

	// Apply entity-specific filters
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to apply query options").
			Mark(ierr.ErrDatabase)
	}

	// Apply common query options
	query = ApplyQueryOptions(ctx, query, filter.QueryFilter, r.queryOpts)

	entPhases, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		r.log.Errorw("failed to list subscription phases", "error", err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list subscription phases").
			Mark(ierr.ErrDatabase)
	}

	// Convert to domain model
	result := subscription.SubscriptionPhaseListFromEnt(entPhases)

	SetSpanSuccess(span)
	return result, nil
}

// Count returns the count of subscription phases matching the filter
func (r *subscriptionPhaseRepository) Count(ctx context.Context, filter *types.SubscriptionPhaseFilter) (int, error) {
	r.log.Debugw("counting subscription phases", "filter", filter)

	if filter == nil {
		filter = &types.SubscriptionPhaseFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	span := StartRepositorySpan(ctx, "subscription_phase", "count", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	if err := filter.Validate(); err != nil {
		SetSpanError(span, err)
		return 0, fmt.Errorf("invalid filter: %w", err)
	}

	client := r.client.Reader(ctx)
	query := client.SubscriptionPhase.Query()

	// Apply entity-specific filters
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to apply query options").
			Mark(ierr.ErrDatabase)
	}

	// Apply common query options (without pagination for count)
	query = ApplyBaseFilters(ctx, query, filter.QueryFilter, r.queryOpts)

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		r.log.Errorw("failed to count subscription phases", "error", err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count subscription phases").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return count, nil
}

// SubscriptionPhaseQueryOptions implements BaseQueryOptions for subscription phase queries
type SubscriptionPhaseQueryOptions struct{}

// Type alias for better readability
type SubscriptionPhaseQuery = *ent.SubscriptionPhaseQuery

func (o SubscriptionPhaseQueryOptions) ApplyTenantFilter(ctx context.Context, query SubscriptionPhaseQuery) SubscriptionPhaseQuery {
	return query.Where(subscriptionphase.TenantID(types.GetTenantID(ctx)))
}

func (o SubscriptionPhaseQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query SubscriptionPhaseQuery) SubscriptionPhaseQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(subscriptionphase.EnvironmentIDEQ(environmentID))
	}
	return query
}

func (o SubscriptionPhaseQueryOptions) ApplyStatusFilter(query SubscriptionPhaseQuery, status string) SubscriptionPhaseQuery {
	if status == "" {
		return query.Where(subscriptionphase.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(subscriptionphase.Status(status))
}

func (o SubscriptionPhaseQueryOptions) ApplySortFilter(query SubscriptionPhaseQuery, field string, order string) SubscriptionPhaseQuery {
	orderFunc := ent.Desc
	if order == "asc" {
		orderFunc = ent.Asc
	}
	return query.Order(orderFunc(o.GetFieldName(field)))
}

func (o SubscriptionPhaseQueryOptions) ApplyPaginationFilter(query SubscriptionPhaseQuery, limit int, offset int) SubscriptionPhaseQuery {
	query = query.Limit(limit)
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o SubscriptionPhaseQueryOptions) GetFieldName(field string) string {
	switch field {
	case "created_at":
		return subscriptionphase.FieldCreatedAt
	case "updated_at":
		return subscriptionphase.FieldUpdatedAt
	case "start_date":
		return subscriptionphase.FieldStartDate
	case "end_date":
		return subscriptionphase.FieldEndDate
	case "status":
		return subscriptionphase.FieldStatus
	case "subscription_id":
		return subscriptionphase.FieldSubscriptionID
	case "metadata":
		return subscriptionphase.FieldMetadata
	default:
		return field
	}
}

func (o SubscriptionPhaseQueryOptions) GetFieldResolver(field string) (string, error) {
	fieldName := o.GetFieldName(field)
	if fieldName == "" {
		return "", ierr.NewErrorf("unknown field name '%s' in subscription phase query", field).
			Mark(ierr.ErrValidation)
	}
	return fieldName, nil
}

// applyEntityQueryOptions applies subscription phase-specific filters to the query
func (o *SubscriptionPhaseQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.SubscriptionPhaseFilter, query SubscriptionPhaseQuery) (SubscriptionPhaseQuery, error) {
	if f == nil {
		return query, nil
	}

	// Apply subscription IDs filter
	if len(f.SubscriptionIDs) > 0 {
		query = query.Where(subscriptionphase.SubscriptionIDIn(f.SubscriptionIDs...))
	}

	// Apply phase IDs filter
	if len(f.PhaseIDs) > 0 {
		query = query.Where(subscriptionphase.IDIn(f.PhaseIDs...))
	}

	// Apply active only filter
	if f.ActiveOnly {
		activeAt := time.Now()
		if f.ActiveAt != nil {
			activeAt = *f.ActiveAt
		}
		query = query.Where(
			subscriptionphase.And(
				subscriptionphase.StartDateLTE(activeAt),
				subscriptionphase.Or(
					subscriptionphase.EndDateGT(activeAt),
					subscriptionphase.EndDateIsNil(),
				),
			),
		)
	}

	// Apply time range filters
	if f.TimeRangeFilter != nil {
		if f.TimeRangeFilter.StartTime != nil {
			query = query.Where(subscriptionphase.StartDateGTE(*f.TimeRangeFilter.StartTime))
		}
		if f.TimeRangeFilter.EndTime != nil {
			query = query.Where(subscriptionphase.Or(
				subscriptionphase.EndDateLTE(*f.TimeRangeFilter.EndTime),
				subscriptionphase.EndDateIsNil(),
			))
		}
	}

	return query, nil
}
