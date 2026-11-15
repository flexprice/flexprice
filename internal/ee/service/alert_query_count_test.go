package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/domain/alert"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// alertQueryCounter counts meter_usage reads.
type alertQueryCounter struct {
	events.MeterUsageRepository
	mu    sync.Mutex
	total int
	final int
	calls map[string]int
}

func newAlertQueryCounter(inner events.MeterUsageRepository) *alertQueryCounter {
	return &alertQueryCounter{MeterUsageRepository: inner, calls: map[string]int{}}
}

func (c *alertQueryCounter) record(name string, useFinal bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.total++
	if useFinal {
		c.final++
	}
	c.calls[name]++
}

func (c *alertQueryCounter) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.total, c.final = 0, 0
	c.calls = map[string]int{}
}

func (c *alertQueryCounter) GetUsage(ctx context.Context, p *events.MeterUsageQueryParams) (*events.MeterUsageAggregationResult, error) {
	c.record("GetUsage", p.UseFinal)
	return c.MeterUsageRepository.GetUsage(ctx, p)
}

func (c *alertQueryCounter) GetUsageMultiMeter(ctx context.Context, p *events.MeterUsageQueryParams) ([]*events.MeterUsageAggregationResult, error) {
	c.record("GetUsageMultiMeter", p.UseFinal)
	return c.MeterUsageRepository.GetUsageMultiMeter(ctx, p)
}

func (c *alertQueryCounter) GetDistinctMeterIDs(ctx context.Context, p *events.MeterUsageQueryParams) ([]string, error) {
	c.record("GetDistinctMeterIDs", p.UseFinal)
	return c.MeterUsageRepository.GetDistinctMeterIDs(ctx, p)
}

func (c *alertQueryCounter) GetUsageForBucketedMeters(ctx context.Context, p *events.MeterUsageQueryParams) (*events.AggregationResult, error) {
	c.record("GetUsageForBucketedMeters", p.UseFinal)
	return c.MeterUsageRepository.GetUsageForBucketedMeters(ctx, p)
}

func (c *alertQueryCounter) GetEarliestUsageTimestamp(ctx context.Context, p *events.MeterUsageQueryParams) (*time.Time, error) {
	c.record("GetEarliestUsageTimestamp", p.UseFinal)
	return c.MeterUsageRepository.GetEarliestUsageTimestamp(ctx, p)
}

func (c *alertQueryCounter) GetDetailedAnalytics(ctx context.Context, p *events.MeterUsageDetailedAnalyticsParams) ([]*events.MeterUsageDetailedResult, error) {
	c.record("GetDetailedAnalytics", p.UseFinal)
	return c.MeterUsageRepository.GetDetailedAnalytics(ctx, p)
}

// alertQueryFixture mirrors the load-test tail customer.
type alertQueryFixture struct {
	cust    *customer.Customer
	sub     *subscription.Subscription
	counter *alertQueryCounter
	params  ServiceParams
}

