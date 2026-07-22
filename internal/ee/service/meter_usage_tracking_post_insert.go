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
	if s.Config.UsageAlerts.Enabled {
		s.scheduleUsageAlertWorkflow(ctx, cust)
		return
	}

	if s.Config.MeterUsageTracking.WalletAlertPushEnabled {
		s.publishWalletBalanceAlert(ctx, event, cust)
	}

	if s.Config.MeterUsageTracking.SpendAlertWebhookEnabled {
		NewAlertService(s.ServiceParams).EvaluateSpendBreachForEvent(ctx, event, cust)
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
	temporalSvc := temporalservice.GetGlobalTemporalService()
	if temporalSvc == nil {
		s.Logger.Debug(ctx, "temporal service not available, skipping usage alert workflow",
			"customer_id", cust.ID,
		)
		return
	}

	delay := s.Config.UsageAlerts.ScheduleDelay

	var throttleLock cache.Lock
	if s.Locker != nil {
		throttleKey := cache.GenerateKey(ctx, cache.PrefixUsageAlertSchedule, cust.ID)
		lock, err := s.Locker.AcquireLock(ctx, throttleKey, delay)
		if err != nil {
			// Fail open: a duplicate StartWorkflow is absorbed by AlreadyStarted.
			s.Logger.Error(ctx, "failed to acquire usage alert schedule lock, scheduling anyway",
				"error", err, "customer_id", cust.ID)
		} else if !lock.AcquiredSuccessfully() {
			return // already scheduled within this window
		} else {
			throttleLock = lock
		}
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
		TenantID:             tenantID,
		EnvironmentID:        envID,
		CustomerID:           cust.ID,
		ActivityStaleAfter:   s.Config.UsageAlerts.ActivityStaleAfter,
		StaleRescheduleDelay: s.Config.UsageAlerts.StaleRescheduleDelay,
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
				s.Logger.Error(ctx, "failed to release usage alert schedule lock",
					"error", releaseErr, "customer_id", cust.ID)
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
