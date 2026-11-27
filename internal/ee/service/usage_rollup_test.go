package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/addon"
	"github.com/flexprice/flexprice/internal/domain/addonassociation"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/meterusagecheckpoint"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/usagerollup"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type UsageRollupSuite struct {
	testutil.BaseServiceTestSuite
	svc         *usageRollupService
	periodStart time.Time
	periodEnd   time.Time
}

func TestUsageRollup(t *testing.T) {
	suite.Run(t, new(UsageRollupSuite))
}

func (s *UsageRollupSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	st := s.GetStores()
	cfg := s.GetConfig()
	cfg.UsageRollup = config.UsageRollupConfig{Enabled: true, EnabledTenants: []string{types.DefaultTenantID}}
	s.svc = &usageRollupService{ServiceParams: ServiceParams{
		Logger:                   s.GetLogger(),
		Config:                   cfg,
		DB:                       s.GetDB(),
		RedisCache:               s.GetRedisCache(),
		WalletRepo:               st.WalletRepo,
		SubRepo:                  st.SubscriptionRepo,
		CommitmentRepo:           st.CommitmentRepo,
		SubscriptionLineItemRepo: st.SubscriptionLineItemRepo,
		PlanRepo:                 st.PlanRepo,
		PriceRepo:                st.PriceRepo,
		PriceUnitRepo:            st.PriceUnitRepo,
		MeterRepo:                st.MeterRepo,
		MeterUsageRepo:           st.MeterUsageRepo,
		MeterUsageCheckpointRepo: st.MeterUsageCheckpointRepo,
		CustomerRepo:             st.CustomerRepo,
		InvoiceRepo:              st.InvoiceRepo,
		InvoiceLineItemRepo:      st.InvoiceLineItemRepo,
		EntitlementRepo:          st.EntitlementRepo,
		EntitlementGrantRepo:     st.EntitlementGrantRepo,
		FeatureRepo:              st.FeatureRepo,
		AddonRepo:                st.AddonRepo,
		AddonAssociationRepo:     st.AddonAssociationRepo,
		SettingsRepo:             st.SettingsRepo,
		CouponRepo:               st.CouponRepo,
		CouponAssociationRepo:    st.CouponAssociationRepo,
		CouponApplicationRepo:    st.CouponApplicationRepo,
		EventPublisher:           s.GetPublisher(),
		WebhookPublisher:         s.GetWebhookPublisher(),
		HourlyUsageRepo:          &fakeHourlyUsageRepository{hourly: map[time.Time][]usagerollup.HourlyUsage{}},
	}}
	s.periodStart = time.Now().UTC().Truncate(time.Hour).AddDate(0, 0, -10)
	s.periodEnd = s.periodStart.AddDate(0, 1, 0)
}

// seed creates customer cus_rt on plan_rt with one SUM meter priced per unit, and returns the subscription.
// change edits the seeded rows before they are saved.
func (s *UsageRollupSuite) seed(change func(sub *subscription.Subscription, m *meter.Meter, p *price.Price, li *subscription.SubscriptionLineItem)) *subscription.Subscription {
	ctx, st := s.GetContext(), s.GetStores()
	before := s.periodStart.AddDate(0, 0, -5)
	base := func() types.BaseModel {
		b := types.GetDefaultBaseModel(ctx)
		b.CreatedAt, b.UpdatedAt = before, before
		return b
	}
	cust := &customer.Customer{ID: "cus_rt", ExternalID: "ext_rt", EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: base()}
	m := &meter.Meter{ID: "mtr_rt", EventName: "api_call", Aggregation: meter.Aggregation{Type: types.AggregationSum, Field: "qty"},
		EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: base()}
	p := &price.Price{ID: "price_rt", Amount: decimal.NewFromInt(2), Currency: "usd", Type: types.PRICE_TYPE_USAGE,
		BillingModel: types.BILLING_MODEL_FLAT_FEE, BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		MeterID: m.ID, EntityType: types.PRICE_ENTITY_TYPE_PLAN, EntityID: "plan_rt",
		EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: base()}
	sub := &subscription.Subscription{ID: "subs_rt", CustomerID: cust.ID, PlanID: "plan_rt", Currency: "usd",
		SubscriptionType: types.SubscriptionTypeStandalone, SubscriptionStatus: types.SubscriptionStatusActive,
		StartDate: s.periodStart, CurrentPeriodStart: s.periodStart, CurrentPeriodEnd: s.periodEnd,
		BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: base()}
	li := &subscription.SubscriptionLineItem{ID: "subli_rt", SubscriptionID: sub.ID, CustomerID: cust.ID,
		EntityID: "plan_rt", EntityType: types.SubscriptionLineItemEntityTypePlan, PriceID: p.ID,
		PriceType: types.PRICE_TYPE_USAGE, MeterID: m.ID, Currency: "usd", BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		StartDate: s.periodStart, EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: base()}
	if change != nil {
		change(sub, m, p, li)
	}
	s.Require().NoError(st.CustomerRepo.Create(ctx, cust))
	s.Require().NoError(st.MeterRepo.CreateMeter(ctx, m))
	s.Require().NoError(st.PriceRepo.Create(ctx, p))
	s.Require().NoError(st.SubscriptionRepo.Create(ctx, sub))
	s.Require().NoError(st.SubscriptionLineItemRepo.Create(ctx, li))
	return sub
}

