package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	entTenant "github.com/flexprice/flexprice/ent/tenant"
	"github.com/flexprice/flexprice/internal/cache"
	domainTenant "github.com/flexprice/flexprice/internal/domain/tenant"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
)

type tenantRepository struct {
	client postgres.IClient
	logger *logger.Logger
	cache  cache.Cache
}

// NewTenantRepository creates a new tenant repository
func NewTenantRepository(client postgres.IClient, logger *logger.Logger, cache cache.Cache) domainTenant.Repository {
	return &tenantRepository{
		client: client,
		logger: logger,
		cache:  cache,
	}
}

// Create creates a new tenant
func (r *tenantRepository) Create(ctx context.Context, tenant *domainTenant.Tenant) error {
	r.logger.Debug(ctx, "creating tenant", "tenant_id", tenant.ID, "name", tenant.Name)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "tenant", "create", map[string]interface{}{
		"tenant_id": tenant.ID,
		"name":      tenant.Name,
	})
	defer FinishSpan(span)

	client := r.client.Writer(ctx)
	_, err := client.Tenant.
		Create().
		SetID(tenant.ID).
		SetName(tenant.Name).
		SetStatus(string(tenant.Status)).
		SetInternalStatus(string(tenant.InternalStatus)).
		SetCreatedAt(tenant.CreatedAt).
		SetUpdatedAt(tenant.UpdatedAt).
		SetBillingDetails(tenant.BillingDetails.ToSchema()).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to create tenant").
			WithReportableDetails(map[string]interface{}{
				"tenant_id": tenant.ID,
				"name":      tenant.Name,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

// GetByID retrieves a tenant by ID.
// Cache lookup order: L1 (in-memory) → L2 (Redis) → DB.
// Both layers are populated on a DB hit; L1 is back-filled on an L2 hit.
func (r *tenantRepository) GetByID(ctx context.Context, id string) (*domainTenant.Tenant, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "tenant", "get_by_id", map[string]interface{}{
		"tenant_id": id,
	})
	defer FinishSpan(span)

	cacheKey := cache.GenerateKey(cache.PrefixTenant, id)

	// L1: in-memory (always checked regardless of cache.enabled)
	if value, found := cache.GetInMemoryCache().ForceCacheGet(ctx, cacheKey); found {
		if t, ok := value.(*domainTenant.Tenant); ok {
			return t, nil
		}
	}

	// L2: Redis (respects cache.enabled)
	if value, found := r.cache.Get(ctx, cacheKey); found {
		if t, ok := value.(*domainTenant.Tenant); ok {
			// Back-fill L1 so the next request doesn't hit Redis
			cache.GetInMemoryCache().ForceCacheSet(ctx, cacheKey, t, cache.ExpiryDefaultInMemory)
			return t, nil
		}
	}

	client := r.client.Reader(ctx)
	tenant, err := client.Tenant.
		Query().
		Where(
			entTenant.ID(id),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Tenant not found").
				WithReportableDetails(map[string]interface{}{
					"tenant_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve tenant").
			WithReportableDetails(map[string]interface{}{
				"tenant_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	tenantData := domainTenant.FromEnt(tenant)
	// Populate both layers
	cache.GetInMemoryCache().ForceCacheSet(ctx, cacheKey, tenantData, cache.ExpiryDefaultInMemory)
	r.cache.Set(ctx, cacheKey, tenantData, cache.ExpiryDefaultRedis)
	return tenantData, nil
}

// List retrieves all tenants
func (r *tenantRepository) List(ctx context.Context) ([]*domainTenant.Tenant, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "tenant", "list", map[string]interface{}{})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)
	tenants, err := client.Tenant.
		Query().
		Order(ent.Desc(entTenant.FieldCreatedAt)).
		All(ctx)

	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list tenants").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainTenant.FromEntList(tenants), nil
}

// Update implements tenant.Repository.
func (r *tenantRepository) Update(ctx context.Context, tenant *domainTenant.Tenant) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "tenant", "update", map[string]interface{}{
		"tenant_id": tenant.ID,
		"name":      tenant.Name,
	})
	defer FinishSpan(span)

	client := r.client.Writer(ctx)

	_, err := client.Tenant.
		UpdateOneID(tenant.ID).
		SetName(tenant.Name).
		SetStatus(string(tenant.Status)).
		SetInternalStatus(string(tenant.InternalStatus)).
		SetUpdatedAt(time.Now()).
		SetMetadata(tenant.Metadata).
		SetBillingDetails(tenant.BillingDetails.ToSchema()).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to update tenant").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	r.DeleteCache(ctx, tenant.ID)
	return nil
}

func (r *tenantRepository) SetCache(ctx context.Context, tenant *domainTenant.Tenant) {
	span := cache.StartCacheSpan(ctx, "tenant", "set", map[string]interface{}{
		"tenant_id": tenant.ID,
	})
	defer cache.FinishSpan(span)
	cacheKey := cache.GenerateKey(cache.PrefixTenant, tenant.ID)
	cache.GetInMemoryCache().ForceCacheSet(ctx, cacheKey, tenant, cache.ExpiryDefaultInMemory)
	r.cache.Set(ctx, cacheKey, tenant, cache.ExpiryDefaultRedis)
}

func (r *tenantRepository) GetCache(ctx context.Context, key string) *domainTenant.Tenant {
	span := cache.StartCacheSpan(ctx, "tenant", "get", map[string]interface{}{
		"tenant_id": key,
	})
	defer cache.FinishSpan(span)

	cacheKey := cache.GenerateKey(cache.PrefixTenant, key)
	// L1 first
	if value, found := cache.GetInMemoryCache().ForceCacheGet(ctx, cacheKey); found {
		if t, ok := value.(*domainTenant.Tenant); ok {
			return t
		}
	}
	// L2 fallback
	if value, found := r.cache.Get(ctx, cacheKey); found {
		if t, ok := value.(*domainTenant.Tenant); ok {
			return t
		}
	}
	return nil
}

func (r *tenantRepository) DeleteCache(ctx context.Context, key string) {
	span := cache.StartCacheSpan(ctx, "tenant", "delete", map[string]interface{}{
		"tenant_id": key,
	})
	defer cache.FinishSpan(span)

	cacheKey := cache.GenerateKey(cache.PrefixTenant, key)
	cache.GetInMemoryCache().ForceCacheDelete(ctx, cacheKey)
	r.cache.Delete(ctx, cacheKey)
}
