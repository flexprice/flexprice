package models

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// BillingEntityEnableWorkflowInput is the input of the billing entities enable workflow and its activities.
type BillingEntityEnableWorkflowInput struct {
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
}

func (i *BillingEntityEnableWorkflowInput) Validate() error {
	if i.TenantID == "" {
		return ierr.NewError("tenant ID is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}
