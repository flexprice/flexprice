package types

import (
	"fmt"
	"strings"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// TemporalTaskQueue represents a logical grouping of workflows and activities
type TemporalTaskQueue string

const (
	// Task Queues - logical groupings to limit worker count
	TemporalTaskQueueTask  TemporalTaskQueue = "task"
	TemporalTaskQueuePrice TemporalTaskQueue = "price"
)

// String returns the string representation of the task queue
func (tq TemporalTaskQueue) String() string {
	return string(tq)
}

// Validate validates the task queue
func (tq TemporalTaskQueue) Validate() error {
	allowedQueues := []TemporalTaskQueue{
		TemporalTaskQueueTask,
		TemporalTaskQueuePrice,
	}
	if lo.Contains(allowedQueues, tq) {
		return nil
	}
	return ierr.NewError("invalid task queue").
		WithHint(fmt.Sprintf("Task queue must be one of: %s", strings.Join(lo.Map(allowedQueues, func(tq TemporalTaskQueue, _ int) string { return string(tq) }), ", "))).
		Mark(ierr.ErrValidation)
}

// TemporalWorkflowType represents the type of workflow
type TemporalWorkflowType string

const (
	// Workflow Types - only include implemented workflows
	TemporalPriceSyncWorkflow            TemporalWorkflowType = "PriceSyncWorkflow"
	TemporalTaskProcessingWorkflow       TemporalWorkflowType = "TaskProcessingWorkflow"
	TemporalSubscriptionChangeWorkflow   TemporalWorkflowType = "SubscriptionChangeWorkflow"
	TemporalSubscriptionCreationWorkflow TemporalWorkflowType = "SubscriptionCreationWorkflow"
	TemporalStripeIntegrationWorkflow    TemporalWorkflowType = "StripeIntegrationWorkflow"
)

// String returns the string representation of the workflow type
func (w TemporalWorkflowType) String() string {
	return string(w)
}

// Validate validates the workflow type
func (w TemporalWorkflowType) Validate() error {
	allowedWorkflows := []TemporalWorkflowType{
		TemporalPriceSyncWorkflow,            // "PriceSyncWorkflow"
		TemporalTaskProcessingWorkflow,       // "TaskProcessingWorkflow"
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

// TaskQueue returns the logical task queue for the workflow
func (w TemporalWorkflowType) TaskQueue() TemporalTaskQueue {
	switch w {
	case TemporalTaskProcessingWorkflow, TemporalSubscriptionChangeWorkflow, TemporalSubscriptionCreationWorkflow:
		return TemporalTaskQueueTask
	case TemporalPriceSyncWorkflow:
		return TemporalTaskQueuePrice
	default:
		return TemporalTaskQueueTask // Default fallback
	}
}

// TaskQueueName returns the task queue name for the workflow
func (w TemporalWorkflowType) TaskQueueName() string {
	return w.TaskQueue().String()
}

// WorkflowID returns the workflow ID for the workflow with given identifier
func (w TemporalWorkflowType) WorkflowID(identifier string) string {
	return string(w) + "-" + identifier
}

// GetWorkflowsForTaskQueue returns all workflows that belong to a specific task queue
func GetWorkflowsForTaskQueue(taskQueue TemporalTaskQueue) []TemporalWorkflowType {
	switch taskQueue {
	case TemporalTaskQueueTask:
		return []TemporalWorkflowType{
			TemporalTaskProcessingWorkflow,
		}
	case TemporalTaskQueuePrice:
		return []TemporalWorkflowType{
			TemporalPriceSyncWorkflow,
		}
	default:
		return []TemporalWorkflowType{}
	}
}

// GetAllTaskQueues returns all available task queues
func GetAllTaskQueues() []TemporalTaskQueue {
	return []TemporalTaskQueue{
		TemporalTaskQueueTask,
		TemporalTaskQueuePrice,
	}
}
