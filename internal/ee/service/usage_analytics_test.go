package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/coupon"
	"github.com/flexprice/flexprice/internal/domain/coupon_association"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// UsageAnalyticsSuite runs the usage breakdown over in-memory stores, with the in-memory reference
// standing in for ClickHouse, and checks it against billing on the same data.
type UsageAnalyticsSuite struct {
	testutil.BaseServiceTestSuite
	params  ServiceParams
	svc     UsageAnalyticsService
	billing BillingService
	parent  *subscription.Subscription
	child   *subscription.Subscription
}

func TestUsageAnalytics(t *testing.T) {
	suite.Run(t, new(UsageAnalyticsSuite))
}

var (
	queryOct1  = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	queryOct15 = time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	queryNov1  = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
)

func (s *UsageAnalyticsSuite) GetContext() context.Context {
	return types.SetEnvironmentID(s.BaseServiceTestSuite.GetContext(), "env_cost")
}

func (s *UsageAnalyticsSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.BaseServiceTestSuite.ClearStores()
	stores := s.GetStores()
	s.params = ServiceParams{
		Logger:                   s.GetLogger(),
		Config:                   s.GetConfig(),
		DB:                       s.GetDB(),
		SubRepo:                  stores.SubscriptionRepo,
		SubscriptionLineItemRepo: stores.SubscriptionLineItemRepo,
		SubscriptionPhaseRepo:    stores.SubscriptionPhaseRepo,
		SubScheduleRepo:          stores.SubscriptionScheduleRepo,
		PlanRepo:                 stores.PlanRepo,
		PriceRepo:                stores.PriceRepo,
		PriceUnitRepo:            stores.PriceUnitRepo,
		EventRepo:                stores.EventRepo,
		MeterRepo:                stores.MeterRepo,
		MeterUsageRepo:           stores.MeterUsageRepo,
		CustomerRepo:             stores.CustomerRepo,
		InvoiceRepo:              stores.InvoiceRepo,
		InvoiceLineItemRepo:      stores.InvoiceLineItemRepo,
		EntitlementRepo:          stores.EntitlementRepo,
		EntitlementGrantRepo:     stores.EntitlementGrantRepo,
		EnvironmentRepo:          stores.EnvironmentRepo,
		FeatureRepo:              stores.FeatureRepo,
		AddonAssociationRepo:     stores.AddonAssociationRepo,
		TenantRepo:               stores.TenantRepo,
		WalletRepo:               stores.WalletRepo,
		CouponRepo:               stores.CouponRepo,
		CouponAssociationRepo:    stores.CouponAssociationRepo,
		CouponApplicationRepo:    stores.CouponApplicationRepo,
		TaxRateRepo:              stores.TaxRateRepo,
		TaxAppliedRepo:           stores.TaxAppliedRepo,
		TaxAssociationRepo:       stores.TaxAssociationRepo,
		CreditGrantRepo:          stores.CreditGrantRepo,
		SettingsRepo:             stores.SettingsRepo,
		CommitmentRepo:           stores.CommitmentRepo,
		BillingTermRepo:          stores.BillingTermRepo,
		BillingInstalmentRepo:    stores.BillingInstalmentRepo,
		AlertLogsRepo:            stores.AlertLogsRepo,
		EventPublisher:           s.GetPublisher(),
		WebhookPublisher:         s.GetWebhookPublisher(),
		ProrationCalculator:      s.GetCalculator(),
	}
	usage := stores.MeterUsageRepo.(*testutil.InMemoryMeterUsageStore)
	s.svc = NewUsageAnalyticsService(s.params, testutil.NewInMemoryUsageAnalyticsStore(usage))
	s.billing = NewBillingService(s.params)
	s.seed()
}

