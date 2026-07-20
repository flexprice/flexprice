package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainAlert "github.com/flexprice/flexprice/internal/domain/alert"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// EvaluateSpendAlertsForCustomer fetches the customer's active subscriptions
// with alert configs, pulls per-subscription usage, calculates charges, and
// logs alerts for every threshold that fires (subscription / line item /
// group). Self-contained — one call from the Temporal activity or the sync
// per-event path drives everything.
//
// meterIDs and periodStart are optional filters used by the sync per-event
// caller (nil for the debouncer path).
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

	// Batched alert-config fetch — same three-scope pattern used before.
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

// computeGroupTotalsForSubscription resolves the features backing this
// subscription's meters and sums each line item's charge into its feature-group
// bucket. Skips the feature fetch entirely when no group-level configs exist.
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

// evaluateSpendForSubscription runs the three threshold scopes for a single
// subscription. Failures on a single scope are logged and skipped so a bad
// row can't block the rest.
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

	// --- Part A: subscription-level threshold ---
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

	// --- Part B: line item-level thresholds ---
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

	// --- Part C: group-level thresholds ---
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

// EvaluateWalletAlertsForCustomer gates on the tenant-level wallet-alert
// setting, then walks every wallet for the customer, delegating each to
// walletService.EvaluateAlertsForWallet. Per-wallet resolve / balance /
// short-circuit / three-step handler dance lives entirely on walletService —
// this coordinator only owns the tenant + customer scope.
func (s *alertService) EvaluateWalletAlertsForCustomer(ctx context.Context, cust *customer.Customer, autoTopupIdempotencySeed string) error {
	settingsSvc := &settingsService{ServiceParams: s.ServiceParams}
	tenantCfg, err := GetSetting[types.AlertSettings](settingsSvc, ctx, types.SettingKeyWalletBalanceAlertConfig)
	if err != nil {
		// Fail-safe: same behavior as the legacy Kafka processEvent — treat
		// missing/unreadable setting as "wallet alerts disabled for this tenant".
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

// EvaluateSpendBreachForEvent is the sync per-event entry used by the meter usage
// post-insert side effect when the debouncer is off. Delegates to the shared
// EvaluateSpendAlertsForCustomer with meterIDs + periodStart filters so the exact
// same code runs on both the sync and the debounced path.
func (s *alertService) EvaluateSpendBreachForEvent(ctx context.Context, event *events.Event, cust *customer.Customer, meterIDs []string) {
	if err := s.EvaluateSpendAlertsForCustomer(ctx, cust, meterIDs, &event.Timestamp); err != nil {
		s.Logger.Error(ctx, "failed to evaluate spend alerts for event", "error", err, "event_id", event.ID, "customer_id", cust.ID)
	}
}
