package invoice

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// RegenerateDraftInvoicesBatchWorkflowInput is the input for the batch workflow that regenerates draft subscription invoices.
type RegenerateDraftInvoicesBatchWorkflowInput struct {
	Invoices []InvoiceTenantEnv `json:"invoices"`
}

// InvoiceTenantEnv holds invoice ID with its tenant and environment for context.
type InvoiceTenantEnv struct {
	InvoiceID     string `json:"invoice_id"`
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
}

// Validate validates the batch workflow input.
func (i *RegenerateDraftInvoicesBatchWorkflowInput) Validate() error {
	if len(i.Invoices) == 0 {
		return ierr.NewError("invoices list is required and must not be empty").
			WithHint("At least one invoice is required").
			Mark(ierr.ErrValidation)
	}
	for j, inv := range i.Invoices {
		if inv.InvoiceID == "" {
			return ierr.NewError("invoice_id is required for each item").
				WithHint("Invoice ID is required").
				WithReportableDetails(map[string]interface{}{"index": j}).
				Mark(ierr.ErrValidation)
		}
		if inv.TenantID == "" {
			return ierr.NewError("tenant_id is required for each item").
				WithHint("Tenant ID is required").
				WithReportableDetails(map[string]interface{}{"index": j, "invoice_id": inv.InvoiceID}).
				Mark(ierr.ErrValidation)
		}
		if inv.EnvironmentID == "" {
			return ierr.NewError("environment_id is required for each item").
				WithHint("Environment ID is required").
				WithReportableDetails(map[string]interface{}{"index": j, "invoice_id": inv.InvoiceID}).
				Mark(ierr.ErrValidation)
		}
	}
	return nil
}

// RegenerateDraftInvoicesBatchWorkflowResult is the result of the batch regeneration workflow/activity.
type RegenerateDraftInvoicesBatchWorkflowResult struct {
	Processed       int      `json:"processed"`
	Succeeded       int      `json:"succeeded"`
	Failed          int      `json:"failed"`
	FailedInvoiceIDs []string `json:"failed_invoice_ids,omitempty"`
}

// TriggerRegenerateDraftInvoicesActivityOutput is the optional output of the trigger activity.
type TriggerRegenerateDraftInvoicesActivityOutput struct {
	BatchesStarted int `json:"batches_started"`
	TotalInvoices  int `json:"total_invoices"`
}
