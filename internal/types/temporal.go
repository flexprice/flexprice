package types

import (
	"fmt"
	"strings"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// TemporalActivityType represents the type of activity
type TemporalActivityType string

const (
	// Activity Types - must match the method names in activity structs
	TemporalActivitySyncPlanPrices       TemporalActivityType = "SyncPlanPrices"
	TemporalActivityFetchData            TemporalActivityType = "FetchData"
	TemporalActivityCalculate            TemporalActivityType = "Calculate"
	TemporalActivitySubscriptionChange   TemporalActivityType = "SubscriptionChange"
	TemporalActivitySubscriptionCreation TemporalActivityType = "SubscriptionCreation"
)

// ActivityInfo holds information about an activity
type ActivityInfo struct {
	Name string // Fully qualified name (e.g., "SyncPlanPrices")
	Type TemporalActivityType
}

// String returns the string representation of the activity type
func (a TemporalActivityType) String() string {
	return string(a)
}

// Validate validates the activity type
func (a TemporalActivityType) Validate() error {
	allowedValues := []string{
		string(TemporalActivitySyncPlanPrices),
		string(TemporalActivityFetchData),
		string(TemporalActivityCalculate),
		string(TemporalActivitySubscriptionChange),
		string(TemporalActivitySubscriptionCreation),
	}
	if !lo.Contains(allowedValues, string(a)) {
		return ierr.NewError("invalid activity type").
			WithHint("Invalid activity type").
			WithReportableDetails(map[string]any{
				"allowed":        allowedValues,
				"type":           a,
				"allowed_values": allowedValues,
				"provided_value": a,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// QualifiedName returns the fully qualified activity name
func (a TemporalActivityType) QualifiedName(prefix string) string {
	return prefix + "." + string(a)
}

// TemporalWorkflowType represents the type of workflow
type TemporalWorkflowType string

const (
	// Workflow Types - using clean aliases for registration
	TemporalBillingWorkflow              TemporalWorkflowType = "CronBillingWorkflow"
	TemporalCalculationWorkflow          TemporalWorkflowType = "CalculateChargesWorkflow"
	TemporalPriceSyncWorkflow            TemporalWorkflowType = "PriceSyncWorkflow"
	TemporalSubscriptionChangeWorkflow   TemporalWorkflowType = "SubscriptionChangeWorkflow"
	TemporalSubscriptionCreationWorkflow TemporalWorkflowType = "SubscriptionCreationWorkflow"
	
)

// String returns the string representation of the workflow type
func (w TemporalWorkflowType) String() string {
	return string(w)
}

// Validate validates the workflow type
func (w TemporalWorkflowType) Validate() error {
	allowedWorkflows := []TemporalWorkflowType{
		TemporalBillingWorkflow,              // "CronBillingWorkflow"
		TemporalCalculationWorkflow,          // "CalculateChargesWorkflow"
		TemporalPriceSyncWorkflow,            // "PriceSyncWorkflow"
		TemporalSubscriptionChangeWorkflow,   // "SubscriptionChangeWorkflow"
		TemporalSubscriptionCreationWorkflow, // "SubscriptionCreationWorkflow"
	}
	if lo.Contains(allowedWorkflows, w) {
		return nil
	}

	return ierr.NewError("invalid workflow type").
		WithHint(fmt.Sprintf("Workflow type must be one of: %s", strings.Join(lo.Map(allowedWorkflows, func(w TemporalWorkflowType, _ int) string { return string(w) }), ", "))).
		Mark(ierr.ErrValidation)
}

// TaskQueueName returns the task queue name for the workflow
func (w TemporalWorkflowType) TaskQueueName() string {
	return string(w) + "TaskQueue"
}

// WorkflowID returns the workflow ID for the workflow with given identifier
func (w TemporalWorkflowType) WorkflowID(identifier string) string {
	return string(w) + "-" + identifier
}
