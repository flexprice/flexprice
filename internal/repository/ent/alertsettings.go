package ent

import (
	"context"
	"errors"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/alertsettings"
	"github.com/flexprice/flexprice/ent/predicate"
	domainAlert "github.com/flexprice/flexprice/internal/domain/alert"
	"github.com/flexprice/flexprice/internal/dsl"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/lib/pq"
)

type alertSettingsRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts AlertSettingsQueryOptions
}

func NewAlertSettingsRepository(client postgres.IClient, log *logger.Logger) domainAlert.Repository {
	return &alertSettingsRepository{
		client:    client,
		log:       log,
		queryOpts: AlertSettingsQueryOptions{},
	}
}

func (r *alertSettingsRepository) Create(ctx context.Context, a *domainAlert.AlertSettings) error {
	client := r.client.Writer(ctx)

	r.log.Debug(ctx, "creating alert settings",
		"alert_settings_id", a.ID,
		"tenant_id", a.TenantID,
		"entity_type", a.EntityType,
		"entity_id", a.EntityID,
	)

	span := StartRepositorySpan(ctx, "alertsettings", "create", map[string]interface{}{
		"alert_settings_id": a.ID,
		"entity_type":       a.EntityType,
		"entity_id":         a.EntityID,
	})
	defer FinishSpan(span)

	if a.EnvironmentID == "" {
		a.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	var config types.AlertSettings
	if a.Config != nil {
		config = *a.Config
	}

	createQuery := client.AlertSettings.Create().
		SetID(a.ID).
		SetTenantID(a.TenantID).
		SetEnabled(a.Enabled).
		SetEntityType(alertsettings.EntityType(a.EntityType)).
		SetEntityID(a.EntityID).
		SetConfig(config).
		SetStatus(string(a.Status)).
		SetCreatedAt(a.CreatedAt).
		SetUpdatedAt(a.UpdatedAt).
		SetCreatedBy(a.CreatedBy).
		SetUpdatedBy(a.UpdatedBy).
		SetEnvironmentID(a.EnvironmentID)

	if a.ParentEntityType != nil {
		createQuery = createQuery.SetParentEntityType(alertsettings.ParentEntityType(*a.ParentEntityType))
	}
	if a.ParentEntityID != nil {
		createQuery = createQuery.SetParentEntityID(*a.ParentEntityID)
	}

	_, err := createQuery.Save(ctx)
	if err != nil {
		SetSpanError(span, err)

		if ent.IsConstraintError(err) {
			var pqErr *pq.Error
			if errors.As(err, &pqErr) {
				return ierr.WithError(err).
					WithHint("Failed to create alert settings due to constraint violation").
					WithReportableDetails(map[string]any{
						"entity_type": a.EntityType,
						"entity_id":   a.EntityID,
					}).
					Mark(ierr.ErrAlreadyExists)
			}
		}
		return ierr.WithError(err).
			WithHint("Failed to create alert settings").
			WithReportableDetails(map[string]any{
				"entity_type": a.EntityType,
				"entity_id":   a.EntityID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

func (r *alertSettingsRepository) Get(ctx context.Context, id string) (*domainAlert.AlertSettings, error) {
	client := r.client.Reader(ctx)

	span := StartRepositorySpan(ctx, "alertsettings", "get", map[string]interface{}{
		"alert_settings_id": id,
	})
	defer FinishSpan(span)

	entAlertSettings, err := client.AlertSettings.Query().
		Where(
			alertsettings.ID(id),
			alertsettings.TenantID(types.GetTenantID(ctx)),
			alertsettings.EnvironmentID(types.GetEnvironmentID(ctx)),
			alertsettings.StatusNotIn(string(types.StatusArchived)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Alert settings not found").
				WithReportableDetails(map[string]any{
					"alert_settings_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get alert settings").
			WithReportableDetails(map[string]any{
				"alert_settings_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainAlert.FromEnt(entAlertSettings), nil
}

func (r *alertSettingsRepository) Update(ctx context.Context, a *domainAlert.AlertSettings) error {
	client := r.client.Writer(ctx)

	span := StartRepositorySpan(ctx, "alertsettings", "update", map[string]interface{}{
		"alert_settings_id": a.ID,
	})
	defer FinishSpan(span)

	var config types.AlertSettings
	if a.Config != nil {
		config = *a.Config
	}

	_, err := client.AlertSettings.UpdateOneID(a.ID).
		Where(
			alertsettings.TenantID(types.GetTenantID(ctx)),
			alertsettings.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetEnabled(a.Enabled).
		SetConfig(config).
		SetStatus(string(a.Status)).
		SetUpdatedAt(a.UpdatedAt).
		SetUpdatedBy(a.UpdatedBy).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Alert settings not found").
				WithReportableDetails(map[string]any{
					"alert_settings_id": a.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update alert settings").
			WithReportableDetails(map[string]any{
				"alert_settings_id": a.ID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

func (r *alertSettingsRepository) Delete(ctx context.Context, id string) error {
	client := r.client.Writer(ctx)

	span := StartRepositorySpan(ctx, "alertsettings", "delete", map[string]interface{}{
		"alert_settings_id": id,
	})
	defer FinishSpan(span)

	_, err := client.AlertSettings.UpdateOneID(id).
		Where(
			alertsettings.TenantID(types.GetTenantID(ctx)),
			alertsettings.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Alert settings not found").
				WithReportableDetails(map[string]any{
					"alert_settings_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete alert settings").
			WithReportableDetails(map[string]any{
				"alert_settings_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

func (r *alertSettingsRepository) List(ctx context.Context, filter *types.AlertSettingsFilter) ([]*domainAlert.AlertSettings, error) {
	client := r.client.Reader(ctx)

	span := StartRepositorySpan(ctx, "alertsettings", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	query := client.AlertSettings.Query()
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list alert settings").
			Mark(ierr.ErrDatabase)
	}
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	entAlertSettingsList, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list alert settings").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainAlert.FromEntList(entAlertSettingsList), nil
}

func (r *alertSettingsRepository) Count(ctx context.Context, filter *types.AlertSettingsFilter) (int, error) {
	client := r.client.Reader(ctx)

	span := StartRepositorySpan(ctx, "alertsettings", "count", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	query := client.AlertSettings.Query()
	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)

	var err error
	query, err = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to apply query options").
			Mark(ierr.ErrDatabase)
	}

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count alert settings").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return count, nil
}

// AlertSettingsQuery type alias for better readability
type AlertSettingsQuery = *ent.AlertSettingsQuery

// AlertSettingsQueryOptions implements BaseQueryOptions for alert settings queries
type AlertSettingsQueryOptions struct{}

func (o AlertSettingsQueryOptions) ApplyTenantFilter(ctx context.Context, query AlertSettingsQuery) AlertSettingsQuery {
	return query.Where(alertsettings.TenantID(types.GetTenantID(ctx)))
}

func (o AlertSettingsQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query AlertSettingsQuery) AlertSettingsQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(alertsettings.EnvironmentID(environmentID))
	}
	return query
}

func (o AlertSettingsQueryOptions) ApplyStatusFilter(query AlertSettingsQuery, status string) AlertSettingsQuery {
	if status == "" {
		return query.Where(alertsettings.StatusNotIn(string(types.StatusArchived)))
	}
	return query.Where(alertsettings.Status(status))
}

func (o AlertSettingsQueryOptions) ApplySortFilter(query AlertSettingsQuery, field string, order string) AlertSettingsQuery {
	field = o.GetFieldName(field)
	if field == "" {
		return query
	}

	if order == types.OrderDesc {
		query = query.Order(ent.Desc(field))
	} else {
		query = query.Order(ent.Asc(field))
	}
	return query
}

func (o AlertSettingsQueryOptions) ApplyPaginationFilter(query AlertSettingsQuery, limit int, offset int) AlertSettingsQuery {
	return query.Offset(offset).Limit(limit)
}

// GetFieldName returns the ent field name for alert settings; delegates to ent's ValidColumn so new schema fields are supported automatically.
func (o AlertSettingsQueryOptions) GetFieldName(field string) string {
	if alertsettings.ValidColumn(field) {
		return field
	}
	return ""
}

func (o AlertSettingsQueryOptions) GetFieldResolver(field string) (string, error) {
	fieldName := o.GetFieldName(field)
	if fieldName == "" {
		return "", ierr.NewErrorf("unknown field name '%s' in alert settings query", field).
			Mark(ierr.ErrValidation)
	}
	return fieldName, nil
}

func (o AlertSettingsQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.AlertSettingsFilter, query AlertSettingsQuery) (AlertSettingsQuery, error) {
	var err error
	if f == nil {
		return query, nil
	}

	if f.EntityType != "" {
		query = query.Where(alertsettings.EntityTypeEQ(alertsettings.EntityType(f.EntityType)))
	}

	if f.EntityID != "" {
		query = query.Where(alertsettings.EntityID(f.EntityID))
	}

	if len(f.EntityIDs) > 0 {
		query = query.Where(alertsettings.EntityIDIn(f.EntityIDs...))
	}

	if f.ParentEntityType != "" {
		query = query.Where(alertsettings.ParentEntityTypeEQ(alertsettings.ParentEntityType(f.ParentEntityType)))
	}

	if f.ParentEntityID != "" {
		query = query.Where(alertsettings.ParentEntityIDEQ(f.ParentEntityID))
	}

	if len(f.ParentEntityIDs) > 0 {
		query = query.Where(alertsettings.ParentEntityIDIn(f.ParentEntityIDs...))
	}

	if f.Enabled != nil {
		query = query.Where(alertsettings.Enabled(*f.Enabled))
	}

	if f.Filters != nil {
		query, err = dsl.ApplyFilters[AlertSettingsQuery, predicate.AlertSettings](
			query,
			f.Filters,
			o.GetFieldResolver,
			func(p dsl.Predicate) predicate.AlertSettings { return predicate.AlertSettings(p) },
		)
		if err != nil {
			return nil, err
		}
	}

	if f.Sort != nil {
		query, err = dsl.ApplySorts[AlertSettingsQuery, alertsettings.OrderOption](
			query,
			f.Sort,
			o.GetFieldResolver,
			func(o dsl.OrderFunc) alertsettings.OrderOption { return alertsettings.OrderOption(o) },
		)
		if err != nil {
			return nil, err
		}
	}

	if f.TimeRangeFilter != nil {
		if f.StartTime != nil {
			query = query.Where(alertsettings.CreatedAtGTE(*f.StartTime))
		}
		if f.EndTime != nil {
			query = query.Where(alertsettings.CreatedAtLTE(*f.EndTime))
		}
	}

	return query, nil
}
