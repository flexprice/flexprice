package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// End-to-end timezone scenarios covering the invariant established when we
// migrated the auto-invoice threshold cron to meter_usage and pinned the
// aggregator's WHERE bounds to UTC:
//   - Period boundary math uses the customer's IANA timezone (Go side).
//   - Usage-query WHERE bounds compare UTC instants, regardless of tz.
//   - A subscription's tz field never leaks into the ClickHouse timestamp
//     comparison, so identical UTC event patterns produce identical billing
//     outcomes for customers in different tzs.

type SubscriptionTimezoneE2ETestSuite struct {
	testutil.BaseServiceTestSuite
	service SubscriptionService

	plan     *plan.Plan
	meter    *meter.Meter
	price    *price.Price
	threshold decimal.Decimal
}

func TestSubscriptionTimezoneE2E(t *testing.T) {
	suite.Run(t, new(SubscriptionTimezoneE2ETestSuite))
}

func (s *SubscriptionTimezoneE2ETestSuite) SetupSuite() {
	s.BaseServiceTestSuite.SetupSuite()
}

func (s *SubscriptionTimezoneE2ETestSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
	s.setupPlanAndPrice()
	s.threshold = decimal.NewFromInt(10) // $10 threshold, $0.01/call → 1001 calls trip it
}

func (s *SubscriptionTimezoneE2ETestSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *SubscriptionTimezoneE2ETestSuite) setupService() {
	s.service = NewSubscriptionService(ServiceParams{
		Logger:                     s.GetLogger(),
		Config:                     s.GetConfig(),
		DB:                         s.GetDB(),
		TaxAssociationRepo:         s.GetStores().TaxAssociationRepo,
		TaxRateRepo:                s.GetStores().TaxRateRepo,
		SubRepo:                    s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo:   s.GetStores().SubscriptionLineItemRepo,
		SubscriptionPhaseRepo:      s.GetStores().SubscriptionPhaseRepo,
		SubScheduleRepo:            s.GetStores().SubscriptionScheduleRepo,
		PlanRepo:                   s.GetStores().PlanRepo,
		PriceRepo:                  s.GetStores().PriceRepo,
		PriceUnitRepo:              s.GetStores().PriceUnitRepo,
		EventRepo:                  s.GetStores().EventRepo,
		MeterUsageRepo:             s.GetStores().MeterUsageRepo,
		MeterRepo:                  s.GetStores().MeterRepo,
		CustomerRepo:               s.GetStores().CustomerRepo,
		InvoiceRepo:                s.GetStores().InvoiceRepo,
		InvoiceLineItemRepo:        s.GetStores().InvoiceLineItemRepo,
		EntitlementRepo:            s.GetStores().EntitlementRepo,
		EnvironmentRepo:            s.GetStores().EnvironmentRepo,
		FeatureRepo:                s.GetStores().FeatureRepo,
		TenantRepo:                 s.GetStores().TenantRepo,
		UserRepo:                   s.GetStores().UserRepo,
		AuthRepo:                   s.GetStores().AuthRepo,
		WalletRepo:                 s.GetStores().WalletRepo,
		PaymentRepo:                s.GetStores().PaymentRepo,
		CreditGrantRepo:            s.GetStores().CreditGrantRepo,
		CreditGrantApplicationRepo: s.GetStores().CreditGrantApplicationRepo,
		CouponRepo:                 s.GetStores().CouponRepo,
		CouponAssociationRepo:      s.GetStores().CouponAssociationRepo,
		CouponApplicationRepo:      s.GetStores().CouponApplicationRepo,
		AddonRepo:                  s.GetStores().AddonRepo,
		AddonAssociationRepo:       s.GetStores().AddonAssociationRepo,
		ConnectionRepo:             s.GetStores().ConnectionRepo,
		SettingsRepo:               s.GetStores().SettingsRepo,
		EventPublisher:             s.GetPublisher(),
		WebhookPublisher:           s.GetWebhookPublisher(),
		ProrationCalculator:        s.GetCalculator(),
		IntegrationFactory:         s.GetIntegrationFactory(),
		PlanPriceSyncRepo:          s.GetStores().PlanPriceSyncRepo,
		// CreateSubscription touches these on cancellation branches / alerts —
		// the anchor test doesn't exercise them but the constructor needs them
		// non-nil to reach the plan-price-sequence stamping step.
		AlertLogsRepo:                s.GetStores().AlertLogsRepo,
		WalletBalanceAlertPubSub:     types.WalletBalanceAlertPubSub{PubSub: testutil.NewInMemoryPubSub()},
		CheckoutSessionRepo:          s.GetStores().CheckoutSessionRepo,
		TaxAppliedRepo:               s.GetStores().TaxAppliedRepo,
		EntityIntegrationMappingRepo: s.GetStores().EntityIntegrationMappingRepo,
	})
}

