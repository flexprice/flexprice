package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainAlert "github.com/flexprice/flexprice/internal/domain/alert"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	workflowModels "github.com/flexprice/flexprice/internal/temporal/models"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"go.temporal.io/api/serviceerror"
)

// runMeterUsagePostInsertSideEffects runs customer resolution/onboarding and wallet
// balance alert publishing after meter_usage rows are written to ClickHouse.
// Failures are logged only; the Kafka message is not retried for side-effect errors.
func (s *meterUsageTrackingService) runMeterUsagePostInsertSideEffects(ctx context.Context, event *events.Event, records []*events.MeterUsage) {
	if event == nil || event.ExternalCustomerID == "" {
		return
	}

	cust, err := ResolveCustomerForUsageEvent(ctx, s.ServiceParams, event)
	if err != nil {
		s.Logger.Error(ctx, "failed to resolve customer after meter usage insert",
			"error", err,
			"event_id", event.ID,
			"external_customer_id", event.ExternalCustomerID,
		)
		return
	}
	if cust == nil {
		s.Logger.Debug(ctx, "no customer resolved after meter usage insert, skipping wallet alert",
			"event_id", event.ID,
			"external_customer_id", event.ExternalCustomerID,
		)
		return
	}

	if event.CustomerID == "" {
		event.CustomerID = cust.ID
	}

	// When the debouncer is on it supersedes the Kafka wallet-alert path and the
	// inline spend-breach check — a single Temporal workflow (deduped per customer)
	// runs both checks once per debounce window.
	if s.Config.MeterUsageTracking.AlertDebounceEnabled {
		s.scheduleMeterUsageAlertWorkflow(ctx, cust)
		return
	}

	if s.Config.MeterUsageTracking.WalletAlertPushEnabled {
		s.publishWalletBalanceAlert(ctx, event, cust)
	}

	if s.Config.MeterUsageTracking.SpendAlertWebhookEnabled {
		meterIDs := lo.Uniq(lo.Map(records, func(r *events.MeterUsage, _ int) string { return r.MeterID }))
		s.checkSpendBreachForEvent(ctx, event, meterIDs, cust)
	}
}

// scheduleMeterUsageAlertWorkflow starts a debounced Temporal workflow that runs
// spend-breach + wallet-balance checks for the customer after Config.AlertDebounceWindow.
// The workflow ID is stable per (tenant, environment, customer); when a workflow with
// that ID is already running (armed but not yet fired) Temporal rejects the start with
// WorkflowExecutionAlreadyStarted — that error IS the dedupe signal and is swallowed.
func (s *meterUsageTrackingService) scheduleMeterUsageAlertWorkflow(ctx context.Context, cust *customer.Customer) {
	temporalSvc := temporalservice.GetGlobalTemporalService()
	if temporalSvc == nil {
		s.Logger.Debug(ctx, "temporal service not available, skipping meter usage alert workflow",
			"customer_id", cust.ID,
		)
		return
	}

	tenantID := types.GetTenantID(ctx)
	envID := types.GetEnvironmentID(ctx)
	workflowID := fmt.Sprintf("%s_%s_%s_%s_%s",
		types.UUID_PREFIX_WORKFLOW,
		types.TemporalMeterUsageAlertWorkflow,
		tenantID,
		envID,
		cust.ID,
	)

	options := workflowModels.StartWorkflowOptions{
		ID:         workflowID,
		TaskQueue:  types.TemporalMeterUsageAlertWorkflow.TaskQueueName(),
		StartDelay: s.Config.MeterUsageTracking.AlertDebounceWindow,
	}
	input := workflowModels.MeterUsageAlertWorkflowInput{
		TenantID:      tenantID,
		EnvironmentID: envID,
		CustomerID:    cust.ID,
	}

	if _, err := temporalSvc.StartWorkflow(ctx, options, types.TemporalMeterUsageAlertWorkflow, input); err != nil {
		var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
		if errors.As(err, &alreadyStarted) {
			s.Logger.Debug(ctx, "meter usage alert workflow already scheduled for customer, absorbed",
				"customer_id", cust.ID,
				"workflow_id", workflowID,
			)
			return
		}
		s.Logger.Error(ctx, "failed to schedule meter usage alert workflow",
			"error", err,
			"customer_id", cust.ID,
			"workflow_id", workflowID,
		)
		return
	}

	s.Logger.Debug(ctx, "meter usage alert workflow scheduled",
		"customer_id", cust.ID,
		"workflow_id", workflowID,
		"fires_in", s.Config.MeterUsageTracking.AlertDebounceWindow.String(),
	)
}

