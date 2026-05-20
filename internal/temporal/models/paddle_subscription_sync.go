package models

import ierr "github.com/flexprice/flexprice/internal/errors"

// PaddleSubscriptionSyncWorkflowInput is the input for PaddleSubscriptionSyncWorkflow.
type PaddleSubscriptionSyncWorkflowInput struct {
	SubscriptionID string `json:"subscription_id"`
	CustomerID     string `json:"customer_id"`
	TenantID       string `json:"tenant_id"`
	EnvironmentID  string `json:"environment_id"`
}

func (i *PaddleSubscriptionSyncWorkflowInput) Validate() error {
	if i.SubscriptionID == "" {
		return ierr.NewError("subscription_id is required").Mark(ierr.ErrValidation)
	}
	if i.TenantID == "" {
		return ierr.NewError("tenant_id is required").Mark(ierr.ErrValidation)
	}
	if i.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").Mark(ierr.ErrValidation)
	}
	return nil
}
