package admin

import (
	"context"

	admindto "github.com/flexprice/flexprice/internal/api/dto/admin"
	"github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// UserService adds users to existing tenants for the admin portal, as the add-new-user script does.
// The user's login is created in Supabase, so it needs auth.provider supabase.
type UserService interface {
	AddUser(ctx context.Context, req admindto.AddUserRequest) (*admindto.UserResponse, error)
	RemoveUser(ctx context.Context, req admindto.RemoveUserRequest) (*admindto.RemoveUserResponse, error)
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
	if err := requireUnusedEmail(ctx, s.UserRepo, req.Email); err != nil {
		return nil, err
	}

	ctx = types.SetTenantID(ctx, req.TenantID)

	userID, err := createLogin(ctx, s.Config, req.Email, req.Password, req.TenantID)
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
		removeLogin(ctx, s.Config, s.Logger, userID)
		return nil, err
	}

	s.Logger.Info(ctx, "added user to tenant", "tenant_id", req.TenantID, "user_id", userID)
	return admindto.NewUserResponse(u), nil
}

// RemoveUser removes a person from their tenant: it archives their row, then deletes their
// Supabase login. API keys they created keep working.
func (s *userService) RemoveUser(ctx context.Context, req admindto.RemoveUserRequest) (*admindto.RemoveUserResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if s.Config == nil || s.Config.Auth.Provider != types.AuthProviderSupabase {
		return nil, ierr.NewError("supabase auth is not configured").
			WithHint("Removing a user's login needs auth.provider supabase").
			Mark(ierr.ErrInvalidOperation)
	}

	target, err := s.UserRepo.GetByEmail(ctx, req.Email)
	if err != nil {
		return nil, err
	}
	if target.TenantID != req.TenantID {
		return nil, ierr.NewError("user not found in this tenant").
			WithHint("This email belongs to a user in a different tenant").
			WithReportableDetails(map[string]interface{}{"email": req.Email, "tenant_id": req.TenantID}).
			Mark(ierr.ErrNotFound)
	}
	if target.Type != types.UserTypeUser {
		return nil, ierr.NewError("only human users can be removed").
			WithHint("Use the service account delete API to remove a service account").
			Mark(ierr.ErrValidation)
	}

	ctx = types.SetTenantID(ctx, req.TenantID)
	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		// Same lock as the dashboard's remove, so two removals cannot both pass the last-user check.
		if err := s.DB.LockWithWait(ctx, postgres.LockRequest{Key: "user_removal:" + req.TenantID}); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to acquire tenant lock for user removal").
				Mark(ierr.ErrInternal)
		}

		// Limit 1 still returns the full count, since the total is counted before the limit applies.
		_, humans, err := s.UserRepo.ListByFilter(ctx, &types.UserFilter{
			QueryFilter: &types.QueryFilter{
				Limit:  lo.ToPtr(1),
				Offset: lo.ToPtr(0),
				Status: lo.ToPtr(types.StatusPublished),
			},
			Type: lo.ToPtr(types.UserTypeUser),
		})
		if err != nil {
			return err
		}
		if humans <= 1 {
			return ierr.NewError("cannot remove the last user in the tenant").
				WithHint("At least one user must remain in the tenant").
				Mark(ierr.ErrValidation)
		}

		if err := s.UserRepo.Delete(ctx, target.ID); err != nil {
			return err
		}
		// Supabase last: a deleted login cannot be undone, but if this call fails the archive rolls back.
		return auth.NewSupabaseAuth(s.Config).RemoveUser(ctx, target.ID)
	})
	if err != nil {
		return nil, err
	}

	s.Logger.Info(ctx, "removed user from tenant", "tenant_id", req.TenantID, "user_id", target.ID)
	return admindto.NewRemoveUserResponse(target), nil
}
