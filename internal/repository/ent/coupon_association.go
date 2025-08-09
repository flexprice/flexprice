package ent

import (
	"context"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/couponassociation"
	"github.com/flexprice/flexprice/internal/cache"
	domainCoupon "github.com/flexprice/flexprice/internal/domain/coupon"
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
	client := r.client.Querier(ctx)

	r.log.Debugw("creating coupon association",
		"association_id", ca.ID,
		"coupon_id", ca.CouponID,
		"subscription_id", ca.SubscriptionID,
		"subscription_line_item_id", ca.SubscriptionLineItemID,
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

	// Handle optional metadata
	if ca.Metadata != nil {
		createQuery = createQuery.SetMetadata(ca.Metadata)
	}

	// Create the coupon association
	_, err := createQuery.Save(ctx)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to create coupon association in database").
			WithReportableDetails(map[string]interface{}{
				"association_id": ca.ID,
				"coupon_id":      ca.CouponID,
			}).
			Mark(ierr.ErrDatabase)
	}

	r.log.Infow("created coupon association",
		"association_id", ca.ID,
		"coupon_id", ca.CouponID,
		"subscription_id", ca.SubscriptionID,
		"subscription_line_item_id", ca.SubscriptionLineItemID)

	return nil
}

func (r *couponAssociationRepository) Get(ctx context.Context, id string) (*domainCouponAssociation.CouponAssociation, error) {
	client := r.client.Querier(ctx)

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
			return nil, ierr.NewError("coupon association not found").
				WithHint("The specified coupon association does not exist").
				WithReportableDetails(map[string]interface{}{
					"association_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get coupon association from database").
			WithReportableDetails(map[string]interface{}{
				"association_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	return r.toDomainCouponAssociation(ca), nil
}

func (r *couponAssociationRepository) Update(ctx context.Context, ca *domainCouponAssociation.CouponAssociation) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("updating coupon association",
		"association_id", ca.ID,
		"coupon_id", ca.CouponID,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon_association", "update", map[string]interface{}{
		"association_id": ca.ID,
	})
	defer FinishSpan(span)

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

	_, err := updateQuery.Save(ctx)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to update coupon association in database").
			WithReportableDetails(map[string]interface{}{
				"association_id": ca.ID,
			}).
			Mark(ierr.ErrDatabase)
	}

	r.log.Infow("updated coupon association",
		"association_id", ca.ID,
		"coupon_id", ca.CouponID)

	return nil
}

func (r *couponAssociationRepository) Delete(ctx context.Context, id string) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("deleting coupon association", "id", id)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon_association", "delete", map[string]interface{}{
		"association_id": id,
	})
	defer FinishSpan(span)

	_, err := client.CouponAssociation.Delete().
		Where(
			couponassociation.ID(id),
			couponassociation.TenantID(types.GetTenantID(ctx)),
			couponassociation.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Exec(ctx)

	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to delete coupon association from database").
			WithReportableDetails(map[string]interface{}{
				"association_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	r.log.Infow("deleted coupon association", "association_id", id)
	return nil
}

func (r *couponAssociationRepository) GetBySubscription(ctx context.Context, subscriptionID string) ([]*domainCouponAssociation.CouponAssociation, error) {
	client := r.client.Querier(ctx)

	r.log.Debugw("getting coupon associations by subscription", "subscription_id", subscriptionID)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon_association", "get_by_subscription", map[string]interface{}{
		"subscription_id": subscriptionID,
	})
	defer FinishSpan(span)

	associations, err := client.CouponAssociation.Query().
		Where(
			couponassociation.SubscriptionID(subscriptionID),
			couponassociation.SubscriptionLineItemIDIsNil(),
			couponassociation.TenantID(types.GetTenantID(ctx)),
			couponassociation.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		WithCoupon().
		All(ctx)

	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get coupon associations from database").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": subscriptionID,
			}).
			Mark(ierr.ErrDatabase)
	}

	domainAssociations := make([]*domainCouponAssociation.CouponAssociation, len(associations))
	for i, ca := range associations {
		domainAssociations[i] = r.toDomainCouponAssociation(ca)
	}

	return domainAssociations, nil
}

func (r *couponAssociationRepository) GetBySubscriptionForLineItems(ctx context.Context, subscriptionID string) ([]*domainCouponAssociation.CouponAssociation, error) {
	client := r.client.Querier(ctx)

	r.log.Debugw("getting coupon associations by subscription line item", "subscription_id", subscriptionID)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon_association", "get_by_subscription_line_item", map[string]interface{}{
		"subscription_id": subscriptionID,
	})
	defer FinishSpan(span)

	associations, err := client.CouponAssociation.Query().
		Where(
			couponassociation.SubscriptionID(subscriptionID),
			couponassociation.SubscriptionLineItemIDNotNil(),
			couponassociation.TenantID(types.GetTenantID(ctx)),
			couponassociation.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		All(ctx)

	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get coupon associations from database").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": subscriptionID,
			}).
			Mark(ierr.ErrDatabase)
	}

	domainAssociations := make([]*domainCouponAssociation.CouponAssociation, len(associations))
	for i, ca := range associations {
		domainAssociations[i] = r.toDomainCouponAssociation(ca)
	}

	return domainAssociations, nil
}

// Helper method to convert ent.CouponAssociation to domain.CouponAssociation
func (r *couponAssociationRepository) toDomainCouponAssociation(ca *ent.CouponAssociation) *domainCouponAssociation.CouponAssociation {

	couponObj := domainCoupon.FromEnt(ca.Edges.Coupon)

	return &domainCouponAssociation.CouponAssociation{
		ID:                     ca.ID,
		CouponID:               ca.CouponID,
		SubscriptionID:         ca.SubscriptionID,
		SubscriptionLineItemID: ca.SubscriptionLineItemID,
		Metadata:               ca.Metadata,
		EnvironmentID:          ca.EnvironmentID,
		Coupon:                 couponObj,
		BaseModel: types.BaseModel{
			TenantID:  ca.TenantID,
			Status:    types.Status(ca.Status),
			CreatedAt: ca.CreatedAt,
			UpdatedAt: ca.UpdatedAt,
			CreatedBy: ca.CreatedBy,
			UpdatedBy: ca.UpdatedBy,
		},
	}
}
