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
	TemporalTaskQueueTask            TemporalTaskQueue = "task"
	TemporalTaskQueuePrice           TemporalTaskQueue = "price"
	TemporalTaskQueueExport          TemporalTaskQueue = "export"
	TemporalTaskQueueWorkflows       TemporalTaskQueue = "workflows"
	TemporalTaskQueueSubscription    TemporalTaskQueue = "subscription"
	TemporalTaskQueueInvoice         TemporalTaskQueue = "invoice"
	TemporalTaskQueueReprocessEvents TemporalTaskQueue = "events"
	TemporalTaskQueueCron            TemporalTaskQueue = "cron"
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
		TemporalTaskQueueExport,
		TemporalTaskQueueSubscription,
		TemporalTaskQueueWorkflows,
		TemporalTaskQueueInvoice,
		TemporalTaskQueueReprocessEvents,
		TemporalTaskQueueCron,
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
	TemporalCreditGrantProcessingWorkflow              TemporalWorkflowType = "CreditGrantProcessingWorkflow"
	TemporalSubscriptionAutoCancellationWorkflow       TemporalWorkflowType = "SubscriptionAutoCancellationWorkflow"
	TemporalWalletCreditExpiryWorkflow                 TemporalWorkflowType = "WalletCreditExpiryWorkflow"
	TemporalSubscriptionBillingPeriodsWorkflow         TemporalWorkflowType = "SubscriptionBillingPeriodsWorkflow"
	TemporalSubscriptionRenewalDueAlertsWorkflow       TemporalWorkflowType = "SubscriptionRenewalDueAlertsWorkflow"
	TemporalOutboundWebhookStaleRetryWorkflow          TemporalWorkflowType = "OutboundWebhookStaleRetryWorkflow"
	TemporalChargebeeCustomerSyncWorkflow              TemporalWorkflowType = "ChargebeeCustomerSyncWorkflow"
	TemporalChargebeeInvoiceSyncWorkflow               TemporalWorkflowType = "ChargebeeInvoiceSyncWorkflow"
	TemporalComputeInvoiceWorkflow                     TemporalWorkflowType = "ComputeInvoiceWorkflow"
	TemporalCustomerOnboardingWorkflow                 TemporalWorkflowType = "CustomerOnboardingWorkflow"
	TemporalDraftAndComputeSubscriptionInvoiceWorkflow TemporalWorkflowType = "DraftAndComputeSubscriptionInvoiceWorkflow"
	TemporalEnvironmentCloneWorkflow                   TemporalWorkflowType = "EnvironmentCloneWorkflow"
	TemporalExecuteExportWorkflow                      TemporalWorkflowType = "ExecuteExportWorkflow"
	TemporalFinalizeDraftInvoiceWorkflow               TemporalWorkflowType = "FinalizeDraftInvoiceWorkflow"
	TemporalHubSpotDealSyncWorkflow                    TemporalWorkflowType = "HubSpotDealSyncWorkflow"
	TemporalHubSpotInvoiceSyncWorkflow                 TemporalWorkflowType = "HubSpotInvoiceSyncWorkflow"
	TemporalHubSpotQuoteSyncWorkflow                   TemporalWorkflowType = "HubSpotQuoteSyncWorkflow"
	TemporalMoyasarInvoiceSyncWorkflow                 TemporalWorkflowType = "MoyasarInvoiceSyncWorkflow"
	TemporalNomodCustomerSyncWorkflow                  TemporalWorkflowType = "NomodCustomerSyncWorkflow"
	TemporalNomodInvoiceSyncWorkflow                   TemporalWorkflowType = "NomodInvoiceSyncWorkflow"
	TemporalPaddleCustomerSyncWorkflow                 TemporalWorkflowType = "PaddleCustomerSyncWorkflow"
	TemporalPaddleInvoiceSyncWorkflow                  TemporalWorkflowType = "PaddleInvoiceSyncWorkflow"
	TemporalPrepareProcessedEventsWorkflow             TemporalWorkflowType = "PrepareProcessedEventsWorkflow"
	TemporalPriceSyncWorkflow                          TemporalWorkflowType = "PriceSyncWorkflow"
	TemporalProcessInvoiceWorkflow                     TemporalWorkflowType = "ProcessInvoiceWorkflow"
	TemporalProcessSubscriptionBillingWorkflow         TemporalWorkflowType = "ProcessSubscriptionBillingWorkflow"
	TemporalQuickBooksCustomerSyncWorkflow             TemporalWorkflowType = "QuickBooksCustomerSyncWorkflow"
	TemporalQuickBooksInvoiceSyncWorkflow              TemporalWorkflowType = "QuickBooksInvoiceSyncWorkflow"
	TemporalQuickBooksPriceSyncWorkflow                TemporalWorkflowType = "QuickBooksPriceSyncWorkflow"
	TemporalRazorpayCustomerSyncWorkflow               TemporalWorkflowType = "RazorpayCustomerSyncWorkflow"
	TemporalRazorpayInvoiceSyncWorkflow                TemporalWorkflowType = "RazorpayInvoiceSyncWorkflow"
	TemporalRecalculateInvoiceWorkflow                 TemporalWorkflowType = "RecalculateInvoiceWorkflow"
	TemporalReprocessEventsForPlanWorkflow             TemporalWorkflowType = "ReprocessEventsForPlanWorkflow"
	TemporalReprocessEventsWorkflow                    TemporalWorkflowType = "ReprocessEventsWorkflow"
	TemporalReprocessRawEventsWorkflow                 TemporalWorkflowType = "ReprocessRawEventsWorkflow"
	TemporalScheduleDraftFinalizationWorkflow          TemporalWorkflowType = "ScheduleDraftFinalizationWorkflow"
	TemporalScheduleSubscriptionBillingWorkflow        TemporalWorkflowType = "ScheduleSubscriptionBillingWorkflow"
	TemporalStripeCustomerSyncWorkflow                 TemporalWorkflowType = "StripeCustomerSyncWorkflow"
	TemporalStripeIntegrationWorkflow                  TemporalWorkflowType = "StripeIntegrationWorkflow"
	TemporalStripeInvoiceSyncWorkflow                  TemporalWorkflowType = "StripeInvoiceSyncWorkflow"
	TemporalZohoBooksInvoiceSyncWorkflow               TemporalWorkflowType = "ZohoBooksInvoiceSyncWorkflow"
	TemporalSubscriptionChangeWorkflow                 TemporalWorkflowType = "SubscriptionChangeWorkflow"
	TemporalSubscriptionCreationWorkflow               TemporalWorkflowType = "SubscriptionCreationWorkflow"
	TemporalTaskProcessingWorkflow                     TemporalWorkflowType = "TaskProcessingWorkflow"
)

