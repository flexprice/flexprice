package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
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
	customer    *customer.Customer
	meterAPI    *meter.Meter
	priceAPI    *price.Price
	sub         *subscription.Subscription
	now         time.Time
	periodStart time.Time
	periodEnd   time.Time
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
	lineItemStart := s.periodStart                              // Jan 1
	lineItemEnd := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC) // Jan 15
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

// ---------------------------------------------------------------------------
// PropertyFilters / Sources — verify analytics-only filters are honored across
// standard, bucketed, and event-count code paths in meter_usage.go.
// ---------------------------------------------------------------------------

// insertMeterUsageWithProps adds a meter_usage record with arbitrary properties + source.
func (s *MeterUsageServiceSuite) insertMeterUsageWithProps(
	ctx context.Context, meterID, extCustID, source string, ts time.Time, qty float64,
	props map[string]interface{},
) {
	s.NoError(s.meterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{
		{
			Event: events.Event{
				ID:                 types.GenerateUUID(),
				TenantID:           types.GetTenantID(ctx),
				EnvironmentID:      types.GetEnvironmentID(ctx),
				ExternalCustomerID: extCustID,
				Timestamp:          ts,
				EventName:          "api_call",
				Source:             source,
				Properties:         props,
			},
			MeterID:  meterID,
			QtyTotal: decimal.NewFromFloat(qty),
		},
	}))
}

// insertMeterUsageFull is the most flexible inserter: lets the test specify
// unique_hash (needed for COUNT_UNIQUE) and event_name.
func (s *MeterUsageServiceSuite) insertMeterUsageFull(
	ctx context.Context, meterID, extCustID, source, eventName string,
	ts time.Time, qty float64, uniqueHash string, props map[string]interface{},
) {
	s.NoError(s.meterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{
		{
			Event: events.Event{
				ID:                 types.GenerateUUID(),
				TenantID:           types.GetTenantID(ctx),
				EnvironmentID:      types.GetEnvironmentID(ctx),
				ExternalCustomerID: extCustID,
				Timestamp:          ts,
				EventName:          eventName,
				Source:             source,
				Properties:         props,
			},
			MeterID:    meterID,
			QtyTotal:   decimal.NewFromFloat(qty),
			UniqueHash: uniqueHash,
		},
	}))
}

// createMeterWithAggregation creates a custom meter with the given aggregation type.
func (s *MeterUsageServiceSuite) createMeterWithAggregation(
	ctx context.Context, id, eventName string, aggType types.AggregationType,
) *meter.Meter {
	m := &meter.Meter{
		ID:        id,
		Name:      id,
		EventName: eventName,
		Aggregation: meter.Aggregation{
			Type: aggType,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, m))
	return m
}

// createPriceForMeter creates a per-unit USD price for the given meter.
func (s *MeterUsageServiceSuite) createPriceForMeter(
	ctx context.Context, id, meterID string, amount decimal.Decimal,
) *price.Price {
	p := &price.Price{
		ID:             id,
		Amount:         amount,
		Currency:       "usd",
		EntityType:     types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:       "plan_1",
		BillingModel:   types.BILLING_MODEL_FLAT_FEE,
		Type:           types.PRICE_TYPE_USAGE,
		MeterID:        meterID,
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, p))
	return p
}

// createLineItemForMeter creates a line item bound to a specific meter + price.
func (s *MeterUsageServiceSuite) createLineItemForMeter(
	ctx context.Context, id, meterID, priceID string,
) *subscription.SubscriptionLineItem {
	li := &subscription.SubscriptionLineItem{
		ID:             id,
		SubscriptionID: s.sub.ID,
		CustomerID:     s.customer.ID,
		PriceID:        priceID,
		PriceType:      types.PRICE_TYPE_USAGE,
		MeterID:        meterID,
		Currency:       "usd",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		StartDate:      s.periodStart,
		EndDate:        s.periodEnd,
		Quantity:       decimal.NewFromInt(1),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li))
	return li
}

// TestPropertyFiltersStandardMeter verifies that property_filters restrict the
// counted events when GetSubscriptionMeterUsage is invoked with PropertyFilters.
// Without the fix, all events are counted — the SQL builder's WHERE clause
// silently dropped PropertyFilters on the scalar query path.
func (s *MeterUsageServiceSuite) TestPropertyFiltersStandardMeter() {
	ctx := s.GetContext()
	s.createLineItem(ctx, "li_props", s.periodStart, s.periodEnd)

	// gpt-4 events: 100 + 50 = 150 (matching the filter)
	s.insertMeterUsageWithProps(ctx, s.meterAPI.ID, s.customer.ExternalID, "",
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 100,
		map[string]interface{}{"model": "gpt-4"})
	s.insertMeterUsageWithProps(ctx, s.meterAPI.ID, s.customer.ExternalID, "",
		time.Date(2026, 1, 10, 10, 0, 0, 0, time.UTC), 50,
		map[string]interface{}{"model": "gpt-4"})
	// gpt-3.5 events: 999 — must be excluded
	s.insertMeterUsageWithProps(ctx, s.meterAPI.ID, s.customer.ExternalID, "",
		time.Date(2026, 1, 12, 10, 0, 0, 0, time.UTC), 999,
		map[string]interface{}{"model": "gpt-3.5"})

	result, err := s.svc.GetSubscriptionMeterUsage(ctx, &GetSubscriptionMeterUsageRequest{
		SubscriptionID:  s.sub.ID,
		StartTime:       s.periodStart,
		EndTime:         s.periodEnd,
		PropertyFilters: map[string][]string{"model": {"gpt-4"}},
	})
	s.NoError(err)
	s.Require().Len(result.LineItemUsages, 1)

	lu := result.LineItemUsages[0]
	s.True(lu.Usage.Equal(decimal.NewFromInt(150)),
		"only gpt-4 events should be counted, got %s", lu.Usage)
}

// TestPropertyFiltersStandardMeter_MultipleValues verifies IN-list semantics
// (multiple values for one property key).
func (s *MeterUsageServiceSuite) TestPropertyFiltersStandardMeter_MultipleValues() {
	ctx := s.GetContext()
	s.createLineItem(ctx, "li_props_multi", s.periodStart, s.periodEnd)

	s.insertMeterUsageWithProps(ctx, s.meterAPI.ID, s.customer.ExternalID, "",
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 10,
		map[string]interface{}{"model": "gpt-4"})
	s.insertMeterUsageWithProps(ctx, s.meterAPI.ID, s.customer.ExternalID, "",
		time.Date(2026, 1, 6, 10, 0, 0, 0, time.UTC), 20,
		map[string]interface{}{"model": "claude-opus"})
	s.insertMeterUsageWithProps(ctx, s.meterAPI.ID, s.customer.ExternalID, "",
		time.Date(2026, 1, 7, 10, 0, 0, 0, time.UTC), 999,
		map[string]interface{}{"model": "gpt-3.5"})

	result, err := s.svc.GetSubscriptionMeterUsage(ctx, &GetSubscriptionMeterUsageRequest{
		SubscriptionID:  s.sub.ID,
		StartTime:       s.periodStart,
		EndTime:         s.periodEnd,
		PropertyFilters: map[string][]string{"model": {"gpt-4", "claude-opus"}},
	})
	s.NoError(err)
	s.Require().Len(result.LineItemUsages, 1)
	s.True(result.LineItemUsages[0].Usage.Equal(decimal.NewFromInt(30)),
		"gpt-4 + claude-opus events should sum to 30, got %s", result.LineItemUsages[0].Usage)
}

// TestSourcesFilter verifies the source-list filter is honored.
func (s *MeterUsageServiceSuite) TestSourcesFilter() {
	ctx := s.GetContext()
	s.createLineItem(ctx, "li_sources", s.periodStart, s.periodEnd)

	s.insertMeterUsageWithProps(ctx, s.meterAPI.ID, s.customer.ExternalID, "stripe",
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 10, nil)
	s.insertMeterUsageWithProps(ctx, s.meterAPI.ID, s.customer.ExternalID, "stripe",
		time.Date(2026, 1, 6, 10, 0, 0, 0, time.UTC), 20, nil)
	s.insertMeterUsageWithProps(ctx, s.meterAPI.ID, s.customer.ExternalID, "internal",
		time.Date(2026, 1, 7, 10, 0, 0, 0, time.UTC), 999, nil)

	result, err := s.svc.GetSubscriptionMeterUsage(ctx, &GetSubscriptionMeterUsageRequest{
		SubscriptionID: s.sub.ID,
		StartTime:      s.periodStart,
		EndTime:        s.periodEnd,
		Sources:        []string{"stripe"},
	})
	s.NoError(err)
	s.Require().Len(result.LineItemUsages, 1)
	s.True(result.LineItemUsages[0].Usage.Equal(decimal.NewFromInt(30)),
		"only stripe-sourced events should be counted, got %s", result.LineItemUsages[0].Usage)
}

