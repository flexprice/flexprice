package models

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// ChargebeeInvoiceSyncWorkflowInput contains the input for the Chargebee invoice sync workflow
type ChargebeeInvoiceSyncWorkflowInput struct {
	InvoiceID     string `json:"invoice_id"`
	CustomerID    string `json:"customer_id"`
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
}

// Validate validates the workflow input
func (input *ChargebeeInvoiceSyncWorkflowInput) Validate() error {
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
