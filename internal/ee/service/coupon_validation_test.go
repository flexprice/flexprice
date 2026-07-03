package service

import (
	"testing"
	"time"

	coupon_domain "github.com/flexprice/flexprice/internal/domain/coupon"
	"github.com/flexprice/flexprice/internal/domain/coupon_application"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type CouponValidationServiceSuite struct {
	testutil.BaseServiceTestSuite
	service CouponValidationService
	now     time.Time
}

func TestCouponValidationService(t *testing.T) {
	suite.Run(t, new(CouponValidationServiceSuite))
}

func (s *CouponValidationServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = NewCouponValidationService(newTestServiceParams(&s.BaseServiceTestSuite))
	s.now = time.Now().UTC()
}

// ---------------------------------------------------------------------------
// fixtures
// ---------------------------------------------------------------------------

// newValidPercentageCoupon returns a published, currently-redeemable percentage
// coupon with "once" cadence. Cases mutate it to hit specific validation rules.
func (s *CouponValidationServiceSuite) newValidPercentageCoupon() coupon_domain.Coupon {
	ctx := s.GetContext()
	pct := decimal.RequireFromString("10")
	return coupon_domain.Coupon{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON),
		Name:          "Validation Coupon",
		Type:          types.CouponTypePercentage,
		Cadence:       types.CouponCadenceOnce,
		PercentageOff: &pct,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
}

func (s *CouponValidationServiceSuite) newSubscription(currency string) *subscription.Subscription {
	return &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		Currency:           currency,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
}

// addCouponApplications persists n coupon applications for the coupon/subscription
// pair so cadence rules that count prior applications can be exercised.
func (s *CouponValidationServiceSuite) addCouponApplications(couponID, subscriptionID string, n int) {
	ctx := s.GetContext()
	for i := 0; i < n; i++ {
		app := &coupon_application.CouponApplication{
			ID:                  types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON_APPLICATION),
			CouponID:            couponID,
			CouponAssociationID: types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON_ASSOCIATION),
			InvoiceID:           types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE),
			SubscriptionID:      lo.ToPtr(subscriptionID),
			AppliedAt:           s.now,
			OriginalPrice:       decimal.RequireFromString("100"),
			FinalPrice:          decimal.RequireFromString("90"),
			DiscountedAmount:    decimal.RequireFromString("10"),
			DiscountType:        types.CouponTypePercentage,
			Currency:            "usd",
			EnvironmentID:       types.GetEnvironmentID(ctx),
			BaseModel:           types.GetDefaultBaseModel(ctx),
		}
		s.Require().NoError(s.GetStores().CouponApplicationRepo.Create(ctx, app))
	}
}

// assertValidationErrorCode asserts err is a *CouponValidationError carrying code.
func (s *CouponValidationServiceSuite) assertValidationErrorCode(err error, code types.CouponValidationErrorCode) {
	s.Require().Error(err)
	var vErr *CouponValidationError
	s.Require().ErrorAs(err, &vErr)
	s.Equal(code, vErr.Code)
	s.NotEmpty(vErr.Error())
}

// ---------------------------------------------------------------------------
// ValidateCouponBasic
// ---------------------------------------------------------------------------

func (s *CouponValidationServiceSuite) TestValidateCouponBasic() {
	s.Run("published_coupon_passes", func() {
		c := s.newValidPercentageCoupon()
		s.NoError(s.service.ValidateCouponBasic(c))
	})

	s.Run("archived_coupon_fails_with_not_published_code", func() {
		c := s.newValidPercentageCoupon()
		c.Status = types.StatusArchived
		err := s.service.ValidateCouponBasic(c)
		s.assertValidationErrorCode(err, types.CouponValidationErrorCodeNotPublished)
	})

	s.Run("deleted_coupon_fails_with_not_published_code", func() {
		c := s.newValidPercentageCoupon()
		c.Status = types.StatusDeleted
		err := s.service.ValidateCouponBasic(c)
		s.assertValidationErrorCode(err, types.CouponValidationErrorCodeNotPublished)
	})
}

// ---------------------------------------------------------------------------
// ValidateCoupon — status, date window, currency, redemption limits
// ---------------------------------------------------------------------------