// TestPropertyFiltersSkipCommitment verifies that when property_filters are set,
// commitment is NOT applied during analytics cost calculation — because the
// filter restricts the SQL result to a subset of actual usage, and applying
// commitment over a subset surfaces misleading true-up/overage amounts.
func (s *MeterUsageServiceSuite) TestPropertyFiltersSkipCommitment() {
	ctx := s.GetContext()

	// Line item with a non-trivial commitment configured.
	commitmentAmount := decimal.NewFromInt(100) // $100 commitment
	overageFactor := decimal.NewFromFloat(1.5)
	li := &subscription.SubscriptionLineItem{
		ID:                      "li_commit",
		SubscriptionID:          s.sub.ID,
		CustomerID:              s.customer.ID,
		PriceID:                 s.priceAPI.ID,
		PriceType:               types.PRICE_TYPE_USAGE,
		MeterID:                 s.meterAPI.ID,
		Currency:                "usd",
		BillingPeriod:           types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence:          types.InvoiceCadenceArrear,
		StartDate:               s.periodStart,
		EndDate:                 s.periodEnd,
		Quantity:                decimal.NewFromInt(1),
		CommitmentAmount:        &commitmentAmount,
		CommitmentType:          types.COMMITMENT_TYPE_AMOUNT,
		CommitmentOverageFactor: &overageFactor,
		CommitmentTrueUpEnabled: true, // would charge full commitment if usage < commitment
		BaseModel:               types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li))

	// Only insert a small amount of matching usage (run_id=run123, qty=10).
	// Without the filter this would yield $0.10 in cost (below commitment),
	// so commitment+true-up would push the charge to $100. With the filter,
	// the SQL returns only the matching event(s); commitment must NOT apply.
	s.insertMeterUsageWithProps(ctx, s.meterAPI.ID, s.customer.ExternalID, "",
		time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC), 10,
		map[string]interface{}{"run_id": "run123"})
	// Non-matching events to confirm the filter is doing its job.
	s.insertMeterUsageWithProps(ctx, s.meterAPI.ID, s.customer.ExternalID, "",
		time.Date(2026, 1, 11, 12, 0, 0, 0, time.UTC), 50,
		map[string]interface{}{"run_id": "OTHER"})

	params := &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: s.customer.ExternalID,
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
		PropertyFilters:    map[string][]string{"run_id": {"run123"}},
	}
	resp, err := s.svc.GetDetailedAnalytics(ctx, params)
	s.NoError(err)
	s.Require().NotEmpty(resp.Items)

	// Find the line item analytic for our committed line item.
	var item *dto.UsageAnalyticItem
	for i := range resp.Items {
		if resp.Items[i].SubLineItemID == "li_commit" {
			item = &resp.Items[i]
			break
		}
	}
	s.Require().NotNil(item, "expected analytic for commit line item")

	// Filtered usage = 10 (only matching event); raw cost = 10 * $0.01 = $0.10.
	// If commitment had been applied, TotalCost would be $100 (commitment+true-up).
	expectedRawCost := decimal.NewFromFloat(0.10)
	s.True(item.TotalCost.Equal(expectedRawCost),
		"property-filtered analytics must NOT apply commitment; expected raw cost %s, got %s",
		expectedRawCost, item.TotalCost)
	s.Nil(item.CommitmentInfo,
		"commitment_info should not be populated when filters are active")
}

// TestPropertyFilters_ExcludesNonMatchingMissingAndNilProperties verifies that
// a single-value property filter correctly excludes every event that doesn't
// have an exact match — covering three distinct exclusion cases in one pass:
//  1. property is present but the value differs (run_id="OTHER")
//  2. property key is entirely absent from the event (no run_id, only "model")
//  3. the event's properties map is nil
//
// Only the event whose property both exists AND matches the filter value should
// contribute to the usage total.
func (s *MeterUsageServiceSuite) TestPropertyFilters_ExcludesNonMatchingMissingAndNilProperties() {
	ctx := s.GetContext()

	// Customer with external_id "1" — matching the production payload.
	prodCustomer := &customer.Customer{
		ID:         "cust_prod",
		ExternalID: "1",
		Name:       "Prod Customer",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, prodCustomer))

	prodSub := &subscription.Subscription{
		ID:                 "sub_prod",
		CustomerID:         prodCustomer.ID,
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
	s.NoError(s.GetStores().SubscriptionRepo.Create(ctx, prodSub))

	li := &subscription.SubscriptionLineItem{
		ID:             "li_prod",
		SubscriptionID: prodSub.ID,
		CustomerID:     prodCustomer.ID,
		PriceID:        s.priceAPI.ID,
		PriceType:      types.PRICE_TYPE_USAGE,
		MeterID:        s.meterAPI.ID,
		Currency:       "usd",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		StartDate:      s.periodStart,
		EndDate:        s.periodEnd,
		Quantity:       decimal.NewFromInt(1),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li))

	// Matching event: run_id = "run123"
	s.insertMeterUsageWithProps(ctx, s.meterAPI.ID, "1", "",
		time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC), 42,
		map[string]interface{}{"run_id": "run123"})
	// Non-matching event: run_id = different
	s.insertMeterUsageWithProps(ctx, s.meterAPI.ID, "1", "",
		time.Date(2026, 1, 11, 12, 0, 0, 0, time.UTC), 999,
		map[string]interface{}{"run_id": "OTHER"})
	// Event with no run_id property at all
	s.insertMeterUsageWithProps(ctx, s.meterAPI.ID, "1", "",
		time.Date(2026, 1, 12, 12, 0, 0, 0, time.UTC), 777,
		map[string]interface{}{"model": "gpt-4"})
	// Event with empty properties
	s.insertMeterUsageWithProps(ctx, s.meterAPI.ID, "1", "",
		time.Date(2026, 1, 13, 12, 0, 0, 0, time.UTC), 555, nil)

	result, err := s.svc.GetSubscriptionMeterUsage(ctx, &GetSubscriptionMeterUsageRequest{
		SubscriptionID:  prodSub.ID,
		StartTime:       s.periodStart,
		EndTime:         s.periodEnd,
		PropertyFilters: map[string][]string{"run_id": {"run123"}},
	})
	s.NoError(err)
	s.Require().Len(result.LineItemUsages, 1)

	lu := result.LineItemUsages[0]
	// Only the matching event (qty=42) should be counted. The other three —
	// run_id="OTHER" (qty=999), no run_id key (qty=777), and nil properties
	// (qty=555) — must all be excluded by the JSONExtractString filter.
	s.True(lu.Usage.Equal(decimal.NewFromInt(42)),
		"only the matching run_id event should be counted, got %s", lu.Usage)
}

// TestGroupByPropertyField verifies that group_by supports "properties.<field>"
// in meter_usage analytics, mirroring feature_usage's behavior. The response
// should contain one item per distinct property value, with usage correctly
// aggregated within each group.
func (s *MeterUsageServiceSuite) TestGroupByPropertyField() {
	ctx := s.GetContext()
	s.createLineItem(ctx, "li_groupby_prop", s.periodStart, s.periodEnd)

	// run_id "A": 10 + 20 = 30
	s.insertMeterUsageWithProps(ctx, s.meterAPI.ID, s.customer.ExternalID, "",
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 10,
		map[string]interface{}{"run_id": "A", "region": "us-east"})
	s.insertMeterUsageWithProps(ctx, s.meterAPI.ID, s.customer.ExternalID, "",
		time.Date(2026, 1, 6, 10, 0, 0, 0, time.UTC), 20,
		map[string]interface{}{"run_id": "A", "region": "us-east"})
	// run_id "B": 5 + 50 = 55
	s.insertMeterUsageWithProps(ctx, s.meterAPI.ID, s.customer.ExternalID, "",
		time.Date(2026, 1, 7, 10, 0, 0, 0, time.UTC), 5,
		map[string]interface{}{"run_id": "B", "region": "us-west"})
	s.insertMeterUsageWithProps(ctx, s.meterAPI.ID, s.customer.ExternalID, "",
		time.Date(2026, 1, 8, 10, 0, 0, 0, time.UTC), 50,
		map[string]interface{}{"run_id": "B", "region": "us-west"})

	params := &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: s.customer.ExternalID,
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
		GroupBy:            []string{"properties.run_id"},
	}
	resp, err := s.svc.GetDetailedAnalytics(ctx, params)
	s.NoError(err)
	s.Require().NotNil(resp)

	// Expect two groups: run_id=A (usage 30) and run_id=B (usage 55).
	byRunID := make(map[string]decimal.Decimal)
	for _, item := range resp.Items {
		v, ok := item.Properties["run_id"]
		if !ok {
			continue
		}
		byRunID[v] = byRunID[v].Add(item.TotalUsage)
	}
	s.Require().Lenf(byRunID, 2, "expected 2 groups by run_id, got %d: %v", len(byRunID), byRunID)
	s.True(byRunID["A"].Equal(decimal.NewFromInt(30)),
		"run_id=A: expected 30, got %s", byRunID["A"])
	s.True(byRunID["B"].Equal(decimal.NewFromInt(55)),
		"run_id=B: expected 55, got %s", byRunID["B"])
}

