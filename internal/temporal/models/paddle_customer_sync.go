package models

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
)

type PaddleCustomerSyncWorkflowInput struct {
	CustomerID    string `json:"customer_id"`
	// InvoiceID is set when the workflow is triggered from invoice events without a customer id in the payload;
	// EnsureCustomerSyncedToPaddle resolves CustomerID by loading this invoice first.
	InvoiceID     string `json:"invoice_id,omitempty"`
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
}

func (input *PaddleCustomerSyncWorkflowInput) Validate() error {
	if input.CustomerID == "" && input.InvoiceID == "" {
		return ierr.NewError("customer ID or invoice ID is required").
			WithHint("Either CustomerID must be set, or InvoiceID for resolution from invoice").
			Mark(ierr.ErrValidation)
	}
	if input.TenantID == "" {
		return ierr.NewError("tenant_id is required").WithHint("TenantID must not be empty").Mark(ierr.ErrValidation)
	}
	if input.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").WithHint("EnvironmentID must not be empty").Mark(ierr.ErrValidation)
	}
	return nil
}
