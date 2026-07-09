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

// ===================== Outbound webhook stale retry =====================

// OutboundWebhookStaleRetryWorkflowInput is the input for OutboundWebhookStaleRetryWorkflow.
type OutboundWebhookStaleRetryWorkflowInput struct{}

// OutboundWebhookStaleRetryWorkflowResult captures bulk retry metrics.
type OutboundWebhookStaleRetryWorkflowResult struct {
	Total     int `json:"total"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
}

// ===================== Paddle Invoice Pull Sync =====================

// PaddleInvoicePullSyncCronInput is the input for PaddleInvoicePullSyncCronWorkflow.
// No fields required — the activity fetches all qualifying invoices itself.
type PaddleInvoicePullSyncCronInput struct{}

// PaddleInvoicePullSyncCronResult captures fan-out metrics.
type PaddleInvoicePullSyncCronResult struct {
	Total     int `json:"total"`
	Triggered int `json:"triggered"`
	Failed    int `json:"failed"`
}

// ===================== Moyasar AUTH payment refund =====================

// MoyasarReconcilePendingResult captures metrics for the pending-reconciliation pass.
type MoyasarReconcilePendingResult struct {
	Total     int `json:"total"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Skipped   int `json:"skipped"`
	Errors    int `json:"errors"`
}

// MoyasarVoidOrRefundResult captures metrics for the void/refund pass.
type MoyasarVoidOrRefundResult struct {
	Total    int `json:"total"`
	Voided   int `json:"voided"`
	Refunded int `json:"refunded"`
	Skipped  int `json:"skipped"`
	Errors   int `json:"errors"`
}

// MoyasarAuthPaymentSettlementWorkflowResult is the combined workflow result.
type MoyasarAuthPaymentSettlementWorkflowResult struct {
	Reconcile  MoyasarReconcilePendingResult `json:"reconcile"`
	VoidRefund MoyasarVoidOrRefundResult     `json:"void_refund"`
}

// ===================== Auto invoice threshold billing =====================

// AutoInvoiceThresholdBillingWorkflowInput is the input for AutoInvoiceThresholdBillingWorkflow.
// No fields required — the activity fetches all qualifying subscriptions itself.
type AutoInvoiceThresholdBillingWorkflowInput struct{}

// AutoInvoiceThresholdBillingWorkflowResult mirrors key counts from ProcessAutoInvoiceThresholdBilling.
type AutoInvoiceThresholdBillingWorkflowResult struct {
	TotalChecked  int `json:"total_checked"`
	TotalInvoiced int `json:"total_invoiced"`
	TotalSkipped  int `json:"total_skipped"`
	TotalFailed   int `json:"total_failed"`
}

// ===================== Checkout Session Expiry =====================

// CheckoutSessionExpiryWorkflowInput is the input for CheckoutSessionExpiryWorkflow.
type CheckoutSessionExpiryWorkflowInput struct{}

// CheckoutSessionExpiryWorkflowResult captures outcome metrics.
type CheckoutSessionExpiryWorkflowResult struct {
	Total     int `json:"total"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
}

// ===================== Razorpay Autocharge Claim Reconciliation =====================

// RazorpayReconciliationSweepWorkflowInput is the input for
// RazorpayReconciliationSweepWorkflow. No fields required — the activity
// fetches all stuck claims across tenants itself.
type RazorpayReconciliationSweepWorkflowInput struct{}

// RazorpayReconciliationSweepWorkflowResult captures outcome metrics for
// ResolveStuckClaims — design spec §8's reconciliation sweep over
// InvoiceCharge/TokenCycleCharge claims stuck in "claimed".
type RazorpayReconciliationSweepWorkflowResult struct {
	Total          int `json:"total"`
	Succeeded      int `json:"succeeded"`
	Failed         int `json:"failed"`
	StillPending   int `json:"still_pending"`
	AbandonedNoRef int `json:"abandoned_no_payment_ref"`
	Errors         int `json:"errors"`
}
