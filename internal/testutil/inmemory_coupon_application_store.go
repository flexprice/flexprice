package testutil

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/coupon_application"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryCouponApplicationStore implements coupon_application.Repository
type InMemoryCouponApplicationStore struct {
	*InMemoryStore[*coupon_application.CouponApplication]
}

// NewInMemoryCouponApplicationStore creates a new in-memory coupon application store
func NewInMemoryCouponApplicationStore() *InMemoryCouponApplicationStore {
	return &InMemoryCouponApplicationStore{
		InMemoryStore: NewInMemoryStore[*coupon_application.CouponApplication](),
	}
}

// Helper to copy coupon application
func copyCouponApplication(ca *coupon_application.CouponApplication) *coupon_application.CouponApplication {
	if ca == nil {
		return nil
	}

	// Deep copy of coupon application
	copied := &coupon_application.CouponApplication{
		ID:                  ca.ID,
		CouponID:            ca.CouponID,
		CouponAssociationID: ca.CouponAssociationID,
		InvoiceID:           ca.InvoiceID,
		InvoiceLineItemID:   ca.InvoiceLineItemID,
		SubscriptionID:      ca.SubscriptionID,
		AppliedAt:           ca.AppliedAt,
		OriginalPrice:       ca.OriginalPrice,
		FinalPrice:          ca.FinalPrice,
		DiscountedAmount:    ca.DiscountedAmount,
		DiscountType:        ca.DiscountType,
		DiscountPercentage:  ca.DiscountPercentage,
		Currency:            ca.Currency,
		CouponSnapshot:      ca.CouponSnapshot,
		Metadata:            lo.Assign(map[string]string{}, ca.Metadata),
		EnvironmentID:       ca.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  ca.TenantID,
			Status:    ca.Status,
			CreatedAt: ca.CreatedAt,
			UpdatedAt: ca.UpdatedAt,
			CreatedBy: ca.CreatedBy,
			UpdatedBy: ca.UpdatedBy,
		},
	}

	return copied
}

func (s *InMemoryCouponApplicationStore) Create(ctx context.Context, ca *coupon_application.CouponApplication) error {
	if ca == nil {
		return ierr.NewError("coupon application cannot be nil").
			WithHint("Coupon application cannot be nil").
			Mark(ierr.ErrValidation)
	}

	// Set environment ID from context if not already set
	if ca.EnvironmentID == "" {
		ca.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	return s.InMemoryStore.Create(ctx, ca.ID, copyCouponApplication(ca))
}

func (s *InMemoryCouponApplicationStore) Get(ctx context.Context, id string) (*coupon_application.CouponApplication, error) {
	ca, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		return nil, ierr.NewError("coupon application not found").
			WithHint("Coupon application not found").
			WithReportableDetails(map[string]interface{}{
				"id": id,
			}).
			Mark(ierr.ErrNotFound)
	}
	return copyCouponApplication(ca), nil
}

func (s *InMemoryCouponApplicationStore) Update(ctx context.Context, ca *coupon_application.CouponApplication) error {
	if ca == nil {
		return ierr.NewError("coupon application cannot be nil").
			WithHint("Coupon application cannot be nil").
			Mark(ierr.ErrValidation)
	}

	return s.InMemoryStore.Update(ctx, ca.ID, copyCouponApplication(ca))
}

func (s *InMemoryCouponApplicationStore) Delete(ctx context.Context, id string) error {
	return s.InMemoryStore.Delete(ctx, id)
}

func (s *InMemoryCouponApplicationStore) GetByInvoice(ctx context.Context, invoiceID string) ([]*coupon_application.CouponApplication, error) {
	// Create a filter function that matches by invoice_id
	filterFn := func(ctx context.Context, ca *coupon_application.CouponApplication, _ interface{}) bool {
		return ca.InvoiceID == invoiceID &&
			ca.TenantID == types.GetTenantID(ctx) &&
			CheckEnvironmentFilter(ctx, ca.EnvironmentID)
	}

	// List all coupon applications with our filter
	applications, err := s.InMemoryStore.List(ctx, nil, filterFn, nil)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list coupon applications").
			Mark(ierr.ErrDatabase)
	}

	return lo.Map(applications, func(ca *coupon_application.CouponApplication, _ int) *coupon_application.CouponApplication {
		return copyCouponApplication(ca)
	}), nil
}

func (s *InMemoryCouponApplicationStore) GetBySubscription(ctx context.Context, subscriptionID string) ([]*coupon_application.CouponApplication, error) {
	// Create a filter function that matches by subscription_id
	filterFn := func(ctx context.Context, ca *coupon_application.CouponApplication, _ interface{}) bool {
		return ca.SubscriptionID != nil && *ca.SubscriptionID == subscriptionID &&
			ca.TenantID == types.GetTenantID(ctx) &&
			CheckEnvironmentFilter(ctx, ca.EnvironmentID)
	}

	// List all coupon applications with our filter
	applications, err := s.InMemoryStore.List(ctx, nil, filterFn, nil)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list coupon applications").
			Mark(ierr.ErrDatabase)
	}

	return lo.Map(applications, func(ca *coupon_application.CouponApplication, _ int) *coupon_application.CouponApplication {
		return copyCouponApplication(ca)
	}), nil
}

func (s *InMemoryCouponApplicationStore) GetBySubscriptionAndCoupon(ctx context.Context, subscriptionID string, couponID string) ([]*coupon_application.CouponApplication, error) {
	// Create a filter function that matches by subscription_id and coupon_id
	filterFn := func(ctx context.Context, ca *coupon_application.CouponApplication, _ interface{}) bool {
		return ca.SubscriptionID != nil && *ca.SubscriptionID == subscriptionID &&
			ca.CouponID == couponID &&
			ca.TenantID == types.GetTenantID(ctx) &&
			CheckEnvironmentFilter(ctx, ca.EnvironmentID)
	}

	// List all coupon applications with our filter
	applications, err := s.InMemoryStore.List(ctx, nil, filterFn, nil)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list coupon applications").
			Mark(ierr.ErrDatabase)
	}

	return lo.Map(applications, func(ca *coupon_application.CouponApplication, _ int) *coupon_application.CouponApplication {
		return copyCouponApplication(ca)
	}), nil
}

func (s *InMemoryCouponApplicationStore) CountBySubscriptionAndCoupon(ctx context.Context, subscriptionID string, couponID string) (int, error) {
	// Create a filter function that matches by subscription_id and coupon_id
	filterFn := func(ctx context.Context, ca *coupon_application.CouponApplication, _ interface{}) bool {
		return ca.SubscriptionID != nil && *ca.SubscriptionID == subscriptionID &&
			ca.CouponID == couponID &&
			ca.TenantID == types.GetTenantID(ctx) &&
			CheckEnvironmentFilter(ctx, ca.EnvironmentID)
	}

	// Count all coupon applications with our filter
	return s.InMemoryStore.Count(ctx, nil, filterFn)
}