// publishWalletBalanceAlert publishes a wallet balance alert for the given event and customer.
func (s *meterUsageTrackingService) publishWalletBalanceAlert(ctx context.Context, event *events.Event, cust *customer.Customer) {
	alertEvent := &wallet.WalletBalanceAlertEvent{
		ID:                    types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_ALERT),
		Timestamp:             time.Now().UTC(),
		Source:                EventSourceMeterUsage,
		CustomerID:            cust.ID,
		ForceCalculateBalance: false,
		TenantID:              types.GetTenantID(ctx),
		EnvironmentID:         types.GetEnvironmentID(ctx),
	}

	walletBalanceAlertService := NewWalletBalanceAlertService(s.ServiceParams)
	if err := walletBalanceAlertService.PublishEvent(ctx, alertEvent); err != nil {
		s.Logger.Error(ctx, "failed to publish wallet balance alert after meter usage insert",
			"error", err,
			"event_id", event.ID,
			"customer_id", cust.ID,
			"alert_event_id", alertEvent.ID,
		)
		return
	}

	s.Logger.Debug(ctx, "wallet balance alert published after meter usage insert",
		"event_id", event.ID,
		"customer_id", cust.ID,
		"alert_event_id", alertEvent.ID,
	)
}

// ResolveCustomerForUsageEvent looks up the customer by external_customer_id and,
// when missing, optionally runs the tenant's customer onboarding Temporal workflow.
// Returns (nil, nil) when the customer does not exist and onboarding is not configured.
func ResolveCustomerForUsageEvent(
	ctx context.Context,
	params ServiceParams,
	event *events.Event,
) (*customer.Customer, error) {
	if event == nil || event.ExternalCustomerID == "" {
		return nil, nil
	}

	cust, err := params.CustomerRepo.GetByLookupKey(ctx, event.ExternalCustomerID)
	if err == nil {
		return cust, nil
	}
	if !ierr.IsNotFound(err) {
		return nil, err
	}

	params.Logger.Debug(ctx, "customer not found for event, attempting onboarding workflow",
		"event_id", event.ID,
		"external_customer_id", event.ExternalCustomerID,
	)

	return executeCustomerOnboardingForEvent(ctx, params, event)
}

