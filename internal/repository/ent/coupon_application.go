package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/couponapplication"
	"github.com/flexprice/flexprice/internal/cache"
	domainCouponApplication "github.com/flexprice/flexprice/internal/domain/coupon_application"
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
	client := r.client.Querier(ctx)

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
		r.log.Errorw("failed to create coupon application",
			"coupon_application_id", ca.ID,
			"coupon_id", ca.CouponID,
			"invoice_id", ca.InvoiceID,
			"error", err)
		return ierr.WithError(err).
			WithHint("Failed to create coupon application").
			Mark(ierr.ErrDatabase)
	}

	// Set cache
	r.SetCache(ctx, ca)

	r.log.Infow("successfully created coupon application",
		"coupon_application_id", ca.ID,
		"coupon_id", ca.CouponID,
		"invoice_id", ca.InvoiceID,
		"discounted_amount", ca.DiscountedAmount)

	return nil
}

func (r *couponApplicationRepository) Get(ctx context.Context, id string) (*domainCouponApplication.CouponApplication, error) {
	client := r.client.Querier(ctx)

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
		if ent.IsNotFound(err) {
			r.log.Debugw("coupon application not found",
				"coupon_application_id", id)
			return nil, ierr.NewError("coupon application not found").
				WithHint("The specified coupon application does not exist").
				WithReportableDetails(map[string]interface{}{
					"coupon_application_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		r.log.Errorw("failed to get coupon application",
			"coupon_application_id", id,
			"error", err)
		return nil, ierr.WithError(err).
			WithHint("Failed to get coupon application").
			Mark(ierr.ErrDatabase)
	}

	// Convert to domain model
	domainCA := r.toDomainCouponApplication(ca)

	// Set cache
	r.SetCache(ctx, domainCA)

	r.log.Debugw("successfully got coupon application",
		"coupon_application_id", id,
		"coupon_id", domainCA.CouponID,
		"invoice_id", domainCA.InvoiceID)

	return domainCA, nil
}

func (r *couponApplicationRepository) Update(ctx context.Context, ca *domainCouponApplication.CouponApplication) error {
	client := r.client.Querier(ctx)

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
		SetUpdatedAt(ca.UpdatedAt).
		SetUpdatedBy(ca.UpdatedBy)

	if ca.Metadata != nil {
		updateQuery = updateQuery.SetMetadata(ca.Metadata)
	}

	_, err := updateQuery.Save(ctx)
	if err != nil {
		r.log.Errorw("failed to update coupon application",
			"coupon_application_id", ca.ID,
			"error", err)
		return ierr.WithError(err).
			WithHint("Failed to update coupon application").
			Mark(ierr.ErrDatabase)
	}

	// Update cache
	r.SetCache(ctx, ca)

	r.log.Infow("successfully updated coupon application",
		"coupon_application_id", ca.ID,
		"coupon_id", ca.CouponID,
		"invoice_id", ca.InvoiceID)

	return nil
}

func (r *couponApplicationRepository) Delete(ctx context.Context, id string) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("deleting coupon application",
		"coupon_application_id", id)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon_application", "delete", map[string]interface{}{
		"coupon_application_id": id,
	})
	defer FinishSpan(span)

	err := client.CouponApplication.DeleteOneID(id).Exec(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			r.log.Debugw("coupon application not found for deletion",
				"coupon_application_id", id)
			return ierr.NewError("coupon application not found").
				WithHint("The specified coupon application does not exist").
				WithReportableDetails(map[string]interface{}{
					"coupon_application_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		r.log.Errorw("failed to delete coupon application",
			"coupon_application_id", id,
			"error", err)
		return ierr.WithError(err).
			WithHint("Failed to delete coupon application").
			Mark(ierr.ErrDatabase)
	}

	// Delete from cache
	r.DeleteCache(ctx, &domainCouponApplication.CouponApplication{ID: id})

	r.log.Infow("successfully deleted coupon application",
		"coupon_application_id", id)

	return nil
}

