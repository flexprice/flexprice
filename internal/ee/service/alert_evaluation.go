package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainAlert "github.com/flexprice/flexprice/internal/domain/alert"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// EvaluateSpendAlertsForCustomer evaluates subscription-level spend alerts for
// the customer's active subscriptions. Line-item and group scopes were
// deliberately dropped — only subscription totals fire alerts. Config lookup
// happens before any usage query so customers without alert settings cost two
// indexed reads and nothing else.
func (s *alertService) EvaluateSpendAlertsForCustomer(ctx context.Context, cust *customer.Customer) error {
	subFilter := types.NewNoLimitSubscriptionFilter()
	subFilter.CustomerID = cust.ID
	subFilter.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusTrialing,
	}
	subs, err := s.SubRepo.List(ctx, subFilter)
	if err != nil {
		return err
	}
	if len(subs) == 0 {
		return nil
	}
	subsByID := make(map[string]*subscription.Subscription, len(subs))
	subscriptionIDs := make([]string, 0, len(subs))
	for _, sub := range subs {
		subsByID[sub.ID] = sub
		subscriptionIDs = append(subscriptionIDs, sub.ID)
	}

	subCfgs, err := s.AlertRepo.List(ctx, &types.AlertSettingsFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		EntityType:  types.AlertEntityTypeSubscription,
		EntityIDs:   subscriptionIDs,
		Enabled:     lo.ToPtr(true),
	})
	if err != nil {
		return err
	}
	if len(subCfgs) == 0 {
		return nil
	}

	subscriptionSvc := NewSubscriptionService(s.ServiceParams)
	billingSvc := NewBillingService(s.ServiceParams)
	alertLogsSvc := NewAlertLogsService(s.ServiceParams)
	now := time.Now().UTC()

	for _, cfg := range subCfgs {
		sub, ok := subsByID[cfg.EntityID]
		if !ok {
			continue
		}

		// Data-fed call: the sub we already hold flows through usage + charges
		// with no repeat subscription fetch. Line items are loaded window-scoped
		// inside GetMeterUsageForSubscription and land on sub.LineItems.
		usage, err := subscriptionSvc.GetMeterUsageForSubscription(ctx, sub, &dto.GetUsageBySubscriptionRequest{
			SubscriptionID: sub.ID,
			StartTime:      sub.CurrentPeriodStart,
			EndTime:        now,
			Source:         string(types.UsageSourceInvoiceCreation),
		})
		if err != nil {
			s.Logger.Error(ctx, "spend alerts: failed to get meter usage", "error", err, "subscription_id", sub.ID)
			continue
		}

		_, totalUsageCost, err := billingSvc.CalculateMeterUsageCharges(
			ctx, sub, usage, sub.CurrentPeriodStart, now, types.UsageSourceInvoiceCreation,
		)
		if err != nil {
			s.Logger.Error(ctx, "spend alerts: failed to calculate meter usage charges", "error", err, "subscription_id", sub.ID)
			continue
		}

		s.logSubscriptionSpendAlert(ctx, alertLogsSvc, cust.ID, sub, cfg, totalUsageCost, now)
	}
	return nil
}

func (s *alertService) logSubscriptionSpendAlert(
	ctx context.Context,
	alertLogsSvc AlertLogsService,
	customerID string,
	sub *subscription.Subscription,
	cfg *domainAlert.AlertSettings,
	totalUsageCost decimal.Decimal,
	now time.Time,
) {
	state, err := cfg.Config.AlertState(totalUsageCost)
	if err != nil {
		s.Logger.Error(ctx, "failed to determine subscription spend alert state", "error", err, "subscription_id", sub.ID)
		return
	}
	periodStart := sub.CurrentPeriodStart
	if err := alertLogsSvc.LogAlert(ctx, &LogAlertRequest{
		AlertSettingID: &cfg.ID,
		PeriodStart:    &periodStart,
		EntityType:     types.AlertEntityTypeSubscription,
		EntityID:       sub.ID,
		CustomerID:     &customerID,
		AlertType:      types.AlertTypeSubscriptionSpend,
		AlertStatus:    state,
		AlertInfo: types.AlertInfo{
			AlertSettings: cfg.Config,
			ValueAtTime:   totalUsageCost,
			Timestamp:     now,
		},
	}); err != nil {
		s.Logger.Error(ctx, "failed to log subscription spend alert", "error", err, "subscription_id", sub.ID)
	}
}