func (s *UsageRollupSuite) reason(sub *subscription.Subscription) usageRollupIneligibleReason {
	reason, err := s.svc.ineligibleReason(s.GetContext(), sub.CustomerID)
	s.Require().NoError(err)
	return reason
}

func (s *UsageRollupSuite) TestEligibleCustomer() {
	s.Empty(s.reason(s.seed(nil)))
}

// UsagePrices charges the plan's usage prices for the subscription's currency and billing period only.
func (s *UsageRollupSuite) TestUsagePricesMatchTheSubscription() {
	ctx := s.GetContext()
	sub := s.seed(nil)
	eur := &price.Price{ID: "price_rt_eur", Amount: decimal.NewFromInt(3), Currency: "eur", Type: types.PRICE_TYPE_USAGE,
		BillingModel: types.BILLING_MODEL_FLAT_FEE, BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		MeterID: "mtr_rt", EntityType: types.PRICE_ENTITY_TYPE_PLAN, EntityID: "plan_rt",
		EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx)}
	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, eur))

	prices, meters, err := s.svc.UsagePrices(ctx, sub)
	s.Require().NoError(err)
	s.Require().Contains(prices, "mtr_rt")
	s.Equal("price_rt", prices["mtr_rt"].ID)
	s.Equal(types.AggregationSum, meters["mtr_rt"].Aggregation.Type)
}

// An addon associated with the subscription has its usage prices charged next to the plan's.
func (s *UsageRollupSuite) TestUsagePricesIncludeActiveAddons() {
	ctx, st := s.GetContext(), s.GetStores()
	sub := s.seed(nil)
	base := types.GetDefaultBaseModel(ctx)
	s.Require().NoError(st.AddonRepo.Create(ctx, &addon.Addon{ID: "addon_rt", Name: "Storage", LookupKey: "storage",
		EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: base}))
	s.Require().NoError(st.MeterRepo.CreateMeter(ctx, &meter.Meter{ID: "mtr_rt_storage", EventName: "storage",
		Aggregation: meter.Aggregation{Type: types.AggregationSum, Field: "gb"}, EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: base}))
	s.Require().NoError(st.PriceRepo.Create(ctx, &price.Price{ID: "price_rt_storage", Amount: decimal.NewFromInt(1), Currency: "usd",
		Type: types.PRICE_TYPE_USAGE, BillingModel: types.BILLING_MODEL_FLAT_FEE, BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1, MeterID: "mtr_rt_storage", EntityType: types.PRICE_ENTITY_TYPE_ADDON, EntityID: "addon_rt",
		EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: base}))
	s.Require().NoError(st.AddonAssociationRepo.Create(ctx, &addonassociation.AddonAssociation{ID: "aa_rt", EntityID: sub.ID,
		EntityType: types.AddonAssociationEntityTypeSubscription, AddonID: "addon_rt", AddonStatus: types.AddonStatusActive,
		StartDate: lo.ToPtr(s.periodStart), EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: base}))

	prices, _, err := s.svc.UsagePrices(ctx, sub)
	s.Require().NoError(err)
	s.Equal("price_rt", prices["mtr_rt"].ID)
	s.Require().Contains(prices, "mtr_rt_storage")
	s.Equal("price_rt_storage", prices["mtr_rt_storage"].ID)
}

// Redis stores the cached prices, snapshot and subscription as JSON, so what the read path uses must survive the round trip.
func (s *UsageRollupSuite) TestCachedEntriesJSONRoundTrip() {
	ctx := s.GetContext()
	sub := s.seed(nil)
	entityPrices, err := s.svc.cachedEntityUsagePrices(ctx, "plan_rt")
	s.Require().NoError(err)
	entityPrices.Prices[0].Tiers = price.JSONBTiers{{UpTo: lo.ToPtr(uint64(10)), UnitAmount: decimal.NewFromInt(1)}}
	raw, err := json.Marshal(entityPrices)
	s.Require().NoError(err)
	var decoded usageRollupEntityPrices
	s.Require().NoError(json.Unmarshal(raw, &decoded))
	s.Require().Len(decoded.Prices, 1)
	got, want := decoded.Prices[0], entityPrices.Prices[0]
	s.Equal(want.ID, got.ID)
	s.Equal(want.Amount.String(), got.Amount.String())
	s.Equal(want.BillingModel, got.BillingModel)
	s.Equal(want.MeterID, got.MeterID)
	s.Equal(want.Currency, got.Currency)
	s.Equal(want.Tiers[0].UnitAmount.String(), got.Tiers[0].UnitAmount.String())
	s.Require().Contains(decoded.Meters, "mtr_rt")
	s.Equal(types.AggregationSum, decoded.Meters["mtr_rt"].Aggregation.Type)

	snapshot := &usageSnapshot{AggregatedUntil: s.periodStart, Totals: map[string]decimal.Decimal{"mtr_rt": decimal.RequireFromString("12.5")}}
	raw, err = json.Marshal(snapshot)
	s.Require().NoError(err)
	var decodedSnapshot usageSnapshot
	s.Require().NoError(json.Unmarshal(raw, &decodedSnapshot))
	s.True(s.periodStart.Equal(decodedSnapshot.AggregatedUntil))
	s.Equal("12.5", decodedSnapshot.Totals["mtr_rt"].String())

	fetched, err := s.svc.cachedCustomerAndAddons(ctx, sub)
	s.Require().NoError(err)
	raw, err = json.Marshal(fetched)
	s.Require().NoError(err)
	var decodedCustomerAndAddons customerAndAddons
	s.Require().NoError(json.Unmarshal(raw, &decodedCustomerAndAddons))
	s.Equal("ext_rt", decodedCustomerAndAddons.ExternalCustomerID)
}

