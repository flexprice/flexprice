package admin

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	admindto "github.com/flexprice/flexprice/internal/api/dto/admin"
	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// TenantService onboards a new tenant with its first user, as the onboard-tenant script does.
// The user's login is created in Supabase, so it needs auth.provider supabase.
type TenantService interface {
	CreateTenant(ctx context.Context, req admindto.CreateTenantRequest) (*admindto.CreateTenantResponse, error)
}

type tenantService struct {
	service.ServiceParams
	tenants service.TenantService
}

func NewTenantService(params service.ServiceParams, tenants service.TenantService) TenantService {
	return &tenantService{ServiceParams: params, tenants: tenants}
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
	if err := requireUnusedEmail(ctx, s.UserRepo, req.Email); err != nil {
		return nil, err
	}

	tenantID := types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TENANT)
	ctx = types.SetTenantID(ctx, tenantID)

	userID, err := createLogin(ctx, s.Config, req.Email, req.Password, tenantID)
	if err != nil {
		return nil, err
	}
	ctx = types.SetUserID(ctx, userID)

	newTenant := (&dto.CreateTenantRequest{ID: tenantID, Name: req.TenantName}).ToTenant(ctx)
	owner := newOwner(ctx, userID, req.Email)
	envs := newEnvironments(ctx, req.CreateProduction)

	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		if err := s.TenantRepo.Create(ctx, newTenant); err != nil {
			return err
		}
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
		removeLogin(ctx, s.Config, s.Logger, userID)
		return nil, err
	}

	// Only after the commit: the billing customer's events are sent at once, and a rollback could not recall them.
	if err := s.tenants.CreateTenantAsBillingCustomer(ctx, newTenant); err != nil {
		s.Logger.Error(ctx, "failed to create billing customer for tenant", "error", err, "tenant_id", tenantID)
	}

	s.Logger.Info(ctx, "created tenant", "tenant_id", tenantID, "user_id", userID, "production", req.CreateProduction)
	return admindto.NewCreateTenantResponse(dto.NewTenantResponse(newTenant), owner, envs), nil
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