func (s *CouponValidationServiceSuite) TestValidateCoupon_BasicAndBusinessRules() {
	testCases := []struct {
		name         string
		mutateCoupon func(c *coupon_domain.Coupon)
		subscription func() *subscription.Subscription
		expectedCode *types.CouponValidationErrorCode
	}{
		{
			name:         "valid_coupon_without_subscription_passes",
			mutateCoupon: func(c *coupon_domain.Coupon) {},
			subscription: func() *subscription.Subscription { return nil },
			expectedCode: nil,
		},
		{
			name: "unpublished_coupon_fails_before_other_rules",
			mutateCoupon: func(c *coupon_domain.Coupon) {
				c.Status = types.StatusArchived
				// Also expired — not-published must win because it is checked first
				c.RedeemBefore = lo.ToPtr(s.now.Add(-time.Hour))
			},
			subscription: func() *subscription.Subscription { return nil },
			expectedCode: lo.ToPtr(types.CouponValidationErrorCodeNotPublished),
		},
		{
			name: "coupon_with_future_redeem_after_fails_not_active",
			mutateCoupon: func(c *coupon_domain.Coupon) {
				c.RedeemAfter = lo.ToPtr(s.now.Add(24 * time.Hour))
			},
			subscription: func() *subscription.Subscription { return nil },
			expectedCode: lo.ToPtr(types.CouponValidationErrorCodeNotActive),
		},
		{
			name: "coupon_with_past_redeem_before_fails_expired",
			mutateCoupon: func(c *coupon_domain.Coupon) {
				c.RedeemBefore = lo.ToPtr(s.now.Add(-24 * time.Hour))
			},
			subscription: func() *subscription.Subscription { return nil },
			expectedCode: lo.ToPtr(types.CouponValidationErrorCodeExpired),
		},
		{
			name: "coupon_inside_redemption_window_passes",
			mutateCoupon: func(c *coupon_domain.Coupon) {
				c.RedeemAfter = lo.ToPtr(s.now.Add(-24 * time.Hour))
				c.RedeemBefore = lo.ToPtr(s.now.Add(24 * time.Hour))
			},
			subscription: func() *subscription.Subscription { return nil },
			expectedCode: nil,
		},
		{
			name: "fixed_coupon_with_currency_mismatch_fails",
			mutateCoupon: func(c *coupon_domain.Coupon) {
				amt := decimal.RequireFromString("5")
				c.Type = types.CouponTypeFixed
				c.PercentageOff = nil
				c.AmountOff = &amt
				c.Currency = "eur"
			},
			subscription: func() *subscription.Subscription { return s.newSubscription("usd") },
			expectedCode: lo.ToPtr(types.CouponValidationErrorCodeCurrencyMismatch),
		},
		{
			name: "fixed_coupon_with_matching_currency_case_insensitive_passes",
			mutateCoupon: func(c *coupon_domain.Coupon) {
				amt := decimal.RequireFromString("5")
				c.Type = types.CouponTypeFixed
				c.PercentageOff = nil
				c.AmountOff = &amt
				c.Currency = "USD"
			},
			subscription: func() *subscription.Subscription { return s.newSubscription("usd") },
			expectedCode: nil,
		},
		{
			name: "fixed_coupon_without_currency_is_currency_agnostic",
			mutateCoupon: func(c *coupon_domain.Coupon) {
				amt := decimal.RequireFromString("5")
				c.Type = types.CouponTypeFixed
				c.PercentageOff = nil
				c.AmountOff = &amt
				c.Currency = ""
			},
			subscription: func() *subscription.Subscription { return s.newSubscription("usd") },
			expectedCode: nil,
		},
		{
			name: "percentage_coupon_skips_currency_check",
			mutateCoupon: func(c *coupon_domain.Coupon) {
				c.Currency = "eur"
			},
			subscription: func() *subscription.Subscription { return s.newSubscription("usd") },
			expectedCode: nil,
		},
		{
			name: "coupon_at_max_redemptions_fails",
			mutateCoupon: func(c *coupon_domain.Coupon) {
				c.MaxRedemptions = lo.ToPtr(3)
				c.TotalRedemptions = 3
			},
			subscription: func() *subscription.Subscription { return nil },
			expectedCode: lo.ToPtr(types.CouponValidationErrorCodeRedemptionLimitReached),
		},
		{
			name: "coupon_over_max_redemptions_fails",
			mutateCoupon: func(c *coupon_domain.Coupon) {
				c.MaxRedemptions = lo.ToPtr(3)
				c.TotalRedemptions = 4
			},
			subscription: func() *subscription.Subscription { return nil },
			expectedCode: lo.ToPtr(types.CouponValidationErrorCodeRedemptionLimitReached),
		},
		{
			name: "coupon_under_max_redemptions_passes",
			mutateCoupon: func(c *coupon_domain.Coupon) {
				c.MaxRedemptions = lo.ToPtr(3)
				c.TotalRedemptions = 2
			},
			subscription: func() *subscription.Subscription { return nil },
			expectedCode: nil,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			c := s.newValidPercentageCoupon()
			tc.mutateCoupon(&c)
			err := s.service.ValidateCoupon(s.GetContext(), c, tc.subscription())
			if tc.expectedCode == nil {
				s.NoError(err)
				return
			}
			s.assertValidationErrorCode(err, *tc.expectedCode)
		})
	}
}