func (s *UsageRollupSuite) TestIneligibleReasons() {
	tests := []struct {
		name   string
		change func(sub *subscription.Subscription, m *meter.Meter, p *price.Price, li *subscription.SubscriptionLineItem)
		want   usageRollupIneligibleReason
	}{
		{"parent subscription", func(sub *subscription.Subscription, _ *meter.Meter, _ *price.Price, _ *subscription.SubscriptionLineItem) {
			sub.SubscriptionType = types.SubscriptionTypeParent
		}, usageRollupIneligibleNotStandalone},
		{"subscription commitment", func(sub *subscription.Subscription, _ *meter.Meter, _ *price.Price, _ *subscription.SubscriptionLineItem) {
			sub.CommitmentAmount, sub.OverageFactor = lo.ToPtr(decimal.NewFromInt(100)), lo.ToPtr(decimal.NewFromFloat(1.5))
		}, usageRollupIneligibleCommitment},
		{"line item added to the subscription", func(_ *subscription.Subscription, _ *meter.Meter, _ *price.Price, li *subscription.SubscriptionLineItem) {
			li.EntityType = types.SubscriptionLineItemEntityTypeSubscription
		}, usageRollupIneligibleOverride},
		{"override price", func(_ *subscription.Subscription, _ *meter.Meter, p *price.Price, li *subscription.SubscriptionLineItem) {
			p.EntityType, p.EntityID, p.ParentPriceID = types.PRICE_ENTITY_TYPE_SUBSCRIPTION, "subs_rt", "price_plan"
			li.EntityID = "subs_rt"
		}, usageRollupIneligibleOverride},
		{"plan price edited this period", func(_ *subscription.Subscription, _ *meter.Meter, p *price.Price, _ *subscription.SubscriptionLineItem) {
			p.UpdatedAt = time.Now().UTC()
		}, usageRollupIneligiblePriceChanged},
		{"plan price added in an earlier period", func(sub *subscription.Subscription, _ *meter.Meter, p *price.Price, _ *subscription.SubscriptionLineItem) {
			sub.StartDate = s.periodStart.AddDate(0, -2, 0)
			p.CreatedAt = s.periodStart.AddDate(0, -1, 0)
		}, usageRollupIneligiblePriceChanged},
		{"line item started mid-period", func(_ *subscription.Subscription, _ *meter.Meter, _ *price.Price, li *subscription.SubscriptionLineItem) {
			li.StartDate = s.periodStart.AddDate(0, 0, 3)
		}, usageRollupIneligiblePriceChanged},
		{"unsupported meter", func(_ *subscription.Subscription, m *meter.Meter, _ *price.Price, _ *subscription.SubscriptionLineItem) {
			m.Aggregation.Type = types.AggregationCountUnique
		}, usageRollupIneligibleMeter},
		{"bucketed price", func(_ *subscription.Subscription, m *meter.Meter, p *price.Price, _ *subscription.SubscriptionLineItem) {
			m.Aggregation.Type = types.AggregationMax
			p.BucketSize = types.WindowSizeHour
		}, usageRollupIneligibleMeter},
		{"custom price unit", func(_ *subscription.Subscription, _ *meter.Meter, _ *price.Price, li *subscription.SubscriptionLineItem) {
			li.PriceUnit = lo.ToPtr("credits")
		}, usageRollupIneligiblePriceUnit},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			s.Equal(tt.want, s.reason(s.seed(tt.change)))
		})
	}
}

// A plan price with no line item on the subscription (the plan price sync never ran) would be charged
// by the usage rollup path but not by invoices, so the customer is ineligible.
func (s *UsageRollupSuite) TestPlanPriceNotOnTheSubscription() {
	ctx, st := s.GetContext(), s.GetStores()
	sub := s.seed(nil)
	base := types.GetDefaultBaseModel(ctx)
	base.CreatedAt, base.UpdatedAt = s.periodStart.AddDate(0, 0, -5), s.periodStart.AddDate(0, 0, -5)
	s.Require().NoError(st.MeterRepo.CreateMeter(ctx, &meter.Meter{ID: "mtr_rt_storage", EventName: "storage",
		Aggregation: meter.Aggregation{Type: types.AggregationSum, Field: "gb"}, EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: base}))
	s.Require().NoError(st.PriceRepo.Create(ctx, &price.Price{ID: "price_rt_storage", Amount: decimal.NewFromInt(1), Currency: "usd",
		Type: types.PRICE_TYPE_USAGE, BillingModel: types.BILLING_MODEL_FLAT_FEE, BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1, MeterID: "mtr_rt_storage", EntityType: types.PRICE_ENTITY_TYPE_PLAN, EntityID: "plan_rt",
		EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: base}))

	s.Equal(usageRollupIneligiblePriceChanged, s.reason(sub))
}

