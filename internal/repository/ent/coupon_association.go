package ent

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/couponassociation"
	"github.com/flexprice/flexprice/internal/cache"
	domainCouponAssociation "github.com/flexprice/flexprice/internal/domain/coupon_association"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

// CouponAssociationQueryOptions implements BaseQueryOptions for coupon association queries
type CouponAssociationQueryOptions struct{}

func (o CouponAssociationQueryOptions) ApplyTenantFilter(ctx context.Context, query *ent.CouponAssociationQuery) *ent.CouponAssociationQuery {
	return query.Where(couponassociation.TenantID(types.GetTenantID(ctx)))
}

func (o CouponAssociationQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query *ent.CouponAssociationQuery) *ent.CouponAssociationQuery {
	return query.Where(couponassociation.EnvironmentID(types.GetEnvironmentID(ctx)))
}

func (o CouponAssociationQueryOptions) ApplyStatusFilter(query *ent.CouponAssociationQuery, status string) *ent.CouponAssociationQuery {
	if status == "" {
		return query.Where(couponassociation.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(couponassociation.Status(status))
}

func (o CouponAssociationQueryOptions) ApplySortFilter(query *ent.CouponAssociationQuery, field string, order string) *ent.CouponAssociationQuery {
	orderFunc := ent.Desc
	if order == "asc" {
		orderFunc = ent.Asc
	}
	return query.Order(orderFunc(o.GetFieldName(field)))
}

func (o CouponAssociationQueryOptions) ApplyPaginationFilter(query *ent.CouponAssociationQuery, limit int, offset int) *ent.CouponAssociationQuery {
	query = query.Limit(limit)
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o CouponAssociationQueryOptions) GetFieldName(field string) string {
	switch field {
	case "created_at":
		return couponassociation.FieldCreatedAt
	case "updated_at":
		return couponassociation.FieldUpdatedAt
	case "start_date":
		return couponassociation.FieldStartDate
	case "end_date":
		return couponassociation.FieldEndDate
	default:
		return field
	}
}

// applyEntityQueryOptions applies entity-specific filters from CouponAssociationFilter
func (o CouponAssociationQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.CouponAssociationFilter, query *ent.CouponAssociationQuery) (*ent.CouponAssociationQuery, error) {
	if f == nil {
		return query, nil
	}

	// Apply subscription ID filters (plural handles both single and multiple values)
	if len(f.SubscriptionIDs) > 0 {
		if len(f.SubscriptionIDs) == 1 {
			query = query.Where(couponassociation.SubscriptionID(f.SubscriptionIDs[0]))
		} else {
			query = query.Where(couponassociation.SubscriptionIDIn(f.SubscriptionIDs...))
		}
	}

	// Apply coupon ID filters (plural handles both single and multiple values)
	if len(f.CouponIDs) > 0 {
		if len(f.CouponIDs) == 1 {
			query = query.Where(couponassociation.CouponID(f.CouponIDs[0]))
		} else {
			query = query.Where(couponassociation.CouponIDIn(f.CouponIDs...))
		}
	}

	// Apply subscription line item ID filters
	// Priority: SubscriptionLineItemIDIsNil > SubscriptionLineItemIDs
	if f.SubscriptionLineItemIDIsNil != nil {
		if *f.SubscriptionLineItemIDIsNil {
			query = query.Where(couponassociation.SubscriptionLineItemIDIsNil())
		} else {
			query = query.Where(couponassociation.SubscriptionLineItemIDNotNil())
		}
	} else if len(f.SubscriptionLineItemIDs) > 0 {
		if len(f.SubscriptionLineItemIDs) == 1 {
			query = query.Where(couponassociation.SubscriptionLineItemID(f.SubscriptionLineItemIDs[0]))
		} else {
			query = query.Where(couponassociation.SubscriptionLineItemIDIn(f.SubscriptionLineItemIDs...))
		}
	}

	// Apply subscription phase ID filters (plural handles both single and multiple values)
	if len(f.SubscriptionPhaseIDs) > 0 {
		if len(f.SubscriptionPhaseIDs) == 1 {
			query = query.Where(couponassociation.SubscriptionPhaseID(f.SubscriptionPhaseIDs[0]))
		} else {
			query = query.Where(couponassociation.SubscriptionPhaseIDIn(f.SubscriptionPhaseIDs...))
		}
	}

	// Load coupon relation if requested
	if f.WithCoupon {
		query = query.WithCoupon()
	}

	return query, nil
}

type couponAssociationRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts CouponAssociationQueryOptions
	cache     cache.Cache
}

func NewCouponAssociationRepository(client postgres.IClient, log *logger.Logger, cache cache.Cache) domainCouponAssociation.Repository {
	return &couponAssociationRepository{
		client:    client,
		log:       log,
		queryOpts: CouponAssociationQueryOptions{},
		cache:     cache,
	}
}

func (r *couponAssociationRepository) Create(ctx context.Context, ca *domainCouponAssociation.CouponAssociation) error {
	client := r.client.Writer(ctx)

	r.log.Debugw("creating coupon association",
		"association_id", ca.ID,
		"coupon_id", ca.CouponID,
		"subscription_id", ca.SubscriptionID,
		"subscription_line_item_id", ca.SubscriptionLineItemID,
		"subscription_phase_id", ca.SubscriptionPhaseID,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon_association", "create", map[string]interface{}{
		"association_id": ca.ID,
		"coupon_id":      ca.CouponID,
	})
	defer FinishSpan(span)

	// Set environment ID from context if not already set
	if ca.EnvironmentID == "" {
		ca.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	// Build the create query
	createQuery := client.CouponAssociation.Create().
		SetID(ca.ID).
		SetTenantID(ca.TenantID).
		SetCouponID(ca.CouponID).
		SetSubscriptionID(ca.SubscriptionID).
		SetStatus(string(ca.Status)).
		SetCreatedAt(ca.CreatedAt).
		SetUpdatedAt(ca.UpdatedAt).
		SetCreatedBy(ca.CreatedBy).
		SetUpdatedBy(ca.UpdatedBy).
		SetEnvironmentID(ca.EnvironmentID)

	// Handle optional subscription line item ID
	if ca.SubscriptionLineItemID != nil {
		createQuery = createQuery.SetSubscriptionLineItemID(*ca.SubscriptionLineItemID)
	}

	// Handle optional subscription phase ID
	if ca.SubscriptionPhaseID != nil {
		createQuery = createQuery.SetSubscriptionPhaseID(*ca.SubscriptionPhaseID)
	}

	// Handle optional start date (nullable in schema)
	// Note: Since start_date has a default in schema, we only set if provided
	if !ca.StartDate.IsZero() {
		createQuery = createQuery.SetStartDate(ca.StartDate)
	}

	// Handle optional end date
	if ca.EndDate != nil {
		createQuery = createQuery.SetEndDate(*ca.EndDate)
	}

	// Handle optional metadata
	if ca.Metadata != nil {
		createQuery = createQuery.SetMetadata(ca.Metadata)
	}

	// Create the coupon association
	_, err := createQuery.Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to create coupon association in database").
			WithReportableDetails(map[string]interface{}{
				"association_id": ca.ID,
				"coupon_id":      ca.CouponID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	r.log.Infow("created coupon association",
		"association_id", ca.ID,
		"coupon_id", ca.CouponID,
		"subscription_id", ca.SubscriptionID,
		"subscription_line_item_id", ca.SubscriptionLineItemID,
		"subscription_phase_id", ca.SubscriptionPhaseID)

	return nil
}

func (r *couponAssociationRepository) Get(ctx context.Context, id string) (*domainCouponAssociation.CouponAssociation, error) {
	client := r.client.Reader(ctx)

	r.log.Debugw("getting coupon association", "id", id)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon_association", "get", map[string]interface{}{
		"association_id": id,
	})
	defer FinishSpan(span)

	ca, err := client.CouponAssociation.Query().
		Where(
			couponassociation.ID(id),
			couponassociation.TenantID(types.GetTenantID(ctx)),
			couponassociation.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			SetSpanError(span, err)
			return nil, ierr.NewError("coupon association not found").
				WithHint("The specified coupon association does not exist").
				WithReportableDetails(map[string]interface{}{
					"association_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to get coupon association from database").
			WithReportableDetails(map[string]interface{}{
				"association_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainCouponAssociation.FromEnt(ca), nil
}

func (r *couponAssociationRepository) Update(ctx context.Context, ca *domainCouponAssociation.CouponAssociation) error {
	client := r.client.Writer(ctx)

	r.log.Debugw("updating coupon association",
		"association_id", ca.ID,
		"coupon_id", ca.CouponID,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon_association", "update", map[string]interface{}{
		"association_id": ca.ID,
	})
	defer FinishSpan(span)

	// Build the update query
	updateQuery := client.CouponAssociation.Update().
		Where(
			couponassociation.ID(ca.ID),
			couponassociation.TenantID(types.GetTenantID(ctx)),
			couponassociation.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetUpdatedAt(ca.UpdatedAt).
		SetUpdatedBy(ca.UpdatedBy)

	// Handle optional metadata
	if ca.Metadata != nil {
		updateQuery = updateQuery.SetMetadata(ca.Metadata)
	}

	// Handle optional end date (can be updated)
	if ca.EndDate != nil {
		updateQuery = updateQuery.SetEndDate(*ca.EndDate)
	}

	// Execute the update
	count, err := updateQuery.Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to update coupon association in database").
			WithReportableDetails(map[string]interface{}{
				"association_id": ca.ID,
			}).
			Mark(ierr.ErrDatabase)
	}

	if count == 0 {
		notFoundErr := fmt.Errorf("coupon association not found or already updated")
		SetSpanError(span, notFoundErr)
		return ierr.NewError("coupon association not found").
			WithHint("The specified coupon association does not exist or was already updated").
			WithReportableDetails(map[string]interface{}{
				"association_id": ca.ID,
			}).
			Mark(ierr.ErrNotFound)
	}

	SetSpanSuccess(span)
	r.log.Infow("updated coupon association",
		"association_id", ca.ID,
		"coupon_id", ca.CouponID,
		"rows_updated", count)

	return nil
}

func (r *couponAssociationRepository) Delete(ctx context.Context, id string) error {
	client := r.client.Writer(ctx)

	r.log.Debugw("deleting coupon association", "id", id)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon_association", "delete", map[string]interface{}{
		"association_id": id,
	})
	defer FinishSpan(span)

	count, err := client.CouponAssociation.Delete().
		Where(
			couponassociation.ID(id),
			couponassociation.TenantID(types.GetTenantID(ctx)),
			couponassociation.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Exec(ctx)

	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to delete coupon association from database").
			WithReportableDetails(map[string]interface{}{
				"association_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	if count == 0 {
		notFoundErr := fmt.Errorf("coupon association not found")
		SetSpanError(span, notFoundErr)
		return ierr.NewError("coupon association not found").
			WithHint("The specified coupon association does not exist").
			WithReportableDetails(map[string]interface{}{
				"association_id": id,
			}).
			Mark(ierr.ErrNotFound)
	}

	SetSpanSuccess(span)
	r.log.Infow("deleted coupon association",
		"association_id", id,
		"rows_deleted", count)
	return nil
}

// List retrieves coupon associations based on the provided filter
func (r *couponAssociationRepository) List(ctx context.Context, filter *types.CouponAssociationFilter) ([]*domainCouponAssociation.CouponAssociation, error) {
	if filter == nil {
		filter = types.NewCouponAssociationFilter()
	}

	r.log.Debugw("listing coupon associations", "filter", filter)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon_association", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	if err := filter.Validate(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Invalid coupon association filter").
			Mark(ierr.ErrValidation)
	}

	client := r.client.Reader(ctx)
	query := client.CouponAssociation.Query()

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

	associations, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list coupon associations from database").
			WithReportableDetails(map[string]interface{}{
				"filter": filter,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainCouponAssociation.FromEntList(associations), nil
}

func (r *couponAssociationRepository) GetBySubscription(ctx context.Context, subscriptionID string) ([]*domainCouponAssociation.CouponAssociation, error) {
	// Use List method with filter for backwards compatibility
	filter := types.NewNoLimitCouponAssociationFilter()
	subscriptionLineItemIDIsNil := true
	filter.SubscriptionIDs = []string{subscriptionID}
	filter.SubscriptionLineItemIDIsNil = &subscriptionLineItemIDIsNil
	filter.WithCoupon = true
	return r.List(ctx, filter)
}

func (r *couponAssociationRepository) GetBySubscriptionForLineItems(ctx context.Context, subscriptionID string) ([]*domainCouponAssociation.CouponAssociation, error) {
	// Use List method with filter for backwards compatibility
	filter := types.NewNoLimitCouponAssociationFilter()
	subscriptionLineItemIDIsNil := false
	filter.SubscriptionIDs = []string{subscriptionID}
	filter.SubscriptionLineItemIDIsNil = &subscriptionLineItemIDIsNil
	return r.List(ctx, filter)
}
