package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// analyticsV2Fixture is a customer on one monthly subscription with a SUM meter ($0.01/call) and a MAX meter
// ($1 per GB of peak), with February events from two apps.
type analyticsV2Fixture struct {
	sub          *subscription.Subscription
	feb1, mar1   time.Time
	calls, store *meter.Meter
}

func (s *BillingServiceSuite) setupAnalyticsV2Fixture() *analyticsV2Fixture {
	s.BaseServiceTestSuite.ClearStores()
	ctx := s.GetContext()
	f := &analyticsV2Fixture{
		feb1: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		mar1: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
	}
	jan1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	cust := &customer.Customer{ID: "cust_qa", ExternalID: "ext_qa", Name: "QA", Timezone: "UTC", BaseModel: types.GetDefaultBaseModel(ctx)}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, cust))

	f.calls = &meter.Meter{ID: "meter_calls", Name: "Calls", EventName: "call",
		Aggregation: meter.Aggregation{Type: types.AggregationSum}, BaseModel: types.GetDefaultBaseModel(ctx)}
	f.store = &meter.Meter{ID: "meter_storage", Name: "Storage", EventName: "storage",
		Aggregation: meter.Aggregation{Type: types.AggregationMax}, BaseModel: types.GetDefaultBaseModel(ctx)}
	s.Require().NoError(s.GetStores().MeterRepo.CreateMeter(ctx, f.calls))
	s.Require().NoError(s.GetStores().MeterRepo.CreateMeter(ctx, f.store))

	f.sub = &subscription.Subscription{
		ID: "sub_qa", CustomerID: cust.ID, StartDate: jan1, BillingAnchor: jan1,
		CurrentPeriodStart: f.feb1, CurrentPeriodEnd: f.mar1, Currency: "usd",
		BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1, BillingCycle: types.BillingCycleAnniversary,
		SubscriptionStatus: types.SubscriptionStatusActive, SubscriptionType: types.SubscriptionTypeStandalone,
		Timezone: "UTC", BaseModel: types.GetDefaultBaseModel(ctx),
	}
	var lines []*subscription.SubscriptionLineItem
	for _, spec := range []struct {
		m      *meter.Meter
		amount string
	}{{f.calls, "0.01"}, {f.store, "1"}} {
		pr := &price.Price{
			ID: "price_" + spec.m.ID, Amount: decimal.RequireFromString(spec.amount), Currency: "usd",
			Type: types.PRICE_TYPE_USAGE, BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
			BillingModel: types.BILLING_MODEL_FLAT_FEE, BillingCadence: types.BILLING_CADENCE_RECURRING,
			InvoiceCadence: types.InvoiceCadenceArrear, MeterID: spec.m.ID, BaseModel: types.GetDefaultBaseModel(ctx),
		}
		s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, pr))
		lines = append(lines, &subscription.SubscriptionLineItem{
			ID: "li_" + spec.m.ID, SubscriptionID: f.sub.ID, CustomerID: cust.ID, PriceID: pr.ID,
			PriceType: types.PRICE_TYPE_USAGE, MeterID: spec.m.ID, DisplayName: spec.m.Name, Quantity: decimal.Zero,
			Currency: "usd", BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
			InvoiceCadence: types.InvoiceCadenceArrear, StartDate: jan1, BaseModel: types.GetDefaultBaseModel(ctx),
		})
	}
	s.Require().NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, f.sub, lines))
	f.sub.LineItems = lines

	// payout: 300 calls (build 100, run 200), storage peak 8. support: 100 calls (run), storage peak 2.
	var usage []*events.MeterUsage
	add := func(m *meter.Meter, app, kind string, day int, qty int64) {
		ts := time.Date(2026, 2, day, 12, 0, 0, 0, time.UTC)
		usage = append(usage, &events.MeterUsage{
			Event: events.Event{ID: s.GetUUID(), TenantID: f.sub.TenantID, EnvironmentID: f.sub.EnvironmentID, EventName: m.EventName,
				ExternalCustomerID: cust.ExternalID, CustomerID: cust.ID, Timestamp: ts, IngestedAt: ts,
				Properties: map[string]interface{}{"app": app, "type": kind}},
			MeterID: m.ID, QtyTotal: decimal.NewFromInt(qty),
		})
	}
	add(f.calls, "payout", "build", 3, 100)
	add(f.calls, "payout", "run", 10, 200)
	add(f.calls, "support", "run", 10, 100)
	add(f.store, "payout", "run", 5, 8)
	add(f.store, "payout", "run", 6, 3)
	add(f.store, "support", "run", 5, 2)
	s.Require().NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, usage))
	return f
}