// TestCountMeter_NoSubscriptionAnalytics verifies the COUNT-meter fix for the
// no-subscription analytics path (getDetailedAnalyticsWithoutSubscriptionContext).
// Triggered when no external_customer_id is supplied (or the customer has no
// subscriptions), this path goes through the "Convert results to analytics"
// loop in meter_usage.go (around line 1138) which copies r.TotalUsage directly.
// For COUNT meters that field is literal zero in the analytics SQL — without
// substituting EventCount, every item would report TotalUsage=0 (and per-point
// Usage=0). The subscription path was fixed earlier via getUsageValueFromDetailedResult;
// this test pins the parity fix for the no-subscription branch.
func (s *MeterUsageServiceSuite) TestCountMeter_NoSubscriptionAnalytics() {
	ctx := s.GetContext()

	cm := &meter.Meter{
		ID:        "meter_count_nosub",
		Name:      "Sessions",
		EventName: "session_start",
		Aggregation: meter.Aggregation{
			Type: types.AggregationCount,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, cm))

	// 3 events for an external_customer_id that has NO Flexprice customer
	// record — forces resolveCustomerAndSubscriptions to return empty, and
	// GetDetailedAnalytics falls through to the no-subscription path.
	for i := 0; i < 3; i++ {
		s.insertMeterUsage(ctx, cm.ID, "unknown_customer",
			time.Date(2026, 1, 5+i, 10, 0, 0, 0, time.UTC), 1)
	}

	resp, err := s.svc.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: "unknown_customer", // no customer record → no-sub path
		MeterIDs:           []string{cm.ID},
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
	})
	s.NoError(err)
	s.Require().Lenf(resp.Items, 1, "expected one item for the count meter, got %d", len(resp.Items))

	item := resp.Items[0]
	s.True(item.TotalUsage.Equal(decimal.NewFromInt(3)),
		"no-sub COUNT path: expected TotalUsage=3 (was 0 before fix), got %s", item.TotalUsage)
	s.Equal(uint64(3), item.EventCount,
		"no-sub COUNT path: expected EventCount=3, got %d", item.EventCount)
}

// TestCountMeter_ScalarBilling sanity-checks the scalar billing path for COUNT
// meters — that path doesn't go through the helpers I changed; it routes
// directly via GetUsageMultiMeter which emits "COUNT(DISTINCT id) AS value".
// Verifies my COUNT fix didn't accidentally change this.
func (s *MeterUsageServiceSuite) TestCountMeter_ScalarBilling() {
	ctx := s.GetContext()

	cm := &meter.Meter{
		ID:        "meter_count_scalar",
		Name:      "Sessions",
		EventName: "session",
		Aggregation: meter.Aggregation{
			Type: types.AggregationCount,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, cm))

	cp := &price.Price{
		ID: "price_count_scalar", Amount: decimal.NewFromInt(1), Currency: "usd",
		EntityType: types.PRICE_ENTITY_TYPE_PLAN, EntityID: "plan_1",
		BillingModel: types.BILLING_MODEL_FLAT_FEE, Type: types.PRICE_TYPE_USAGE,
		MeterID: cm.ID, BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, cp))

	li := &subscription.SubscriptionLineItem{
		ID: "li_count_scalar", SubscriptionID: s.sub.ID, CustomerID: s.customer.ID,
		PriceID: cp.ID, PriceType: types.PRICE_TYPE_USAGE, MeterID: cm.ID,
		Currency: "usd", BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		StartDate:      s.periodStart, EndDate: s.periodEnd,
		Quantity: decimal.NewFromInt(1), BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li))

	for i := 0; i < 3; i++ {
		s.insertMeterUsage(ctx, cm.ID, s.customer.ExternalID,
			time.Date(2026, 1, 5+i, 10, 0, 0, 0, time.UTC), 1)
	}

	// No analytics filters → scalar billing path (GetUsageMultiMeter).
	result, err := s.svc.GetSubscriptionMeterUsage(ctx, &GetSubscriptionMeterUsageRequest{
		SubscriptionID: s.sub.ID,
		StartTime:      s.periodStart,
		EndTime:        s.periodEnd,
	})
	s.NoError(err)

	var lu *LineItemMeterUsage
	for _, x := range result.LineItemUsages {
		if x.LineItem.ID == "li_count_scalar" {
			lu = x
			break
		}
	}
	s.Require().NotNil(lu, "count line item usage should exist")
	s.True(lu.Usage.Equal(decimal.NewFromInt(3)),
		"scalar COUNT path: expected Usage=3 (one per event), got %s", lu.Usage)
	s.Equal(uint64(3), lu.EventCount)
}

// TestSumMeter_AnalyticsWithGroupBy is a regression guard verifying the SUM
// path through getCorrectUsageValue / getUsageValueFromDetailedResult is
// unchanged by the AggregationCount addition. SUM still reads TotalUsage.
func (s *MeterUsageServiceSuite) TestSumMeter_AnalyticsWithGroupBy() {
	ctx := s.GetContext()
	s.createLineItem(ctx, "li_sum_groupby", s.periodStart, s.periodEnd)

	s.insertMeterUsageWithProps(ctx, s.meterAPI.ID, s.customer.ExternalID, "",
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 100,
		map[string]interface{}{"region": "us-east"})
	s.insertMeterUsageWithProps(ctx, s.meterAPI.ID, s.customer.ExternalID, "",
		time.Date(2026, 1, 6, 10, 0, 0, 0, time.UTC), 200,
		map[string]interface{}{"region": "us-west"})

	resp, err := s.svc.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: s.customer.ExternalID,
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
		GroupBy:            []string{"properties.region"},
	})
	s.NoError(err)
	byRegion := make(map[string]decimal.Decimal)
	for _, item := range resp.Items {
		byRegion[item.Properties["region"]] = byRegion[item.Properties["region"]].Add(item.TotalUsage)
	}
	s.True(byRegion["us-east"].Equal(decimal.NewFromInt(100)),
		"SUM us-east unchanged: expected 100, got %s", byRegion["us-east"])
	s.True(byRegion["us-west"].Equal(decimal.NewFromInt(200)),
		"SUM us-west unchanged: expected 200, got %s", byRegion["us-west"])
}

// TestGroupByPropertyField_CountMeter reproduces the production bug where
// COUNT-aggregation meters returned TotalUsage=0 / TotalCost=0 in every item
// (and the root total_cost) when group_by was a property field. Root cause:
// the analytics SQL emits total_usage as a literal zero for COUNT meters; the
// real count lives in event_count, but the Go helpers (getUsageValueFromDetailedResult /
// getCorrectUsageValue) fell through to TotalUsage and returned 0. Without group_by,
// the scalar path's COUNT aggregator emits the count directly as "value", which
// is why the no-group-by query worked.
func (s *MeterUsageServiceSuite) TestGroupByPropertyField_CountMeter() {
	ctx := s.GetContext()

	// COUNT-aggregation meter (mirrors the production case: COUNT(DISTINCT id)).
	countMeter := &meter.Meter{
		ID:        "meter_count",
		Name:      "Sessions",
		EventName: "session_start",
		Aggregation: meter.Aggregation{
			Type: types.AggregationCount,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, countMeter))

	countPrice := &price.Price{
		ID:             "price_count",
		Amount:         decimal.NewFromInt(1), // $1 per event
		Currency:       "usd",
		EntityType:     types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:       "plan_1",
		BillingModel:   types.BILLING_MODEL_FLAT_FEE,
		Type:           types.PRICE_TYPE_USAGE,
		MeterID:        countMeter.ID,
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, countPrice))

	li := &subscription.SubscriptionLineItem{
		ID:             "li_count",
		SubscriptionID: s.sub.ID,
		CustomerID:     s.customer.ID,
		PriceID:        countPrice.ID,
		PriceType:      types.PRICE_TYPE_USAGE,
		MeterID:        countMeter.ID,
		Currency:       "usd",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		StartDate:      s.periodStart,
		EndDate:        s.periodEnd,
		Quantity:       decimal.NewFromInt(1),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li))

	// 5 events across 5 distinct sessions.
	sessions := []string{"s1", "s2", "s3", "s4", "s5"}
	for i, sid := range sessions {
		s.insertMeterUsageWithProps(ctx, countMeter.ID, s.customer.ExternalID, "",
			time.Date(2026, 1, 5+i, 10, 0, 0, 0, time.UTC), 1,
			map[string]interface{}{"session_id": sid})
	}

	params := &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: s.customer.ExternalID,
		MeterIDs:           []string{countMeter.ID},
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
		GroupBy:            []string{"meter_id", "properties.session_id"},
	}
	resp, err := s.svc.GetDetailedAnalytics(ctx, params)
	s.NoError(err)
	s.Require().Lenf(resp.Items, 5, "expected one item per session, got %d", len(resp.Items))

	// Each item: TotalUsage = 1 (one event per session), TotalCost = $1.
	totalCost := decimal.Zero
	for _, item := range resp.Items {
		s.True(item.TotalUsage.Equal(decimal.NewFromInt(1)),
			"per-item TotalUsage: expected 1, got %s (session=%s)",
			item.TotalUsage, item.Properties["session_id"])
		s.True(item.TotalCost.Equal(decimal.NewFromInt(1)),
			"per-item TotalCost: expected 1, got %s (session=%s)",
			item.TotalCost, item.Properties["session_id"])
		s.Equal(uint64(1), item.EventCount,
			"per-item EventCount: expected 1, got %d", item.EventCount)
		totalCost = totalCost.Add(item.TotalCost)
	}
	// Root TotalCost = sum of items = 5.
	s.True(resp.TotalCost.Equal(decimal.NewFromInt(5)),
		"root TotalCost: expected 5, got %s", resp.TotalCost)
	s.True(totalCost.Equal(resp.TotalCost),
		"root TotalCost should equal sum of item.TotalCost (got root=%s, sum=%s)",
		resp.TotalCost, totalCost)
}

