package models

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// AddonPriceSyncWorkflowInput represents input for the addon usage-price sync workflow
type AddonPriceSyncWorkflowInput struct {
	AddonID       string `json:"addon_id"`
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
	UserID        string `json:"user_id"`
}

func (a *AddonPriceSyncWorkflowInput) Validate() error {
	if a.AddonID == "" {
		return ierr.NewError("addon ID is required").
			WithHint("Addon ID is required").
			Mark(ierr.ErrValidation)
	}

	if a.TenantID == "" || a.EnvironmentID == "" || a.UserID == "" {
		return ierr.NewError("tenant ID, environment ID and user ID are required").
			WithHint("Tenant ID, environment ID and user ID are required").
			Mark(ierr.ErrValidation)
	}

	return nil
}
