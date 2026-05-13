package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// ---------------------------------------------------------------------------
// Suite setup
// ---------------------------------------------------------------------------

type MeterUsageServiceSuite struct {
	testutil.BaseServiceTestSuite
	svc            MeterUsageService
	meterUsageRepo *testutil.InMemoryMeterUsageStore

	// Shared test entities
	customer     *customer.Customer
	meterAPI     *meter.Meter
	priceAPI     *price.Price
	sub          *subscription.Subscription
	now          time.Time
	periodStart  time.Time
	periodEnd    time.Time
}

func TestMeterUsageService(t *testing.T) {
	suite.Run(t, new(MeterUsageServiceSuite))
}

func (s *MeterUsageServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()

	s.meterUsageRepo = s.GetStores().MeterUsageRepo.(*testutil.InMemoryMeterUsageStore)

	s.now = time.Date(2026, 1, 20, 12, 0, 0, 0, time.UTC)
	s.periodStart = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.periodEnd = time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

	s.setupEntities()

	s.svc = NewMeterUsageService(ServiceParams{
		Logger:                   s.GetLogger(),
		Config:                   s.GetConfig(),
		DB:                       s.GetDB(),
		SubRepo:                  s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo: s.GetStores().SubscriptionLineItemRepo,
		PlanRepo:                 s.GetStores().PlanRepo,
		PriceRepo:                s.GetStores().PriceRepo,
		MeterRepo:                s.GetStores().MeterRepo,
		CustomerRepo:             s.GetStores().CustomerRepo,
		FeatureRepo:              s.GetStores().FeatureRepo,
		MeterUsageRepo:           s.meterUsageRepo,
		EnvironmentRepo:          s.GetStores().EnvironmentRepo,
		TenantRepo:               s.GetStores().TenantRepo,
		EventRepo:                s.GetStores().EventRepo,
		EntitlementRepo:          s.GetStores().EntitlementRepo,
		InvoiceRepo:              s.GetStores().InvoiceRepo,
		WalletRepo:               s.GetStores().WalletRepo,
		UserRepo:                 s.GetStores().UserRepo,
		AuthRepo:                 s.GetStores().AuthRepo,
	})
}

func (s *MeterUsageServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
	s.meterUsageRepo.Clear()
}

// setupEntities creates a customer, meter, price, and subscription used by
// all test cases. Individual tests add line items and meter_usage records.
func (s *MeterUsageServiceSuite) setupEntities() {
	ctx := s.GetContext()

	s.customer = &customer.Customer{
		ID:         "cust_1",
		ExternalID: "ext_cust_1",
		Name:       "Test Customer",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, s.customer))

	s.meterAPI = &meter.Meter{
		ID:        "meter_api",
		Name:      "API Calls",
		EventName: "api_call",
		Aggregation: meter.Aggregation{
			Type: types.AggregationSum,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, s.meterAPI))

	s.priceAPI = &price.Price{
		ID:             "price_api",
		Amount:         decimal.NewFromFloat(0.01), // $0.01 per unit
		Currency:       "usd",
		EntityType:     types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:       "plan_1",
		BillingModel:   types.BILLING_MODEL_FLAT_FEE,
		Type:           types.PRICE_TYPE_USAGE,
		MeterID:        s.meterAPI.ID,
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, s.priceAPI))

	s.sub = &subscription.Subscription{
		ID:                 "sub_1",
		CustomerID:         s.customer.ID,
		PlanID:             "plan_1",
		Currency:           "usd",
		SubscriptionStatus: types.SubscriptionStatusActive,
		CurrentPeriodStart: s.periodStart,
		CurrentPeriodEnd:   s.periodEnd,
		BillingAnchor:      s.periodStart,
		StartDate:          s.periodStart,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(ctx, s.sub))
}

// insertMeterUsage is a helper that adds a single meter_usage record.
func (s *MeterUsageServiceSuite) insertMeterUsage(ctx context.Context, meterID, extCustID string, ts time.Time, qty float64) {
	s.NoError(s.meterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{
		{
			Event: events.Event{
				ID:                 types.GenerateUUID(),
				TenantID:           types.GetTenantID(ctx),
				EnvironmentID:      types.GetEnvironmentID(ctx),
				ExternalCustomerID: extCustID,
				Timestamp:          ts,
				EventName:          "api_call",
			},
			MeterID:  meterID,
			QtyTotal: decimal.NewFromFloat(qty),
		},
	}))
}

