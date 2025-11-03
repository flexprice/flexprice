package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// CouponService defines the interface for core coupon CRUD operations
type CouponService interface {
	// Core coupon operations
	CreateCoupon(ctx context.Context, req dto.CreateCouponRequest) (*dto.CouponResponse, error)
	GetCoupon(ctx context.Context, id string) (*dto.CouponResponse, error)
	UpdateCoupon(ctx context.Context, id string, req dto.UpdateCouponRequest) (*dto.CouponResponse, error)
	DeleteCoupon(ctx context.Context, id string) error
	ListCoupons(ctx context.Context, filter *types.CouponFilter) (*dto.ListCouponsResponse, error)
	CalculateDiscount(ctx context.Context, couponID string, originalPrice decimal.Decimal) (decimal.Decimal, error)
}

type couponService struct {
	ServiceParams
}

// NewCouponService creates a new coupon service
func NewCouponService(
	params ServiceParams,
) CouponService {
	return &couponService{
		ServiceParams: params,
	}
}

// CreateCoupon creates a new coupon
func (s *couponService) CreateCoupon(ctx context.Context, req dto.CreateCouponRequest) (*dto.CouponResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	c := req.ToCoupon(ctx)

	if err := s.CouponRepo.Create(ctx, c); err != nil {
		return nil, err
	}

	return dto.NewCouponResponse(c), nil
}

// GetCoupon retrieves a coupon by ID
func (s *couponService) GetCoupon(ctx context.Context, id string) (*dto.CouponResponse, error) {
	c, err := s.CouponRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	return dto.NewCouponResponse(c), nil
}

// UpdateCoupon updates an existing coupon
func (s *couponService) UpdateCoupon(ctx context.Context, id string, req dto.UpdateCouponRequest) (*dto.CouponResponse, error) {
	c, err := s.CouponRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Update fields if provided
	if req.Name != nil {
		c.Name = *req.Name
	}

	if req.Metadata != nil {
		c.Metadata = req.Metadata
	}

	if err := s.CouponRepo.Update(ctx, c); err != nil {
		return nil, err
	}

	return dto.NewCouponResponse(c), nil
}

// DeleteCoupon deletes a coupon
func (s *couponService) DeleteCoupon(ctx context.Context, id string) error {
	return s.CouponRepo.Delete(ctx, id)
}

// ListCoupons lists coupons with filtering
func (s *couponService) ListCoupons(ctx context.Context, filter *types.CouponFilter) (*dto.ListCouponsResponse, error) {
	if filter == nil {
		filter = types.NewCouponFilter()
	}

	// Ensure QueryFilter is initialized
	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewDefaultQueryFilter()
	}

	if err := filter.Validate(); err != nil {
		return nil, err
	}

	coupons, err := s.CouponRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	total, err := s.CouponRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	responses := make([]*dto.CouponResponse, len(coupons))
	for i, c := range coupons {
		responses[i] = dto.NewCouponResponse(c)
	}

	listResponse := types.NewListResponse(responses, total, filter.GetLimit(), filter.GetOffset())
	return &listResponse, nil
}

// CalculateDiscount calculates the discount amount for a given coupon and price
func (s *couponService) CalculateDiscount(ctx context.Context, couponID string, originalPrice decimal.Decimal) (decimal.Decimal, error) {
	// Validate input parameters
	if couponID == "" {
		return decimal.Zero, ierr.NewError("coupon_id is required").
			WithHint("Please provide a valid coupon ID").
			Mark(ierr.ErrValidation)
	}

	if originalPrice.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero, ierr.NewError("original_price must be greater than zero").
			WithHint("Please provide a valid original price").
			WithReportableDetails(map[string]interface{}{
				"original_price": originalPrice,
			}).
			Mark(ierr.ErrValidation)
	}

	s.Logger.Debugw("calculating discount for coupon",
		"coupon_id", couponID,
		"original_price", originalPrice)

	// Get the coupon
	c, err := s.CouponRepo.Get(ctx, couponID)
	if err != nil {
		return decimal.Zero, ierr.WithError(err).
			WithHint("Failed to get coupon for discount calculation").
			WithReportableDetails(map[string]interface{}{
				"coupon_id": couponID,
			}).
			Mark(ierr.ErrNotFound)
	}

	// Validate coupon is active
	if c.Status != types.StatusPublished {
		return decimal.Zero, ierr.NewError("only active coupons can be used for discount calculation").
			WithHint("Please select an active coupon").
			WithReportableDetails(map[string]interface{}{
				"coupon_id": couponID,
				"status":    c.Status,
			}).
			Mark(ierr.ErrValidation)
	}

	// Validate coupon is valid for redemption
	if !c.IsValid() {
		return decimal.Zero, ierr.NewError("coupon is not valid for redemption").
			WithHint("Coupon may be expired, have reached maximum redemptions, or not yet available for redemption").
			WithReportableDetails(map[string]interface{}{
				"coupon_id":         couponID,
				"redeem_after":      c.RedeemAfter,
				"redeem_before":     c.RedeemBefore,
				"total_redemptions": c.TotalRedemptions,
				"max_redemptions":   c.MaxRedemptions,
			}).
			Mark(ierr.ErrValidation)
	}

	discount := c.CalculateDiscount(originalPrice)

	s.Logger.Debugw("calculated discount for coupon",
		"coupon_id", couponID,
		"original_price", originalPrice,
		"discount", discount,
		"coupon_type", c.Type)

	return discount, nil
}
