package models

// TaskQueue constants for different types of workflows
const (
	// BillingTaskQueue handles billing-related workflows
	BillingTaskQueue = "billing-queue"

	// TaskProcessingTaskQueue handles task processing workflows
	TaskProcessingTaskQueue = "task-processing-queue"

	// PriceSyncTaskQueue handles price synchronization workflows
	PriceSyncTaskQueue = "price-sync-queue"
)

// WorkflowName constants for different workflow types
const (
	// BillingWorkflowName is the name of the billing workflow
	BillingWorkflowName = "BillingWorkflow"

	// TaskProcessingWorkflowName is the name of the task processing workflow
	TaskProcessingWorkflowName = "TaskProcessingWorkflow"

	// PriceSyncWorkflowName is the name of the price sync workflow
	PriceSyncWorkflowName = "PriceSyncWorkflow"
)

// ActivityName constants for different activity types
const (
	// TaskProcessingActivityName is the name of the task processing activity
	TaskProcessingActivityName = "ProcessTask"

	// PriceSyncActivityName is the name of the price sync activity
	PriceSyncActivityName = "SyncPlanPrices"
)

// GetTaskQueueForWorkflow returns the appropriate task queue for a given workflow name
func GetTaskQueueForWorkflow(workflowName string) string {
	switch workflowName {
	case BillingWorkflowName:
		return BillingTaskQueue
	case TaskProcessingWorkflowName:
		return TaskProcessingTaskQueue
	case PriceSyncWorkflowName:
		return PriceSyncTaskQueue
	default:
		return "default-queue"
	}
}

// GetAllTaskQueues returns all available task queues
func GetAllTaskQueues() []string {
	return []string{
		BillingTaskQueue,
		TaskProcessingTaskQueue,
		PriceSyncTaskQueue,
	}
}