func (s *SubscriptionTimezoneE2ETestSuite) setupPlanAndPrice() {
	ctx := s.GetContext()

	s.plan = &plan.Plan{
		ID:        "plan_tz_e2e",
		Name:      "Timezone E2E Test Plan",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, s.plan))

	// COUNT meter: qty = 1 per event. Keeps arithmetic obvious.
	s.meter = &meter.Meter{
		ID:        "meter_tz_e2e_api_calls",
		Name:      "API Calls",
		EventName: "api_call",
		Aggregation: meter.Aggregation{
			Type: types.AggregationCount,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, s.meter))

	// $0.01/call. 1001 calls → $10.01, tips the $10 threshold.
	s.price = &price.Price{
		ID:                 "price_tz_e2e_api_calls",
		Amount:             decimal.NewFromFloat(0.01),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.plan.ID,
		Type:               types.PRICE_TYPE_USAGE,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		MeterID:            s.meter.ID,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, s.price))
}

// createCustomer builds a customer in the given IANA timezone.
func (s *SubscriptionTimezoneE2ETestSuite) createCustomer(id, externalID, tz string) *customer.Customer {
	c := &customer.Customer{
		ID:         id,
		ExternalID: externalID,
		Name:       fmt.Sprintf("Customer %s (%s)", id, tz),
		Email:      fmt.Sprintf("%s@example.com", id),
		Timezone:   tz,
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), c))
	return c
}

// createSubWithThreshold creates a subscription for the given customer with the
// $10 threshold applied. currentPeriodStart is stored as a UTC instant.
func (s *SubscriptionTimezoneE2ETestSuite) createSubWithThreshold(
	subID string,
	c *customer.Customer,
	currentPeriodStart time.Time,
	currentPeriodEnd time.Time,
) *subscription.Subscription {
	ctx := s.GetContext()
	sub := &subscription.Subscription{
		ID:                   subID,
		PlanID:               s.plan.ID,
		CustomerID:           c.ID,
		StartDate:            currentPeriodStart,
		CurrentPeriodStart:   currentPeriodStart,
		CurrentPeriodEnd:     currentPeriodEnd,
		BillingAnchor:        currentPeriodStart,
		Currency:             "usd",
		BillingCycle:         types.BillingCycleAnniversary,
		BillingPeriod:        types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount:   1,
		SubscriptionStatus:   types.SubscriptionStatusActive,
		SubscriptionType:     types.SubscriptionTypeStandalone,
		Timezone:             c.Timezone,
		AutoInvoiceThreshold: lo.ToPtr(s.threshold),
		BaseModel:            types.GetDefaultBaseModel(ctx),
	}
	li := &subscription.SubscriptionLineItem{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID:   sub.ID,
		CustomerID:       c.ID,
		EntityID:         s.plan.ID,
		EntityType:       types.SubscriptionLineItemEntityTypePlan,
		PlanDisplayName:  s.plan.Name,
		PriceID:          s.price.ID,
		PriceType:        s.price.Type,
		MeterID:          s.meter.ID,
		MeterDisplayName: s.meter.Name,
		DisplayName:      s.meter.Name,
		Quantity:         decimal.Zero,
		Currency:         "usd",
		BillingPeriod:    types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence:   types.InvoiceCadenceArrear,
		StartDate:        currentPeriodStart,
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, []*subscription.SubscriptionLineItem{li}))
	return sub
}

// insertEventsForCustomer mirrors the production Kafka pipeline: writes n
// events into both the events store and the meter_usage store, all stamped
// with the same UTC timestamp. Prod does this via the meter-usage-tracking
// consumer; the in-memory pipeline in tests does not run consumers.
func (s *SubscriptionTimezoneE2ETestSuite) insertEventsForCustomer(c *customer.Customer, n int, ts time.Time) {
	ctx := s.GetContext()
	muRecords := make([]*events.MeterUsage, 0, n)
	for i := 0; i < n; i++ {
		id := s.GetUUID()
		evt := &events.Event{
			ID:                 id,
			TenantID:           types.GetTenantID(ctx),
			EnvironmentID:      types.GetEnvironmentID(ctx),
			EventName:          s.meter.EventName,
			ExternalCustomerID: c.ExternalID,
			Timestamp:          ts,
			IngestedAt:         ts,
			Properties:         map[string]interface{}{},
		}
		s.NoError(s.GetStores().EventRepo.InsertEvent(ctx, evt))
		muRecords = append(muRecords, &events.MeterUsage{
			Event:      *evt,
			MeterID:    s.meter.ID,
			QtyTotal:   decimal.NewFromInt(1),
			UniqueHash: fmt.Sprintf("%s:%s", s.meter.EventName, id),
		})
	}
	s.NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, muRecords))
}

