package models

import ierr "github.com/flexprice/flexprice/internal/errors"

// TabsInvoiceSyncWorkflowInput contains input for the Tabs invoice sync workflow.
type TabsInvoiceSyncWorkflowInput struct {
	InvoiceID     string `json:"invoice_id"`
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
}

func (input *TabsInvoiceSyncWorkflowInput) Validate() error {
	if input.InvoiceID == "" {
		return ierr.NewError("invoice_id is required").WithHint("InvoiceID must not be empty").Mark(ierr.ErrValidation)
	}
	if input.TenantID == "" {
		return ierr.NewError("tenant_id is required").WithHint("TenantID must not be empty").Mark(ierr.ErrValidation)
	}
	if input.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").WithHint("EnvironmentID must not be empty").Mark(ierr.ErrValidation)
	}
	return nil
}
