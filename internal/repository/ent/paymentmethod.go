package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	entpaymentmethod "github.com/flexprice/flexprice/ent/paymentmethod"
	"github.com/flexprice/flexprice/internal/cache"
	domainPaymentMethod "github.com/flexprice/flexprice/internal/domain/paymentmethod"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type paymentMethodRepository struct {
	client     postgres.IClient
	log        *logger.Logger
	queryOpts  PaymentMethodQueryOptions
	redisCache cache.RedisCache
}

func NewPaymentMethodRepository(client postgres.IClient, log *logger.Logger, redisCache cache.RedisCache) domainPaymentMethod.Repository {
	return &paymentMethodRepository{
		client:     client,
		log:        log,
		queryOpts:  PaymentMethodQueryOptions{},
		redisCache: redisCache,
	}
}

func (r *paymentMethodRepository) Create(ctx context.Context, pm *domainPaymentMethod.PaymentMethod) error {
	client := r.client.Writer(ctx)

	r.log.Debug(ctx, "creating payment method",
		"payment_method_id", pm.ID,
		"customer_id", pm.CustomerID,
		"gateway", pm.Gateway,
		"type", pm.Type,
	)

	span := StartRepositorySpan(ctx, "payment_method", "create", map[string]interface{}{
		"payment_method_id": pm.ID,
		"customer_id":       pm.CustomerID,
		"gateway":           pm.Gateway,
	})
	defer FinishSpan(span)

	if pm.EnvironmentID == "" {
		pm.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	result, err := client.PaymentMethod.Create().
		SetID(pm.ID).
		SetCustomerID(pm.CustomerID).
		SetType(pm.Type).
		SetGateway(pm.Gateway).
		SetGatewayMethodID(pm.GatewayMethodID).
		SetPaymentMethodStatus(pm.PaymentMethodStatus).
		SetIsDefault(pm.IsDefault).
		SetMethodDetails(pm.MethodDetails).
		SetTenantID(pm.TenantID).
		SetStatus(string(pm.Status)).
		SetCreatedAt(pm.CreatedAt).
		SetUpdatedAt(pm.UpdatedAt).
		SetCreatedBy(pm.CreatedBy).
		SetUpdatedBy(pm.UpdatedBy).
		SetEnvironmentID(pm.EnvironmentID).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to create payment method").
			WithReportableDetails(map[string]interface{}{
				"payment_method_id": pm.ID,
				"customer_id":       pm.CustomerID,
				"gateway":           pm.Gateway,
			}).
			Mark(ierr.ErrDatabase)
	}

	*pm = *domainPaymentMethod.FromEnt(result)
	return nil
}

