package admin

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/domain/tenant"
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
	// Lowercased as Supabase and SAML store emails, so lookups and duplicate checks match.
	r.Email = strings.ToLower(strings.TrimSpace(r.Email))

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

const (
	defaultTenantPageSize = 50
	maxTenantPageSize     = 100
)

// ListTenantsRequest filters and pages the tenant list. Every field is optional.
type ListTenantsRequest struct {
	// Search matches part of a tenant's name, its exact id, or the exact email of one of its users.
	Search         string                     `form:"search"`
	InternalStatus types.TenantInternalStatus `form:"internal_status"`
	Limit          int                        `form:"limit"`
	Offset         int                        `form:"offset"`
}

// TenantResponse is one row of the tenant list.
type TenantResponse struct {
	ID             string                     `json:"id"`
	Name           string                     `json:"name"`
	Status         types.Status               `json:"status"`
	InternalStatus types.TenantInternalStatus `json:"internal_status"`
	CreatedAt      string                     `json:"created_at"`
}

type ListTenantsResponse = types.ListResponse[*TenantResponse]

// Validate trims search and sets a limit of 0 to the default page size.
func (r *ListTenantsRequest) Validate() error {
	if r == nil {
		return ierr.NewError("request is required").
			WithHint("Provide the list query").
			Mark(ierr.ErrValidation)
	}

	r.Search = strings.TrimSpace(r.Search)
	if r.Limit == 0 {
		r.Limit = defaultTenantPageSize
	}

	if r.Limit < 1 || r.Limit > maxTenantPageSize {
		return ierr.NewError("invalid limit").
			WithHintf("limit must be between 1 and %d", maxTenantPageSize).
			Mark(ierr.ErrValidation)
	}
	if r.Offset < 0 {
		return ierr.NewError("invalid offset").
			WithHint("offset cannot be negative").
			Mark(ierr.ErrValidation)
	}
	if r.InternalStatus != "" {
		return r.InternalStatus.Validate()
	}
	return nil
}

func NewTenantResponse(t *tenant.Tenant) *TenantResponse {
	if t == nil {
		return nil
	}
	return &TenantResponse{
		ID:             t.ID,
		Name:           t.Name,
		Status:         t.Status,
		InternalStatus: t.InternalStatus,
		CreatedAt:      t.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// NewListTenantsResponse builds one page of the tenant list. total counts every match, not just this page.
func NewListTenantsResponse(page []*tenant.Tenant, total, limit, offset int) *ListTenantsResponse {
	items := make([]*TenantResponse, 0, len(page))
	for _, t := range page {
		items = append(items, NewTenantResponse(t))
	}
	resp := types.NewListResponse(items, total, limit, offset)
	return &resp
}

// TenantDetailResponse is the list row plus the fields the tenant's own page shows.
type TenantDetailResponse struct {
	TenantResponse
	BillingDetails *dto.TenantBillingDetails `json:"billing_details,omitempty"`
	Metadata       types.Metadata            `json:"metadata,omitempty"`
	UpdatedAt      string                    `json:"updated_at"`
}

func NewTenantDetailResponse(t *tenant.Tenant) *TenantDetailResponse {
	if t == nil {
		return nil
	}
	resp := &TenantDetailResponse{
		TenantResponse: *NewTenantResponse(t),
		Metadata:       t.Metadata,
		UpdatedAt:      t.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if t.BillingDetails != (tenant.TenantBillingDetails{}) {
		billing := dto.NewTenantBillingDetails(t.BillingDetails)
		resp.BillingDetails = &billing
	}
	return resp
}