// temporalCronWorkflowTypes is the single list of schedule/worker cron workflows (keeps
// task queue, tracking exclusions, and Validate in sync when adding a cron workflow).
var temporalCronWorkflowTypes = []TemporalWorkflowType{
	TemporalCreditGrantProcessingWorkflow,
	TemporalSubscriptionAutoCancellationWorkflow,
	TemporalWalletCreditExpiryWorkflow,
	TemporalSubscriptionBillingPeriodsWorkflow,
	TemporalSubscriptionRenewalDueAlertsWorkflow,
	TemporalOutboundWebhookStaleRetryWorkflow,
}

var workflowTypesExcludedFromTrackingCore = []TemporalWorkflowType{
	TemporalScheduleSubscriptionBillingWorkflow,
	TemporalProcessSubscriptionBillingWorkflow,
	TemporalProcessInvoiceWorkflow,
	TemporalScheduleDraftFinalizationWorkflow,
}

// WorkflowTypesExcludedFromTracking are workflow types that are not persisted to the
// workflow_execution table and do not run start/end tracking in the interceptor.
// Use the existing TemporalWorkflowType enums so the list stays type-safe and discoverable.
var WorkflowTypesExcludedFromTracking = append(
	append([]TemporalWorkflowType{}, workflowTypesExcludedFromTrackingCore...),
	temporalCronWorkflowTypes...,
)

// ShouldTrackWorkflowType returns false if this workflow type is excluded from tracking
// (no DB save, no interceptor start/end logic). Used by the workflow tracking interceptor.
func ShouldTrackWorkflowType(w TemporalWorkflowType) bool {
	return !lo.Contains(WorkflowTypesExcludedFromTracking, w)
}

// String returns the string representation of the workflow type
func (w TemporalWorkflowType) String() string {
	return string(w)
}

