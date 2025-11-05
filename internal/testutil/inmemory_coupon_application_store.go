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

// List retrieves coupon applications based on the provided filter
func (s *InMemoryCouponApplicationStore) List(ctx context.Context, filter *types.CouponApplicationFilter) ([]*coupon_application.CouponApplication, error) {
	if filter == nil {
		filter = types.NewCouponApplicationFilter()
	}

	items, err := s.InMemoryStore.List(ctx, filter, couponApplicationFilterFn, couponApplicationSortFn)
	if err != nil {
		return nil, err
	}

	return lo.Map(items, func(ca *coupon_application.CouponApplication, _ int) *coupon_application.CouponApplication {
		return copyCouponApplication(ca)
	}), nil
}

// Count counts coupon applications based on the provided filter
func (s *InMemoryCouponApplicationStore) Count(ctx context.Context, filter *types.CouponApplicationFilter) (int, error) {
	if filter == nil {
		filter = types.NewCouponApplicationFilter()
	}

	return s.InMemoryStore.Count(ctx, filter, couponApplicationFilterFn)
}

// couponApplicationFilterFn implements filtering logic for coupon applications
func couponApplicationFilterFn(ctx context.Context, ca *coupon_application.CouponApplication, filter interface{}) bool {
	f, ok := filter.(*types.CouponApplicationFilter)
	if !ok {
		return false
	}

	// Apply tenant filter
	tenantID := types.GetTenantID(ctx)
	if tenantID != "" && ca.TenantID != tenantID {
		return false
	}

	// Apply environment filter
	if !CheckEnvironmentFilter(ctx, ca.EnvironmentID) {
		return false
	}

	// Apply status filter (default: exclude deleted)
	if f.QueryFilter != nil {
		status := f.QueryFilter.GetStatus()
		if status == "" {
			if ca.Status == types.StatusDeleted {
				return false
			}
		} else if ca.Status != types.Status(status) {
			return false
		}
	} else if ca.Status == types.StatusDeleted {
		return false
	}

	// Check invoice IDs filter
	if len(f.InvoiceIDs) > 0 && !lo.Contains(f.InvoiceIDs, ca.InvoiceID) {
		return false
	}

	// Check subscription IDs filter
	if len(f.SubscriptionIDs) > 0 {
		if ca.SubscriptionID == nil || !lo.Contains(f.SubscriptionIDs, *ca.SubscriptionID) {
			return false
		}
	}

	// Check coupon IDs filter
	if len(f.CouponIDs) > 0 && !lo.Contains(f.CouponIDs, ca.CouponID) {
		return false
	}

	// Check invoice line item IDs filter
	if len(f.InvoiceLineItemIDs) > 0 {
		if ca.InvoiceLineItemID == nil || !lo.Contains(f.InvoiceLineItemIDs, *ca.InvoiceLineItemID) {
			return false
		}
	}

	// Check coupon association IDs filter
	if len(f.CouponAssociationIDs) > 0 && !lo.Contains(f.CouponAssociationIDs, ca.CouponAssociationID) {
		return false
	}

	return true
}

// couponApplicationSortFn implements sorting logic for coupon applications
func couponApplicationSortFn(i, j *coupon_application.CouponApplication) bool {
	// Default sort by created_at desc
	return i.CreatedAt.After(j.CreatedAt)
}
