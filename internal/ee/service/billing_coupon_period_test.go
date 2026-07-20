package service

// billing_coupon_period_test.go — regression test for a bug found by code
// review: CreateInvoiceRequestForCharges selected coupons using
// sub.CurrentPeriodStart/CurrentPeriodEnd instead of the invoice's actually
// REQUESTED period (params.PeriodStart/PeriodEnd). Retries, backfills, or
// delayed invoice generation for a period other than the subscription's
// current one would then attach the wrong (or no) coupon associations.
//
// This constructs a subscription whose CurrentPeriodStart/End differ from the
// requested invoice period, with a coupon association active ONLY during the
// requested period — proving the fix uses the correct period, not merely that
// the line executes.

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/coupon"
	"github.com/flexprice/flexprice/internal/domain/coupon_association"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type BillingCouponPeriodSuite struct {
	testutil.BaseServiceTestSuite
	svc BillingService
}

func TestBillingCouponPeriod(t *testing.T) {
	suite.Run(t, new(BillingCouponPeriodSuite))
}

func (s *BillingCouponPeriodSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.svc = NewBillingService(ServiceParams{
		Logger:                   s.GetLogger(),
		Config:                   s.GetConfig(),
		DB:                       s.GetDB(),
		SubRepo:                  s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo: s.GetStores().SubscriptionLineItemRepo,
		PlanRepo:                 s.GetStores().PlanRepo,
		PriceRepo:                s.GetStores().PriceRepo,
		EventRepo:                s.GetStores().EventRepo,
		MeterRepo:                s.GetStores().MeterRepo,
		CustomerRepo:             s.GetStores().CustomerRepo,
		InvoiceRepo:              s.GetStores().InvoiceRepo,
		EntitlementRepo:          s.GetStores().EntitlementRepo,
		EnvironmentRepo:          s.GetStores().EnvironmentRepo,
		FeatureRepo:              s.GetStores().FeatureRepo,
		TenantRepo:               s.GetStores().TenantRepo,
		UserRepo:                 s.GetStores().UserRepo,
		AuthRepo:                 s.GetStores().AuthRepo,
		WalletRepo:               s.GetStores().WalletRepo,
		PaymentRepo:              s.GetStores().PaymentRepo,
		CouponAssociationRepo:    s.GetStores().CouponAssociationRepo,
		CouponRepo:               s.GetStores().CouponRepo,
		CouponApplicationRepo:    s.GetStores().CouponApplicationRepo,
		AddonAssociationRepo:     s.GetStores().AddonAssociationRepo,
		TaxRateRepo:              s.GetStores().TaxRateRepo,
		TaxAssociationRepo:       s.GetStores().TaxAssociationRepo,
		TaxAppliedRepo:           s.GetStores().TaxAppliedRepo,
		SettingsRepo:             s.GetStores().SettingsRepo,
		EventPublisher:           s.GetPublisher(),
		WebhookPublisher:         s.GetWebhookPublisher(),
		ProrationCalculator:      s.GetCalculator(),
		AlertLogsRepo:            s.GetStores().AlertLogsRepo,
		MeterUsageRepo:           s.GetStores().MeterUsageRepo,
	})
}

