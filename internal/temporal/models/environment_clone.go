package models

import (
	"fmt"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// EnvironmentCloneWorkflowInput represents input for the environment clone workflow.
// The target environment must already exist before the workflow is started — the handler
// is responsible for creating it (or accepting a pre-existing one) before kicking off the workflow.
type EnvironmentCloneWorkflowInput struct {
	// SourceEnvironmentID is the environment being cloned from.
	SourceEnvironmentID string `json:"source_environment_id"`
	// TargetEnvironmentID is the environment to clone into (must already exist).
	TargetEnvironmentID string `json:"target_environment_id"`
	TenantID            string `json:"tenant_id"`
	UserID              string `json:"user_id"`
}

func (e *EnvironmentCloneWorkflowInput) Validate() error {
	if e.SourceEnvironmentID == "" {
		return ierr.NewError("source environment ID is required").
			WithHint("Source environment ID is required").
			Mark(ierr.ErrValidation)
	}
	if e.TargetEnvironmentID == "" {
		return ierr.NewError("target environment ID is required").
			WithHint("Target environment ID is required").
			Mark(ierr.ErrValidation)
	}
	if e.SourceEnvironmentID == e.TargetEnvironmentID {
		return ierr.NewError("source and target environment IDs cannot be the same").
			WithHint("Source and target environment IDs cannot be the same").
			Mark(ierr.ErrValidation)
	}
	if e.TenantID == "" {
		return ierr.NewError("tenant ID is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// CloneError records one failed clone attempt (feature or plan).
type CloneError struct {
	EntityType string `json:"entity_type"` // "feature" or "plan"
	EntityID   string `json:"entity_id"`   // source entity ID
	EntityName string `json:"entity_name"` // display name
	Reason     string `json:"reason"`
}

func (e CloneError) Error() string {
	return fmt.Sprintf("%s %s (%s): %s", e.EntityType, e.EntityID, e.EntityName, e.Reason)
}

// CloneEnvironmentInput is shared activity input for both feature and plan cloning.
type CloneEnvironmentInput struct {
	SourceEnvironmentID string `json:"source_environment_id"`
	TargetEnvironmentID string `json:"target_environment_id"`
	TenantID            string `json:"tenant_id"`
	UserID              string `json:"user_id"`
}

// CloneEnvironmentFeaturesOutput is activity output for feature cloning.
type CloneEnvironmentFeaturesOutput struct {
	FeaturesCloned    int      `json:"features_cloned"`
	FeatureIDs        []string `json:"feature_ids"`
	FeaturesSkipped   int      `json:"features_skipped"`
	SkippedFeatureIDs []string `json:"skipped_feature_ids,omitempty"`
}

// CloneEnvironmentPlansOutput is activity output for plan cloning.
type CloneEnvironmentPlansOutput struct {
	PlansCloned    int      `json:"plans_cloned"`
	PlanIDs        []string `json:"plan_ids"`
	PlansSkipped   int      `json:"plans_skipped"`
	SkippedPlanIDs []string `json:"skipped_plan_ids,omitempty"`
}

// EnvironmentCloneResult is the workflow result.
// CloneErrors are aggregated here so the outputs stay clean.
type EnvironmentCloneResult struct {
	Features CloneEnvironmentFeaturesOutput `json:"features"`
	Plans    CloneEnvironmentPlansOutput    `json:"plans"`
	Errors   []CloneError                   `json:"errors,omitempty"`
}
