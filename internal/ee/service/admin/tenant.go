package admin

import (
	"context"
	"slices"
	"strings"

	"github.com/flexprice/flexprice/internal/api/dto"
	admindto "github.com/flexprice/flexprice/internal/api/dto/admin"
	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// TenantService onboards and lists tenants for the admin portal. Onboarding creates the first
// user's login in Supabase, as the onboard-tenant script does, so it needs auth.provider supabase.
type TenantService interface {
	CreateTenant(ctx context.Context, req admindto.CreateTenantRequest) (*admindto.CreateTenantResponse, error)
	// ListTenants returns one page of tenants, newest first, that match the search and internal status.
	ListTenants(ctx context.Context, req admindto.ListTenantsRequest) (*admindto.ListTenantsResponse, error)
	// GetTenant returns one tenant with its billing details and metadata.
	GetTenant(ctx context.Context, tenantID string) (*admindto.TenantDetailResponse, error)
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

	// Only after the commit: provisioning calls Flexprice Cloud, and a rollback could not recall it.
	s.SelfBilling.OnTenantCreated(ctx, tenantID, newTenant.Name, req.Email)

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

func (s *tenantService) ListTenants(ctx context.Context, req admindto.ListTenantsRequest) (*admindto.ListTenantsResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	tenants, err := s.TenantRepo.List(ctx)
	if err != nil {
		return nil, err
	}
	emailTenantID, err := s.tenantIDByEmail(ctx, req.Search)
	if err != nil {
		return nil, err
	}

	name := strings.ToLower(req.Search)
	matches := lo.Filter(tenants, func(t *tenant.Tenant, _ int) bool {
		if req.InternalStatus != "" && t.InternalStatus != req.InternalStatus {
			return false
		}
		if req.Search == "" {
			return true
		}
		return strings.Contains(strings.ToLower(t.Name), name) ||
			t.ID == req.Search ||
			(emailTenantID != "" && t.ID == emailTenantID)
	})
	slices.SortStableFunc(matches, func(a, b *tenant.Tenant) int {
		return b.CreatedAt.Compare(a.CreatedAt)
	})

	start := min(req.Offset, len(matches))
	end := min(start+req.Limit, len(matches))
	return admindto.NewListTenantsResponse(matches[start:end], len(matches), req.Limit, req.Offset), nil
}

// tenantIDByEmail returns the tenant of the published user whose email is search, or "" if there is none.
func (s *tenantService) tenantIDByEmail(ctx context.Context, search string) (string, error) {
	if !strings.Contains(search, "@") {
		return "", nil
	}
	// No tenant is set on ctx, so the lookup covers every tenant.
	u, err := s.UserRepo.GetByEmail(ctx, strings.ToLower(search))
	if ierr.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return u.TenantID, nil
}

func (s *tenantService) GetTenant(ctx context.Context, tenantID string) (*admindto.TenantDetailResponse, error) {
	t, err := s.TenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return admindto.NewTenantDetailResponse(t), nil
}