// seed builds a parent (acme) with one inherited child (acme_child) on a monthly plan:
// tokens (SUM, price change on Oct 15), calls (COUNT), scaled (SUM_WITH_MULTIPLIER), tiered (excluded).
func (s *UsageAnalyticsSuite) seed() {
	ctx := s.GetContext()
	st := s.GetStores()
	base := func() types.BaseModel { return types.GetDefaultBaseModel(ctx) }

	for _, c := range []*customer.Customer{
		{ID: "cust_parent", ExternalID: "acme", Name: "Acme", BaseModel: base()},
		{ID: "cust_child", ExternalID: "acme_child", Name: "Acme child", BaseModel: base()},
	} {
		s.NoError(st.CustomerRepo.Create(ctx, c))
	}
	s.NoError(st.PlanRepo.Create(ctx, &plan.Plan{ID: "plan_usage", Name: "Amount plan", BaseModel: base()}))

	meters := map[string]types.AggregationType{
		"m_tokens": types.AggregationSum, "m_calls": types.AggregationCount,
		"m_scaled": types.AggregationSumWithMultiplier, "m_tiered": types.AggregationSum,
	}
	for id, agg := range meters {
		m := &meter.Meter{ID: id, Name: id, EventName: id, Aggregation: meter.Aggregation{Type: agg, Field: "qty"}, BaseModel: base()}
		if agg == types.AggregationSumWithMultiplier {
			m.Aggregation.Multiplier = lo.ToPtr(decimal.NewFromInt(10))
		}
		s.NoError(st.MeterRepo.CreateMeter(ctx, m))
		s.NoError(st.FeatureRepo.Create(ctx, &feature.Feature{
			ID: "f_" + id[2:], Name: id, Type: types.FeatureTypeMetered, MeterID: id, BaseModel: base(),
		}))
	}

	flat := func(id, meterID, amount string) *price.Price {
		return &price.Price{
			ID: id, Amount: decimal.RequireFromString(amount), Currency: "usd",
			EntityType: types.PRICE_ENTITY_TYPE_PLAN, EntityID: "plan_usage", Type: types.PRICE_TYPE_USAGE,
			BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1, BillingModel: types.BILLING_MODEL_FLAT_FEE,
			BillingCadence: types.BILLING_CADENCE_RECURRING, InvoiceCadence: types.InvoiceCadenceArrear,
			MeterID: meterID, BaseModel: base(),
		}
	}
	tiered := flat("p_tiered", "m_tiered", "0")
	tiered.BillingModel, tiered.TierMode = types.BILLING_MODEL_TIERED, types.BILLING_TIER_SLAB
	tiered.Tiers = []price.PriceTier{{UpTo: lo.ToPtr(uint64(100)), UnitAmount: decimal.NewFromInt(1)}, {UnitAmount: decimal.RequireFromString("0.5")}}
	prices := []*price.Price{
		flat("p_tok_old", "m_tokens", "0.002"), flat("p_tok_new", "m_tokens", "0.0025"),
		flat("p_calls", "m_calls", "0.01"), flat("p_scaled", "m_scaled", "0.5"), tiered,
	}
	for _, p := range prices {
		s.NoError(st.PriceRepo.Create(ctx, p))
	}

	s.parent = &subscription.Subscription{
		ID: "sub_parent", PlanID: "plan_usage", CustomerID: "cust_parent", StartDate: queryOct1,
		BillingAnchor: queryOct1, CurrentPeriodStart: queryOct1, CurrentPeriodEnd: queryNov1, Currency: "usd",
		BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive, SubscriptionType: types.SubscriptionTypeParent, BaseModel: base(),
	}
	line := func(id, priceID, meterID string, start time.Time, end time.Time) *subscription.SubscriptionLineItem {
		return &subscription.SubscriptionLineItem{
			ID: id, SubscriptionID: s.parent.ID, CustomerID: s.parent.CustomerID, EntityID: "plan_usage",
			EntityType: types.SubscriptionLineItemEntityTypePlan, PriceID: priceID, PriceType: types.PRICE_TYPE_USAGE,
			MeterID: meterID, DisplayName: id, Currency: "usd", BillingPeriod: types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1, InvoiceCadence: types.InvoiceCadenceArrear, StartDate: start, EndDate: end, BaseModel: base(),
		}
	}
	s.NoError(st.SubscriptionRepo.CreateWithLineItems(ctx, s.parent, []*subscription.SubscriptionLineItem{
		line("li_tok_old", "p_tok_old", "m_tokens", queryOct1, queryOct15),
		line("li_tok_new", "p_tok_new", "m_tokens", queryOct15, time.Time{}),
		line("li_calls", "p_calls", "m_calls", queryOct1, time.Time{}),
		line("li_scaled", "p_scaled", "m_scaled", queryOct1, time.Time{}),
		line("li_tiered", "p_tiered", "m_tiered", queryOct1, time.Time{}),
	}))

	s.child = &subscription.Subscription{
		ID: "sub_child", PlanID: "plan_usage", CustomerID: "cust_child", StartDate: queryOct1,
		BillingAnchor: queryOct1, CurrentPeriodStart: queryOct1, CurrentPeriodEnd: queryNov1, Currency: "usd",
		BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive, SubscriptionType: types.SubscriptionTypeInherited,
		ParentSubscriptionID: lo.ToPtr(s.parent.ID), BaseModel: base(),
	}
	s.NoError(st.SubscriptionRepo.CreateWithLineItems(ctx, s.child, nil))

	// The PRD example: alice $2,000 + $5,000 across the price change, bob $6,000 from the child.
	s.usage("m_tokens", "acme", time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC), "1000000", map[string]any{"user": "alice"})
	s.usage("m_tokens", "acme_child", time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC), "3000000", map[string]any{"user": "bob"})
	s.usage("m_tokens", "acme", time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC), "2000000", map[string]any{"user": "alice"})
	// Calls: a numeric user id and one event without a user.
	for i := 0; i < 3; i++ {
		s.usage("m_calls", "acme", queryOct1.Add(time.Duration(i)*time.Hour), "1", map[string]any{"user": 123})
	}
	s.usage("m_calls", "acme_child", queryOct1.Add(5*time.Hour), "1", map[string]any{})
	// SUM_WITH_MULTIPLIER rows carry the scaled quantity (field × 10).
	s.usage("m_scaled", "acme", queryOct1.Add(2*24*time.Hour), "40", map[string]any{"user": "alice"})
	s.usage("m_tiered", "acme", queryOct1.Add(3*24*time.Hour), "500", map[string]any{"user": "alice"})
	// Outside the period, never counted.
	s.usage("m_tokens", "acme", queryNov1.Add(time.Hour), "999", map[string]any{"user": "alice"})
}

func (s *UsageAnalyticsSuite) usage(meterID, customerID string, ts time.Time, qty string, props map[string]any) {
	ctx := s.GetContext()
	id := types.GenerateUUIDWithPrefix("mu")
	s.NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{{
		Event: events.Event{
			ID: id, TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx), EventName: meterID,
			ExternalCustomerID: customerID, Timestamp: ts, IngestedAt: ts, Properties: props, Source: "api",
		},
		MeterID: meterID, QtyTotal: decimal.RequireFromString(qty), UniqueHash: fmt.Sprintf("%s:%s", meterID, id),
	}}))
}

// supportedMeters are the fixture's flat-priced meters; m_tiered makes an unfiltered request fail.
var supportedMeters = []string{"m_tokens", "m_calls", "m_scaled"}

func (s *UsageAnalyticsSuite) query(mutate func(r *dto.UsageAnalyticsRequest)) (*dto.UsageAnalyticsResponse, error) {
	req := &dto.UsageAnalyticsRequest{
		ExternalCustomerID: "acme",
		StartTime:          lo.ToPtr(queryOct1),
		EndTime:            lo.ToPtr(queryNov1),
		MeterIDs:           supportedMeters,
		GroupBy:            []string{"properties.user"},
		Deduplicate:        true,
		Limit:              100,
	}
	if mutate != nil {
		mutate(req)
	}
	return s.svc.GetBreakdown(s.GetContext(), req)
}

func amountByGroup(resp *dto.UsageAnalyticsResponse, key string) map[string]string {
	return lo.SliceToMap(resp.Items, func(i dto.UsageAnalyticsItem) (string, string) { return i.Group[key], i.Amount })
}

func (s *UsageAnalyticsSuite) TestPriceChangeAttributesEachEventAtItsOwnPrice() {
	resp, err := s.query(func(r *dto.UsageAnalyticsRequest) { r.MeterIDs = []string{"m_tokens"} })
	s.NoError(err)
	s.Equal(map[string]string{"alice": "7000.00", "bob": "6000.00"}, amountByGroup(resp, "properties.user"))
	s.Equal("13000.00", resp.Totals.Amount)
	s.Equal("usd", resp.Currency)
	s.Equal("alice", resp.Items[0].Group["properties.user"], "default sort is total amount, descending")
}

// assertLinesEqualInvoice checks that every line in the breakdown amounts exactly what the October
// invoice charges for it, and returns how many lines there were.
func (s *UsageAnalyticsSuite) assertLinesEqualInvoice(meterIDs ...string) int {
	sub, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), s.parent.ID)
	s.NoError(err)
	inv, err := s.billing.PrepareSubscriptionInvoiceRequest(s.GetContext(), &dto.PrepareSubscriptionInvoiceRequestParams{
		Subscription: sub, PeriodStart: queryOct1, PeriodEnd: queryNov1, ReferencePoint: types.ReferencePointPeriodEnd,
	})
	s.NoError(err)
	invoiced := map[string]decimal.Decimal{}
	for _, li := range inv.LineItems {
		if li.PriceID != nil {
			invoiced[*li.PriceID] = invoiced[*li.PriceID].Add(li.Amount)
		}
	}

	resp, err := s.query(func(r *dto.UsageAnalyticsRequest) {
		r.GroupBy = []string{"sub_line_item_id"}
		r.Expand = []dto.UsageExpand{dto.UsageExpandPrice}
		if len(meterIDs) > 0 {
			r.MeterIDs = meterIDs
		}
	})
	s.NoError(err)
	for _, item := range resp.Items {
		s.Len(item.PriceIDs, 1)
		want, ok := invoiced[item.PriceIDs[0]]
		s.True(ok, "line %s has no invoice line", item.Group["sub_line_item_id"])
		s.Equal(want.StringFixed(2), item.Amount, "line %s", item.Group["sub_line_item_id"])
	}
	return len(resp.Items)
}

