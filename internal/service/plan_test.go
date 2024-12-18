package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/suite"
)

type PlanServiceSuite struct {
	suite.Suite
	ctx         context.Context
	planService *planService
	planRepo    *testutil.InMemoryPlanRepository
}

func TestPlanService(t *testing.T) {
	suite.Run(t, new(PlanServiceSuite))
}

func (s *PlanServiceSuite) SetupTest() {
	s.ctx = context.Background()
	s.planRepo = testutil.NewInMemoryPlanRepository()

	s.planService = &planService{
		planRepo:  s.planRepo,
		priceRepo: testutil.NewInMemoryPriceRepository(),
	}
}

func (s *PlanServiceSuite) TestCreatePlan() {
	req := dto.CreatePlanRequest{
		Name:        "Test Plan",
		Description: "Description",
		Prices: []dto.CreatePlanPriceRequest{
			{
				CreatePriceRequest: &dto.CreatePriceRequest{
					Amount:             100,
					Currency:           "USD",
					PlanID:             "plan-1",
					Type:               types.PRICE_TYPE_USAGE,
					BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
					BillingPeriodCount: 1,
					BillingModel:       types.BILLING_MODEL_TIERED,
					BillingCadence:     types.BILLING_CADENCE_RECURRING,
					Description:        "Test Price",
					MeterID:            "meter-1",
					Tiers: []price.PriceTier{
						{
							UpTo:       10,
							UnitAmount: 100,
							FlatAmount: 20,
						},
						{
							UpTo:       20,
							UnitAmount: 80,
							FlatAmount: 10,
						},
					},
				},
			},
		},
	}

	resp, err := s.planService.CreatePlan(s.ctx, req)

	// Assert no errors occurred and response is not nil
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(req.Name, resp.Plan.Name)
	s.Equal(req.Description, resp.Plan.Description)
}

func (s *PlanServiceSuite) TestGetPlan() {
	// Create a plan
	plan := &plan.Plan{
		ID:          "plan-1",
		Name:        "Test Plan",
		Description: "Description",
	}
	_ = s.planRepo.Create(s.ctx, plan)

	resp, err := s.planService.GetPlan(s.ctx, "plan-1")
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(plan.Name, resp.Plan.Name)

	// Non-existent plan
	resp, err = s.planService.GetPlan(s.ctx, "nonexistent-id")
	s.Error(err)
	s.Nil(resp)
}

func (s *PlanServiceSuite) TestGetPlans() {
	// Prepopulate the repository with plans
	_ = s.planRepo.Create(s.ctx, &plan.Plan{ID: "plan-1", Name: "Plan One"})
	_ = s.planRepo.Create(s.ctx, &plan.Plan{ID: "plan-2", Name: "Plan Two"})

	resp, err := s.planService.GetPlans(s.ctx, types.Filter{Offset: 0, Limit: 10})
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(2, resp.Total)

	resp, err = s.planService.GetPlans(s.ctx, types.Filter{Offset: 10, Limit: 10})
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(0, resp.Total)
}

func (s *PlanServiceSuite) TestUpdatePlan() {
	// Create a plan
	plan := &plan.Plan{
		ID:          "plan-1",
		Name:        "Old Plan",
		Description: "Old Description",
	}
	_ = s.planRepo.Create(s.ctx, plan)

	req := dto.UpdatePlanRequest{
		Name:        "New Plan",
		Description: "New Description",
	}

	resp, err := s.planService.UpdatePlan(s.ctx, "plan-1", req)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(req.Name, resp.Plan.Name)
}

func (s *PlanServiceSuite) TestDeletePlan() {
	// Create a plan
	plan := &plan.Plan{ID: "plan-1", Name: "Plan to Delete"}
	_ = s.planRepo.Create(s.ctx, plan)

	err := s.planService.DeletePlan(s.ctx, "plan-1")
	s.NoError(err)

	// Ensure the plan no longer exists
	_, err = s.planRepo.Get(s.ctx, "plan-1")
	s.Error(err)
}