func (r *couponApplicationRepository) GetByInvoice(ctx context.Context, invoiceID string) ([]*domainCouponApplication.CouponApplication, error) {
	client := r.client.Querier(ctx)

	r.log.Debugw("getting coupon applications by invoice",
		"invoice_id", invoiceID)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon_application", "get_by_invoice", map[string]interface{}{
		"invoice_id": invoiceID,
	})
	defer FinishSpan(span)

	applications, err := client.CouponApplication.Query().
		Where(
			couponapplication.InvoiceID(invoiceID),
			couponapplication.TenantID(types.GetTenantID(ctx)),
			couponapplication.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		WithCoupon().
		All(ctx)
	if err != nil {
		r.log.Errorw("failed to get coupon applications by invoice",
			"invoice_id", invoiceID,
			"error", err)
		return nil, ierr.WithError(err).
			WithHint("Failed to get coupon applications by invoice").
			Mark(ierr.ErrDatabase)
	}

	// Convert to domain models
	domainApplications := make([]*domainCouponApplication.CouponApplication, len(applications))
	for i, app := range applications {
		domainApplications[i] = r.toDomainCouponApplication(app)
	}

	r.log.Debugw("successfully got coupon applications by invoice",
		"invoice_id", invoiceID,
		"count", len(domainApplications))

	return domainApplications, nil
}

func (r *couponApplicationRepository) GetBySubscription(ctx context.Context, subscriptionID string) ([]*domainCouponApplication.CouponApplication, error) {
	client := r.client.Querier(ctx)

	r.log.Debugw("getting coupon applications by subscription",
		"subscription_id", subscriptionID)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon_application", "get_by_subscription", map[string]interface{}{
		"subscription_id": subscriptionID,
	})
	defer FinishSpan(span)

	applications, err := client.CouponApplication.Query().
		Where(
			couponapplication.SubscriptionID(subscriptionID),
			couponapplication.TenantID(types.GetTenantID(ctx)),
			couponapplication.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		WithCoupon().
		All(ctx)
	if err != nil {
		r.log.Errorw("failed to get coupon applications by subscription",
			"subscription_id", subscriptionID,
			"error", err)
		return nil, ierr.WithError(err).
			WithHint("Failed to get coupon applications by subscription").
			Mark(ierr.ErrDatabase)
	}

	// Convert to domain models
	domainApplications := make([]*domainCouponApplication.CouponApplication, len(applications))
	for i, app := range applications {
		domainApplications[i] = r.toDomainCouponApplication(app)
	}

	r.log.Debugw("successfully got coupon applications by subscription",
		"subscription_id", subscriptionID,
		"count", len(domainApplications))

	return domainApplications, nil
}

func (r *couponApplicationRepository) GetBySubscriptionAndCoupon(ctx context.Context, subscriptionID string, couponID string) ([]*domainCouponApplication.CouponApplication, error) {
	client := r.client.Querier(ctx)

	r.log.Debugw("getting coupon applications by subscription and coupon",
		"subscription_id", subscriptionID,
		"coupon_id", couponID)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon_application", "get_by_subscription_and_coupon", map[string]interface{}{
		"subscription_id": subscriptionID,
		"coupon_id":       couponID,
	})
	defer FinishSpan(span)

	applications, err := client.CouponApplication.Query().
		Where(
			couponapplication.SubscriptionID(subscriptionID),
			couponapplication.CouponID(couponID),
			couponapplication.TenantID(types.GetTenantID(ctx)),
			couponapplication.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		WithCoupon().
		All(ctx)
	if err != nil {
		r.log.Errorw("failed to get coupon applications by subscription and coupon",
			"subscription_id", subscriptionID,
			"coupon_id", couponID,
			"error", err)
		return nil, ierr.WithError(err).
			WithHint("Failed to get coupon applications by subscription and coupon").
			Mark(ierr.ErrDatabase)
	}

	// Convert to domain models
	domainApplications := make([]*domainCouponApplication.CouponApplication, len(applications))
	for i, app := range applications {
		domainApplications[i] = r.toDomainCouponApplication(app)
	}

	r.log.Debugw("successfully got coupon applications by subscription and coupon",
		"subscription_id", subscriptionID,
		"coupon_id", couponID,
		"count", len(domainApplications))

	return domainApplications, nil
}

