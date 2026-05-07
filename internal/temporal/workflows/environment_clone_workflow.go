package workflows

import (
	"time"

	environmentActivities "github.com/flexprice/flexprice/internal/temporal/activities/environment"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	WorkflowEnvironmentClone         = "EnvironmentCloneWorkflow"
	ActivityCloneEnvironmentFeatures = "CloneEnvironmentFeatures"
	ActivityCloneEnvironmentPlans    = "CloneEnvironmentPlans"
)

// EnvironmentCloneWorkflow orchestrates cloning all published features and plans
// from a source environment into a target environment.
// The target environment must already exist when the workflow starts.
// Activity 1: Clone features (runs first — plans reference features via entitlements)
// Activity 2: Clone plans (runs after features so ID maps resolve correctly)
func EnvironmentCloneWorkflow(ctx workflow.Context, in models.EnvironmentCloneWorkflowInput) (*models.EnvironmentCloneResult, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: time.Hour * 1,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute * 5,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	input := models.CloneEnvironmentInput{
		SourceEnvironmentID: in.SourceEnvironmentID,
		TargetEnvironmentID: in.TargetEnvironmentID,
		TenantID:            in.TenantID,
		UserID:              in.UserID,
	}

	var featResult environmentActivities.CloneFeaturesResult
	if err := workflow.ExecuteActivity(ctx, ActivityCloneEnvironmentFeatures, input).Get(ctx, &featResult); err != nil {
		return nil, err
	}

	var planResult environmentActivities.ClonePlansResult
	if err := workflow.ExecuteActivity(ctx, ActivityCloneEnvironmentPlans, input).Get(ctx, &planResult); err != nil {
		return nil, err
	}

	return &models.EnvironmentCloneResult{
		Features: featResult.CloneEnvironmentFeaturesOutput,
		Plans:    planResult.CloneEnvironmentPlansOutput,
		Errors:   append(featResult.Errors, planResult.Errors...),
	}, nil
}
