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

// TestWindowCommitment_NoTimeBuckets_AppliesToAllWindows verifies the windowed
// per-window commitment path (no per-bucket pricing): when CommitmentTimeBuckets
// is omitted, every window with usage takes the commitment path regardless of
// hour-of-day. This guards against regressions where an empty/nil TimeBuckets
// accidentally filters everything out.
//
// Setup:
//   - hourly SUM meter, $1/unit
//   - $5 commitment per window, 2x overage factor, true-up disabled
//   - 10:00 UTC event: 10 units → cost $10 > $5 → $5 + ($5*2) = $15
//   - 18:00 UTC event: 10 units → cost $10 > $5 → $5 + ($5*2) = $15
//
// Expected TotalCost = $30 (both windows take the overage path).
func (s *MeterUsageServiceSuite) TestWindowCommitment_NoTimeBuckets_AppliesToAllWindows() {
	ctx := s.GetContext()

	bucketedMeter := &meter.Meter{
		ID:        "meter_no_tb",
		Name:      "Hourly Bucketed SUM (no time buckets)",
		EventName: "api_call",
		Aggregation: meter.Aggregation{
			Type:       types.AggregationSum,
			BucketSize: types.WindowSizeHour,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, bucketedMeter))

	flatPrice := &price.Price{
		ID:             "price_no_tb",
		Amount:         decimal.NewFromInt(1),
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
	s.NoError(s.GetStores().PriceRepo.Create(ctx, flatPrice))

	commitmentAmount := decimal.NewFromInt(5)
	overageFactor := decimal.NewFromInt(2)
	li := &subscription.SubscriptionLineItem{
		ID:                      "li_no_tb",
		SubscriptionID:          s.sub.ID,
		CustomerID:              s.customer.ID,
		PriceID:                 flatPrice.ID,
		PriceType:               types.PRICE_TYPE_USAGE,
		MeterID:                 bucketedMeter.ID,
		Currency:                "usd",
		BillingPeriod:           types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence:          types.InvoiceCadenceArrear,
		StartDate:               s.periodStart,
		EndDate:                 s.periodEnd,
		Quantity:                decimal.NewFromInt(1),
		CommitmentType:          types.COMMITMENT_TYPE_AMOUNT,
		CommitmentAmount:        &commitmentAmount,
		CommitmentOverageFactor: &overageFactor,
		CommitmentTrueUpEnabled: false,
		CommitmentWindowed:      true,
		// CommitmentTimeBuckets intentionally omitted — no time-of-day restriction.
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li))

	// Same two events as the time-bucket test, deliberately at hours that
	// would be in- and out-of-bucket under a 09:00-17:00 restriction.
	s.insertMeterUsage(ctx, bucketedMeter.ID, s.customer.ExternalID,
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 10)
	s.insertMeterUsage(ctx, bucketedMeter.ID, s.customer.ExternalID,
		time.Date(2026, 1, 5, 18, 0, 0, 0, time.UTC), 10)

	resp, err := s.svc.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: s.customer.ExternalID,
		MeterIDs:           []string{bucketedMeter.ID},
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
	})
	s.NoError(err)

	var item *dto.UsageAnalyticItem
	for i := range resp.Items {
		if resp.Items[i].SubLineItemID == "li_no_tb" {
			item = &resp.Items[i]
			break
		}
	}
	s.Require().NotNil(item, "expected analytic for no-time-bucket line item")

	// Both windows take the overage path: $15 + $15 = $30.
	expectedTotal := decimal.NewFromInt(30)
	s.True(item.TotalCost.Equal(expectedTotal),
		"expected $30 (both windows in overage; no time-bucket restriction); got %s",
		item.TotalCost)
}

// TestWindowCommitment_PerBucket_BreakdownAndSummaries verifies the per-bucket
// commitment path through meter-usage analytics: in-bucket usage is priced and
// committed by the bucket's own price/commitment, out-of-bucket usage falls back
// to the line item, and (with breakdown_bucket=true) each point is stamped with
// its BucketID and a per-bucket summary is produced.
//
// Setup: hourly SUM meter; line item price $1/unit with a $5 line-item
// commitment (2x overage) for out-of-bucket; one bucket [09:00,12:00) with its
// own $2/unit price, $5 commitment, 2x overage.
//
//	10:00 UTC (in-bucket):    10u × $2 = $20 base → $5 + ($15×2) = $35
//	18:00 UTC (out-of-bucket): 10u × $1 = $10 base → $5 + ($5×2) = $15
//
// Expected TotalCost = $50.
func (s *MeterUsageServiceSuite) TestWindowCommitment_PerBucket_BreakdownAndSummaries() {
	ctx := s.GetContext()

	bucketedMeter := &meter.Meter{
		ID:        "meter_pb_window",
		Name:      "Hourly Bucketed SUM (per-bucket)",
		EventName: "api_call",
		Aggregation: meter.Aggregation{
			Type:       types.AggregationSum,
			BucketSize: types.WindowSizeHour,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, bucketedMeter))

	// Line item (out-of-bucket) price: $1/unit.
	linePrice := &price.Price{
		ID:             "price_pb_line",
		Amount:         decimal.NewFromInt(1),
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
	s.NoError(s.GetStores().PriceRepo.Create(ctx, linePrice))

	// Bucket (in-bucket) price: $2/unit, SUBSCRIPTION-scoped.
	bucketPrice := &price.Price{
		ID:             "price_pb_bucket",
		Amount:         decimal.NewFromInt(2),
		Currency:       "usd",
		EntityType:     types.PRICE_ENTITY_TYPE_SUBSCRIPTION,
		EntityID:       s.sub.ID,
		BillingModel:   types.BILLING_MODEL_FLAT_FEE,
		Type:           types.PRICE_TYPE_USAGE,
		MeterID:        bucketedMeter.ID,
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, bucketPrice))

	commitmentAmount := decimal.NewFromInt(5)
	overageFactor := decimal.NewFromInt(2)
	li := &subscription.SubscriptionLineItem{
		ID:                      "li_pb_window",
		SubscriptionID:          s.sub.ID,
		CustomerID:              s.customer.ID,
		PriceID:                 linePrice.ID,
		PriceType:               types.PRICE_TYPE_USAGE,
		MeterID:                 bucketedMeter.ID,
		Currency:                "usd",
		BillingPeriod:           types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence:          types.InvoiceCadenceArrear,
		StartDate:               s.periodStart,
		EndDate:                 s.periodEnd,
		Quantity:                decimal.NewFromInt(1),
		CommitmentType:          types.COMMITMENT_TYPE_AMOUNT,
		CommitmentAmount:        &commitmentAmount,
		CommitmentOverageFactor: &overageFactor,
		CommitmentTrueUpEnabled: false,
		CommitmentWindowed:      true,
		CommitmentTimeBuckets: types.TimeOfDayBuckets{
			{
				ID:              "bkt_morning",
				Start:           types.Bucket{Hour: 9, Minute: 0},
				End:             types.Bucket{Hour: 12, Minute: 0},
				PriceID:         bucketPrice.ID,
				CommitmentType:  types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.NewFromInt(5),
				OverageFactor:   &overageFactor,
			},
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li))

	// 10:00 UTC in-bucket, 18:00 UTC out-of-bucket, 10 units each.
	s.insertMeterUsage(ctx, bucketedMeter.ID, s.customer.ExternalID,
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 10)
	s.insertMeterUsage(ctx, bucketedMeter.ID, s.customer.ExternalID,
		time.Date(2026, 1, 5, 18, 0, 0, 0, time.UTC), 10)

	resp, err := s.svc.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: s.customer.ExternalID,
		MeterIDs:           []string{bucketedMeter.ID},
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
		WindowSize:         types.WindowSizeHour,
		BreakdownBucket:    true,
	})
	s.NoError(err)

	var item *dto.UsageAnalyticItem
	for i := range resp.Items {
		if resp.Items[i].SubLineItemID == "li_pb_window" {
			item = &resp.Items[i]
			break
		}
	}
	s.Require().NotNil(item, "expected analytic for per-bucket line item")

	// In-bucket overage ($35) + out-of-bucket overage ($15) = $50.
	s.True(item.TotalCost.Equal(decimal.NewFromInt(50)),
		"expected $50 (in-bucket $35 via bucket price + out-of-bucket $15); got %s", item.TotalCost)

	// Per-point bucket identity: the 10:00 window must be stamped with the bucket.
	var inBucketPoints int
	for _, pt := range item.Points {
		if pt.BucketID == "bkt_morning" {
			inBucketPoints++
			s.Equal(bucketPrice.ID, pt.PriceID, "in-bucket point must carry the bucket price id")
		}
	}
	s.Positive(inBucketPoints, "expected at least one point stamped with the bucket id")

	// Bucket summaries: one per configured bucket; out-of-bucket usage is not
	// summarized (the item's CommitmentInfo carries the line-item totals).
	s.Require().Len(item.BucketSummaries, 1, "expected one summary per configured bucket")
	bucketSummary := item.BucketSummaries[0]
	s.Equal("bkt_morning", bucketSummary.BucketID)
	s.True(bucketSummary.TotalUsage.Equal(decimal.NewFromInt(10)), "bucket usage should be 10, got %s", bucketSummary.TotalUsage)
	s.True(bucketSummary.BaseCharge.Equal(decimal.NewFromInt(20)), "bucket base charge should be $20 (10u × $2), got %s", bucketSummary.BaseCharge)
	s.True(bucketSummary.ComputedOverage.GreaterThan(decimal.Zero), "bucket overage should be positive, got %s", bucketSummary.ComputedOverage)
}

