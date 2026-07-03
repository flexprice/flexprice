package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	coupon_domain "github.com/flexprice/flexprice/internal/domain/coupon"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type CouponServiceSuite struct {
	testutil.BaseServiceTestSuite
	service CouponService
	now     time.Time
}

func TestCouponService(t *testing.T) {
	suite.Run(t, new(CouponServiceSuite))
}

func (s *CouponServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = NewCouponService(newTestServiceParams(&s.BaseServiceTestSuite))
	s.now = time.Now().UTC()
}

// ---------------------------------------------------------------------------
// fixtures
// ---------------------------------------------------------------------------

func (s *CouponServiceSuite) createFixedCoupon(name, amount, currency string) *dto.CouponResponse {
	resp, err := s.service.CreateCoupon(s.GetContext(), dto.CreateCouponRequest{
		Name:      name,
		Type:      types.CouponTypeFixed,
		Cadence:   types.CouponCadenceOnce,
		AmountOff: lo.ToPtr(decimal.RequireFromString(amount)),
		Currency:  lo.ToPtr(currency),
	})
	s.Require().NoError(err)
	s.Require().NotNil(resp)
	return resp
}

func (s *CouponServiceSuite) createPercentageCoupon(name, percentage string) *dto.CouponResponse {
	resp, err := s.service.CreateCoupon(s.GetContext(), dto.CreateCouponRequest{
		Name:          name,
		Type:          types.CouponTypePercentage,
		Cadence:       types.CouponCadenceOnce,
		PercentageOff: lo.ToPtr(decimal.RequireFromString(percentage)),
	})
	s.Require().NoError(err)
	s.Require().NotNil(resp)
	return resp
}

// ---------------------------------------------------------------------------
// CreateCoupon
// ---------------------------------------------------------------------------

