package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	workflowModels "github.com/flexprice/flexprice/internal/temporal/models"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"go.temporal.io/api/serviceerror"
)

// runMeterUsagePostInsertSideEffects runs customer resolution/onboarding and
// alert dispatch after meter_usage rows are written to ClickHouse. Failures are
// logged only; the Kafka message is not retried for side-effect errors.
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
		s.Logger.Debug(ctx, "no customer resolved after meter usage insert, skipping alerts",
			"event_id", event.ID,
			"external_customer_id", event.ExternalCustomerID,
		)
		return
	}

	if event.CustomerID == "" {
		event.CustomerID = cust.ID
	}

	// Debouncer supersedes the Kafka wallet-alert and inline spend-breach paths
	// with one deduped Temporal workflow per customer.
	if s.Config.MeterUsageTracking.AlertDebounceEnabled {
		s.scheduleUsageAlertWorkflow(ctx, cust)
		return
	}

	if s.Config.MeterUsageTracking.WalletAlertPushEnabled {
		s.publishWalletBalanceAlert(ctx, event, cust)
	}

	if s.Config.MeterUsageTracking.SpendAlertWebhookEnabled {
		meterIDs := lo.Uniq(lo.Map(records, func(r *events.MeterUsage, _ int) string { return r.MeterID }))
		NewAlertService(s.ServiceParams).EvaluateSpendBreachForEvent(ctx, event, cust, meterIDs)
	}
}

// scheduleUsageAlertWorkflow starts a debounced per-customer workflow. WorkflowID
// is stable per (tenant, env, customer); AlreadyStarted is the dedup signal.
// hasUsageAlertConfigForCustomer prevents OOMs for customers with no alert config.
func (s *meterUsageTrackingService) scheduleUsageAlertWorkflow(ctx context.Context, cust *customer.Customer) {
	temporalSvc := temporalservice.GetGlobalTemporalService()
	if temporalSvc == nil {
		s.Logger.Debug(ctx, "temporal service not available, skipping usage alert workflow",
			"customer_id", cust.ID,
		)
		return
	}

	if !s.hasUsageAlertConfigForCustomer(ctx, cust) {
		s.Logger.Debug(ctx, "no usage alert config for customer, skipping workflow schedule",
			"customer_id", cust.ID,
		)
		return
	}

	tenantID := types.GetTenantID(ctx)
	envID := types.GetEnvironmentID(ctx)
	workflowID := fmt.Sprintf("%s_%s_%s_%s_%s",
		types.UUID_PREFIX_WORKFLOW,
		types.TemporalUsageAlertWorkflow,
		tenantID,
		envID,
		cust.ID,
	)

	options := workflowModels.StartWorkflowOptions{
		ID:         workflowID,
		TaskQueue:  types.TemporalUsageAlertWorkflow.TaskQueueName(),
		StartDelay: s.Config.MeterUsageTracking.AlertDebounceWindow,
	}
	input := workflowModels.UsageAlertWorkflowInput{
		TenantID:      tenantID,
		EnvironmentID: envID,
		CustomerID:    cust.ID,
	}

	if _, err := temporalSvc.StartWorkflow(ctx, options, types.TemporalUsageAlertWorkflow, input); err != nil {
		var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
		if errors.As(err, &alreadyStarted) {
			s.Logger.Debug(ctx, "usage alert workflow already scheduled for customer, absorbed",
				"customer_id", cust.ID,
				"workflow_id", workflowID,
			)
			return
		}
		s.Logger.Error(ctx, "failed to schedule usage alert workflow",
			"error", err,
			"customer_id", cust.ID,
			"workflow_id", workflowID,
		)
		return
	}

	s.Logger.Debug(ctx, "usage alert workflow scheduled",
		"customer_id", cust.ID,
		"workflow_id", workflowID,
		"fires_in", s.Config.MeterUsageTracking.AlertDebounceWindow.String(),
	)
}

// hasUsageAlertConfigForCustomer is true when the customer has any spend-alert
// setting or a wallet under a tenant with wallet alerts on. Redis-cached with a
// short TTL. Fail-open on repo errors — a spurious workflow is cheaper than a miss.
func (s *meterUsageTrackingService) hasUsageAlertConfigForCustomer(ctx context.Context, cust *customer.Customer) bool {
	if cust == nil {
		return false
	}
	if v, ok := s.getUsageAlertGateCache(ctx, cust.ID); ok {
		return v
	}
	result := s.computeUsageAlertGate(ctx, cust)
	s.setUsageAlertGateCache(ctx, cust.ID, result)
	return result
}