// TestWindowCommitment_MultipleBuckets_WithTrueUp verifies two commitment
// buckets with bucket-level true-up: empty windows inside each bucket are
// filled and trued up to that bucket's commitment, even though the line item's
// top-level true-up flag is OFF (bucket-level TrueUpEnabled alone must engage
// the window-fill path).
//
// Line item scoped to one day (Jan 5 → Jan 6) so the grid is 24 hourly windows.
//
//	bucket A [09:00,12:00): $2/u, amount commit $5/window, overage 2x, true-up ON
//	bucket B [12:00,15:00): $1/u, amount commit $3/window, overage 2x, true-up ON
//	line item: $1/u, NO top-level commitment
//
// Events: 10:00 → 10u; 18:00 → 10u.
//
//	A: 09 empty → true-up $5; 10 → 10×$2=$20 ≥ $5 → $5+($15×2)=$35; 11 empty → $5
//	B: 12,13,14 empty → true-up $3×3 = $9
//	out-of-bucket: 18:00 → 10×$1=$10 base; other empty windows $0
//
// Total = $64; true-up $19, overage $30, utilized $15 (sum invariant holds).
func (s *MeterUsageServiceSuite) TestWindowCommitment_MultipleBuckets_WithTrueUp() {
	ctx := s.GetContext()

	bucketedMeter := &meter.Meter{
		ID:        "meter_multi_tb",
		Name:      "Hourly SUM (multi-bucket true-up)",
		EventName: "api_call",
		Aggregation: meter.Aggregation{
			Type:       types.AggregationSum,
			BucketSize: types.WindowSizeHour,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, bucketedMeter))

	linePrice := &price.Price{
		ID: "price_multi_tb_line", Amount: decimal.NewFromInt(1), Currency: "usd",
		EntityType: types.PRICE_ENTITY_TYPE_PLAN, EntityID: "plan_1",
		BillingModel: types.BILLING_MODEL_FLAT_FEE, Type: types.PRICE_TYPE_USAGE,
		MeterID: bucketedMeter.ID, BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, linePrice))

	bucketPriceA := &price.Price{
		ID: "price_multi_tb_a", Amount: decimal.NewFromInt(2), Currency: "usd",
		EntityType: types.PRICE_ENTITY_TYPE_SUBSCRIPTION, EntityID: s.sub.ID,
		BillingModel: types.BILLING_MODEL_FLAT_FEE, Type: types.PRICE_TYPE_USAGE,
		MeterID: bucketedMeter.ID, BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, bucketPriceA))

	bucketPriceB := &price.Price{
		ID: "price_multi_tb_b", Amount: decimal.NewFromInt(1), Currency: "usd",
		EntityType: types.PRICE_ENTITY_TYPE_SUBSCRIPTION, EntityID: s.sub.ID,
		BillingModel: types.BILLING_MODEL_FLAT_FEE, Type: types.PRICE_TYPE_USAGE,
		MeterID: bucketedMeter.ID, BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, bucketPriceB))

	overage := decimal.NewFromInt(2)
	li := &subscription.SubscriptionLineItem{
		ID:             "li_multi_tb",
		SubscriptionID: s.sub.ID,
		CustomerID:     s.customer.ID,
		PriceID:        linePrice.ID,
		PriceType:      types.PRICE_TYPE_USAGE,
		MeterID:        bucketedMeter.ID,
		Currency:       "usd",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		// One-day scope keeps the expected-window math tractable: 24 hourly windows.
		StartDate: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC),
		Quantity:  decimal.NewFromInt(1),
		// No top-level commitment; top-level true-up OFF — bucket-level true-up
		// alone must engage the fill path.
		CommitmentTrueUpEnabled: false,
		CommitmentWindowed:      true,
		CommitmentTimeBuckets: types.TimeOfDayBuckets{
			{
				ID: "bkt_a", Start: types.Bucket{Hour: 9}, End: types.Bucket{Hour: 12},
				PriceID: bucketPriceA.ID, CommitmentType: types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.NewFromInt(5), OverageFactor: &overage, TrueUpEnabled: true,
			},
			{
				ID: "bkt_b", Start: types.Bucket{Hour: 12}, End: types.Bucket{Hour: 15},
				PriceID: bucketPriceB.ID, CommitmentType: types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.NewFromInt(3), OverageFactor: &overage, TrueUpEnabled: true,
			},
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li))

	s.insertMeterUsage(ctx, bucketedMeter.ID, s.customer.ExternalID,
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 10)
	s.insertMeterUsage(ctx, bucketedMeter.ID, s.customer.ExternalID,
		time.Date(2026, 1, 5, 18, 0, 0, 0, time.UTC), 10)

	resp, err := s.svc.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: s.customer.ExternalID,
		MeterIDs:           []string{bucketedMeter.ID},
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
	})
	s.NoError(err)

	var item *dto.UsageAnalyticItem
	for i := range resp.Items {
		if resp.Items[i].SubLineItemID == "li_multi_tb" {
			item = &resp.Items[i]
			break
		}
	}
	s.Require().NotNil(item, "expected analytic for multi-bucket line item")

	s.True(item.TotalCost.Equal(decimal.NewFromInt(64)),
		"expected $64 (A: $5+$35+$5, B: $9 true-up, out: $10); got %s", item.TotalCost)

	s.Require().NotNil(item.CommitmentInfo, "windowed fill path must record commitment info")
	s.True(item.CommitmentInfo.ComputedTrueUpAmount.Equal(decimal.NewFromInt(19)),
		"expected true-up $19 (A: $10, B: $9); got %s", item.CommitmentInfo.ComputedTrueUpAmount)
	s.True(item.CommitmentInfo.ComputedOverageAmount.Equal(decimal.NewFromInt(30)),
		"expected overage $30; got %s", item.CommitmentInfo.ComputedOverageAmount)
	s.True(item.CommitmentInfo.ComputedCommitmentUtilizedAmount.Equal(decimal.NewFromInt(15)),
		"expected utilized $15; got %s", item.CommitmentInfo.ComputedCommitmentUtilizedAmount)
	// Sum invariant: total = utilized + overage + true-up.
	s.True(item.TotalCost.Equal(
		item.CommitmentInfo.ComputedCommitmentUtilizedAmount.
			Add(item.CommitmentInfo.ComputedOverageAmount).
			Add(item.CommitmentInfo.ComputedTrueUpAmount)))
}

