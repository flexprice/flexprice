package service

import (
	"context"
	"errors"
	"time"

	"github.com/flexprice/flexprice/internal/clock"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainAlert "github.com/flexprice/flexprice/internal/domain/alert"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
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
	now := clock.Now()

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
			nil,
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

// EvaluateSpendAndEntitlementAlertsForCustomer runs spend alerts and grant
// evaluation in one activity, sharing a single active-subscriptions fetch.
// The two halves are independent — one failing must not block the other — so
// both run and their errors join for the Temporal retry decision.
// Idempotent under Temporal retries.
func (s *alertService) EvaluateSpendAndEntitlementAlertsForCustomer(
	ctx context.Context,
	cust *customer.Customer,
	withSpendAlerts bool,
	withEntitlementAlerts bool,
) error {
	if cust == nil {
		return nil
	}
	at := clock.Now()

	subs, err := s.listActiveSubscriptions(ctx, cust.ID)
	if err != nil {
		return err
	}
	if len(subs) == 0 {
		return nil
	}

	var spendErr error
	if withSpendAlerts {
		spendErr = s.evaluateSpendAlertsForSubscriptions(ctx, cust, subs)
		if spendErr != nil {
			s.Logger.Error(ctx, "fused evaluator: spend alerts returned error", "error", spendErr, "customer_id", cust.ID)
		}
	}

	var grantErr error
	if withEntitlementAlerts {
		grantErr = s.EvaluateAndAlertEntitlementGrants(ctx, cust, subs, at)
		if grantErr != nil {
			s.Logger.Error(ctx, "fused evaluator: grant evaluation returned error", "error", grantErr, "customer_id", cust.ID)
		}
	}

	return errors.Join(spendErr, grantErr)
}

// EvaluateAndAlertEntitlementGrants opens the subscriptions' missing grant windows in each one's
// current period, then measures, alerts on and snapshots each open or unfinalized window. Alert-log
// dedup makes it safe under Temporal retries. Per-grant failures never block the remaining grants;
// they join into the returned error.
func (s *alertService) EvaluateAndAlertEntitlementGrants(ctx context.Context, cust *customer.Customer, subs []*subscription.Subscription, at time.Time) error {
	if s.EntitlementGrantRepo == nil || cust == nil || len(subs) == 0 {
		return nil
	}
	grantSvc := NewEntitlementGrantService(s.ServiceParams)
	grants, meta, err := grantSvc.EnsureGrantsForSubscriptions(ctx, cust, subs, at)
	if err != nil || meta == nil {
		return err
	}

	alertLogsSvc := NewAlertLogsService(s.ServiceParams)
	var errs []error
	for _, g := range grants {
		// Grants of subscriptions not passed in (e.g. cancelled mid-window) are skipped.
		sub, ok := meta.subsByID[g.SubscriptionID]
		if !ok {
			s.Logger.Debug(ctx, "entitlement grant evaluation: subscription missing", "grant_id", g.ID)
			continue
		}

		// A window dated ahead of the change that opened it — a future-dated addon
		// attach — has nothing to measure yet. Snapshotting it would stamp
		// last_computed_at before valid_from, which reads as "never evaluated" to a
		// later close and would drop the segment instead of splitting it.
		if g.ValidFrom.After(at) {
			s.Logger.Debug(ctx, "entitlement grant evaluation: window has not started",
				"grant_id", g.ID, "valid_from", g.ValidFrom)
			continue
		}
		// Already measured as far as this pass would: a historical recalculation must not move usage back.
		if g.LastComputedAt != nil && !g.LastComputedAt.Before(types.EarliestOf(at, g.ValidTo)) {
			continue
		}

		usage, err := grantSvc.MeasureEntitlementGrantUsage(ctx, meta, sub, g, at)
		if ierr.IsNotFound(err) {
			s.Logger.Debug(ctx, "entitlement grant evaluation: feature or meter missing", "grant_id", g.ID)
			continue
		}
		if err != nil {
			s.Logger.Error(ctx, "entitlement grant evaluation: usage refresh failed",
				"grant_id", g.ID, "error", err)
			errs = append(errs, err)
			continue
		}

		if err := s.transitionEntitlementGrantAlert(ctx, alertLogsSvc, cust, g, usage, at); err != nil {
			s.Logger.Error(ctx, "entitlement grant evaluation: alert transition failed",
				"grant_id", g.ID, "error", err)
			errs = append(errs, err)
		}

		if err := s.EntitlementGrantRepo.UpdateSnapshot(ctx, grantUsageSnapshot(g, usage, at)); err != nil {
			s.Logger.Error(ctx, "entitlement grant evaluation: snapshot write failed",
				"grant_id", g.ID, "error", err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
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
	status := types.AlertStateInAlarm
	if !g.IsExhausted(usage) {
		if !g.ValidTo.After(at) {
			return nil
		}
		status = types.AlertStateOk
	}

	// Zero quota is spent by definition, and dividing by it would panic.
	ratio := decimal.NewFromInt(1)
	if g.Quota.IsPositive() {
		ratio = usage.Div(g.Quota)
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
		AlertStatus:      status,
		AlertInfo: types.AlertInfo{
			ValueAtTime: ratio,
			Timestamp:   at,
		},
	})
}