// TestGroupByMultipleProperties verifies multi-property group_by works
// (properties.run_id + properties.region together produce one row per combo).
func (s *MeterUsageServiceSuite) TestGroupByMultipleProperties() {
	ctx := s.GetContext()
	s.createLineItem(ctx, "li_groupby_multi", s.periodStart, s.periodEnd)

	s.insertMeterUsageWithProps(ctx, s.meterAPI.ID, s.customer.ExternalID, "",
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 10,
		map[string]interface{}{"run_id": "A", "region": "us-east"})
	s.insertMeterUsageWithProps(ctx, s.meterAPI.ID, s.customer.ExternalID, "",
		time.Date(2026, 1, 6, 10, 0, 0, 0, time.UTC), 30,
		map[string]interface{}{"run_id": "A", "region": "us-west"})
	s.insertMeterUsageWithProps(ctx, s.meterAPI.ID, s.customer.ExternalID, "",
		time.Date(2026, 1, 7, 10, 0, 0, 0, time.UTC), 7,
		map[string]interface{}{"run_id": "B", "region": "us-east"})

	params := &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: s.customer.ExternalID,
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
		GroupBy:            []string{"properties.run_id", "properties.region"},
	}
	resp, err := s.svc.GetDetailedAnalytics(ctx, params)
	s.NoError(err)
	s.Require().NotNil(resp)

	// Expect three distinct (run_id, region) groups: (A, us-east)=10, (A, us-west)=30, (B, us-east)=7.
	type k struct{ run, region string }
	byCombo := make(map[k]decimal.Decimal)
	for _, item := range resp.Items {
		key := k{run: item.Properties["run_id"], region: item.Properties["region"]}
		byCombo[key] = byCombo[key].Add(item.TotalUsage)
	}
	s.Require().Lenf(byCombo, 3, "expected 3 (run_id, region) groups, got %d: %v", len(byCombo), byCombo)
	s.True(byCombo[k{"A", "us-east"}].Equal(decimal.NewFromInt(10)),
		"(A, us-east): expected 10, got %s", byCombo[k{"A", "us-east"}])
	s.True(byCombo[k{"A", "us-west"}].Equal(decimal.NewFromInt(30)),
		"(A, us-west): expected 30, got %s", byCombo[k{"A", "us-west"}])
	s.True(byCombo[k{"B", "us-east"}].Equal(decimal.NewFromInt(7)),
		"(B, us-east): expected 7, got %s", byCombo[k{"B", "us-east"}])
}

// TestPropertyFiltersBucketedMeter verifies property filters are honored on the
// bucketed-meter path (queryBucketedMeterUsage → GetUsageForBucketedMeters).
// This path silently dropped filters before the fix.
func (s *MeterUsageServiceSuite) TestPropertyFiltersBucketedMeter() {
	ctx := s.GetContext()

	// Create a bucketed-sum meter (BucketSize set → bucketed code path).
	bucketedMeter := &meter.Meter{
		ID:        "meter_bucketed",
		Name:      "Bucketed SUM",
		EventName: "api_call",
		Aggregation: meter.Aggregation{
			Type:       types.AggregationSum,
			BucketSize: types.WindowSizeHour,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, bucketedMeter))

	bucketedPrice := &price.Price{
		ID:             "price_bucketed",
		Amount:         decimal.NewFromFloat(0.01),
		Currency:       "usd",
		EntityType:     types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:       "plan_1",
		BillingModel:   types.BILLING_MODEL_FLAT_FEE,
		Type:           types.PRICE_TYPE_USAGE,
		MeterID:        bucketedMeter.ID,
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, bucketedPrice))

	li := &subscription.SubscriptionLineItem{
		ID:             "li_bucketed",
		SubscriptionID: s.sub.ID,
		CustomerID:     s.customer.ID,
		PriceID:        bucketedPrice.ID,
		PriceType:      types.PRICE_TYPE_USAGE,
		MeterID:        bucketedMeter.ID,
		Currency:       "usd",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		StartDate:      s.periodStart,
		EndDate:        s.periodEnd,
		Quantity:       decimal.NewFromInt(1),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li))

	// gpt-4 events: 10 + 20 = 30
	s.insertMeterUsageWithProps(ctx, bucketedMeter.ID, s.customer.ExternalID, "",
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 10,
		map[string]interface{}{"model": "gpt-4"})
	s.insertMeterUsageWithProps(ctx, bucketedMeter.ID, s.customer.ExternalID, "",
		time.Date(2026, 1, 5, 11, 0, 0, 0, time.UTC), 20,
		map[string]interface{}{"model": "gpt-4"})
	// gpt-3.5 events: 999 — must be excluded
	s.insertMeterUsageWithProps(ctx, bucketedMeter.ID, s.customer.ExternalID, "",
		time.Date(2026, 1, 6, 10, 0, 0, 0, time.UTC), 999,
		map[string]interface{}{"model": "gpt-3.5"})

	result, err := s.svc.GetSubscriptionMeterUsage(ctx, &GetSubscriptionMeterUsageRequest{
		SubscriptionID:  s.sub.ID,
		StartTime:       s.periodStart,
		EndTime:         s.periodEnd,
		PropertyFilters: map[string][]string{"model": {"gpt-4"}},
	})
	s.NoError(err)

	// Find the bucketed line item's usage entry
	var bucketedUsage *LineItemMeterUsage
	for _, lu := range result.LineItemUsages {
		if lu.LineItem != nil && lu.LineItem.ID == "li_bucketed" {
			bucketedUsage = lu
			break
		}
	}
	s.Require().NotNil(bucketedUsage, "bucketed line item usage entry should exist")
	s.True(bucketedUsage.Usage.Equal(decimal.NewFromInt(30)),
		"only gpt-4 events should be counted on bucketed meter, got %s", bucketedUsage.Usage)
}

// ---------------------------------------------------------------------------
// Aggregation-type matrix
//
// Verifies that buildMeterUsageAggregationColumns puts the correct value in
// total_usage for every aggregation type, across both the subscription
// analytics path (queryAndAppendAnalyticsEntries) and the no-subscription
// analytics path (getDetailedAnalyticsWithoutSubscriptionContext), plus the
// scalar billing path (GetUsageMultiMeter) where applicable.
// ---------------------------------------------------------------------------

// --- MAX ---

func (s *MeterUsageServiceSuite) TestMaxMeter_AnalyticsWithGroupBy() {
	ctx := s.GetContext()
	m := s.createMeterWithAggregation(ctx, "mtr_max_grp", "ev_max", types.AggregationMax)
	p := s.createPriceForMeter(ctx, "pr_max_grp", m.ID, decimal.NewFromInt(1))
	s.createLineItemForMeter(ctx, "li_max_grp", m.ID, p.ID)

	// us-east: 10, 50, 20 → MAX = 50;  us-west: 5, 30 → MAX = 30
	s.insertMeterUsageFull(ctx, m.ID, s.customer.ExternalID, "", "ev_max",
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 10, "", map[string]interface{}{"region": "us-east"})
	s.insertMeterUsageFull(ctx, m.ID, s.customer.ExternalID, "", "ev_max",
		time.Date(2026, 1, 6, 10, 0, 0, 0, time.UTC), 50, "", map[string]interface{}{"region": "us-east"})
	s.insertMeterUsageFull(ctx, m.ID, s.customer.ExternalID, "", "ev_max",
		time.Date(2026, 1, 7, 10, 0, 0, 0, time.UTC), 20, "", map[string]interface{}{"region": "us-east"})
	s.insertMeterUsageFull(ctx, m.ID, s.customer.ExternalID, "", "ev_max",
		time.Date(2026, 1, 8, 10, 0, 0, 0, time.UTC), 5, "", map[string]interface{}{"region": "us-west"})
	s.insertMeterUsageFull(ctx, m.ID, s.customer.ExternalID, "", "ev_max",
		time.Date(2026, 1, 9, 10, 0, 0, 0, time.UTC), 30, "", map[string]interface{}{"region": "us-west"})

	resp, err := s.svc.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: s.customer.ExternalID,
		MeterIDs:           []string{m.ID},
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
		GroupBy:            []string{"properties.region"},
	})
	s.NoError(err)
	byRegion := map[string]decimal.Decimal{}
	for _, item := range resp.Items {
		byRegion[item.Properties["region"]] = item.TotalUsage
	}
	s.True(byRegion["us-east"].Equal(decimal.NewFromInt(50)), "MAX us-east: expected 50, got %s", byRegion["us-east"])
	s.True(byRegion["us-west"].Equal(decimal.NewFromInt(30)), "MAX us-west: expected 30, got %s", byRegion["us-west"])
}

func (s *MeterUsageServiceSuite) TestMaxMeter_NoSubscriptionAnalytics() {
	ctx := s.GetContext()
	m := s.createMeterWithAggregation(ctx, "mtr_max_nosub", "ev_max", types.AggregationMax)

	// External customer with no Flexprice record → no-sub path.
	for _, q := range []float64{10, 50, 20} {
		s.insertMeterUsageFull(ctx, m.ID, "unknown_cust", "", "ev_max",
			time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC).Add(time.Duration(q)*time.Hour), q, "", nil)
	}

	resp, err := s.svc.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: "unknown_cust",
		MeterIDs:           []string{m.ID},
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
	})
	s.NoError(err)
	s.Require().Len(resp.Items, 1)
	s.True(resp.Items[0].TotalUsage.Equal(decimal.NewFromInt(50)),
		"no-sub MAX: expected 50, got %s", resp.Items[0].TotalUsage)
}