func (s *UsageAnalyticsSuite) TestFullPeriodLineTotalsEqualTheInvoice() {
	s.Equal(4, s.assertLinesEqualInvoice(), "tok_old, tok_new, calls, scaled; tiered is excluded")
}

// assertUnsupported checks the request fails as bad input naming exactly these meters and reasons.
func (s *UsageAnalyticsSuite) assertUnsupported(err error, want map[string]string) {
	s.Require().Error(err)
	s.True(ierr.IsValidation(err), "want validation error, got %v", err)
	ids := lo.Keys(want)
	sort.Strings(ids)
	named := lo.Map(ids, func(id string, _ int) string { return id + " (" + want[id] + ")" })
	s.Contains(err.Error(), "usage analytics does not support meters "+strings.Join(named, ", ")+" yet")
}

func (s *UsageAnalyticsSuite) TestGroupByDefaultsToMeter() {
	resp, err := s.query(func(r *dto.UsageAnalyticsRequest) { r.GroupBy = nil })
	s.NoError(err)
	// 13,000 tokens, 4 calls × 0.01, 40 scaled × 0.5.
	s.Equal(map[string]string{"m_tokens": "13000.00", "m_calls": "0.04", "m_scaled": "20.00"}, amountByGroup(resp, "meter_id"))
}

func (s *UsageAnalyticsSuite) TestUnsupportedMeterRejectsTheRequest() {
	_, err := s.query(func(r *dto.UsageAnalyticsRequest) { r.MeterIDs = nil })
	s.assertUnsupported(err, map[string]string{"m_tiered": excludedNotFlatPrice})

	resp, err := s.query(nil)
	s.NoError(err)
	// 13,000 tokens + 4 calls × 0.01 + 40 scaled × 0.5; the tiered $ never appears.
	s.Equal("13020.04", resp.Totals.Amount)
	s.Equal(map[string]string{"alice": "7020.00", "bob": "6000.00", "123": "0.03", dto.UsageNoneValue: "0.01"}, amountByGroup(resp, "properties.user"))
}

func (s *UsageAnalyticsSuite) TestEntitlementsAndGrantsExcludeTheirMeters() {
	ctx := s.GetContext()
	_, err := s.GetStores().EntitlementRepo.Create(ctx, &entitlement.Entitlement{
		ID: "ent_calls", EntityType: types.ENTITLEMENT_ENTITY_TYPE_PLAN, EntityID: "plan_usage", FeatureID: "f_calls",
		FeatureType: types.FeatureTypeMetered, IsEnabled: true, UsageLimit: lo.ToPtr(int64(100)),
		UsageResetPeriod: types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY, BaseModel: types.GetDefaultBaseModel(ctx),
	})
	s.NoError(err)
	_, err = s.GetStores().EntitlementGrantRepo.Create(ctx, &entitlementgrant.EntitlementGrant{
		ID: "eg_scaled", CustomerID: "cust_parent", SubscriptionID: "sub_parent",
		ScopeEntityType: types.EntitlementGrantScopeFeature, ScopeEntityID: "f_scaled",
		Measure: types.EntitlementGrantMeasureQuantity, Quota: decimal.NewFromInt(10),
		ValidFrom: queryOct1, ValidTo: queryNov1, BaseModel: types.GetDefaultBaseModel(ctx),
	})
	s.NoError(err)

	_, err = s.query(nil)
	s.assertUnsupported(err, map[string]string{"m_calls": excludedEntitlement, "m_scaled": excludedEntitlement})

	resp, err := s.query(func(r *dto.UsageAnalyticsRequest) { r.MeterIDs = []string{"m_tokens"} })
	s.NoError(err)
	s.Equal("13000.00", resp.Totals.Amount)
}

func (s *UsageAnalyticsSuite) TestSubscriptionGrantExcludesTheLinesBillingLowered() {
	ctx := s.GetContext()
	// Billing refuses subscription grants next to a tiered line, so end it before the period.
	tiered, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, "li_tiered")
	s.NoError(err)
	tiered.EndDate = queryOct1
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Update(ctx, tiered))
	_, err = s.GetStores().EntitlementGrantRepo.Create(ctx, &entitlementgrant.EntitlementGrant{
		ID: "eg_sub", CustomerID: "cust_parent", SubscriptionID: "sub_parent",
		ScopeEntityType: types.EntitlementGrantScopeSubscription, ScopeEntityID: "sub_parent",
		Measure: types.EntitlementGrantMeasureAmount, Quota: decimal.NewFromInt(10),
		ValidFrom: queryOct1, ValidTo: queryNov1, BaseModel: types.GetDefaultBaseModel(ctx),
	})
	s.NoError(err)
	// Billing applies the grant from its stored usage, which the invoice run refreshes.
	sub, err := s.GetStores().SubscriptionRepo.Get(ctx, s.parent.ID)
	s.NoError(err)
	_, err = s.billing.PrepareSubscriptionInvoiceRequest(ctx, &dto.PrepareSubscriptionInvoiceRequestParams{
		Subscription: sub, PeriodStart: queryOct1, PeriodEnd: queryNov1, ReferencePoint: types.ReferencePointPeriodEnd,
	})
	s.NoError(err)

	// Billing spreads the $10 over the tokens and scaled lines; calls stay at their flat price.
	_, err = s.query(nil)
	s.assertUnsupported(err, map[string]string{"m_scaled": excludedEntitlement, "m_tokens": excludedEntitlement})

	resp, err := s.query(func(r *dto.UsageAnalyticsRequest) { r.MeterIDs = []string{"m_calls"} })
	s.NoError(err)
	s.Equal("0.04", resp.Totals.Amount)
	s.Equal(1, s.assertLinesEqualInvoice("m_calls"), "the calls line equals its invoice line")
}

func (s *UsageAnalyticsSuite) TestChildSeesOnlyItsOwnEventsAtTheParentsPrices() {
	resp, err := s.query(func(r *dto.UsageAnalyticsRequest) {
		r.ExternalCustomerID = "acme_child"
		r.Expand = []dto.UsageExpand{dto.UsageExpandSubscription, dto.UsageExpandPlan}
	})
	s.NoError(err)
	s.Equal(map[string]string{"bob": "6000.00", dto.UsageNoneValue: "0.01"}, amountByGroup(resp, "properties.user"))
	s.Equal([]string{"sub_parent"}, resp.Items[0].SubscriptionIDs, "priced on the parent's lines")
	s.Equal([]string{"plan_usage"}, resp.Items[0].PlanIDs)
}

