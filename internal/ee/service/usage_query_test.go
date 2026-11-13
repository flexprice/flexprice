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

// UsageQuerySuite runs the usage breakdown over in-memory stores, with the in-memory reference
// standing in for ClickHouse, and checks it against billing on the same data.
type UsageQuerySuite struct {
	testutil.BaseServiceTestSuite
	params  ServiceParams
	svc     UsageQueryService
	billing BillingService
	parent  *subscription.Subscription
	child   *subscription.Subscription
}

func TestUsageQuery(t *testing.T) {
	suite.Run(t, new(UsageQuerySuite))
}

var (
	queryOct1  = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	queryOct15 = time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	queryNov1  = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
)

func (s *UsageQuerySuite) GetContext() context.Context {
	return types.SetEnvironmentID(s.BaseServiceTestSuite.GetContext(), "env_cost")
}

func (s *UsageQuerySuite) SetupTest() {
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
	s.svc = NewUsageQueryService(s.params, testutil.NewInMemoryUsageQueryStore(usage))
	s.billing = NewBillingService(s.params)
	s.seed()
}

// seed builds a parent (acme) with one inherited child (acme_child) on a monthly plan:
// tokens (SUM, price change on Oct 15), calls (COUNT), scaled (SUM_WITH_MULTIPLIER), tiered (excluded).
func (s *UsageQuerySuite) seed() {
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

func (s *UsageQuerySuite) usage(meterID, customerID string, ts time.Time, qty string, props map[string]any) {
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

func (s *UsageQuerySuite) query(mutate func(r *dto.UsageQueryRequest)) (*dto.UsageQueryResponse, error) {
	req := &dto.UsageQueryRequest{
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

func amountByGroup(resp *dto.UsageQueryResponse, key string) map[string]string {
	return lo.SliceToMap(resp.Items, func(i dto.UsageQueryItem) (string, string) { return i.Group[key], i.Amount })
}

func (s *UsageQuerySuite) TestPriceChangeAttributesEachEventAtItsOwnPrice() {
	resp, err := s.query(func(r *dto.UsageQueryRequest) { r.MeterIDs = []string{"m_tokens"} })
	s.NoError(err)
	s.Equal(map[string]string{"alice": "7000.00", "bob": "6000.00"}, amountByGroup(resp, "properties.user"))
	s.Equal("13000.00", resp.Totals.Amount)
	s.Equal("usd", resp.Currency)
	s.Equal("alice", resp.Items[0].Group["properties.user"], "default sort is total amount, descending")
}

// assertLinesEqualInvoice checks that every line in the breakdown amounts exactly what the October
// invoice charges for it, and returns how many lines there were.
func (s *UsageQuerySuite) assertLinesEqualInvoice(meterIDs ...string) int {
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

	resp, err := s.query(func(r *dto.UsageQueryRequest) {
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

func (s *UsageQuerySuite) TestFullPeriodLineTotalsEqualTheInvoice() {
	s.Equal(4, s.assertLinesEqualInvoice(), "tok_old, tok_new, calls, scaled; tiered is excluded")
}

// assertUnsupported checks the request fails as bad input naming exactly these meters and reasons.
func (s *UsageQuerySuite) assertUnsupported(err error, want map[string]string) {
	s.Require().Error(err)
	s.True(ierr.IsValidation(err), "want validation error, got %v", err)
	ids := lo.Keys(want)
	sort.Strings(ids)
	named := lo.Map(ids, func(id string, _ int) string { return id + " (" + want[id] + ")" })
	s.Contains(err.Error(), "usage analytics does not support meters "+strings.Join(named, ", ")+" yet")
}

func (s *UsageQuerySuite) TestGroupByDefaultsToMeter() {
	resp, err := s.query(func(r *dto.UsageQueryRequest) { r.GroupBy = nil })
	s.NoError(err)
	// 13,000 tokens, 4 calls × 0.01, 40 scaled × 0.5.
	s.Equal(map[string]string{"m_tokens": "13000.00", "m_calls": "0.04", "m_scaled": "20.00"}, amountByGroup(resp, "meter_id"))
}

func (s *UsageQuerySuite) TestUnsupportedMeterRejectsTheRequest() {
	_, err := s.query(func(r *dto.UsageQueryRequest) { r.MeterIDs = nil })
	s.assertUnsupported(err, map[string]string{"m_tiered": excludedNotFlatPrice})

	resp, err := s.query(nil)
	s.NoError(err)
	// 13,000 tokens + 4 calls × 0.01 + 40 scaled × 0.5; the tiered $ never appears.
	s.Equal("13020.04", resp.Totals.Amount)
	s.Equal(map[string]string{"alice": "7020.00", "bob": "6000.00", "123": "0.03", dto.UsageNoneValue: "0.01"}, amountByGroup(resp, "properties.user"))
}

func (s *UsageQuerySuite) TestEntitlementsAndGrantsExcludeTheirMeters() {
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

	resp, err := s.query(func(r *dto.UsageQueryRequest) { r.MeterIDs = []string{"m_tokens"} })
	s.NoError(err)
	s.Equal("13000.00", resp.Totals.Amount)
}

func (s *UsageQuerySuite) TestSubscriptionGrantExcludesTheLinesBillingLowered() {
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

	resp, err := s.query(func(r *dto.UsageQueryRequest) { r.MeterIDs = []string{"m_calls"} })
	s.NoError(err)
	s.Equal("0.04", resp.Totals.Amount)
	s.Equal(1, s.assertLinesEqualInvoice("m_calls"), "the calls line equals its invoice line")
}

func (s *UsageQuerySuite) TestChildSeesOnlyItsOwnEventsAtTheParentsPrices() {
	resp, err := s.query(func(r *dto.UsageQueryRequest) {
		r.ExternalCustomerID = "acme_child"
		r.Expand = []dto.UsageExpand{dto.UsageExpandSubscription, dto.UsageExpandPlan}
	})
	s.NoError(err)
	s.Equal(map[string]string{"bob": "6000.00", dto.UsageNoneValue: "0.01"}, amountByGroup(resp, "properties.user"))
	s.Equal([]string{"sub_parent"}, resp.Items[0].SubscriptionIDs, "priced on the parent's lines")
	s.Equal([]string{"plan_usage"}, resp.Items[0].PlanIDs)
}

func (s *UsageQuerySuite) TestPausedChildHasNoAmountAsTheParentInvoiceSkipsIt() {
	child := *s.child
	child.SubscriptionStatus = types.SubscriptionStatusPaused
	s.NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), &child))

	resp, err := s.query(func(r *dto.UsageQueryRequest) { r.ExternalCustomerID = "acme_child" })
	s.NoError(err)
	s.Empty(resp.Items)
	s.Equal("0.00", resp.Totals.Amount)
}

func (s *UsageQuerySuite) TestPercentageCouponsDiscountEachLineAsTheInvoiceDoes() {
	ctx := s.GetContext()
	st := s.GetStores()
	addCoupon := func(id string, typ types.CouponType, cadence types.CouponCadence, amount string, lineID *string) {
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
	addCoupon("c_line20", types.CouponTypePercentage, types.CouponCadenceForever, "20", lo.ToPtr("li_tok_new"))
	addCoupon("c_sub10", types.CouponTypePercentage, types.CouponCadenceRepeated, "10", nil)
	addCoupon("c_fixed5", types.CouponTypeFixed, types.CouponCadenceForever, "5", nil)

	tokens := func(apply bool) *dto.UsageQueryResponse {
		resp, err := s.query(func(r *dto.UsageQueryRequest) {
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

func (s *UsageQuerySuite) TestSourcesKeepOnlyTheirEvents() {
	resp, err := s.query(func(r *dto.UsageQueryRequest) { r.Sources = []string{"api"} })
	s.NoError(err)
	s.Equal("13020.04", resp.Totals.Amount, "every fixture event comes from api")

	resp, err = s.query(func(r *dto.UsageQueryRequest) { r.Sources = []string{"sdk"} })
	s.NoError(err)
	s.Equal("0.00", resp.Totals.Amount)
	s.Empty(resp.Items)
}

func (s *UsageQuerySuite) TestRowsAlwaysCarryIDsAndExpandAddsObjectMaps() {
	resp, err := s.query(func(r *dto.UsageQueryRequest) {
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

	resp, err = s.query(func(r *dto.UsageQueryRequest) {
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

func (s *UsageQuerySuite) TestCancelledChildKeepsTheAmountOfPeriodsItsParentInvoiced() {
	cancel := func(at time.Time) {
		child := *s.child
		child.SubscriptionStatus = types.SubscriptionStatusCancelled
		child.CancelledAt = lo.ToPtr(at)
		s.NoError(s.GetStores().SubscriptionRepo.Update(s.GetContext(), &child))
	}
	tokens := func(customer string) map[string]string {
		resp, err := s.query(func(r *dto.UsageQueryRequest) {
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

func (s *UsageQuerySuite) TestColumnsMetricsAndPaging() {
	resp, err := s.query(func(r *dto.UsageQueryRequest) {
		r.GroupBy = []string{"meter_id"}
		r.Columns = "properties.user"
		r.Metrics = []dto.UsageQueryMetric{{Type: "distinct", Key: "properties.user"}}
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
	s.Equal(dto.UsageQueryValue{Count: 1, Values: []string{"alice"}}, row.Metrics["distinct:properties.user"])
}

func (s *UsageQuerySuite) TestWeeklyPointsListEveryWindow() {
	resp, err := s.query(func(r *dto.UsageQueryRequest) {
		r.MeterIDs = []string{"m_tokens"}
		r.WindowSize = types.WindowSizeWeek
	})
	s.NoError(err)

	type point struct{ start, amount string }
	points := func(ps []dto.UsagePoint) []point {
		return lo.Map(ps, func(p dto.UsagePoint, _ int) point { return point{p.Timestamp.Format(time.DateOnly), p.Amount} })
	}
	// Weeks start on Sunday; Oct 1 2026 is a Thursday, so the first window starts Sep 27.
	byUser := lo.SliceToMap(resp.Items, func(i dto.UsageQueryItem) (string, []point) { return i.Group["properties.user"], points(i.Points) })
	s.Equal([]point{{"2026-09-27", "0.00"}, {"2026-10-04", "2000.00"}, {"2026-10-11", "0.00"}, {"2026-10-18", "5000.00"}, {"2026-10-25", "0.00"}}, byUser["alice"])
	s.Equal([]point{{"2026-09-27", "0.00"}, {"2026-10-04", "0.00"}, {"2026-10-11", "6000.00"}, {"2026-10-18", "0.00"}, {"2026-10-25", "0.00"}}, byUser["bob"])
	s.Equal([]point{{"2026-09-27", "0.00"}, {"2026-10-04", "2000.00"}, {"2026-10-11", "6000.00"}, {"2026-10-18", "5000.00"}, {"2026-10-25", "0.00"}}, points(resp.Totals.Points))
}

func (s *UsageQuerySuite) TestRejectedRequests() {
	tests := []struct {
		name   string
		mutate func(r *dto.UsageQueryRequest)
	}{
		{"usage sort across meters", func(r *dto.UsageQueryRequest) { r.Sort = &dto.UsageQuerySort{Field: "usage"} }},
		{"unknown metric", func(r *dto.UsageQueryRequest) {
			r.Metrics = []dto.UsageQueryMetric{{Type: "percentile", Key: "properties.user"}}
		}},
		{"unknown key", func(r *dto.UsageQueryRequest) { r.GroupBy = []string{"customer"} }},
		{"only start_time", func(r *dto.UsageQueryRequest) { r.EndTime = nil }},
		{"unknown timezone", func(r *dto.UsageQueryRequest) { r.Timezone = "Mars/Base" }},
		{"unknown expand", func(r *dto.UsageQueryRequest) { r.Expand = []dto.UsageExpand{"wallet"} }},
		{"line item columns", func(r *dto.UsageQueryRequest) { r.Columns = "sub_line_item_id" }},
		{"hourly windows", func(r *dto.UsageQueryRequest) { r.WindowSize = types.WindowSizeHour }},
		{"more than 400 daily windows", func(r *dto.UsageQueryRequest) {
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

func (s *UsageQuerySuite) TestFilterOnMissingValueAndSingleMeterUsageSort() {
	resp, err := s.query(func(r *dto.UsageQueryRequest) {
		r.MeterIDs = []string{"m_calls"}
		r.Filters = []dto.UsageQueryFilter{{Key: "properties.user", Op: "in", Values: []string{dto.UsageNoneValue}}}
		r.Sort = &dto.UsageQuerySort{Field: "usage"}
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
