package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/environment"
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
