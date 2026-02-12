package workflows

import (
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// Workflow name - must match the function name
	WorkflowCustomerOnboarding = "CustomerOnboardingWorkflow"
	// Activity names - must match the registered method names
	ActivityCreateCustomer     = "CreateCustomerActivity"
	ActivityCreateWallet       = "CreateWalletActivity"
	ActivityCreateSubscription = "CreateSubscriptionActivity"
)

// CustomerOnboardingWorkflow orchestrates the customer onboarding process
func CustomerOnboardingWorkflow(ctx workflow.Context, input models.CustomerOnboardingWorkflowInput) (*models.CustomerOnboardingWorkflowResult, error) {
	// Validate input
	if err := input.Validate(); err != nil {
		return nil, err
	}

	logger := workflow.GetLogger(ctx)
	logger.Info("Starting customer onboarding workflow",
		"customer_id", input.CustomerID,
		"action_count", len(input.WorkflowConfig.Actions))

	// Define activity options
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 10, // 10 minutes for each activity
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 5,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute * 2,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	// Initialize result
	result := &models.CustomerOnboardingWorkflowResult{
		CustomerID:      input.CustomerID,
		Status:          "processing",
		ActionsExecuted: 0,
		Results:         make([]models.CustomerOnboardingActionResult, 0, len(input.WorkflowConfig.Actions)),
	}

	// Execute each action in sequence
	for i, action := range input.WorkflowConfig.Actions {
		actionType := action.GetAction()
		logger.Info("Executing workflow action",
			"customer_id", input.CustomerID,
			"action_index", i,
			"action_type", actionType)

		actionResult := models.CustomerOnboardingActionResult{
			ActionType:  actionType,
			ActionIndex: i,
			Status:      "processing",
		}

		var err error
		switch actionType {
		case models.WorkflowActionCreateCustomer:
			err = executeCreateCustomerAction(ctx, input, action, &actionResult)
			// If customer creation succeeds, update the input.CustomerID for subsequent actions
			if err == nil && actionResult.ResourceID != "" {
				input.CustomerID = actionResult.ResourceID
			}

		case models.WorkflowActionCreateWallet:
			err = executeCreateWalletAction(ctx, input, action, &actionResult)

		case models.WorkflowActionCreateSubscription:
			err = executeCreateSubscriptionAction(ctx, input, action, &actionResult)

		default:
			logger.Warn("Unknown workflow action type",
				"customer_id", input.CustomerID,
				"action_type", actionType,
				"action_index", i)

			actionResult.Status = models.WorkflowStatusFailed
			errorMsg := "unknown workflow action type: " + string(actionType)
			actionResult.Error = &errorMsg
			result.Results = append(result.Results, actionResult)

			result.Status = models.WorkflowStatusFailed
			result.CompletedAt = workflow.Now(ctx)
			result.ErrorSummary = &errorMsg
			return result, nil
		}

		if err != nil {
			logger.Error("Workflow action failed",
				"customer_id", input.CustomerID,
				"action_index", i,
				"action_type", actionType,
				"error", err)

			actionResult.Status = models.WorkflowStatusFailed
			errorMsg := err.Error()
			actionResult.Error = &errorMsg
			result.Results = append(result.Results, actionResult)

			result.Status = models.WorkflowStatusFailed
			result.CompletedAt = workflow.Now(ctx)
			result.ErrorSummary = &errorMsg
			return result, nil
		}

		actionResult.Status = models.WorkflowStatusCompleted
		result.Results = append(result.Results, actionResult)
		result.ActionsExecuted++

		logger.Info("Workflow action completed successfully",
			"customer_id", input.CustomerID,
			"action_index", i,
			"action_type", actionType,
			"resource_id", actionResult.ResourceID)
	}

	// All actions completed successfully
	result.Status = models.WorkflowStatusCompleted
	result.CompletedAt = workflow.Now(ctx)

	logger.Info("Customer onboarding workflow completed successfully",
		"customer_id", input.CustomerID,
		"actions_executed", result.ActionsExecuted)

	return result, nil
}