func (s *MeterUsageServiceSuite) TestMaxMeter_ScalarBilling() {
	ctx := s.GetContext()
	m := s.createMeterWithAggregation(ctx, "mtr_max_scalar", "ev_max", types.AggregationMax)
	p := s.createPriceForMeter(ctx, "pr_max_scalar", m.ID, decimal.NewFromInt(1))
	s.createLineItemForMeter(ctx, "li_max_scalar", m.ID, p.ID)

	for _, q := range []float64{10, 50, 20} {
		s.insertMeterUsageFull(ctx, m.ID, s.customer.ExternalID, "", "ev_max",
			time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC).Add(time.Duration(q)*time.Hour), q, "", nil)
	}

	result, err := s.svc.GetSubscriptionMeterUsage(ctx, &GetSubscriptionMeterUsageRequest{
		SubscriptionID: s.sub.ID,
		StartTime:      s.periodStart,
		EndTime:        s.periodEnd,
	})
	s.NoError(err)

	var lu *LineItemMeterUsage
	for _, x := range result.LineItemUsages {
		if x.LineItem.ID == "li_max_scalar" {
			lu = x
			break
		}
	}
	s.Require().NotNil(lu)
	s.True(lu.Usage.Equal(decimal.NewFromInt(50)), "scalar MAX: expected 50, got %s", lu.Usage)
}

// --- LATEST ---

func (s *MeterUsageServiceSuite) TestLatestMeter_AnalyticsWithGroupBy() {
	ctx := s.GetContext()
	m := s.createMeterWithAggregation(ctx, "mtr_latest_grp", "ev_latest", types.AggregationLatest)
	p := s.createPriceForMeter(ctx, "pr_latest_grp", m.ID, decimal.NewFromInt(1))
	s.createLineItemForMeter(ctx, "li_latest_grp", m.ID, p.ID)

	// us-east: 10 @ Jan5, 99 @ Jan10  → LATEST = 99
	// us-west: 7 @ Jan8, 3 @ Jan12   → LATEST = 3
	s.insertMeterUsageFull(ctx, m.ID, s.customer.ExternalID, "", "ev_latest",
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 10, "", map[string]interface{}{"region": "us-east"})
	s.insertMeterUsageFull(ctx, m.ID, s.customer.ExternalID, "", "ev_latest",
		time.Date(2026, 1, 10, 10, 0, 0, 0, time.UTC), 99, "", map[string]interface{}{"region": "us-east"})
	s.insertMeterUsageFull(ctx, m.ID, s.customer.ExternalID, "", "ev_latest",
		time.Date(2026, 1, 8, 10, 0, 0, 0, time.UTC), 7, "", map[string]interface{}{"region": "us-west"})
	s.insertMeterUsageFull(ctx, m.ID, s.customer.ExternalID, "", "ev_latest",
		time.Date(2026, 1, 12, 10, 0, 0, 0, time.UTC), 3, "", map[string]interface{}{"region": "us-west"})

	resp, err := s.svc.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: s.customer.ExternalID,
		MeterIDs:           []string{m.ID},
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
		GroupBy:            []string{"properties.region"},
	})
	s.NoError(err)
	byRegion := map[string]decimal.Decimal{}
	for _, item := range resp.Items {
		byRegion[item.Properties["region"]] = item.TotalUsage
	}
	s.True(byRegion["us-east"].Equal(decimal.NewFromInt(99)), "LATEST us-east: expected 99, got %s", byRegion["us-east"])
	s.True(byRegion["us-west"].Equal(decimal.NewFromInt(3)), "LATEST us-west: expected 3, got %s", byRegion["us-west"])
}

func (s *MeterUsageServiceSuite) TestLatestMeter_NoSubscriptionAnalytics() {
	ctx := s.GetContext()
	m := s.createMeterWithAggregation(ctx, "mtr_latest_nosub", "ev_latest", types.AggregationLatest)

	s.insertMeterUsageFull(ctx, m.ID, "unknown_cust", "", "ev_latest",
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 10, "", nil)
	s.insertMeterUsageFull(ctx, m.ID, "unknown_cust", "", "ev_latest",
		time.Date(2026, 1, 12, 10, 0, 0, 0, time.UTC), 77, "", nil)
	s.insertMeterUsageFull(ctx, m.ID, "unknown_cust", "", "ev_latest",
		time.Date(2026, 1, 8, 10, 0, 0, 0, time.UTC), 22, "", nil)

	resp, err := s.svc.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: "unknown_cust",
		MeterIDs:           []string{m.ID},
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
	})
	s.NoError(err)
	s.Require().Len(resp.Items, 1)
	s.True(resp.Items[0].TotalUsage.Equal(decimal.NewFromInt(77)),
		"no-sub LATEST: expected 77 (Jan 12), got %s", resp.Items[0].TotalUsage)
}

// --- COUNT_UNIQUE ---

func (s *MeterUsageServiceSuite) TestCountUniqueMeter_AnalyticsWithGroupBy() {
	ctx := s.GetContext()
	m := s.createMeterWithAggregation(ctx, "mtr_unique_grp", "ev_unique", types.AggregationCountUnique)
	p := s.createPriceForMeter(ctx, "pr_unique_grp", m.ID, decimal.NewFromInt(1))
	s.createLineItemForMeter(ctx, "li_unique_grp", m.ID, p.ID)

	// us-east: unique_hash ∈ {u1, u2, u1} → 2 distinct
	// us-west: unique_hash ∈ {u3}         → 1 distinct
	s.insertMeterUsageFull(ctx, m.ID, s.customer.ExternalID, "", "ev_unique",
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 1, "u1", map[string]interface{}{"region": "us-east"})
	s.insertMeterUsageFull(ctx, m.ID, s.customer.ExternalID, "", "ev_unique",
		time.Date(2026, 1, 6, 10, 0, 0, 0, time.UTC), 1, "u2", map[string]interface{}{"region": "us-east"})
	s.insertMeterUsageFull(ctx, m.ID, s.customer.ExternalID, "", "ev_unique",
		time.Date(2026, 1, 7, 10, 0, 0, 0, time.UTC), 1, "u1", map[string]interface{}{"region": "us-east"})
	s.insertMeterUsageFull(ctx, m.ID, s.customer.ExternalID, "", "ev_unique",
		time.Date(2026, 1, 8, 10, 0, 0, 0, time.UTC), 1, "u3", map[string]interface{}{"region": "us-west"})

	resp, err := s.svc.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: s.customer.ExternalID,
		MeterIDs:           []string{m.ID},
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
		GroupBy:            []string{"properties.region"},
	})
	s.NoError(err)
	byRegion := map[string]decimal.Decimal{}
	for _, item := range resp.Items {
		byRegion[item.Properties["region"]] = item.TotalUsage
	}
	s.True(byRegion["us-east"].Equal(decimal.NewFromInt(2)),
		"COUNT_UNIQUE us-east: expected 2, got %s", byRegion["us-east"])
	s.True(byRegion["us-west"].Equal(decimal.NewFromInt(1)),
		"COUNT_UNIQUE us-west: expected 1, got %s", byRegion["us-west"])
}

func (s *MeterUsageServiceSuite) TestCountUniqueMeter_NoSubscriptionAnalytics() {
	ctx := s.GetContext()
	m := s.createMeterWithAggregation(ctx, "mtr_unique_nosub", "ev_unique", types.AggregationCountUnique)

	// 3 events, 2 distinct unique_hash values.
	s.insertMeterUsageFull(ctx, m.ID, "unknown_cust", "", "ev_unique",
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 1, "u1", nil)
	s.insertMeterUsageFull(ctx, m.ID, "unknown_cust", "", "ev_unique",
		time.Date(2026, 1, 6, 10, 0, 0, 0, time.UTC), 1, "u2", nil)
	s.insertMeterUsageFull(ctx, m.ID, "unknown_cust", "", "ev_unique",
		time.Date(2026, 1, 7, 10, 0, 0, 0, time.UTC), 1, "u1", nil)

	resp, err := s.svc.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: "unknown_cust",
		MeterIDs:           []string{m.ID},
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
	})
	s.NoError(err)
	s.Require().Len(resp.Items, 1)
	s.True(resp.Items[0].TotalUsage.Equal(decimal.NewFromInt(2)),
		"no-sub COUNT_UNIQUE: expected 2, got %s", resp.Items[0].TotalUsage)
}

// ---------------------------------------------------------------------------
// Windowed analytics — per-window points carry the aggregation-aware Usage.
// ---------------------------------------------------------------------------

