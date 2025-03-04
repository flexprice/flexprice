package dto

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/environment"
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
