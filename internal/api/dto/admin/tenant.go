package admin

import (
	"strings"
	"unicode/utf8"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/domain/user"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// minPasswordLength matches public signup.
const minPasswordLength = 8

// CreateTenantRequest onboards a new tenant with its first user, as the onboard-tenant script does.
type CreateTenantRequest struct {
	TenantName       string `json:"tenant_name"`
	Email            string `json:"email"`
	Password         string `json:"password"`
	CreateProduction bool   `json:"create_production"`
}

type CreateTenantResponse struct {
	TenantID     string                 `json:"tenant_id"`
	TenantName   string                 `json:"tenant_name"`
	UserID       string                 `json:"user_id"`
	Email        string                 `json:"email"`
	Environments []*EnvironmentResponse `json:"environments"`
}

func (r *CreateTenantRequest) Validate() error {
	if r == nil {
		return ierr.NewError("request is required").
			WithHint("Provide a request body").
			Mark(ierr.ErrValidation)
	}

	r.TenantName = strings.TrimSpace(r.TenantName)
	r.Email = strings.TrimSpace(r.Email)

	if r.TenantName == "" {
		return ierr.NewError("tenant_name is required").
			WithHint("Provide a name for the tenant").
			Mark(ierr.ErrValidation)
	}
	if r.Email == "" {
		return ierr.NewError("email is required").
			WithHint("Provide the email of the tenant's first user").
			Mark(ierr.ErrValidation)
	}
	if !types.IsValidEmail(r.Email) {
		return ierr.NewError("invalid email").
			WithHint("Provide a valid email for the tenant's first user").
			Mark(ierr.ErrValidation)
	}
	if utf8.RuneCountInString(r.Password) < minPasswordLength {
		return ierr.NewError("password is too short").
			WithHintf("password must be at least %d characters", minPasswordLength).
			Mark(ierr.ErrValidation)
	}
	return nil
}

func NewCreateTenantResponse(t *dto.TenantResponse, u *user.User, envs []*environment.Environment) *CreateTenantResponse {
	if t == nil || u == nil {
		return nil
	}
	environments := make([]*EnvironmentResponse, 0, len(envs))
	for _, e := range envs {
		environments = append(environments, NewEnvironmentResponse(e))
	}
	return &CreateTenantResponse{
		TenantID:     t.ID,
		TenantName:   t.Name,
		UserID:       u.ID,
		Email:        u.Email,
		Environments: environments,
	}
}