// A plan entitlement on a priced meter makes the plan's customers ineligible, and removing it makes them
// eligible again.
func (s *UsageRollupSuite) TestPlanEntitlementChangeClearsEligibility() {
	ctx, st := s.GetContext(), s.GetStores()
	sub := s.seed(nil)
	s.clearEligibility(UsageRollupSubscription, sub.ID)
	s.Require().True(s.eligible(sub))
	s.Require().NoError(st.FeatureRepo.Create(ctx, &feature.Feature{ID: "feat_rt", Name: "API calls", LookupKey: "api_calls",
		MeterID: "mtr_rt", Type: types.FeatureTypeMetered, EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx)}))
	ent, err := st.EntitlementRepo.Create(ctx, &entitlement.Entitlement{ID: "ent_rt", EntityType: types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		EntityID: "plan_rt", FeatureID: "feat_rt", FeatureType: types.FeatureTypeMetered, IsEnabled: true,
		EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx)})
	s.Require().NoError(err)

	(&entitlementService{ServiceParams: s.svc.ServiceParams}).markEntitlementSourceChanged(ctx, ent.ID)
	s.False(s.eligible(sub), "the cleared flag is evaluated again and finds the entitlement")

	s.Require().NoError(st.EntitlementRepo.Delete(ctx, ent.ID))
	s.clearEligibility(UsageRollupPlan, "plan_rt")
	s.True(s.eligible(sub), "with the entitlement gone the customer is eligible again")
}

// A commitment in the current period keeps the customer ineligible, including one terminated mid-period: billing
// applies it up to its termination.
func (s *UsageRollupSuite) TestCommitmentsInThePeriodDecideEligibility() {
	tests := []struct {
		name   string
		change func(c *commitment.Commitment)
		want   usageRollupIneligibleReason
	}{
		{name: "active", want: usageRollupIneligibleCommitment},
		{name: "terminated mid-period", change: func(c *commitment.Commitment) {
			c.CommitmentStatus, c.TerminatedAt = types.CommitmentStatusTerminated, lo.ToPtr(s.periodStart.AddDate(0, 0, 5))
		}, want: usageRollupIneligibleCommitment},
		{name: "terminated before the period", change: func(c *commitment.Commitment) {
			c.CommitmentStatus, c.TerminatedAt = types.CommitmentStatusTerminated, lo.ToPtr(s.periodStart.AddDate(0, 0, -1))
		}},
		{name: "ended before the period", change: func(c *commitment.Commitment) {
			c.EndDate = lo.ToPtr(s.periodStart.AddDate(0, 0, -1))
		}},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			ctx := s.GetContext()
			sub := s.seed(nil)
			c := &commitment.Commitment{ID: "cmt_rt", SubscriptionID: sub.ID, CustomerID: sub.CustomerID,
				StartDate: s.periodStart.AddDate(0, -1, 0), CommitmentStatus: types.CommitmentStatusActive,
				EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx)}
			if tt.change != nil {
				tt.change(c)
			}
			_, err := s.GetStores().CommitmentRepo.Create(ctx, c)
			s.Require().NoError(err)
			s.Equal(tt.want, s.reason(sub))
		})
	}
}

// Grants billing applies to the current period keep the customer ineligible after their entitlement is disabled
// or deleted mid-period.
func (s *UsageRollupSuite) TestPeriodGrantsDecideEligibility() {
	tests := []struct {
		name       string
		scope      types.EntitlementGrantScopeEntityType
		featureID  string
		validTo    func() time.Time
		noEntitled bool
		want       usageRollupIneligibleReason
	}{
		{name: "feature grant on the charged meter, entitlement disabled", scope: types.EntitlementGrantScopeFeature,
			featureID: "feat_rt", want: usageRollupIneligibleEntitlement},
		{name: "subscription grant, entitlement disabled", scope: types.EntitlementGrantScopeSubscription,
			want: usageRollupIneligibleEntitlement},
		{name: "feature grant whose entitlement was deleted", scope: types.EntitlementGrantScopeFeature,
			featureID: "feat_rt", noEntitled: true, want: usageRollupIneligibleEntitlement},
		{name: "feature grant on a meter the plan doesn't charge", scope: types.EntitlementGrantScopeFeature,
			featureID: "feat_other"},
		{name: "grant that ended before the period", scope: types.EntitlementGrantScopeFeature, featureID: "feat_rt",
			validTo: func() time.Time { return s.periodStart.AddDate(0, 0, -1) }},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			ctx, st := s.GetContext(), s.GetStores()
			sub := s.seed(nil)
			for id, meterID := range map[string]string{"feat_rt": "mtr_rt", "feat_other": "mtr_other"} {
				s.Require().NoError(st.FeatureRepo.Create(ctx, &feature.Feature{ID: id, Name: id, LookupKey: id, MeterID: meterID,
					Type: types.FeatureTypeMetered, EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx)}))
			}
			if !tt.noEntitled {
				_, err := st.EntitlementRepo.Create(ctx, &entitlement.Entitlement{ID: "ent_grant", EntityType: types.ENTITLEMENT_ENTITY_TYPE_PLAN,
					EntityID: "plan_rt", FeatureID: tt.featureID, FeatureType: types.FeatureTypeMetered, GrantScope: tt.scope,
					EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx)})
				s.Require().NoError(err)
			}
			validTo := s.periodEnd
			if tt.validTo != nil {
				validTo = tt.validTo()
			}
			_, err := st.EntitlementGrantRepo.Create(ctx, &entitlementgrant.EntitlementGrant{ID: "grant_rt", EntitlementConfigID: "ent_grant",
				CustomerID: sub.CustomerID, SubscriptionID: sub.ID, ScopeEntityType: tt.scope,
				ValidFrom: s.periodStart.AddDate(0, -1, 0), ValidTo: validTo,
				EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx)})
			s.Require().NoError(err)
			s.Equal(tt.want, s.reason(sub))
		})
	}
}

