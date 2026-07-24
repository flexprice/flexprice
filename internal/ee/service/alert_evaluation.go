package service

import (
	"context"
	"errors"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainAlert "github.com/flexprice/flexprice/internal/domain/alert"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// EvaluateSpendAlertsForCustomer evaluates subscription-level spend alerts for
// the customer's active subscriptions. Line-item and group scopes were
// deliberately dropped — only subscription totals fire alerts.
func (s *alertService) EvaluateSpendAlertsForCustomer(ctx context.Context, cust *customer.Customer) error {
	subs, err := s.listActiveSubscriptions(ctx, cust.ID)
	if err != nil {
		return err
	}
	return s.evaluateSpendAlertsForSubscriptions(ctx, cust, subs)
}

func (s *alertService) listActiveSubscriptions(ctx context.Context, customerID string) ([]*subscription.Subscription, error) {
	filter := types.NewNoLimitSubscriptionFilter()
	filter.CustomerID = customerID
	filter.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusTrialing,
	}
	return s.SubRepo.List(ctx, filter)
}

// evaluateSpendAlertsForSubscriptions is the data-fed core: config lookup
// happens before any usage query, so customers without alert settings cost one
// indexed read and nothing else.
func (s *alertService) evaluateSpendAlertsForSubscriptions(ctx context.Context, cust *customer.Customer, subs []*subscription.Subscription) error {
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
// evaluation in one activity, sharing a single active-subscriptions fetch.
// The two halves are independent — one failing must not block the other — so
// both run and their errors join for the Temporal retry decision.
// Idempotent under Temporal retries.
func (s *alertService) EvaluateSpendAndEntitlementAlertsForCustomer(
	ctx context.Context,
	cust *customer.Customer,
) error {
	if cust == nil {
		return nil
	}
	at := time.Now().UTC()

	subs, err := s.listActiveSubscriptions(ctx, cust.ID)
	if err != nil {
		return err
	}
	if len(subs) == 0 {
		return nil
	}

	spendErr := s.evaluateSpendAlertsForSubscriptions(ctx, cust, subs)
	if spendErr != nil {
		s.Logger.Error(ctx, "fused evaluator: spend alerts returned error", "error", spendErr, "customer_id", cust.ID)
	}

	grantSvc := NewEntitlementGrantService(s.ServiceParams)
	grants, meta, err := grantSvc.EnsureGrantsForSubscriptions(ctx, cust, subs, at)
	if err != nil {
		return err
	}
	grantErr := s.evaluateEntitlementGrantsForCustomer(ctx, cust, meta, grants, at)
	if grantErr != nil {
		s.Logger.Error(ctx, "fused evaluator: grant evaluation returned error", "error", grantErr, "customer_id", cust.ID)
	}

	return errors.Join(spendErr, grantErr)
}

// evaluateEntitlementGrantsForCustomer refreshes usage and fires alerts for
// each returned grant (open windows plus closed ones getting their final
// refresh). Alert-log dedup makes it safe under Temporal retries. meta is the
// lookup bundle built during EnsureGrantsForSubscriptions; grants whose
// subscription is not in it (e.g. cancelled mid-window) are skipped.
func (s *alertService) evaluateEntitlementGrantsForCustomer(
	ctx context.Context,
	cust *customer.Customer,
	meta *grantEvalMeta,
	grants []*entitlementgrant.EntitlementGrant,
	at time.Time,
) error {
	if len(grants) == 0 || meta == nil {
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

	alertLogsSvc := NewAlertLogsService(s.ServiceParams)

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

		extIDs, err := meta.externalIDs(ctx, sub)
		if err != nil {
			s.Logger.Error(ctx, "entitlement grant evaluation: external customer id lookup failed",
				"grant_id", g.ID, "subscription_id", sub.ID, "error", err)
			continue
		}

		usage, err := s.refreshEntitlementGrantUsage(ctx, g, m, sub, extIDs, at)
		if err != nil {
			s.Logger.Error(ctx, "entitlement grant evaluation: usage refresh failed",
				"grant_id", g.ID, "error", err)
			continue
		}

		if err := s.transitionEntitlementGrantAlert(ctx, alertLogsSvc, cust, g, usage, at); err != nil {
			s.Logger.Error(ctx, "entitlement grant evaluation: alert transition failed",
				"grant_id", g.ID, "error", err)
		}

		// Snapshot the refreshed usage; last_computed_at >= valid_to marks a
		// closed window as finalized so it never re-enters the refresh set.
		builder := entitlementgrant.NewEntitlementGrantBuilder(g).
			WithUsage(usage).
			WithLastComputedAt(&at)
		if usage.GreaterThanOrEqual(g.Quota) && g.GrantStatus == types.EntitlementGrantStatusActive {
			builder = builder.WithGrantStatus(types.EntitlementGrantStatusExhausted)
		}
		if err := s.EntitlementGrantRepo.UpdateSnapshot(ctx, builder.Build()); err != nil {
			s.Logger.Error(ctx, "entitlement grant evaluation: snapshot write failed",
				"grant_id", g.ID, "error", err)
		}
	}
	return nil
}

// refreshEntitlementGrantUsage refreshes the grant's consumed usage over
// [valid_from, min(at, valid_to)).
//
//   - quantity measure: one raw meter-usage query over the window.
//   - amount measure: rides the billing path (GetSubscriptionMeterUsageWithSub +
//     ConvertToBillingCharges), which splits the window into per-line-item date
//     ranges — a mid-window price change produces a new line-item segment, so
//     each segment is priced at its own price. No price pinning required.
func (s *alertService) refreshEntitlementGrantUsage(
	ctx context.Context,
	g *entitlementgrant.EntitlementGrant,
	m *meter.Meter,
	sub *subscription.Subscription,
	extCustomerIDs []string,
	at time.Time,
) (decimal.Decimal, error) {
	end := at
	if end.After(g.ValidTo) {
		end = g.ValidTo
	}
	if !end.After(g.ValidFrom) {
		return decimal.Zero, nil
	}

	if g.Measure == types.EntitlementGrantMeasureQuantity {
		req := &dto.GrantWindowUsageRequest{
			TenantID:            g.TenantID,
			EnvironmentID:       g.EnvironmentID,
			ExternalCustomerIDs: extCustomerIDs,
			MeterID:             m.ID,
			AggregationType:     m.Aggregation.Type,
			StartTime:           g.ValidFrom,
			EndTime:             end,
		}
		result, err := s.MeterUsageRepo.GetUsage(ctx, req.ToParams())
		if err != nil {
			return decimal.Zero, err
		}
		return result.TotalValue, nil
	}

	meterUsageSvc := NewMeterUsageService(s.ServiceParams)
	subUsage, err := meterUsageSvc.GetSubscriptionMeterUsageWithSub(ctx, sub, &GetSubscriptionMeterUsageRequest{
		SubscriptionID:  sub.ID,
		StartTime:       g.ValidFrom,
		EndTime:         end,
		UseFinal:        true,
		IncludeChildren: true,
	})
	if err != nil {
		return decimal.Zero, err
	}

	// The returned totalCost spans every meter on the subscription; the grant
	// only owns its meter, so filter charges down to m.ID (a meter can carry
	// several charges when price segments split the window).
	charges, _, err := meterUsageSvc.ConvertToBillingCharges(ctx, subUsage)
	if err != nil {
		return decimal.Zero, err
	}

	total := decimal.Zero
	for _, c := range charges {
		if c != nil && c.MeterID == m.ID {
			total = total.Add(decimal.NewFromFloat(c.Amount))
		}
	}
	return total, nil
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
	if ratio.LessThan(decimal.NewFromInt(1)) {
		// not exhausted yet, AlertStateOk
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
		AlertStatus:      types.AlertStateInAlarm,
		AlertInfo: types.AlertInfo{
			ValueAtTime: ratio,
			Timestamp:   at,
		},
	})
}