func (s *EntitlementGrantSuite) newAlertQueryFixture(sumMeters, countMeters int) alertQueryFixture {
	ctx := s.GetContext()
	counter := newAlertQueryCounter(s.GetStores().MeterUsageRepo)

	params := s.buildServiceParams()
	stores := s.GetStores()
	params.MeterUsageRepo = counter
	params.InvoiceRepo = stores.InvoiceRepo
	params.InvoiceLineItemRepo = stores.InvoiceLineItemRepo
	params.TenantRepo = stores.TenantRepo
	params.EnvironmentRepo = stores.EnvironmentRepo
	params.PaymentRepo = stores.PaymentRepo
	params.CouponRepo = stores.CouponRepo
	params.CouponAssociationRepo = stores.CouponAssociationRepo
	params.CouponApplicationRepo = stores.CouponApplicationRepo
	params.TaxAssociationRepo = stores.TaxAssociationRepo
	params.TaxRateRepo = stores.TaxRateRepo
	params.TaxAppliedRepo = stores.TaxAppliedRepo
	params.RedisCache = s.GetRedisCache()
	params.EventPublisher = s.GetPublisher()

	for _, key := range []types.SettingKey{types.SettingKeySubscriptionAlertConfig, types.SettingKeyWalletBalanceAlertConfig} {
		s.Require().NoError(stores.SettingsRepo.Create(ctx, &settings.Setting{
			ID:            types.GenerateUUIDWithPrefix("setting"),
			Key:           key,
			Value:         map[string]interface{}{"alert_enabled": true},
			EnvironmentID: types.GetEnvironmentID(ctx),
			BaseModel:     types.GetDefaultBaseModel(ctx),
		}))
	}

	p := s.simplePlan("plan-aq")
	cust := s.simpleCustomer("cust-aq")
	start := time.Now().UTC().Truncate(time.Second).Add(-48 * time.Hour)
	sub := &subscription.Subscription{
		ID:                 "sub-aq",
		CustomerID:         cust.ID,
		PlanID:             p.ID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		SubscriptionType:   types.SubscriptionTypeStandalone,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		StartDate:          start,
		BillingAnchor:      start,
		CurrentPeriodStart: start,
		CurrentPeriodEnd:   start.AddDate(0, 1, 0),
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(stores.SubscriptionRepo.Create(ctx, sub))

	addLine := func(i int, agg types.AggregationType) {
		id := fmt.Sprintf("aq-%s-%02d", agg, i)
		m := &meter.Meter{
			ID:          "meter-" + id,
			Name:        id,
			EventName:   id,
			Aggregation: meter.Aggregation{Type: agg},
			ResetUsage:  types.ResetUsageBillingPeriod,
			BaseModel:   types.GetDefaultBaseModel(ctx),
		}
		s.Require().NoError(s.meterStore.CreateMeter(ctx, m))
		pr := &price.Price{
			ID:           "price-" + id,
			Amount:       decimal.NewFromFloat(0.01),
			Currency:     "usd",
			Type:         types.PRICE_TYPE_USAGE,
			BillingModel: types.BILLING_MODEL_FLAT_FEE,
			MeterID:      m.ID,
			BaseModel:    types.GetDefaultBaseModel(ctx),
		}
		s.Require().NoError(stores.PriceRepo.Create(ctx, pr))
		s.Require().NoError(stores.SubscriptionLineItemRepo.Create(ctx, &subscription.SubscriptionLineItem{
			ID:             "sli-" + id,
			SubscriptionID: sub.ID,
			CustomerID:     cust.ID,
			EntityID:       p.ID,
			EntityType:     types.SubscriptionLineItemEntityTypePlan,
			PriceID:        pr.ID,
			PriceType:      types.PRICE_TYPE_USAGE,
			MeterID:        m.ID,
			Currency:       "usd",
			BillingPeriod:  sub.BillingPeriod,
			InvoiceCadence: types.InvoiceCadenceArrear,
			StartDate:      sub.StartDate,
			BaseModel:      types.GetDefaultBaseModel(ctx),
		}))
		if i > 0 || agg != types.AggregationCount {
			s.seedMeterUsage(cust.ExternalID, m.ID, start.Add(time.Minute), 3)
		}
	}
	for i := 0; i < sumMeters; i++ {
		addLine(i, types.AggregationSum)
	}
	for i := 0; i < countMeters; i++ {
		addLine(i, types.AggregationCount)
	}

	s.Require().NoError(stores.AlertRepo.Create(ctx, &alert.AlertSettings{
		ID:         "alert-aq-sub",
		EntityType: types.AlertEntityTypeSubscription,
		EntityID:   sub.ID,
		Enabled:    true,
		Config: &types.AlertSettings{
			AlertEnabled: lo.ToPtr(true),
			Critical:     &types.AlertThreshold{Threshold: decimal.NewFromInt(1000), Condition: types.AlertConditionAbove},
		},
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}))

	s.Require().NoError(stores.WalletRepo.CreateWallet(ctx, &wallet.Wallet{
		ID:             "wallet-aq",
		CustomerID:     cust.ID,
		Currency:       "usd",
		Balance:        decimal.NewFromInt(100),
		CreditBalance:  decimal.NewFromInt(100),
		WalletStatus:   types.WalletStatusActive,
		WalletType:     types.WalletTypePrePaid,
		ConversionRate: decimal.NewFromInt(1),
		AlertSettings: &types.AlertSettings{
			AlertEnabled: lo.ToPtr(true),
			Critical:     &types.AlertThreshold{Threshold: decimal.NewFromInt(1), Condition: types.AlertConditionBelow},
		},
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}))

	counter.reset()
	return alertQueryFixture{cust: cust, sub: sub, counter: counter, params: params}
}

func (s *EntitlementGrantSuite) runAlertEvaluation(fx alertQueryFixture) {
	ctx := s.GetContext()
	svc := NewAlertService(fx.params)
	s.Require().NoError(svc.EvaluateSpendAndEntitlementAlertsForCustomer(ctx, fx.cust, true, false))
	s.Require().NoError(svc.EvaluateWalletAlertsForCustomer(ctx, fx.cust, "seed"))
}

func (s *EntitlementGrantSuite) setAlertReadFlags(on bool) {
	cfg := s.GetConfig()
	prevAlerts, prevUsage := cfg.UsageAlerts, cfg.MeterUsage
	cfg.UsageAlerts.SkipFinal = on
	cfg.MeterUsage.SkipDistinctMeterPrefilter = on
	s.T().Cleanup(func() { cfg.UsageAlerts, cfg.MeterUsage = prevAlerts, prevUsage })
}

func (s *EntitlementGrantSuite) TestAlertEvaluation_MeterUsageQueryCount() {
	fx := s.newAlertQueryFixture(25, 25)

	s.setAlertReadFlags(false)
	s.runAlertEvaluation(fx)
	s.T().Logf("default: total=%d final=%d calls=%v", fx.counter.total, fx.counter.final, fx.counter.calls)
	s.Equal(6, fx.counter.total)
	s.Equal(2, fx.counter.final)
	s.Equal(2, fx.counter.calls["GetDistinctMeterIDs"])

	fx.counter.reset()
	s.setAlertReadFlags(true)
	s.runAlertEvaluation(fx)
	s.T().Logf("flags on: total=%d final=%d calls=%v", fx.counter.total, fx.counter.final, fx.counter.calls)
	s.Equal(4, fx.counter.total)
	s.Equal(0, fx.counter.final)
	s.Zero(fx.counter.calls["GetDistinctMeterIDs"])
}

func (s *EntitlementGrantSuite) TestSkipDistinctMeterPrefilter_SameUsage() {
	fx := s.newAlertQueryFixture(3, 3)
	ctx := s.GetContext()
	svc := NewMeterUsageService(fx.params)
	req := func() *GetSubscriptionMeterUsageRequest {
		return &GetSubscriptionMeterUsageRequest{SubscriptionID: fx.sub.ID, StartTime: fx.sub.CurrentPeriodStart, EndTime: fx.sub.CurrentPeriodEnd, IncludeChildren: true}
	}
	usageByLine := func() map[string]string {
		sub := *fx.sub
		res, err := svc.GetSubscriptionMeterUsageWithSub(ctx, &sub, req())
		s.Require().NoError(err)
		out := map[string]string{}
		for _, u := range res.LineItemUsages {
			out[u.LineItem.ID] = fmt.Sprintf("%s/%d", u.Usage, u.EventCount)
		}
		return out
	}

	s.setAlertReadFlags(false)
	before := usageByLine()
	s.setAlertReadFlags(true)
	after := usageByLine()
	s.Len(before, 6)
	s.Equal(before, after)
}
