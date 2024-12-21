package dto

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
)

type CreateEnvironmentRequest struct {
	Name string `json:"name" validate:"required"`
	Type string `json:"type" validate:"required"`
	Slug string `json:"slug" validate:"required"`
}

type UpdateEnvironmentRequest struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Slug string `json:"slug"`
}

type EnvironmentResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Slug      string `json:"slug"`
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
	return validator.New().Struct(r)
}

func (r *CreateEnvironmentRequest) ToEnvironment(ctx context.Context) *environment.Environment {
	return &environment.Environment{
		ID:        uuid.New().String(),
		Name:      r.Name,
		Type:      environment.EnvironmentType(r.Type),
		Slug:      r.Slug,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
}

func (r *UpdateEnvironmentRequest) Validate() error {
	return validator.New().Struct(r)
}

func NewEnvironmentResponse(e *environment.Environment) *EnvironmentResponse {
	return &EnvironmentResponse{
		ID:        e.ID,
		Name:      e.Name,
		Type:      string(e.Type),
		Slug:      e.Slug,
		CreatedAt: e.CreatedAt.Format(time.RFC3339),
		UpdatedAt: e.UpdatedAt.Format(time.RFC3339),
	}
}