// Enabled entitlements on the plan, addons or subscription decide eligibility before any grant exists.
func (s *UsageRollupSuite) TestEntitlementsDecideEligibility() {
	tests := []struct {
		name        string
		entitlement func(sub *subscription.Subscription) *entitlement.Entitlement
		want        usageRollupIneligibleReason
	}{
		{
			name: "subscription grant on the subscription",
			entitlement: func(sub *subscription.Subscription) *entitlement.Entitlement {
				return &entitlement.Entitlement{EntityType: types.ENTITLEMENT_ENTITY_TYPE_SUBSCRIPTION, EntityID: sub.ID,
					IsEnabled: true, GrantScope: types.EntitlementGrantScopeSubscription}
			},
			want: usageRollupIneligibleEntitlement,
		},
		{
			name: "metered feature on a meter the plan doesn't charge",
			entitlement: func(*subscription.Subscription) *entitlement.Entitlement {
				return &entitlement.Entitlement{EntityType: types.ENTITLEMENT_ENTITY_TYPE_PLAN, EntityID: "plan_rt",
					FeatureID: "feat_other", FeatureType: types.FeatureTypeMetered, IsEnabled: true}
			},
		},
		{
			name: "disabled metered feature on the charged meter",
			entitlement: func(*subscription.Subscription) *entitlement.Entitlement {
				return &entitlement.Entitlement{EntityType: types.ENTITLEMENT_ENTITY_TYPE_PLAN, EntityID: "plan_rt",
					FeatureID: "feat_rt", FeatureType: types.FeatureTypeMetered}
			},
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.SetupTest()
			ctx, st := s.GetContext(), s.GetStores()
			sub := s.seed(nil)
			for id, meterID := range map[string]string{"feat_rt": "mtr_rt", "feat_other": "mtr_other"} {
				s.Require().NoError(st.FeatureRepo.Create(ctx, &feature.Feature{ID: id, Name: id, LookupKey: id, MeterID: meterID,
					Type: types.FeatureTypeMetered, EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx)}))
			}
			e := tt.entitlement(sub)
			e.ID, e.EnvironmentID, e.BaseModel = "ent_case", types.GetEnvironmentID(ctx), types.GetDefaultBaseModel(ctx)
			_, err := st.EntitlementRepo.Create(ctx, e)
			s.Require().NoError(err)
			s.Equal(tt.want, s.reason(sub))
		})
	}
}

// A read takes the flag from the cache, then Postgres, and evaluates it once a write has cleared it.
func (s *UsageRollupSuite) TestEligibilityReadsCacheThenPostgresThenEvaluates() {
	ctx, customers := s.GetContext(), s.GetStores().CustomerRepo
	sub := s.seed(nil)
	key := usageRollupEligibilityKey(ctx, sub.CustomerID)

	s.True(s.eligible(sub), "never evaluated: the read evaluates it")
	saved, err := customers.GetUsageRollupEligible(ctx, sub.CustomerID)
	s.Require().NoError(err)
	s.Equal(lo.ToPtr(true), saved, "the evaluation is saved in Postgres")
	cached, found := s.svc.RedisCache.ForceCacheGet(ctx, key)
	s.Require().True(found, "and cached")
	s.True(*lo.Must(cache.UnmarshalCacheValue[bool](cached)))

	s.Require().NoError(customers.SetUsageRollupEligible(ctx, sub.CustomerID, false))
	s.True(s.eligible(sub), "the cache is read first")
	s.svc.RedisCache.ForceCacheDelete(ctx, key)
	s.False(s.eligible(sub), "on a cache miss Postgres is read")

	s.svc.ClearEligibility(ctx, UsageRollupSubscription, sub.ID)
	saved, err = customers.GetUsageRollupEligible(ctx, sub.CustomerID)
	s.Require().NoError(err)
	s.Nil(saved, "a write clears the flag")
	s.True(s.eligible(sub), "and the next read evaluates it again")
}

func (s *UsageRollupSuite) TestClearEligibilitySkipsTenantsWithoutUsageRollup() {
	ctx, customers := s.GetContext(), s.GetStores().CustomerRepo
	sub := s.seed(nil)
	s.Require().NoError(customers.SetUsageRollupEligible(ctx, sub.CustomerID, false))
	s.svc.Config.UsageRollup = config.UsageRollupConfig{}

	s.svc.ClearEligibility(ctx, UsageRollupSubscription, sub.ID)
	saved, err := customers.GetUsageRollupEligible(ctx, sub.CustomerID)
	s.Require().NoError(err)
	s.Equal(lo.ToPtr(false), saved)
}

// clearEligibility clears the entity's customers' flags, as a write to it does.
func (s *UsageRollupSuite) clearEligibility(entity UsageRollupEntity, entityID string) {
	s.svc.ClearEligibility(s.GetContext(), entity, entityID)
}

func (s *UsageRollupSuite) eligible(sub *subscription.Subscription) bool {
	return s.svc.IsUsageRollupEligible(s.GetContext(), sub, types.UsageSourceWallet)
}

