package admin

import (
	"context"

	admindto "github.com/flexprice/flexprice/internal/api/dto/admin"
	"github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/nedpals/supabase-go"
)

// UserService adds users to existing tenants for the admin portal, as the add-new-user script does.
// The user's login is created in Supabase, so it needs auth.provider supabase.
type UserService interface {
	AddUser(ctx context.Context, req admindto.AddUserRequest) (*admindto.UserResponse, error)
}

type userService struct {
	service.ServiceParams
}

func NewUserService(params service.ServiceParams) UserService {
	return &userService{ServiceParams: params}
}

func (s *userService) AddUser(ctx context.Context, req admindto.AddUserRequest) (*admindto.UserResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if s.Config == nil || s.Config.Auth.Provider != types.AuthProviderSupabase {
		return nil, ierr.NewError("supabase auth is not configured").
			WithHint("Creating a user with a password needs auth.provider supabase").
			Mark(ierr.ErrInvalidOperation)
	}
	if _, err := s.TenantRepo.GetByID(ctx, req.TenantID); err != nil {
		return nil, err
	}
	if err := s.requireUnusedEmail(ctx, req.Email); err != nil {
		return nil, err
	}

	ctx = types.SetTenantID(ctx, req.TenantID)

	userID, err := s.createLogin(ctx, req.Email, req.Password, req.TenantID)
	if err != nil {
		return nil, err
	}
	ctx = types.SetUserID(ctx, userID)

	role := types.RoleAllReader
	if req.Role != "" {
		role = req.Role
	}
	u := &user.User{
		ID:        userID,
		Email:     req.Email,
		Type:      types.UserTypeUser,
		Roles:     []string{role.String()},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	if err := s.UserRepo.Create(ctx, u); err != nil {
		s.removeLogin(ctx, userID)
		return nil, err
	}

	s.Logger.Info(ctx, "added user to tenant", "tenant_id", req.TenantID, "user_id", userID)
	return admindto.NewUserResponse(u), nil
}

// requireUnusedEmail refuses an email that already belongs to a user in any tenant.
func (s *userService) requireUnusedEmail(ctx context.Context, email string) error {
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
func (s *userService) createLogin(ctx context.Context, email, password, tenantID string) (string, error) {
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

// removeLogin deletes a Supabase user whose row was not saved, so the email can be retried.
func (s *userService) removeLogin(ctx context.Context, userID string) {
	if err := auth.NewSupabaseAuth(s.Config).RemoveUser(context.WithoutCancel(ctx), userID); err != nil {
		s.Logger.Error(ctx, "failed to remove supabase user after adding the user failed", "error", err, "user_id", userID)
	}
}
