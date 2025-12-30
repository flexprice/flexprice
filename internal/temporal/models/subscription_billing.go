package models

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/shopspring/decimal"
)

// ===================== Scheduler Workflow Models =====================

// SubscriptionSchedulerWorkflowInput represents the input for the subscription scheduler workflow
type SubscriptionSchedulerWorkflowInput struct {
	BatchSize int `json:"batch_size"`
}

// Validate validates the subscription scheduler workflow input
func (i *SubscriptionSchedulerWorkflowInput) Validate() error {
	if i.BatchSize <= 0 {
		i.BatchSize = 100 // Default to 100
	}
	return nil
}

// FetchSubscriptionBatchInput represents the input for fetching subscription batch
type FetchSubscriptionBatchInput struct {
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
	BatchSize     int    `json:"batch_size"`
	Offset        int    `json:"offset"`
}

// Validate validates the fetch subscription batch input
func (i *FetchSubscriptionBatchInput) Validate() error {
	if i.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("Environment ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.BatchSize <= 0 {
		i.BatchSize = 100
	}
	return nil
}

// FetchSubscriptionBatchOutput represents the output for fetching subscription batch
type FetchSubscriptionBatchOutput struct {
	SubscriptionIDs []string `json:"subscription_ids"`
}

// EnqueueSubscriptionWorkflowsInput represents the input for enqueueing subscription workflows
type EnqueueSubscriptionWorkflowsInput struct {
	SubscriptionIDs []string `json:"subscription_ids"`
	TenantID        string   `json:"tenant_id"`
	EnvironmentID   string   `json:"environment_id"`
}

// Validate validates the enqueue subscription workflows input
func (i *EnqueueSubscriptionWorkflowsInput) Validate() error {
	if len(i.SubscriptionIDs) == 0 {
		return ierr.NewError("subscription_ids is required").
			WithHint("At least one subscription ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("Environment ID is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// ===================== Single Subscription Workflow Models =====================

// ProcessSingleSubscriptionWorkflowInput represents the input for processing a single subscription
type ProcessSingleSubscriptionWorkflowInput struct {
	SubscriptionID string `json:"subscription_id"`
	TenantID       string `json:"tenant_id"`
	EnvironmentID  string `json:"environment_id"`
}

// Validate validates the process single subscription workflow input
func (i *ProcessSingleSubscriptionWorkflowInput) Validate() error {
	if i.SubscriptionID == "" {
		return ierr.NewError("subscription_id is required").
			WithHint("Subscription ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("Environment ID is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// ProcessSingleSubscriptionWorkflowResult represents the result of processing a single subscription
type ProcessSingleSubscriptionWorkflowResult struct {
	SubscriptionID     string    `json:"subscription_id"`
	Status             string    `json:"status"`
	BillingPeriodsProcessed int       `json:"billing_periods_processed"`
	InvoicesCreated    []string  `json:"invoices_created"`
	ErrorSummary       *string   `json:"error_summary,omitempty"`
	CompletedAt        time.Time `json:"completed_at"`
}

// ===================== Activity Models =====================

// CheckPauseActivityInput represents the input for checking if a subscription is paused
type CheckPauseActivityInput struct {
	SubscriptionID string `json:"subscription_id"`
	TenantID       string `json:"tenant_id"`
	EnvironmentID  string `json:"environment_id"`
}

// Validate validates the check pause activity input
func (i *CheckPauseActivityInput) Validate() error {
	if i.SubscriptionID == "" {
		return ierr.NewError("subscription_id is required").
			WithHint("Subscription ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("Environment ID is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// CheckPauseActivityOutput represents the output for checking if a subscription is paused
type CheckPauseActivityOutput struct {
	IsPaused bool `json:"is_paused"`
}

// CalculateBillingPeriodsActivityInput represents the input for calculating billing periods
type CalculateBillingPeriodsActivityInput struct {
	SubscriptionID string `json:"subscription_id"`
	TenantID       string `json:"tenant_id"`
	EnvironmentID  string `json:"environment_id"`
}

// Validate validates the calculate billing periods activity input
func (i *CalculateBillingPeriodsActivityInput) Validate() error {
	if i.SubscriptionID == "" {
		return ierr.NewError("subscription_id is required").
			WithHint("Subscription ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("Environment ID is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// BillingPeriod represents a single billing period
type BillingPeriod struct {
	SubscriptionID string          `json:"subscription_id"`
	StartDate      time.Time       `json:"start_date"`
	EndDate        time.Time       `json:"end_date"`
	Amount         decimal.Decimal `json:"amount"`
}

// CalculateBillingPeriodsActivityOutput represents the output for calculating billing periods
type CalculateBillingPeriodsActivityOutput struct {
	BillingPeriods []BillingPeriod `json:"billing_periods"`
}

// CreateInvoiceActivityInput represents the input for creating an invoice
type CreateInvoiceActivityInput struct {
	SubscriptionID string        `json:"subscription_id"`
	TenantID       string        `json:"tenant_id"`
	EnvironmentID  string        `json:"environment_id"`
	Period         BillingPeriod `json:"period"`
}

// Validate validates the create invoice activity input
func (i *CreateInvoiceActivityInput) Validate() error {
	if i.SubscriptionID == "" {
		return ierr.NewError("subscription_id is required").
			WithHint("Subscription ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("Environment ID is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// CreateInvoiceActivityOutput represents the output for creating an invoice
type CreateInvoiceActivityOutput struct {
	InvoiceID string `json:"invoice_id"`
}

// SyncToExternalVendorActivityInput represents the input for syncing invoice to external vendors
type SyncToExternalVendorActivityInput struct {
	InvoiceID      string `json:"invoice_id"`
	SubscriptionID string `json:"subscription_id"`
	TenantID       string `json:"tenant_id"`
	EnvironmentID  string `json:"environment_id"`
}

// Validate validates the sync to external vendor activity input
func (i *SyncToExternalVendorActivityInput) Validate() error {
	if i.InvoiceID == "" {
		return ierr.NewError("invoice_id is required").
			WithHint("Invoice ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.SubscriptionID == "" {
		return ierr.NewError("subscription_id is required").
			WithHint("Subscription ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("Environment ID is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// AttemptPaymentActivityInput represents the input for attempting payment
type AttemptPaymentActivityInput struct {
	InvoiceID      string `json:"invoice_id"`
	SubscriptionID string `json:"subscription_id"`
	TenantID       string `json:"tenant_id"`
	EnvironmentID  string `json:"environment_id"`
}

// Validate validates the attempt payment activity input
func (i *AttemptPaymentActivityInput) Validate() error {
	if i.InvoiceID == "" {
		return ierr.NewError("invoice_id is required").
			WithHint("Invoice ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.SubscriptionID == "" {
		return ierr.NewError("subscription_id is required").
			WithHint("Subscription ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("Environment ID is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// UpdateSubscriptionPeriodActivityInput represents the input for updating subscription period
type UpdateSubscriptionPeriodActivityInput struct {
	SubscriptionID string `json:"subscription_id"`
	TenantID       string `json:"tenant_id"`
	EnvironmentID  string `json:"environment_id"`
}

// Validate validates the update subscription period activity input
func (i *UpdateSubscriptionPeriodActivityInput) Validate() error {
	if i.SubscriptionID == "" {
		return ierr.NewError("subscription_id is required").
			WithHint("Subscription ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("Environment ID is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

