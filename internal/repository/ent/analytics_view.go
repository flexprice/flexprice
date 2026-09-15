package ent

import (
	"context"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/analyticsview"
	domainAnalytics "github.com/flexprice/flexprice/internal/domain/analytics"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type analyticsViewRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

// NewAnalyticsViewRepository creates a new ent-backed analytics.Repository.
func NewAnalyticsViewRepository(client postgres.IClient, log *logger.Logger) domainAnalytics.Repository {
	return &analyticsViewRepository{client: client, log: log}
}

// viewFromEnt maps an ent AnalyticsView row into the domain View. The ent→domain
// mapping lives in the repository layer so the domain package stays ent-free;
// ent owns the jsonb (un)marshal of the typed definition field.
func viewFromEnt(e *ent.AnalyticsView) *domainAnalytics.View {
	if e == nil {
		return nil
	}
	return &domainAnalytics.View{
		ID:            e.ID,
		Name:          e.Name,
		Version:       e.Version,
		Definition:    &e.Definition,
		EnvironmentID: e.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
		},
	}
}

func (r *analyticsViewRepository) Create(ctx context.Context, v *domainAnalytics.View) error {
	if v.Definition == nil {
		return ierr.NewError("view definition is required").Mark(ierr.ErrValidation)
	}

	span := StartRepositorySpan(ctx, "analytics_view", "create", map[string]interface{}{
		"view_id":   v.ID,
		"name":      v.Name,
		"tenant_id": types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	client := r.client.Writer(ctx)

	r.log.Debug(ctx, "creating analytics view",
		"view_id", v.ID,
		"tenant_id", types.GetTenantID(ctx),
		"name", v.Name,
	)

	// tenant_id and environment_id are always sourced from ctx (RLS), never the
	// domain object. Version is normalized to 1 when unset so a view never
	// persists with version 0 even if a caller bypasses service.CreateView.
	version := v.Version
	if version == 0 {
		version = 1
	}
	created, err := client.AnalyticsView.Create().
		SetID(v.ID).
		SetTenantID(types.GetTenantID(ctx)).
		SetEnvironmentID(types.GetEnvironmentID(ctx)).
		SetName(v.Name).
		SetVersion(version).
		SetDefinition(*v.Definition).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsConstraintError(err) {
			return ierr.WithError(err).
				WithHint("An analytics view with this ID already exists").
				WithReportableDetails(map[string]any{
					"view_id": v.ID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to create analytics view").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	*v = *viewFromEnt(created)
	return nil
}

func (r *analyticsViewRepository) Get(ctx context.Context, id string) (*domainAnalytics.View, error) {
	span := StartRepositorySpan(ctx, "analytics_view", "get", map[string]interface{}{
		"view_id": id,
	})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)

	r.log.Debug(ctx, "getting analytics view", "view_id", id)

	v, err := client.AnalyticsView.Query().
		Where(
			analyticsview.ID(id),
			analyticsview.TenantID(types.GetTenantID(ctx)),
			analyticsview.EnvironmentID(types.GetEnvironmentID(ctx)),
			analyticsview.Status(string(types.StatusPublished)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("analytics view with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"view_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get analytics view").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return viewFromEnt(v), nil
}

func (r *analyticsViewRepository) List(ctx context.Context) ([]*domainAnalytics.View, error) {
	span := StartRepositorySpan(ctx, "analytics_view", "list", map[string]interface{}{
		"tenant_id": types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)

	r.log.Debug(ctx, "listing analytics views",
		"tenant_id", types.GetTenantID(ctx),
		"environment_id", types.GetEnvironmentID(ctx),
	)

	views, err := client.AnalyticsView.Query().
		Where(
			analyticsview.TenantID(types.GetTenantID(ctx)),
			analyticsview.EnvironmentID(types.GetEnvironmentID(ctx)),
			analyticsview.Status(string(types.StatusPublished)),
		).
		All(ctx)

	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list analytics views").
			Mark(ierr.ErrDatabase)
	}

	result := make([]*domainAnalytics.View, 0, len(views))
	for _, v := range views {
		result = append(result, viewFromEnt(v))
	}
	SetSpanSuccess(span)
	return result, nil
}
