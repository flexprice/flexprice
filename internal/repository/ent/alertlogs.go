package ent

import (
	"context"
	"errors"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/alertlogs"
	"github.com/flexprice/flexprice/internal/cache"
	domainAlertLogs "github.com/flexprice/flexprice/internal/domain/alertlogs"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/lib/pq"
)

type alertLogsRepository struct {
	client postgres.IClient
	log    *logger.Logger
	cache  cache.Cache
}

func NewAlertLogsRepository(client postgres.IClient, log *logger.Logger, cache cache.Cache) domainAlertLogs.Repository {
	return &alertLogsRepository{
		client: client,
		log:    log,
		cache:  cache,
	}
}

func (r *alertLogsRepository) Create(ctx context.Context, al *domainAlertLogs.AlertLog) error {
	client := r.client.Querier(ctx)

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

	_, err := client.AlertLogs.Create().
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
		SetEnvironmentID(al.EnvironmentID).
		Save(ctx)

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
	client := r.client.Querier(ctx)

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
	client := r.client.Querier(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "alertlogs", "list", map[string]interface{}{
		"limit":  filter.GetLimit(),
		"offset": filter.GetOffset(),
	})
	defer FinishSpan(span)

	query := client.AlertLogs.Query().Where(
		alertlogs.TenantID(types.GetTenantID(ctx)),
		alertlogs.EnvironmentID(types.GetEnvironmentID(ctx)),
	)

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

	// Apply pagination
	if filter.GetLimit() > 0 {
		query = query.Limit(filter.GetLimit())
	}
	if filter.GetOffset() > 0 {
		query = query.Offset(filter.GetOffset())
	}

	// Apply default sorting by creation time (latest first)
	query = query.Order(ent.Desc(alertlogs.FieldCreatedAt))

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
	client := r.client.Querier(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "alertlogs", "count", map[string]interface{}{})
	defer FinishSpan(span)

	query := client.AlertLogs.Query().Where(
		alertlogs.TenantID(types.GetTenantID(ctx)),
		alertlogs.EnvironmentID(types.GetEnvironmentID(ctx)),
	)

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

func (r *alertLogsRepository) GetLatestByEntity(ctx context.Context, entityType types.AlertEntityType, entityID string) (*domainAlertLogs.AlertLog, error) {
	client := r.client.Querier(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "alertlogs", "get_latest_by_entity", map[string]interface{}{
		"entity_type": entityType,
		"entity_id":   entityID,
	})
	defer FinishSpan(span)

	query := client.AlertLogs.Query().Where(
		alertlogs.EntityType(string(entityType)),
		alertlogs.EntityID(entityID),
		alertlogs.TenantID(types.GetTenantID(ctx)),
		alertlogs.EnvironmentID(types.GetEnvironmentID(ctx)),
	)

	// Order by creation time descending to get the latest
	query = query.Order(ent.Desc(alertlogs.FieldCreatedAt)).Limit(1)

	alertLog, err := query.First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			// No alert logs found for this entity - this is not an error
			SetSpanSuccess(span)
			return nil, nil
		}
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to get latest alert log for entity").
			WithReportableDetails(map[string]any{
				"entity_type": entityType,
				"entity_id":   entityID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainAlertLogs.FromEnt(alertLog), nil
}

func (r *alertLogsRepository) GetLatestByEntityAndAlertType(ctx context.Context, entityType types.AlertEntityType, entityID string, alertType types.AlertType) (*domainAlertLogs.AlertLog, error) {
	client := r.client.Querier(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "alertlogs", "get_latest_by_entity_and_alert_type", map[string]interface{}{
		"entity_type": entityType,
		"entity_id":   entityID,
		"alert_type":  alertType,
	})
	defer FinishSpan(span)

	query := client.AlertLogs.Query().Where(
		alertlogs.EntityType(string(entityType)),
		alertlogs.EntityID(entityID),
		alertlogs.AlertType(string(alertType)),
		alertlogs.TenantID(types.GetTenantID(ctx)),
		alertlogs.EnvironmentID(types.GetEnvironmentID(ctx)),
	)

	// Order by creation time descending to get the latest
	query = query.Order(ent.Desc(alertlogs.FieldCreatedAt)).Limit(1)

	alertLog, err := query.First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			// No alert logs found for this entity and alert type - this is not an error
			SetSpanSuccess(span)
			return nil, nil
		}
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to get latest alert log for entity and alert type").
			WithReportableDetails(map[string]any{
				"entity_type": entityType,
				"entity_id":   entityID,
				"alert_type":  alertType,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainAlertLogs.FromEnt(alertLog), nil
}

func (r *alertLogsRepository) ListByEntity(ctx context.Context, entityType types.AlertEntityType, entityID string, limit int) ([]*domainAlertLogs.AlertLog, error) {
	client := r.client.Querier(ctx)

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
	client := r.client.Querier(ctx)

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
