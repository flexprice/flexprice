package models

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// PrepareProcessedEventsWorkflowInput represents input for the prepare processed events workflow.
// It is used when an ingested event references a non-existent feature/meter/price.
type PrepareProcessedEventsWorkflowInput struct {
	EventID                    string                 `json:"event_id"` // Event ID for workflow ID generation
	EventName                  string                 `json:"event_name"`
	EventTimestamp             time.Time              `json:"event_timestamp"` // Event timestamp for line item StartDate
	EventProperties            map[string]interface{} `json:"event_properties"` // Event properties to determine feature creation logic
	TenantID                   string                 `json:"tenant_id"`
	EnvironmentID              string                 `json:"environment_id"`
	WorkflowConfig             WorkflowConfig         `json:"workflow_config"`
	OnlyCreateAggregationFields []string               `json:"only_create_aggregation_fields,omitempty"` // When set, create only features for these aggregation fields (skip existing)
}

func (p *PrepareProcessedEventsWorkflowInput) Validate() error {
	if p.EventName == "" {
		return ierr.NewError("event_name is required").
			WithHint("Provide event_name to create feature/meter for the event").
			Mark(ierr.ErrValidation)
	}
	if p.TenantID == "" || p.EnvironmentID == "" {
		return ierr.NewError("tenant_id and environment_id are required").
			WithHint("Provide tenant_id and environment_id").
			Mark(ierr.ErrValidation)
	}
	// user_id is optional (can be empty)
	if err := p.WorkflowConfig.Validate(); err != nil {
		return err
	}
	return nil
}

type PrepareProcessedEventsWorkflowResult struct {
	EventName       string                               `json:"event_name"`
	Status          WorkflowStatus                       `json:"status"`
	CompletedAt     time.Time                            `json:"completed_at"`
	ActionsExecuted int                                  `json:"actions_executed"`
	Results         []PrepareProcessedEventsActionResult `json:"results"`
	ErrorSummary    *string                              `json:"error_summary,omitempty"`
}

// PrepareProcessedEventsActionResult represents the result of a single action in the workflow
type PrepareProcessedEventsActionResult struct {
	ActionType   WorkflowAction       `json:"action_type"`
	ActionIndex  int                  `json:"action_index"`
	Status       WorkflowStatus       `json:"status"`
	ResourceID   string               `json:"resource_id,omitempty"`
	ResourceType WorkflowResourceType `json:"resource_type,omitempty"`
	Error        *string              `json:"error,omitempty"`
}

type CreateFeatureAndPriceActivityInput struct {
	EventName                  string                             `json:"event_name"`
	EventProperties            map[string]interface{}             `json:"event_properties"` // Event properties for feature determination
	TenantID                   string                             `json:"tenant_id"`
	EnvironmentID              string                             `json:"environment_id"`
	FeatureAndPriceConfig      *CreateFeatureAndPriceActionConfig `json:"feature_and_price_config" validate:"required"`
	OnlyCreateAggregationFields []string                           `json:"only_create_aggregation_fields,omitempty"` // When set, create only features for these aggregation fields (skip existing)
}

func (c *CreateFeatureAndPriceActivityInput) Validate() error {
	if c.EventName == "" {
		return ierr.NewError("event_name is required").
			WithHint("Provide event_name").
			Mark(ierr.ErrValidation)
	}
	if c.TenantID == "" || c.EnvironmentID == "" {
		return ierr.NewError("tenant_id and environment_id are required").
			WithHint("Provide tenant_id and environment_id").
			Mark(ierr.ErrValidation)
	}
	if c.FeatureAndPriceConfig == nil {
		return ierr.NewError("feature_and_price_config is required").
			WithHint("Provide feature and price configuration").
			Mark(ierr.ErrValidation)
	}
	return c.FeatureAndPriceConfig.Validate()
}

type CreateFeatureAndPriceActivityResult struct {
	Features []FeaturePriceResult `json:"features"` // Multiple features can be created
	PlanID   string               `json:"plan_id"`
}

type FeaturePriceResult struct {
	FeatureID string `json:"feature_id"`
	MeterID   string `json:"meter_id"`
	PriceID   string `json:"price_id"`
}

type RolloutToSubscriptionsActivityInput struct {
	PlanID         string    `json:"plan_id"`
	PriceID        string    `json:"price_id"`
	EventTimestamp time.Time `json:"event_timestamp"` // Used as line item StartDate
	TenantID       string    `json:"tenant_id"`
	EnvironmentID  string    `json:"environment_id"`
}

func (r *RolloutToSubscriptionsActivityInput) Validate() error {
	if r.PlanID == "" {
		return ierr.NewError("plan_id is required").
			WithHint("Provide plan_id").
			Mark(ierr.ErrValidation)
	}
	if r.PriceID == "" {
		return ierr.NewError("price_id is required").
			WithHint("Provide price_id").
			Mark(ierr.ErrValidation)
	}
	if r.TenantID == "" || r.EnvironmentID == "" {
		return ierr.NewError("tenant_id and environment_id are required").
			WithHint("Provide tenant_id and environment_id").
			Mark(ierr.ErrValidation)
	}
	return nil
}

type RolloutToSubscriptionsActivityResult struct {
	LineItemsCreated int `json:"line_items_created"`
	LineItemsFailed  int `json:"line_items_failed"`
}

// WorkflowResourceType for prepare processed events
const (
	WorkflowResourceTypeFeature WorkflowResourceType = "feature"
	WorkflowResourceTypeMeter   WorkflowResourceType = "meter"
	WorkflowResourceTypePrice   WorkflowResourceType = "price"
	WorkflowResourceTypePlan    WorkflowResourceType = "plan"
)
