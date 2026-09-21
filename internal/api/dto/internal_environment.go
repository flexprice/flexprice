package dto

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/environment"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// InternalCreateEnvironmentRequest creates an environment for a tenant from the
// internal router. The tenant is identified by tenant_id, by a user's email, or both.
// When both are set they must refer to the same tenant.
type InternalCreateEnvironmentRequest struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	TenantID string `json:"tenant_id"`
	Email    string `json:"email"`
}

type InternalEnvironmentResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	TenantID  string `json:"tenant_id"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func (r *InternalCreateEnvironmentRequest) Validate() error {
	if r == nil {
		return ierr.NewError("request is required").
			WithHint("Provide a request body").
			Mark(ierr.ErrValidation)
	}

	r.Name = strings.TrimSpace(r.Name)
	r.Type = strings.TrimSpace(r.Type)
	r.TenantID = strings.TrimSpace(r.TenantID)
	r.Email = strings.TrimSpace(r.Email)

	if r.Name == "" {
		return ierr.NewError("name is required").
			WithHint("Provide a name for the environment").
			Mark(ierr.ErrValidation)
	}
	if r.Type != string(types.EnvironmentDevelopment) && r.Type != string(types.EnvironmentProduction) {
		return ierr.NewError("invalid environment type").
			WithHintf("type must be one of: %s, %s", types.EnvironmentDevelopment, types.EnvironmentProduction).
			Mark(ierr.ErrValidation)
	}
	if r.TenantID == "" && r.Email == "" {
		return ierr.NewError("tenant_id or email is required").
			WithHint("Provide tenant_id or the email of a user in the tenant").
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (r *InternalCreateEnvironmentRequest) ToEnvironment(ctx context.Context) *environment.Environment {
	if r == nil {
		return nil
	}
	return &environment.Environment{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENVIRONMENT),
		Name:      r.Name,
		Type:      types.EnvironmentType(r.Type),
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
}

func NewInternalEnvironmentResponse(e *environment.Environment) *InternalEnvironmentResponse {
	if e == nil {
		return nil
	}
	return &InternalEnvironmentResponse{
		ID:        e.ID,
		Name:      e.Name,
		Type:      string(e.Type),
		TenantID:  e.TenantID,
		CreatedAt: e.CreatedAt.Format(time.RFC3339),
		UpdatedAt: e.UpdatedAt.Format(time.RFC3339),
	}
}