func (r *couponApplicationRepository) CountBySubscriptionAndCoupon(ctx context.Context, subscriptionID string, couponID string) (int, error) {
	client := r.client.Querier(ctx)

	r.log.Debugw("counting coupon applications by subscription and coupon",
		"subscription_id", subscriptionID,
		"coupon_id", couponID)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon_application", "count_by_subscription_and_coupon", map[string]interface{}{
		"subscription_id": subscriptionID,
		"coupon_id":       couponID,
	})
	defer FinishSpan(span)

	count, err := client.CouponApplication.Query().
		Where(
			couponapplication.SubscriptionID(subscriptionID),
			couponapplication.CouponID(couponID),
			couponapplication.TenantID(types.GetTenantID(ctx)),
			couponapplication.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Count(ctx)
	if err != nil {
		r.log.Errorw("failed to count coupon applications by subscription and coupon",
			"subscription_id", subscriptionID,
			"coupon_id", couponID,
			"error", err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count coupon applications by subscription and coupon").
			Mark(ierr.ErrDatabase)
	}

	r.log.Debugw("successfully counted coupon applications by subscription and coupon",
		"subscription_id", subscriptionID,
		"coupon_id", couponID,
		"count", count)

	return count, nil
}

// Helper method to convert ent.CouponApplication to domain.CouponApplication
func (r *couponApplicationRepository) toDomainCouponApplication(ca *ent.CouponApplication) *domainCouponApplication.CouponApplication {
	domainCA := &domainCouponApplication.CouponApplication{
		ID:                  ca.ID,
		CouponID:            ca.CouponID,
		CouponAssociationID: *ca.CouponAssociationID,
		InvoiceID:           ca.InvoiceID,
		AppliedAt:           ca.AppliedAt,
		OriginalPrice:       ca.OriginalPrice,
		FinalPrice:          ca.FinalPrice,
		DiscountedAmount:    ca.DiscountedAmount,
		DiscountType:        types.CouponType(ca.DiscountType),
		Currency:            *ca.Currency,
		EnvironmentID:       ca.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  ca.TenantID,
			Status:    types.Status(ca.Status),
			CreatedBy: ca.CreatedBy,
			UpdatedBy: ca.UpdatedBy,
			CreatedAt: ca.CreatedAt,
			UpdatedAt: ca.UpdatedAt,
		},
	}

	// Handle optional fields
	if ca.InvoiceLineItemID != nil {
		domainCA.InvoiceLineItemID = ca.InvoiceLineItemID
	}
	if ca.SubscriptionID != nil {
		domainCA.SubscriptionID = ca.SubscriptionID
	}
	if ca.DiscountPercentage != nil {
		domainCA.DiscountPercentage = ca.DiscountPercentage
	}
	if ca.CouponSnapshot != nil {
		domainCA.CouponSnapshot = ca.CouponSnapshot
	}
	if ca.Metadata != nil {
		domainCA.Metadata = ca.Metadata
	}

	return domainCA
}

// CouponApplicationQueryOptions holds query options for coupon application operations
type CouponApplicationQuery = *ent.CouponApplicationQuery

type CouponApplicationQueryOptions struct{}

// Cache methods
func (r *couponApplicationRepository) SetCache(ctx context.Context, ca *domainCouponApplication.CouponApplication) {
	if r.cache == nil {
		return
	}

	key := "coupon_application:" + ca.ID
	r.cache.Set(ctx, key, ca, time.Hour)
}

func (r *couponApplicationRepository) GetCache(ctx context.Context, key string) *domainCouponApplication.CouponApplication {
	if r.cache == nil {
		return nil
	}

	cacheKey := "coupon_application:" + key
	if cached, found := r.cache.Get(ctx, cacheKey); found {
		if ca, ok := cached.(*domainCouponApplication.CouponApplication); ok {
			return ca
		}
	}

	return nil
}

func (r *couponApplicationRepository) DeleteCache(ctx context.Context, ca *domainCouponApplication.CouponApplication) {
	if r.cache == nil {
		return
	}

	key := "coupon_application:" + ca.ID
	r.cache.Delete(ctx, key)
}
