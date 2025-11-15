package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/couponapplication"
	"github.com/flexprice/flexprice/ent/predicate"
	"github.com/flexprice/flexprice/internal/cache"
	domainCouponApplication "github.com/flexprice/flexprice/internal/domain/coupon_application"
	"github.com/flexprice/flexprice/internal/dsl"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type couponApplicationRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts CouponApplicationQueryOptions
	cache     cache.Cache
}

func NewCouponApplicationRepository(client postgres.IClient, log *logger.Logger, cache cache.Cache) domainCouponApplication.Repository {
	return &couponApplicationRepository{
		client:    client,
		log:       log,
		queryOpts: CouponApplicationQueryOptions{},
		cache:     cache,
	}
}

func (r *couponApplicationRepository) Create(ctx context.Context, ca *domainCouponApplication.CouponApplication) error {
	client := r.client.Writer(ctx)

	r.log.Debugw("creating coupon application",
		"coupon_application_id", ca.ID,
		"coupon_id", ca.CouponID,
		"invoice_id", ca.InvoiceID,
		"tenant_id", ca.TenantID,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon_application", "create", map[string]interface{}{
		"coupon_application_id": ca.ID,
		"coupon_id":             ca.CouponID,
		"invoice_id":            ca.InvoiceID,
	})
	defer FinishSpan(span)

	// Set environment ID from context if not already set
	if ca.EnvironmentID == "" {
		ca.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	createQuery := client.CouponApplication.Create().
		SetID(ca.ID).
		SetCouponID(ca.CouponID).
		SetCouponAssociationID(ca.CouponAssociationID).
		SetInvoiceID(ca.InvoiceID).
		SetAppliedAt(ca.AppliedAt).
		SetOriginalPrice(ca.OriginalPrice).
		SetFinalPrice(ca.FinalPrice).
		SetDiscountedAmount(ca.DiscountedAmount).
		SetDiscountType(string(ca.DiscountType)).
		SetCurrency(ca.Currency).
		SetStatus(string(ca.Status)).
		SetCreatedAt(ca.CreatedAt).
		SetUpdatedAt(ca.UpdatedAt).
		SetCreatedBy(ca.CreatedBy).
		SetUpdatedBy(ca.UpdatedBy).
		SetTenantID(ca.TenantID).
		SetEnvironmentID(ca.EnvironmentID).
		SetNillableInvoiceLineItemID(ca.InvoiceLineItemID).
		SetNillableSubscriptionID(ca.SubscriptionID).
		SetNillableDiscountPercentage(ca.DiscountPercentage)

	if ca.CouponSnapshot != nil {
		createQuery = createQuery.SetCouponSnapshot(ca.CouponSnapshot)
	}
	if ca.Metadata != nil {
		createQuery = createQuery.SetMetadata(ca.Metadata)
	}

	_, err := createQuery.Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsConstraintError(err) {
			return ierr.WithError(err).
				WithHint("A coupon application with this ID already exists").
				WithReportableDetails(map[string]any{
					"coupon_application_id": ca.ID,
					"coupon_id":             ca.CouponID,
					"invoice_id":            ca.InvoiceID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to create coupon application").
			WithReportableDetails(map[string]any{
				"coupon_application_id": ca.ID,
				"coupon_id":             ca.CouponID,
				"invoice_id":            ca.InvoiceID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	// Set cache
	r.SetCache(ctx, ca)

	return nil
}

func (r *couponApplicationRepository) Get(ctx context.Context, id string) (*domainCouponApplication.CouponApplication, error) {
	client := r.client.Reader(ctx)

	r.log.Debugw("getting coupon application",
		"coupon_application_id", id)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon_application", "get", map[string]interface{}{
		"coupon_application_id": id,
	})
	defer FinishSpan(span)

	// Try to get from cache first
	if cached := r.GetCache(ctx, id); cached != nil {
		r.log.Debugw("found coupon application in cache",
			"coupon_application_id", id)
		return cached, nil
	}

	ca, err := client.CouponApplication.Query().
		Where(
			couponapplication.ID(id),
			couponapplication.TenantID(types.GetTenantID(ctx)),
			couponapplication.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		WithCoupon().
		Only(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Coupon application with ID %s was not found", id).
				WithReportableDetails(map[string]interface{}{
					"coupon_application_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHintf("Failed to get coupon application with ID %s", id).
			WithReportableDetails(map[string]interface{}{
				"coupon_application_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	// Convert to domain model
	domainCA := domainCouponApplication.FromEnt(ca)

	// Set cache
	r.SetCache(ctx, domainCA)

	return domainCA, nil
}

func (r *couponApplicationRepository) Update(ctx context.Context, ca *domainCouponApplication.CouponApplication) error {
	client := r.client.Writer(ctx)

	r.log.Debugw("updating coupon application",
		"coupon_application_id", ca.ID,
		"coupon_id", ca.CouponID,
		"invoice_id", ca.InvoiceID,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon_application", "update", map[string]interface{}{
		"coupon_application_id": ca.ID,
		"coupon_id":             ca.CouponID,
		"invoice_id":            ca.InvoiceID,
	})
	defer FinishSpan(span)

	updateQuery := client.CouponApplication.UpdateOneID(ca.ID).
		Where(
			couponapplication.TenantID(ca.TenantID),
			couponapplication.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx))

	if ca.Metadata != nil {
		updateQuery = updateQuery.SetMetadata(ca.Metadata)
	}

	_, err := updateQuery.Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Coupon application with ID %s was not found", ca.ID).
				WithReportableDetails(map[string]any{
					"coupon_application_id": ca.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update coupon application").
			WithReportableDetails(map[string]any{
				"coupon_application_id": ca.ID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	r.DeleteCache(ctx, ca)
	return nil
}

func (r *couponApplicationRepository) Delete(ctx context.Context, id string) error {
	client := r.client.Writer(ctx)

	r.log.Debugw("deleting coupon application",
		"coupon_application_id", id,
		"tenant_id", types.GetTenantID(ctx))

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon_application", "delete", map[string]interface{}{
		"coupon_application_id": id,
	})
	defer FinishSpan(span)

	_, err := client.CouponApplication.Update().
		Where(
			couponapplication.ID(id),
			couponapplication.TenantID(types.GetTenantID(ctx)),
			couponapplication.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Coupon application with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"coupon_application_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete coupon application").
			WithReportableDetails(map[string]any{
				"coupon_application_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	r.DeleteCache(ctx, &domainCouponApplication.CouponApplication{ID: id})
	return nil
}

// List retrieves coupon applications based on the provided filter
func (r *couponApplicationRepository) List(ctx context.Context, filter *types.CouponApplicationFilter) ([]*domainCouponApplication.CouponApplication, error) {
	if filter == nil {
		filter = types.NewCouponApplicationFilter()
	}

	client := r.client.Reader(ctx)

	r.log.Debugw("listing coupon applications", "filter", filter)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon_application", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	if err := filter.Validate(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Invalid coupon application filter").
			Mark(ierr.ErrValidation)
	}

	query := client.CouponApplication.Query()

	// Apply entity-specific filters
	var err error
	query, err = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to apply query options").
			Mark(ierr.ErrDatabase)
	}

	// Apply common query options (tenant, environment, status, pagination, sorting)
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	// Always load coupon relation
	query = query.WithCoupon()

	applications, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list coupon applications from database").
			WithReportableDetails(map[string]interface{}{
				"filter": filter,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)

	// Convert to domain models
	return domainCouponApplication.FromEntList(applications), nil
}

// Count retrieves the count of coupon applications based on the provided filter
func (r *couponApplicationRepository) Count(ctx context.Context, filter *types.CouponApplicationFilter) (int, error) {
	if filter == nil {
		filter = types.NewCouponApplicationFilter()
	}

	client := r.client.Reader(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon_application", "count", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	if err := filter.Validate(); err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Invalid coupon application filter").
			Mark(ierr.ErrValidation)
	}

	query := client.CouponApplication.Query()
	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)

	// Apply entity-specific filters
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
			WithHint("Failed to count coupon applications").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return count, nil
}

// CouponApplicationQueryOptions holds query options for coupon application operations
type CouponApplicationQuery = *ent.CouponApplicationQuery

type CouponApplicationQueryOptions struct{}

// ApplyTenantFilter applies tenant filter to the query
func (o CouponApplicationQueryOptions) ApplyTenantFilter(ctx context.Context, query CouponApplicationQuery) CouponApplicationQuery {
	return query.Where(couponapplication.TenantIDEQ(types.GetTenantID(ctx)))
}

// ApplyEnvironmentFilter applies environment filter to the query
func (o CouponApplicationQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query CouponApplicationQuery) CouponApplicationQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(couponapplication.EnvironmentIDEQ(environmentID))
	}
	return query
}

// ApplyStatusFilter applies status filter to the query
func (o CouponApplicationQueryOptions) ApplyStatusFilter(query CouponApplicationQuery, status string) CouponApplicationQuery {
	if status == "" {
		return query.Where(couponapplication.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(couponapplication.Status(status))
}

// ApplySortFilter applies sorting to the query
func (o CouponApplicationQueryOptions) ApplySortFilter(query CouponApplicationQuery, field string, order string) CouponApplicationQuery {
	if field != "" {
		fieldName := o.GetFieldName(field)
		if fieldName != "" {
			if order == types.OrderDesc {
				query = query.Order(ent.Desc(fieldName))
			} else {
				query = query.Order(ent.Asc(fieldName))
			}
		}
	}
	return query
}

// ApplyPaginationFilter applies pagination to the query
func (o CouponApplicationQueryOptions) ApplyPaginationFilter(query CouponApplicationQuery, limit int, offset int) CouponApplicationQuery {
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

// GetFieldName returns the ent field name for a given field
func (o CouponApplicationQueryOptions) GetFieldName(field string) string {
	switch field {
	case "created_at":
		return couponapplication.FieldCreatedAt
	case "updated_at":
		return couponapplication.FieldUpdatedAt
	case "applied_at":
		return couponapplication.FieldAppliedAt
	case "discounted_amount":
		return couponapplication.FieldDiscountedAmount
	case "final_price":
		return couponapplication.FieldFinalPrice
	case "original_price":
		return couponapplication.FieldOriginalPrice
	case "status":
		return couponapplication.FieldStatus
	default:
		// unknown field
		return ""
	}
}

// GetFieldResolver returns the ent field name for a given field with error handling
func (o CouponApplicationQueryOptions) GetFieldResolver(field string) (string, error) {
	fieldName := o.GetFieldName(field)
	if fieldName == "" {
		return "", ierr.NewErrorf("unknown field name '%s' in coupon application query", field).
			WithHintf("Unknown field name '%s' in coupon application query", field).
			Mark(ierr.ErrValidation)
	}
	return fieldName, nil
}

// applyEntityQueryOptions applies entity-specific filters from CouponApplicationFilter
func (o CouponApplicationQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.CouponApplicationFilter, query CouponApplicationQuery) (CouponApplicationQuery, error) {
	var err error
	if f == nil {
		return query, nil
	}

	// Apply invoice ID filters
	if len(f.InvoiceIDs) > 0 {
		query = query.Where(couponapplication.InvoiceIDIn(f.InvoiceIDs...))
	}

	// Apply subscription ID filters
	if len(f.SubscriptionIDs) > 0 {
		query = query.Where(couponapplication.SubscriptionIDIn(f.SubscriptionIDs...))
	}

	// Apply coupon ID filters
	if len(f.CouponIDs) > 0 {
		query = query.Where(couponapplication.CouponIDIn(f.CouponIDs...))
	}

	// Apply invoice line item ID filters
	if len(f.InvoiceLineItemIDs) > 0 {
		query = query.Where(couponapplication.InvoiceLineItemIDIn(f.InvoiceLineItemIDs...))
	}

	// Apply coupon association ID filters
	if len(f.CouponAssociationIDs) > 0 {
		query = query.Where(couponapplication.CouponAssociationIDIn(f.CouponAssociationIDs...))
	}

	// Apply filters using the generic function
	if f.Filters != nil {
		query, err = dsl.ApplyFilters[CouponApplicationQuery, predicate.CouponApplication](
			query,
			f.Filters,
			o.GetFieldResolver,
			func(p dsl.Predicate) predicate.CouponApplication { return predicate.CouponApplication(p) },
		)
		if err != nil {
			return nil, err
		}
	}

	// Apply sorts using the generic function
	if f.Sort != nil {
		query, err = dsl.ApplySorts[CouponApplicationQuery, couponapplication.OrderOption](
			query,
			f.Sort,
			o.GetFieldResolver,
			func(o dsl.OrderFunc) couponapplication.OrderOption { return couponapplication.OrderOption(o) },
		)
		if err != nil {
			return nil, err
		}
	}

	return query, nil
}

// Cache methods
func (r *couponApplicationRepository) SetCache(ctx context.Context, ca *domainCouponApplication.CouponApplication) {
	span := cache.StartCacheSpan(ctx, "coupon_application", "set", map[string]interface{}{
		"coupon_application_id": ca.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixCouponApplication, tenantID, environmentID, ca.ID)
	r.cache.Set(ctx, cacheKey, ca, cache.ExpiryDefaultInMemory)
}

func (r *couponApplicationRepository) GetCache(ctx context.Context, key string) *domainCouponApplication.CouponApplication {
	span := cache.StartCacheSpan(ctx, "coupon_application", "get", map[string]interface{}{
		"coupon_application_id": key,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixCouponApplication, tenantID, environmentID, key)
	if value, found := r.cache.Get(ctx, cacheKey); found {
		return value.(*domainCouponApplication.CouponApplication)
	}
	return nil
}

func (r *couponApplicationRepository) DeleteCache(ctx context.Context, ca *domainCouponApplication.CouponApplication) {
	span := cache.StartCacheSpan(ctx, "coupon_application", "delete", map[string]interface{}{
		"coupon_application_id": ca.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixCouponApplication, tenantID, environmentID, ca.ID)
	r.cache.Delete(ctx, cacheKey)
}