// TestWindowCommitment_Bucket_SlabPricing_OverageFactorOne verifies a bucket
// whose price is TIERED/SLAB and whose overage factor is exactly 1.0 (allowed
// for buckets): usage beyond the commitment bills at the slab rate with no
// premium, so the in-bucket charge equals the raw slab cost.
//
//	bucket [09:00,12:00): SLAB tiers — first 5u @ $2, rest @ $1;
//	                      amount commit $5/window, overage 1x, no true-up
//	line item: $1/u flat, no commitment
//
// Events: 10:00 → 8u (in-bucket); 18:00 → 4u (out-of-bucket).
//
//	10:00: slab cost = 5×$2 + 3×$1 = $13 ≥ $5 → $5 + ($8×1) = $13
//	18:00: 4×$1 = $4 base rate
//
// Total = $17.
func (s *MeterUsageServiceSuite) TestWindowCommitment_Bucket_SlabPricing_OverageFactorOne() {
	ctx := s.GetContext()

	bucketedMeter := &meter.Meter{
		ID:        "meter_slab_tb",
		Name:      "Hourly SUM (slab bucket)",
		EventName: "api_call",
		Aggregation: meter.Aggregation{
			Type:       types.AggregationSum,
			BucketSize: types.WindowSizeHour,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, bucketedMeter))

	linePrice := &price.Price{
		ID: "price_slab_tb_line", Amount: decimal.NewFromInt(1), Currency: "usd",
		EntityType: types.PRICE_ENTITY_TYPE_PLAN, EntityID: "plan_1",
		BillingModel: types.BILLING_MODEL_FLAT_FEE, Type: types.PRICE_TYPE_USAGE,
		MeterID: bucketedMeter.ID, BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, linePrice))

	upTo5 := uint64(5)
	slabPrice := &price.Price{
		ID: "price_slab_tb_bucket", Currency: "usd",
		EntityType: types.PRICE_ENTITY_TYPE_SUBSCRIPTION, EntityID: s.sub.ID,
		BillingModel: types.BILLING_MODEL_TIERED, TierMode: types.BILLING_TIER_SLAB,
		Tiers: []price.PriceTier{
			{UpTo: &upTo5, UnitAmount: decimal.NewFromInt(2)},
			{UnitAmount: decimal.NewFromInt(1)},
		},
		Type: types.PRICE_TYPE_USAGE, MeterID: bucketedMeter.ID,
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, slabPrice))

	overageOne := decimal.NewFromInt(1)
	li := &subscription.SubscriptionLineItem{
		ID:                 "li_slab_tb",
		SubscriptionID:     s.sub.ID,
		CustomerID:         s.customer.ID,
		PriceID:            linePrice.ID,
		PriceType:          types.PRICE_TYPE_USAGE,
		MeterID:            bucketedMeter.ID,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		StartDate:          s.periodStart,
		EndDate:            s.periodEnd,
		Quantity:           decimal.NewFromInt(1),
		CommitmentWindowed: true,
		CommitmentTimeBuckets: types.TimeOfDayBuckets{
			{
				ID: "bkt_slab", Start: types.Bucket{Hour: 9}, End: types.Bucket{Hour: 12},
				PriceID: slabPrice.ID, CommitmentType: types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.NewFromInt(5), OverageFactor: &overageOne,
			},
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li))

	s.insertMeterUsage(ctx, bucketedMeter.ID, s.customer.ExternalID,
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 8)
	s.insertMeterUsage(ctx, bucketedMeter.ID, s.customer.ExternalID,
		time.Date(2026, 1, 5, 18, 0, 0, 0, time.UTC), 4)

	resp, err := s.svc.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: s.customer.ExternalID,
		MeterIDs:           []string{bucketedMeter.ID},
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
		WindowSize:         types.WindowSizeHour,
		BreakdownBucket:    true,
	})
	s.NoError(err)

	var item *dto.UsageAnalyticItem
	for i := range resp.Items {
		if resp.Items[i].SubLineItemID == "li_slab_tb" {
			item = &resp.Items[i]
			break
		}
	}
	s.Require().NotNil(item, "expected analytic for slab-bucket line item")

	// With overage factor 1, the in-bucket charge equals the raw slab cost.
	s.True(item.TotalCost.Equal(decimal.NewFromInt(17)),
		"expected $17 (slab $13 in-bucket + $4 out-of-bucket); got %s", item.TotalCost)

	// Per-point: the 10:00 point bills $13 with $8 overage at no premium.
	var inBucketPoint *dto.UsageAnalyticPoint
	for i := range item.Points {
		if item.Points[i].BucketID == "bkt_slab" && item.Points[i].Usage.Equal(decimal.NewFromInt(8)) {
			inBucketPoint = &item.Points[i]
			break
		}
	}
	s.Require().NotNil(inBucketPoint, "expected the 8-unit point stamped with the slab bucket")
	s.True(inBucketPoint.Cost.Equal(decimal.NewFromInt(13)),
		"expected in-bucket point cost $13 (slab), got %s", inBucketPoint.Cost)
	s.True(inBucketPoint.ComputedOverageAmount.Equal(decimal.NewFromInt(8)),
		"expected overage $8 ($13−$5 at 1x), got %s", inBucketPoint.ComputedOverageAmount)
	s.True(inBucketPoint.ComputedCommitmentUtilizedAmount.Equal(decimal.NewFromInt(5)),
		"expected utilized $5, got %s", inBucketPoint.ComputedCommitmentUtilizedAmount)
}

