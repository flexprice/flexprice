package testutil

import (
	"context"
	"strings"

	"github.com/flexprice/flexprice/internal/domain/coupon"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryCouponStore implements coupon.Repository
type InMemoryCouponStore struct {
	*InMemoryStore[*coupon.Coupon]
}

// NewInMemoryCouponStore creates a new in-memory coupon store
func NewInMemoryCouponStore() *InMemoryCouponStore {
	return &InMemoryCouponStore{
		InMemoryStore: NewInMemoryStore[*coupon.Coupon](),
	}
}

// Helper to copy coupon
func copyCoupon(c *coupon.Coupon) *coupon.Coupon {
	if c == nil {
		return nil
	}

	// Deep copy of coupon
	copied := &coupon.Coupon{
		ID:                c.ID,
		Name:              c.Name,
		RedeemAfter:       c.RedeemAfter,
		RedeemBefore:      c.RedeemBefore,
		MaxRedemptions:    c.MaxRedemptions,
		TotalRedemptions:  c.TotalRedemptions,
		Rules:             c.Rules,
		AmountOff:         c.AmountOff,
		PercentageOff:     c.PercentageOff,
		Type:              c.Type,
		Cadence:           c.Cadence,
		DurationInPeriods: c.DurationInPeriods,
		Currency:          c.Currency,
		Metadata:          c.Metadata,
		EnvironmentID:     c.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  c.TenantID,
			Status:    c.Status,
			CreatedAt: c.CreatedAt,
			UpdatedAt: c.UpdatedAt,
			CreatedBy: c.CreatedBy,
			UpdatedBy: c.UpdatedBy,
		},
	}

	return copied
}

func (s *InMemoryCouponStore) Create(ctx context.Context, c *coupon.Coupon) error {
	if c == nil {
		return ierr.NewError("coupon cannot be nil").
			WithHint("Coupon cannot be nil").
			Mark(ierr.ErrValidation)
	}

	// Set environment ID from context if not already set
	if c.EnvironmentID == "" {
		c.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	return s.InMemoryStore.Create(ctx, c.ID, copyCoupon(c))
}

func (s *InMemoryCouponStore) Get(ctx context.Context, id string) (*coupon.Coupon, error) {
	c, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		return nil, ierr.NewError("coupon not found").
			WithHint("Coupon not found").
			WithReportableDetails(map[string]interface{}{
				"id": id,
			}).
			Mark(ierr.ErrNotFound)
	}
	return copyCoupon(c), nil
}

func (s *InMemoryCouponStore) GetBatch(ctx context.Context, ids []string) ([]*coupon.Coupon, error) {
	coupons := make([]*coupon.Coupon, 0, len(ids))
	for _, id := range ids {
		c, err := s.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		coupons = append(coupons, c)
	}
	return coupons, nil
}

func (s *InMemoryCouponStore) Update(ctx context.Context, c *coupon.Coupon) error {
	if c == nil {
		return ierr.NewError("coupon cannot be nil").
			WithHint("Coupon cannot be nil").
			Mark(ierr.ErrValidation)
	}

	return s.InMemoryStore.Update(ctx, c.ID, copyCoupon(c))
}

func (s *InMemoryCouponStore) Delete(ctx context.Context, id string) error {
	return s.InMemoryStore.Delete(ctx, id)
}

func (s *InMemoryCouponStore) List(ctx context.Context, filter *types.CouponFilter) ([]*coupon.Coupon, error) {
	items, err := s.InMemoryStore.List(ctx, filter, couponFilterFn, couponSortFn)
	if err != nil {
		return nil, err
	}

	return lo.Map(items, func(c *coupon.Coupon, _ int) *coupon.Coupon {
		return copyCoupon(c)
	}), nil
}

func (s *InMemoryCouponStore) Count(ctx context.Context, filter *types.CouponFilter) (int, error) {
	return s.InMemoryStore.Count(ctx, filter, couponFilterFn)
}

func (s *InMemoryCouponStore) IncrementRedemptions(ctx context.Context, id string) error {
	c, err := s.Get(ctx, id)
	if err != nil {
		return err
	}

	c.TotalRedemptions++
	return s.Update(ctx, c)
}

// couponFilterFn implements filtering logic for coupons
func couponFilterFn(ctx context.Context, c *coupon.Coupon, filter interface{}) bool {
	f, ok := filter.(*types.CouponFilter)
	if !ok {
		return false
	}

	// Apply tenant filter
	tenantID := types.GetTenantID(ctx)
	if tenantID != "" && c.TenantID != tenantID {
		return false
	}

	// Apply environment filter
	if !CheckEnvironmentFilter(ctx, c.EnvironmentID) {
		return false
	}

	// Apply coupon IDs filter
	if len(f.CouponIDs) > 0 && !lo.Contains(f.CouponIDs, c.ID) {
		return false
	}

	// Apply filters from filter conditions
	if f.Filters != nil {
		for _, filterCondition := range f.Filters {
			if !applyFilterCondition(c, filterCondition) {
				return false
			}
		}
	}

	return true
}

// applyFilterCondition applies a single filter condition to a coupon
func applyFilterCondition(c *coupon.Coupon, condition *types.FilterCondition) bool {
	if condition.Field == nil {
		return true
	}

	switch *condition.Field {
	case "name":
		if condition.Value != nil && condition.Value.String != nil {
			return strings.Contains(strings.ToLower(c.Name), strings.ToLower(*condition.Value.String))
		}
	case "type":
		if condition.Value != nil && condition.Value.String != nil {
			return string(c.Type) == *condition.Value.String
		}
	case "cadence":
		if condition.Value != nil && condition.Value.String != nil {
			return string(c.Cadence) == *condition.Value.String
		}
	case "currency":
		if condition.Value != nil && condition.Value.String != nil {
			return c.Currency == *condition.Value.String
		}
	case "status":
		if condition.Value != nil && condition.Value.String != nil {
			return string(c.Status) == *condition.Value.String
		}
	default:
		return true
	}

	return true
}

// couponSortFn implements sorting logic for coupons
func couponSortFn(i, j *coupon.Coupon) bool {
	// Default sort by created_at desc
	return i.CreatedAt.After(j.CreatedAt)
}
