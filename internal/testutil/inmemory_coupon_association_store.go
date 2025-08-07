package testutil

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/coupon_association"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryCouponAssociationStore implements coupon_association.Repository
type InMemoryCouponAssociationStore struct {
	*InMemoryStore[*coupon_association.CouponAssociation]
}

// NewInMemoryCouponAssociationStore creates a new in-memory coupon association store
func NewInMemoryCouponAssociationStore() *InMemoryCouponAssociationStore {
	return &InMemoryCouponAssociationStore{
		InMemoryStore: NewInMemoryStore[*coupon_association.CouponAssociation](),
	}
}

// Helper to copy coupon association
func copyCouponAssociation(ca *coupon_association.CouponAssociation) *coupon_association.CouponAssociation {
	if ca == nil {
		return nil
	}

	// Deep copy of coupon association
	copied := &coupon_association.CouponAssociation{
		ID:                     ca.ID,
		CouponID:               ca.CouponID,
		SubscriptionID:         ca.SubscriptionID,
		SubscriptionLineItemID: ca.SubscriptionLineItemID,
		Metadata:               lo.Assign(map[string]string{}, ca.Metadata),
		EnvironmentID:          ca.EnvironmentID,
		Coupon:                 ca.Coupon,
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

func (s *InMemoryCouponAssociationStore) Create(ctx context.Context, ca *coupon_association.CouponAssociation) error {
	if ca == nil {
		return ierr.NewError("coupon association cannot be nil").
			WithHint("Coupon association cannot be nil").
			Mark(ierr.ErrValidation)
	}

	// Set environment ID from context if not already set
	if ca.EnvironmentID == "" {
		ca.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	return s.InMemoryStore.Create(ctx, ca.ID, copyCouponAssociation(ca))
}

func (s *InMemoryCouponAssociationStore) Get(ctx context.Context, id string) (*coupon_association.CouponAssociation, error) {
	ca, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		return nil, ierr.NewError("coupon association not found").
			WithHint("Coupon association not found").
			WithReportableDetails(map[string]interface{}{
				"id": id,
			}).
			Mark(ierr.ErrNotFound)
	}
	return copyCouponAssociation(ca), nil
}

func (s *InMemoryCouponAssociationStore) Update(ctx context.Context, ca *coupon_association.CouponAssociation) error {
	if ca == nil {
		return ierr.NewError("coupon association cannot be nil").
			WithHint("Coupon association cannot be nil").
			Mark(ierr.ErrValidation)
	}

	return s.InMemoryStore.Update(ctx, ca.ID, copyCouponAssociation(ca))
}

func (s *InMemoryCouponAssociationStore) Delete(ctx context.Context, id string) error {
	return s.InMemoryStore.Delete(ctx, id)
}

func (s *InMemoryCouponAssociationStore) GetBySubscription(ctx context.Context, subscriptionID string) ([]*coupon_association.CouponAssociation, error) {
	// Create a filter function that matches by subscription_id
	filterFn := func(ctx context.Context, ca *coupon_association.CouponAssociation, _ interface{}) bool {
		return ca.SubscriptionID == subscriptionID &&
			ca.TenantID == types.GetTenantID(ctx) &&
			CheckEnvironmentFilter(ctx, ca.EnvironmentID)
	}

	// List all coupon associations with our filter
	associations, err := s.InMemoryStore.List(ctx, nil, filterFn, nil)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list coupon associations").
			Mark(ierr.ErrDatabase)
	}

	return lo.Map(associations, func(ca *coupon_association.CouponAssociation, _ int) *coupon_association.CouponAssociation {
		return copyCouponAssociation(ca)
	}), nil
}
