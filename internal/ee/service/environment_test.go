package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/environment"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/suite"
)

type EnvironmentServiceSuite struct {
	suite.Suite
	ctx                context.Context
	environmentService *environmentService
	environmentRepo    *testutil.InMemoryEnvironmentStore
}

func TestEnvironmentService(t *testing.T) {
	suite.Run(t, new(EnvironmentServiceSuite))
}

func (s *EnvironmentServiceSuite) SetupTest() {
	s.ctx = context.Background()
	s.ctx = context.WithValue(s.ctx, types.CtxTenantID, "test-tenant-id")
	s.environmentRepo = testutil.NewInMemoryEnvironmentStore()

	// Create env access service that allows all access
	cfg := &config.Configuration{
		EnvAccess: config.EnvAccessConfig{
			UserEnvMapping: nil, // nil means all users are super admin
		},
	}
	envAccessService := NewEnvAccessService(cfg)

	// Create a real settings service for test (needed for generic GetSetting function)
	settingsRepo := testutil.NewInMemorySettingsStore()
	serviceParams := ServiceParams{
		SettingsRepo: settingsRepo,
	}
	realSettingsService := NewSettingsService(serviceParams)

	s.environmentService = &environmentService{
		repo:             s.environmentRepo,
		envAccessService: envAccessService,
		settingsService:  realSettingsService,
		ServiceParams:    serviceParams,
	}
}

func (s *EnvironmentServiceSuite) TestCreateEnvironment() {
	req := dto.CreateEnvironmentRequest{
		Name: "Production",
		Type: "development",
	}

	resp, err := s.environmentService.CreateEnvironment(s.ctx, req)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(req.Name, resp.Name)
}
func (s *EnvironmentServiceSuite) TestGetEnvironmentByID() {
	env := &environment.Environment{
		ID:   "env-1",
		Name: "Testing",
		Type: types.EnvironmentDevelopment,
	}

	_ = s.environmentRepo.Create(s.ctx, env)

	// Test retrieval
	resp, err := s.environmentService.GetEnvironment(s.ctx, "env-1")
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(env.Name, resp.Name)

	// Test non-existent environment
	resp, err = s.environmentService.GetEnvironment(s.ctx, "non-existent")
	s.Error(err)
	s.Nil(resp)
}

func (s *EnvironmentServiceSuite) TestListEnvironments() {
	_ = s.environmentRepo.Create(s.ctx, &environment.Environment{ID: "env-1", Name: "Production", Type: types.EnvironmentProduction})
	_ = s.environmentRepo.Create(s.ctx, &environment.Environment{ID: "env-2", Name: "Development", Type: types.EnvironmentDevelopment})
	_ = s.environmentRepo.Create(s.ctx, &environment.Environment{ID: "env-deleted", Name: "Deleted", Type: types.EnvironmentDevelopment, BaseModel: types.BaseModel{Status: types.StatusDeleted}})

	resp, err := s.environmentService.GetEnvironments(s.ctx, types.Filter{Offset: 0, Limit: 10})
	s.NoError(err)
	s.Len(resp.Environments, 2)

	resp, err = s.environmentService.GetEnvironments(s.ctx, types.Filter{Offset: 10, Limit: 10})
	s.NoError(err)
	s.Len(resp.Environments, 0)
}

func (s *EnvironmentServiceSuite) TestUpdateEnvironment() {
	env := &environment.Environment{
		ID:   "env-1",
		Name: "Development",
		Type: types.EnvironmentDevelopment,
	}
	_ = s.environmentRepo.Create(s.ctx, env)

	// Name updates and an unchanged type should succeed; type stays as it was.
	resp, err := s.environmentService.UpdateEnvironment(s.ctx, "env-1", dto.UpdateEnvironmentRequest{
		Name: "Updated Development",
		Type: string(types.EnvironmentDevelopment),
	})
	s.NoError(err)
	s.NotNil(resp)
	s.Equal("Updated Development", resp.Name)
	s.Equal(string(types.EnvironmentDevelopment), resp.Type)

	// Omitting type should also work and leave the type intact.
	resp, err = s.environmentService.UpdateEnvironment(s.ctx, "env-1", dto.UpdateEnvironmentRequest{
		Name: "Renamed Again",
	})
	s.NoError(err)
	s.Equal(string(types.EnvironmentDevelopment), resp.Type)

	// Attempting to change the type must be rejected.
	_, err = s.environmentService.UpdateEnvironment(s.ctx, "env-1", dto.UpdateEnvironmentRequest{
		Type: string(types.EnvironmentProduction),
	})
	s.Error(err)
}

// Regression: the update must be authorised against the environment named in
// the path. The caller's selected environment is not the target and is absent
// entirely when the request carries no X-Environment-ID, so a check tied to it
// let a user mutate an environment it had no access to.
func (s *EnvironmentServiceSuite) TestUpdateEnvironmentDeniesUnauthorisedTarget() {
	cfg := &config.Configuration{
		EnvAccess: config.EnvAccessConfig{
			UserEnvMapping: map[string]map[string][]string{
				"t_tenant1": {"usr_dev": {"env_dev"}},
			},
		},
	}
	s.environmentService.envAccessService = NewEnvAccessService(cfg)

	ctx := context.WithValue(context.Background(), types.CtxTenantID, "t_tenant1")
	ctx = context.WithValue(ctx, types.CtxUserID, "usr_dev")

	_ = s.environmentRepo.Create(ctx, &environment.Environment{ID: "env_dev", Name: "Development", Type: types.EnvironmentDevelopment})
	_ = s.environmentRepo.Create(ctx, &environment.Environment{ID: "env_prod", Name: "Production", Type: types.EnvironmentProduction})

	// The environment the user may reach is still writable.
	resp, err := s.environmentService.UpdateEnvironment(ctx, "env_dev", dto.UpdateEnvironmentRequest{Name: "Renamed Dev"})
	s.NoError(err)
	s.Equal("Renamed Dev", resp.Name)

	// The one it may not reach is refused, and the record is left untouched.
	_, err = s.environmentService.UpdateEnvironment(ctx, "env_prod", dto.UpdateEnvironmentRequest{Name: "cross-env-write"})
	s.Error(err)
	s.True(ierr.IsPermissionDenied(err))

	unchanged, err := s.environmentRepo.Get(ctx, "env_prod")
	s.NoError(err)
	s.Equal("Production", unchanged.Name)
}
