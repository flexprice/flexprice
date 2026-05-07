package invoice

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// ===================== Process Invoice Workflow Models =====================

// ProcessInvoiceWorkflowInput represents the input for processing a single invoice
type ProcessInvoiceWorkflowInput struct {
	InvoiceID     string `json:"invoice_id"`
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
	UserID        string `json:"user_id"`
}

// Validate validates the process invoice workflow input
func (i *ProcessInvoiceWorkflowInput) Validate() error {
	if i.InvoiceID == "" {
		return ierr.NewError("invoice_id is required").
			WithHint("Invoice ID is required").
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

// ProcessInvoiceWorkflowResult represents the result of processing an invoice
type ProcessInvoiceWorkflowResult struct {
	Success     bool      `json:"success"`
	Error       *string   `json:"error,omitempty"`
	CompletedAt time.Time `json:"completed_at"`
}

// ===================== Compute Invoice Activity Models =====================

// ComputeInvoiceActivityInput represents the input for computing an invoice
type ComputeInvoiceActivityInput struct {
	InvoiceID     string `json:"invoice_id"`
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
	UserID        string `json:"user_id"`
}

// Validate validates the compute invoice activity input
func (i *ComputeInvoiceActivityInput) Validate() error {
	if i.InvoiceID == "" {
		return ierr.NewError("invoice_id is required").
			WithHint("Invoice ID is required").
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

// ComputeInvoiceActivityOutput represents the output (Skipped true = zero-dollar, no finalize/sync/payment)
type ComputeInvoiceActivityOutput struct {
	Skipped bool `json:"skipped"`
}

// ===================== Finalize Invoice Activity Models =====================

// FinalizeInvoiceActivityInput represents the input for finalizing an invoice
type FinalizeInvoiceActivityInput struct {
	InvoiceID     string `json:"invoice_id"`
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
	UserID        string `json:"user_id"`
}

// Validate validates the finalize invoice activity input
func (i *FinalizeInvoiceActivityInput) Validate() error {
	if i.InvoiceID == "" {
		return ierr.NewError("invoice_id is required").
			WithHint("Invoice ID is required").
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

// FinalizeInvoiceActivityOutput represents the output for finalizing an invoice
type FinalizeInvoiceActivityOutput struct {
	Success bool `json:"success"`
	Skipped bool `json:"skipped"` // true if finalization delay has not yet elapsed
}

// ===================== Sync Invoice Activity Models =====================

// SyncInvoiceActivityInput represents the input for syncing a single invoice
type SyncInvoiceActivityInput struct {
	InvoiceID     string `json:"invoice_id"`
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
	UserID        string `json:"user_id"`
}

// Validate validates the sync invoice activity input
func (i *SyncInvoiceActivityInput) Validate() error {
	if i.InvoiceID == "" {
		return ierr.NewError("invoice_id is required").
			WithHint("Invoice ID is required").
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

// SyncInvoiceActivityOutput represents the output for syncing an invoice
type SyncInvoiceActivityOutput struct {
	Success bool `json:"success"`
}

// ===================== Attempt Payment Activity Models =====================

// PaymentActivityInput represents the input for attempting payment on a single invoice
type PaymentActivityInput struct {
	InvoiceID     string `json:"invoice_id"`
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
	UserID        string `json:"user_id"`
}

// Validate validates the payment activity input
func (i *PaymentActivityInput) Validate() error {
	if i.InvoiceID == "" {
		return ierr.NewError("invoice_id is required").
			WithHint("Invoice ID is required").
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

// PaymentActivityOutput represents the output for attempting payment
type PaymentActivityOutput struct {
	Success bool `json:"success"`
}

// ===================== Trigger Invoice Workflow Activity Models =====================

// TriggerInvoiceWorkflowActivityInput represents the input for triggering invoice workflows
type TriggerInvoiceWorkflowActivityInput struct {
	InvoiceIDs    []string `json:"invoice_ids"`
	TenantID      string   `json:"tenant_id"`
	EnvironmentID string   `json:"environment_id"`
	UserID        string   `json:"user_id"`
}

// Validate validates the trigger invoice workflow activity input
func (i *TriggerInvoiceWorkflowActivityInput) Validate() error {
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

// TriggerInvoiceWorkflowActivityOutput represents the output for triggering invoice workflows
type TriggerInvoiceWorkflowActivityOutput struct {
	TriggeredCount int      `json:"triggered_count"`
	FailedCount    int      `json:"failed_count"`
	FailedInvoices []string `json:"failed_invoices,omitempty"`
}

// ===================== Schedule Draft Finalization Models =====================

// ScheduleDraftFinalizationWorkflowInput represents the input for the scheduled draft finalization workflow
type ScheduleDraftFinalizationWorkflowInput struct {
	BatchSize int `json:"batch_size"`
}

// ScheduleDraftFinalizationWorkflowResult represents the result of the scheduled draft finalization workflow
type ScheduleDraftFinalizationWorkflowResult struct {
	TotalProcessed int `json:"total_processed"`
	FinalizedCount int `json:"finalized_count"`
	SkippedCount   int `json:"skipped_count"`
	FailedCount    int `json:"failed_count"`
}

// ===================== Compute Invoice Workflow Models =====================

// ComputeInvoiceWorkflowInput represents the input for the compute invoice workflow
type ComputeInvoiceWorkflowInput struct {
	InvoiceID     string `json:"invoice_id"`
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
	UserID        string `json:"user_id"`
}

// Validate validates the compute invoice workflow input
func (i *ComputeInvoiceWorkflowInput) Validate() error {
	if i.InvoiceID == "" {
		return ierr.NewError("invoice_id is required").
			WithHint("Invoice ID is required").
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

// ComputeInvoiceWorkflowResult represents the result of computing an invoice
type ComputeInvoiceWorkflowResult struct {
	Success     bool      `json:"success"`
	Skipped     bool      `json:"skipped"`
	CompletedAt time.Time `json:"completed_at"`
}

// ===================== Draft and compute subscription invoice workflow =====================

// DraftAndComputeSubscriptionInvoiceWorkflowInput is input for DraftAndComputeSubscriptionInvoiceWorkflow.
type DraftAndComputeSubscriptionInvoiceWorkflowInput struct {
	SubscriptionID string `json:"subscription_id"`
	TenantID       string `json:"tenant_id"`
	EnvironmentID  string `json:"environment_id"`
	UserID         string `json:"user_id"`
}

// Validate validates the draft-and-compute subscription invoice workflow input.
func (i *DraftAndComputeSubscriptionInvoiceWorkflowInput) Validate() error {
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

// DraftAndComputeSubscriptionInvoiceWorkflowResult is the workflow result.
type DraftAndComputeSubscriptionInvoiceWorkflowResult struct {
	InvoiceID      string    `json:"invoice_id"`
	ComputeSkipped bool      `json:"compute_skipped"`
	Success        bool      `json:"success"`
	CompletedAt    time.Time `json:"completed_at"`
}

// CreateDraftForCurrentSubscriptionPeriodActivityInput is input for CreateDraftForCurrentSubscriptionPeriodActivity.
type CreateDraftForCurrentSubscriptionPeriodActivityInput struct {
	SubscriptionID string `json:"subscription_id"`
	TenantID       string `json:"tenant_id"`
	EnvironmentID  string `json:"environment_id"`
	UserID         string `json:"user_id"`
}

// Validate validates the activity input.
func (i *CreateDraftForCurrentSubscriptionPeriodActivityInput) Validate() error {
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

// CreateDraftForCurrentSubscriptionPeriodActivityOutput is returned after creating the idempotent draft.
type CreateDraftForCurrentSubscriptionPeriodActivityOutput struct {
	InvoiceID string `json:"invoice_id"`
}

// FinalizeDueDraftsActivityInput represents the input for the finalize due drafts activity
type FinalizeDueDraftsActivityInput struct {
	BatchSize int `json:"batch_size"`
}