// periodQuantitiesWithCheckpointAt seeds a checkpoint total of 10 up to aggregatedUntil, 3 in ClickHouse and 5 in
// Redis for the hour before the cutoff, 4 in Redis at the cutoff and 1 in the current hour, and reads the period.
func (s *UsageRollupSuite) periodQuantitiesWithCheckpointAt(aggregatedUntil func(cutoff time.Time) time.Time) (first, cached decimal.Decimal) {
	ctx := s.GetContext()
	sub := s.seed(nil)
	s.clearEligibility(UsageRollupSubscription, sub.ID)

	now := time.Now().UTC()
	cutoff := usagerollup.Cutoff(now)
	_, err := s.svc.MeterUsageCheckpointRepo.UpdateCustomerCheckpoints(ctx, []*meterusagecheckpoint.CustomerCheckpointUpdate{{
		CustomerID: sub.CustomerID, To: aggregatedUntil(cutoff),
		Totals: []meterusagecheckpoint.PeriodTotal{{PeriodStart: s.periodStart, MeterID: "mtr_rt", AggregationType: types.AggregationSum, Quantity: decimal.NewFromInt(10)}},
	}})
	s.Require().NoError(err)
	s.Require().NoError(s.svc.MeterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{{
		Event: events.Event{ID: "evt_gap", TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx),
			ExternalCustomerID: "ext_rt", Timestamp: cutoff.Add(-30 * time.Minute)},
		MeterID: "mtr_rt", QtyTotal: decimal.NewFromInt(3),
	}}))
	usageRollup := s.svc.HourlyUsageRepo.(*fakeHourlyUsageRepository)
	usageRollup.hourly[cutoff.Add(-time.Hour)] = []usagerollup.HourlyUsage{{MeterID: "mtr_rt", Aggregation: types.AggregationSum, Quantity: decimal.NewFromInt(5)}}
	usageRollup.hourly[cutoff] = []usagerollup.HourlyUsage{{MeterID: "mtr_rt", Aggregation: types.AggregationSum, Quantity: decimal.NewFromInt(4)}}
	usageRollup.hourly[now.Truncate(time.Hour)] = []usagerollup.HourlyUsage{{MeterID: "mtr_rt", Aggregation: types.AggregationSum, Quantity: decimal.NewFromInt(1)}}

	s.Require().True(s.eligible(sub))
	_, meters, err := s.svc.UsagePrices(ctx, sub)
	s.Require().NoError(err)
	quantities, err := s.svc.PeriodQuantities(ctx, sub, meters)
	s.Require().NoError(err)
	cachedQuantities, err := s.svc.PeriodQuantities(ctx, sub, meters)
	s.Require().NoError(err)
	return quantities["mtr_rt"], cachedQuantities["mtr_rt"]
}

func (s *UsageRollupSuite) TestPeriodQuantitiesReadsUncheckpointedHoursFromRedis() {
	first, cached := s.periodQuantitiesWithCheckpointAt(func(cutoff time.Time) time.Time { return cutoff.Add(-time.Hour) })
	s.Equal("20", first.String(), "10 + 5 (Redis, not ClickHouse's 3) + 4 + 1")
	s.Equal("20", cached.String(), "the second read takes the checkpoint from the snapshot cache")
}

func (s *UsageRollupSuite) TestPeriodQuantitiesReadsClickHouseBeforeOldestRedisHour() {
	first, _ := s.periodQuantitiesWithCheckpointAt(func(time.Time) time.Time {
		return usagerollup.OldestRedisHour(time.Now()).Add(-time.Hour)
	})
	s.Equal("18", first.String(), "10 + 3 (ClickHouse; hours this old may have expired in Redis) + 4 + 1")
}

func (s *UsageRollupSuite) TestIsUsageRollupEligible() {
	ctx := s.GetContext()
	sub := s.seed(nil)
	s.clearEligibility(UsageRollupSubscription, sub.ID)

	s.True(s.eligible(sub))
	s.False(s.svc.IsUsageRollupEligible(ctx, sub, types.UsageSourceInvoiceCreation), "only wallet reads")

	p, err := s.GetStores().PriceRepo.Get(ctx, "price_rt")
	s.Require().NoError(err)
	p.UpdatedAt = time.Now().UTC()
	s.Require().NoError(s.GetStores().PriceRepo.Update(ctx, p, false))
	s.clearEligibility(UsageRollupPlan, "plan_rt")
	s.False(s.eligible(sub), "clearing the plan finds the price change on the next read")
	s.clearEligibility(UsageRollupSubscription, sub.ID)
	s.False(s.eligible(sub), "a price change keeps the customer ineligible")
}

// On the usage rollup path a charge is price × quantity. sub carries no line items, so the full path would
// price nothing: a non-zero total shows the usage rollup branch ran.
func (s *UsageRollupSuite) TestCalculateMeterUsageChargesOnTheUsageRollupPath() {
	ctx := s.GetContext()
	sub := s.seed(nil)
	s.clearEligibility(UsageRollupSubscription, sub.ID)
	p, err := s.GetStores().PriceRepo.Get(ctx, "price_rt")
	s.Require().NoError(err)
	usage := &dto.GetUsageBySubscriptionResponse{Charges: []*dto.SubscriptionUsageByMetersResponse{{
		Price: p, SubscriptionLineItemID: "subli_rt", Quantity: 5,
	}}}
	billing := NewBillingService(s.svc.ServiceParams)

	lines, total, err := billing.CalculateMeterUsageCharges(ctx, sub, usage, s.periodStart, s.periodEnd, types.UsageSourceWallet, nil)
	s.Require().NoError(err)
	s.Equal("10", total.String(), "5 units at 2")
	s.Require().Len(lines, 1)
	s.Equal("subli_rt", lo.FromPtr(lines[0].SubscriptionLineItemID))

	_, total, err = billing.CalculateMeterUsageCharges(ctx, sub, usage, s.periodStart, s.periodEnd, types.UsageSourceInvoiceCreation, nil)
	s.Require().NoError(err)
	s.True(total.IsZero(), "invoice pricing takes the full path")
}