// ---------------------------------------------------------------------------
// ValidateCoupon — cadence rules
// ---------------------------------------------------------------------------

func (s *CouponValidationServiceSuite) TestValidateCoupon_OnceCadence() {
	s.Run("once_cadence_with_no_prior_applications_passes", func() {
		c := s.newValidPercentageCoupon()
		sub := s.newSubscription("usd")
		s.NoError(s.service.ValidateCoupon(s.GetContext(), c, sub))
	})

	s.Run("once_cadence_with_multiple_prior_applications_fails", func() {
		c := s.newValidPercentageCoupon()
		sub := s.newSubscription("usd")
		s.addCouponApplications(c.ID, sub.ID, 2)

		err := s.service.ValidateCoupon(s.GetContext(), c, sub)
		s.assertValidationErrorCode(err, types.CouponValidationErrorCodeOnceCadenceViolation)
	})

	s.Run("once_cadence_only_counts_applications_of_same_subscription", func() {
		c := s.newValidPercentageCoupon()
		sub := s.newSubscription("usd")
		otherSub := s.newSubscription("usd")
		s.addCouponApplications(c.ID, otherSub.ID, 2)

		s.NoError(s.service.ValidateCoupon(s.GetContext(), c, sub))
	})
}

func (s *CouponValidationServiceSuite) TestValidateCoupon_ForeverCadence() {
	s.Run("forever_cadence_passes_regardless_of_prior_applications", func() {
		c := s.newValidPercentageCoupon()
		c.Cadence = types.CouponCadenceForever
		sub := s.newSubscription("usd")
		s.addCouponApplications(c.ID, sub.ID, 5)

		s.NoError(s.service.ValidateCoupon(s.GetContext(), c, sub))
	})
}

func (s *CouponValidationServiceSuite) TestValidateCoupon_RepeatedCadence() {
	testCases := []struct {
		name              string
		durationInPeriods *int
		priorApplications int
		expectedCode      *types.CouponValidationErrorCode
	}{
		{
			name:              "repeated_cadence_without_duration_fails",
			durationInPeriods: nil,
			priorApplications: 0,
			expectedCode:      lo.ToPtr(types.CouponValidationErrorCodeInvalidRepeatedCadence),
		},
		{
			name:              "repeated_cadence_with_zero_duration_fails",
			durationInPeriods: lo.ToPtr(0),
			priorApplications: 0,
			expectedCode:      lo.ToPtr(types.CouponValidationErrorCodeInvalidRepeatedCadence),
		},
		{
			name:              "repeated_cadence_under_duration_limit_passes",
			durationInPeriods: lo.ToPtr(3),
			priorApplications: 2,
			expectedCode:      nil,
		},
		{
			name:              "repeated_cadence_at_duration_limit_fails",
			durationInPeriods: lo.ToPtr(3),
			priorApplications: 3,
			expectedCode:      lo.ToPtr(types.CouponValidationErrorCodeRepeatedCadenceLimitReached),
		},
		{
			name:              "repeated_cadence_over_duration_limit_fails",
			durationInPeriods: lo.ToPtr(2),
			priorApplications: 4,
			expectedCode:      lo.ToPtr(types.CouponValidationErrorCodeRepeatedCadenceLimitReached),
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			c := s.newValidPercentageCoupon()
			c.Cadence = types.CouponCadenceRepeated
			c.DurationInPeriods = tc.durationInPeriods
			sub := s.newSubscription("usd")
			if tc.priorApplications > 0 {
				s.addCouponApplications(c.ID, sub.ID, tc.priorApplications)
			}

			err := s.service.ValidateCoupon(s.GetContext(), c, sub)
			if tc.expectedCode == nil {
				s.NoError(err)
				return
			}
			s.assertValidationErrorCode(err, *tc.expectedCode)
		})
	}
}

func (s *CouponValidationServiceSuite) TestValidateCoupon_InvalidCadence() {
	s.Run("unknown_cadence_with_subscription_fails", func() {
		c := s.newValidPercentageCoupon()
		c.Cadence = types.CouponCadence("bogus")
		sub := s.newSubscription("usd")

		err := s.service.ValidateCoupon(s.GetContext(), c, sub)
		s.assertValidationErrorCode(err, types.CouponValidationErrorCodeInvalidCadence)
	})

	s.Run("unknown_cadence_without_subscription_skips_cadence_validation", func() {
		c := s.newValidPercentageCoupon()
		c.Cadence = types.CouponCadence("bogus")

		s.NoError(s.service.ValidateCoupon(s.GetContext(), c, nil))
	})
}
