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

// ===================== Calculate Invoice Activity Models =====================

// CalculateInvoiceActivityInput represents the input for calculating and populating an invoice
type CalculateInvoiceActivityInput struct {
	InvoiceID     string `json:"invoice_id"`
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
	UserID        string `json:"user_id"`
}

// Validate validates the calculate invoice activity input
func (i *CalculateInvoiceActivityInput) Validate() error {
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

// CalculateInvoiceActivityOutput represents the output for calculating an invoice
type CalculateInvoiceActivityOutput struct {
	Skipped bool `json:"skipped"`
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