// executeCustomerOnboardingForEvent runs the synchronous CustomerOnboarding workflow
// when the tenant has customer_onboarding_config with create_customer as the first action.
func executeCustomerOnboardingForEvent(ctx context.Context, params ServiceParams, event *events.Event) (*customer.Customer, error) {
	settingsService := &settingsService{ServiceParams: params}
	workflowConfig, err := GetSetting[*workflowModels.WorkflowConfig](
		settingsService,
		ctx,
		types.SettingKeyCustomerOnboarding,
	)
	if err != nil {
		params.Logger.Debug(ctx, "failed to get workflow config",
			"event_id", event.ID,
			"error", err,
		)
		return nil, nil
	}

	if workflowConfig == nil || len(workflowConfig.Actions) == 0 {
		params.Logger.Debug(ctx, "no workflow config found for customer onboarding",
			"event_id", event.ID,
		)
		return nil, nil
	}

	hasCreateCustomer := len(workflowConfig.Actions) > 0 &&
		workflowConfig.Actions[0].GetAction() == workflowModels.WorkflowActionCreateCustomer
	if !hasCreateCustomer {
		params.Logger.Debug(ctx, "workflow config does not have create_customer as first action",
			"event_id", event.ID,
		)
		return nil, nil
	}

	params.Logger.Info(ctx, "executing customer onboarding workflow synchronously",
		"event_id", event.ID,
		"external_customer_id", event.ExternalCustomerID,
		"action_count", len(workflowConfig.Actions),
	)

	input := &workflowModels.CustomerOnboardingWorkflowInput{
		ExternalCustomerID: event.ExternalCustomerID,
		EventTimestamp:     &event.Timestamp,
		TenantID:           types.GetTenantID(ctx),
		EnvironmentID:      types.GetEnvironmentID(ctx),
		UserID:             types.GetUserID(ctx),
		WorkflowConfig:     *workflowConfig,
	}

	if err := input.Validate(); err != nil {
		params.Logger.Error(ctx, "invalid workflow input for customer onboarding",
			"error", err,
			"event_id", event.ID,
			"external_customer_id", event.ExternalCustomerID,
		)
		return nil, ierr.WithError(err).
			WithHint("Invalid workflow input for customer onboarding").
			WithReportableDetails(map[string]interface{}{
				"event_id":             event.ID,
				"external_customer_id": event.ExternalCustomerID,
			}).
			Mark(ierr.ErrValidation)
	}

	temporalSvc := temporalservice.GetGlobalTemporalService()
	if temporalSvc == nil {
		return nil, ierr.NewError("temporal service not available").
			WithHint("Customer onboarding workflow requires Temporal service").
			WithReportableDetails(map[string]interface{}{
				"event_id":             event.ID,
				"external_customer_id": event.ExternalCustomerID,
			}).
			Mark(ierr.ErrInternal)
	}

	result, err := temporalSvc.ExecuteWorkflowSync(
		ctx,
		types.TemporalCustomerOnboardingWorkflow,
		input,
		30,
	)
	if err != nil {
		params.Logger.Error(ctx, "failed to execute customer onboarding workflow synchronously",
			"error", err,
			"event_id", event.ID,
			"external_customer_id", event.ExternalCustomerID,
		)
		return nil, ierr.WithError(err).
			WithHint("Failed to execute customer onboarding workflow").
			WithReportableDetails(map[string]interface{}{
				"event_id":             event.ID,
				"external_customer_id": event.ExternalCustomerID,
			}).
			Mark(ierr.ErrInternal)
	}

	workflowResult, ok := result.(*workflowModels.CustomerOnboardingWorkflowResult)
	if !ok {
		return nil, ierr.NewError("invalid workflow result type").
			WithHint("Expected CustomerOnboardingWorkflowResult").
			WithReportableDetails(map[string]interface{}{
				"event_id":             event.ID,
				"external_customer_id": event.ExternalCustomerID,
			}).
			Mark(ierr.ErrInternal)
	}

	if workflowResult.Status != "completed" {
		errorMsg := "workflow did not complete successfully"
		if workflowResult.ErrorSummary != nil {
			errorMsg = *workflowResult.ErrorSummary
		}
		return nil, ierr.NewError(errorMsg).
			WithHint("Customer onboarding workflow failed").
			WithReportableDetails(map[string]interface{}{
				"event_id":             event.ID,
				"external_customer_id": event.ExternalCustomerID,
				"workflow_status":      workflowResult.Status,
				"actions_executed":     workflowResult.ActionsExecuted,
			}).
			Mark(ierr.ErrInternal)
	}

	var customerID string
	for _, actionResult := range workflowResult.Results {
		if actionResult.ActionType == workflowModels.WorkflowActionCreateCustomer &&
			actionResult.Status == workflowModels.WorkflowStatusCompleted &&
			actionResult.ResourceID != "" {
			customerID = actionResult.ResourceID
			break
		}
	}

	if customerID == "" {
		return nil, ierr.NewError("customer ID not found in workflow results").
			WithHint("Workflow completed but customer was not created").
			WithReportableDetails(map[string]interface{}{
				"event_id":             event.ID,
				"external_customer_id": event.ExternalCustomerID,
			}).
			Mark(ierr.ErrInternal)
	}

	createdCustomer, err := params.CustomerRepo.Get(ctx, customerID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to fetch created customer").
			WithReportableDetails(map[string]interface{}{
				"event_id":             event.ID,
				"external_customer_id": event.ExternalCustomerID,
				"customer_id":          customerID,
			}).
			Mark(ierr.ErrDatabase)
	}

	params.Logger.Info(ctx, "customer onboarding workflow completed successfully",
		"event_id", event.ID,
		"external_customer_id", event.ExternalCustomerID,
		"customer_id", customerID,
		"actions_executed", workflowResult.ActionsExecuted,
	)

	return createdCustomer, nil
}

