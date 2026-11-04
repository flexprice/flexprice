package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/coupon"
	"github.com/flexprice/flexprice/internal/domain/coupon_association"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// The analytics pricer must reproduce the invoice: each billing period in the range priced like billing.
func (s *BillingServiceSuite) TestPriceSubscriptions_MatchesInvoicePerPeriod() {
	s.BaseServiceTestSuite.ClearStores()
	ctx := s.GetContext()
	jan1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	feb1 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	mar1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	apr1 := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	cust := &customer.Customer{ID: "cust_av2", ExternalID: "ext_av2", Name: "AV2", BaseModel: types.GetDefaultBaseModel(ctx)}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, cust))
	m := &meter.Meter{ID: "meter_av2", Name: "Calls", EventName: "av2_call",
		Aggregation: meter.Aggregation{Type: types.AggregationSum}, BaseModel: types.GetDefaultBaseModel(ctx)}
	s.Require().NoError(s.GetStores().MeterRepo.CreateMeter(ctx, m))
	pr := &price.Price{
		ID: "price_av2", Amount: decimal.NewFromFloat(0.01), Currency: "usd",
		Type: types.PRICE_TYPE_USAGE, BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		BillingModel: types.BILLING_MODEL_FLAT_FEE, BillingCadence: types.BILLING_CADENCE_RECURRING,
		InvoiceCadence: types.InvoiceCadenceArrear, MeterID: m.ID, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, pr))
	sub := &subscription.Subscription{
		ID: "sub_av2", CustomerID: cust.ID, StartDate: jan1, BillingAnchor: jan1,
		CurrentPeriodStart: mar1, CurrentPeriodEnd: apr1, Currency: "usd",
		BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1, BillingCycle: types.BillingCycleAnniversary,
		SubscriptionStatus: types.SubscriptionStatusActive, Timezone: "UTC", BaseModel: types.GetDefaultBaseModel(ctx),
	}
	li := &subscription.SubscriptionLineItem{
		ID: types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM), SubscriptionID: sub.ID,
		CustomerID: cust.ID, PriceID: pr.ID, PriceType: types.PRICE_TYPE_USAGE, MeterID: m.ID,
		DisplayName: "Calls", Quantity: decimal.Zero, Currency: "usd",
		BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1, InvoiceCadence: types.InvoiceCadenceArrear,
		StartDate: jan1, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, []*subscription.SubscriptionLineItem{li}))
	sub.LineItems = []*subscription.SubscriptionLineItem{li}

	for i, qty := range []int64{100, 200, 300} {
		id := s.GetUUID()
		ts := time.Date(2026, time.Month(i+1), 15, 12, 0, 0, 0, time.UTC)
		s.Require().NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{{
			Event: events.Event{ID: id, TenantID: sub.TenantID, EnvironmentID: sub.EnvironmentID, EventName: m.EventName,
				ExternalCustomerID: cust.ExternalID, CustomerID: cust.ID, Timestamp: ts, IngestedAt: ts},
			MeterID: m.ID, QtyTotal: decimal.NewFromInt(qty), UniqueHash: id,
		}}))
	}

	billing := s.service.(*billingService)
	usageSvc := &meterUsageService{ServiceParams: billing.ServiceParams, repo: billing.MeterUsageRepo, logger: billing.Logger}

	// Feb 10 - Mar 20 touches the February and March billing periods; January must not be priced.
	priced, err := usageSvc.priceSubscriptions(ctx, []*subscription.Subscription{sub},
		time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC), time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC))
	s.Require().NoError(err)
	s.Require().Len(priced.lines, 2)
	s.Equal(2, priced.periods)

	for i, period := range [][2]time.Time{{feb1, mar1}, {mar1, apr1}} {
		invoice, err := billing.calculateMeterUsageCharges(ctx, sub, sub.LineItems, period[0], period[1], true, nil)
		s.Require().NoError(err)

		line := priced.lines[i]
		s.Equal(li.ID, line.key.lineItemID)
		s.Equal(period[0], line.key.windowStart)
		s.Equal(period[1], line.key.windowEnd)
		s.True(invoice.TotalAmount.Equal(line.total()), "period %d: invoice %s vs pricer %s", i, invoice.TotalAmount, line.total())
	}
	s.True(decimal.NewFromFloat(2).Equal(priced.lines[0].total()), "February: 200 x $0.01")
	s.True(decimal.NewFromFloat(3).Equal(priced.lines[1].total()), "March: 300 x $0.01")
	s.True(priced.lines[0].discount.IsZero())

	// A 10% line coupon from March 2 discounts the March period only, as the invoices would.
	c := &coupon.Coupon{
		ID: "coupon_av2", Name: "Ten", Type: types.CouponTypePercentage, PercentageOff: lo.ToPtr(decimal.NewFromInt(10)),
		Cadence: types.CouponCadenceForever, Currency: "usd", EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CouponRepo.Create(ctx, c))
	s.Require().NoError(s.GetStores().CouponAssociationRepo.Create(ctx, &coupon_association.CouponAssociation{
		ID: "ca_av2", CouponID: c.ID, SubscriptionID: sub.ID, SubscriptionLineItemID: lo.ToPtr(li.ID),
		StartDate: mar1.AddDate(0, 0, 1), EnvironmentID: types.GetEnvironmentID(ctx), Coupon: c, BaseModel: types.GetDefaultBaseModel(ctx),
	}))

	priced, err = usageSvc.priceSubscriptions(ctx, []*subscription.Subscription{sub},
		time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC), time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC))
	s.Require().NoError(err)
	s.True(priced.lines[0].discount.IsZero(), "February is before the coupon starts")
	s.True(decimal.NewFromFloat(0.3).Equal(priced.lines[1].discount), "March: 10%% of $3, got %s", priced.lines[1].discount)
}