// TestWindowedAnalytics_CountMeter exercises BuildDetailedPointsQuery for a
// COUNT meter with WindowSize=DAY. Each per-window point.Usage should equal
// the count of events in that window — not zero.
func (s *MeterUsageServiceSuite) TestWindowedAnalytics_CountMeter() {
	ctx := s.GetContext()
	m := s.createMeterWithAggregation(ctx, "mtr_count_win", "ev_cnt", types.AggregationCount)
	p := s.createPriceForMeter(ctx, "pr_count_win", m.ID, decimal.NewFromInt(1))
	s.createLineItemForMeter(ctx, "li_count_win", m.ID, p.ID)

	// 2 events on Jan 5, 3 events on Jan 6, 1 event on Jan 7.
	for i := 0; i < 2; i++ {
		s.insertMeterUsageFull(ctx, m.ID, s.customer.ExternalID, "", "ev_cnt",
			time.Date(2026, 1, 5, 10+i, 0, 0, 0, time.UTC), 1, "", nil)
	}
	for i := 0; i < 3; i++ {
		s.insertMeterUsageFull(ctx, m.ID, s.customer.ExternalID, "", "ev_cnt",
			time.Date(2026, 1, 6, 10+i, 0, 0, 0, time.UTC), 1, "", nil)
	}
	s.insertMeterUsageFull(ctx, m.ID, s.customer.ExternalID, "", "ev_cnt",
		time.Date(2026, 1, 7, 10, 0, 0, 0, time.UTC), 1, "", nil)

	resp, err := s.svc.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: s.customer.ExternalID,
		MeterIDs:           []string{m.ID},
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
		WindowSize:         types.WindowSizeDay,
	})
	s.NoError(err)
	s.Require().Lenf(resp.Items, 1, "expected one item, got %d", len(resp.Items))
	item := resp.Items[0]

	// Aggregate TotalUsage = 2 + 3 + 1 = 6 distinct events.
	s.True(item.TotalUsage.Equal(decimal.NewFromInt(6)),
		"windowed COUNT total: expected 6, got %s", item.TotalUsage)

	// Per-window points: each carries its day's count in Usage.
	byDay := map[string]decimal.Decimal{}
	for _, pt := range item.Points {
		byDay[pt.Timestamp.UTC().Format("2006-01-02")] = pt.Usage
	}
	s.True(byDay["2026-01-05"].Equal(decimal.NewFromInt(2)),
		"per-window COUNT Jan 5: expected 2, got %s", byDay["2026-01-05"])
	s.True(byDay["2026-01-06"].Equal(decimal.NewFromInt(3)),
		"per-window COUNT Jan 6: expected 3, got %s", byDay["2026-01-06"])
	s.True(byDay["2026-01-07"].Equal(decimal.NewFromInt(1)),
		"per-window COUNT Jan 7: expected 1, got %s", byDay["2026-01-07"])
}

// TestWindowedAnalytics_MaxMeter same as above but for MAX — verifies per-window
// Usage carries the per-window MAX value via total_usage.
func (s *MeterUsageServiceSuite) TestWindowedAnalytics_MaxMeter() {
	ctx := s.GetContext()
	m := s.createMeterWithAggregation(ctx, "mtr_max_win", "ev_max", types.AggregationMax)
	p := s.createPriceForMeter(ctx, "pr_max_win", m.ID, decimal.NewFromInt(1))
	s.createLineItemForMeter(ctx, "li_max_win", m.ID, p.ID)

	// Jan 5: qty 10, 50 → MAX 50
	// Jan 6: qty 20    → MAX 20
	s.insertMeterUsageFull(ctx, m.ID, s.customer.ExternalID, "", "ev_max",
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 10, "", nil)
	s.insertMeterUsageFull(ctx, m.ID, s.customer.ExternalID, "", "ev_max",
		time.Date(2026, 1, 5, 11, 0, 0, 0, time.UTC), 50, "", nil)
	s.insertMeterUsageFull(ctx, m.ID, s.customer.ExternalID, "", "ev_max",
		time.Date(2026, 1, 6, 10, 0, 0, 0, time.UTC), 20, "", nil)

	resp, err := s.svc.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: s.customer.ExternalID,
		MeterIDs:           []string{m.ID},
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
		WindowSize:         types.WindowSizeDay,
	})
	s.NoError(err)
	s.Require().Len(resp.Items, 1)
	item := resp.Items[0]

	// Aggregate MAX across all events = 50.
	s.True(item.TotalUsage.Equal(decimal.NewFromInt(50)),
		"windowed MAX total: expected 50, got %s", item.TotalUsage)

	byDay := map[string]decimal.Decimal{}
	for _, pt := range item.Points {
		byDay[pt.Timestamp.UTC().Format("2006-01-02")] = pt.Usage
	}
	s.True(byDay["2026-01-05"].Equal(decimal.NewFromInt(50)),
		"per-window MAX Jan 5: expected 50, got %s", byDay["2026-01-05"])
	s.True(byDay["2026-01-06"].Equal(decimal.NewFromInt(20)),
		"per-window MAX Jan 6: expected 20, got %s", byDay["2026-01-06"])
}

// ---------------------------------------------------------------------------
// Commitment + non-SUM aggregation
//
// Before the primary-aggregation SQL fix, COUNT/MAX meters returned TotalUsage=0
// in analytics, which made commitment + overage / true-up surface bogus values.
// These tests pin the correct commitment behavior across aggregation types.
// ---------------------------------------------------------------------------

// TestCommitmentNonWindowed_CountMeter exercises the billing path through
// GetSubscriptionMeterUsage with a COUNT meter at $1/event, a $10 commitment,
// and true-up enabled. 15 events ingested with no property/source filters so
// the commitment runs normally: $10 utilized + $5 overage × 1.5 overage factor
// → TotalCost = $17.50. Asserts:
//   - EventCount == 15 and TotalUsage == 15 (COUNT semantics for COUNT meter)
//   - TotalCost == 17.5 (commitment applied with overage)
//   - CommitmentInfo populated (commitment recorded on the response)
//
// This pins the COUNT-meter contract end-to-end: the primary aggregation
// expression in total_usage feeds commitment computation correctly.
func (s *MeterUsageServiceSuite) TestCommitmentNonWindowed_CountMeter() {
	ctx := s.GetContext()
	m := s.createMeterWithAggregation(ctx, "mtr_cnt_commit", "ev_cnt", types.AggregationCount)
	p := s.createPriceForMeter(ctx, "pr_cnt_commit", m.ID, decimal.NewFromInt(1))

	commitmentAmount := decimal.NewFromInt(10)
	overageFactor := decimal.NewFromFloat(1.5)
	li := &subscription.SubscriptionLineItem{
		ID:                      "li_cnt_commit",
		SubscriptionID:          s.sub.ID,
		CustomerID:              s.customer.ID,
		PriceID:                 p.ID,
		PriceType:               types.PRICE_TYPE_USAGE,
		MeterID:                 m.ID,
		Currency:                "usd",
		BillingPeriod:           types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence:          types.InvoiceCadenceArrear,
		StartDate:               s.periodStart,
		EndDate:                 s.periodEnd,
		Quantity:                decimal.NewFromInt(1),
		CommitmentAmount:        &commitmentAmount,
		CommitmentType:          types.COMMITMENT_TYPE_AMOUNT,
		CommitmentOverageFactor: &overageFactor,
		CommitmentTrueUpEnabled: true,
		BaseModel:               types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li))

	// 15 events @ $1 = $15 → above $10 commitment, expect overage.
	for i := 0; i < 15; i++ {
		s.insertMeterUsageFull(ctx, m.ID, s.customer.ExternalID, "", "ev_cnt",
			time.Date(2026, 1, 5, 10, i, 0, 0, time.UTC), 1, "", nil)
	}

	resp, err := s.svc.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: s.customer.ExternalID,
		MeterIDs:           []string{m.ID},
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
	})
	s.NoError(err)
	var item *dto.UsageAnalyticItem
	for i := range resp.Items {
		if resp.Items[i].SubLineItemID == "li_cnt_commit" {
			item = &resp.Items[i]
			break
		}
	}
	s.Require().NotNil(item)

	// Without filters the commitment path runs: cost = $10 commitment +
	// ($15-$10)*1.5 = $10 + $7.5 = $17.5.
	s.True(item.TotalUsage.Equal(decimal.NewFromInt(15)),
		"COUNT total usage: expected 15 events, got %s", item.TotalUsage)
	s.True(item.TotalCost.Equal(decimal.NewFromFloat(17.5)),
		"commitment + overage: expected 17.5, got %s", item.TotalCost)
	s.Require().NotNil(item.CommitmentInfo)
	s.True(item.CommitmentInfo.ComputedCommitmentUtilizedAmount.Equal(decimal.NewFromInt(10)),
		"commitment utilized: expected 10, got %s",
		item.CommitmentInfo.ComputedCommitmentUtilizedAmount)
}

// ---------------------------------------------------------------------------
// Multi-meter analytics query — exercises the no-subscription path with
// mixed aggregation types in a single call (passes a single AggregationTypes
// slice containing both SUM and COUNT).
// ---------------------------------------------------------------------------