// TestWindowCommitment_LineItemAndBucketCommitment verifies BOTH commitment
// levels acting together per window: in-bucket windows use the bucket's price +
// commitment, while out-of-bucket windows use the line item's own commitment —
// including true-up over filled empty windows (line-item true-up ON pulls every
// window into the grid).
//
// Line item scoped to one day (Jan 5 → Jan 6): 24 hourly windows.
//
//	bucket [09:00,12:00): $2/u, amount commit $5/window, overage 2x, true-up OFF
//	line item: $1/u, amount commit $10/window, overage 2x, true-up ON
//
// Events: 10:00 → 10u (in-bucket); 18:00 → 3u (out-of-bucket).
//
//	in-bucket:  09,11 empty → $0 (bucket true-up off); 10 → $20 ≥ $5 → $5+($15×2)=$35
//	out-of-bucket: 18:00 → $3 < $10 → true-up to $10; 20 empty windows → $10 each = $200
//
// Total = $245; utilized $8 ($5 + $3), overage $30, true-up $207 ($7 + $200).
func (s *MeterUsageServiceSuite) TestWindowCommitment_LineItemAndBucketCommitment() {
	ctx := s.GetContext()

	bucketedMeter := &meter.Meter{
		ID:        "meter_combined_tb",
		Name:      "Hourly SUM (line-item + bucket commitment)",
		EventName: "api_call",
		Aggregation: meter.Aggregation{
			Type:       types.AggregationSum,
			BucketSize: types.WindowSizeHour,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, bucketedMeter))

	linePrice := &price.Price{
		ID: "price_combined_line", Amount: decimal.NewFromInt(1), Currency: "usd",
		EntityType: types.PRICE_ENTITY_TYPE_PLAN, EntityID: "plan_1",
		BillingModel: types.BILLING_MODEL_FLAT_FEE, Type: types.PRICE_TYPE_USAGE,
		MeterID: bucketedMeter.ID, BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, linePrice))

	bucketPrice := &price.Price{
		ID: "price_combined_bucket", Amount: decimal.NewFromInt(2), Currency: "usd",
		EntityType: types.PRICE_ENTITY_TYPE_SUBSCRIPTION, EntityID: s.sub.ID,
		BillingModel: types.BILLING_MODEL_FLAT_FEE, Type: types.PRICE_TYPE_USAGE,
		MeterID: bucketedMeter.ID, BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, bucketPrice))

	overage := decimal.NewFromInt(2)
	liCommit := decimal.NewFromInt(10)
	li := &subscription.SubscriptionLineItem{
		ID:             "li_combined_tb",
		SubscriptionID: s.sub.ID,
		CustomerID:     s.customer.ID,
		PriceID:        linePrice.ID,
		PriceType:      types.PRICE_TYPE_USAGE,
		MeterID:        bucketedMeter.ID,
		Currency:       "usd",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		StartDate:      time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
		EndDate:        time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC),
		Quantity:       decimal.NewFromInt(1),
		// Top-level commitment WITH true-up: out-of-bucket windows true up to
		// $10 each; the true-up flag also pulls every window into the fill grid.
		CommitmentType:          types.COMMITMENT_TYPE_AMOUNT,
		CommitmentAmount:        &liCommit,
		CommitmentOverageFactor: &overage,
		CommitmentTrueUpEnabled: true,
		CommitmentWindowed:      true,
		CommitmentTimeBuckets: types.TimeOfDayBuckets{
			{
				ID: "bkt_combined", Start: types.Bucket{Hour: 9}, End: types.Bucket{Hour: 12},
				PriceID: bucketPrice.ID, CommitmentType: types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.NewFromInt(5), OverageFactor: &overage, TrueUpEnabled: false,
			},
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li))

	s.insertMeterUsage(ctx, bucketedMeter.ID, s.customer.ExternalID,
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 10)
	s.insertMeterUsage(ctx, bucketedMeter.ID, s.customer.ExternalID,
		time.Date(2026, 1, 5, 18, 0, 0, 0, time.UTC), 3)

	resp, err := s.svc.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: s.customer.ExternalID,
		MeterIDs:           []string{bucketedMeter.ID},
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
	})
	s.NoError(err)

	var item *dto.UsageAnalyticItem
	for i := range resp.Items {
		if resp.Items[i].SubLineItemID == "li_combined_tb" {
			item = &resp.Items[i]
			break
		}
	}
	s.Require().NotNil(item, "expected analytic for combined-commitment line item")

	s.True(item.TotalCost.Equal(decimal.NewFromInt(245)),
		"expected $245 (bucket $35 + out-of-bucket $10 + 20×$10 true-up); got %s", item.TotalCost)

	s.Require().NotNil(item.CommitmentInfo)
	s.True(item.CommitmentInfo.ComputedTrueUpAmount.Equal(decimal.NewFromInt(207)),
		"expected true-up $207 ($7 at 18:00 + $200 empty windows); got %s", item.CommitmentInfo.ComputedTrueUpAmount)
	s.True(item.CommitmentInfo.ComputedOverageAmount.Equal(decimal.NewFromInt(30)),
		"expected overage $30 (bucket window); got %s", item.CommitmentInfo.ComputedOverageAmount)
	s.True(item.CommitmentInfo.ComputedCommitmentUtilizedAmount.Equal(decimal.NewFromInt(8)),
		"expected utilized $8 ($5 bucket + $3 out); got %s", item.CommitmentInfo.ComputedCommitmentUtilizedAmount)
	// Sum invariant: total = utilized + overage + true-up.
	s.True(item.TotalCost.Equal(
		item.CommitmentInfo.ComputedCommitmentUtilizedAmount.
			Add(item.CommitmentInfo.ComputedOverageAmount).
			Add(item.CommitmentInfo.ComputedTrueUpAmount)))
}

