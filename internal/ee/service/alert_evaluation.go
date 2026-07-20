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

// EvaluateSpendAlertsForCustomer evaluates subscription / line-item / group
// spend alerts for the customer's active subscriptions. meterIDs and periodStart
// are optional filters used by the sync per-event caller.
func (s *alertService) EvaluateSpendAlertsForCustomer(
	ctx context.Context,
	cust *customer.Customer,
	meterIDs []string,
	periodStart *time.Time,
) error {
	affectedLineItems, err := s.SubscriptionLineItemRepo.List(ctx, &types.SubscriptionLineItemFilter{
		QueryFilter:        types.NewNoLimitQueryFilter(),
		CustomerIDs:        []string{cust.ID},
		MeterIDs:           meterIDs,
		ActiveFilter:       true,
		CurrentPeriodStart: periodStart,
	})
	if err != nil {
		return err
	}
	if len(affectedLineItems) == 0 {
		return nil
	}

	subscriptionIDs := lo.Uniq(lo.Map(affectedLineItems, func(li *subscription.SubscriptionLineItem, _ int) string {
		return li.SubscriptionID
	}))

	allSubCfgs, err := s.AlertRepo.List(ctx, &types.AlertSettingsFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		EntityType:  types.AlertEntityTypeSubscription,
		EntityIDs:   subscriptionIDs,
		Enabled:     lo.ToPtr(true),
	})
	if err != nil {
		return err
	}
	allLineItemCfgs, err := s.AlertRepo.List(ctx, &types.AlertSettingsFilter{
		QueryFilter:      types.NewNoLimitQueryFilter(),
		EntityType:       types.AlertEntityTypeSubscriptionLineItem,
		ParentEntityType: types.AlertEntityTypeSubscription,
		ParentEntityIDs:  subscriptionIDs,
		Enabled:          lo.ToPtr(true),
	})
	if err != nil {
		return err
	}
	allGroupCfgs, err := s.AlertRepo.List(ctx, &types.AlertSettingsFilter{
		QueryFilter:      types.NewNoLimitQueryFilter(),
		EntityType:       types.AlertEntityTypeGroup,
		ParentEntityType: types.AlertEntityTypeSubscription,
		ParentEntityIDs:  subscriptionIDs,
		Enabled:          lo.ToPtr(true),
	})
	if err != nil {
		return err
	}
	if len(allSubCfgs) == 0 && len(allLineItemCfgs) == 0 && len(allGroupCfgs) == 0 {
		return nil
	}

	subscriptionSvc := NewSubscriptionService(s.ServiceParams)
	billingSvc := NewBillingService(s.ServiceParams)
	alertLogsSvc := NewAlertLogsService(s.ServiceParams)
	now := time.Now().UTC()

	for _, subscriptionID := range subscriptionIDs {
		var subCfg *domainAlert.AlertSettings
		for _, c := range allSubCfgs {
			if c.EntityID == subscriptionID {
				subCfg = c
				break
			}
		}
		lineItemCfgs := lo.Filter(allLineItemCfgs, func(c *domainAlert.AlertSettings, _ int) bool {
			return c.ParentEntityID != nil && *c.ParentEntityID == subscriptionID
		})
		groupCfgs := lo.Filter(allGroupCfgs, func(c *domainAlert.AlertSettings, _ int) bool {
			return c.ParentEntityID != nil && *c.ParentEntityID == subscriptionID
		})
		if subCfg == nil && len(lineItemCfgs) == 0 && len(groupCfgs) == 0 {
			continue
		}

		sub, _, err := s.SubRepo.GetWithLineItems(ctx, subscriptionID)
		if err != nil {
			s.Logger.Error(ctx, "spend alerts: failed to get subscription with line items", "error", err, "subscription_id", subscriptionID)
			continue
		}

		usage, err := subscriptionSvc.GetMeterUsageBySubscription(ctx, &dto.GetUsageBySubscriptionRequest{
			SubscriptionID: subscriptionID,
			StartTime:      sub.CurrentPeriodStart,
			EndTime:        now,
			Source:         string(types.UsageSourceInvoiceCreation),
		})
		if err != nil {
			s.Logger.Error(ctx, "spend alerts: failed to get meter usage", "error", err, "subscription_id", subscriptionID)
			continue
		}

		usageCharges, totalUsageCost, err := billingSvc.CalculateMeterUsageCharges(
			ctx, sub, usage, sub.CurrentPeriodStart, now, types.UsageSourceInvoiceCreation,
		)
		if err != nil {
			s.Logger.Error(ctx, "spend alerts: failed to calculate meter usage charges", "error", err, "subscription_id", subscriptionID)
			continue
		}

		chargesByLine := make(map[string]decimal.Decimal, len(usageCharges))
		for _, c := range usageCharges {
			if c.SubscriptionLineItemID != nil {
				chargesByLine[*c.SubscriptionLineItemID] = c.Amount
			}
		}
		groupTotals := s.computeGroupTotalsForSubscription(ctx, sub, chargesByLine, groupCfgs)

		s.evaluateSpendForSubscription(ctx, cust.ID, sub, totalUsageCost, chargesByLine, groupTotals, subCfg, lineItemCfgs, groupCfgs, now, alertLogsSvc)
	}
	return nil
}