func (s *BillingServiceSuite) queryAnalyticsV2(f *analyticsV2Fixture, mutate func(r *dto.GetUsageAnalyticsV2Request)) *dto.GetUsageAnalyticsV2Response {
	req := &dto.GetUsageAnalyticsV2Request{
		ExternalCustomerID: "ext_qa", StartTime: f.feb1, EndTime: f.mar1, GroupBy: []string{"properties.app"},
		DataThrough: lo.ToPtr(f.mar1.Add(time.Hour)),
	}
	if mutate != nil {
		mutate(req)
	}
	req.ApplyDefaults(f.mar1)
	s.Require().NoError(req.Validate())

	billing := s.service.(*billingService)
	usageSvc := &meterUsageService{ServiceParams: billing.ServiceParams, repo: billing.MeterUsageRepo, logger: billing.Logger}
	resp, err := usageSvc.GetUsageAnalyticsV2(s.GetContext(), req)
	s.Require().NoError(err)
	return resp
}

func (s *BillingServiceSuite) requireMoney(want string, got decimal.Decimal, what string) {
	s.Truef(decimal.RequireFromString(want).Equal(got), "%s: want %s got %s", what, want, got)
}

func (s *BillingServiceSuite) TestGetUsageAnalyticsV2_RanksAndReconcilesToInvoice() {
	f := s.setupAnalyticsV2Fixture()
	ctx := s.GetContext()

	invoice, err := s.service.(*billingService).calculateMeterUsageCharges(ctx, f.sub, f.sub.LineItems, f.feb1, f.mar1, true, nil)
	s.Require().NoError(err)
	// calls: 400 x $0.01 = $4; storage: peak 8 x $1 = $8.
	s.requireMoney("12", invoice.TotalAmount, "invoice")

	resp := s.queryAnalyticsV2(f, func(r *dto.GetUsageAnalyticsV2Request) { r.Expand = []string{"price", "plan"} })
	s.Equal(int64(2), resp.TotalCount)
	s.requireMoney("12", resp.Totals.Cost, "totals match the invoice")
	s.requireMoney("12", resp.Totals.RowsCost, "all cost is on rows")
	s.Require().Len(resp.Items, 2)

	// payout: 300 calls = $3 + storage peak 8 of row peaks 8+2 -> $8 x 0.8 = $6.40.
	payout, support := resp.Items[0], resp.Items[1]
	s.Equal("payout", payout.Group["app"])
	s.requireMoney("9.4", payout.Cost, "payout")
	s.requireMoney("2.6", support.Cost, "support: $1 calls + $1.60 storage")
	s.Nil(payout.Usage, "rows mix calls and GB, so usage is not summed")
	s.Equal([]string{"price_meter_calls", "price_meter_storage"}, payout.PriceIDs)
	s.Equal([]string{"sub_qa"}, payout.SubscriptionIDs)
	s.Len(resp.Prices, 2)
	s.Empty(resp.Unattributed.Charges)
	s.Empty(resp.ExcludedMeters)

	s.Run("page size 1 keeps the total count", func() {
		page := s.queryAnalyticsV2(f, func(r *dto.GetUsageAnalyticsV2Request) { r.Limit = 1; r.Offset = 1 })
		s.Equal(int64(2), page.TotalCount)
		s.Require().Len(page.Items, 1)
		s.Equal("support", page.Items[0].Group["app"])
	})

	s.Run("a filtered row costs the same", func() {
		filtered := s.queryAnalyticsV2(f, func(r *dto.GetUsageAnalyticsV2Request) {
			r.Filters = []dto.AnalyticsV2Filter{{Key: "properties.app", Op: dto.AnalyticsV2FilterOpNotIn, Values: []string{"payout"}}}
		})
		s.Require().Len(filtered.Items, 1)
		s.requireMoney("2.6", filtered.Items[0].Cost, "support unchanged by filtering out payout")
		s.requireMoney("2.6", filtered.Totals.Cost, "filtered totals are the rows")
	})

	s.Run("build/run columns add up to each row", func() {
		cols := s.queryAnalyticsV2(f, func(r *dto.GetUsageAnalyticsV2Request) {
			r.MeterIDs = []string{"meter_calls"}
			r.Columns = "properties.type"
			r.Sort = &dto.AnalyticsV2Sort{By: dto.AnalyticsV2SortByColumn, Column: "run"}
		})
		s.Equal([]string{"build", "run"}, cols.ColumnValues)
		payout := cols.Items[0]
		s.requireMoney("1", payout.Columns["build"].Cost, "payout build")
		s.requireMoney("2", payout.Columns["run"].Cost, "payout run")
		s.requireMoney("3", payout.Cost, "payout calls")
		s.Require().NotNil(payout.Usage, "one meter: usage is shown")
		s.requireMoney("300", *payout.Usage, "payout calls")
	})

	s.Run("daily points add up to each row", func() {
		daily := s.queryAnalyticsV2(f, func(r *dto.GetUsageAnalyticsV2Request) {
			r.WindowSize = types.WindowSizeDay
			r.Metrics = []string{"distinct:properties.type"}
		})
		for _, it := range daily.Items {
			sum := decimal.Zero
			for _, p := range it.Points {
				sum = sum.Add(p.Cost)
			}
			s.requireMoney(it.Cost.String(), sum, "points of "+it.Group["app"])
		}
		s.Equal(uint64(2), *daily.Items[0].Metrics["distinct:properties.type"].Count)
		s.Equal([]string{"build", "run"}, daily.Items[0].Metrics["distinct:properties.type"].Values)
		total := decimal.Zero
		for _, p := range daily.Totals.Points {
			total = total.Add(p.Cost)
		}
		s.requireMoney("12", total, "all-rows points")
	})
}

