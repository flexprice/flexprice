package service

import (
	"strings"

	"github.com/flexprice/flexprice/internal/api/dto"
	coupon_domain "github.com/flexprice/flexprice/internal/domain/coupon"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// ─────────────────────────────────────────────
// GetByCode tests
// ─────────────────────────────────────────────

func (s *SubscriptionModificationServiceSuite) TestCouponCodeGetByCode_CaseInsensitive() {
	ctx := s.GetContext()
	code := "SUMMER20"
	pct := decimal.NewFromInt(20)
	c := &coupon_domain.Coupon{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON),
		Name:          "Summer Sale",
		CouponCode:    lo.ToPtr(code),
		Type:          types.CouponTypePercentage,
		Cadence:       types.CouponCadenceOnce,
		PercentageOff: &pct,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	c.Status = types.StatusPublished
	s.Require().NoError(s.GetStores().CouponRepo.Create(ctx, c))

	// Lookup with lowercase
	found, err := s.GetStores().CouponRepo.GetByCode(ctx, "summer20")
	s.Require().NoError(err)
	s.Require().NotNil(found)
	s.Equal(c.ID, found.ID)

	// Lookup with uppercase should also work (normalised)
	found2, err := s.GetStores().CouponRepo.GetByCode(ctx, "SUMMER20")
	s.Require().NoError(err)
	s.Require().NotNil(found2)
	s.Equal(c.ID, found2.ID)
}

func (s *SubscriptionModificationServiceSuite) TestCouponCodeGetByCode_NotFound() {
	ctx := s.GetContext()
	_, err := s.GetStores().CouponRepo.GetByCode(ctx, "NONEXISTENT99")
	s.Require().Error(err)
}

// ─────────────────────────────────────────────
// Subscription modify via coupon_code tests
// ─────────────────────────────────────────────

func (s *SubscriptionModificationServiceSuite) TestCouponCodeModify_AddByCouponCode() {
	ctx := s.GetContext()
	cust := s.createCustomer("coupon-code-add")
	sub := s.createActiveSub(cust.ID)

	// Create a coupon with a human-readable code (store it lowercase)
	code := "LAUNCH50"
	pct := decimal.NewFromInt(50)
	c := &coupon_domain.Coupon{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON),
		Name:          "Launch Promo",
		CouponCode:    lo.ToPtr(strings.ToLower(code)),
		Type:          types.CouponTypePercentage,
		Cadence:       types.CouponCadenceOnce,
		PercentageOff: &pct,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	c.Status = types.StatusPublished
	s.Require().NoError(s.GetStores().CouponRepo.Create(ctx, c))

	// Add coupon by code (uppercase — should be normalised by the store)
	req := dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeCoupon,
		CouponParams: &dto.SubModifyCouponParams{
			Action:     dto.SubModifyCouponActionAdd,
			CouponCode: lo.ToPtr(code),
		},
	}
	resp, err := s.service.Execute(ctx, sub.ID, req)
	s.Require().NoError(err)
	s.Require().NotNil(resp)
	s.Require().NotNil(resp.Subscription)

	// Verify association was created for the correct coupon
	filter := &types.CouponAssociationFilter{
		QueryFilter:     types.NewNoLimitQueryFilter(),
		SubscriptionIDs: []string{sub.ID},
		CouponIDs:       []string{c.ID},
	}
	assocs, err := s.GetStores().CouponAssociationRepo.List(ctx, filter)
	s.Require().NoError(err)
	s.Require().Len(assocs, 1, "should have one association created")
}

func (s *SubscriptionModificationServiceSuite) TestCouponCodeModify_AddByCouponCode_NotFound() {
	ctx := s.GetContext()
	cust := s.createCustomer("coupon-code-notfound")
	sub := s.createActiveSub(cust.ID)

	req := dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeCoupon,
		CouponParams: &dto.SubModifyCouponParams{
			Action:     dto.SubModifyCouponActionAdd,
			CouponCode: lo.ToPtr("NONEXISTENT"),
		},
	}
	_, err := s.service.Execute(ctx, sub.ID, req)
	s.Require().Error(err, "should fail with coupon not found")
}
