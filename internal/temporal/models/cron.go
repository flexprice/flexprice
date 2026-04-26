package models

import "time"

// ===================== Credit Grant Processing =====================

// CreditGrantProcessingWorkflowInput is the input for CreditGrantProcessingWorkflow.
// No fields required — the activity fetches all pending applications across tenants.
type CreditGrantProcessingWorkflowInput struct{}

// CreditGrantProcessingWorkflowResult captures outcome metrics.
type CreditGrantProcessingWorkflowResult struct {
	Processed int `json:"processed"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
}

// ===================== Subscription Auto-Cancellation =====================

// SubscriptionAutoCancellationWorkflowInput is the input for SubscriptionAutoCancellationWorkflow.
type SubscriptionAutoCancellationWorkflowInput struct{}

// SubscriptionAutoCancellationWorkflowResult is returned by the auto-cancellation activity.
// Add fields when you expose real counts from the service.
type SubscriptionAutoCancellationWorkflowResult struct{}

// ===================== Wallet Credit Expiry =====================

// WalletCreditExpiryWorkflowInput is the input for WalletCreditExpiryWorkflow.
type WalletCreditExpiryWorkflowInput struct{}

// WalletCreditExpiryWorkflowResult captures outcome metrics.
type WalletCreditExpiryWorkflowResult struct {
	Total                          int `json:"total"`
	Succeeded                      int `json:"succeeded"`
	Failed                         int `json:"failed"`
	SkippedDueToActiveSubscription int `json:"skipped_due_to_active_subscription"`
	SkippedDueToActiveInvoice      int `json:"skipped_due_to_active_invoice"`
}

// SubscriptionBillingPeriodsWorkflowInput is the input for SubscriptionBillingPeriodsWorkflow.
type SubscriptionBillingPeriodsWorkflowInput struct{}

// SubscriptionBillingPeriodsWorkflowResult is a placeholder; HTTP cron returns the full DTO to callers.
type SubscriptionBillingPeriodsWorkflowResult struct{}

// SubscriptionRenewalDueAlertsWorkflowInput is the input for SubscriptionRenewalDueAlertsWorkflow.
type SubscriptionRenewalDueAlertsWorkflowInput struct{}

// SubscriptionRenewalDueAlertsWorkflowResult is a placeholder.
type SubscriptionRenewalDueAlertsWorkflowResult struct{}

// SubscriptionTrialEndDueWorkflowInput is the input for SubscriptionTrialEndDueWorkflow.
type SubscriptionTrialEndDueWorkflowInput struct{}

// SubscriptionTrialEndDueWorkflowResult mirrors key counts from ProcessTrialEndDue for schedule runs.
type SubscriptionTrialEndDueWorkflowResult struct {
	TotalSuccess int       `json:"total_success"`
	TotalFailed  int       `json:"total_failed"`
	StartAt      time.Time `json:"start_at"`
}
