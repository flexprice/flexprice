package ent

import (
	"context"
	"errors"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/alertlogs"
	"github.com/flexprice/flexprice/ent/predicate"
	"github.com/flexprice/flexprice/internal/cache"
	domainAlertLogs "github.com/flexprice/flexprice/internal/domain/alertlogs"
	"github.com/flexprice/flexprice/internal/dsl"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/lib/pq"
	"github.com/samber/lo"
)

type alertLogsRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts AlertLogQueryOptions
	cache     cache.Cache
}

func NewAlertLogsRepository(client postgres.IClient, log *logger.Logger, cache cache.Cache) domainAlertLogs.Repository {
	return &alertLogsRepository{
		client:    client,
		log:       log,
		queryOpts: AlertLogQueryOptions{},
		cache:     cache,
	}
}

// applyFilters applies common filter logic to an AlertLogs query
func (r *alertLogsRepository) applyFilters(query *ent.AlertLogsQuery, filter *types.AlertLogFilter) *ent.AlertLogsQuery {
	// Apply filters
	if filter.EntityType != "" {
		query = query.Where(alertlogs.EntityType(string(filter.EntityType)))
	}
	if filter.EntityID != "" {
		query = query.Where(alertlogs.EntityID(filter.EntityID))
	}
	if filter.AlertType != "" {
		query = query.Where(alertlogs.AlertType(string(filter.AlertType)))
	}
	if filter.AlertStatus != "" {
		query = query.Where(alertlogs.AlertStatus(string(filter.AlertStatus)))
	}
	if filter.CustomerID != "" {
		query = query.Where(alertlogs.CustomerID(filter.CustomerID))
	}

	// Apply time range filters if provided
	if filter.TimeRangeFilter != nil {
		if filter.TimeRangeFilter.StartTime != nil {
			query = query.Where(alertlogs.CreatedAtGTE(*filter.TimeRangeFilter.StartTime))
		}
		if filter.TimeRangeFilter.EndTime != nil {
			query = query.Where(alertlogs.CreatedAtLTE(*filter.TimeRangeFilter.EndTime))
		}
	}

	return query
}

