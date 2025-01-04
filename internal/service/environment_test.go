package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
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
	s.environmentRepo = testutil.NewInMemoryEnvironmentStore()
	s.environmentService = &environmentService{repo: s.environmentRepo}
}

func (s *EnvironmentServiceSuite) TestCreateEnvironment() {
	req := dto.CreateEnvironmentRequest{
		Name: "Production",
		Type: "production",
		Slug: "prod-environment",
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
		Type: types.EnvironmentTesting,
		Slug: "testing-environment",
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
	_ = s.environmentRepo.Create(s.ctx, &environment.Environment{ID: "env-1", Name: "Production", Type: types.EnvironmentProduction, Slug: "prod-environment"})
	_ = s.environmentRepo.Create(s.ctx, &environment.Environment{ID: "env-2", Name: "Testing", Type: types.EnvironmentTesting, Slug: "testing-environment"})

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
		Slug: "dev-environment",
	}
	_ = s.environmentRepo.Create(s.ctx, env)

	updateReq := dto.UpdateEnvironmentRequest{
		Name: "Updated Development",
		Slug: "updated-dev-environment",
		Type: "updated-type",
	}

	resp, err := s.environmentService.UpdateEnvironment(s.ctx, "env-1", updateReq)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(updateReq.Name, resp.Name)
	s.Equal(updateReq.Slug, resp.Slug)
	s.Equal(updateReq.Type, resp.Type)
}