// executeCreateCustomerAction executes the create customer action
func executeCreateCustomerAction(
	ctx workflow.Context,
	input models.CustomerOnboardingWorkflowInput,
	action models.WorkflowActionConfig,
	actionResult *models.CustomerOnboardingActionResult,
) error {
	createCustomerAction, ok := action.(*models.CreateCustomerActionConfig)
	if !ok {
		return temporal.NewApplicationError("invalid action config type for create_customer", "InvalidActionConfig")
	}

	// Use ExternalCustomerID from input for customer creation
	externalID := input.ExternalCustomerID
	if externalID == "" {
		return temporal.NewApplicationError("external_customer_id is required for create_customer action", "MissingExternalID")
	}

	userID := ""
	if createCustomerAction.DefaultUserID != nil {
		userID = *createCustomerAction.DefaultUserID
	}

	activityInput := models.CreateCustomerActivityInput{
		ExternalID:    externalID,
		Name:          externalID,
		Email:         "",
		TenantID:      input.TenantID,
		EnvironmentID: input.EnvironmentID,
		UserID:        userID,
	}

	var activityResult models.CreateCustomerActivityResult
	err := workflow.ExecuteActivity(ctx, ActivityCreateCustomer, activityInput).Get(ctx, &activityResult)
	if err != nil {
		return err
	}

	actionResult.ResourceID = activityResult.CustomerID
	actionResult.ResourceType = models.WorkflowResourceTypeCustomer
	return nil
}

// executeCreateWalletAction executes the create wallet action
func executeCreateWalletAction(
	ctx workflow.Context,
	input models.CustomerOnboardingWorkflowInput,
	action models.WorkflowActionConfig,
	actionResult *models.CustomerOnboardingActionResult,
) error {
	walletAction, ok := action.(*models.CreateWalletActionConfig)
	if !ok {
		return temporal.NewApplicationError("invalid action config type for create_wallet", "InvalidActionConfig")
	}

	activityInput := models.CreateWalletActivityInput{
		CustomerID:    input.CustomerID,
		TenantID:      input.TenantID,
		EnvironmentID: input.EnvironmentID,
		UserID:        input.UserID,
		WalletConfig:  walletAction,
	}

	var activityResult models.CreateWalletActivityResult
	err := workflow.ExecuteActivity(ctx, ActivityCreateWallet, activityInput).Get(ctx, &activityResult)
	if err != nil {
		return err
	}

	actionResult.ResourceID = activityResult.WalletID
	actionResult.ResourceType = models.WorkflowResourceTypeWallet
	return nil
}

// executeCreateSubscriptionAction executes the create subscription action
func executeCreateSubscriptionAction(
	ctx workflow.Context,
	input models.CustomerOnboardingWorkflowInput,
	action models.WorkflowActionConfig,
	actionResult *models.CustomerOnboardingActionResult,
) error {
	subAction, ok := action.(*models.CreateSubscriptionActionConfig)
	if !ok {
		return temporal.NewApplicationError("invalid action config type for create_subscription", "InvalidActionConfig")
	}

	activityInput := models.CreateSubscriptionActivityInput{
		CustomerID:         input.CustomerID,
		EventTimestamp:     input.EventTimestamp,
		TenantID:           input.TenantID,
		EnvironmentID:      input.EnvironmentID,
		UserID:             input.UserID,
		SubscriptionConfig: subAction,
	}

	var activityResult models.CreateSubscriptionActivityResult
	err := workflow.ExecuteActivity(ctx, ActivityCreateSubscription, activityInput).Get(ctx, &activityResult)
	if err != nil {
		return err
	}

	actionResult.ResourceID = activityResult.SubscriptionID
	actionResult.ResourceType = models.WorkflowResourceTypeSubscription
	return nil
}