func (s *UsageAnalyticsSuite) TestPausedChildHasNoAmountAsTheParentInvoiceSkipsIt() {
	child := *s.child
	child.SubscriptionStatus = types.SubscriptionStatusPaused
	s.NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), &child))

	resp, err := s.query(func(r *dto.UsageAnalyticsRequest) { r.ExternalCustomerID = "acme_child" })
	s.NoError(err)
	s.Empty(resp.Items)
	s.Equal("0.00", resp.Totals.Amount)
}

// addCoupon attaches a coupon to the parent subscription, or to one of its lines.
func (s *UsageAnalyticsSuite) addCoupon(id string, typ types.CouponType, cadence types.CouponCadence, amount string, lineID *string) {
	ctx := s.GetContext()
	st := s.GetStores()
	c := &coupon.Coupon{ID: id, Name: id, Type: typ, Cadence: cadence, Currency: "usd", BaseModel: types.GetDefaultBaseModel(ctx)}
	if typ == types.CouponTypePercentage {
		c.PercentageOff = lo.ToPtr(decimal.RequireFromString(amount))
	} else {
		c.AmountOff = lo.ToPtr(decimal.RequireFromString(amount))
	}
	s.NoError(st.CouponRepo.Create(ctx, c))
	s.NoError(st.CouponAssociationRepo.Create(ctx, &coupon_association.CouponAssociation{
		ID: "ca_" + id, CouponID: id, SubscriptionID: s.parent.ID, SubscriptionLineItemID: lineID,
		StartDate: queryOct1, BaseModel: types.GetDefaultBaseModel(ctx),
	}))
}

func (s *UsageAnalyticsSuite) TestPercentageCouponsDiscountEachLineAsTheInvoiceDoes() {
	s.addCoupon("c_line20", types.CouponTypePercentage, types.CouponCadenceForever, "20", lo.ToPtr("li_tok_new"))
	s.addCoupon("c_sub10", types.CouponTypePercentage, types.CouponCadenceRepeated, "10", nil)
	s.addCoupon("c_fixed5", types.CouponTypeFixed, types.CouponCadenceForever, "5", nil)

	tokens := func(apply bool) *dto.UsageAnalyticsResponse {
		resp, err := s.query(func(r *dto.UsageAnalyticsRequest) {
			r.MeterIDs = []string{"m_tokens"}
			r.ApplyDiscounts = apply
		})
		s.NoError(err)
		return resp
	}

	// alice: $2,000 on the old line × 0.9, plus $5,000 on the new line × 0.8 × 0.9; bob: $6,000 × 0.9.
	resp := tokens(true)
	s.Equal(map[string]string{"alice": "5400.00", "bob": "5400.00"}, amountByGroup(resp, "properties.user"))
	s.Equal(dto.UsageAmounts{Subtotal: "13000.00", Discount: "2200.00", Amount: "10800.00"}, resp.Totals.UsageAmounts)
	s.Equal([]string{"c_fixed5"}, resp.UnappliedCouponIDs, "a fixed-amount coupon cannot be spread over events")

	resp = tokens(false)
	s.Equal(map[string]string{"alice": "7000.00", "bob": "6000.00"}, amountByGroup(resp, "properties.user"))
	s.Equal(dto.UsageAmounts{Subtotal: "13000.00", Discount: "0.00", Amount: "13000.00"}, resp.Totals.UsageAmounts, "no discount without the flag")
	s.Empty(resp.UnappliedCouponIDs)
}

// A second, quarterly subscription on m_calls overlaps the monthly one: both charge the same events,
// but the events and their usage count once unless rows are split by line.
// addQuarterlyCallsSubscription gives acme a second, quarterly subscription on m_calls at $0.02,
// overlapping the monthly one.
func (s *UsageAnalyticsSuite) addQuarterlyCallsSubscription() {
	ctx := s.GetContext()
	st := s.GetStores()
	base := types.GetDefaultBaseModel(ctx)
	s.NoError(st.PriceRepo.Create(ctx, &price.Price{
		ID: "p_calls_q", Amount: decimal.RequireFromString("0.02"), Currency: "usd",
		EntityType: types.PRICE_ENTITY_TYPE_PLAN, EntityID: "plan_usage", Type: types.PRICE_TYPE_USAGE,
		BillingPeriod: types.BILLING_PERIOD_QUARTER, BillingPeriodCount: 1, BillingModel: types.BILLING_MODEL_FLAT_FEE,
		BillingCadence: types.BILLING_CADENCE_RECURRING, InvoiceCadence: types.InvoiceCadenceArrear,
		MeterID: "m_calls", BaseModel: base,
	}))
	quarterly := &subscription.Subscription{
		ID: "sub_quarterly", PlanID: "plan_usage", CustomerID: "cust_parent", StartDate: queryOct1,
		BillingAnchor: queryOct1, CurrentPeriodStart: queryOct1, CurrentPeriodEnd: queryOct1.AddDate(0, 3, 0), Currency: "usd",
		BillingPeriod: types.BILLING_PERIOD_QUARTER, BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive, SubscriptionType: types.SubscriptionTypeStandalone, BaseModel: base,
	}
	s.NoError(st.SubscriptionRepo.CreateWithLineItems(ctx, quarterly, []*subscription.SubscriptionLineItem{{
		ID: "li_calls_q", SubscriptionID: quarterly.ID, CustomerID: quarterly.CustomerID, EntityID: "plan_usage",
		EntityType: types.SubscriptionLineItemEntityTypePlan, PriceID: "p_calls_q", PriceType: types.PRICE_TYPE_USAGE,
		MeterID: "m_calls", DisplayName: "li_calls_q", Currency: "usd", BillingPeriod: types.BILLING_PERIOD_QUARTER,
		BillingPeriodCount: 1, InvoiceCadence: types.InvoiceCadenceArrear, StartDate: queryOct1, BaseModel: base,
	}}))
}

func (s *UsageAnalyticsSuite) TestOverlappingSubscriptionsCountEachEventOnce() {
	s.addQuarterlyCallsSubscription()

	// 15 days inside both subscriptions.
	window := func(r *dto.UsageAnalyticsRequest) {
		r.MeterIDs = []string{"m_calls"}
		r.StartTime, r.EndTime = lo.ToPtr(queryOct1), lo.ToPtr(queryOct15)
	}

	// Monthly: acme's 3 calls + the child's 1 at $0.01; quarterly: acme's 3 calls at $0.02.
	resp, err := s.query(func(r *dto.UsageAnalyticsRequest) { window(r); r.GroupBy = []string{"meter_id"} })
	s.NoError(err)
	s.Require().Len(resp.Items, 1)
	row := resp.Items[0]
	s.Equal("0.10", row.Amount, "both subscriptions charge the events")
	s.Equal("4", row.Usage, "each event counts once")
	s.EqualValues(4, row.EventCount)
	s.Equal([]string{"sub_parent", "sub_quarterly"}, row.SubscriptionIDs)
	s.Equal("4", resp.Totals.Usage)

	resp, err = s.query(func(r *dto.UsageAnalyticsRequest) { window(r); r.GroupBy = []string{"sub_line_item_id"} })
	s.NoError(err)
	byLine := lo.SliceToMap(resp.Items, func(i dto.UsageAnalyticsItem) (string, [2]string) {
		return i.Group["sub_line_item_id"], [2]string{i.Amount, i.Usage}
	})
	s.Equal(map[string][2]string{"li_calls": {"0.04", "4"}, "li_calls_q": {"0.06", "3"}}, byLine, "split by line, each line counts its own events")
	s.Equal("7", resp.Totals.Usage, "totals add up the lines")

	resp, err = s.query(func(r *dto.UsageAnalyticsRequest) {
		window(r)
		r.GroupBy = []string{"meter_id"}
		r.Filters = []dto.UsageAnalyticsFilter{{Key: "sub_line_item_id", Op: "in", Values: []string{"li_calls_q"}}}
	})
	s.NoError(err)
	s.Require().Len(resp.Items, 1)
	s.Equal([2]string{"0.06", "3"}, [2]string{resp.Items[0].Amount, resp.Items[0].Usage}, "the filtered line's events count")
}