func (s *BillingServiceSuite) TestGetUsageAnalyticsV2_ExcludedMetersAndGuards() {
	f := s.setupAnalyticsV2Fixture()
	ctx := s.GetContext()

	// A count-unique meter is priced but not split across rows: it is reported, and totals still match the invoice.
	users := &meter.Meter{ID: "meter_users", Name: "Users", EventName: "user",
		Aggregation: meter.Aggregation{Type: types.AggregationCountUnique, Field: "user"}, BaseModel: types.GetDefaultBaseModel(ctx)}
	s.Require().NoError(s.GetStores().MeterRepo.CreateMeter(ctx, users))
	pr := &price.Price{
		ID: "price_meter_users", Amount: decimal.NewFromInt(5), Currency: "usd",
		Type: types.PRICE_TYPE_USAGE, BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		BillingModel: types.BILLING_MODEL_FLAT_FEE, BillingCadence: types.BILLING_CADENCE_RECURRING,
		InvoiceCadence: types.InvoiceCadenceArrear, MeterID: users.ID, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, pr))
	li := &subscription.SubscriptionLineItem{
		ID: "li_meter_users", SubscriptionID: f.sub.ID, CustomerID: f.sub.CustomerID, PriceID: pr.ID,
		PriceType: types.PRICE_TYPE_USAGE, MeterID: users.ID, DisplayName: "Users", Quantity: decimal.Zero,
		Currency: "usd", BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		InvoiceCadence: types.InvoiceCadenceArrear, StartDate: f.sub.StartDate, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li))
	f.sub.LineItems = append(f.sub.LineItems, li)
	for i, u := range []string{"u1", "u2", "u1"} {
		ts := time.Date(2026, 2, 7+i, 0, 0, 0, 0, time.UTC)
		s.Require().NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{{
			Event: events.Event{ID: s.GetUUID(), TenantID: f.sub.TenantID, EnvironmentID: f.sub.EnvironmentID, EventName: users.EventName,
				ExternalCustomerID: "ext_qa", CustomerID: f.sub.CustomerID, Timestamp: ts, IngestedAt: ts,
				Properties: map[string]interface{}{"app": "payout", "user": u}},
			MeterID: users.ID, QtyTotal: decimal.NewFromInt(1), UniqueHash: u,
		}}))
	}

	invoice, err := s.service.(*billingService).calculateMeterUsageCharges(ctx, f.sub, f.sub.LineItems, f.feb1, f.mar1, true, nil)
	s.Require().NoError(err)
	resp := s.queryAnalyticsV2(f, nil)
	s.Require().Len(resp.ExcludedMeters, 1)
	ex := resp.ExcludedMeters[0]
	s.Equal("meter_users", ex.MeterID)
	s.Equal(dto.AnalyticsV2ReasonAggregationNotSupported, ex.Reason)
	s.Require().NotNil(ex.Cost)
	s.True(ex.Cost.IsPositive())
	s.requireMoney(invoice.TotalAmount.String(), resp.Totals.Cost, "rows + excluded = invoice")
	s.requireMoney("12", resp.Totals.RowsCost, "rows carry only the meters v2 can split")

	filtered := s.queryAnalyticsV2(f, func(r *dto.GetUsageAnalyticsV2Request) { r.Search = "pay" })
	s.Require().Len(filtered.ExcludedMeters, 1)
	s.Nil(filtered.ExcludedMeters[0].Cost, "a filtered view cannot tell the excluded meter's share")

	s.Run("parent or child subscription is rejected", func() {
		f.sub.SubscriptionType = types.SubscriptionTypeParent
		s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, f.sub))
		req := &dto.GetUsageAnalyticsV2Request{ExternalCustomerID: "ext_qa", StartTime: f.feb1, EndTime: f.mar1, GroupBy: []string{"properties.app"}}
		req.ApplyDefaults(f.mar1)
		billing := s.service.(*billingService)
		usageSvc := &meterUsageService{ServiceParams: billing.ServiceParams, repo: billing.MeterUsageRepo, logger: billing.Logger}
		_, err := usageSvc.GetUsageAnalyticsV2(ctx, req)
		s.Require().Error(err)
		s.True(ierr.IsValidation(err))
	})

	s.Run("customer without subscriptions gets an empty page", func() {
		cust := &customer.Customer{ID: "cust_none", ExternalID: "ext_none", Name: "None", BaseModel: types.GetDefaultBaseModel(ctx)}
		s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, cust))
		req := &dto.GetUsageAnalyticsV2Request{ExternalCustomerID: "ext_none", StartTime: f.feb1, EndTime: f.mar1, GroupBy: []string{"properties.app"}}
		req.ApplyDefaults(f.mar1)
		billing := s.service.(*billingService)
		usageSvc := &meterUsageService{ServiceParams: billing.ServiceParams, repo: billing.MeterUsageRepo, logger: billing.Logger}
		resp, err := usageSvc.GetUsageAnalyticsV2(ctx, req)
		s.Require().NoError(err)
		s.Empty(resp.Items)
		s.True(resp.Totals.Cost.IsZero())
	})
}