func (s *MeterUsageServiceSuite) TestMultiMeter_MixedAggregations_NoSubscription() {
	ctx := s.GetContext()
	mSum := s.createMeterWithAggregation(ctx, "mtr_mix_sum", "ev_sum", types.AggregationSum)
	mCnt := s.createMeterWithAggregation(ctx, "mtr_mix_cnt", "ev_cnt", types.AggregationCount)

	// SUM meter: 3 events qty 10 + 20 + 30 = 60.
	s.insertMeterUsageFull(ctx, mSum.ID, "unknown_cust", "", "ev_sum",
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 10, "", nil)
	s.insertMeterUsageFull(ctx, mSum.ID, "unknown_cust", "", "ev_sum",
		time.Date(2026, 1, 6, 10, 0, 0, 0, time.UTC), 20, "", nil)
	s.insertMeterUsageFull(ctx, mSum.ID, "unknown_cust", "", "ev_sum",
		time.Date(2026, 1, 7, 10, 0, 0, 0, time.UTC), 30, "", nil)
	// COUNT meter: 4 distinct events.
	for i := 0; i < 4; i++ {
		s.insertMeterUsageFull(ctx, mCnt.ID, "unknown_cust", "", "ev_cnt",
			time.Date(2026, 1, 5, 10, i, 0, 0, time.UTC), 1, "", nil)
	}

	resp, err := s.svc.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: "unknown_cust",
		MeterIDs:           []string{mSum.ID, mCnt.ID},
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
	})
	s.NoError(err)

	byMeter := map[string]decimal.Decimal{}
	for _, item := range resp.Items {
		byMeter[item.MeterID] = item.TotalUsage
	}
	// With mixed aggregations, the SUM in the priority order populates total_usage
	// for any row that GROUPs together SUM rows; COUNT rows still report their
	// distinct-event count via the same column thanks to the priority fallback.
	// Concretely: SUM meter → 60; COUNT meter → 4 distinct events.
	s.True(byMeter[mSum.ID].Equal(decimal.NewFromInt(60)),
		"multi-meter SUM total: expected 60, got %s", byMeter[mSum.ID])
	s.True(byMeter[mCnt.ID].Equal(decimal.NewFromInt(4)),
		"multi-meter COUNT total: expected 4, got %s", byMeter[mCnt.ID])
}

// ---------------------------------------------------------------------------
// Mixed-aggregation regression — MAX + LATEST. Pre-fix, the fallback path
// sent both meters through one repo call with AggregationTypes=[MAX,LATEST].
// buildMeterUsageAggregationColumns prefers MAX, so total_usage came back as
// MAX(qty_total) for every row — wrong for the LATEST meter (which should
// report argMax(qty_total, timestamp)). With the split fix each meter gets
// its own primary expression, so values are correct.
// ---------------------------------------------------------------------------

func (s *MeterUsageServiceSuite) TestMultiMeter_MaxAndLatest_NoSubscription() {
	ctx := s.GetContext()
	mMax := s.createMeterWithAggregation(ctx, "mtr_mix_max", "ev_max", types.AggregationMax)
	mLatest := s.createMeterWithAggregation(ctx, "mtr_mix_latest", "ev_latest", types.AggregationLatest)

	// MAX meter: 3 events qty 5, 30, 12 → MAX=30.
	s.insertMeterUsageFull(ctx, mMax.ID, "unknown_cust", "", "ev_max",
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 5, "", nil)
	s.insertMeterUsageFull(ctx, mMax.ID, "unknown_cust", "", "ev_max",
		time.Date(2026, 1, 6, 10, 0, 0, 0, time.UTC), 30, "", nil)
	s.insertMeterUsageFull(ctx, mMax.ID, "unknown_cust", "", "ev_max",
		time.Date(2026, 1, 7, 10, 0, 0, 0, time.UTC), 12, "", nil)

	// LATEST meter: 3 events qty 100, 200, 7 at increasing timestamps → LATEST=7.
	// Critically, MAX of this set is 200, so a MAX-poisoned total_usage would
	// be 200 — clearly distinguishable from the correct LATEST=7.
	s.insertMeterUsageFull(ctx, mLatest.ID, "unknown_cust", "", "ev_latest",
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 100, "", nil)
	s.insertMeterUsageFull(ctx, mLatest.ID, "unknown_cust", "", "ev_latest",
		time.Date(2026, 1, 6, 10, 0, 0, 0, time.UTC), 200, "", nil)
	s.insertMeterUsageFull(ctx, mLatest.ID, "unknown_cust", "", "ev_latest",
		time.Date(2026, 1, 7, 10, 0, 0, 0, time.UTC), 7, "", nil)

	resp, err := s.svc.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: "unknown_cust",
		MeterIDs:           []string{mMax.ID, mLatest.ID},
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
	})
	s.NoError(err)

	byMeter := map[string]decimal.Decimal{}
	for _, item := range resp.Items {
		byMeter[item.MeterID] = item.TotalUsage
	}
	s.True(byMeter[mMax.ID].Equal(decimal.NewFromInt(30)),
		"MAX meter total: expected 30, got %s", byMeter[mMax.ID])
	s.True(byMeter[mLatest.ID].Equal(decimal.NewFromInt(7)),
		"LATEST meter total: expected 7 (would be 200 under priority-collapse bug), got %s", byMeter[mLatest.ID])
}

// TestMultiMeter_MixedAggregations_GroupByAndFilter exercises the no-sub
// fallback with the full combination: three meters with distinct aggregation
// types (SUM, MAX, COUNT), a user-supplied group_by on a property field, and
// a property filter. Each meter gets its own subquery (per the split-by-agg
// pattern); each subquery applies the filter and groups by (meter_id, region).
// The converter then produces one item per (meter, region) with the correct
// per-meter primary aggregation in TotalUsage.
func (s *MeterUsageServiceSuite) TestMultiMeter_MixedAggregations_GroupByAndFilter() {
	ctx := s.GetContext()
	mSum := s.createMeterWithAggregation(ctx, "mtr_full_sum", "ev_sum", types.AggregationSum)
	mMax := s.createMeterWithAggregation(ctx, "mtr_full_max", "ev_max", types.AggregationMax)
	mCnt := s.createMeterWithAggregation(ctx, "mtr_full_cnt", "ev_cnt", types.AggregationCount)

	// us-east + cloud=aws — should pass filter.
	props := func(region, cloud string) map[string]interface{} {
		return map[string]interface{}{"region": region, "cloud": cloud}
	}

	// SUM meter: us-east+aws → 10+20=30; us-west+aws → 50; us-east+gcp → filtered out.
	s.insertMeterUsageFull(ctx, mSum.ID, "unknown_cust", "", "ev_sum",
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 10, "", props("us-east", "aws"))
	s.insertMeterUsageFull(ctx, mSum.ID, "unknown_cust", "", "ev_sum",
		time.Date(2026, 1, 6, 10, 0, 0, 0, time.UTC), 20, "", props("us-east", "aws"))
	s.insertMeterUsageFull(ctx, mSum.ID, "unknown_cust", "", "ev_sum",
		time.Date(2026, 1, 7, 10, 0, 0, 0, time.UTC), 50, "", props("us-west", "aws"))
	s.insertMeterUsageFull(ctx, mSum.ID, "unknown_cust", "", "ev_sum",
		time.Date(2026, 1, 8, 10, 0, 0, 0, time.UTC), 999, "", props("us-east", "gcp"))

	// MAX meter: us-east+aws → max(7,15)=15; us-west+aws → 99; us-east+gcp → filtered.
	s.insertMeterUsageFull(ctx, mMax.ID, "unknown_cust", "", "ev_max",
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 7, "", props("us-east", "aws"))
	s.insertMeterUsageFull(ctx, mMax.ID, "unknown_cust", "", "ev_max",
		time.Date(2026, 1, 6, 10, 0, 0, 0, time.UTC), 15, "", props("us-east", "aws"))
	s.insertMeterUsageFull(ctx, mMax.ID, "unknown_cust", "", "ev_max",
		time.Date(2026, 1, 7, 10, 0, 0, 0, time.UTC), 99, "", props("us-west", "aws"))
	s.insertMeterUsageFull(ctx, mMax.ID, "unknown_cust", "", "ev_max",
		time.Date(2026, 1, 8, 10, 0, 0, 0, time.UTC), 8888, "", props("us-east", "gcp"))

	// COUNT meter: us-east+aws → 3 distinct ids; us-west+aws → 1; us-east+gcp → filtered.
	for i := 0; i < 3; i++ {
		s.insertMeterUsageFull(ctx, mCnt.ID, "unknown_cust", "", "ev_cnt",
			time.Date(2026, 1, 5, 10, i, 0, 0, time.UTC), 1, "", props("us-east", "aws"))
	}
	s.insertMeterUsageFull(ctx, mCnt.ID, "unknown_cust", "", "ev_cnt",
		time.Date(2026, 1, 7, 10, 0, 0, 0, time.UTC), 1, "", props("us-west", "aws"))
	s.insertMeterUsageFull(ctx, mCnt.ID, "unknown_cust", "", "ev_cnt",
		time.Date(2026, 1, 8, 10, 0, 0, 0, time.UTC), 1, "", props("us-east", "gcp"))

	resp, err := s.svc.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: "unknown_cust",
		MeterIDs:           []string{mSum.ID, mMax.ID, mCnt.ID},
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
		GroupBy:            []string{"properties.region"},
		PropertyFilters:    map[string][]string{"cloud": {"aws"}},
	})
	s.NoError(err)

	// Key by (meter, region) → expected primary value.
	type k struct{ meter, region string }
	got := map[k]decimal.Decimal{}
	for _, item := range resp.Items {
		got[k{item.MeterID, item.Properties["region"]}] = item.TotalUsage
	}

	s.True(got[k{mSum.ID, "us-east"}].Equal(decimal.NewFromInt(30)),
		"SUM us-east: expected 30, got %s", got[k{mSum.ID, "us-east"}])
	s.True(got[k{mSum.ID, "us-west"}].Equal(decimal.NewFromInt(50)),
		"SUM us-west: expected 50, got %s", got[k{mSum.ID, "us-west"}])
	s.True(got[k{mMax.ID, "us-east"}].Equal(decimal.NewFromInt(15)),
		"MAX us-east: expected 15, got %s", got[k{mMax.ID, "us-east"}])
	s.True(got[k{mMax.ID, "us-west"}].Equal(decimal.NewFromInt(99)),
		"MAX us-west: expected 99, got %s", got[k{mMax.ID, "us-west"}])
	s.True(got[k{mCnt.ID, "us-east"}].Equal(decimal.NewFromInt(3)),
		"COUNT us-east: expected 3, got %s", got[k{mCnt.ID, "us-east"}])
	s.True(got[k{mCnt.ID, "us-west"}].Equal(decimal.NewFromInt(1)),
		"COUNT us-west: expected 1, got %s", got[k{mCnt.ID, "us-west"}])

	// gcp rows must be filtered out — no (meter, "gcp") keys should exist
	// AND no value should equal the gcp-only payload (999, 8888).
	for kk, v := range got {
		s.False(v.Equal(decimal.NewFromInt(999)), "SUM gcp leaked: %v=%s", kk, v)
		s.False(v.Equal(decimal.NewFromInt(8888)), "MAX gcp leaked: %v=%s", kk, v)
	}
}