func (r *paymentMethodRepository) Get(ctx context.Context, id string) (*domainPaymentMethod.PaymentMethod, error) {
	span := StartRepositorySpan(ctx, "payment_method", "get", map[string]interface{}{
		"payment_method_id": id,
		"tenant_id":         types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	if cached := r.getCache(ctx, id); cached != nil {
		return cached, nil
	}

	client := r.client.Reader(ctx)

	r.log.Debug(ctx, "getting payment method",
		"payment_method_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	pm, err := client.PaymentMethod.Query().
		Where(
			entpaymentmethod.ID(id),
			entpaymentmethod.TenantID(types.GetTenantID(ctx)),
			entpaymentmethod.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Payment method not found").
				WithReportableDetails(map[string]interface{}{
					"payment_method_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get payment method").
			WithReportableDetails(map[string]interface{}{
				"payment_method_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	result := domainPaymentMethod.FromEnt(pm)
	r.setCache(ctx, result)
	return result, nil
}

func (r *paymentMethodRepository) Update(ctx context.Context, pm *domainPaymentMethod.PaymentMethod) error {
	client := r.client.Writer(ctx)

	r.log.Debug(ctx, "updating payment method",
		"payment_method_id", pm.ID,
		"tenant_id", pm.TenantID,
	)

	span := StartRepositorySpan(ctx, "payment_method", "update", map[string]interface{}{
		"payment_method_id": pm.ID,
		"tenant_id":         pm.TenantID,
	})
	defer FinishSpan(span)

	_, err := client.PaymentMethod.Update().
		Where(
			entpaymentmethod.ID(pm.ID),
			entpaymentmethod.TenantID(pm.TenantID),
			entpaymentmethod.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetPaymentMethodStatus(pm.PaymentMethodStatus).
		SetIsDefault(pm.IsDefault).
		SetMethodDetails(pm.MethodDetails).
		SetStatus(string(pm.Status)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(pm.UpdatedBy).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Payment method not found").
				WithReportableDetails(map[string]interface{}{
					"payment_method_id": pm.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update payment method").
			WithReportableDetails(map[string]interface{}{
				"payment_method_id": pm.ID,
			}).
			Mark(ierr.ErrDatabase)
	}

	r.deleteCache(ctx, pm.ID)
	return nil
}

func (r *paymentMethodRepository) Delete(ctx context.Context, id string) error {
	span := StartRepositorySpan(ctx, "payment_method", "delete", map[string]interface{}{
		"payment_method_id": id,
		"tenant_id":         types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	client := r.client.Writer(ctx)

	r.log.Debug(ctx, "deleting payment method",
		"payment_method_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	_, err := client.PaymentMethod.Update().
		Where(
			entpaymentmethod.ID(id),
			entpaymentmethod.TenantID(types.GetTenantID(ctx)),
			entpaymentmethod.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetStatus(string(types.StatusDeleted)).
		SetUpdatedAt(time.Now().UTC()).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Payment method not found").
				WithReportableDetails(map[string]interface{}{
					"payment_method_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete payment method").
			WithReportableDetails(map[string]interface{}{
				"payment_method_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	r.deleteCache(ctx, id)
	return nil
}

func (r *paymentMethodRepository) List(ctx context.Context, filter *types.PaymentMethodFilter) ([]*domainPaymentMethod.PaymentMethod, error) {
	if filter == nil {
		filter = &types.PaymentMethodFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	client := r.client.Reader(ctx)

	span := StartRepositorySpan(ctx, "payment_method", "list", map[string]interface{}{
		"tenant_id": types.GetTenantID(ctx),
		"filter":    filter,
	})
	defer FinishSpan(span)

	query := client.PaymentMethod.Query().
		Order(ent.Desc(entpaymentmethod.FieldCreatedAt))
	query = r.queryOpts.applyEntityFilters(ctx, filter, query)
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	results, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list payment methods").
			WithReportableDetails(map[string]interface{}{
				"filter": filter,
			}).
			Mark(ierr.ErrDatabase)
	}

	return domainPaymentMethod.FromEntList(results), nil
}

func (r *paymentMethodRepository) Count(ctx context.Context, filter *types.PaymentMethodFilter) (int, error) {
	client := r.client.Reader(ctx)

	span := StartRepositorySpan(ctx, "payment_method", "count", map[string]interface{}{
		"tenant_id": types.GetTenantID(ctx),
		"filter":    filter,
	})
	defer FinishSpan(span)

	query := client.PaymentMethod.Query()
	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	query = r.queryOpts.applyEntityFilters(ctx, filter, query)

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count payment methods").
			WithReportableDetails(map[string]interface{}{
				"filter": filter,
			}).
			Mark(ierr.ErrDatabase)
	}

	return count, nil
}

func (r *paymentMethodRepository) GetDefaultForCustomer(ctx context.Context, customerID, gateway string) (*domainPaymentMethod.PaymentMethod, error) {
	span := StartRepositorySpan(ctx, "payment_method", "get_default_for_customer", map[string]interface{}{
		"customer_id": customerID,
		"gateway":     gateway,
		"tenant_id":   types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)

	r.log.Debug(ctx, "getting default payment method for customer",
		"customer_id", customerID,
		"gateway", gateway,
		"tenant_id", types.GetTenantID(ctx),
	)

	pm, err := client.PaymentMethod.Query().
		Where(
			entpaymentmethod.CustomerID(customerID),
			entpaymentmethod.Gateway(types.PaymentGatewayType(gateway)),
			entpaymentmethod.TenantID(types.GetTenantID(ctx)),
			entpaymentmethod.EnvironmentID(types.GetEnvironmentID(ctx)),
			entpaymentmethod.Status(string(types.StatusPublished)),
			entpaymentmethod.PaymentMethodStatus(types.PaymentMethodStatusActive),
		).
		Order(ent.Desc(entpaymentmethod.FieldIsDefault), ent.Desc(entpaymentmethod.FieldCreatedAt)).
		First(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("No payment method found for customer").
				WithReportableDetails(map[string]interface{}{
					"customer_id": customerID,
					"gateway":     gateway,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get default payment method").
			WithReportableDetails(map[string]interface{}{
				"customer_id": customerID,
				"gateway":     gateway,
			}).
			Mark(ierr.ErrDatabase)
	}

	return domainPaymentMethod.FromEnt(pm), nil
}

// PaymentMethodQuery type alias for readability
type PaymentMethodQuery = *ent.PaymentMethodQuery

// PaymentMethodQueryOptions implements BaseQueryOptions for payment method queries
type PaymentMethodQueryOptions struct{}

func (o PaymentMethodQueryOptions) ApplyTenantFilter(ctx context.Context, query PaymentMethodQuery) PaymentMethodQuery {
	return query.Where(entpaymentmethod.TenantID(types.GetTenantID(ctx)))
}

func (o PaymentMethodQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query PaymentMethodQuery) PaymentMethodQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(entpaymentmethod.EnvironmentID(environmentID))
	}
	return query
}

func (o PaymentMethodQueryOptions) ApplyStatusFilter(query PaymentMethodQuery, status string) PaymentMethodQuery {
	if status == "" {
		return query.Where(entpaymentmethod.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(entpaymentmethod.Status(status))
}

func (o PaymentMethodQueryOptions) ApplySortFilter(query PaymentMethodQuery, field string, order string) PaymentMethodQuery {
	orderFunc := ent.Desc
	if order == "asc" {
		orderFunc = ent.Asc
	}
	return query.Order(orderFunc(o.GetFieldName(field)))
}

func (o PaymentMethodQueryOptions) ApplyPaginationFilter(query PaymentMethodQuery, limit int, offset int) PaymentMethodQuery {
	query = query.Limit(limit)
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o PaymentMethodQueryOptions) GetFieldName(field string) string {
	if entpaymentmethod.ValidColumn(field) {
		return field
	}
	return ""
}

func (o PaymentMethodQueryOptions) applyEntityFilters(_ context.Context, f *types.PaymentMethodFilter, query PaymentMethodQuery) PaymentMethodQuery {
	if f == nil {
		return query
	}
	if f.CustomerID != nil {
		query = query.Where(entpaymentmethod.CustomerID(*f.CustomerID))
	}
	if f.Gateway != nil {
		query = query.Where(entpaymentmethod.Gateway(types.PaymentGatewayType(*f.Gateway)))
	}
	if f.GatewayMethodID != nil {
		query = query.Where(entpaymentmethod.GatewayMethodID(*f.GatewayMethodID))
	}
	if f.Type != nil {
		query = query.Where(entpaymentmethod.Type(types.PaymentMethodType(*f.Type)))
	}
	if f.PaymentMethodStatus != nil {
		query = query.Where(entpaymentmethod.PaymentMethodStatus(types.PaymentMethodStatus(*f.PaymentMethodStatus)))
	}
	if f.IsDefault != nil {
		query = query.Where(entpaymentmethod.IsDefault(*f.IsDefault))
	}
	if f.TimeRangeFilter != nil {
		if f.TimeRangeFilter.StartTime != nil {
			query = query.Where(entpaymentmethod.CreatedAtGTE(*f.TimeRangeFilter.StartTime))
		}
		if f.TimeRangeFilter.EndTime != nil {
			query = query.Where(entpaymentmethod.CreatedAtLTE(*f.TimeRangeFilter.EndTime))
		}
	}
	return query
}

func (r *paymentMethodRepository) setCache(ctx context.Context, pm *domainPaymentMethod.PaymentMethod) {
	span, ctx := cache.StartRedisCacheSpan(ctx, "payment_method", "set", map[string]interface{}{
		"payment_method_id": pm.ID,
	})
	defer cache.FinishSpan(span)

	cacheKey := cache.GenerateKey(ctx, cache.PrefixPaymentMethod, pm.ID)
	r.redisCache.Set(ctx, cacheKey, pm, cache.ExpiryDefaultRedis)
}

func (r *paymentMethodRepository) getCache(ctx context.Context, id string) *domainPaymentMethod.PaymentMethod {
	span, ctx := cache.StartRedisCacheSpan(ctx, "payment_method", "get", map[string]interface{}{
		"payment_method_id": id,
	})
	defer cache.FinishSpan(span)

	cacheKey := cache.GenerateKey(ctx, cache.PrefixPaymentMethod, id)
	value, found := r.redisCache.Get(ctx, cacheKey)
	if !found {
		return nil
	}
	pm, ok := cache.UnmarshalCacheValue[domainPaymentMethod.PaymentMethod](value)
	if !ok {
		return nil
	}
	return pm
}

func (r *paymentMethodRepository) deleteCache(ctx context.Context, id string) {
	span, ctx := cache.StartRedisCacheSpan(ctx, "payment_method", "delete", map[string]interface{}{
		"payment_method_id": id,
	})
	defer cache.FinishSpan(span)

	cacheKey := cache.GenerateKey(ctx, cache.PrefixPaymentMethod, id)
	r.redisCache.Delete(ctx, cacheKey)
}