// computeGroupTotalsForSubscription sums each line item's charge into its
// feature-group bucket. Skips the feature fetch when no group configs exist.
func (s *alertService) computeGroupTotalsForSubscription(
	ctx context.Context,
	sub *subscription.Subscription,
	chargesByLine map[string]decimal.Decimal,
	groupCfgs []*domainAlert.AlertSettings,
) map[string]decimal.Decimal {
	if len(groupCfgs) == 0 {
		return nil
	}
	subMeterIDs := lo.Uniq(lo.Map(sub.LineItems, func(li *subscription.SubscriptionLineItem, _ int) string {
		return li.MeterID
	}))
	subFeatures, err := s.FeatureRepo.List(ctx, &types.FeatureFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		MeterIDs:    subMeterIDs,
	})
	if err != nil {
		s.Logger.Error(ctx, "spend alerts: failed to list features for group summation", "error", err, "subscription_id", sub.ID)
		return nil
	}
	featuresByMeterID := make(map[string]*feature.Feature, len(subFeatures))
	for _, f := range subFeatures {
		featuresByMeterID[f.MeterID] = f
	}
	totals := make(map[string]decimal.Decimal, len(groupCfgs))
	for _, li := range sub.LineItems {
		f, ok := featuresByMeterID[li.MeterID]
		if !ok || f.GroupID == "" {
			continue
		}
		amount, found := chargesByLine[li.ID]
		if !found {
			continue
		}
		totals[f.GroupID] = totals[f.GroupID].Add(amount)
	}
	return totals
}