// TestAvgMeter_NoSubscriptionAnalytics: pre-fix AVG was missing from the
// primary switch in buildMeterUsageAggregationColumns and from the in-memory
// primaryAggregationValue, so AVG meters returned total_usage = 0. After fix,
// AVG meters compute AVG(qty_total) and the in-memory store mirrors that.
func (s *MeterUsageServiceSuite) TestAvgMeter_NoSubscriptionAnalytics() {
	ctx := s.GetContext()
	m := s.createMeterWithAggregation(ctx, "mtr_avg", "ev_avg", types.AggregationAvg)

	// 4 events qty 10, 20, 30, 40 → AVG = 25.
	for i, q := range []int64{10, 20, 30, 40} {
		s.insertMeterUsageFull(ctx, m.ID, "unknown_cust", "", "ev_avg",
			time.Date(2026, 1, 5, 10, i, 0, 0, time.UTC), float64(q), "", nil)
	}

	resp, err := s.svc.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: "unknown_cust",
		MeterIDs:           []string{m.ID},
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
	})
	s.NoError(err)
	s.Len(resp.Items, 1)
	s.True(resp.Items[0].TotalUsage.Equal(decimal.NewFromInt(25)),
		"AVG meter total: expected 25, got %s", resp.Items[0].TotalUsage)
}

// ---------------------------------------------------------------------------
// Time-bounding sanity for non-SUM aggregations — make sure the basic
// effective-period bounding (already tested for SUM) also works for MAX/COUNT.
// ---------------------------------------------------------------------------

func (s *MeterUsageServiceSuite) TestMaxMeter_LineItemDateBounding() {
	ctx := s.GetContext()
	m := s.createMeterWithAggregation(ctx, "mtr_max_bound", "ev_max", types.AggregationMax)
	p := s.createPriceForMeter(ctx, "pr_max_bound", m.ID, decimal.NewFromInt(1))

	// Line item active Jan 1 – Jan 15.
	li := &subscription.SubscriptionLineItem{
		ID:             "li_max_bound",
		SubscriptionID: s.sub.ID,
		CustomerID:     s.customer.ID,
		PriceID:        p.ID,
		PriceType:      types.PRICE_TYPE_USAGE,
		MeterID:        m.ID,
		Currency:       "usd",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		StartDate:      s.periodStart,
		EndDate:        time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		Quantity:       decimal.NewFromInt(1),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li))

	// In-bounds events with MAX 50.
	s.insertMeterUsageFull(ctx, m.ID, s.customer.ExternalID, "", "ev_max",
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 10, "", nil)
	s.insertMeterUsageFull(ctx, m.ID, s.customer.ExternalID, "", "ev_max",
		time.Date(2026, 1, 10, 10, 0, 0, 0, time.UTC), 50, "", nil)
	// Out-of-bounds with qty 999 — must be excluded.
	s.insertMeterUsageFull(ctx, m.ID, s.customer.ExternalID, "", "ev_max",
		time.Date(2026, 1, 20, 10, 0, 0, 0, time.UTC), 999, "", nil)

	result, err := s.svc.GetSubscriptionMeterUsage(ctx, &GetSubscriptionMeterUsageRequest{
		SubscriptionID: s.sub.ID,
		StartTime:      s.periodStart,
		EndTime:        s.periodEnd,
	})
	s.NoError(err)

	var lu *LineItemMeterUsage
	for _, x := range result.LineItemUsages {
		if x.LineItem.ID == "li_max_bound" {
			lu = x
			break
		}
	}
	s.Require().NotNil(lu)
	s.True(lu.Usage.Equal(decimal.NewFromInt(50)),
		"MAX with date bounding: expected 50 (Jan 20 event excluded), got %s", lu.Usage)
}

func (s *MeterUsageServiceSuite) TestCountMeter_LineItemDateBounding() {
	ctx := s.GetContext()
	m := s.createMeterWithAggregation(ctx, "mtr_cnt_bound", "ev_cnt", types.AggregationCount)
	p := s.createPriceForMeter(ctx, "pr_cnt_bound", m.ID, decimal.NewFromInt(1))

	li := &subscription.SubscriptionLineItem{
		ID:             "li_cnt_bound",
		SubscriptionID: s.sub.ID,
		CustomerID:     s.customer.ID,
		PriceID:        p.ID,
		PriceType:      types.PRICE_TYPE_USAGE,
		MeterID:        m.ID,
		Currency:       "usd",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		StartDate:      s.periodStart,
		EndDate:        time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		Quantity:       decimal.NewFromInt(1),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li))

	// 4 in-bounds, 2 out-of-bounds.
	for _, t := range []time.Time{
		time.Date(2026, 1, 3, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 8, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 14, 10, 0, 0, 0, time.UTC),
	} {
		s.insertMeterUsageFull(ctx, m.ID, s.customer.ExternalID, "", "ev_cnt", t, 1, "", nil)
	}
	for _, t := range []time.Time{
		time.Date(2026, 1, 20, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 25, 10, 0, 0, 0, time.UTC),
	} {
		s.insertMeterUsageFull(ctx, m.ID, s.customer.ExternalID, "", "ev_cnt", t, 1, "", nil)
	}

	result, err := s.svc.GetSubscriptionMeterUsage(ctx, &GetSubscriptionMeterUsageRequest{
		SubscriptionID: s.sub.ID,
		StartTime:      s.periodStart,
		EndTime:        s.periodEnd,
	})
	s.NoError(err)

	var lu *LineItemMeterUsage
	for _, x := range result.LineItemUsages {
		if x.LineItem.ID == "li_cnt_bound" {
			lu = x
			break
		}
	}
	s.Require().NotNil(lu)
	s.True(lu.Usage.Equal(decimal.NewFromInt(4)),
		"COUNT with date bounding: expected 4 in-bounds events, got %s", lu.Usage)
	s.Equal(uint64(4), lu.EventCount,
		"COUNT EventCount: expected 4, got %d", lu.EventCount)
}

// TestGroupByFeatureID_RewritesToMeterID: the API contract (dto/events.go)
// documents group_by=[feature_id], but meter_usage has no feature_id column.
// The service rewrites feature_id → meter_id at entry (features are 1:1 with
// meters), and the converter populates FeatureID on each item via the
// meter→feature lookup. This test pins both: the query no longer errors out,
// AND callers still get FeatureID in the response.
func (s *MeterUsageServiceSuite) TestGroupByFeatureID_RewritesToMeterID() {
	ctx := s.GetContext()
	s.createLineItem(ctx, "li_feat_groupby", s.periodStart, s.periodEnd)

	// Feature pointing at the existing s.meterAPI.
	feat := &feature.Feature{
		ID: "feat_api", Name: "API Feature", MeterID: s.meterAPI.ID,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().FeatureRepo.Create(ctx, feat))

	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID,
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 25)

	resp, err := s.svc.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: s.customer.ExternalID,
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
		GroupBy:            []string{"feature_id"}, // public contract — must not error
	})
	s.NoError(err)
	s.Require().Len(resp.Items, 1, "expected one item for the single meter")
	s.Equal(feat.ID, resp.Items[0].FeatureID,
		"FeatureID should be populated from meter→feature lookup")
	s.True(resp.Items[0].TotalUsage.Equal(decimal.NewFromInt(25)),
		"TotalUsage: expected 25, got %s", resp.Items[0].TotalUsage)
}

// TestGroupByFeatureIDAndMeterID_Deduplicates: passing both feature_id and
// meter_id in GroupBy shouldn't produce [meter_id, meter_id] after rewrite.
func (s *MeterUsageServiceSuite) TestGroupByFeatureIDAndMeterID_Deduplicates() {
	ctx := s.GetContext()
	s.createLineItem(ctx, "li_feat_dedup", s.periodStart, s.periodEnd)

	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID,
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 42)

	resp, err := s.svc.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: s.customer.ExternalID,
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
		GroupBy:            []string{"feature_id", "meter_id"},
	})
	s.NoError(err)
	s.Require().Len(resp.Items, 1)
	s.True(resp.Items[0].TotalUsage.Equal(decimal.NewFromInt(42)))
}