// Validate validates the workflow type
func (w TemporalWorkflowType) Validate() error {
	allowedWorkflows := append(append([]TemporalWorkflowType{}, temporalCronWorkflowTypes...), []TemporalWorkflowType{
		TemporalChargebeeCustomerSyncWorkflow,
		TemporalChargebeeInvoiceSyncWorkflow,
		TemporalComputeInvoiceWorkflow,
		TemporalCustomerOnboardingWorkflow,
		TemporalDraftAndComputeSubscriptionInvoiceWorkflow,
		TemporalEnvironmentCloneWorkflow,
		TemporalExecuteExportWorkflow,
		TemporalFinalizeDraftInvoiceWorkflow,
		TemporalHubSpotDealSyncWorkflow,
		TemporalHubSpotInvoiceSyncWorkflow,
		TemporalHubSpotQuoteSyncWorkflow,
		TemporalMoyasarInvoiceSyncWorkflow,
		TemporalNomodCustomerSyncWorkflow,
		TemporalNomodInvoiceSyncWorkflow,
		TemporalPaddleCustomerSyncWorkflow,
		TemporalPaddleInvoiceSyncWorkflow,
		TemporalPrepareProcessedEventsWorkflow,
		TemporalPriceSyncWorkflow,
		TemporalProcessInvoiceWorkflow,
		TemporalProcessSubscriptionBillingWorkflow,
		TemporalQuickBooksCustomerSyncWorkflow,
		TemporalQuickBooksInvoiceSyncWorkflow,
		TemporalQuickBooksPriceSyncWorkflow,
		TemporalRazorpayCustomerSyncWorkflow,
		TemporalRazorpayInvoiceSyncWorkflow,
		TemporalRecalculateInvoiceWorkflow,
		TemporalReprocessEventsForPlanWorkflow,
		TemporalReprocessEventsWorkflow,
		TemporalReprocessRawEventsWorkflow,
		TemporalScheduleDraftFinalizationWorkflow,
		TemporalScheduleSubscriptionBillingWorkflow,
		TemporalStripeCustomerSyncWorkflow,
		TemporalStripeIntegrationWorkflow,
		TemporalStripeInvoiceSyncWorkflow,
		TemporalZohoBooksInvoiceSyncWorkflow,
		TemporalSubscriptionChangeWorkflow,
		TemporalSubscriptionCreationWorkflow,
		TemporalTaskProcessingWorkflow,
	}...)
	if lo.Contains(allowedWorkflows, w) {
		return nil
	}

	return ierr.NewError("invalid workflow type").
		WithHint(fmt.Sprintf("Workflow type must be one of: %s", strings.Join(lo.Map(allowedWorkflows, func(w TemporalWorkflowType, _ int) string { return string(w) }), ", "))).
		Mark(ierr.ErrValidation)
}

