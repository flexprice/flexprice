package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
	workflowModels "github.com/flexprice/flexprice/internal/temporal/models"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
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

	// Start temporal workflow to evaluate usage alerts
	if s.Config.UsageAlerts.Enabled {
		s.scheduleUsageAlertWorkflow(ctx, cust)
		return
	}
}

// runBulkMeterUsagePostInsertSideEffects is the batch-mode sibling of runMeterUsagePostInsertSideEffects.
// It fans out scheduleUsageAlertWorkflow once per unique external_customer_id in the successfully-inserted event set.
func (s *meterUsageTrackingService) runBulkMeterUsagePostInsertSideEffects(ctx context.Context, insertedEvents []*events.Event) {
	// NOTE: we're only running the usage alert workflow for now, so this check is for early return
	if !s.Config.UsageAlerts.Enabled || len(insertedEvents) == 0 {
		return
	}

	seen := make(map[string]struct{}, len(insertedEvents))
	for _, evt := range insertedEvents {
		if evt == nil || evt.ExternalCustomerID == "" {
			continue
		}
		if _, ok := seen[evt.ExternalCustomerID]; ok {
			continue
		}
		seen[evt.ExternalCustomerID] = struct{}{}

		cust, err := s.CustomerRepo.GetByLookupKey(ctx, evt.ExternalCustomerID)
		if err != nil {
			if ierr.IsNotFound(err) {
				continue
			}
			s.Logger.Error(ctx, "failed to resolve customer after bulk meter usage insert",
				"error", err,
				"external_customer_id", evt.ExternalCustomerID,
			)
			continue
		}
		if cust == nil {
			continue
		}

		s.scheduleUsageAlertWorkflow(ctx, cust)
	}
}

// scheduleUsageAlertWorkflow starts a debounced per-customer workflow. WorkflowID
// is stable per (tenant, env, customer); WorkflowExecutionAlreadyStarted is the
// dedup safety net on the Temporal side.
//
// A Redis throttle lock (TTL = schedule delay) keeps the Temporal RPC to one
// attempt per customer per window: an event at 5:02pm scheduling a 5:07pm run
// locks the customer until 5:07pm, so the burst in between never talks to
// Temporal. There is no "does this customer have alert configs" pre-check here —
// the workflow-side evaluators each bail on cheap indexed DB reads when there is
// nothing to do.
func (s *meterUsageTrackingService) scheduleUsageAlertWorkflow(ctx context.Context, cust *customer.Customer) {
	usageAlertConfig := s.Config.UsageAlerts
	if !usageAlertConfig.WalletAlertsEnabled && !usageAlertConfig.SpendAlertsEnabled && !usageAlertConfig.EntitlementAlertsEnabled {
		s.Logger.Debug(ctx, "none of the usage alerts are enabled, skipping", "customer_id", cust.ID)
		return
	}

	temporalSvc := temporalservice.GetGlobalTemporalService()
	if temporalSvc == nil {
		s.Logger.Debug(ctx, "temporal service not available, skipping usage alert workflow",
			"customer_id", cust.ID,
		)
		return
	}

	delay := usageAlertConfig.ScheduleDelay

	var throttleLock cache.Lock
	if s.Locker != nil {
		throttleKey := cache.GenerateKey(ctx, cache.PrefixUsageAlertSchedule, cust.ID)
		lock, err := s.Locker.AcquireLock(ctx, throttleKey, delay)
		if err != nil {
			// Fail open: a duplicate StartWorkflow is absorbed by AlreadyStarted.
			s.Logger.Error(ctx, "failed to acquire usage alert schedule lock, scheduling anyway", "error", err, "customer_id", cust.ID)
		} else if !lock.AcquiredSuccessfully() {
			return // already scheduled within this window
		} else {
			throttleLock = lock
		}
	}

	// Per-tenant gate: settings can turn any alert type off independently of the deployment-level config.
	walletEnabled, spendEnabled, entitlementEnabled := s.effectiveUsageAlertFlags(ctx)
	if !walletEnabled && !spendEnabled && !entitlementEnabled {
		if throttleLock != nil {
			if releaseErr := throttleLock.Release(ctx); releaseErr != nil {
				s.Logger.Error(ctx, "failed to release usage alert schedule lock", "error", releaseErr, "customer_id", cust.ID)
			}
		}
		s.Logger.Debug(ctx, "usage alerts disabled for tenant, skipping workflow", "customer_id", cust.ID)
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
		StartDelay: delay,
	}
	input := workflowModels.UsageAlertWorkflowInput{
		TenantID:                 tenantID,
		EnvironmentID:            envID,
		CustomerID:               cust.ID,
		ScheduledFor:             time.Now().UTC().Add(delay),
		StaleAfter:               usageAlertConfig.StaleAfter,
		WalletAlertsEnabled:      walletEnabled,
		SpendAlertsEnabled:       spendEnabled,
		EntitlementAlertsEnabled: entitlementEnabled,
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
		// Release the throttle lock so a later event in the window can retry the schedule.
		if throttleLock != nil {
			if releaseErr := throttleLock.Release(ctx); releaseErr != nil {
				s.Logger.Error(ctx, "failed to release usage alert schedule lock", "error", releaseErr, "customer_id", cust.ID)
			}
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
		"fires_in", delay.String(),
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

// effectiveUsageAlertFlags AND-combines the deployment-level config flags with the per-tenant setting toggles.
// Fail-closed: any settings-read error treats that setting as disabled (matches the wallet gate's behaviour and prevents a transient DB blip from mass-firing evaluations).
func (s *meterUsageTrackingService) effectiveUsageAlertFlags(ctx context.Context) (wallet, spend, entitlement bool) {
	settingsSvc := NewSettingsService(s.ServiceParams).(*settingsService)

	if s.Config.UsageAlerts.WalletAlertsEnabled {
		walletCfg, err := GetSetting[types.AlertSettings](settingsSvc, ctx, types.SettingKeyWalletBalanceAlertConfig)
		if err != nil && !ierr.IsNotFound(err) {
			s.Logger.Error(ctx, "usage alerts: failed to read wallet alert setting, treating as disabled",
				"error", err,
				"setting_key", types.SettingKeyWalletBalanceAlertConfig,
			)
		}
		wallet = walletCfg.IsAlertEnabled()
	}
	if s.Config.UsageAlerts.SpendAlertsEnabled {
		spendCfg, err := GetSetting[types.AlertToggleConfig](settingsSvc, ctx, types.SettingKeySubscriptionAlertConfig)
		if err != nil && !ierr.IsNotFound(err) {
			s.Logger.Error(ctx, "usage alerts: failed to read subscription alert setting, treating as disabled",
				"error", err,
				"setting_key", types.SettingKeySubscriptionAlertConfig,
			)
		}
		spend = spendCfg.IsAlertEnabled()
	}
	if s.Config.UsageAlerts.EntitlementAlertsEnabled {
		entCfg, err := GetSetting[types.AlertToggleConfig](settingsSvc, ctx, types.SettingKeyEntitlementAlertConfig)
		if err != nil && !ierr.IsNotFound(err) {
			s.Logger.Error(ctx, "usage alerts: failed to read entitlement alert setting, treating as disabled",
				"error", err,
				"setting_key", types.SettingKeyEntitlementAlertConfig,
			)
		}
		entitlement = entCfg.IsAlertEnabled()
	}
	return
}