// TestWindowCommitment_MixedBucketTypes_OneLineItem covers four bucket flavours
// on a single line item, including a QUANTITY (volume) commitment with true-up
// on a SLAB-priced bucket:
//
//	A [00,06): FLAT $1/u,  AMOUNT  $5/window, overage 2x, true-up ON
//	B [06,12): FLAT $2/u,  AMOUNT  $4/window, overage 2x, true-up OFF
//	C [12,18): SLAB (≤5u @ $2, rest @ $1), QUANTITY 5u/window, overage 1x, true-up ON
//	D [18,24): FLAT $3/u,  QUANTITY 3u/window, overage 3x, true-up OFF
//
// Line item: $1/u, no top-level commitment, scoped to one day (24 windows; the
// buckets cover the whole day). One event per bucket:
//
//	02:00 → 2u: $2 < $5 → true-up $5; A's 5 empty windows true up $5 each → A = $30
//	08:00 → 5u: $10 ≥ $4 → $4+($6×2) = $16; B empties $0 (no true-up)  → B = $16
//	14:00 → 8u: slab $13 ≥ slab(5u)=$10 → $10+($3×1) = $13; C empties true
//	            up to slab(5u)=$10 each → +$50                          → C = $63
//	20:00 → 5u: $15 ≥ 3u×$3=$9 → $9+($6×3) = $27; D empties $0          → D = $27
//
// Total = $136; utilized $25, overage $33, true-up $78 (sum invariant holds).
func (s *MeterUsageServiceSuite) TestWindowCommitment_MixedBucketTypes_OneLineItem() {
	ctx := s.GetContext()

	bucketedMeter := &meter.Meter{
		ID:        "meter_mixed_tb",
		Name:      "Hourly SUM (mixed bucket types)",
		EventName: "api_call",
		Aggregation: meter.Aggregation{
			Type:       types.AggregationSum,
			BucketSize: types.WindowSizeHour,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, bucketedMeter))

	flatPrice := func(id string, amount int64) *price.Price {
		return &price.Price{
			ID: id, Amount: decimal.NewFromInt(amount), Currency: "usd",
			EntityType: types.PRICE_ENTITY_TYPE_SUBSCRIPTION, EntityID: s.sub.ID,
			BillingModel: types.BILLING_MODEL_FLAT_FEE, Type: types.PRICE_TYPE_USAGE,
			MeterID: bucketedMeter.ID, BillingPeriod: types.BILLING_PERIOD_MONTHLY,
			InvoiceCadence: types.InvoiceCadenceArrear, BaseModel: types.GetDefaultBaseModel(ctx),
		}
	}

	linePrice := flatPrice("price_mixed_line", 1)
	linePrice.EntityType = types.PRICE_ENTITY_TYPE_PLAN
	linePrice.EntityID = "plan_1"
	s.NoError(s.GetStores().PriceRepo.Create(ctx, linePrice))

	priceA := flatPrice("price_mixed_a", 1)
	s.NoError(s.GetStores().PriceRepo.Create(ctx, priceA))
	priceB := flatPrice("price_mixed_b", 2)
	s.NoError(s.GetStores().PriceRepo.Create(ctx, priceB))
	priceD := flatPrice("price_mixed_d", 3)
	s.NoError(s.GetStores().PriceRepo.Create(ctx, priceD))

	upTo5 := uint64(5)
	priceC := &price.Price{
		ID: "price_mixed_c_slab", Currency: "usd",
		EntityType: types.PRICE_ENTITY_TYPE_SUBSCRIPTION, EntityID: s.sub.ID,
		BillingModel: types.BILLING_MODEL_TIERED, TierMode: types.BILLING_TIER_SLAB,
		Tiers: []price.PriceTier{
			{UpTo: &upTo5, UnitAmount: decimal.NewFromInt(2)},
			{UnitAmount: decimal.NewFromInt(1)},
		},
		Type: types.PRICE_TYPE_USAGE, MeterID: bucketedMeter.ID,
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, priceC))

	overage2x := decimal.NewFromInt(2)
	overage1x := decimal.NewFromInt(1)
	overage3x := decimal.NewFromInt(3)
	li := &subscription.SubscriptionLineItem{
		ID:                 "li_mixed_tb",
		SubscriptionID:     s.sub.ID,
		CustomerID:         s.customer.ID,
		PriceID:            linePrice.ID,
		PriceType:          types.PRICE_TYPE_USAGE,
		MeterID:            bucketedMeter.ID,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		StartDate:          time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
		EndDate:            time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC),
		Quantity:           decimal.NewFromInt(1),
		CommitmentWindowed: true,
		CommitmentTimeBuckets: types.TimeOfDayBuckets{
			{
				ID: "bkt_a", Start: types.Bucket{Hour: 0}, End: types.Bucket{Hour: 6},
				PriceID: priceA.ID, CommitmentType: types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.NewFromInt(5), OverageFactor: &overage2x, TrueUpEnabled: true,
			},
			{
				ID: "bkt_b", Start: types.Bucket{Hour: 6}, End: types.Bucket{Hour: 12},
				PriceID: priceB.ID, CommitmentType: types.COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.NewFromInt(4), OverageFactor: &overage2x, TrueUpEnabled: false,
			},
			{
				ID: "bkt_c", Start: types.Bucket{Hour: 12}, End: types.Bucket{Hour: 18},
				PriceID: priceC.ID, CommitmentType: types.COMMITMENT_TYPE_QUANTITY,
				CommitmentValue: decimal.NewFromInt(5), OverageFactor: &overage1x, TrueUpEnabled: true,
			},
			{
				ID: "bkt_d", Start: types.Bucket{Hour: 18}, End: types.Bucket{Hour: 24},
				PriceID: priceD.ID, CommitmentType: types.COMMITMENT_TYPE_QUANTITY,
				CommitmentValue: decimal.NewFromInt(3), OverageFactor: &overage3x, TrueUpEnabled: false,
			},
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li))

	for _, ev := range []struct {
		hour int
		qty  float64
	}{
		{2, 2}, {8, 5}, {14, 8}, {20, 5},
	} {
		s.insertMeterUsage(ctx, bucketedMeter.ID, s.customer.ExternalID,
			time.Date(2026, 1, 5, ev.hour, 0, 0, 0, time.UTC), ev.qty)
	}

	resp, err := s.svc.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: s.customer.ExternalID,
		MeterIDs:           []string{bucketedMeter.ID},
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
		WindowSize:         types.WindowSizeHour,
		BreakdownBucket:    true,
	})
	s.NoError(err)

	var item *dto.UsageAnalyticItem
	for i := range resp.Items {
		if resp.Items[i].SubLineItemID == "li_mixed_tb" {
			item = &resp.Items[i]
			break
		}
	}
	s.Require().NotNil(item, "expected analytic for mixed-bucket line item")

	s.True(item.TotalCost.Equal(decimal.NewFromInt(136)),
		"expected $136 (A $30 + B $16 + C $63 + D $27); got %s", item.TotalCost)

	s.Require().NotNil(item.CommitmentInfo)
	s.True(item.CommitmentInfo.ComputedCommitmentUtilizedAmount.Equal(decimal.NewFromInt(25)),
		"expected utilized $25 ($2+$4+$10+$9); got %s", item.CommitmentInfo.ComputedCommitmentUtilizedAmount)
	s.True(item.CommitmentInfo.ComputedOverageAmount.Equal(decimal.NewFromInt(33)),
		"expected overage $33 ($12+$3+$18); got %s", item.CommitmentInfo.ComputedOverageAmount)
	s.True(item.CommitmentInfo.ComputedTrueUpAmount.Equal(decimal.NewFromInt(78)),
		"expected true-up $78 (A $3+$25, C $50); got %s", item.CommitmentInfo.ComputedTrueUpAmount)
	// Sum invariant: total = utilized + overage + true-up.
	s.True(item.TotalCost.Equal(
		item.CommitmentInfo.ComputedCommitmentUtilizedAmount.
			Add(item.CommitmentInfo.ComputedOverageAmount).
			Add(item.CommitmentInfo.ComputedTrueUpAmount)))

	// Bucket summaries: 4 buckets + the out-of-bucket aggregate (empty here —
	// the buckets cover the whole day).
	// One summary per configured bucket (no out-of-bucket row).
	s.Require().Len(item.BucketSummaries, 4)
	summaries := make(map[string]dto.BucketSummary, len(item.BucketSummaries))
	for _, bs := range item.BucketSummaries {
		summaries[bs.BucketID] = bs
		s.Equal("li_mixed_tb", bs.SubscriptionLineItemID)
	}

	// Spot-check C — the volume (QUANTITY) commitment with true-up on SLAB pricing.
	c := summaries["bkt_c"]
	s.Equal(priceC.ID, c.PriceID)
	s.True(c.TotalUsage.Equal(decimal.NewFromInt(8)), "C usage: got %s", c.TotalUsage)
	s.True(c.BaseCharge.Equal(decimal.NewFromInt(13)), "C slab base $13: got %s", c.BaseCharge)
	s.True(c.ComputedUtilized.Equal(decimal.NewFromInt(10)), "C utilized $10 (slab of 5u): got %s", c.ComputedUtilized)
	s.True(c.ComputedOverage.Equal(decimal.NewFromInt(3)), "C overage $3 at 1x: got %s", c.ComputedOverage)
	s.True(c.ComputedTrueUp.Equal(decimal.NewFromInt(50)), "C true-up $50 (5 empty windows × slab(5u)): got %s", c.ComputedTrueUp)

	// Spot-check A (amount + true-up).
	a := summaries["bkt_a"]
	s.True(a.ComputedTrueUp.Equal(decimal.NewFromInt(28)), "A true-up $28 ($3 + 5×$5): got %s", a.ComputedTrueUp)
}

