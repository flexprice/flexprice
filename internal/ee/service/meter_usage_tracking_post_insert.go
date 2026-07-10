package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	workflowModels "github.com/flexprice/flexprice/internal/temporal/models"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
)

// runMeterUsagePostInsertSideEffects runs customer resolution/onboarding and wallet
// balance alert publishing after meter_usage rows are written to ClickHouse.
// Failures are logged only; the Kafka message is not retried for side-effect errors.
func (s *meterUsageTrackingService) runMeterUsagePostInsertSideEffects(ctx context.Context, event *events.Event) {
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

	if !s.Config.MeterUsageTracking.WalletAlertPushEnabled {
		s.Logger.Debug(ctx, "wallet balance alert push disabled for meter usage tracking",
			"event_id", event.ID,
			"customer_id", cust.ID,
		)
		return
	}

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
func executeCustomerOnboardingForEvent(
	ctx context.Context,
	params ServiceParams,
	event *events.Event,
) (*customer.Customer, error) {
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
