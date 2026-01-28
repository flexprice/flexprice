package workflows

import (
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// Workflow name - must match the function name
	WorkflowPrepareProcessedEvents = "PrepareProcessedEventsWorkflow"

	// Activity names - must match registered method names
	ActivityCreateFeatureAndPrice  = "CreateFeatureAndPriceActivity"
	ActivityRolloutToSubscriptions = "RolloutToSubscriptionsActivity"
)

// PrepareProcessedEventsWorkflow creates missing feature/meter/price for an event name and optionally rolls out the plan prices to subscriptions.
func PrepareProcessedEventsWorkflow(ctx workflow.Context, input models.PrepareProcessedEventsWorkflowInput) (*models.PrepareProcessedEventsWorkflowResult, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	logger := workflow.GetLogger(ctx)
	logger.Debug("Starting PrepareProcessedEventsWorkflow",
		"event_name", input.EventName,
		"action_count", len(input.WorkflowConfig.Actions),
	)

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 10,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 5,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute * 2,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	result := &models.PrepareProcessedEventsWorkflowResult{
		EventName:       input.EventName,
		Status:          "processing",
		ActionsExecuted: 0,
		Results:         make([]models.PrepareProcessedEventsActionResult, 0, len(input.WorkflowConfig.Actions)),
	}

	// Track price_ids from create_feature_and_price action for use in rollout action
	var createdPriceIDs []string

	// Execute each action in sequence
	for i, action := range input.WorkflowConfig.Actions {
		actionType := action.GetAction()
		logger.Debug("Executing workflow action",
			"event_name", input.EventName,
			"action_index", i,
			"action_type", actionType)

		actionResult := models.PrepareProcessedEventsActionResult{
			ActionType:  actionType,
			ActionIndex: i,
			Status:      "processing",
		}

		var err error
		switch actionType {
		case models.WorkflowActionCreateFeatureAndPrice:
			err = executeCreateFeatureAndPriceAction(ctx, input, action, &actionResult, &createdPriceIDs)

		case models.WorkflowActionRolloutToSubscriptions:
			err = executeRolloutToSubscriptionsAction(ctx, input, action, &actionResult, createdPriceIDs)

		default:
			logger.Warn("Unknown workflow action type",
				"event_name", input.EventName,
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
				"event_name", input.EventName,
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

		logger.Debug("Workflow action completed successfully",
			"event_name", input.EventName,
			"action_index", i,
			"action_type", actionType,
			"resource_id", actionResult.ResourceID)
	}

	// All actions completed successfully
	result.Status = models.WorkflowStatusCompleted
	result.CompletedAt = workflow.Now(ctx)

	logger.Info("PrepareProcessedEventsWorkflow completed successfully",
		"event_name", input.EventName,
		"actions_executed", result.ActionsExecuted)

	return result, nil
}

// executeCreateFeatureAndPriceAction executes the create feature and price action
func executeCreateFeatureAndPriceAction(
	ctx workflow.Context,
	input models.PrepareProcessedEventsWorkflowInput,
	action models.WorkflowActionConfig,
	actionResult *models.PrepareProcessedEventsActionResult,
	createdPriceIDs *[]string,
) error {
	logger := workflow.GetLogger(ctx)
	featureAction, ok := action.(*models.CreateFeatureAndPriceActionConfig)
	if !ok {
		logger.Error("Invalid action config type for create_feature_and_price",
			"event_name", input.EventName,
			"action_type", action.GetAction())
		return temporal.NewApplicationError("invalid action config type for create_feature_and_price", "InvalidActionConfig")
	}

	if featureAction.PlanID == "" {
		logger.Error("plan_id is required for create_feature_and_price action",
			"event_name", input.EventName)
		return temporal.NewApplicationError("plan_id is required for create_feature_and_price action", "MissingPlanID")
	}

	activityInput := models.CreateFeatureAndPriceActivityInput{
		EventName:                  input.EventName,
		EventProperties:            input.EventProperties,
		TenantID:                   input.TenantID,
		EnvironmentID:              input.EnvironmentID,
		FeatureAndPriceConfig:      featureAction,
		OnlyCreateAggregationFields: input.OnlyCreateAggregationFields,
	}

	var activityResult models.CreateFeatureAndPriceActivityResult
	err := workflow.ExecuteActivity(ctx, ActivityCreateFeatureAndPrice, activityInput).Get(ctx, &activityResult)
	if err != nil {
		logger.Error("CreateFeatureAndPriceActivity failed",
			"event_name", input.EventName,
			"plan_id", featureAction.PlanID,
			"error", err)
		return err
	}

	if len(activityResult.Features) == 0 {
		// When OnlyCreateAggregationFields is set, zero features is valid (all requested aggregation fields already existed)
		if len(input.OnlyCreateAggregationFields) > 0 {
			logger.Debug("CreateFeatureAndPriceActivity created no features (only_create_aggregation_fields set; all may already exist)",
				"event_name", input.EventName,
				"plan_id", featureAction.PlanID,
				"only_create_aggregation_fields", input.OnlyCreateAggregationFields)
			actionResult.ResourceID = ""
			actionResult.ResourceType = models.WorkflowResourceTypeFeature
			*createdPriceIDs = []string{}
			return nil
		}
		logger.Error("CreateFeatureAndPriceActivity returned no features",
			"event_name", input.EventName,
			"plan_id", featureAction.PlanID)
		return temporal.NewApplicationError("no features were created", "NoFeaturesCreated")
	}
	// Store first feature_id as the primary resource ID (for backward compatibility)
	actionResult.ResourceID = activityResult.Features[0].FeatureID
	actionResult.ResourceType = models.WorkflowResourceTypeFeature

	// Store all price_ids for rollout action to use
	*createdPriceIDs = make([]string, 0, len(activityResult.Features))
	for _, featureResult := range activityResult.Features {
		*createdPriceIDs = append(*createdPriceIDs, featureResult.PriceID)
	}

	logger.Debug("CreateFeatureAndPriceAction completed successfully",
		"event_name", input.EventName,
		"plan_id", featureAction.PlanID,
		"features_created", len(activityResult.Features),
		"prices_created", len(*createdPriceIDs))
	return nil
}

// executeRolloutToSubscriptionsAction executes the rollout to subscriptions action
func executeRolloutToSubscriptionsAction(
	ctx workflow.Context,
	input models.PrepareProcessedEventsWorkflowInput,
	action models.WorkflowActionConfig,
	actionResult *models.PrepareProcessedEventsActionResult,
	priceIDs []string,
) error {
	logger := workflow.GetLogger(ctx)
	rolloutAction, ok := action.(*models.RolloutToSubscriptionsActionConfig)
	if !ok {
		logger.Error("Invalid action config type for rollout_to_subscriptions",
			"event_name", input.EventName,
			"action_type", action.GetAction())
		return temporal.NewApplicationError("invalid action config type for rollout_to_subscriptions", "InvalidActionConfig")
	}

	if rolloutAction.PlanID == "" {
		logger.Error("plan_id is required for rollout_to_subscriptions action",
			"event_name", input.EventName)
		return temporal.NewApplicationError("plan_id is required for rollout_to_subscriptions action", "MissingPlanID")
	}

	if len(priceIDs) == 0 {
		logger.Error("at least one price_id is required for rollout_to_subscriptions action",
			"event_name", input.EventName,
			"plan_id", rolloutAction.PlanID)
		return temporal.NewApplicationError("at least one price_id is required for rollout_to_subscriptions action", "MissingPriceID")
	}

	// Roll out each price to subscriptions
	totalLineItemsCreated := 0
	totalLineItemsFailed := 0

	for _, priceID := range priceIDs {
		activityInput := models.RolloutToSubscriptionsActivityInput{
			PlanID:         rolloutAction.PlanID,
			PriceID:        priceID,
			EventTimestamp: input.EventTimestamp,
			TenantID:       input.TenantID,
			EnvironmentID:  input.EnvironmentID,
		}

		var activityResult models.RolloutToSubscriptionsActivityResult
		err := workflow.ExecuteActivity(ctx, ActivityRolloutToSubscriptions, activityInput).Get(ctx, &activityResult)
		if err != nil {
			// Log error but continue with other prices
			logger.Error("Failed to rollout price to subscriptions",
				"price_id", priceID,
				"plan_id", rolloutAction.PlanID,
				"event_name", input.EventName,
				"error", err)
			totalLineItemsFailed += activityResult.LineItemsFailed
			continue
		}

		totalLineItemsCreated += activityResult.LineItemsCreated
		totalLineItemsFailed += activityResult.LineItemsFailed

		if activityResult.LineItemsCreated > 0 {
			logger.Debug("Successfully rolled out price to subscriptions",
				"price_id", priceID,
				"plan_id", rolloutAction.PlanID,
				"line_items_created", activityResult.LineItemsCreated,
				"line_items_failed", activityResult.LineItemsFailed)
		}
	}

	// Log summary
	if totalLineItemsFailed > 0 {
		logger.Debug("RolloutToSubscriptionsAction completed with some failures",
			"event_name", input.EventName,
			"plan_id", rolloutAction.PlanID,
			"total_line_items_created", totalLineItemsCreated,
			"total_line_items_failed", totalLineItemsFailed,
			"prices_processed", len(priceIDs))
	} else {
		logger.Debug("RolloutToSubscriptionsAction completed successfully",
			"event_name", input.EventName,
			"plan_id", rolloutAction.PlanID,
			"total_line_items_created", totalLineItemsCreated,
			"prices_processed", len(priceIDs))
	}

	// Store plan_id as the resource ID
	actionResult.ResourceID = rolloutAction.PlanID
	actionResult.ResourceType = models.WorkflowResourceTypePlan
	return nil
}