func TestPlanUsageReads(t *testing.T) {
	at := func(hour, minute int) time.Time { return time.Date(2026, 11, 18, hour, minute, 0, 0, time.UTC) }
	cutoff := at(10, 0)
	tests := []struct {
		name           string
		from, end      time.Time
		ongoing        bool
		wantClickhouse []usageWindow
		wantHours      []time.Time
	}{
		{"steady state reads only Redis", at(10, 0), at(13, 25), true, nil, []time.Time{at(10, 0), at(11, 0), at(12, 0), at(13, 0)}},
		{"job behind: ClickHouse up to the cutoff", at(8, 0), at(13, 25), true,
			[]usageWindow{{at(8, 0), at(10, 0)}}, []time.Time{at(10, 0), at(11, 0), at(12, 0), at(13, 0)}},
		{"period started mid-hour inside Redis: that partial hour from ClickHouse", at(11, 30), at(13, 25), true,
			[]usageWindow{{at(11, 30), at(12, 0)}}, []time.Time{at(12, 0), at(13, 0)}},
		{"period ended mid-hour: the partial last hour from ClickHouse", at(10, 0), at(12, 40), false,
			[]usageWindow{{at(12, 0), at(12, 40)}}, []time.Time{at(10, 0), at(11, 0)}},
		{"checkpointed to the enable time after the cutoff: Redis from there, no ClickHouse", at(12, 0), at(13, 25), true,
			nil, []time.Time{at(12, 0), at(13, 0)}},
		{"period entirely before the cutoff", at(5, 0), at(7, 30), false, []usageWindow{{at(5, 0), at(7, 30)}}, nil},
		{"empty range", at(10, 0), at(10, 0), true, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clickhouse, hours := planUsageReads(tt.from, tt.end, cutoff, tt.ongoing)
			require.Equal(t, tt.wantClickhouse, clickhouse)
			require.Equal(t, tt.wantHours, hours)
		})
	}
}

// Money parity: the same usage priced by the full compute (usage rollup reads off) and by the usage rollup gives the
// same wallet balance.
func (s *UsageRollupSuite) TestWalletBalanceMatchesFullCompute() {
	ctx := s.GetContext()
	sub := s.seed(nil)
	s.clearEligibility(UsageRollupSubscription, sub.ID)
	w := &wallet.Wallet{ID: "wallet_rt", CustomerID: sub.CustomerID, Currency: "usd", WalletType: types.WalletTypePrePaid,
		Balance: decimal.NewFromInt(1000), CreditBalance: decimal.NewFromInt(1000), ConversionRate: decimal.NewFromInt(1),
		TopupConversionRate: decimal.NewFromInt(1), WalletStatus: types.WalletStatusActive, BaseModel: types.GetDefaultBaseModel(ctx)}
	s.Require().NoError(s.GetStores().WalletRepo.CreateWallet(ctx, w))

	now := time.Now().UTC()
	recent := now.Add(-time.Hour)
	usage := func(id string, ts time.Time, qty int64) *events.MeterUsage {
		return &events.MeterUsage{Event: events.Event{ID: id, TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx),
			ExternalCustomerID: "ext_rt", Timestamp: ts}, MeterID: "mtr_rt", QtyTotal: decimal.NewFromInt(qty)}
	}
	s.Require().NoError(s.svc.MeterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{
		usage("evt_old", s.periodStart.Add(24*time.Hour), 5),
		usage("evt_recent", recent, 2),
	}))
	// The recent event is past the cutoff, so the usage rollup holds it in Redis and ClickHouse covers the rest.
	s.svc.HourlyUsageRepo.(*fakeHourlyUsageRepository).hourly[recent.Truncate(time.Hour)] = []usagerollup.HourlyUsage{
		{MeterID: "mtr_rt", Aggregation: types.AggregationSum, Quantity: decimal.NewFromInt(2)},
	}
	wallets := NewWalletService(s.svc.ServiceParams)

	s.svc.Config.UsageRollup.Enabled = false
	full, err := wallets.GetWalletBalanceV2(ctx, w.ID)
	s.Require().NoError(err)
	s.svc.Config.UsageRollup.Enabled = true
	fast, err := wallets.GetWalletBalanceV2(ctx, w.ID)
	s.Require().NoError(err)

	s.Equal("986", lo.FromPtr(full.RealTimeBalance).String(), "1000 - 7 units at 2")
	s.Equal(lo.FromPtr(full.RealTimeBalance).String(), lo.FromPtr(fast.RealTimeBalance).String())
	s.Equal(lo.FromPtr(full.CurrentPeriodUsage).String(), lo.FromPtr(fast.CurrentPeriodUsage).String())
	s.False(fast.IsCachedFallback)

	// A unit only Redis holds shows the wallet reads quantities from the usage rollup and invoicing doesn't.
	usageRollup := s.svc.HourlyUsageRepo.(*fakeHourlyUsageRepository)
	usageRollup.hourly[now.Truncate(time.Hour)] = []usagerollup.HourlyUsage{{MeterID: "mtr_rt", Aggregation: types.AggregationSum, Quantity: decimal.NewFromInt(1)}}
	fast, err = wallets.GetWalletBalanceV2(ctx, w.ID)
	s.Require().NoError(err)
	s.Equal("984", lo.FromPtr(fast.RealTimeBalance).String(), "1000 - 8 units at 2")
	s.Equal("7", s.lineItemUsage(sub.ID, types.UsageSourceInvoiceCreation).String())
	s.Equal("8", s.lineItemUsage(sub.ID, types.UsageSourceWallet).String())
}

