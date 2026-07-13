package models

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// MeterUsageAlertWorkflowInput is the input to MeterUsageAlertWorkflow. The workflow
// runs once per (tenant, environment, customer) per debounce window; the first event
// for a customer schedules the workflow with StartDelay, subsequent events collide on
// the stable workflow ID and are absorbed by Temporal.
type MeterUsageAlertWorkflowInput struct {
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
	CustomerID    string `json:"customer_id"`
}

func (i MeterUsageAlertWorkflowInput) Validate() error {
	if i.TenantID == "" {
		return ierr.NewError("tenant_id is required").Mark(ierr.ErrValidation)
	}
	if i.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").Mark(ierr.ErrValidation)
	}
	if i.CustomerID == "" {
		return ierr.NewError("customer_id is required").Mark(ierr.ErrValidation)
	}
	return nil
}

// MeterUsageAlertActivityInput is shared by both alert-check activities. Same fields
// as the workflow input; kept as a separate type so activity signatures don't couple
// to workflow-level input evolution.
type MeterUsageAlertActivityInput struct {
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
	CustomerID    string `json:"customer_id"`
}