// createLineItem creates and stores a subscription line item.
func (s *MeterUsageServiceSuite) createLineItem(ctx context.Context, id string, startDate, endDate time.Time) *subscription.SubscriptionLineItem {
	li := &subscription.SubscriptionLineItem{
		ID:             id,
		SubscriptionID: s.sub.ID,
		CustomerID:     s.customer.ID,
		PriceID:        s.priceAPI.ID,
		PriceType:      types.PRICE_TYPE_USAGE,
		MeterID:        s.meterAPI.ID,
		Currency:       "usd",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		StartDate:      startDate,
		EndDate:        endDate,
		Quantity:       decimal.NewFromInt(1),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li))
	return li
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestLineItemDateBounding verifies that usage is bounded by line item dates,
// not the subscription period dates. This is the core bug fix.
func (s *MeterUsageServiceSuite) TestLineItemDateBounding() {
	ctx := s.GetContext()

	// Line item active Jan 1 – Jan 15 (subscription runs full month Jan 1 – Feb 1)
	lineItemStart := s.periodStart                                          // Jan 1
	lineItemEnd := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)          // Jan 15
	s.createLineItem(ctx, "li_bounded", lineItemStart, lineItemEnd)

	// Insert usage WITHIN the line item period (Jan 5) — should be counted
	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID,
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 100)

	// Insert usage OUTSIDE the line item period (Jan 20) — should NOT be counted
	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID,
		time.Date(2026, 1, 20, 10, 0, 0, 0, time.UTC), 200)

	result, err := s.svc.GetSubscriptionMeterUsage(ctx, &GetSubscriptionMeterUsageRequest{
		SubscriptionID: s.sub.ID,
		StartTime:      s.periodStart,
		EndTime:        s.periodEnd,
	})
	s.NoError(err)
	s.Require().Len(result.LineItemUsages, 1, "should have exactly one line item usage")

	lu := result.LineItemUsages[0]
	s.Equal("li_bounded", lu.LineItem.ID)

	// The effective period should be bounded to the line item dates
	s.Equal(lineItemStart, lu.PeriodStart, "PeriodStart should be line item start")
	s.Equal(lineItemEnd, lu.PeriodEnd, "PeriodEnd should be line item end")

	// Usage should only include the 100 from Jan 5, not the 200 from Jan 20
	s.True(lu.Usage.Equal(decimal.NewFromInt(100)),
		"usage should be 100 (bounded to line item dates), got %s", lu.Usage)
}

// TestLineItemDatesMatchSubscription verifies that when line item dates
// equal the subscription period, all usage within the period is counted.
func (s *MeterUsageServiceSuite) TestLineItemDatesMatchSubscription() {
	ctx := s.GetContext()

	// Line item covers the full subscription period
	s.createLineItem(ctx, "li_full", s.periodStart, s.periodEnd)

	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID,
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 100)
	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID,
		time.Date(2026, 1, 20, 10, 0, 0, 0, time.UTC), 200)

	result, err := s.svc.GetSubscriptionMeterUsage(ctx, &GetSubscriptionMeterUsageRequest{
		SubscriptionID: s.sub.ID,
		StartTime:      s.periodStart,
		EndTime:        s.periodEnd,
	})
	s.NoError(err)
	s.Require().Len(result.LineItemUsages, 1)

	lu := result.LineItemUsages[0]
	s.True(lu.Usage.Equal(decimal.NewFromInt(300)),
		"usage should be 300 (all events within period), got %s", lu.Usage)
}

// TestMultipleLineItemsSameMeterDifferentDates verifies that two line items
// for the same meter with different date ranges get independent usage.
func (s *MeterUsageServiceSuite) TestMultipleLineItemsSameMeterDifferentDates() {
	ctx := s.GetContext()

	// Create a second price for the same meter so we have two line items
	price2 := &price.Price{
		ID:             "price_api_2",
		Amount:         decimal.NewFromFloat(0.02),
		Currency:       "usd",
		EntityType:     types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:       "plan_1",
		BillingModel:   types.BILLING_MODEL_FLAT_FEE,
		Type:           types.PRICE_TYPE_USAGE,
		MeterID:        s.meterAPI.ID,
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, price2))

	// Line item 1: Jan 1 – Jan 15
	li1Start := s.periodStart
	li1End := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	s.createLineItem(ctx, "li_first_half", li1Start, li1End)

	// Line item 2: Jan 15 – Feb 1 (with different price)
	li2Start := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	li2End := s.periodEnd
	li2 := &subscription.SubscriptionLineItem{
		ID:             "li_second_half",
		SubscriptionID: s.sub.ID,
		CustomerID:     s.customer.ID,
		PriceID:        price2.ID,
		PriceType:      types.PRICE_TYPE_USAGE,
		MeterID:        s.meterAPI.ID,
		Currency:       "usd",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		StartDate:      li2Start,
		EndDate:        li2End,
		Quantity:       decimal.NewFromInt(1),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li2))

	// Events: Jan 5 (in li1 only), Jan 10 (in li1 only), Jan 20 (in li2 only)
	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID,
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 100)
	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID,
		time.Date(2026, 1, 10, 10, 0, 0, 0, time.UTC), 50)
	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID,
		time.Date(2026, 1, 20, 10, 0, 0, 0, time.UTC), 200)

	result, err := s.svc.GetSubscriptionMeterUsage(ctx, &GetSubscriptionMeterUsageRequest{
		SubscriptionID: s.sub.ID,
		StartTime:      s.periodStart,
		EndTime:        s.periodEnd,
	})
	s.NoError(err)
	s.Require().Len(result.LineItemUsages, 2, "should have two line item usages")

	usageByLineItem := make(map[string]decimal.Decimal)
	for _, lu := range result.LineItemUsages {
		usageByLineItem[lu.LineItem.ID] = lu.Usage
	}

	s.True(usageByLineItem["li_first_half"].Equal(decimal.NewFromInt(150)),
		"li_first_half should have 150 (100+50), got %s", usageByLineItem["li_first_half"])
	s.True(usageByLineItem["li_second_half"].Equal(decimal.NewFromInt(200)),
		"li_second_half should have 200, got %s", usageByLineItem["li_second_half"])
}