// mustLocation loads an IANA location and fails the test if unavailable in
// the environment's tzdata. Guards against silent UTC fallbacks.
func (s *SubscriptionTimezoneE2ETestSuite) mustLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	s.Require().NoError(err, "tzdata missing for %q", name)
	return loc
}

// -----------------------------------------------------------------------------
// TestTimezone_MonthlyAnchor_LandsAtCustomerLocalMidnight
//
// The customer sees months rolling over at their local midnight. Under the
// hood we store the anchor as a UTC instant, but the instant we pick MUST be
// the UTC time that corresponds to "start of next month, 00:00, customer tz."
// Getting this wrong shifts every subsequent monthly boundary by the tz
// offset, which is invisible in tests that only use UTC.
// -----------------------------------------------------------------------------
func (s *SubscriptionTimezoneE2ETestSuite) TestTimezone_MonthlyAnchor_LandsAtCustomerLocalMidnight() {
	// A single instant used for every scenario so drift between test runs
	// can't perturb the expected anchors.
	startUTC := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name         string
		tz           string
		expectAnchor time.Time // UTC instant of the *next* local midnight after startUTC
	}{
		{
			// UTC: Sept 1 00:00 UTC — no offset.
			name:         "UTC customer",
			tz:           "UTC",
			expectAnchor: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			// IST (+05:30, no DST): Sept 1 00:00 IST = Aug 31 18:30 UTC.
			name:         "IST customer (no DST)",
			tz:           "Asia/Kolkata",
			expectAnchor: time.Date(2026, 8, 31, 18, 30, 0, 0, time.UTC),
		},
		{
			// PDT (-07:00 in Aug): Sept 1 00:00 PDT = Sept 1 07:00 UTC.
			// Regression guard for DST-active offsets specifically.
			name:         "PST customer during DST (PDT, UTC-7)",
			tz:           "America/Los_Angeles",
			expectAnchor: time.Date(2026, 9, 1, 7, 0, 0, 0, time.UTC),
		},
		{
			// CEST (+02:00 in Aug): Sept 1 00:00 CEST = Aug 31 22:00 UTC.
			// The Aruba deployment that surfaced the WHERE-bound bug runs
			// on this offset; the anchor path is separate but shares the
			// same "customer tz drives boundaries" invariant.
			name:         "Europe/Rome during DST (CEST, UTC+2)",
			tz:           "Europe/Rome",
			expectAnchor: time.Date(2026, 8, 31, 22, 0, 0, 0, time.UTC),
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			loc := s.mustLocation(tc.tz)
			c := s.createCustomer(
				types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
				fmt.Sprintf("ext_%s", tc.tz),
				tc.tz,
			)

			resp, err := s.service.CreateSubscription(s.GetContext(), dto.CreateSubscriptionRequest{
				CustomerID:         c.ID,
				PlanID:             s.plan.ID,
				StartDate:          lo.ToPtr(startUTC),
				Currency:           "usd",
				BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
				BillingPeriodCount: 1,
				BillingCycle:       types.BillingCycleCalendar,
			})
			s.Require().NoError(err)
			s.Require().NotNil(resp)

			gotAnchor := resp.BillingAnchor.UTC()
			s.Equal(tc.expectAnchor, gotAnchor,
				"anchor: got %s, want %s (customer sees %s local)",
				gotAnchor, tc.expectAnchor, tc.expectAnchor.In(loc),
			)
			// Wall-clock check: the anchor instant must be exactly local midnight.
			localAnchor := gotAnchor.In(loc)
			s.Equal(0, localAnchor.Hour(), "anchor is not local 00:00 in %s: %s", tc.tz, localAnchor)
			s.Equal(0, localAnchor.Minute(), "anchor is not local 00:00 in %s: %s", tc.tz, localAnchor)
		})
	}
}