// EvaluateWalletAlertsForCustomer gates on the tenant wallet-alert setting,
// then delegates each wallet to walletService.EvaluateAlertsForWallet.
func (s *alertService) EvaluateWalletAlertsForCustomer(ctx context.Context, cust *customer.Customer, autoTopupIdempotencySeed string) error {
	settingsSvc := &settingsService{ServiceParams: s.ServiceParams}
	tenantCfg, err := GetSetting[types.AlertSettings](settingsSvc, ctx, types.SettingKeyWalletBalanceAlertConfig)
	if err != nil {
		s.Logger.Debug(ctx, "wallet alerts: config unavailable, treating as disabled",
			"customer_id", cust.ID, "error", err,
		)
		return nil
	}
	if !tenantCfg.IsAlertEnabled() {
		return nil
	}

	wallets, err := s.WalletRepo.GetWalletsByCustomerID(ctx, cust.ID)
	if err != nil {
		return err
	}
	if len(wallets) == 0 {
		return nil
	}

	walletSvc := NewWalletService(s.ServiceParams)
	alertLogsSvc := NewAlertLogsService(s.ServiceParams)

	for _, w := range wallets {
		if err := walletSvc.EvaluateAlertsForWallet(ctx, w, alertLogsSvc, autoTopupIdempotencySeed); err != nil {
			s.Logger.Error(ctx, "wallet alerts: EvaluateAlertsForWallet returned error", "error", err, "wallet_id", w.ID)
		}
	}
	return nil
}

// EvaluateSpendBreachForEvent is the sync per-event entry used when the debouncer is off.
func (s *alertService) EvaluateSpendBreachForEvent(ctx context.Context, event *events.Event, cust *customer.Customer) {
	if err := s.EvaluateSpendAlertsForCustomer(ctx, cust); err != nil {
		s.Logger.Error(ctx, "failed to evaluate spend alerts for event", "error", err, "event_id", event.ID, "customer_id", cust.ID)
	}
}

// EvaluateSpendAndEntitlementAlertsForCustomer runs spend alerts and grant
// evaluation in one activity. Idempotent under Temporal retries.
func (s *alertService) EvaluateSpendAndEntitlementAlertsForCustomer(
	ctx context.Context,
	cust *customer.Customer,
) error {
	if cust == nil {
		return nil
	}
	at := time.Now().UTC()

	spendErr := s.EvaluateSpendAlertsForCustomer(ctx, cust)
	if spendErr != nil {
		s.Logger.Error(ctx, "fused evaluator: spend alerts returned error", "error", spendErr, "customer_id", cust.ID)
	}

	grantSvc := NewEntitlementGrantService(s.ServiceParams)
	grants, err := grantSvc.EnsureGrants(ctx, cust, at)
	if err != nil {
		if spendErr != nil {
			return spendErr
		}
		return err
	}

	if err := s.evaluateEntitlementGrantsForCustomer(ctx, cust, grants, at); err != nil {
		if spendErr != nil {
			return spendErr
		}
		return err
	}
	return spendErr
}