func (s *UsageAnalyticsSuite) TestSourcesKeepOnlyTheirEvents() {
	resp, err := s.query(func(r *dto.UsageAnalyticsRequest) { r.Sources = []string{"api"} })
	s.NoError(err)
	s.Equal("13020.04", resp.Totals.Amount, "every fixture event comes from api")

	resp, err = s.query(func(r *dto.UsageAnalyticsRequest) { r.Sources = []string{"sdk"} })
	s.NoError(err)
	s.Equal("0.00", resp.Totals.Amount)
	s.Empty(resp.Items)
}

func (s *UsageAnalyticsSuite) TestRowsAlwaysCarryIDsAndExpandAddsObjectMaps() {
	resp, err := s.query(func(r *dto.UsageAnalyticsRequest) {
		r.MeterIDs = []string{"m_tokens"}
		r.GroupBy = []string{"meter_id"}
	})
	s.NoError(err)
	s.Require().Len(resp.Items, 1)
	row := resp.Items[0]
	s.Equal([]string{"p_tok_new", "p_tok_old"}, row.PriceIDs, "IDs come back without expand")
	s.Equal([]string{"plan_usage"}, row.PlanIDs)
	s.Equal([]string{"sub_parent"}, row.SubscriptionIDs)
	s.Nil(resp.Prices)
	s.Nil(resp.Plans)

	resp, err = s.query(func(r *dto.UsageAnalyticsRequest) {
		r.MeterIDs = []string{"m_tokens"}
		r.GroupBy = []string{"meter_id"}
		r.Expand = []dto.UsageExpand{dto.UsageExpandPrice, dto.UsageExpandPlan, dto.UsageExpandAddon,
			dto.UsageExpandSubscription, dto.UsageExpandMeter, dto.UsageExpandFeature}
	})
	s.NoError(err)
	s.ElementsMatch([]string{"p_tok_new", "p_tok_old"}, lo.Keys(resp.Prices))
	s.Equal("0.0025", resp.Prices["p_tok_new"].Amount.String())
	s.Equal([]string{"plan_usage"}, lo.Keys(resp.Plans))
	s.Empty(resp.Addons, "no add-on priced these rows")
	s.Equal([]string{"sub_parent"}, lo.Keys(resp.Subscriptions))
	s.Equal([]string{"m_tokens"}, lo.Keys(resp.Meters))
	s.Equal([]string{"f_tokens"}, lo.Keys(resp.Features))
}

func (s *UsageAnalyticsSuite) TestCancelledChildKeepsTheAmountOfPeriodsItsParentInvoiced() {
	cancel := func(at time.Time) {
		child := *s.child
		child.SubscriptionStatus = types.SubscriptionStatusCancelled
		child.CancelledAt = lo.ToPtr(at)
		s.NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), &child))
	}
	tokens := func(customer string) map[string]string {
		resp, err := s.query(func(r *dto.UsageAnalyticsRequest) {
			r.ExternalCustomerID = customer
			r.MeterIDs = []string{"m_tokens"}
		})
		s.NoError(err)
		return amountByGroup(resp, "properties.user")
	}

	// Cancelled after October: the October invoice, computed on Nov 1, still charged bob.
	cancel(queryNov1.AddDate(0, 0, 4))
	s.Equal(map[string]string{"bob": "6000.00"}, tokens("acme_child"))
	s.Equal(map[string]string{"alice": "7000.00", "bob": "6000.00"}, tokens("acme"))

	// Cancelled during October: the October invoice left the child out.
	cancel(queryOct15.AddDate(0, 0, 5))
	s.Empty(tokens("acme_child"))
	s.Equal(map[string]string{"alice": "7000.00"}, tokens("acme"))
	s.Positive(s.assertLinesEqualInvoice(), "billing's own October invoice agrees")
}

func (s *UsageAnalyticsSuite) TestColumnsMetricsAndPaging() {
	resp, err := s.query(func(r *dto.UsageAnalyticsRequest) {
		r.GroupBy = []string{"meter_id"}
		r.Columns = "properties.user"
		r.Metrics = []dto.UsageAnalyticsMetric{{Type: "distinct", Key: "properties.user"}}
		r.Limit = 1
		r.Offset = 1
	})
	s.NoError(err)
	s.Equal(types.PaginationResponse{Total: 3, Limit: 1, Offset: 1}, resp.Pagination, "offset echoes the request")
	s.Len(resp.Items, 1)
	s.Equal([]string{"alice", "bob", "123", dto.UsageNoneValue}, resp.ColumnValues, "ordered by total amount")
	s.Equal("13020.04", resp.Totals.Amount, "totals cover every row, not the page")

	row := resp.Items[0]
	s.Equal("m_scaled", row.Group["meter_id"])
	s.Equal(map[string]dto.UsageColumn{"alice": {Amount: "20.00"}}, row.Columns)
	s.Equal(dto.UsageAnalyticsValue{Count: 1, Values: []string{"alice"}}, row.Metrics["distinct:properties.user"])
}

func (s *UsageAnalyticsSuite) TestWeeklyPointsListEveryWindow() {
	resp, err := s.query(func(r *dto.UsageAnalyticsRequest) {
		r.MeterIDs = []string{"m_tokens"}
		r.WindowSize = types.WindowSizeWeek
	})
	s.NoError(err)

	type point struct{ start, amount string }
	points := func(ps []dto.UsagePoint) []point {
		return lo.Map(ps, func(p dto.UsagePoint, _ int) point { return point{p.Timestamp.Format(time.DateOnly), p.Amount} })
	}
	// Weeks start on Sunday; Oct 1 2026 is a Thursday, so the first window starts Sep 27.
	byUser := lo.SliceToMap(resp.Items, func(i dto.UsageAnalyticsItem) (string, []point) { return i.Group["properties.user"], points(i.Points) })
	s.Equal([]point{{"2026-09-27", "0.00"}, {"2026-10-04", "2000.00"}, {"2026-10-11", "0.00"}, {"2026-10-18", "5000.00"}, {"2026-10-25", "0.00"}}, byUser["alice"])
	s.Equal([]point{{"2026-09-27", "0.00"}, {"2026-10-04", "0.00"}, {"2026-10-11", "6000.00"}, {"2026-10-18", "0.00"}, {"2026-10-25", "0.00"}}, byUser["bob"])
}

