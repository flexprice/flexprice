package models

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
)

type NomodCustomerSyncWorkflowInput struct {
	CustomerID    string `json:"customer_id"`
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
}

func (input *NomodCustomerSyncWorkflowInput) Validate() error {
	if input.CustomerID == "" {
		return ierr.NewError("customer_id is required").WithHint("CustomerID must not be empty").Mark(ierr.ErrValidation)
	}
	if input.TenantID == "" {
		return ierr.NewError("tenant_id is required").WithHint("TenantID must not be empty").Mark(ierr.ErrValidation)
	}
	if input.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").WithHint("EnvironmentID must not be empty").Mark(ierr.ErrValidation)
	}
	return nil
}