// evaluateEntitlementGrantsForCustomer refreshes usage and fires alerts for
// each live grant. Alert-log dedup makes it safe under Temporal retries.
func (s *alertService) evaluateEntitlementGrantsForCustomer(
	ctx context.Context,
	cust *customer.Customer,
	grants []*entitlementgrant.EntitlementGrant,
	at time.Time,
) error {
	if len(grants) == 0 {
		return nil
	}

	featureGrants := make([]*entitlementgrant.EntitlementGrant, 0, len(grants))
	for _, g := range grants {
		if g.IsFeatureScoped() {
			featureGrants = append(featureGrants, g)
			continue
		}
		s.Logger.Debug(ctx, "entitlement grant evaluation: skipping non-feature scope",
			"grant_id", g.ID,
			"scope_entity_type", g.ScopeEntityType,
		)
	}
	if len(featureGrants) == 0 {
		return nil
	}

	meta, err := s.loadEntitlementGrantMeta(ctx, featureGrants)
	if err != nil {
		return err
	}

	alertLogsSvc := NewAlertLogsService(s.ServiceParams)
	subscriptionSvc := NewSubscriptionService(s.ServiceParams)
	priceSvc := NewPriceService(s.ServiceParams)

	extIDsBySub := make(map[string][]string, len(meta.subsByID))

	for _, g := range featureGrants {
		f, ok := meta.featureByID[g.ScopeEntityID]
		if !ok || f.MeterID == "" {
			s.Logger.Debug(ctx, "entitlement grant evaluation: feature or meter missing", "grant_id", g.ID)
			continue
		}
		m, ok := meta.meterByID[f.MeterID]
		if !ok {
			s.Logger.Debug(ctx, "entitlement grant evaluation: meter missing", "grant_id", g.ID, "meter_id", f.MeterID)
			continue
		}
		sub, ok := meta.subsByID[g.SubscriptionID]
		if !ok {
			s.Logger.Debug(ctx, "entitlement grant evaluation: subscription missing", "grant_id", g.ID)
			continue
		}

		extIDs, ok := extIDsBySub[sub.ID]
		if !ok {
			extIDs, err = subscriptionSvc.ExternalCustomerIDsForSubscription(ctx, sub)
			if err != nil {
				s.Logger.Error(ctx, "entitlement grant evaluation: external customer id lookup failed",
					"grant_id", g.ID, "subscription_id", sub.ID, "error", err)
				continue
			}
			extIDsBySub[sub.ID] = extIDs
		}

		usage, err := s.refreshEntitlementGrantUsage(ctx, g, m, extIDs, priceSvc, meta.pricesByMeter, at)
		if err != nil {
			s.Logger.Error(ctx, "entitlement grant evaluation: usage refresh failed",
				"grant_id", g.ID, "error", err)
			continue
		}

		if err := s.transitionEntitlementGrantAlert(ctx, alertLogsSvc, cust, g, usage, at); err != nil {
			s.Logger.Error(ctx, "entitlement grant evaluation: alert transition failed",
				"grant_id", g.ID, "error", err)
		}

		if err := s.snapshotEntitlementGrant(ctx, g, usage, at); err != nil {
			s.Logger.Error(ctx, "entitlement grant evaluation: snapshot write failed",
				"grant_id", g.ID, "error", err)
		}
	}
	return nil
}

// entitlementGrantEvalMeta is the per-tick lookup bundle for the grant loop.
type entitlementGrantEvalMeta struct {
	subsByID      map[string]*subscription.Subscription
	featureByID   map[string]*feature.Feature
	meterByID     map[string]*meter.Meter
	pricesByMeter map[string][]*price.Price
}

func (s *alertService) loadEntitlementGrantMeta(
	ctx context.Context,
	grants []*entitlementgrant.EntitlementGrant,
) (*entitlementGrantEvalMeta, error) {
	subIDs := lo.Uniq(lo.Map(grants, func(g *entitlementgrant.EntitlementGrant, _ int) string {
		return g.SubscriptionID
	}))
	subs := make(map[string]*subscription.Subscription, len(subIDs))
	for _, id := range subIDs {
		sub, err := s.SubRepo.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		subs[id] = sub
	}

	featureIDs := lo.Uniq(lo.Map(grants, func(g *entitlementgrant.EntitlementGrant, _ int) string {
		return g.ScopeEntityID
	}))
	featureFilter := types.NewNoLimitFeatureFilter()
	featureFilter.FeatureIDs = featureIDs
	features, err := s.FeatureRepo.List(ctx, featureFilter)
	if err != nil {
		return nil, err
	}
	featureByID := lo.KeyBy(features, func(f *feature.Feature) string { return f.ID })

	meterIDs := lo.Uniq(lo.FilterMap(features, func(f *feature.Feature, _ int) (string, bool) {
		return f.MeterID, f.MeterID != ""
	}))
	var meters []*meter.Meter
	if len(meterIDs) > 0 {
		meterFilter := types.NewNoLimitMeterFilter()
		meterFilter.MeterIDs = meterIDs
		meters, err = s.MeterRepo.List(ctx, meterFilter)
		if err != nil {
			return nil, err
		}
	}
	meterByID := lo.KeyBy(meters, func(m *meter.Meter) string { return m.ID })

	pricesByMeter := make(map[string][]*price.Price)
	if len(meterIDs) > 0 {
		priceFilter := types.NewNoLimitPriceFilter()
		priceFilter.MeterIDs = meterIDs
		prices, err := s.PriceRepo.List(ctx, priceFilter)
		if err != nil {
			return nil, err
		}
		for _, p := range prices {
			pricesByMeter[p.MeterID] = append(pricesByMeter[p.MeterID], p)
		}
	}

	return &entitlementGrantEvalMeta{
		subsByID:      subs,
		featureByID:   featureByID,
		meterByID:     meterByID,
		pricesByMeter: pricesByMeter,
	}, nil
}