// TestMeterUsage_CancelledSubBeforeWindow_NotAttributed is a regression test
// for the discrepancy where meter-usage analytics duplicated active-sub usage
// onto cancelled-sub line items. meter_usage has no per-event subscription
// linkage — it's keyed by (customer, meter, timestamp) — so iterating over a
// customer's cancelled subs and asking each for its line-item period window
// made every cancelled-sub line item swallow the active sub's events.
//
// The fix clamps the per-subscription query window by sub.CancelledAt in the
// GetDetailedAnalytics loop. When CancelledAt is BEFORE the query start, the
// clamped window has no overlap and the sub is skipped entirely.
//
// Setup mirrors the original prod bug: two subs for the same customer, same
// shared meter, line items with the same StartDate and no EndDate. One sub
// is Active, the other was Cancelled BEFORE the query window. Events exist
// only in the query window. The cancelled sub must not appear in the
// response; the active sub owns all events.
func (s *MeterUsageServiceSuite) TestMeterUsage_CancelledSubBeforeWindow_NotAttributed() {
	ctx := s.GetContext()

	// Cancelled well before the query window: subscription created and
	// cancelled in 2025; query window is January 2026.
	cancelledStart := s.periodStart.Add(-180 * 24 * time.Hour)
	cancelledAt := cancelledStart.Add(time.Hour)
	cancelledSub := &subscription.Subscription{
		ID:                 "sub_cancelled_before",
		CustomerID:         s.customer.ID,
		PlanID:             "plan_1",
		Currency:           "usd",
		SubscriptionStatus: types.SubscriptionStatusCancelled,
		CurrentPeriodStart: cancelledStart,
		CurrentPeriodEnd:   cancelledStart.Add(30 * 24 * time.Hour),
		BillingAnchor:      cancelledStart,
		StartDate:          cancelledStart,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		CancelledAt:        &cancelledAt,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(ctx, cancelledSub))

	cancelledLI := &subscription.SubscriptionLineItem{
		ID:             "li_cancelled_before",
		SubscriptionID: cancelledSub.ID,
		CustomerID:     s.customer.ID,
		PriceID:        s.priceAPI.ID,
		PriceType:      types.PRICE_TYPE_USAGE,
		MeterID:        s.meterAPI.ID,
		Currency:       "usd",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		StartDate:      cancelledStart,
		Quantity:       decimal.NewFromInt(1),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, cancelledLI))

	activeLI := &subscription.SubscriptionLineItem{
		ID:             "li_active_before",
		SubscriptionID: s.sub.ID,
		CustomerID:     s.customer.ID,
		PriceID:        s.priceAPI.ID,
		PriceType:      types.PRICE_TYPE_USAGE,
		MeterID:        s.meterAPI.ID,
		Currency:       "usd",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		StartDate:      s.periodStart,
		Quantity:       decimal.NewFromInt(1),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, activeLI))

	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID,
		s.periodStart.Add(48*time.Hour), 100)
	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID,
		s.periodStart.Add(72*time.Hour), 50)

	resp, err := s.svc.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: s.customer.ExternalID,
		MeterIDs:           []string{s.meterAPI.ID},
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
	})
	s.NoError(err)

	cancelledSeen := false
	activeSeen := false
	for _, item := range resp.Items {
		if item.SubscriptionID == cancelledSub.ID || item.SubLineItemID == cancelledLI.ID {
			cancelledSeen = true
		}
		if item.SubscriptionID == s.sub.ID && item.SubLineItemID == activeLI.ID {
			activeSeen = true
			s.True(item.TotalUsage.Equal(decimal.NewFromInt(150)),
				"active sub should own both events (100 + 50); got usage %s", item.TotalUsage)
		}
	}
	s.False(cancelledSeen, "sub cancelled before the query window must not appear in meter-usage analytics")
	s.True(activeSeen, "active subscription's line item must be present")
}

