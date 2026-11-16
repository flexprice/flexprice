package dto

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/environment"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

type CreateEnvironmentRequest struct {
	Name string `json:"name" validate:"required"`
	Type string `json:"type" validate:"required"`
}

type UpdateEnvironmentRequest struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type EnvironmentResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type ListEnvironmentsResponse struct {
	Environments []EnvironmentResponse `json:"environments"`
	Total        int                   `json:"total"`
	Offset       int                   `json:"offset"`
	Limit        int                   `json:"limit"`
}

// CloneEnvironmentRequest represents the request to clone an environment's published features and plans.
// If TargetEnvironmentID is provided, entities are cloned into that existing environment.
// If TargetEnvironmentID is omitted, a new environment is created from Name and Type, then entities are cloned into it.
type CloneEnvironmentRequest struct {
	// TargetEnvironmentID is the ID of an existing environment to clone into (optional).
	// When provided, Name and Type are ignored. When omitted, Name and Type are required.
	TargetEnvironmentID string `json:"target_environment_id,omitempty"`
	// Name of the new environment (required when target_environment_id is not provided)
	Name string `json:"name"`
	// Type of the new environment, e.g. "production" or "development" (required when target_environment_id is not provided)
	Type types.EnvironmentType `json:"type"`
}

func (r *CloneEnvironmentRequest) Validate() error {
	if r.TargetEnvironmentID != "" {
		// When cloning into an existing environment, Name and Type are not needed
		return nil
	}

	// Creating a new environment — Name and Type are required
	if r.Name == "" {
		return ierr.NewError("name is required when target_environment_id is not provided").
			WithHint("Provide a name for the new environment or specify a target_environment_id").
			Mark(ierr.ErrValidation)
	}
	if r.Type == "" {
		return ierr.NewError("type is required when target_environment_id is not provided").
			WithHint("Provide a type for the new environment or specify a target_environment_id").
			Mark(ierr.ErrValidation)
	}
	if r.Type != types.EnvironmentDevelopment && r.Type != types.EnvironmentProduction {
		return ierr.NewError("invalid environment type").
			WithHintf("type must be one of: %s, %s", types.EnvironmentDevelopment, types.EnvironmentProduction).
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (r *CreateEnvironmentRequest) Validate() error {
	return validator.ValidateRequest(r)
}

func (r *CreateEnvironmentRequest) ToEnvironment(ctx context.Context) *environment.Environment {
	return &environment.Environment{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENVIRONMENT),
		Name:      r.Name,
		Type:      types.EnvironmentType(r.Type),
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
}

func (r *UpdateEnvironmentRequest) Validate() error {
	return validator.ValidateRequest(r)
}

func NewEnvironmentResponse(e *environment.Environment) *EnvironmentResponse {
	return &EnvironmentResponse{
		ID:        e.ID,
		Name:      e.Name,
		Type:      string(e.Type),
		CreatedAt: e.CreatedAt.Format(time.RFC3339),
		UpdatedAt: e.UpdatedAt.Format(time.RFC3339),
	}
}