func (s *UsageAnalyticsSuite) TestRejectedRequests() {
	tests := []struct {
		name   string
		mutate func(r *dto.UsageAnalyticsRequest)
	}{
		{"usage sort across meters", func(r *dto.UsageAnalyticsRequest) { r.Sort = &dto.UsageAnalyticsSort{Field: "usage"} }},
		{"unknown metric", func(r *dto.UsageAnalyticsRequest) {
			r.Metrics = []dto.UsageAnalyticsMetric{{Type: "percentile", Key: "properties.user"}}
		}},
		{"unknown key", func(r *dto.UsageAnalyticsRequest) { r.GroupBy = []string{"customer"} }},
		{"only start_time", func(r *dto.UsageAnalyticsRequest) { r.EndTime = nil }},
		{"unknown timezone", func(r *dto.UsageAnalyticsRequest) { r.Timezone = "Mars/Base" }},
		{"unknown expand", func(r *dto.UsageAnalyticsRequest) { r.Expand = []dto.UsageExpand{"wallet"} }},
		{"line item columns", func(r *dto.UsageAnalyticsRequest) { r.Columns = "sub_line_item_id" }},
		{"hourly windows", func(r *dto.UsageAnalyticsRequest) { r.WindowSize = types.WindowSizeHour }},
		{"more than 400 daily windows", func(r *dto.UsageAnalyticsRequest) {
			r.WindowSize = types.WindowSizeDay
			r.StartTime = lo.ToPtr(queryOct1.AddDate(-2, 0, 0))
		}},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			_, err := s.query(tt.mutate)
			s.True(ierr.IsValidation(err), "want validation error, got %v", err)
		})
	}
}

func (s *UsageAnalyticsSuite) TestUnsupportedKeyNamesTheSupportedOnes() {
	tests := []struct {
		name   string
		mutate func(r *dto.UsageAnalyticsRequest)
		want   string
	}{
		{"group_by", func(r *dto.UsageAnalyticsRequest) { r.GroupBy = []string{"customer"} },
			`unsupported group_by "customer"; supported: properties.<name>, source, meter_id, feature_id, sub_line_item_id`},
		{"month in group_by", func(r *dto.UsageAnalyticsRequest) { r.GroupBy = []string{"month"} },
			`unsupported group_by "month"; supported: properties.<name>, source, meter_id, feature_id, sub_line_item_id`},
		{"columns", func(r *dto.UsageAnalyticsRequest) { r.Columns = "feature_id" },
			`unsupported columns "feature_id"; supported: properties.<name>, source, meter_id, month`},
		{"filters", func(r *dto.UsageAnalyticsRequest) {
			r.Filters = []dto.UsageAnalyticsFilter{{Key: "month", Op: "in", Values: []string{"2026-10"}}}
		}, `unsupported filters "month"; supported: properties.<name>, source, meter_id, feature_id, sub_line_item_id`},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			_, err := s.query(tt.mutate)
			s.True(ierr.IsValidation(err), "want validation error, got %v", err)
			s.ErrorContains(err, tt.want)
		})
	}
}

func (s *UsageAnalyticsSuite) TestFilterOnMissingValueAndSingleMeterUsageSort() {
	resp, err := s.query(func(r *dto.UsageAnalyticsRequest) {
		r.MeterIDs = []string{"m_calls"}
		r.Filters = []dto.UsageAnalyticsFilter{{Key: "properties.user", Op: "in", Values: []string{dto.UsageNoneValue}}}
		r.Sort = &dto.UsageAnalyticsSort{Field: "usage"}
	})
	s.NoError(err)
	s.Equal(map[string]string{dto.UsageNoneValue: "0.01"}, amountByGroup(resp, "properties.user"))
}

func TestSplitByBillingPeriod(t *testing.T) {
	sub := &subscription.Subscription{
		ID: "sub", StartDate: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), BillingAnchor: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
	}
	cs := billedSubscription{sub: sub, owner: sub, start: time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC), end: time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)}
	segs, err := splitByBillingPeriod(cs)
	if err != nil {
		t.Fatal(err)
	}

	if len(segs) != 2 {
		t.Fatalf("want 2 segments, got %d", len(segs))
	}
	sep1, oct1 := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if !segs[0].PeriodStart.Equal(sep1) || !segs[0].PeriodEnd.Equal(oct1) || !segs[0].Start.Equal(cs.start) || !segs[0].End.Equal(oct1) {
		t.Fatalf("segment 1 wrong: %+v", segs[0])
	}
	if !segs[1].Start.Equal(oct1) || !segs[1].End.Equal(cs.end) || !segs[1].PeriodEnd.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("segment 2 wrong: %+v", segs[1])
	}

	// Billing cannot invoice a subscription without a valid schedule, so neither can usage analytics.
	broken := *sub
	broken.BillingPeriod = "FORTNIGHTLY"
	if _, err := splitByBillingPeriod(billedSubscription{sub: &broken, owner: &broken, start: cs.start, end: cs.end}); err == nil {
		t.Fatal("want an error for an invalid billing period")
	}
}

// parityValues are the values both analytics APIs report for one group, money rounded to cents.
type parityValues struct {
	Usage, Subtotal, Discount, Amount string
	Events                            uint64
}

// oldAnalytics runs the existing analytics API (/events/analytics) over the suite's data.
func (s *UsageAnalyticsSuite) oldAnalytics(mutate func(p *events.MeterUsageDetailedAnalyticsParams)) *dto.GetUsageAnalyticsResponse {
	ctx := s.GetContext()
	p := &events.MeterUsageDetailedAnalyticsParams{
		TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx),
		ExternalCustomerID: "acme", StartTime: queryOct1, EndTime: queryNov1,
		MeterIDs: supportedMeters, UseFinal: true,
	}
	if mutate != nil {
		mutate(p)
	}
	resp, err := NewMeterUsageService(s.params).GetDetailedAnalytics(ctx, p)
	s.Require().NoError(err)
	return resp
}

// oldBy sums the old API's items per key.
func oldBy(resp *dto.GetUsageAnalyticsResponse, key func(dto.UsageAnalyticItem) string) map[string]parityValues {
	type acc struct {
		usage, subtotal, discount, amount decimal.Decimal
		events                            uint64
	}
	sums := map[string]*acc{}
	for _, it := range resp.Items {
		k := key(it)
		a := sums[k]
		if a == nil {
			a = &acc{}
			sums[k] = a
		}
		a.usage = a.usage.Add(it.TotalUsage)
		a.subtotal = a.subtotal.Add(it.Subtotal)
		a.discount = a.discount.Add(it.TotalDiscount)
		a.amount = a.amount.Add(it.TotalCost)
		a.events += it.EventCount
	}
	out := map[string]parityValues{}
	for k, a := range sums {
		out[k] = parityValues{
			Usage: a.usage.String(), Subtotal: a.subtotal.StringFixed(2), Discount: a.discount.StringFixed(2),
			Amount: a.amount.StringFixed(2), Events: a.events,
		}
	}
	return out
}