func (s *CouponServiceSuite) TestCreateCoupon() {
	testCases := []struct {
		name          string
		req           dto.CreateCouponRequest
		expectedError bool
	}{
		{
			name: "valid_fixed_coupon_persists_amount_and_currency",
			req: dto.CreateCouponRequest{
				Name:      "Ten Off",
				Type:      types.CouponTypeFixed,
				Cadence:   types.CouponCadenceOnce,
				AmountOff: lo.ToPtr(decimal.RequireFromString("10")),
				Currency:  lo.ToPtr("usd"),
			},
			expectedError: false,
		},
		{
			name: "valid_percentage_coupon_ignores_currency",
			req: dto.CreateCouponRequest{
				Name:          "Twenty Percent",
				Type:          types.CouponTypePercentage,
				Cadence:       types.CouponCadenceForever,
				PercentageOff: lo.ToPtr(decimal.RequireFromString("20")),
				Currency:      lo.ToPtr("usd"),
			},
			expectedError: false,
		},
		{
			name: "valid_repeated_cadence_coupon_with_duration",
			req: dto.CreateCouponRequest{
				Name:              "Repeated Discount",
				Type:              types.CouponTypePercentage,
				Cadence:           types.CouponCadenceRepeated,
				PercentageOff:     lo.ToPtr(decimal.RequireFromString("15")),
				DurationInPeriods: lo.ToPtr(3),
			},
			expectedError: false,
		},
		{
			name: "missing_name_returns_validation_error",
			req: dto.CreateCouponRequest{
				Type:      types.CouponTypeFixed,
				Cadence:   types.CouponCadenceOnce,
				AmountOff: lo.ToPtr(decimal.RequireFromString("10")),
			},
			expectedError: true,
		},
		{
			name: "missing_type_returns_validation_error",
			req: dto.CreateCouponRequest{
				Name:    "No Type",
				Cadence: types.CouponCadenceOnce,
			},
			expectedError: true,
		},
		{
			name: "fixed_coupon_without_amount_off_returns_error",
			req: dto.CreateCouponRequest{
				Name:    "No Amount",
				Type:    types.CouponTypeFixed,
				Cadence: types.CouponCadenceOnce,
			},
			expectedError: true,
		},
		{
			name: "fixed_coupon_with_zero_amount_returns_error",
			req: dto.CreateCouponRequest{
				Name:      "Zero Amount",
				Type:      types.CouponTypeFixed,
				Cadence:   types.CouponCadenceOnce,
				AmountOff: lo.ToPtr(decimal.Zero),
			},
			expectedError: true,
		},
		{
			name: "percentage_coupon_without_percentage_off_returns_error",
			req: dto.CreateCouponRequest{
				Name:    "No Percentage",
				Type:    types.CouponTypePercentage,
				Cadence: types.CouponCadenceOnce,
			},
			expectedError: true,
		},
		{
			name: "percentage_above_100_returns_error",
			req: dto.CreateCouponRequest{
				Name:          "Over 100",
				Type:          types.CouponTypePercentage,
				Cadence:       types.CouponCadenceOnce,
				PercentageOff: lo.ToPtr(decimal.RequireFromString("100.5")),
			},
			expectedError: true,
		},
		{
			name: "redeem_after_later_than_redeem_before_returns_error",
			req: dto.CreateCouponRequest{
				Name:          "Bad Window",
				Type:          types.CouponTypePercentage,
				Cadence:       types.CouponCadenceOnce,
				PercentageOff: lo.ToPtr(decimal.RequireFromString("10")),
				RedeemAfter:   lo.ToPtr(time.Now().UTC().Add(48 * time.Hour)),
				RedeemBefore:  lo.ToPtr(time.Now().UTC().Add(24 * time.Hour)),
			},
			expectedError: true,
		},
		{
			name: "zero_max_redemptions_returns_error",
			req: dto.CreateCouponRequest{
				Name:           "Zero Redemptions",
				Type:           types.CouponTypePercentage,
				Cadence:        types.CouponCadenceOnce,
				PercentageOff:  lo.ToPtr(decimal.RequireFromString("10")),
				MaxRedemptions: lo.ToPtr(0),
			},
			expectedError: true,
		},
		{
			name: "repeated_cadence_without_duration_returns_error",
			req: dto.CreateCouponRequest{
				Name:          "Repeated No Duration",
				Type:          types.CouponTypePercentage,
				Cadence:       types.CouponCadenceRepeated,
				PercentageOff: lo.ToPtr(decimal.RequireFromString("10")),
			},
			expectedError: true,
		},
		{
			name: "repeated_cadence_with_zero_duration_returns_error",
			req: dto.CreateCouponRequest{
				Name:              "Repeated Zero Duration",
				Type:              types.CouponTypePercentage,
				Cadence:           types.CouponCadenceRepeated,
				PercentageOff:     lo.ToPtr(decimal.RequireFromString("10")),
				DurationInPeriods: lo.ToPtr(0),
			},
			expectedError: true,
		},
		{
			name: "non_repeated_cadence_with_duration_returns_error",
			req: dto.CreateCouponRequest{
				Name:              "Once With Duration",
				Type:              types.CouponTypePercentage,
				Cadence:           types.CouponCadenceOnce,
				PercentageOff:     lo.ToPtr(decimal.RequireFromString("10")),
				DurationInPeriods: lo.ToPtr(2),
			},
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			resp, err := s.service.CreateCoupon(s.GetContext(), tc.req)
			if tc.expectedError {
				s.Error(err)
				s.True(ierr.IsValidation(err))
				return
			}
			s.NoError(err)
			s.Require().NotNil(resp)

			// Read back through the repository and verify persisted state
			stored, err := s.GetStores().CouponRepo.Get(s.GetContext(), resp.ID)
			s.Require().NoError(err)
			s.Equal(tc.req.Name, stored.Name)
			s.Equal(tc.req.Type, stored.Type)
			s.Equal(tc.req.Cadence, stored.Cadence)
			s.Equal(0, stored.TotalRedemptions)
			if tc.req.AmountOff != nil {
				s.Require().NotNil(stored.AmountOff)
				s.True(stored.AmountOff.Equal(*tc.req.AmountOff))
			}
			if tc.req.PercentageOff != nil {
				s.Require().NotNil(stored.PercentageOff)
				s.True(stored.PercentageOff.Equal(*tc.req.PercentageOff))
			}
			if tc.req.Type == types.CouponTypePercentage {
				s.Empty(stored.Currency, "percentage coupons must not persist a currency")
			} else if tc.req.Currency != nil {
				s.Equal(*tc.req.Currency, stored.Currency)
			}
			if tc.req.DurationInPeriods != nil {
				s.Require().NotNil(stored.DurationInPeriods)
				s.Equal(*tc.req.DurationInPeriods, *stored.DurationInPeriods)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// GetCoupon / GetCouponByCode
// ---------------------------------------------------------------------------

func (s *CouponServiceSuite) TestGetCoupon() {
	created := s.createFixedCoupon("Get Coupon", "5", "usd")

	s.Run("existing_id_returns_coupon", func() {
		resp, err := s.service.GetCoupon(s.GetContext(), created.ID)
		s.NoError(err)
		s.Require().NotNil(resp)
		s.Equal(created.ID, resp.ID)
		s.Equal("Get Coupon", resp.Name)
		s.Require().NotNil(resp.AmountOff)
		s.True(resp.AmountOff.Equal(decimal.RequireFromString("5")))
	})

	s.Run("not_found_id_returns_error", func() {
		_, err := s.service.GetCoupon(s.GetContext(), "coupon_missing")
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})
}

func (s *CouponServiceSuite) TestGetCouponByCode() {
	ctx := s.GetContext()
	pct := decimal.RequireFromString("25")
	c := &coupon_domain.Coupon{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON),
		Name:          "Coded Coupon",
		CouponCode:    lo.ToPtr("spring25"),
		Type:          types.CouponTypePercentage,
		Cadence:       types.CouponCadenceOnce,
		PercentageOff: &pct,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CouponRepo.Create(ctx, c))

	s.Run("empty_code_returns_validation_error", func() {
		_, err := s.service.GetCouponByCode(ctx, "")
		s.Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("unknown_code_returns_not_found", func() {
		_, err := s.service.GetCouponByCode(ctx, "no-such-code")
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})

	s.Run("existing_code_returns_coupon_case_insensitively", func() {
		resp, err := s.service.GetCouponByCode(ctx, "SPRING25")
		s.NoError(err)
		s.Require().NotNil(resp)
		s.Equal(c.ID, resp.ID)
	})
}

// ---------------------------------------------------------------------------
// UpdateCoupon
// ---------------------------------------------------------------------------

func (s *CouponServiceSuite) TestUpdateCoupon() {
	s.Run("not_found_id_returns_error", func() {
		_, err := s.service.UpdateCoupon(s.GetContext(), "coupon_missing", dto.UpdateCouponRequest{
			Name: lo.ToPtr("x"),
		})
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})

	s.Run("updates_name_and_metadata_and_persists", func() {
		created := s.createFixedCoupon("Before Update", "5", "usd")
		resp, err := s.service.UpdateCoupon(s.GetContext(), created.ID, dto.UpdateCouponRequest{
			Name:     lo.ToPtr("After Update"),
			Metadata: lo.ToPtr(map[string]string{"campaign": "q3"}),
		})
		s.NoError(err)
		s.Require().NotNil(resp)
		s.Equal("After Update", resp.Name)

		stored, err := s.GetStores().CouponRepo.Get(s.GetContext(), created.ID)
		s.Require().NoError(err)
		s.Equal("After Update", stored.Name)
		s.Require().NotNil(stored.Metadata)
		s.Equal(map[string]string{"campaign": "q3"}, *stored.Metadata)
	})

	s.Run("nil_fields_keep_existing_values", func() {
		created := s.createFixedCoupon("Keep Me", "5", "usd")
		_, err := s.service.UpdateCoupon(s.GetContext(), created.ID, dto.UpdateCouponRequest{})
		s.NoError(err)

		stored, err := s.GetStores().CouponRepo.Get(s.GetContext(), created.ID)
		s.Require().NoError(err)
		s.Equal("Keep Me", stored.Name)
		s.Require().NotNil(stored.AmountOff)
		s.True(stored.AmountOff.Equal(decimal.RequireFromString("5")))
	})
}

// ---------------------------------------------------------------------------
// DeleteCoupon
// ---------------------------------------------------------------------------

func (s *CouponServiceSuite) TestDeleteCoupon() {
	s.Run("existing_coupon_is_deleted", func() {
		created := s.createFixedCoupon("Delete Me", "5", "usd")
		err := s.service.DeleteCoupon(s.GetContext(), created.ID)
		s.NoError(err)

		_, err = s.GetStores().CouponRepo.Get(s.GetContext(), created.ID)
		s.Error(err)
		s.True(ierr.IsNotFound(err))
	})

	s.Run("not_found_id_returns_error", func() {
		err := s.service.DeleteCoupon(s.GetContext(), "coupon_missing")
		s.Error(err)
	})
}

// ---------------------------------------------------------------------------
// ListCoupons
// ---------------------------------------------------------------------------

func (s *CouponServiceSuite) TestListCoupons() {
	s.createFixedCoupon("List A", "5", "usd")
	s.createFixedCoupon("List B", "10", "usd")
	s.createPercentageCoupon("List C", "15")

	s.Run("nil_filter_returns_all_coupons", func() {
		resp, err := s.service.ListCoupons(s.GetContext(), nil)
		s.NoError(err)
		s.Require().NotNil(resp)
		s.Len(resp.Items, 3)
		s.Equal(3, resp.Pagination.Total)
	})

	s.Run("filter_without_query_filter_is_defaulted", func() {
		resp, err := s.service.ListCoupons(s.GetContext(), &types.CouponFilter{})
		s.NoError(err)
		s.Require().NotNil(resp)
		s.Len(resp.Items, 3)
	})

	s.Run("limit_is_respected_with_total_unchanged", func() {
		filter := types.NewCouponFilter()
		filter.QueryFilter.Limit = lo.ToPtr(2)
		resp, err := s.service.ListCoupons(s.GetContext(), filter)
		s.NoError(err)
		s.Require().NotNil(resp)
		s.Len(resp.Items, 2)
		s.Equal(3, resp.Pagination.Total)
	})
}

// ---------------------------------------------------------------------------
// ApplyDiscount
// ---------------------------------------------------------------------------

func (s *CouponServiceSuite) TestApplyDiscount() {
	fixed := s.createFixedCoupon("Fixed Five", "5", "usd")
	pct := s.createPercentageCoupon("Ten Percent", "10")
	big := s.createFixedCoupon("Bigger Than Price", "50", "usd")
	fractional := s.createPercentageCoupon("Fractional", "33.333")

	testCases := []struct {
		name               string
		req                dto.ApplyDiscountRequest
		expectedError      bool
		expectedDiscount   string
		expectedFinalPrice string
	}{
		{
			name: "missing_coupon_id_returns_validation_error",
			req: dto.ApplyDiscountRequest{
				OriginalPrice: decimal.RequireFromString("100"),
				Currency:      "usd",
			},
			expectedError: true,
		},
		{
			name: "zero_original_price_returns_validation_error",
			req: dto.ApplyDiscountRequest{
				CouponID:      fixed.ID,
				OriginalPrice: decimal.Zero,
				Currency:      "usd",
			},
			expectedError: true,
		},
		{
			name: "missing_currency_returns_validation_error",
			req: dto.ApplyDiscountRequest{
				CouponID:      fixed.ID,
				OriginalPrice: decimal.RequireFromString("100"),
			},
			expectedError: true,
		},
		{
			name: "unknown_coupon_returns_not_found",
			req: dto.ApplyDiscountRequest{
				CouponID:      "coupon_missing",
				OriginalPrice: decimal.RequireFromString("100"),
				Currency:      "usd",
			},
			expectedError: true,
		},
		{
			name: "fixed_discount_is_subtracted_from_price",
			req: dto.ApplyDiscountRequest{
				CouponID:      fixed.ID,
				OriginalPrice: decimal.RequireFromString("100"),
				Currency:      "usd",
			},
			expectedDiscount:   "5",
			expectedFinalPrice: "95",
		},
		{
			name: "percentage_discount_is_computed_from_price",
			req: dto.ApplyDiscountRequest{
				CouponID:      pct.ID,
				OriginalPrice: decimal.RequireFromString("200"),
				Currency:      "usd",
			},
			expectedDiscount:   "20",
			expectedFinalPrice: "180",
		},
		{
			name: "percentage_discount_rounds_to_currency_precision",
			req: dto.ApplyDiscountRequest{
				CouponID:      fractional.ID,
				OriginalPrice: decimal.RequireFromString("9.99"),
				Currency:      "usd",
			},
			// 9.99 * 33.333% = 3.3299667 -> rounded to 3.33
			expectedDiscount:   "3.33",
			expectedFinalPrice: "6.66",
		},
		{
			name: "discount_larger_than_price_clamps_final_price_to_zero",
			req: dto.ApplyDiscountRequest{
				CouponID:      big.ID,
				OriginalPrice: decimal.RequireFromString("30"),
				Currency:      "usd",
			},
			expectedDiscount:   "30",
			expectedFinalPrice: "0",
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			result, err := s.service.ApplyDiscount(s.GetContext(), tc.req)
			if tc.expectedError {
				s.Error(err)
				return
			}
			s.Require().NoError(err)
			s.Require().NotNil(result)
			s.True(result.Discount.Equal(decimal.RequireFromString(tc.expectedDiscount)),
				"expected discount %s, got %s", tc.expectedDiscount, result.Discount.String())
			s.True(result.FinalPrice.Equal(decimal.RequireFromString(tc.expectedFinalPrice)),
				"expected final price %s, got %s", tc.expectedFinalPrice, result.FinalPrice.String())
		})
	}
}

func (s *CouponServiceSuite) TestApplyDiscount_InvalidCoupons() {
	ctx := s.GetContext()

	newCoupon := func(mutate func(c *coupon_domain.Coupon)) *coupon_domain.Coupon {
		amt := decimal.RequireFromString("5")
		c := &coupon_domain.Coupon{
			ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON),
			Name:          "Invalid Redemption",
			Type:          types.CouponTypeFixed,
			Cadence:       types.CouponCadenceOnce,
			AmountOff:     &amt,
			Currency:      "usd",
			EnvironmentID: types.GetEnvironmentID(ctx),
			BaseModel:     types.GetDefaultBaseModel(ctx),
		}
		mutate(c)
		s.Require().NoError(s.GetStores().CouponRepo.Create(ctx, c))
		return c
	}

	testCases := []struct {
		name   string
		mutate func(c *coupon_domain.Coupon)
	}{
		{
			name: "expired_coupon_is_rejected",
			mutate: func(c *coupon_domain.Coupon) {
				c.RedeemBefore = lo.ToPtr(s.now.Add(-24 * time.Hour))
			},
		},
		{
			name: "not_yet_redeemable_coupon_is_rejected",
			mutate: func(c *coupon_domain.Coupon) {
				c.RedeemAfter = lo.ToPtr(s.now.Add(24 * time.Hour))
			},
		},
		{
			name: "max_redemptions_reached_coupon_is_rejected",
			mutate: func(c *coupon_domain.Coupon) {
				c.MaxRedemptions = lo.ToPtr(3)
				c.TotalRedemptions = 3
			},
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			c := newCoupon(tc.mutate)
			_, err := s.service.ApplyDiscount(ctx, dto.ApplyDiscountRequest{
				CouponID:      c.ID,
				OriginalPrice: decimal.RequireFromString("100"),
				Currency:      "usd",
			})
			s.Error(err)
			s.True(ierr.IsValidation(err))
		})
	}
}