// TaskQueue returns the logical task queue for the workflow
func (w TemporalWorkflowType) TaskQueue() TemporalTaskQueue {
	if lo.Contains(temporalCronWorkflowTypes, w) {
		return TemporalTaskQueueCron
	}
	switch w {
	case TemporalTaskProcessingWorkflow, TemporalSubscriptionChangeWorkflow, TemporalSubscriptionCreationWorkflow, TemporalStripeIntegrationWorkflow, TemporalHubSpotDealSyncWorkflow, TemporalHubSpotInvoiceSyncWorkflow, TemporalHubSpotQuoteSyncWorkflow, TemporalNomodInvoiceSyncWorkflow, TemporalMoyasarInvoiceSyncWorkflow, TemporalPaddleInvoiceSyncWorkflow, TemporalStripeInvoiceSyncWorkflow, TemporalRazorpayInvoiceSyncWorkflow, TemporalChargebeeInvoiceSyncWorkflow, TemporalQuickBooksInvoiceSyncWorkflow, TemporalZohoBooksInvoiceSyncWorkflow, TemporalStripeCustomerSyncWorkflow, TemporalRazorpayCustomerSyncWorkflow, TemporalChargebeeCustomerSyncWorkflow, TemporalQuickBooksCustomerSyncWorkflow, TemporalNomodCustomerSyncWorkflow, TemporalPaddleCustomerSyncWorkflow:
		return TemporalTaskQueueTask
	case TemporalPriceSyncWorkflow, TemporalQuickBooksPriceSyncWorkflow:
		return TemporalTaskQueuePrice
	case TemporalExecuteExportWorkflow:
		return TemporalTaskQueueExport
	case TemporalScheduleSubscriptionBillingWorkflow:
		return TemporalTaskQueueSubscription
	case TemporalProcessSubscriptionBillingWorkflow:
		return TemporalTaskQueueSubscription
	case TemporalRecalculateInvoiceWorkflow:
		return TemporalTaskQueueSubscription
	case TemporalProcessInvoiceWorkflow, TemporalFinalizeDraftInvoiceWorkflow, TemporalScheduleDraftFinalizationWorkflow, TemporalComputeInvoiceWorkflow, TemporalDraftAndComputeSubscriptionInvoiceWorkflow:
		return TemporalTaskQueueInvoice
	case TemporalCustomerOnboardingWorkflow, TemporalPrepareProcessedEventsWorkflow, TemporalEnvironmentCloneWorkflow:
		return TemporalTaskQueueWorkflows
	case TemporalReprocessEventsWorkflow, TemporalReprocessRawEventsWorkflow, TemporalReprocessEventsForPlanWorkflow:
		return TemporalTaskQueueReprocessEvents
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
			TemporalSubscriptionChangeWorkflow,
			TemporalSubscriptionCreationWorkflow,
			TemporalStripeIntegrationWorkflow,
			TemporalHubSpotDealSyncWorkflow,
			TemporalHubSpotInvoiceSyncWorkflow,
			TemporalHubSpotQuoteSyncWorkflow,
			TemporalNomodInvoiceSyncWorkflow,
			TemporalMoyasarInvoiceSyncWorkflow,
			TemporalPaddleInvoiceSyncWorkflow,
			TemporalStripeInvoiceSyncWorkflow,
			TemporalRazorpayInvoiceSyncWorkflow,
			TemporalChargebeeInvoiceSyncWorkflow,
			TemporalQuickBooksInvoiceSyncWorkflow,
			TemporalZohoBooksInvoiceSyncWorkflow,
			TemporalStripeCustomerSyncWorkflow,
			TemporalRazorpayCustomerSyncWorkflow,
			TemporalChargebeeCustomerSyncWorkflow,
			TemporalQuickBooksCustomerSyncWorkflow,
			TemporalNomodCustomerSyncWorkflow,
			TemporalPaddleCustomerSyncWorkflow,
		}
	case TemporalTaskQueuePrice:
		return []TemporalWorkflowType{
			TemporalPriceSyncWorkflow,
			TemporalQuickBooksPriceSyncWorkflow,
		}
	case TemporalTaskQueueExport:
		return []TemporalWorkflowType{
			TemporalExecuteExportWorkflow,
		}
	case TemporalTaskQueueSubscription:
		return []TemporalWorkflowType{
			TemporalScheduleSubscriptionBillingWorkflow,
			TemporalProcessSubscriptionBillingWorkflow,
			TemporalRecalculateInvoiceWorkflow,
		}
	case TemporalTaskQueueInvoice:
		return []TemporalWorkflowType{
			TemporalProcessInvoiceWorkflow,
			TemporalFinalizeDraftInvoiceWorkflow,
			TemporalScheduleDraftFinalizationWorkflow,
			TemporalComputeInvoiceWorkflow,
			TemporalDraftAndComputeSubscriptionInvoiceWorkflow,
		}
	case TemporalTaskQueueWorkflows:
		return []TemporalWorkflowType{
			TemporalCustomerOnboardingWorkflow,
			TemporalPrepareProcessedEventsWorkflow,
			TemporalEnvironmentCloneWorkflow,
		}
	case TemporalTaskQueueReprocessEvents:
		return []TemporalWorkflowType{
			TemporalReprocessEventsWorkflow,
			TemporalReprocessRawEventsWorkflow,
			TemporalReprocessEventsForPlanWorkflow,
		}
	case TemporalTaskQueueCron:
		out := make([]TemporalWorkflowType, len(temporalCronWorkflowTypes))
		copy(out, temporalCronWorkflowTypes)
		return out
	default:
		return []TemporalWorkflowType{}
	}
}

// GetAllTaskQueues returns all available task queues
func GetAllTaskQueues() []TemporalTaskQueue {
	return []TemporalTaskQueue{
		TemporalTaskQueueTask,
		TemporalTaskQueuePrice,
		TemporalTaskQueueExport,
		TemporalTaskQueueSubscription,
		TemporalTaskQueueInvoice,
		TemporalTaskQueueWorkflows,
		TemporalTaskQueueReprocessEvents,
		TemporalTaskQueueCron,
	}
}

// WorkflowExecutionStatus represents the execution state of a Temporal workflow run.
// Values align with Temporal's workflow execution status.
type WorkflowExecutionStatus string

const (
	WorkflowExecutionStatusRunning        WorkflowExecutionStatus = "Running"
	WorkflowExecutionStatusCompleted      WorkflowExecutionStatus = "Completed"
	WorkflowExecutionStatusFailed         WorkflowExecutionStatus = "Failed"
	WorkflowExecutionStatusCanceled       WorkflowExecutionStatus = "Canceled"
	WorkflowExecutionStatusTerminated     WorkflowExecutionStatus = "Terminated"
	WorkflowExecutionStatusContinuedAsNew WorkflowExecutionStatus = "ContinuedAsNew"
	WorkflowExecutionStatusTimedOut       WorkflowExecutionStatus = "TimedOut"
	// WorkflowExecutionStatusUnknown is used when we have a DB record but haven't synced status from Temporal yet
	WorkflowExecutionStatusUnknown WorkflowExecutionStatus = "Unknown"
)