// TestLineItemStartAfterSubscriptionStart verifies that when a line item
// starts mid-period, earlier events are excluded.
func (s *MeterUsageServiceSuite) TestLineItemStartAfterSubscriptionStart() {
	ctx := s.GetContext()

	// Line item starts Jan 10 (subscription starts Jan 1)
	liStart := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	s.createLineItem(ctx, "li_late_start", liStart, s.periodEnd)

	// Event before line item start
	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID,
		time.Date(2026, 1, 3, 10, 0, 0, 0, time.UTC), 50)

	// Event after line item start
	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID,
		time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC), 75)

	result, err := s.svc.GetSubscriptionMeterUsage(ctx, &GetSubscriptionMeterUsageRequest{
		SubscriptionID: s.sub.ID,
		StartTime:      s.periodStart,
		EndTime:        s.periodEnd,
	})
	s.NoError(err)
	s.Require().Len(result.LineItemUsages, 1)

	lu := result.LineItemUsages[0]
	s.Equal(liStart, lu.PeriodStart, "PeriodStart should be line item start (Jan 10)")
	s.True(lu.Usage.Equal(decimal.NewFromInt(75)),
		"usage should be 75 (only event after Jan 10), got %s", lu.Usage)
}

// TestLineItemEndBeforeSubscriptionEnd verifies that when a line item
// ends mid-period, later events are excluded.
func (s *MeterUsageServiceSuite) TestLineItemEndBeforeSubscriptionEnd() {
	ctx := s.GetContext()

	// Line item ends Jan 20 (subscription ends Feb 1)
	liEnd := time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC)
	s.createLineItem(ctx, "li_early_end", s.periodStart, liEnd)

	// Event within line item period
	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID,
		time.Date(2026, 1, 10, 10, 0, 0, 0, time.UTC), 100)

	// Event after line item end
	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID,
		time.Date(2026, 1, 25, 10, 0, 0, 0, time.UTC), 200)

	result, err := s.svc.GetSubscriptionMeterUsage(ctx, &GetSubscriptionMeterUsageRequest{
		SubscriptionID: s.sub.ID,
		StartTime:      s.periodStart,
		EndTime:        s.periodEnd,
	})
	s.NoError(err)
	s.Require().Len(result.LineItemUsages, 1)

	lu := result.LineItemUsages[0]
	s.Equal(liEnd, lu.PeriodEnd, "PeriodEnd should be line item end (Jan 20)")
	s.True(lu.Usage.Equal(decimal.NewFromInt(100)),
		"usage should be 100 (only event before Jan 20), got %s", lu.Usage)
}

// TestZeroUsageLineItem verifies that line items with no matching events
// still appear in results with zero usage.
func (s *MeterUsageServiceSuite) TestZeroUsageLineItem() {
	ctx := s.GetContext()

	s.createLineItem(ctx, "li_no_usage", s.periodStart, s.periodEnd)
	// No meter_usage records inserted

	result, err := s.svc.GetSubscriptionMeterUsage(ctx, &GetSubscriptionMeterUsageRequest{
		SubscriptionID: s.sub.ID,
		StartTime:      s.periodStart,
		EndTime:        s.periodEnd,
	})
	s.NoError(err)
	s.Require().Len(result.LineItemUsages, 1, "zero-usage line item should still appear")

	lu := result.LineItemUsages[0]
	s.True(lu.Usage.IsZero(), "usage should be zero, got %s", lu.Usage)
}
