package testutil

import (
	"context"
	"time"

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
	var subscriptionPhaseID *string
	if ca.SubscriptionPhaseID != nil {
		phaseID := *ca.SubscriptionPhaseID
		subscriptionPhaseID = &phaseID
	}

	var endDate *time.Time
	if ca.EndDate != nil {
		endDateVal := *ca.EndDate
		endDate = &endDateVal
	}

	copied := &coupon_association.CouponAssociation{
		ID:                     ca.ID,
		CouponID:               ca.CouponID,
		SubscriptionID:         ca.SubscriptionID,
		SubscriptionLineItemID: ca.SubscriptionLineItemID,
		SubscriptionPhaseID:    subscriptionPhaseID,
		StartDate:              ca.StartDate,
		EndDate:                endDate,
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

func (s *InMemoryCouponAssociationStore) List(ctx context.Context, filter *types.CouponAssociationFilter) ([]*coupon_association.CouponAssociation, error) {
	if filter == nil {
		filter = types.NewCouponAssociationFilter()
	}

	items, err := s.InMemoryStore.List(ctx, filter, couponAssociationFilterFn, couponAssociationSortFn)
	if err != nil {
		return nil, err
	}

	return lo.Map(items, func(ca *coupon_association.CouponAssociation, _ int) *coupon_association.CouponAssociation {
		return copyCouponAssociation(ca)
	}), nil
}

func (s *InMemoryCouponAssociationStore) Count(ctx context.Context, filter *types.CouponAssociationFilter) (int, error) {
	if filter == nil {
		filter = types.NewCouponAssociationFilter()
	}

	return s.InMemoryStore.Count(ctx, filter, couponAssociationFilterFn)
}

// couponAssociationFilterFn implements filtering logic for coupon associations
func couponAssociationFilterFn(ctx context.Context, ca *coupon_association.CouponAssociation, filter interface{}) bool {
	f, ok := filter.(*types.CouponAssociationFilter)
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

	// Apply subscription IDs filter
	if len(f.SubscriptionIDs) > 0 && !lo.Contains(f.SubscriptionIDs, ca.SubscriptionID) {
		return false
	}

	// Apply coupon IDs filter
	if len(f.CouponIDs) > 0 && !lo.Contains(f.CouponIDs, ca.CouponID) {
		return false
	}

	// Apply subscription line item ID filters
	if len(f.SubscriptionLineItemIDs) > 0 {
		if ca.SubscriptionLineItemID == nil {
			return false
		}
		if !lo.Contains(f.SubscriptionLineItemIDs, *ca.SubscriptionLineItemID) {
			return false
		}
	}

	// Apply subscription phase ID filters
	if len(f.SubscriptionPhaseIDs) > 0 {
		if ca.SubscriptionPhaseID == nil {
			return false
		}
		if !lo.Contains(f.SubscriptionPhaseIDs, *ca.SubscriptionPhaseID) {
			return false
		}
	}

	// Apply active filter based on start_date and end_date
	if f.ActiveOnly {
		var periodStart, periodEnd time.Time

		if f.PeriodStart != nil && f.PeriodEnd != nil {
			// Use provided period
			periodStart = f.PeriodStart.UTC()
			periodEnd = f.PeriodEnd.UTC()
		} else if f.PeriodStart != nil {
			// Only ActivePeriodStart provided, use it for both checks
			periodStart = f.PeriodStart.UTC()
			periodEnd = periodStart
		} else if f.PeriodEnd != nil {
			// Only ActivePeriodEnd provided, use it for both checks
			periodEnd = f.PeriodEnd.UTC()
			periodStart = periodEnd
		} else {
			// No period provided, use current time
			now := time.Now().UTC()
			periodStart = now
			periodEnd = now
		}

		// Check if association is active during the period
		// Association is active if:
		// - start_date <= period_end (association started before or during the period)
		// - AND (end_date IS NULL OR end_date >= period_start) (association hasn't ended before the period or is indefinite)
		if ca.StartDate.After(periodEnd) {
			return false
		}
		if ca.EndDate != nil && ca.EndDate.Before(periodStart) {
			return false
		}
	}

	// Apply filter conditions if any
	if f.Filters != nil {
		for _, condition := range f.Filters {
			if !applyCouponAssociationFilterCondition(ca, condition) {
				return false
			}
		}
	}

	return true
}

// applyCouponAssociationFilterCondition applies a single filter condition
func applyCouponAssociationFilterCondition(ca *coupon_association.CouponAssociation, condition *types.FilterCondition) bool {
	if condition.Field == nil {
		return true
	}

	switch *condition.Field {
	case "status":
		if condition.Value != nil && condition.Value.String != nil {
			return string(ca.Status) == *condition.Value.String
		}
	case "created_at":
		if condition.Value != nil && condition.Value.Date != nil {
			// Implement date comparison logic if needed
			return true
		}
	default:
		return true
	}

	return true
}

// couponAssociationSortFn implements sorting logic for coupon associations
func couponAssociationSortFn(i, j *coupon_association.CouponAssociation) bool {
	// Default sort by created_at desc
	return i.CreatedAt.After(j.CreatedAt)
}
