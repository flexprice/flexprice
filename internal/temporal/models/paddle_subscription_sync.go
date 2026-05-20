package models

import ierr "github.com/flexprice/flexprice/internal/errors"

// SubscriptionSyncStatusResult is returned by the CheckSubscriptionSyncStatus activity.
// It carries the resolved subscription/customer IDs so the parent workflow can pass them
// directly to the child PaddleSubscriptionSyncWorkflow without a second DB round-trip.
type SubscriptionSyncStatusResult struct {
	// Status is either "activated" (mapping exists or no Paddle connection) or "not_synced".
	Status         string `json:"status"`
	SubscriptionID string `json:"subscription_id"`
	CustomerID     string `json:"customer_id"`
}

// PaddleSubscriptionSyncWorkflowInput is the input for PaddleSubscriptionSyncWorkflow.
type PaddleSubscriptionSyncWorkflowInput struct {
	SubscriptionID string `json:"subscription_id"`
	CustomerID     string `json:"customer_id"`
	TenantID       string `json:"tenant_id"`
	EnvironmentID  string `json:"environment_id"`
}

func (i *PaddleSubscriptionSyncWorkflowInput) Validate() error {
	if i.SubscriptionID == "" {
		return ierr.NewError("subscription_id is required").
			WithHint("SubscriptionID must not be empty").
			Mark(ierr.ErrValidation)
	}
	if i.CustomerID == "" {
		return ierr.NewError("customer_id is required").
			WithHint("CustomerID must not be empty").
			Mark(ierr.ErrValidation)
	}
	if i.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("TenantID must not be empty").
			Mark(ierr.ErrValidation)
	}
	if i.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("EnvironmentID must not be empty").
			Mark(ierr.ErrValidation)
	}
	return nil
}