// newBy reads the new API's rows per key.
func newBy(resp *dto.UsageAnalyticsResponse, key func(dto.UsageAnalyticsItem) string) map[string]parityValues {
	out := map[string]parityValues{}
	for _, it := range resp.Items {
		out[key(it)] = parityValues{
			Usage: decimal.RequireFromString(it.Usage).String(), Subtotal: it.Subtotal, Discount: it.Discount,
			Amount: it.Amount, Events: it.EventCount,
		}
	}
	return out
}

// pointKey is one group's time window, with money rounded to cents.
type pointKey struct{ group, window string }

func oldPoints(resp *dto.GetUsageAnalyticsResponse, key func(dto.UsageAnalyticItem) string) map[pointKey][2]string {
	sums := map[pointKey][2]decimal.Decimal{}
	for _, it := range resp.Items {
		for _, p := range it.Points {
			k := pointKey{key(it), p.Timestamp.UTC().Format(time.RFC3339)}
			v := sums[k]
			sums[k] = [2]decimal.Decimal{v[0].Add(p.Usage), v[1].Add(p.Cost)}
		}
	}
	return nonZeroPoints(sums)
}

func newPoints(resp *dto.UsageAnalyticsResponse, key func(dto.UsageAnalyticsItem) string) map[pointKey][2]string {
	sums := map[pointKey][2]decimal.Decimal{}
	for _, it := range resp.Items {
		for _, p := range it.Points {
			sums[pointKey{key(it), p.Timestamp.UTC().Format(time.RFC3339)}] = [2]decimal.Decimal{
				decimal.RequireFromString(p.Usage), decimal.RequireFromString(p.Amount),
			}
		}
	}
	return nonZeroPoints(sums)
}

// nonZeroPoints drops empty windows: the new API lists every window, the old one only those with usage.
func nonZeroPoints(sums map[pointKey][2]decimal.Decimal) map[pointKey][2]string {
	out := map[pointKey][2]string{}
	for k, v := range sums {
		if !v[0].IsZero() || !v[1].IsZero() {
			out[k] = [2]string{v[0].String(), v[1].StringFixed(2)}
		}
	}
	return out
}

// groupKey joins a new row's group values in order, with $none read as the old API's empty value.
func groupKey(keys ...string) func(dto.UsageAnalyticsItem) string {
	return func(it dto.UsageAnalyticsItem) string {
		return strings.Join(lo.Map(keys, func(k string, _ int) string { return strings.TrimPrefix(it.Group[k], dto.UsageNoneValue) }), "|")
	}
}

// parityCase is one request asked of both analytics APIs.
type parityCase struct {
	name   string
	old    func(p *events.MeterUsageDetailedAnalyticsParams)
	new    func(r *dto.UsageAnalyticsRequest)
	oldKey func(dto.UsageAnalyticItem) string
	newKey func(dto.UsageAnalyticsItem) string
}

var (
	byOldLine  = func(it dto.UsageAnalyticItem) string { return it.SubLineItemID }
	byOldMeter = func(it dto.UsageAnalyticItem) string { return it.MeterID }
	byOldUser  = func(it dto.UsageAnalyticItem) string { return it.MeterID + "|" + it.Properties["user"] }
	byOldSrc   = func(it dto.UsageAnalyticItem) string { return it.MeterID + "|" + it.Source }
)

// assertParity runs a case through both APIs and requires the same values per group, per window and in total.
func (s *UsageAnalyticsSuite) assertParity(c parityCase) {
	old := s.oldAnalytics(func(p *events.MeterUsageDetailedAnalyticsParams) {
		p.IncludeChildren = true // the new API, like the invoice, always prices a parent's children
		if c.old != nil {
			c.old(p)
		}
	})
	resp, err := s.query(func(r *dto.UsageAnalyticsRequest) {
		r.ApplyDiscounts = true // the old API always applies discounts
		if c.new != nil {
			c.new(r)
		}
	})
	s.Require().NoError(err)

	s.Equal(oldBy(old, c.oldKey), newBy(resp, c.newKey), "values per group")
	s.Equal(oldPoints(old, c.oldKey), newPoints(resp, c.newKey), "values per window")
	s.Equal(
		dto.UsageAmounts{Subtotal: old.Subtotal.StringFixed(2), Discount: old.TotalDiscount.StringFixed(2), Amount: old.TotalCost.StringFixed(2)},
		resp.Totals.UsageAmounts, "totals")
}

// metersWithoutMultiplier leave out m_scaled: the old API reads SUM_WITH_MULTIPLIER as zero once the
// request sets group_by, window_size or sources (buildMeterUsageAggregationColumns has no case for it).
var metersWithoutMultiplier = []string{"m_tokens", "m_calls"}