// TestMeterUsage_CancelledSubInsideWindow_AttributesPreCancellationUsage
// verifies the other half of the CancelledAt clamp: when a subscription was
// cancelled INSIDE the query window, pre-cancellation usage is still
// attributed to it, and post-cancellation events are NOT.
//
// Setup: cancelled sub with CancelledAt mid-window. Two events:
//   - 24 hours after query start (pre-cancellation): must contribute to the
//     cancelled sub's line item.
//   - 96 hours after query start (post-cancellation): must NOT contribute to
//     the cancelled sub's line item.
//
// The active sub (s.sub) sees all events; the cancelled sub sees only event 1.
func (s *MeterUsageServiceSuite) TestMeterUsage_CancelledSubInsideWindow_AttributesPreCancellationUsage() {
	ctx := s.GetContext()

	cancelledAt := s.periodStart.Add(72 * time.Hour)
	cancelledSub := &subscription.Subscription{
		ID:                 "sub_cancelled_mid",
		CustomerID:         s.customer.ID,
		PlanID:             "plan_1",
		Currency:           "usd",
		SubscriptionStatus: types.SubscriptionStatusCancelled,
		CurrentPeriodStart: s.periodStart,
		CurrentPeriodEnd:   s.periodEnd,
		BillingAnchor:      s.periodStart,
		StartDate:          s.periodStart,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		CancelledAt:        &cancelledAt,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(ctx, cancelledSub))

	cancelledLI := &subscription.SubscriptionLineItem{
		ID:             "li_cancelled_mid",
		SubscriptionID: cancelledSub.ID,
		CustomerID:     s.customer.ID,
		PriceID:        s.priceAPI.ID,
		PriceType:      types.PRICE_TYPE_USAGE,
		MeterID:        s.meterAPI.ID,
		Currency:       "usd",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		StartDate:      s.periodStart,
		Quantity:       decimal.NewFromInt(1),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, cancelledLI))

	activeLI := &subscription.SubscriptionLineItem{
		ID:             "li_active_mid",
		SubscriptionID: s.sub.ID,
		CustomerID:     s.customer.ID,
		PriceID:        s.priceAPI.ID,
		PriceType:      types.PRICE_TYPE_USAGE,
		MeterID:        s.meterAPI.ID,
		Currency:       "usd",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceArrear,
		StartDate:      s.periodStart,
		Quantity:       decimal.NewFromInt(1),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, activeLI))

	// Event 1: 24h after start → before cancellation at 72h. Both subs see it.
	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID,
		s.periodStart.Add(24*time.Hour), 100)
	// Event 2: 96h after start → after cancellation. Only active sub sees it.
	s.insertMeterUsage(ctx, s.meterAPI.ID, s.customer.ExternalID,
		s.periodStart.Add(96*time.Hour), 50)

	resp, err := s.svc.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: s.customer.ExternalID,
		MeterIDs:           []string{s.meterAPI.ID},
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
	})
	s.NoError(err)

	var cancelledUsage, activeUsage decimal.Decimal
	cancelledSeen := false
	activeSeen := false
	for _, item := range resp.Items {
		if item.SubscriptionID == cancelledSub.ID && item.SubLineItemID == cancelledLI.ID {
			cancelledSeen = true
			cancelledUsage = item.TotalUsage
		}
		if item.SubscriptionID == s.sub.ID && item.SubLineItemID == activeLI.ID {
			activeSeen = true
			activeUsage = item.TotalUsage
		}
	}

	s.Require().True(cancelledSeen, "cancelled sub with pre-cancellation usage must appear")
	s.Require().True(activeSeen, "active sub must appear")
	s.True(cancelledUsage.Equal(decimal.NewFromInt(100)),
		"cancelled sub should own only the pre-cancellation event (100); got %s", cancelledUsage)
	s.True(activeUsage.Equal(decimal.NewFromInt(150)),
		"active sub should own both events (100 + 50); got %s", activeUsage)
}

// ---------------------------------------------------------------------------
// skipSyntheticZeros — suppress zero-usage line-item injection under filters.
//
// When PropertyFilters or Sources are present, the SQL result is a deliberate
// subset of the customer's usage. The zero-fill loop in GetSubscriptionMeterUsage
// must not fabricate entries for line items whose events filtered out — those
// would misrepresent the filtered slice and (for committed line items) pin
// commitment cost regardless of filter. Baseline (no filter) zero-fill is
// covered by TestZeroUsageLineItem.
// ---------------------------------------------------------------------------

// setupBucketedMeterForSkipZeros creates a bucketed SUM meter (HOUR bucket) +
// price + line item bound to the suite's subscription. Returns the line item ID.
func (s *MeterUsageServiceSuite) setupBucketedMeterForSkipZeros(ctx context.Context, idSuffix string) string {
	bucketedMeter := &meter.Meter{
		ID:        "mtr_bkt_" + idSuffix,
		Name:      "Bucketed SUM " + idSuffix,
		EventName: "api_call",
		Aggregation: meter.Aggregation{
			Type:       types.AggregationSum,
			BucketSize: types.WindowSizeHour,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, bucketedMeter))
	bucketedPrice := s.createPriceForMeter(ctx, "pr_bkt_"+idSuffix, bucketedMeter.ID, decimal.NewFromInt(1))
	li := s.createLineItemForMeter(ctx, "li_bkt_"+idSuffix, bucketedMeter.ID, bucketedPrice.ID)
	return li.ID
}

// TestSkipSyntheticZeros_PropertyFiltersSuppressZeroFill: a bucketed-meter
// line item whose only event fails the property filter must not appear in the
// result. Bucketed meters take the step-11 path (which unconditionally appended
// an entry per line item before the gate) — that's the production scenario
// where this bug actually surfaces.
func (s *MeterUsageServiceSuite) TestSkipSyntheticZeros_PropertyFiltersSuppressZeroFill() {
	ctx := s.GetContext()

	liID := s.setupBucketedMeterForSkipZeros(ctx, "pf")
	s.insertMeterUsageWithProps(ctx, "mtr_bkt_pf", s.customer.ExternalID, "",
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 100,
		map[string]interface{}{"model": "claude-opus"})

	result, err := s.svc.GetSubscriptionMeterUsage(ctx, &GetSubscriptionMeterUsageRequest{
		SubscriptionID:  s.sub.ID,
		StartTime:       s.periodStart,
		EndTime:         s.periodEnd,
		PropertyFilters: map[string][]string{"model": {"gpt-4"}},
	})
	s.NoError(err)
	for _, lu := range result.LineItemUsages {
		if lu.LineItem != nil && lu.LineItem.ID == liID {
			s.Failf("zero-fill leak",
				"bucketed line item with no filter-matching events must not surface under PropertyFilters; got entry %+v", lu)
		}
	}
}

// TestSkipSyntheticZeros_SourcesFilterSuppressesZeroFill: same as the property
// filter case but for Sources. Same step-11 bucketed path.
func (s *MeterUsageServiceSuite) TestSkipSyntheticZeros_SourcesFilterSuppressesZeroFill() {
	ctx := s.GetContext()

	liID := s.setupBucketedMeterForSkipZeros(ctx, "src")
	s.insertMeterUsageWithProps(ctx, "mtr_bkt_src", s.customer.ExternalID, "internal",
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 100, nil)

	result, err := s.svc.GetSubscriptionMeterUsage(ctx, &GetSubscriptionMeterUsageRequest{
		SubscriptionID: s.sub.ID,
		StartTime:      s.periodStart,
		EndTime:        s.periodEnd,
		Sources:        []string{"stripe"},
	})
	s.NoError(err)
	for _, lu := range result.LineItemUsages {
		if lu.LineItem != nil && lu.LineItem.ID == liID {
			s.Failf("zero-fill leak",
				"bucketed line item with no source-matching events must not surface under Sources; got entry %+v", lu)
		}
	}
}

// TestSkipSyntheticZeros_NoFilter_BucketedLineItemStillZeroFilled is the
// counterpart regression guard: with NO filters, the bucketed step-11 path
// must still append an entry for line items with no usage (Usage=0). This
// preserves the existing contract that committed line items can have their
// commitment fire on empty usage.
func (s *MeterUsageServiceSuite) TestSkipSyntheticZeros_NoFilter_BucketedLineItemStillZeroFilled() {
	ctx := s.GetContext()

	liID := s.setupBucketedMeterForSkipZeros(ctx, "nofilter")
	// No usage inserted at all.

	result, err := s.svc.GetSubscriptionMeterUsage(ctx, &GetSubscriptionMeterUsageRequest{
		SubscriptionID: s.sub.ID,
		StartTime:      s.periodStart,
		EndTime:        s.periodEnd,
		// no filters
	})
	s.NoError(err)

	var found *LineItemMeterUsage
	for _, lu := range result.LineItemUsages {
		if lu.LineItem != nil && lu.LineItem.ID == liID {
			found = lu
			break
		}
	}
	s.Require().NotNil(found, "without filters, bucketed line item with no usage must still appear (zero-usage entry)")
	s.True(found.Usage.IsZero(), "zero-fill entry should have usage=0, got %s", found.Usage)
}

