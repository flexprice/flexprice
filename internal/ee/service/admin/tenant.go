package admin

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/flexprice/flexprice/internal/api/dto"
	admindto "github.com/flexprice/flexprice/internal/api/dto/admin"
	"github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/nedpals/supabase-go"
)

// TenantService onboards a new tenant with its first user, as the onboard-tenant script does.
// The user's login is created in Supabase, so it needs auth.provider supabase.
type TenantService interface {
	CreateTenant(ctx context.Context, req admindto.CreateTenantRequest) (*admindto.CreateTenantResponse, error)
}

type tenantService struct {
	service.ServiceParams
}

func NewTenantService(params service.ServiceParams) TenantService {
	return &tenantService{ServiceParams: params}
}

func (s *tenantService) CreateTenant(ctx context.Context, req admindto.CreateTenantRequest) (*admindto.CreateTenantResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if s.Config == nil || s.Config.Auth.Provider != types.AuthProviderSupabase {
		return nil, ierr.NewError("supabase auth is not configured").
			WithHint("Creating a user with a password needs auth.provider supabase").
			Mark(ierr.ErrInvalidOperation)
	}
	if err := s.requireUnusedEmail(ctx, req.Email); err != nil {
		return nil, err
	}

	tenantID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TENANT)
	ctx = types.SetTenantID(ctx, tenantID)

	userID, err := s.createLogin(ctx, req.Email, req.Password, tenantID)
	if err != nil {
		return nil, err
	}
	ctx = types.SetUserID(ctx, userID)

	owner := newOwner(ctx, userID, req.Email)
	envs := newEnvironments(ctx, req.CreateProduction)

	var created *dto.TenantResponse
	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		// Same path as self-serve signup, so the tenant also becomes a billing customer.
		t, err := service.NewTenantService(s.ServiceParams).CreateTenant(ctx, dto.CreateTenantRequest{ID: tenantID, Name: req.TenantName})
		if err != nil {
			return err
		}
		created = t
		if err := s.UserRepo.Create(ctx, owner); err != nil {
			return err
		}
		for _, env := range envs {
			if err := s.EnvironmentRepo.Create(ctx, env); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		s.removeLogin(ctx, userID)
		return nil, err
	}

	s.Logger.Info(ctx, "created tenant", "tenant_id", tenantID, "user_id", userID, "production", req.CreateProduction)
	return admindto.NewCreateTenantResponse(created, owner, envs), nil
}

// requireUnusedEmail refuses an email that already belongs to a user in any tenant.
func (s *tenantService) requireUnusedEmail(ctx context.Context, email string) error {
	existing, err := s.UserRepo.GetByEmail(ctx, email)
	if ierr.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return ierr.NewError("email already in use").
		WithHint("A user with this email already exists").
		WithReportableDetails(map[string]interface{}{"email": email, "tenant_id": existing.TenantID}).
		Mark(ierr.ErrAlreadyExists)
}

// createLogin creates a confirmed Supabase user with the given password, tagged with the tenant.
func (s *tenantService) createLogin(ctx context.Context, email, password, tenantID string) (string, error) {
	client := supabase.CreateClient(s.Config.Auth.Supabase.BaseURL, s.Config.Auth.Supabase.ServiceKey)
	created, err := client.Admin.CreateUser(ctx, supabase.AdminUserParams{
		Email:        email,
		Password:     &password,
		EmailConfirm: true,
		AppMetadata: map[string]interface{}{
			"tenant_id": tenantID,
		},
	})
	if err != nil {
		return "", supabaseError(err, email)
	}
	return created.ID, nil
}

// removeLogin deletes a Supabase user whose tenant was not saved, so the email can be retried.
func (s *tenantService) removeLogin(ctx context.Context, userID string) {
	if err := auth.NewSupabaseAuth(s.Config).RemoveUser(context.WithoutCancel(ctx), userID); err != nil {
		s.Logger.Error(ctx, "failed to remove supabase user after tenant creation failed", "error", err, "user_id", userID)
	}
}

// supabaseError maps a Supabase failure to an API error: an email it already has is a conflict, other refused input is bad input.
func supabaseError(err error, email string) error {
	var supaErr *supabase.ErrorResponse
	if !errors.As(err, &supaErr) {
		return ierr.WithError(err).
			WithHint("Failed to create the user in Supabase").
			WithReportableDetails(map[string]interface{}{"email": email}).
			Mark(ierr.ErrSystem)
	}

	details := map[string]interface{}{
		"email":          email,
		"supabase_code":  supaErr.Code,
		"supabase_error": supaErr.Message,
	}
	if supaErr.Code == http.StatusConflict ||
		strings.Contains(strings.ToLower(supaErr.Message), "already") {
		return ierr.WithError(supaErr).
			WithHint("A user with this email already exists in Supabase").
			WithReportableDetails(details).
			Mark(ierr.ErrAlreadyExists)
	}
	// Supabase also uses 422 for input it refuses, such as a password weaker than its policy allows.
	if supaErr.Code == http.StatusBadRequest || supaErr.Code == http.StatusUnprocessableEntity {
		return ierr.WithError(supaErr).
			WithHintf("Supabase rejected the user: %s", supaErr.Message).
			WithReportableDetails(details).
			Mark(ierr.ErrValidation)
	}
	return ierr.WithError(supaErr).
		WithHintf("Supabase rejected the user (code %d)", supaErr.Code).
		WithReportableDetails(details).
		Mark(ierr.ErrSystem)
}

// newOwner builds the tenant's first user as super_admin so it can invite the rest of the team.
func newOwner(ctx context.Context, userID, email string) *user.User {
	return &user.User{
		ID:        userID,
		Email:     email,
		Type:      types.UserTypeUser,
		Roles:     []string{types.RoleSuperAdmin.String()},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
}

// newEnvironments builds the Sandbox every tenant starts with, plus Production when asked.
func newEnvironments(ctx context.Context, withProduction bool) []*environment.Environment {
	envTypes := []types.EnvironmentType{types.EnvironmentDevelopment}
	if withProduction {
		envTypes = append(envTypes, types.EnvironmentProduction)
	}

	envs := make([]*environment.Environment, 0, len(envTypes))
	for _, envType := range envTypes {
		envs = append(envs, &environment.Environment{
			ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENVIRONMENT),
			Name:      envType.DisplayTitle(),
			Type:      envType,
			BaseModel: types.GetDefaultBaseModel(ctx),
		})
	}
	return envs
}