func (s *UsageAnalyticsSuite) TestParityWithOldAnalytics() {
	week := func(p *events.MeterUsageDetailedAnalyticsParams) {
		p.MeterIDs, p.WindowSize = metersWithoutMultiplier, types.WindowSizeWeek
	}
	day := func(p *events.MeterUsageDetailedAnalyticsParams) {
		p.MeterIDs, p.WindowSize = metersWithoutMultiplier, types.WindowSizeDay
	}
	oct10, oct20 := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)

	cases := []parityCase{
		{name: "per line, full period", oldKey: byOldLine, newKey: groupKey("sub_line_item_id"),
			new: func(r *dto.UsageAnalyticsRequest) { r.GroupBy = []string{"sub_line_item_id"} }},
		{name: "per meter", oldKey: byOldMeter, newKey: groupKey("meter_id"),
			new: func(r *dto.UsageAnalyticsRequest) { r.GroupBy = []string{"meter_id"} }},
		{name: "per meter, part of the period", oldKey: byOldMeter, newKey: groupKey("meter_id"),
			old: func(p *events.MeterUsageDetailedAnalyticsParams) { p.StartTime, p.EndTime = oct10, oct20 },
			new: func(r *dto.UsageAnalyticsRequest) {
				r.GroupBy = []string{"meter_id"}
				r.StartTime, r.EndTime = lo.ToPtr(oct10), lo.ToPtr(oct20)
			}},
		{name: "per meter, daily points", oldKey: byOldMeter, newKey: groupKey("meter_id"), old: day,
			new: func(r *dto.UsageAnalyticsRequest) {
				r.MeterIDs, r.GroupBy, r.WindowSize = metersWithoutMultiplier, []string{"meter_id"}, types.WindowSizeDay
			}},
		{name: "per meter, weekly points", oldKey: byOldMeter, newKey: groupKey("meter_id"), old: week,
			new: func(r *dto.UsageAnalyticsRequest) {
				r.MeterIDs, r.GroupBy, r.WindowSize = metersWithoutMultiplier, []string{"meter_id"}, types.WindowSizeWeek
			}},
		{name: "per feature", oldKey: byOldMeter, newKey: groupKey("meter_id"),
			old: func(p *events.MeterUsageDetailedAnalyticsParams) {
				p.MeterIDs = nil
				p.FeatureIDs = []string{"f_tokens", "f_calls"}
			},
			new: func(r *dto.UsageAnalyticsRequest) {
				r.MeterIDs, r.FeatureIDs, r.GroupBy = nil, []string{"f_tokens", "f_calls"}, []string{"meter_id"}
			}},
		{name: "per user property", oldKey: byOldUser, newKey: groupKey("meter_id", "properties.user"),
			old: func(p *events.MeterUsageDetailedAnalyticsParams) {
				p.MeterIDs = metersWithoutMultiplier
				p.GroupBy = []string{"properties.user"}
			},
			new: func(r *dto.UsageAnalyticsRequest) {
				r.MeterIDs, r.GroupBy = metersWithoutMultiplier, []string{"meter_id", "properties.user"}
			}},
		{name: "per user property, weekly points", oldKey: byOldUser, newKey: groupKey("meter_id", "properties.user"),
			old: func(p *events.MeterUsageDetailedAnalyticsParams) {
				p.MeterIDs, p.GroupBy, p.WindowSize = metersWithoutMultiplier, []string{"properties.user"}, types.WindowSizeWeek
			},
			new: func(r *dto.UsageAnalyticsRequest) {
				r.MeterIDs, r.GroupBy, r.WindowSize = metersWithoutMultiplier, []string{"meter_id", "properties.user"}, types.WindowSizeWeek
			}},
		{name: "per source", oldKey: byOldSrc, newKey: groupKey("meter_id", "source"),
			old: func(p *events.MeterUsageDetailedAnalyticsParams) {
				p.MeterIDs = metersWithoutMultiplier
				p.GroupBy = []string{"source"}
			},
			new: func(r *dto.UsageAnalyticsRequest) {
				r.MeterIDs, r.GroupBy = metersWithoutMultiplier, []string{"meter_id", "source"}
			}},
		{name: "property filter", oldKey: byOldMeter, newKey: groupKey("meter_id"),
			old: func(p *events.MeterUsageDetailedAnalyticsParams) {
				p.MeterIDs, p.PropertyFilters = metersWithoutMultiplier, map[string][]string{"user": {"alice"}}
			},
			new: func(r *dto.UsageAnalyticsRequest) {
				r.MeterIDs, r.GroupBy = metersWithoutMultiplier, []string{"meter_id"}
				r.Filters = []dto.UsageAnalyticsFilter{{Key: "properties.user", Op: "in", Values: []string{"alice"}}}
			}},
		{name: "source filter", oldKey: byOldMeter, newKey: groupKey("meter_id"),
			old: func(p *events.MeterUsageDetailedAnalyticsParams) {
				p.MeterIDs, p.Sources = metersWithoutMultiplier, []string{"api"}
			},
			new: func(r *dto.UsageAnalyticsRequest) {
				r.MeterIDs, r.GroupBy, r.Sources = metersWithoutMultiplier, []string{"meter_id"}, []string{"api"}
			}},
		{name: "child customer", oldKey: byOldLine, newKey: groupKey("sub_line_item_id"),
			old: func(p *events.MeterUsageDetailedAnalyticsParams) { p.ExternalCustomerID = "acme_child" },
			new: func(r *dto.UsageAnalyticsRequest) {
				r.ExternalCustomerID, r.GroupBy = "acme_child", []string{"sub_line_item_id"}
			}},
	}
	for _, c := range cases {
		s.Run(c.name, func() { s.assertParity(c) })
	}
}

func (s *UsageAnalyticsSuite) TestParityWithOldAnalyticsWithCoupons() {
	s.addCoupon("c_line20", types.CouponTypePercentage, types.CouponCadenceForever, "20", lo.ToPtr("li_tok_new"))
	s.addCoupon("c_sub10", types.CouponTypePercentage, types.CouponCadenceRepeated, "10", nil)
	for _, c := range []parityCase{
		{name: "per line", oldKey: byOldLine, newKey: groupKey("sub_line_item_id"),
			new: func(r *dto.UsageAnalyticsRequest) { r.GroupBy = []string{"sub_line_item_id"} }},
		{name: "per user property, daily points", oldKey: byOldUser, newKey: groupKey("meter_id", "properties.user"),
			old: func(p *events.MeterUsageDetailedAnalyticsParams) {
				p.MeterIDs, p.GroupBy, p.WindowSize = metersWithoutMultiplier, []string{"properties.user"}, types.WindowSizeDay
			},
			new: func(r *dto.UsageAnalyticsRequest) {
				r.MeterIDs, r.GroupBy, r.WindowSize = metersWithoutMultiplier, []string{"meter_id", "properties.user"}, types.WindowSizeDay
			}},
	} {
		s.Run(c.name, func() { s.assertParity(c) })
	}
}

// With overlapping subscriptions both APIs charge each line the same; per line, usage matches too.
// Per meter the old API adds each line's usage, so an event under two lines counts twice there;
// the new API counts it once.
func (s *UsageAnalyticsSuite) TestParityWithOldAnalyticsOverlappingSubscriptions() {
	s.addQuarterlyCallsSubscription()
	oct15 := func(p *events.MeterUsageDetailedAnalyticsParams) {
		p.MeterIDs, p.EndTime = []string{"m_calls"}, queryOct15
	}
	s.Run("per line", func() {
		s.assertParity(parityCase{oldKey: byOldLine, newKey: groupKey("sub_line_item_id"), old: oct15,
			new: func(r *dto.UsageAnalyticsRequest) {
				r.MeterIDs, r.EndTime, r.GroupBy = []string{"m_calls"}, lo.ToPtr(queryOct15), []string{"sub_line_item_id"}
			}})
	})
	s.Run("per meter: same money, usage counted once", func() {
		old := oldBy(s.oldAnalytics(func(p *events.MeterUsageDetailedAnalyticsParams) { p.IncludeChildren = true; oct15(p) }), byOldMeter)
		resp, err := s.query(func(r *dto.UsageAnalyticsRequest) {
			r.MeterIDs, r.EndTime, r.GroupBy, r.ApplyDiscounts = []string{"m_calls"}, lo.ToPtr(queryOct15), []string{"meter_id"}, true
		})
		s.Require().NoError(err)
		got := newBy(resp, groupKey("meter_id"))["m_calls"]
		s.Equal(old["m_calls"].Amount, got.Amount)
		s.Equal(parityValues{Usage: "7", Subtotal: "0.10", Discount: "0.00", Amount: "0.10", Events: 7}, old["m_calls"], "old: 4 + 3 events")
		s.Equal(parityValues{Usage: "4", Subtotal: "0.10", Discount: "0.00", Amount: "0.10", Events: 4}, got, "new: 4 distinct events")
	})
}
