package admin

import (
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/flexprice/flexprice/internal/domain/user"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// AddUserRequest adds a user to an existing tenant, as the add-new-user script does.
type AddUserRequest struct {
	TenantID string `json:"tenant_id"`
	Email    string `json:"email"`
	Password string `json:"password"`
	// Role is optional and defaults to all_reader.
	Role types.Role `json:"role"`
}

type UserResponse struct {
	UserID   string   `json:"user_id"`
	Email    string   `json:"email"`
	TenantID string   `json:"tenant_id"`
	Roles    []string `json:"roles"`
}

func (r *AddUserRequest) Validate() error {
	if r == nil {
		return ierr.NewError("request is required").
			WithHint("Provide a request body").
			Mark(ierr.ErrValidation)
	}

	r.TenantID = strings.TrimSpace(r.TenantID)
	// Lowercased as Supabase and SAML store emails, so lookups and duplicate checks match.
	r.Email = strings.ToLower(strings.TrimSpace(r.Email))

	if r.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Provide the id of the tenant to add the user to").
			Mark(ierr.ErrValidation)
	}
	if r.Email == "" {
		return ierr.NewError("email is required").
			WithHint("Provide the email of the user to add").
			Mark(ierr.ErrValidation)
	}
	if !types.IsValidEmail(r.Email) {
		return ierr.NewError("invalid email").
			WithHint("Provide a valid email for the user").
			Mark(ierr.ErrValidation)
	}
	if utf8.RuneCountInString(r.Password) < minPasswordLength {
		return ierr.NewError("password is too short").
			WithHintf("password must be at least %d characters", minPasswordLength).
			Mark(ierr.ErrValidation)
	}
	allowed := types.UserTypeUser.AllowedRoles()
	if r.Role != "" && !slices.Contains(allowed, r.Role) {
		return ierr.NewError("invalid role").
			WithHintf("role must be one of: %v", allowed).
			Mark(ierr.ErrValidation)
	}
	return nil
}

func NewUserResponse(u *user.User) *UserResponse {
	if u == nil {
		return nil
	}
	return &UserResponse{
		UserID:   u.ID,
		Email:    u.Email,
		TenantID: u.TenantID,
		Roles:    u.Roles,
	}
}

// RemoveUserRequest removes a user from a tenant, picked by email.
type RemoveUserRequest struct {
	TenantID string `json:"tenant_id"`
	Email    string `json:"email"`
}

func (r *RemoveUserRequest) Validate() error {
	if r == nil {
		return ierr.NewError("request is required").
			WithHint("Provide a request body").
			Mark(ierr.ErrValidation)
	}

	r.TenantID = strings.TrimSpace(r.TenantID)
	// Lowercased as Supabase and SAML store emails, so lookups and duplicate checks match.
	r.Email = strings.ToLower(strings.TrimSpace(r.Email))

	if r.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Provide the id of the tenant to remove the user from").
			Mark(ierr.ErrValidation)
	}
	if r.Email == "" {
		return ierr.NewError("email is required").
			WithHint("Provide the email of the user to remove").
			Mark(ierr.ErrValidation)
	}
	if !types.IsValidEmail(r.Email) {
		return ierr.NewError("invalid email").
			WithHint("Provide a valid email for the user").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// RemoveUserResponse confirms which user was removed; their row stays archived for history.
type RemoveUserResponse struct {
	UserID   string       `json:"user_id"`
	Email    string       `json:"email"`
	TenantID string       `json:"tenant_id"`
	Status   types.Status `json:"status"`
}

// NewRemoveUserResponse confirms the removal of u, whose row is now archived.
func NewRemoveUserResponse(u *user.User) *RemoveUserResponse {
	if u == nil {
		return nil
	}
	return &RemoveUserResponse{
		UserID:   u.ID,
		Email:    u.Email,
		TenantID: u.TenantID,
		Status:   types.StatusArchived,
	}
}

// TenantUserResponse is one user or service account of a tenant. It carries no auth data.
type TenantUserResponse struct {
	ID        string         `json:"id"`
	Email     string         `json:"email"`
	Type      types.UserType `json:"type"`
	Roles     []string       `json:"roles"`
	Status    types.Status   `json:"status"`
	CreatedAt string         `json:"created_at"`
}

// ListUsersResponse holds every user and service account of one tenant.
type ListUsersResponse struct {
	Items []*TenantUserResponse `json:"items"`
}

func NewTenantUserResponse(u *user.User) *TenantUserResponse {
	if u == nil {
		return nil
	}
	roles := u.Roles
	if roles == nil {
		roles = []string{}
	}
	return &TenantUserResponse{
		ID:        u.ID,
		Email:     u.Email,
		Type:      u.Type,
		Roles:     roles,
		Status:    u.Status,
		CreatedAt: u.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func NewListUsersResponse(users []*user.User) *ListUsersResponse {
	items := make([]*TenantUserResponse, 0, len(users))
	for _, u := range users {
		items = append(items, NewTenantUserResponse(u))
	}
	return &ListUsersResponse{Items: items}
}
