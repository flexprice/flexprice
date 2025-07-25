package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	entStripeTenantConfig "github.com/flexprice/flexprice/ent/stripetenantconfig"
	"github.com/flexprice/flexprice/internal/cache"
	domainIntegration "github.com/flexprice/flexprice/internal/domain/integration"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type stripeTenantConfigRepository struct {
	client postgres.IClient
	log    *logger.Logger
	cache  cache.Cache
}

func NewStripeTenantConfigRepository(client postgres.IClient, log *logger.Logger, cache cache.Cache) domainIntegration.StripeTenantConfigRepository {
	return &stripeTenantConfigRepository{
		client: client,
		log:    log,
		cache:  cache,
	}
}

// Basic CRUD operations - placeholder implementations until Ent schemas are generated
func (r *stripeTenantConfigRepository) Create(ctx context.Context, config *domainIntegration.StripeTenantConfig) error {
	span := StartRepositorySpan(ctx, "stripe_tenant_config", "create", map[string]interface{}{
		"config_id":      config.ID,
		"tenant_id":      config.TenantID,
		"environment_id": config.EnvironmentID,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)

	// Persist entity
	_, err := client.StripeTenantConfig.Create().
		SetID(config.ID).
		SetTenantID(config.TenantID).
		SetStatus(string(config.Status)).
		SetEnvironmentID(config.EnvironmentID).
		SetAPIKeyEncrypted(config.APIKeyEncrypted).
		SetSyncEnabled(config.SyncEnabled).
		SetAggregationWindowMinutes(config.AggregationWindowMinutes).
		SetWebhookConfig(config.WebhookConfig).
		SetCreatedAt(config.CreatedAt).
		SetUpdatedAt(config.UpdatedAt).
		SetCreatedBy(config.CreatedBy).
		SetUpdatedBy(config.UpdatedBy).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to create Stripe tenant config").
			WithReportableDetails(map[string]any{
				"config_id":      config.ID,
				"tenant_id":      config.TenantID,
				"environment_id": config.EnvironmentID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

func (r *stripeTenantConfigRepository) Get(ctx context.Context, id string) (*domainIntegration.StripeTenantConfig, error) {
	span := StartRepositorySpan(ctx, "stripe_tenant_config", "get", map[string]interface{}{
		"config_id": id,
		"tenant_id": types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)

	stc, err := client.StripeTenantConfig.Query().
		Where(
			entStripeTenantConfig.ID(id),
			entStripeTenantConfig.TenantID(types.GetTenantID(ctx)),
			entStripeTenantConfig.EnvironmentID(types.GetEnvironmentID(ctx)),
		).Only(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Stripe tenant config with ID %s not found for tenant %s and environment %s", id, types.GetTenantID(ctx), types.GetEnvironmentID(ctx)).
				WithReportableDetails(map[string]any{
					"config_id":      id,
					"tenant_id":      types.GetTenantID(ctx),
					"environment_id": types.GetEnvironmentID(ctx),
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get Stripe tenant config").
			WithReportableDetails(map[string]any{
				"config_id":      id,
				"tenant_id":      types.GetTenantID(ctx),
				"environment_id": types.GetEnvironmentID(ctx),
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return toDomainStripeTenantConfig(stc), nil
}

func (r *stripeTenantConfigRepository) List(ctx context.Context, filter *domainIntegration.StripeTenantConfigFilter) ([]*domainIntegration.StripeTenantConfig, error) {
	span := StartRepositorySpan(ctx, "stripe_tenant_config", "list", map[string]interface{}{
		"tenant_id": types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	if filter == nil {
		filter = &domainIntegration.StripeTenantConfigFilter{}
	}

	client := r.client.Querier(ctx)
	query := client.StripeTenantConfig.Query().
		Where(
			entStripeTenantConfig.TenantID(types.GetTenantID(ctx)),
			entStripeTenantConfig.EnvironmentID(types.GetEnvironmentID(ctx)),
		)

	if filter.SyncEnabled != nil {
		query = query.Where(entStripeTenantConfig.SyncEnabled(*filter.SyncEnabled))
	}

	// Pagination & Sorting using embedded QueryFilter methods
	if !filter.IsUnlimited() {
		query = query.Limit(filter.GetLimit()).Offset(filter.GetOffset())
	}
	// Sorting
	orderField := filter.GetSort()
	order := filter.GetOrder()
	if orderField == "" {
		orderField = "created_at"
	}
	switch orderField {
	case "created_at":
		if order == "asc" {
			query = query.Order(ent.Asc(entStripeTenantConfig.FieldCreatedAt))
		} else {
			query = query.Order(ent.Desc(entStripeTenantConfig.FieldCreatedAt))
		}
	case "updated_at":
		if order == "asc" {
			query = query.Order(ent.Asc(entStripeTenantConfig.FieldUpdatedAt))
		} else {
			query = query.Order(ent.Desc(entStripeTenantConfig.FieldUpdatedAt))
		}
	default:
		// default created_at desc
		query = query.Order(ent.Desc(entStripeTenantConfig.FieldCreatedAt))
	}

	stcs, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list Stripe tenant configs").
			WithReportableDetails(map[string]any{
				"tenant_id":      types.GetTenantID(ctx),
				"environment_id": types.GetEnvironmentID(ctx),
			}).
			Mark(ierr.ErrDatabase)
	}

	configs := make([]*domainIntegration.StripeTenantConfig, 0, len(stcs))
	for _, s := range stcs {
		configs = append(configs, toDomainStripeTenantConfig(s))
	}
	SetSpanSuccess(span)
	return configs, nil
}

func (r *stripeTenantConfigRepository) Count(ctx context.Context, filter *domainIntegration.StripeTenantConfigFilter) (int, error) {
	span := StartRepositorySpan(ctx, "stripe_tenant_config", "count", map[string]interface{}{
		"tenant_id": types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	if filter == nil {
		filter = &domainIntegration.StripeTenantConfigFilter{}
	}

	client := r.client.Querier(ctx)
	query := client.StripeTenantConfig.Query().
		Where(
			entStripeTenantConfig.TenantID(types.GetTenantID(ctx)),
			entStripeTenantConfig.EnvironmentID(types.GetEnvironmentID(ctx)),
		)
	if filter.SyncEnabled != nil {
		query = query.Where(entStripeTenantConfig.SyncEnabled(*filter.SyncEnabled))
	}

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count Stripe tenant configs").
			WithReportableDetails(map[string]any{
				"tenant_id":      types.GetTenantID(ctx),
				"environment_id": types.GetEnvironmentID(ctx),
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return count, nil
}

func (r *stripeTenantConfigRepository) Update(ctx context.Context, config *domainIntegration.StripeTenantConfig) error {
	span := StartRepositorySpan(ctx, "stripe_tenant_config", "update", map[string]interface{}{
		"config_id": config.ID,
		"tenant_id": config.TenantID,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)

	_, err := client.StripeTenantConfig.Update().
		Where(
			entStripeTenantConfig.ID(config.ID),
			entStripeTenantConfig.TenantID(types.GetTenantID(ctx)),
			entStripeTenantConfig.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetAPIKeyEncrypted(config.APIKeyEncrypted).
		SetSyncEnabled(config.SyncEnabled).
		SetAggregationWindowMinutes(config.AggregationWindowMinutes).
		SetWebhookConfig(config.WebhookConfig).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Stripe tenant config with ID %s not found for tenant %s and environment %s", config.ID, types.GetTenantID(ctx), types.GetEnvironmentID(ctx)).
				WithReportableDetails(map[string]any{
					"config_id":      config.ID,
					"tenant_id":      types.GetTenantID(ctx),
					"environment_id": types.GetEnvironmentID(ctx),
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update Stripe tenant config").
			WithReportableDetails(map[string]any{
				"config_id":      config.ID,
				"tenant_id":      types.GetTenantID(ctx),
				"environment_id": types.GetEnvironmentID(ctx),
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

func (r *stripeTenantConfigRepository) Delete(ctx context.Context, config *domainIntegration.StripeTenantConfig) error {
	span := StartRepositorySpan(ctx, "stripe_tenant_config", "delete", map[string]interface{}{
		"config_id": config.ID,
		"tenant_id": config.TenantID,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)

	_, err := client.StripeTenantConfig.Update().
		Where(
			entStripeTenantConfig.ID(config.ID),
			entStripeTenantConfig.TenantID(types.GetTenantID(ctx)),
			entStripeTenantConfig.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to delete Stripe tenant config").
			WithReportableDetails(map[string]any{
				"config_id":      config.ID,
				"tenant_id":      config.TenantID,
				"environment_id": config.EnvironmentID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

// Config-specific methods
func (r *stripeTenantConfigRepository) GetByTenantAndEnvironment(ctx context.Context, tenantID, environmentID string) (*domainIntegration.StripeTenantConfig, error) {
	span := StartRepositorySpan(ctx, "stripe_tenant_config", "get_by_tenant_env", map[string]interface{}{
		"tenant_id":      tenantID,
		"environment_id": environmentID,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)

	stc, err := client.StripeTenantConfig.Query().
		Where(
			entStripeTenantConfig.TenantID(tenantID),
			entStripeTenantConfig.EnvironmentID(environmentID),
			entStripeTenantConfig.StatusEQ(string(types.StatusPublished)),
		).Only(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Stripe tenant config not found for tenant %s and environment %s", tenantID, environmentID).
				WithReportableDetails(map[string]any{
					"tenant_id":      tenantID,
					"environment_id": environmentID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get Stripe tenant config by tenant and environment").
			WithReportableDetails(map[string]any{
				"tenant_id":      tenantID,
				"environment_id": environmentID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return toDomainStripeTenantConfig(stc), nil
}

func (r *stripeTenantConfigRepository) ListActiveTenants(ctx context.Context, filter *domainIntegration.StripeTenantConfigFilter) ([]*domainIntegration.StripeTenantConfig, error) {
	span := StartRepositorySpan(ctx, "stripe_tenant_config", "list_active_tenants", nil)
	defer FinishSpan(span)

	if filter == nil {
		filter = &domainIntegration.StripeTenantConfigFilter{}
	}

	client := r.client.Querier(ctx)
	query := client.StripeTenantConfig.Query().
		Where(
			entStripeTenantConfig.SyncEnabled(true),
			entStripeTenantConfig.APIKeyEncryptedNEQ(""),
			entStripeTenantConfig.StatusEQ(string(types.StatusPublished)),
		)
	if filter.SyncEnabled != nil {
		query = query.Where(entStripeTenantConfig.SyncEnabled(*filter.SyncEnabled))
	}

	stcs, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list active Stripe tenant configs").
			WithReportableDetails(map[string]any{
				"filter": filter,
			}).
			Mark(ierr.ErrDatabase)
	}

	configs := make([]*domainIntegration.StripeTenantConfig, 0, len(stcs))
	for _, s := range stcs {
		configs = append(configs, toDomainStripeTenantConfig(s))
	}

	SetSpanSuccess(span)
	return configs, nil
}

// toDomainStripeTenantConfig converts ent model to domain model
func toDomainStripeTenantConfig(stc *ent.StripeTenantConfig) *domainIntegration.StripeTenantConfig {
	if stc == nil {
		return nil
	}
	return &domainIntegration.StripeTenantConfig{
		ID:                       stc.ID,
		APIKeyEncrypted:          stc.APIKeyEncrypted,
		SyncEnabled:              stc.SyncEnabled,
		AggregationWindowMinutes: stc.AggregationWindowMinutes,
		WebhookConfig:            stc.WebhookConfig,
		EnvironmentID:            stc.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  stc.TenantID,
			Status:    types.Status(stc.Status),
			CreatedAt: stc.CreatedAt,
			UpdatedAt: stc.UpdatedAt,
			CreatedBy: stc.CreatedBy,
			UpdatedBy: stc.UpdatedBy,
		},
	}
}
