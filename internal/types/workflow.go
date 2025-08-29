package types

import (
	"fmt"
	"strings"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// WorkflowType represents the type of workflow
type WorkflowType string

const (
	// BillingWorkflow represents the billing workflow
	BillingWorkflow WorkflowType = "BillingWorkflow"
	// CalculationWorkflow represents the calculation workflow
	CalculationWorkflow WorkflowType = "CalculationWorkflow"
	// PriceSyncWorkflow represents the price sync workflow
	PriceSyncWorkflow WorkflowType = "PriceSyncWorkflow"
)

// String returns the string representation of the workflow type
func (w WorkflowType) String() string {
	return string(w)
}

// Validate validates the workflow type
func (w WorkflowType) Validate() error {
	allowedWorkflows := []WorkflowType{
		BillingWorkflow,
		CalculationWorkflow,
		PriceSyncWorkflow,
	}
	if lo.Contains(allowedWorkflows, w) {
		return nil
	}

	return ierr.NewError("invalid workflow type").
		WithHint(fmt.Sprintf("Workflow type must be one of: %s", strings.Join(lo.Map(allowedWorkflows, func(w WorkflowType, _ int) string { return string(w) }), ", "))).
		Mark(ierr.ErrValidation)
}

// TaskQueueName returns the task queue name for the workflow
func (w WorkflowType) TaskQueueName() string {
	return string(w) + "TaskQueue"
}

// WorkflowID returns the workflow ID for the workflow with given identifier
func (w WorkflowType) WorkflowID(identifier string) string {
	return string(w) + "-" + identifier
}