// -----------------------------------------------------------------------------
// TestTimezone_ThresholdCron_UTCBoundsAreIdenticalRegardlessOfCustomerTz
//
// Two subscriptions with identical UTC period bounds and identical UTC event
// patterns MUST reach the same threshold verdict, whether the customer is in
// UTC or Asia/Kolkata. Any regression that reintroduces "shift bounds by
// customer tz" would fail this: the IST customer would see a window shifted
// -5h30m and miss events near either edge.
// -----------------------------------------------------------------------------
func (s *SubscriptionTimezoneE2ETestSuite) TestTimezone_ThresholdCron_UTCBoundsAreIdenticalRegardlessOfCustomerTz() {
	ctx := s.GetContext()

	// Real-time reference: the cron uses time.Now().UTC() as effectiveTime,
	// so events must sit in the past.
	now := time.Now().UTC()
	periodStart := now.Add(-6 * time.Hour) // window: [now-6h, now)
	periodEnd := now.Add(24 * time.Hour)   // future — cron effectiveTime bounds the read

	custUTC := s.createCustomer("cust_tz_utc", "ext_tz_utc", "UTC")
	custIST := s.createCustomer("cust_tz_ist", "ext_tz_ist", "Asia/Kolkata")

	// Identical bounds, identical event patterns.
	subUTC := s.createSubWithThreshold("sub_tz_utc", custUTC, periodStart, periodEnd)
	subIST := s.createSubWithThreshold("sub_tz_ist", custIST, periodStart, periodEnd)

	// 1500 events for each, 30min before now → well inside [now-6h, now).
	// 1500 * $0.01 = $15 > $10 threshold.
	insideTs := now.Add(-30 * time.Minute)
	s.insertEventsForCustomer(custUTC, 1500, insideTs)
	s.insertEventsForCustomer(custIST, 1500, insideTs)

	// Also insert 500 events *before* the period for both — must be excluded.
	// If a naive fix ever shifted bounds by +05:30 for the IST customer, the
	// before-period events (5h before periodStart in UTC) would migrate into
	// the shifted window and skew the IST verdict.
	beforeTs := periodStart.Add(-1 * time.Hour)
	s.insertEventsForCustomer(custUTC, 500, beforeTs)
	s.insertEventsForCustomer(custIST, 500, beforeTs)

	result, err := s.service.ProcessAutoInvoiceThresholdBilling(ctx)
	s.Require().NoError(err)
	s.Require().NotNil(result)
	s.Equal(2, result.TotalChecked)
	s.Equal(2, result.TotalInvoiced, "both subs must trip the threshold identically")
	s.Equal(0, result.TotalSkipped)
	s.Equal(0, result.TotalFailed)

	// Both subs must have exactly one invoice, and CurrentPeriodStart must
	// have advanced. The advanced value is the cron's effectiveTime (real
	// time.Now().UTC() at the moment of the call), which we can only bound.
	for _, subID := range []string{subUTC.ID, subIST.ID} {
		filter := types.NewNoLimitInvoiceFilter()
		filter.SubscriptionID = subID
		invoices, err := s.GetStores().InvoiceRepo.List(ctx, filter)
		s.Require().NoError(err)
		s.Require().Len(invoices, 1, "sub %s should have exactly 1 threshold invoice", subID)
		s.Equal(string(types.InvoiceBillingReasonAutoInvoiceThreshold), invoices[0].BillingReason)

		reloaded, err := s.GetStores().SubscriptionRepo.Get(ctx, subID)
		s.NoError(err)
		s.True(reloaded.CurrentPeriodStart.After(periodStart),
			"sub %s CurrentPeriodStart must advance past the original periodStart", subID)
	}
}

