package models

import ierr "github.com/flexprice/flexprice/internal/errors"

type ContractExtractionWorkflowInput struct {
	ContractID    string `json:"contract_id"`
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
	UserID        string `json:"user_id"`
}

func (input *ContractExtractionWorkflowInput) Validate() error {
	if input.ContractID == "" {
		return ierr.NewError("contract_id is required").WithHint("ContractID must not be empty").Mark(ierr.ErrValidation)
	}
	if input.TenantID == "" {
		return ierr.NewError("tenant_id is required").WithHint("TenantID must not be empty").Mark(ierr.ErrValidation)
	}
	if input.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").WithHint("EnvironmentID must not be empty").Mark(ierr.ErrValidation)
	}
	return nil
}

type MarkContractExtractionFailedInput struct {
	ContractExtractionWorkflowInput
	Reason string `json:"reason"`
}
