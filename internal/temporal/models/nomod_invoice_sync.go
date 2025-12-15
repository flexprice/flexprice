package models

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// NomodInvoiceSyncWorkflowInput contains the input for the Nomod invoice sync workflow
type NomodInvoiceSyncWorkflowInput struct {
	InvoiceID     string `json:"invoice_id"`
	CustomerID    string `json:"customer_id"`
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
}

// Validate validates the workflow input
func (input *NomodInvoiceSyncWorkflowInput) Validate() error {
	if input.InvoiceID == "" {
		return ierr.NewError("invoice_id is required").
			WithHint("InvoiceID must not be empty").
			Mark(ierr.ErrValidation)
	}
	if input.CustomerID == "" {
		return ierr.NewError("customer_id is required").
			WithHint("CustomerID must not be empty").
			Mark(ierr.ErrValidation)
	}
	if input.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("TenantID must not be empty").
			Mark(ierr.ErrValidation)
	}
	if input.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("EnvironmentID must not be empty").
			Mark(ierr.ErrValidation)
	}
	return nil
}

