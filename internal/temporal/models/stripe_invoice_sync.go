package models

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// StripeInvoiceSyncWorkflowInput contains the input for the Stripe invoice sync workflow
type StripeInvoiceSyncWorkflowInput struct {
	InvoiceID        string `json:"invoice_id"`
	CustomerID       string `json:"customer_id"`
	CollectionMethod string `json:"collection_method,omitempty"`
	TenantID         string `json:"tenant_id"`
	EnvironmentID    string `json:"environment_id"`
}

// Validate validates the workflow input
func (input *StripeInvoiceSyncWorkflowInput) Validate() error {
	if input.InvoiceID == "" {
		return ierr.NewError("invoice_id is required").
			WithHint("InvoiceID must not be empty").
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
