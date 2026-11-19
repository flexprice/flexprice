package models

import ierr "github.com/flexprice/flexprice/internal/errors"

// ZohoBooksCreditNoteSyncWorkflowInput contains input for the Zoho Books credit note sync workflow.
type ZohoBooksCreditNoteSyncWorkflowInput struct {
	CreditNoteID  string `json:"credit_note_id"`
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
}

func (input *ZohoBooksCreditNoteSyncWorkflowInput) Validate() error {
	if input.CreditNoteID == "" {
		return ierr.NewError("credit_note_id is required").WithHint("CreditNoteID must not be empty").Mark(ierr.ErrValidation)
	}
	if input.TenantID == "" {
		return ierr.NewError("tenant_id is required").WithHint("TenantID must not be empty").Mark(ierr.ErrValidation)
	}
	if input.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").WithHint("EnvironmentID must not be empty").Mark(ierr.ErrValidation)
	}
	return nil
}
