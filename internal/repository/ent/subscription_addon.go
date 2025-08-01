package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	subscriptionAddon "github.com/flexprice/flexprice/ent/subscriptionaddon"
	"github.com/flexprice/flexprice/internal/cache"
	domainAddon "github.com/flexprice/flexprice/internal/domain/addon"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type subscriptionAddonRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts SubscriptionAddonQueryOptions
	cache     cache.Cache
}

func NewSubscriptionAddonRepository(client postgres.IClient, log *logger.Logger, cache cache.Cache) domainAddon.SubscriptionAddonRepository {
	return &subscriptionAddonRepository{
		client:    client,
		log:       log,
		queryOpts: SubscriptionAddonQueryOptions{},
		cache:     cache,
	}
}

func (r *subscriptionAddonRepository) Create(ctx context.Context, sa *domainAddon.SubscriptionAddon) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("creating subscription addon",
		"subscription_id", sa.SubscriptionID,
		"addon_id", sa.AddonID,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_addon", "create", map[string]interface{}{
		"subscription_id": sa.SubscriptionID,
		"addon_id":        sa.AddonID,
	})
	defer FinishSpan(span)

	// Set environment ID from context if not already set
	if sa.EnvironmentID == "" {
		sa.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	_, err := client.SubscriptionAddon.Create().
		SetID(sa.ID).
		SetTenantID(sa.TenantID).
		SetEnvironmentID(sa.EnvironmentID).
		SetStatus(string(sa.Status)).
		SetSubscriptionID(sa.SubscriptionID).
		SetAddonID(sa.AddonID).
		SetNillableStartDate(sa.StartDate).
		SetNillableEndDate(sa.EndDate).
		SetAddonStatus(string(sa.AddonStatus)).
		SetCancellationReason(sa.CancellationReason).
		SetNillableCancelledAt(sa.CancelledAt).
		SetMetadata(sa.Metadata).
		SetCreatedBy(types.GetUserID(ctx)).
		SetUpdatedBy(types.GetUserID(ctx)).
		SetCreatedAt(sa.CreatedAt).
		SetUpdatedAt(sa.UpdatedAt).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to create subscription addon").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

func (r *subscriptionAddonRepository) GetByID(ctx context.Context, id string) (*domainAddon.SubscriptionAddon, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_addon", "get", map[string]interface{}{
		"subscription_addon_id": id,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)

	sa, err := client.SubscriptionAddon.Query().
		Where(
			subscriptionAddon.ID(id),
			subscriptionAddon.TenantID(types.GetTenantID(ctx)),
			subscriptionAddon.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Subscription addon with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"subscription_addon_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHintf("Failed to get subscription addon with ID %s", id).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return (&domainAddon.SubscriptionAddon{}).FromEnt(sa), nil
}

func (r *subscriptionAddonRepository) GetBySubscriptionID(ctx context.Context, subscriptionID string) ([]*domainAddon.SubscriptionAddon, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_addon", "get_by_subscription", map[string]interface{}{
		"subscription_id": subscriptionID,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)

	subscriptionAddons, err := client.SubscriptionAddon.Query().
		Where(
			subscriptionAddon.SubscriptionID(subscriptionID),
			subscriptionAddon.TenantID(types.GetTenantID(ctx)),
			subscriptionAddon.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		All(ctx)

	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve subscription addons").
			WithReportableDetails(map[string]any{
				"subscription_id": subscriptionID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return (&domainAddon.SubscriptionAddon{}).FromEntList(subscriptionAddons), nil
}

func (r *subscriptionAddonRepository) Update(ctx context.Context, sa *domainAddon.SubscriptionAddon) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("updating subscription addon",
		"subscription_addon_id", sa.ID,
		"subscription_id", sa.SubscriptionID,
		"addon_id", sa.AddonID,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_addon", "update", map[string]interface{}{
		"subscription_addon_id": sa.ID,
	})
	defer FinishSpan(span)

	_, err := client.SubscriptionAddon.Update().
		Where(
			subscriptionAddon.ID(sa.ID),
			subscriptionAddon.TenantID(sa.TenantID),
			subscriptionAddon.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetNillableStartDate(sa.StartDate).
		SetNillableEndDate(sa.EndDate).
		SetAddonStatus(string(sa.AddonStatus)).
		SetCancellationReason(sa.CancellationReason).
		SetNillableCancelledAt(sa.CancelledAt).
		SetMetadata(sa.Metadata).
		SetStatus(string(sa.Status)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Subscription addon with ID %s was not found", sa.ID).
				WithReportableDetails(map[string]any{
					"subscription_addon_id": sa.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update subscription addon").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

func (r *subscriptionAddonRepository) List(ctx context.Context, filter *types.SubscriptionAddonFilter) ([]*domainAddon.SubscriptionAddon, error) {
	if filter == nil {
		filter = &types.SubscriptionAddonFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_addon", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	if err := filter.Validate(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation)
	}

	client := r.client.Querier(ctx)
	query := client.SubscriptionAddon.Query()

	// Apply entity-specific filters
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return nil, err
	}

	// Apply common query options
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	subscriptionAddons, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list subscription addons").
			WithReportableDetails(map[string]any{
				"filter": filter,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return (&domainAddon.SubscriptionAddon{}).FromEntList(subscriptionAddons), nil
}

func (r *subscriptionAddonRepository) Count(ctx context.Context, filter *types.SubscriptionAddonFilter) (int, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_addon", "count", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	if filter != nil {
		if err := filter.Validate(); err != nil {
			SetSpanError(span, err)
			return 0, ierr.WithError(err).
				WithHint("Invalid filter parameters").
				Mark(ierr.ErrValidation)
		}
	}

	client := r.client.Querier(ctx)
	query := client.SubscriptionAddon.Query()

	// Apply base filters (tenant, environment, status)
	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)

	// Apply entity-specific filters
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return 0, err
	}

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count subscription addons").
			WithReportableDetails(map[string]any{
				"filter": filter,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return count, nil
}

// SubscriptionAddonQuery type alias for better readability
type SubscriptionAddonQuery = *ent.SubscriptionAddonQuery

// SubscriptionAddonQueryOptions implements BaseQueryOptions for subscription addon queries
type SubscriptionAddonQueryOptions struct{}

func (o SubscriptionAddonQueryOptions) ApplyTenantFilter(ctx context.Context, query SubscriptionAddonQuery) SubscriptionAddonQuery {
	return query.Where(subscriptionAddon.TenantID(types.GetTenantID(ctx)))
}

func (o SubscriptionAddonQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query SubscriptionAddonQuery) SubscriptionAddonQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(subscriptionAddon.EnvironmentIDEQ(environmentID))
	}
	return query
}

func (o SubscriptionAddonQueryOptions) ApplyStatusFilter(query SubscriptionAddonQuery, status string) SubscriptionAddonQuery {
	if status == "" {
		return query.Where(subscriptionAddon.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(subscriptionAddon.Status(status))
}

func (o SubscriptionAddonQueryOptions) ApplySortFilter(query SubscriptionAddonQuery, field string, order string) SubscriptionAddonQuery {
	orderFunc := ent.Desc
	if order == "asc" {
		orderFunc = ent.Asc
	}
	return query.Order(orderFunc(o.GetFieldName(field)))
}

func (o SubscriptionAddonQueryOptions) ApplyPaginationFilter(query SubscriptionAddonQuery, limit int, offset int) SubscriptionAddonQuery {
	query = query.Limit(limit)
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o SubscriptionAddonQueryOptions) GetFieldName(field string) string {
	switch field {
	case "created_at":
		return subscriptionAddon.FieldCreatedAt
	case "updated_at":
		return subscriptionAddon.FieldUpdatedAt
	case "start_date":
		return subscriptionAddon.FieldStartDate
	case "end_date":
		return subscriptionAddon.FieldEndDate
	case "addon_status":
		return subscriptionAddon.FieldAddonStatus
	case "subscription_id":
		return subscriptionAddon.FieldSubscriptionID
	case "addon_id":
		return subscriptionAddon.FieldAddonID
	case "status":
		return subscriptionAddon.FieldStatus
	default:
		// unknown field
		return ""
	}
}

func (o SubscriptionAddonQueryOptions) applyEntityQueryOptions(ctx context.Context, f *types.SubscriptionAddonFilter, query SubscriptionAddonQuery) (SubscriptionAddonQuery, error) {
	if f == nil {
		return query, nil
	}

	// Apply subscription IDs filter if specified
	if len(f.SubscriptionIDs) > 0 {
		query = query.Where(subscriptionAddon.SubscriptionIDIn(f.SubscriptionIDs...))
	}

	// Apply addon IDs filter if specified
	if len(f.AddonIDs) > 0 {
		query = query.Where(subscriptionAddon.AddonIDIn(f.AddonIDs...))
	}

	// Apply addon statuses filter if specified
	if len(f.AddonStatuses) > 0 {
		statusStrings := make([]string, len(f.AddonStatuses))
		for i, status := range f.AddonStatuses {
			statusStrings[i] = string(status)
		}
		query = query.Where(subscriptionAddon.AddonStatusIn(statusStrings...))
	}

	// Apply time range filters if specified
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil {
			query = query.Where(subscriptionAddon.CreatedAtGTE(*f.StartTime))
		}
		if f.EndTime != nil {
			query = query.Where(subscriptionAddon.CreatedAtLTE(*f.EndTime))
		}
	}

	return query, nil
}