func (r *alertLogsRepository) Create(ctx context.Context, al *domainAlertLogs.AlertLog) error {
	client := r.client.Writer(ctx)

	r.log.Debugw("creating alert log",
		"alert_log_id", al.ID,
		"tenant_id", al.TenantID,
		"entity_type", al.EntityType,
		"entity_id", al.EntityID,
		"alert_type", al.AlertType,
		"alert_status", al.AlertStatus,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "alertlogs", "create", map[string]interface{}{
		"alert_log_id": al.ID,
		"entity_type":  al.EntityType,
		"entity_id":    al.EntityID,
		"alert_type":   al.AlertType,
	})
	defer FinishSpan(span)

	// Set environment ID from context if not already set
	if al.EnvironmentID == "" {
		al.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	createQuery := client.AlertLogs.Create().
		SetID(al.ID).
		SetTenantID(al.TenantID).
		SetEntityType(string(al.EntityType)).
		SetEntityID(al.EntityID).
		SetAlertType(string(al.AlertType)).
		SetAlertStatus(string(al.AlertStatus)).
		SetAlertInfo(al.AlertInfo).
		SetStatus(string(al.Status)).
		SetCreatedAt(al.CreatedAt).
		SetUpdatedAt(al.UpdatedAt).
		SetCreatedBy(al.CreatedBy).
		SetUpdatedBy(al.UpdatedBy).
		SetEnvironmentID(al.EnvironmentID)

	// Set parent entity fields if provided
	if al.ParentEntityType != nil {
		createQuery = createQuery.SetParentEntityType(*al.ParentEntityType)
	}
	if al.ParentEntityID != nil {
		createQuery = createQuery.SetParentEntityID(*al.ParentEntityID)
	}
	// Set customer ID if provided
	if al.CustomerID != nil {
		createQuery = createQuery.SetCustomerID(*al.CustomerID)
	}

	_, err := createQuery.Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsConstraintError(err) {
			var pqErr *pq.Error
			if errors.As(err, &pqErr) {
				return ierr.WithError(err).
					WithHint("Failed to create alert log due to constraint violation").
					WithReportableDetails(map[string]any{
						"entity_type": al.EntityType,
						"entity_id":   al.EntityID,
						"alert_type":  al.AlertType,
					}).
					Mark(ierr.ErrAlreadyExists)
			}
		}
		return ierr.WithError(err).
			WithHint("Failed to create alert log").
			WithReportableDetails(map[string]any{
				"entity_type": al.EntityType,
				"entity_id":   al.EntityID,
				"alert_type":  al.AlertType,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

func (r *alertLogsRepository) Get(ctx context.Context, id string) (*domainAlertLogs.AlertLog, error) {
	client := r.client.Reader(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "alertlogs", "get", map[string]interface{}{
		"alert_log_id": id,
	})
	defer FinishSpan(span)

	query := client.AlertLogs.Query().Where(
		alertlogs.ID(id),
		alertlogs.TenantID(types.GetTenantID(ctx)),
		alertlogs.EnvironmentID(types.GetEnvironmentID(ctx)),
	)

	alertLog, err := query.Only(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Alert log not found").
				WithReportableDetails(map[string]any{
					"alert_log_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get alert log").
			WithReportableDetails(map[string]any{
				"alert_log_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainAlertLogs.FromEnt(alertLog), nil
}

func (r *alertLogsRepository) List(ctx context.Context, filter *types.AlertLogFilter) ([]*domainAlertLogs.AlertLog, error) {
	client := r.client.Reader(ctx)
	r.log.Debugw("listing alert logs",
		"tenant_id", types.GetTenantID(ctx),
		"limit", filter.GetLimit(),
		"offset", filter.GetOffset(),
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "alertlogs", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	query := client.AlertLogs.Query()
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list alert logs").
			Mark(ierr.ErrDatabase)
	}
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)
	alertLogs, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list alert logs").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainAlertLogs.FromEntList(alertLogs), nil
}

func (r *alertLogsRepository) Count(ctx context.Context, filter *types.AlertLogFilter) (int, error) {
	client := r.client.Reader(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "alertlogs", "count", map[string]interface{}{})
	defer FinishSpan(span)

	query := client.AlertLogs.Query().Where(
		alertlogs.TenantID(types.GetTenantID(ctx)),
		alertlogs.EnvironmentID(types.GetEnvironmentID(ctx)),
	)

	// Apply common filters
	query = r.applyFilters(query, filter)

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count alert logs").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return count, nil
}

// GetLatestAlert fetches the latest alert log based on provided filters
// All parameters except entityType and entityID are optional
// If alertType is nil, searches across all alert types
// If parentEntityType and parentEntityID are provided, filters by those as well
func (r *alertLogsRepository) GetLatestAlert(ctx context.Context, entityType types.AlertEntityType, entityID string, alertType *types.AlertType, parentEntityType *string, parentEntityID *string) (*domainAlertLogs.AlertLog, error) {
	client := r.client.Reader(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "alertlogs", "get_latest_alert", map[string]interface{}{
		"entity_type":        entityType,
		"entity_id":          entityID,
		"alert_type":         alertType,
		"parent_entity_type": parentEntityType,
		"parent_entity_id":   parentEntityID,
	})
	defer FinishSpan(span)

	// Build the base query with required fields
	query := client.AlertLogs.Query().Where(
		alertlogs.AlertType(string(lo.FromPtr(alertType))),
		alertlogs.EntityType(string(entityType)),
		alertlogs.EntityID(entityID),
		alertlogs.TenantID(types.GetTenantID(ctx)),
		alertlogs.EnvironmentID(types.GetEnvironmentID(ctx)),
	)

	// Add optional parent entity filters
	if parentEntityType != nil {
		query = query.Where(alertlogs.ParentEntityTypeEQ(*parentEntityType))
	}
	if parentEntityID != nil {
		query = query.Where(alertlogs.ParentEntityIDEQ(*parentEntityID))
	}

	// Order by creation time descending to get the latest
	query = query.Order(ent.Desc(alertlogs.FieldCreatedAt)).Limit(1)

	alertLog, err := query.First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			// No alert logs found - this is not an error
			SetSpanSuccess(span)
			return nil, nil
		}
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to get latest alert log").
			WithReportableDetails(map[string]any{
				"entity_type":        entityType,
				"entity_id":          entityID,
				"alert_type":         alertType,
				"parent_entity_type": parentEntityType,
				"parent_entity_id":   parentEntityID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainAlertLogs.FromEnt(alertLog), nil
}

func (r *alertLogsRepository) ListByEntity(ctx context.Context, entityType types.AlertEntityType, entityID string, limit int) ([]*domainAlertLogs.AlertLog, error) {
	client := r.client.Reader(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "alertlogs", "list_by_entity", map[string]interface{}{
		"entity_type": entityType,
		"entity_id":   entityID,
		"limit":       limit,
	})
	defer FinishSpan(span)

	query := client.AlertLogs.Query().Where(
		alertlogs.EntityType(string(entityType)),
		alertlogs.EntityID(entityID),
		alertlogs.TenantID(types.GetTenantID(ctx)),
		alertlogs.EnvironmentID(types.GetEnvironmentID(ctx)),
	)

	// Order by creation time descending to get the latest first
	query = query.Order(ent.Desc(alertlogs.FieldCreatedAt))

	if limit > 0 {
		query = query.Limit(limit)
	}

	alertLogs, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list alert logs for entity").
			WithReportableDetails(map[string]any{
				"entity_type": entityType,
				"entity_id":   entityID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainAlertLogs.FromEntList(alertLogs), nil
}

func (r *alertLogsRepository) ListByAlertType(ctx context.Context, alertType types.AlertType, limit int) ([]*domainAlertLogs.AlertLog, error) {
	client := r.client.Reader(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "alertlogs", "list_by_alert_type", map[string]interface{}{
		"alert_type": alertType,
		"limit":      limit,
	})
	defer FinishSpan(span)

	query := client.AlertLogs.Query().Where(
		alertlogs.AlertType(string(alertType)),
		alertlogs.TenantID(types.GetTenantID(ctx)),
		alertlogs.EnvironmentID(types.GetEnvironmentID(ctx)),
	)

	// Order by creation time descending to get the latest first
	query = query.Order(ent.Desc(alertlogs.FieldCreatedAt))

	if limit > 0 {
		query = query.Limit(limit)
	}

	alertLogs, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list alert logs by type").
			WithReportableDetails(map[string]any{
				"alert_type": alertType,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainAlertLogs.FromEntList(alertLogs), nil
}

// AlertLogQuery type alias for better readability
type AlertLogQuery = *ent.AlertLogsQuery

// AlertLogQueryOptions implements BaseQueryOptions for alert log queries
type AlertLogQueryOptions struct{}

func (o AlertLogQueryOptions) ApplyTenantFilter(ctx context.Context, query AlertLogQuery) AlertLogQuery {
	return query.Where(alertlogs.TenantID(types.GetTenantID(ctx)))
}

func (o AlertLogQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query AlertLogQuery) AlertLogQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(alertlogs.EnvironmentID(environmentID))
	}
	return query
}

func (o AlertLogQueryOptions) ApplyStatusFilter(query AlertLogQuery, status string) AlertLogQuery {
	if status == "" {
		return query.Where(alertlogs.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(alertlogs.Status(status))
}

func (o AlertLogQueryOptions) ApplySortFilter(query AlertLogQuery, field string, order string) AlertLogQuery {
	field = o.GetFieldName(field)

	// Apply standard ordering for all fields
	if order == types.OrderDesc {
		query = query.Order(ent.Desc(field))
	} else {
		query = query.Order(ent.Asc(field))
	}
	return query
}

func (o AlertLogQueryOptions) ApplyPaginationFilter(query AlertLogQuery, limit int, offset int) AlertLogQuery {
	return query.Offset(offset).Limit(limit)
}

func (o AlertLogQueryOptions) GetFieldName(field string) string {
	switch field {
	case "created_at":
		return alertlogs.FieldCreatedAt
	case "updated_at":
		return alertlogs.FieldUpdatedAt
	case "entity_type":
		return alertlogs.FieldEntityType
	case "entity_id":
		return alertlogs.FieldEntityID
	case "parent_entity_type":
		return alertlogs.FieldParentEntityType
	case "parent_entity_id":
		return alertlogs.FieldParentEntityID
	case "customer_id":
		return alertlogs.FieldCustomerID
	case "alert_type":
		return alertlogs.FieldAlertType
	case "alert_status":
		return alertlogs.FieldAlertStatus
	case "status":
		return alertlogs.FieldStatus
	default:
		// unknown field
		return ""
	}
}

func (o AlertLogQueryOptions) GetFieldResolver(field string) (string, error) {
	fieldName := o.GetFieldName(field)
	if fieldName == "" {
		return "", ierr.NewErrorf("unknown field name '%s' in alert log query", field).
			Mark(ierr.ErrValidation)
	}
	return fieldName, nil
}

func (o AlertLogQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.AlertLogFilter, query AlertLogQuery) (AlertLogQuery, error) {
	var err error
	if f == nil {
		return query, nil
	}

	if f.EntityType != "" {
		query = query.Where(alertlogs.EntityType(string(f.EntityType)))
	}

	if f.EntityID != "" {
		query = query.Where(alertlogs.EntityID(f.EntityID))
	}

	if f.AlertType != "" {
		query = query.Where(alertlogs.AlertType(string(f.AlertType)))
	}

	if f.AlertStatus != "" {
		query = query.Where(alertlogs.AlertStatus(string(f.AlertStatus)))
	}

	if f.CustomerID != "" {
		query = query.Where(alertlogs.CustomerID(f.CustomerID))
	}

	if f.Filters != nil {
		query, err = dsl.ApplyFilters[AlertLogQuery, predicate.AlertLogs](
			query,
			f.Filters,
			o.GetFieldResolver,
			func(p dsl.Predicate) predicate.AlertLogs { return predicate.AlertLogs(p) },
		)
		if err != nil {
			return nil, err
		}
	}

	// Apply sorts using the generic function
	if f.Sort != nil {
		query, err = dsl.ApplySorts[AlertLogQuery, alertlogs.OrderOption](
			query,
			f.Sort,
			o.GetFieldResolver,
			func(o dsl.OrderFunc) alertlogs.OrderOption { return alertlogs.OrderOption(o) },
		)
		if err != nil {
			return nil, err
		}
	}

	// Apply time range filters if specified
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil {
			query = query.Where(alertlogs.CreatedAtGTE(*f.StartTime))
		}
		if f.EndTime != nil {
			query = query.Where(alertlogs.CreatedAtLTE(*f.EndTime))
		}
	}

	return query, nil
}
