package models

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// SendEmailWorkflowInput is the input of the send email workflow and its activities.
type SendEmailWorkflowInput struct {
	EmailLogID    string `json:"email_log_id"`
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
	UserID        string `json:"user_id"`
}

func (i *SendEmailWorkflowInput) Validate() error {
	if i.EmailLogID == "" {
		return ierr.NewError("email log ID is required").
			WithHint("Email log ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.TenantID == "" {
		return ierr.NewError("tenant ID is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.EnvironmentID == "" {
		return ierr.NewError("environment ID is required").
			WithHint("Environment ID is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// DeliverEmailActivityInput is one delivery attempt; AllowMissingAttachments sends without
// attachments that cannot be produced instead of retrying.
type DeliverEmailActivityInput struct {
	SendEmailWorkflowInput
	AllowMissingAttachments bool `json:"allow_missing_attachments"`
}