// A shadow tenant's balance is served from the old path; the usage rollup balance is only compared.
func (s *UsageRollupSuite) TestShadowTenantServesOldPathAndCompares() {
	ctx := s.GetContext()
	sub := s.seed(nil)
	s.clearEligibility(UsageRollupSubscription, sub.ID)
	s.svc.Config.UsageRollup = config.UsageRollupConfig{Enabled: true, ShadowTenants: []string{types.DefaultTenantID}}
	w := s.prepaidWallet("wallet_shadow", sub.CustomerID)
	wallets := NewWalletService(s.svc.ServiceParams).(*walletService)

	recent := time.Now().UTC().Add(-time.Hour)
	s.Require().NoError(s.svc.MeterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{{
		Event: events.Event{ID: "evt_shadow", TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx),
			ExternalCustomerID: "ext_rt", Timestamp: recent},
		MeterID: "mtr_rt", QtyTotal: decimal.NewFromInt(2),
	}}))
	usageRollup := s.svc.HourlyUsageRepo.(*fakeHourlyUsageRepository)
	usageRollup.hourly[recent.Truncate(time.Hour)] = []usagerollup.HourlyUsage{{MeterID: "mtr_rt", Aggregation: types.AggregationSum, Quantity: decimal.NewFromInt(2)}}

	served, err := wallets.GetWalletBalanceV2(ctx, w.ID)
	s.Require().NoError(err)
	s.Equal("996", lo.FromPtr(served.RealTimeBalance).String(), "1000 - 2 units at 2")
	s.Equal(usageRollupShadowMatch, wallets.compareUsageRollupBalance(ctx, w, served))

	// A unit only Redis holds: the served balance stays on ClickHouse, the usage rollup balance differs.
	usageRollup.hourly[time.Now().UTC().Truncate(time.Hour)] = []usagerollup.HourlyUsage{{MeterID: "mtr_rt", Aggregation: types.AggregationSum, Quantity: decimal.NewFromInt(1)}}
	served, err = wallets.GetWalletBalanceV2(ctx, w.ID)
	s.Require().NoError(err)
	s.Equal("996", lo.FromPtr(served.RealTimeBalance).String(), "the usage rollup balance is never served")
	s.Equal(usageRollupShadowMismatch, wallets.compareUsageRollupBalance(ctx, w, served))

	p, err := s.GetStores().PriceRepo.Get(ctx, "price_rt")
	s.Require().NoError(err)
	p.UpdatedAt = time.Now().UTC()
	s.Require().NoError(s.GetStores().PriceRepo.Update(ctx, p, false))
	s.clearEligibility(UsageRollupPlan, "plan_rt")
	s.Equal(usageRollupShadowIneligible, wallets.compareUsageRollupBalance(ctx, w, served))

	s.svc.Config.UsageRollup.ShadowTenants = nil
	s.Empty(wallets.compareUsageRollupBalance(ctx, w, served), "not a shadow tenant")
}

func (s *UsageRollupSuite) prepaidWallet(id, customerID string) *wallet.Wallet {
	ctx := s.GetContext()
	w := &wallet.Wallet{ID: id, CustomerID: customerID, Currency: "usd", WalletType: types.WalletTypePrePaid,
		Balance: decimal.NewFromInt(1000), CreditBalance: decimal.NewFromInt(1000), ConversionRate: decimal.NewFromInt(1),
		TopupConversionRate: decimal.NewFromInt(1), WalletStatus: types.WalletStatusActive, BaseModel: types.GetDefaultBaseModel(ctx)}
	s.Require().NoError(s.GetStores().WalletRepo.CreateWallet(ctx, w))
	return w
}

// lineItemUsage returns the seeded line item's current-period usage as read for source.
func (s *UsageRollupSuite) lineItemUsage(subscriptionID string, source types.UsageSource) decimal.Decimal {
	ctx := s.GetContext()
	sub, err := s.GetStores().SubscriptionRepo.Get(ctx, subscriptionID)
	s.Require().NoError(err)
	usage, err := NewMeterUsageService(s.svc.ServiceParams).GetSubscriptionMeterUsageWithSub(ctx, sub, &GetSubscriptionMeterUsageRequest{
		SubscriptionID: sub.ID, StartTime: sub.CurrentPeriodStart, EndTime: sub.CurrentPeriodEnd, IncludeChildren: true, Source: source,
	})
	s.Require().NoError(err)
	s.Require().Len(usage.LineItemUsages, 1)
	return usage.LineItemUsages[0].Usage
}
