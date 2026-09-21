package admin

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/environment"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// CreateEnvironmentRequest creates an environment for a tenant.
// Cross-tenant on purpose: an operator, authorized by admin.secret, names the tenant by id or email.
type CreateEnvironmentRequest struct {
	Name     string                `json:"name"`
	Type     types.EnvironmentType `json:"type"`
	TenantID string                `json:"tenant_id"`
	Email    string                `json:"email"`
}

type EnvironmentResponse struct {
	ID        string                `json:"id"`
	Name      string                `json:"name"`
	Type      types.EnvironmentType `json:"type"`
	TenantID  string                `json:"tenant_id"`
	CreatedAt string                `json:"created_at"`
	UpdatedAt string                `json:"updated_at"`
}

func (r *CreateEnvironmentRequest) Validate() error {
	if r == nil {
		return ierr.NewError("request is required").
			WithHint("Provide a request body").
			Mark(ierr.ErrValidation)
	}

	r.Name = strings.TrimSpace(r.Name)
	r.Type = types.EnvironmentType(strings.TrimSpace(string(r.Type)))
	r.TenantID = strings.TrimSpace(r.TenantID)
	r.Email = strings.TrimSpace(r.Email)

	if r.Name == "" {
		return ierr.NewError("name is required").
			WithHint("Provide a name for the environment").
			Mark(ierr.ErrValidation)
	}
	if r.Type != types.EnvironmentDevelopment && r.Type != types.EnvironmentProduction {
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

func (r *CreateEnvironmentRequest) ToEnvironment(ctx context.Context) *environment.Environment {
	if r == nil {
		return nil
	}
	return &environment.Environment{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENVIRONMENT),
		Name:      r.Name,
		Type:      r.Type,
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
}

func NewEnvironmentResponse(e *environment.Environment) *EnvironmentResponse {
	if e == nil {
		return nil
	}
	return &EnvironmentResponse{
		ID:        e.ID,
		Name:      e.Name,
		Type:      e.Type,
		TenantID:  e.TenantID,
		CreatedAt: e.CreatedAt.Format(time.RFC3339),
		UpdatedAt: e.UpdatedAt.Format(time.RFC3339),
	}
}