// evaluateSpendForSubscription runs the three threshold scopes for one subscription.
func (s *alertService) evaluateSpendForSubscription(
	ctx context.Context,
	customerID string,
	sub *subscription.Subscription,
	totalUsageCost decimal.Decimal,
	chargesByLine map[string]decimal.Decimal,
	groupTotals map[string]decimal.Decimal,
	subCfg *domainAlert.AlertSettings,
	lineItemCfgs []*domainAlert.AlertSettings,
	groupCfgs []*domainAlert.AlertSettings,
	now time.Time,
	alertLogsSvc AlertLogsService,
) {
	periodStart := sub.CurrentPeriodStart

	if subCfg != nil {
		state, err := subCfg.Config.AlertState(totalUsageCost)
		if err != nil {
			s.Logger.Error(ctx, "failed to determine subscription spend alert state", "error", err, "subscription_id", sub.ID)
		} else if err := alertLogsSvc.LogAlert(ctx, &LogAlertRequest{
			AlertSettingID: &subCfg.ID,
			PeriodStart:    &periodStart,
			EntityType:     types.AlertEntityTypeSubscription,
			EntityID:       sub.ID,
			CustomerID:     &customerID,
			AlertType:      types.AlertTypeSubscriptionSpend,
			AlertStatus:    state,
			AlertInfo: types.AlertInfo{
				AlertSettings: subCfg.Config,
				ValueAtTime:   totalUsageCost,
				Timestamp:     now,
			},
		}); err != nil {
			s.Logger.Error(ctx, "failed to log subscription spend alert", "error", err, "subscription_id", sub.ID)
		}
	}

	for _, cfg := range lineItemCfgs {
		amount, found := chargesByLine[cfg.EntityID]
		if !found {
			continue
		}
		state, err := cfg.Config.AlertState(amount)
		if err != nil {
			s.Logger.Error(ctx, "failed to determine line item spend alert state", "error", err, "subscription_line_item_id", cfg.EntityID)
			continue
		}
		parentEntityType := string(types.AlertEntityTypeSubscription)
		if err := alertLogsSvc.LogAlert(ctx, &LogAlertRequest{
			AlertSettingID:   &cfg.ID,
			PeriodStart:      &periodStart,
			EntityType:       types.AlertEntityTypeSubscriptionLineItem,
			EntityID:         cfg.EntityID,
			ParentEntityType: &parentEntityType,
			ParentEntityID:   &sub.ID,
			CustomerID:       &customerID,
			AlertType:        types.AlertTypeSubscriptionLineItemSpend,
			AlertStatus:      state,
			AlertInfo: types.AlertInfo{
				AlertSettings: cfg.Config,
				ValueAtTime:   amount,
				Timestamp:     now,
			},
		}); err != nil {
			s.Logger.Error(ctx, "failed to log line item spend alert", "error", err, "subscription_line_item_id", cfg.EntityID)
		}
	}

	for _, cfg := range groupCfgs {
		groupTotal := groupTotals[cfg.EntityID]
		state, err := cfg.Config.AlertState(groupTotal)
		if err != nil {
			s.Logger.Error(ctx, "failed to determine group spend alert state", "error", err, "group_id", cfg.EntityID)
			continue
		}
		parentEntityType := string(types.AlertEntityTypeSubscription)
		if err := alertLogsSvc.LogAlert(ctx, &LogAlertRequest{
			AlertSettingID:   &cfg.ID,
			PeriodStart:      &periodStart,
			EntityType:       types.AlertEntityTypeGroup,
			EntityID:         cfg.EntityID,
			ParentEntityType: &parentEntityType,
			ParentEntityID:   &sub.ID,
			CustomerID:       &customerID,
			AlertType:        types.AlertTypeSubscriptionGroupSpend,
			AlertStatus:      state,
			AlertInfo: types.AlertInfo{
				AlertSettings: cfg.Config,
				ValueAtTime:   groupTotal,
				Timestamp:     now,
			},
		}); err != nil {
			s.Logger.Error(ctx, "failed to log group spend alert", "error", err, "group_id", cfg.EntityID)
		}
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
func (s *alertService) EvaluateSpendBreachForEvent(ctx context.Context, event *events.Event, cust *customer.Customer, meterIDs []string) {
	if err := s.EvaluateSpendAlertsForCustomer(ctx, cust, meterIDs, &event.Timestamp); err != nil {
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

	spendErr := s.EvaluateSpendAlertsForCustomer(ctx, cust, nil, nil)
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

	unitPrice, ok := s.selectGrantUnitPrice(g, pricesByMeter[m.ID])
	if !ok {
		s.Logger.Error(ctx, "entitlement grant evaluation: amount lane could not resolve flat unit price",
			"grant_id", g.ID, "meter_id", m.ID)
		return decimal.Zero, nil
	}
	amount := priceSvc.CalculateCost(ctx, unitPrice, qty)
	return amount, nil
}

// selectGrantUnitPrice picks the highest-sequence flat price on the meter.
func (s *alertService) selectGrantUnitPrice(
	g *entitlementgrant.EntitlementGrant,
	prices []*price.Price,
) (*price.Price, bool) {
	flatPrices := lo.Filter(prices, func(p *price.Price, _ int) bool {
		return p.BillingModel == types.BILLING_MODEL_FLAT_FEE
	})
	if len(flatPrices) == 0 {
		return nil, false
	}
	best := flatPrices[0]
	for _, p := range flatPrices[1:] {
		if p.Sequence > best.Sequence {
			best = p
		}
	}
	return best, true
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