// checkSpendBreachForEvent checks every subscription this event's usage touches against its
// configured spend thresholds — subscription total, a single line item, and/or a feature group —
// and records any state change through alertLogsSvc.LogAlert, which handles the actual webhook dispatch.
func (s *meterUsageTrackingService) checkSpendBreachForEvent(ctx context.Context, event *events.Event, meterIDs []string, cust *customer.Customer) {
	evaluateSpendBreachesForCustomer(ctx, s.ServiceParams, cust, meterIDs, &event.Timestamp, event.ID)
}

// CheckSpendBreachForCustomer evaluates spend thresholds for every currently-active
// subscription line item on the customer, ignoring any meter filter. Used by the
// MeterUsageAlertWorkflow debouncer where the exact set of meters that fired during
// the debounce window isn't tracked — a full rescan is both simpler and correct.
func CheckSpendBreachForCustomer(ctx context.Context, params ServiceParams, cust *customer.Customer) {
	evaluateSpendBreachesForCustomer(ctx, params, cust, nil, nil, "")
}

// evaluateSpendBreachesForCustomer is the shared body used by both the sync
// per-event check and the debounced customer-level check. meterIDs and periodStart
// are optional filters; logEventID is a correlation hint for logs.
func evaluateSpendBreachesForCustomer(ctx context.Context, params ServiceParams, cust *customer.Customer, meterIDs []string, periodStart *time.Time, logEventID string) {
	// Resolves the active subscription line items to evaluate. Passing nil meterIDs
	// / nil periodStart returns every active line item on the customer — that's the
	// debouncer path. The per-event path narrows to the touched meters and period.
	affectedLineItems, err := params.SubscriptionLineItemRepo.List(ctx, &types.SubscriptionLineItemFilter{
		QueryFilter:        types.NewNoLimitQueryFilter(),
		CustomerIDs:        []string{cust.ID},
		MeterIDs:           meterIDs,
		ActiveFilter:       true,
		CurrentPeriodStart: periodStart,
	})
	if err != nil {
		params.Logger.Error(ctx, "failed to list affected line items for spend alert evaluation", "error", err, "event_id", logEventID, "customer_id", cust.ID)
		return
	}
	if len(affectedLineItems) == 0 {
		return
	}

	// A customer may have multiple subscriptions, and a single event can affect line items across
	// more than one of them. Each affected subscription is evaluated independently below.
	subscriptionIDs := lo.Uniq(lo.Map(affectedLineItems, func(li *subscription.SubscriptionLineItem, _ int) string {
		return li.SubscriptionID
	}))

	// Fetches every enabled alert configuration for all affected subscriptions.
	// TODO: can fetch all alert settings for all affected subscriptions in one query (using parent_entity_id and parent_entity_type)
	allSubCfgs, err := params.AlertRepo.List(ctx, &types.AlertSettingsFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		EntityType:  types.AlertEntityTypeSubscription,
		EntityIDs:   subscriptionIDs,
		Enabled:     lo.ToPtr(true),
	})
	if err != nil {
		params.Logger.Error(ctx, "failed to list subscription alert settings", "error", err, "event_id", logEventID)
		return
	}
	allLineItemCfgs, err := params.AlertRepo.List(ctx, &types.AlertSettingsFilter{
		QueryFilter:      types.NewNoLimitQueryFilter(),
		EntityType:       types.AlertEntityTypeSubscriptionLineItem,
		ParentEntityType: types.AlertEntityTypeSubscription,
		ParentEntityIDs:  subscriptionIDs,
		Enabled:          lo.ToPtr(true),
	})
	if err != nil {
		params.Logger.Error(ctx, "failed to list line item alert settings", "error", err, "event_id", logEventID)
		return
	}
	allGroupCfgs, err := params.AlertRepo.List(ctx, &types.AlertSettingsFilter{
		QueryFilter:      types.NewNoLimitQueryFilter(),
		EntityType:       types.AlertEntityTypeGroup,
		ParentEntityType: types.AlertEntityTypeSubscription,
		ParentEntityIDs:  subscriptionIDs,
		Enabled:          lo.ToPtr(true),
	})
	if err != nil {
		params.Logger.Error(ctx, "failed to list group alert settings", "error", err, "event_id", logEventID)
		return
	}

	params.Logger.Debug(ctx, "batched alert settings fetched",
		"event_id", logEventID,
		"subscription_ids", subscriptionIDs,
		"sub_cfg_count", len(allSubCfgs),
		"line_item_cfg_count", len(allLineItemCfgs),
		"group_cfg_count", len(allGroupCfgs),
	)

	if len(allSubCfgs) == 0 && len(allLineItemCfgs) == 0 && len(allGroupCfgs) == 0 {
		// No alert is configured for any affected subscription. Returns before any billing or
		// ClickHouse work is performed.
		return
	}

	alertLogsSvc := NewAlertLogsService(params)
	subscriptionSvc := NewSubscriptionService(params)
	billingSvc := NewBillingService(params)
	now := time.Now().UTC()

	// Evaluates each affected subscription independently.
	for _, subscriptionID := range subscriptionIDs {
		// Filters the batched configuration lookups down to this subscription.
		// At most one subscription-level configuration can exist per subscription; line-item and group configurations may be multiple.
		var subscriptionCfg *domainAlert.AlertSettings
		for _, c := range allSubCfgs {
			if c.EntityID == subscriptionID {
				subscriptionCfg = c
				break
			}
		}
		lineItemCfgs := lo.Filter(allLineItemCfgs, func(c *domainAlert.AlertSettings, _ int) bool {
			return c.ParentEntityID != nil && *c.ParentEntityID == subscriptionID
		})
		groupCfgsForSub := lo.Filter(allGroupCfgs, func(c *domainAlert.AlertSettings, _ int) bool {
			return c.ParentEntityID != nil && *c.ParentEntityID == subscriptionID
		})

		params.Logger.Debug(ctx, "per-subscription alert config filter result",
			"subscription_id", subscriptionID,
			"has_subscription_cfg", subscriptionCfg != nil,
			"line_item_cfg_count", len(lineItemCfgs),
			"group_cfg_count", len(groupCfgsForSub),
		)

		if subscriptionCfg == nil && len(lineItemCfgs) == 0 && len(groupCfgsForSub) == 0 {
			continue
		}

		// Fetches the subscription with its line items populated.
		sub, _, err := params.SubRepo.GetWithLineItems(ctx, subscriptionID)
		if err != nil {
			params.Logger.Error(ctx, "failed to get subscription for spend alert evaluation", "error", err, "subscription_id", subscriptionID)
			continue
		}

		// Reads accumulated usage for the current billing period, from CurrentPeriodStart through
		// now. The threshold comparison is against the cumulative period total, not a single
		// event's increment.
		usage, err := subscriptionSvc.GetMeterUsageBySubscription(ctx, &dto.GetUsageBySubscriptionRequest{
			SubscriptionID: subscriptionID,
			StartTime:      sub.CurrentPeriodStart,
			EndTime:        now,
			Source:         string(types.UsageSourceInvoiceCreation),
		})
		if err != nil {
			params.Logger.Error(ctx, "failed to get meter usage for spend alert evaluation", "error", err, "subscription_id", subscriptionID)
			continue
		}

		// Uses the same invoicing-grade computation as real invoice generation — commitment- and
		// overage-aware — so the value compared against thresholds matches what the customer is
		// actually billed. Returns both the per-line-item breakdown (usageCharges, used by Parts B
		// and C) and the subscription total (totalUsageCost, used by Part A) from one billing call.
		usageCharges, totalUsageCost, err := billingSvc.CalculateMeterUsageCharges(
			ctx, sub, usage, sub.CurrentPeriodStart, now, types.UsageSourceInvoiceCreation,
		)
		if err != nil {
			params.Logger.Error(ctx, "failed to calculate meter usage charges for spend alert evaluation", "error", err, "subscription_id", subscriptionID)
			continue
		}

		// --- Part A: subscription-level threshold ---
		// Total usage cost across every metered line item on the whole subscription, compared
		// against this one subscription-level configuration (at most one can exist).
		if subscriptionCfg != nil {
			state, err := subscriptionCfg.Config.AlertState(totalUsageCost)
			if err != nil {
				params.Logger.Error(ctx, "failed to determine subscription spend alert state", "error", err, "subscription_id", subscriptionID)
			} else if err := alertLogsSvc.LogAlert(ctx, &LogAlertRequest{
				AlertSettingID: &subscriptionCfg.ID,
				PeriodStart:    &sub.CurrentPeriodStart,
				EntityType:     types.AlertEntityTypeSubscription,
				EntityID:       subscriptionID,
				CustomerID:     &cust.ID,
				AlertType:      types.AlertTypeSubscriptionSpend,
				AlertStatus:    state,
				AlertInfo: types.AlertInfo{
					AlertSettings: subscriptionCfg.Config,
					ValueAtTime:   totalUsageCost,
					Timestamp:     now,
				},
			}); err != nil {
				params.Logger.Error(ctx, "failed to log subscription spend alert", "error", err, "subscription_id", subscriptionID)
			}
		}

		// Avoids re-scanning the flat charge slice for every lookup.
		chargesBySubLiItem := make(map[string]decimal.Decimal, len(usageCharges))
		for _, c := range usageCharges {
			if c.SubscriptionLineItemID != nil {
				chargesBySubLiItem[*c.SubscriptionLineItemID] = c.Amount
			}
		}

		// --- Part B: subscription line item-level thresholds ---
		for _, cfg := range lineItemCfgs {
			amount, found := chargesBySubLiItem[cfg.EntityID]
			if !found {
				continue
			}
			state, err := cfg.Config.AlertState(amount)
			if err != nil {
				params.Logger.Error(ctx, "failed to determine line item spend alert state", "error", err, "subscription_line_item_id", cfg.EntityID)
				continue
			}
			parentEntityType := string(types.AlertEntityTypeSubscription)
			if err := alertLogsSvc.LogAlert(ctx, &LogAlertRequest{
				AlertSettingID:   &cfg.ID,
				PeriodStart:      &sub.CurrentPeriodStart,
				EntityType:       types.AlertEntityTypeSubscriptionLineItem,
				EntityID:         cfg.EntityID,
				ParentEntityType: &parentEntityType,
				ParentEntityID:   &subscriptionID,
				CustomerID:       &cust.ID,
				AlertType:        types.AlertTypeSubscriptionLineItemSpend,
				AlertStatus:      state,
				AlertInfo: types.AlertInfo{
					AlertSettings: cfg.Config,
					ValueAtTime:   amount,
					Timestamp:     now,
				},
			}); err != nil {
				params.Logger.Error(ctx, "failed to log line item spend alert", "error", err, "subscription_line_item_id", cfg.EntityID)
			}
		}

		// --- Part C: group-level thresholds ---
		// A group's spend is the sum of every line item on the subscription whose feature belongs to that group.
		if len(groupCfgsForSub) == 0 {
			continue
		}

		// Resolves features for every meter this subscription bills on (sub.LineItems, populated by GetWithLineItems above).
		// A group's total must include every line item in the group, not only the line item(s) on the meter this specific event touched,
		// since a group can span multiple meters.
		subMeterIDs := lo.Uniq(lo.Map(sub.LineItems, func(li *subscription.SubscriptionLineItem, _ int) string {
			return li.MeterID
		}))
		subFeatures, err := params.FeatureRepo.List(ctx, &types.FeatureFilter{
			QueryFilter: types.NewNoLimitQueryFilter(),
			MeterIDs:    subMeterIDs,
		})
		if err != nil {
			params.Logger.Error(ctx, "failed to list features for group spend summation", "error", err, "subscription_id", subscriptionID)
			continue
		}
		featuresByMeterID := make(map[string]*feature.Feature, len(subFeatures))
		for _, f := range subFeatures {
			featuresByMeterID[f.MeterID] = f
		}

		// Sums each line item's charge into its feature's group in a single pass over
		// sub.LineItems, rather than rescanning all of them once per configured group below
		groupTotals := make(map[string]decimal.Decimal, len(groupCfgsForSub))
		for _, li := range sub.LineItems {
			f, ok := featuresByMeterID[li.MeterID]
			if !ok || f.GroupID == "" {
				continue
			}
			amount, found := chargesBySubLiItem[li.ID]
			if !found {
				continue
			}
			groupTotals[f.GroupID] = groupTotals[f.GroupID].Add(amount)
		}

		// Evaluates each configured group against its own threshold.
		for _, cfg := range groupCfgsForSub {
			groupTotal := groupTotals[cfg.EntityID]
			state, err := cfg.Config.AlertState(groupTotal)
			if err != nil {
				params.Logger.Error(ctx, "failed to determine group spend alert state", "error", err, "group_id", cfg.EntityID)
				continue
			}
			parentEntityType := string(types.AlertEntityTypeSubscription)
			if err := alertLogsSvc.LogAlert(ctx, &LogAlertRequest{
				AlertSettingID:   &cfg.ID,
				PeriodStart:      &sub.CurrentPeriodStart,
				EntityType:       types.AlertEntityTypeGroup,
				EntityID:         cfg.EntityID,
				ParentEntityType: &parentEntityType,
				ParentEntityID:   &subscriptionID,
				CustomerID:       &cust.ID,
				AlertType:        types.AlertTypeSubscriptionGroupSpend,
				AlertStatus:      state,
				AlertInfo: types.AlertInfo{
					AlertSettings: cfg.Config,
					ValueAtTime:   groupTotal,
					Timestamp:     now,
				},
			}); err != nil {
				params.Logger.Error(ctx, "failed to log group spend alert", "error", err, "group_id", cfg.EntityID)
			}
		}
	}
}

// CheckWalletBalanceForCustomer runs the wallet balance alert check for the customer.
// Used by the MeterUsageAlertWorkflow debouncer; ForceCalculateBalance bypasses the
// in-memory throttle in walletService.CheckWalletBalanceAlert since Temporal already
// dedupes the calls (one per customer per debounce window).
func CheckWalletBalanceForCustomer(ctx context.Context, params ServiceParams, cust *customer.Customer) error {
	alertEvent := &wallet.WalletBalanceAlertEvent{
		ID:                    types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_ALERT),
		Timestamp:             time.Now().UTC(),
		Source:                EventSourceMeterUsage,
		CustomerID:            cust.ID,
		ForceCalculateBalance: true,
		TenantID:              types.GetTenantID(ctx),
		EnvironmentID:         types.GetEnvironmentID(ctx),
	}
	walletSvc := NewWalletService(params)
	return walletSvc.CheckWalletBalanceAlert(ctx, alertEvent)
}