// TestCouponSelectedForRequestedPeriodNotCurrentPeriod is the regression case:
// the subscription's CurrentPeriodStart/End is Jan 1 - Feb 1, but the invoice is
// being generated for the NEXT period (Feb 1 - Mar 1) — a plausible retry/backfill
// scenario. A subscription-level coupon is active ONLY Feb 1 onward. Before the
// fix, coupon selection queried Jan 1 - Feb 1 (sub.CurrentPeriodStart/End) and
// would have missed this coupon entirely.
func (s *BillingCouponPeriodSuite) TestCouponSelectedForRequestedPeriodNotCurrentPeriod() {
	ctx := s.GetContext()

	cust := &customer.Customer{ID: "cust_period_1", ExternalID: "ext_period_1", Name: "Period Test Customer", BaseModel: types.GetDefaultBaseModel(ctx)}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))

	currentPeriodStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	currentPeriodEnd := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	requestedPeriodStart := currentPeriodEnd // the NEXT period, not the sub's current one
	requestedPeriodEnd := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	sub := &subscription.Subscription{
		ID: "sub_period_1", CustomerID: cust.ID, Currency: "usd",
		SubscriptionStatus: types.SubscriptionStatusActive,
		CurrentPeriodStart: currentPeriodStart, CurrentPeriodEnd: currentPeriodEnd,
		BillingAnchor: currentPeriodStart, StartDate: currentPeriodStart,
		BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(ctx, sub))

	c := &coupon.Coupon{
		ID: "coupon_period_1", Name: "Next Period Only", Type: types.CouponTypePercentage,
		PercentageOff: lo.ToPtr(decimal.NewFromFloat(10)), Cadence: types.CouponCadenceForever,
		Currency: "usd", EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CouponRepo.Create(ctx, c))

	// StartDate is a full week into the requested period (not exactly at the period
	// boundary) so the two scenarios can't coincide under the inclusive-boundary
	// ActiveOnly semantics — it's unambiguously outside [currentPeriodStart,
	// currentPeriodEnd) and unambiguously inside [requestedPeriodStart, requestedPeriodEnd).
	couponStart := requestedPeriodStart.AddDate(0, 0, 7)
	assoc := &coupon_association.CouponAssociation{
		ID: "assoc_period_1", CouponID: c.ID, SubscriptionID: sub.ID,
		StartDate:     couponStart,
		EnvironmentID: types.GetEnvironmentID(ctx), Coupon: c, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CouponAssociationRepo.Create(ctx, assoc))

	result := &dto.BillingCalculationResult{
		TotalAmount: decimal.NewFromInt(100),
		Currency:    "usd",
		FixedCharges: []dto.CreateInvoiceLineItemRequest{
			{Amount: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(1)},
		},
		UsageCharges: []dto.CreateInvoiceLineItemRequest{},
	}

	req, err := s.svc.CreateInvoiceRequestForCharges(ctx, &dto.CreateInvoiceRequestForChargesParams{
		Subscription: sub,
		Result:       result,
		PeriodStart:  requestedPeriodStart,
		PeriodEnd:    requestedPeriodEnd,
	})
	s.NoError(err)
	s.Require().NotNil(req)

	s.Require().Len(req.InvoiceCoupons, 1, "coupon active only during the REQUESTED period should be selected")
	s.Equal(c.ID, req.InvoiceCoupons[0].CouponID)

	// Sanity: the invoice request itself reflects the requested period, not the sub's current one.
	s.Require().NotNil(req.PeriodStart)
	s.True(req.PeriodStart.Equal(requestedPeriodStart), "req.PeriodStart want %s got %s", requestedPeriodStart, *req.PeriodStart)
}

// TestCouponNotSelectedForSubCurrentPeriodOnly is the inverse: a coupon active
// ONLY during the subscription's stale current period (not the requested one)
// must NOT be selected — proving the fix didn't just widen the window to match
// either period, but specifically uses the requested one.
func (s *BillingCouponPeriodSuite) TestCouponNotSelectedForSubCurrentPeriodOnly() {
	ctx := s.GetContext()

	cust := &customer.Customer{ID: "cust_period_2", ExternalID: "ext_period_2", Name: "Period Test Customer 2", BaseModel: types.GetDefaultBaseModel(ctx)}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))

	currentPeriodStart := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	currentPeriodEnd := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	requestedPeriodStart := currentPeriodEnd
	requestedPeriodEnd := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	sub := &subscription.Subscription{
		ID: "sub_period_2", CustomerID: cust.ID, Currency: "usd",
		SubscriptionStatus: types.SubscriptionStatusActive,
		CurrentPeriodStart: currentPeriodStart, CurrentPeriodEnd: currentPeriodEnd,
		BillingAnchor: currentPeriodStart, StartDate: currentPeriodStart,
		BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(ctx, sub))

	c := &coupon.Coupon{
		ID: "coupon_period_2", Name: "Current Period Only", Type: types.CouponTypePercentage,
		PercentageOff: lo.ToPtr(decimal.NewFromFloat(10)), Cadence: types.CouponCadenceForever,
		Currency: "usd", EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CouponRepo.Create(ctx, c))

	endsBeforeRequestedPeriod := requestedPeriodStart.Add(-time.Second)
	assoc := &coupon_association.CouponAssociation{
		ID: "assoc_period_2", CouponID: c.ID, SubscriptionID: sub.ID,
		StartDate: currentPeriodStart, EndDate: &endsBeforeRequestedPeriod, // expires right before the requested period starts
		EnvironmentID: types.GetEnvironmentID(ctx), Coupon: c, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CouponAssociationRepo.Create(ctx, assoc))

	result := &dto.BillingCalculationResult{
		TotalAmount: decimal.NewFromInt(100), Currency: "usd",
		FixedCharges: []dto.CreateInvoiceLineItemRequest{{Amount: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(1)}},
		UsageCharges: []dto.CreateInvoiceLineItemRequest{},
	}

	req, err := s.svc.CreateInvoiceRequestForCharges(ctx, &dto.CreateInvoiceRequestForChargesParams{
		Subscription: sub, Result: result, PeriodStart: requestedPeriodStart, PeriodEnd: requestedPeriodEnd,
	})
	s.NoError(err)
	s.Require().NotNil(req)
	s.Empty(req.InvoiceCoupons, "coupon expired before the requested period starts must not be selected")
}