// refreshEntitlementGrantUsage sums usage over [valid_from, min(at, valid_to))
// and, for amount lane, multiplies by the flat unit price.
func (s *alertService) refreshEntitlementGrantUsage(
	ctx context.Context,
	g *entitlementgrant.EntitlementGrant,
	m *meter.Meter,
	extCustomerIDs []string,
	priceSvc PriceService,
	pricesByMeter map[string][]*price.Price,
	at time.Time,
) (decimal.Decimal, error) {
	end := at
	if end.After(g.ValidTo) {
		end = g.ValidTo
	}
	if !end.After(g.ValidFrom) {
		return decimal.Zero, nil
	}

	result, err := s.MeterUsageRepo.GetUsage(ctx, &events.MeterUsageQueryParams{
		TenantID:            g.TenantID,
		EnvironmentID:       g.EnvironmentID,
		ExternalCustomerIDs: extCustomerIDs,
		MeterID:             m.ID,
		StartTime:           g.ValidFrom,
		EndTime:             end,
		AggregationType:     m.Aggregation.Type,
		UseFinal:            true,
	})
	if err != nil {
		return decimal.Zero, err
	}
	qty := result.TotalValue

	if g.Measure == types.EntitlementGrantMeasureQuantity {
		return qty, nil
	}

	// Amount lane: use the unit price pinned at open time so mid-window price
	// changes never retroactively reprice consumed usage; a price change takes
	// effect from the next grant window.
	if g.UnitPrice != nil {
		return qty.Mul(*g.UnitPrice), nil
	}

	// Unpinned grant (price resolution failed at open) — live lookup fallback.
	unitPrice, ok := selectFlatUnitPrice(pricesByMeter[m.ID])
	if !ok {
		s.Logger.Error(ctx, "entitlement grant evaluation: amount lane could not resolve flat unit price",
			"grant_id", g.ID, "meter_id", m.ID)
		return decimal.Zero, nil
	}
	amount := priceSvc.CalculateCost(ctx, unitPrice, qty)
	return amount, nil
}

// transitionEntitlementGrantAlert emits an alert-log row on state change.
func (s *alertService) transitionEntitlementGrantAlert(
	ctx context.Context,
	alertLogsSvc AlertLogsService,
	cust *customer.Customer,
	g *entitlementgrant.EntitlementGrant,
	usage decimal.Decimal,
	at time.Time,
) error {
	if !g.Quota.IsPositive() {
		return nil
	}
	ratio := usage.Div(g.Quota)
	state := entitlementGrantStateFromRatio(ratio)
	if state == types.AlertStateOk {
		return nil
	}

	parentEntityType := string(types.AlertEntityTypeSubscription)
	custID := cust.ID
	return alertLogsSvc.LogAlert(ctx, &LogAlertRequest{
		EntityType:       types.AlertEntityTypeEntitlementGrant,
		EntityID:         g.ID,
		ParentEntityType: &parentEntityType,
		ParentEntityID:   &g.SubscriptionID,
		CustomerID:       &custID,
		AlertType:        types.AlertTypeEntitlementGrantExhausted,
		AlertStatus:      state,
		AlertInfo: types.AlertInfo{
			ValueAtTime: ratio,
			Timestamp:   at,
		},
	})
}

// snapshotEntitlementGrant writes usage and flips active→exhausted on crossing.
func (s *alertService) snapshotEntitlementGrant(
	ctx context.Context,
	g *entitlementgrant.EntitlementGrant,
	usage decimal.Decimal,
	at time.Time,
) error {
	g.Usage = usage
	g.LastComputedAt = &at
	if usage.GreaterThanOrEqual(g.Quota) && g.GrantStatus == types.EntitlementGrantStatusActive {
		g.GrantStatus = types.EntitlementGrantStatusExhausted
	}
	return s.EntitlementGrantRepo.UpdateSnapshot(ctx, g)
}

// entitlementGrantStateFromRatio fires only on exhaustion (ratio >= 1).
func entitlementGrantStateFromRatio(ratio decimal.Decimal) types.AlertState {
	if ratio.GreaterThanOrEqual(decimal.NewFromInt(1)) {
		return types.AlertStateInAlarm
	}
	return types.AlertStateOk
}