func (s *meterUsageTrackingService) computeUsageAlertGate(ctx context.Context, cust *customer.Customer) bool {
	subIDs, err := s.activeSubscriptionIDsForCustomer(ctx, cust.ID)
	if err != nil {
		s.Logger.Error(ctx, "usage alert gate: subscription lookup failed, scheduling anyway",
			"error", err, "customer_id", cust.ID)
		return true
	}
	if len(subIDs) > 0 {
		hasSpendAlerts, err := s.hasEnabledSpendAlertConfig(ctx, subIDs)
		if err != nil {
			s.Logger.Error(ctx, "usage alert gate: alert_settings lookup failed, scheduling anyway",
				"error", err, "customer_id", cust.ID)
			return true
		}
		if hasSpendAlerts {
			return true
		}
	}

	needsWallet, err := s.customerNeedsWalletAlertCheck(ctx, cust.ID)
	if err != nil {
		s.Logger.Error(ctx, "usage alert gate: wallet lookup failed, scheduling anyway",
			"error", err, "customer_id", cust.ID)
		return true
	}
	return needsWallet
}

func (s *meterUsageTrackingService) getUsageAlertGateCache(ctx context.Context, customerID string) (bool, bool) {
	if s.RedisCache == nil || !s.RedisCache.IsEnabled() {
		return false, false
	}
	raw, ok := s.RedisCache.Get(ctx, usageAlertGateCacheKey(ctx, customerID))
	if !ok {
		return false, false
	}
	v, ok := raw.(bool)
	return v, ok
}

func (s *meterUsageTrackingService) setUsageAlertGateCache(ctx context.Context, customerID string, v bool) {
	if s.RedisCache == nil || !s.RedisCache.IsEnabled() {
		return
	}
	s.RedisCache.Set(ctx, usageAlertGateCacheKey(ctx, customerID), v, cache.ExpiryUsageAlertGate)
}

func usageAlertGateCacheKey(ctx context.Context, customerID string) string {
	return cache.GenerateKey(ctx, cache.PrefixUsageAlertGate, customerID)
}

func (s *meterUsageTrackingService) activeSubscriptionIDsForCustomer(ctx context.Context, customerID string) ([]string, error) {
	filter := types.NewNoLimitSubscriptionFilter()
	filter.CustomerID = customerID
	filter.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusTrialing,
	}
	subs, err := s.SubRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(subs))
	for _, sub := range subs {
		if sub != nil && sub.ID != "" {
			ids = append(ids, sub.ID)
		}
	}
	return ids, nil
}

// hasEnabledSpendAlertConfig counts subscription-scope and parent-scope alert
// settings separately: subscription-scoped rows live on entity_id, line-item
// and group scopes live on parent_entity_id.
func (s *meterUsageTrackingService) hasEnabledSpendAlertConfig(ctx context.Context, subIDs []string) (bool, error) {
	if len(subIDs) == 0 {
		return false, nil
	}
	enabled := lo.ToPtr(true)

	n, err := s.AlertRepo.Count(ctx, &types.AlertSettingsFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		EntityType:  types.AlertEntityTypeSubscription,
		EntityIDs:   subIDs,
		Enabled:     enabled,
	})
	if err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}

	n, err = s.AlertRepo.Count(ctx, &types.AlertSettingsFilter{
		QueryFilter:      types.NewNoLimitQueryFilter(),
		ParentEntityType: types.AlertEntityTypeSubscription,
		ParentEntityIDs:  subIDs,
		Enabled:          enabled,
	})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// customerNeedsWalletAlertCheck: tenant setting enabled AND customer has any wallet.
func (s *meterUsageTrackingService) customerNeedsWalletAlertCheck(ctx context.Context, customerID string) (bool, error) {
	settingsSvc := &settingsService{ServiceParams: s.ServiceParams}
	cfg, err := GetSetting[types.AlertSettings](settingsSvc, ctx, types.SettingKeyWalletBalanceAlertConfig)
	if err != nil {
		return false, nil
	}
	if !cfg.IsAlertEnabled() {
		return false, nil
	}
	wallets, err := s.WalletRepo.GetWalletsByCustomerID(ctx, customerID)
	if err != nil {
		return false, err
	}
	return len(wallets) > 0, nil
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
