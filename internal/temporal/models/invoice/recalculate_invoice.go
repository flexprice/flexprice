package invoice

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// ===================== Recalculate Invoice (voided) Workflow Models =====================

// RecalculateInvoiceWorkflowInput represents the input for recalculating a voided subscription invoice (creates replacement invoice).
type RecalculateInvoiceWorkflowInput struct {
	InvoiceID     string `json:"invoice_id"`
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
	UserID        string `json:"user_id"`
}

// Validate validates the recalculate invoice workflow input
func (i *RecalculateInvoiceWorkflowInput) Validate() error {
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

// RecalculateInvoiceWorkflowResult represents the result of recalculating a voided invoice.
type RecalculateInvoiceWorkflowResult struct {
	Success     bool      `json:"success"`
	Error       *string   `json:"error,omitempty"`
	CompletedAt time.Time `json:"completed_at"`
	InvoiceID   string    `json:"invoice_id,omitempty"`
}

// ===================== Recalculate Invoice Activity Models =====================

// RecalculateInvoiceActivityInput represents the input for the recalculate invoice (voided) activity
type RecalculateInvoiceActivityInput struct {
	InvoiceID     string `json:"invoice_id"`
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
	UserID        string `json:"user_id"`
}

// Validate validates the recalculate invoice activity input
func (i *RecalculateInvoiceActivityInput) Validate() error {
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

// RecalculateInvoiceActivityOutput represents the output for the recalculate invoice activity
type RecalculateInvoiceActivityOutput struct {
	Success   bool   `json:"success"`
	InvoiceID string `json:"invoice_id,omitempty"`
}