// ---------------------------------------------------------------------------
// Bucketed-meter window roll-up
//
// Bucketed meters query meter_usage at the meter's bucket_size (e.g. HOUR) so
// bucketed cost math has the values it needs. The response must surface points
// at the caller's request window_size — bucket points are rolled up by
// mergeBucketPointsByWindow before the response is built. When the caller
// omits window_size, the response points are suppressed entirely (matches
// feature_usage's response shape).
// ---------------------------------------------------------------------------

// TestBucketedMeter_RollsUpPointsToRequestWindow: HOUR-bucketed meter with
// events spanning multiple hours on two days; request window=DAY. The response
// must collapse the hourly internal buckets to one point per day.
func (s *MeterUsageServiceSuite) TestBucketedMeter_RollsUpPointsToRequestWindow() {
	ctx := s.GetContext()

	bucketedMeter := &meter.Meter{
		ID:        "mtr_rollup",
		Name:      "Bucketed SUM (HOUR)",
		EventName: "api_call",
		Aggregation: meter.Aggregation{
			Type:       types.AggregationSum,
			BucketSize: types.WindowSizeHour,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, bucketedMeter))
	bucketedPrice := s.createPriceForMeter(ctx, "pr_rollup", bucketedMeter.ID, decimal.NewFromInt(1))
	s.createLineItemForMeter(ctx, "li_rollup", bucketedMeter.ID, bucketedPrice.ID)

	// Four events: three on Jan 5 at hours 9/10/14, one on Jan 6 at hour 9.
	// Without rollup these surface as 4 hourly points; with rollup → 2 daily points.
	for _, ev := range []struct {
		t   time.Time
		qty float64
	}{
		{time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC), 10},
		{time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 20},
		{time.Date(2026, 1, 5, 14, 0, 0, 0, time.UTC), 30},
		{time.Date(2026, 1, 6, 9, 0, 0, 0, time.UTC), 5},
	} {
		s.insertMeterUsage(ctx, bucketedMeter.ID, s.customer.ExternalID, ev.t, ev.qty)
	}

	resp, err := s.svc.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: s.customer.ExternalID,
		MeterIDs:           []string{bucketedMeter.ID},
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
		WindowSize:         types.WindowSizeDay,
	})
	s.NoError(err)

	var item *dto.UsageAnalyticItem
	for i := range resp.Items {
		if resp.Items[i].SubLineItemID == "li_rollup" {
			item = &resp.Items[i]
			break
		}
	}
	s.Require().NotNil(item)

	s.Require().Len(item.Points, 2,
		"expected 2 daily points (Jan 5 + Jan 6); got %d", len(item.Points))

	byDay := map[string]decimal.Decimal{}
	for _, pt := range item.Points {
		byDay[pt.Timestamp.UTC().Format("2006-01-02")] = pt.Usage
	}
	s.True(byDay["2026-01-05"].Equal(decimal.NewFromInt(60)),
		"Jan 5 rolled-up usage: expected 60 (10+20+30); got %s", byDay["2026-01-05"])
	s.True(byDay["2026-01-06"].Equal(decimal.NewFromInt(5)),
		"Jan 6 rolled-up usage: expected 5; got %s", byDay["2026-01-06"])
}

// TestBucketedMeter_OmitsPointsWhenWindowSizeUnset: when window_size is absent
// from the request, response Points must be empty even though bucketed cost
// calc still runs internally (TotalUsage is still populated). Mirrors
// feature_usage's response shape.
func (s *MeterUsageServiceSuite) TestBucketedMeter_OmitsPointsWhenWindowSizeUnset() {
	ctx := s.GetContext()

	bucketedMeter := &meter.Meter{
		ID:        "mtr_nows",
		Name:      "Bucketed SUM no window",
		EventName: "api_call",
		Aggregation: meter.Aggregation{
			Type:       types.AggregationSum,
			BucketSize: types.WindowSizeHour,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, bucketedMeter))
	bucketedPrice := s.createPriceForMeter(ctx, "pr_nows", bucketedMeter.ID, decimal.NewFromInt(1))
	s.createLineItemForMeter(ctx, "li_nows", bucketedMeter.ID, bucketedPrice.ID)

	s.insertMeterUsage(ctx, bucketedMeter.ID, s.customer.ExternalID,
		time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC), 10)
	s.insertMeterUsage(ctx, bucketedMeter.ID, s.customer.ExternalID,
		time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), 20)

	resp, err := s.svc.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: s.customer.ExternalID,
		MeterIDs:           []string{bucketedMeter.ID},
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
		// no WindowSize
	})
	s.NoError(err)

	var item *dto.UsageAnalyticItem
	for i := range resp.Items {
		if resp.Items[i].SubLineItemID == "li_nows" {
			item = &resp.Items[i]
			break
		}
	}
	s.Require().NotNil(item)

	s.True(item.TotalUsage.Equal(decimal.NewFromInt(30)),
		"total usage must still be computed: expected 30; got %s", item.TotalUsage)
	s.Empty(item.Points,
		"points must be omitted from response when window_size is not specified")
}

// TestBucketedMeter_WindowSizeReflectsPointGranularity: for a bucketed meter the
// response window_size must report the granularity the points were rolled up to —
// the request window when it is coarser than the meter's bucket size — not the
// meter's bucket size.
func (s *MeterUsageServiceSuite) TestBucketedMeter_WindowSizeReflectsPointGranularity() {
	ctx := s.GetContext()

	bucketedMeter := &meter.Meter{
		ID:        "mtr_wsday",
		Name:      "Bucketed SUM window-size",
		EventName: "api_call",
		Aggregation: meter.Aggregation{
			Type:       types.AggregationSum,
			BucketSize: types.WindowSizeHour,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, bucketedMeter))
	bucketedPrice := s.createPriceForMeter(ctx, "pr_wsday", bucketedMeter.ID, decimal.NewFromInt(1))
	s.createLineItemForMeter(ctx, "li_wsday", bucketedMeter.ID, bucketedPrice.ID)

	s.insertMeterUsage(ctx, bucketedMeter.ID, s.customer.ExternalID,
		time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC), 10)

	resp, err := s.svc.GetDetailedAnalytics(ctx, &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		ExternalCustomerID: s.customer.ExternalID,
		MeterIDs:           []string{bucketedMeter.ID},
		StartTime:          s.periodStart,
		EndTime:            s.periodEnd,
		WindowSize:         types.WindowSizeDay,
	})
	s.NoError(err)

	var item *dto.UsageAnalyticItem
	for i := range resp.Items {
		if resp.Items[i].SubLineItemID == "li_wsday" {
			item = &resp.Items[i]
			break
		}
	}
	s.Require().NotNil(item)
	s.Equal(types.WindowSizeDay, item.WindowSize,
		"window_size must reflect the request window the points were rolled up to, not the meter bucket size")
}