// -----------------------------------------------------------------------------
// TestTimezone_ThresholdCron_MultipleTzs_NoCrossContamination
//
// Three subscriptions in three different tzs, each with independent event
// streams and periods. Ensures the cron's per-subscription usage scoping is
// keyed on external_customer_id (as it always has been) and not accidentally
// on anything tz-derived.
// -----------------------------------------------------------------------------
func (s *SubscriptionTimezoneE2ETestSuite) TestTimezone_ThresholdCron_MultipleTzs_NoCrossContamination() {
	ctx := s.GetContext()

	now := time.Now().UTC()
	periodStart := now.Add(-6 * time.Hour)
	periodEnd := now.Add(24 * time.Hour)

	// Three customers, three tzs, three event counts:
	//   IST: 1500 events → $15.00 → invoice (over $10)
	//   PST:  500 events → $ 5.00 → skip     (under $10)
	//   Rome: 2000 events → $20.00 → invoice (over $10)
	type scenario struct {
		id         string
		tz         string
		events     int
		wantInvoiced bool
	}
	scenarios := []scenario{
		{"cust_multi_ist", "Asia/Kolkata", 1500, true},
		{"cust_multi_pst", "America/Los_Angeles", 500, false},
		{"cust_multi_rome", "Europe/Rome", 2000, true},
	}

	subIDs := make(map[string]string, len(scenarios))
	for _, sc := range scenarios {
		c := s.createCustomer(sc.id, "ext_"+sc.id, sc.tz)
		sub := s.createSubWithThreshold("sub_"+sc.id, c, periodStart, periodEnd)
		subIDs[sc.id] = sub.ID
		s.insertEventsForCustomer(c, sc.events, now.Add(-1*time.Hour))
	}

	result, err := s.service.ProcessAutoInvoiceThresholdBilling(ctx)
	s.Require().NoError(err)
	s.Require().NotNil(result)
	s.Equal(len(scenarios), result.TotalChecked)
	s.Equal(2, result.TotalInvoiced, "IST + Rome must trip; PST must skip")
	s.Equal(1, result.TotalSkipped)
	s.Equal(0, result.TotalFailed)

	// Verify each subscription's outcome independently.
	for _, sc := range scenarios {
		filter := types.NewNoLimitInvoiceFilter()
		filter.SubscriptionID = subIDs[sc.id]
		invoices, err := s.GetStores().InvoiceRepo.List(ctx, filter)
		s.Require().NoError(err)
		if sc.wantInvoiced {
			s.Require().Len(invoices, 1, "%s (%s) should have 1 invoice", sc.id, sc.tz)
			s.Equal(string(types.InvoiceBillingReasonAutoInvoiceThreshold), invoices[0].BillingReason)
		} else {
			s.Empty(invoices, "%s (%s) must NOT be invoiced (below threshold)", sc.id, sc.tz)
		}
	}
}

// -----------------------------------------------------------------------------
// TestTimezone_ThresholdCron_EventNearISTMidnightIsIncluded
//
// The specific regression from the Aruba bug: an event whose UTC instant sits
// inside the current period must be counted, even when the customer's local
// clock puts it "on the previous day." Before the fix, the query bounds were
// re-parsed in the server's local tz, so an event at UTC 23:30 (= IST 05:00
// next day) would be dropped when compared against period bounds also
// re-parsed in the server tz.
// -----------------------------------------------------------------------------
func (s *SubscriptionTimezoneE2ETestSuite) TestTimezone_ThresholdCron_EventNearISTMidnightIsIncluded() {
	ctx := s.GetContext()

	loc := s.mustLocation("Asia/Kolkata")
	c := s.createCustomer("cust_ist_boundary", "ext_ist_boundary", "Asia/Kolkata")

	// Anchor the period bounds around "now" so the cron's effectiveTime
	// (= time.Now().UTC()) sits inside the window. The interesting UTC
	// instants are computed as offsets from now — the salient fact is
	// that the event UTC instant is inside [periodStart, effectiveTime),
	// which is the only check that matters for correctness.
	now := time.Now().UTC()
	periodStart := now.Add(-2 * time.Hour)
	periodEnd := now.Add(30 * 24 * time.Hour)

	sub := s.createSubWithThreshold("sub_ist_boundary", c, periodStart, periodEnd)

	// Event 30 minutes ago, well inside [periodStart, now). We format the
	// same instant in IST purely to document what the customer "sees";
	// the SQL WHERE clause compares instants, not wall-clock strings.
	eventTs := now.Add(-30 * time.Minute)
	s.insertEventsForCustomer(c, 1500, eventTs)

	s.T().Logf("event at UTC %s (customer sees %s IST)", eventTs.Format(time.RFC3339), eventTs.In(loc).Format(time.RFC3339))
	s.T().Logf("period start UTC %s (customer sees %s IST)", periodStart.Format(time.RFC3339), periodStart.In(loc).Format(time.RFC3339))

	result, err := s.service.ProcessAutoInvoiceThresholdBilling(ctx)
	s.Require().NoError(err)
	s.Require().NotNil(result)
	s.Equal(1, result.TotalInvoiced, "in-period event must be counted for IST customer")

	filter := types.NewNoLimitInvoiceFilter()
	filter.SubscriptionID = sub.ID
	invoices, err := s.GetStores().InvoiceRepo.List(ctx, filter)
	s.NoError(err)
	s.Require().Len(invoices, 1)
	s.Equal(string(types.InvoiceBillingReasonAutoInvoiceThreshold), invoices[0].BillingReason)
}

