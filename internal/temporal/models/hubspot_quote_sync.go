package models

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// HubSpotQuoteSyncWorkflowInput contains the input for the HubSpot quote sync workflow
type HubSpotQuoteSyncWorkflowInput struct {
	SubscriptionID string `json:"subscription_id"`
	CustomerID     string `json:"customer_id"`
	DealID         string `json:"deal_id"`
	TenantID       string `json:"tenant_id"`
	EnvironmentID  string `json:"environment_id"`
}

// Validate validates the workflow input
func (input *HubSpotQuoteSyncWorkflowInput) Validate() error {
	if input.SubscriptionID == "" {
		return ierr.NewError("subscription_id is required").
			WithHint("SubscriptionID must not be empty").
			Mark(ierr.ErrValidation)
	}
	if input.CustomerID == "" {
		return ierr.NewError("customer_id is required").
			WithHint("CustomerID must not be empty").
			Mark(ierr.ErrValidation)
	}
	if input.DealID == "" {
		return ierr.NewError("deal_id is required").
			WithHint("DealID must not be empty").
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

