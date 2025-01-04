package service

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type PlanServiceSuite struct {
	suite.Suite
	ctx         context.Context
	planService *planService
	planRepo    *testutil.InMemoryPlanStore
}

func TestPlanService(t *testing.T) {
	suite.Run(t, new(PlanServiceSuite))
}

func (s *PlanServiceSuite) SetupTest() {
	s.ctx = testutil.SetupContext()
	s.planRepo = testutil.NewInMemoryPlanStore()

	s.planService = &planService{
		planRepo:  s.planRepo,
		priceRepo: testutil.NewInMemoryPriceStore(),
	}
}

func (s *PlanServiceSuite) TestCreatePlan() {
	// Test case: Valid plan with a single price
	s.Run("Valid Plan with Single Price", func() {
		req := dto.CreatePlanRequest{
			Name:        "Single Price Plan",
			Description: "A plan with one valid price",
			Prices: []dto.CreatePlanPriceRequest{
				{
					CreatePriceRequest: &dto.CreatePriceRequest{
						Amount:             "100",
						Currency:           "USD",
						Type:               types.PRICE_TYPE_USAGE,
						BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
						BillingPeriodCount: 1,
						BillingModel:       types.BILLING_MODEL_TIERED,
						TierMode:           types.BILLING_TIER_SLAB,
						BillingCadence:     types.BILLING_CADENCE_RECURRING,
						Description:        "Test Price",
						MeterID:            "meter-1",
						Tiers: ConvertToCreatePriceTier([]price.PriceTier{
							{
								UpTo:       lo.ToPtr(uint64(10)),
								UnitAmount: decimal.NewFromFloat(100.0),
								FlatAmount: lo.ToPtr(decimal.NewFromInt(20)),
							},
						}),
					},
				},
			},
		}

		resp, err := s.planService.CreatePlan(s.ctx, req)
		s.NoError(err)
		s.NotNil(resp)
		s.Equal(req.Name, resp.Plan.Name)
	})

	// Test case: Valid plan with multiple prices
	s.Run("Valid Plan with Multiple Prices", func() {
		req := dto.CreatePlanRequest{
			Name:        "Multi-Price Plan",
			Description: "A plan with multiple prices",
			Prices: []dto.CreatePlanPriceRequest{
				{
					CreatePriceRequest: &dto.CreatePriceRequest{
						Amount:             "100",
						Currency:           "USD",
						Type:               types.PRICE_TYPE_FIXED,
						BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
						BillingPeriodCount: 1,
						BillingModel:       types.BILLING_MODEL_FLAT_FEE,
						BillingCadence:     types.BILLING_CADENCE_RECURRING,
						Description:        "Flat Fee Price",
					},
				},
				{
					CreatePriceRequest: &dto.CreatePriceRequest{
						Amount:             "200",
						Currency:           "USD",
						Type:               types.PRICE_TYPE_USAGE,
						BillingPeriod:      types.BILLING_PERIOD_ANNUAL,
						BillingPeriodCount: 1,
						BillingModel:       types.BILLING_MODEL_PACKAGE,
						BillingCadence:     types.BILLING_CADENCE_RECURRING,
						TransformQuantity:  &price.TransformQuantity{DivideBy: 10},
						MeterID:            "meter-2",
						Description:        "Package Price",
					},
				},
			},
		}

		resp, err := s.planService.CreatePlan(s.ctx, req)
		s.NoError(err)
		s.NotNil(resp)
		s.Equal(2, len(req.Prices))
	})

	// Test case: Empty prices (if allowed)
	s.Run("Plan with No Prices", func() {
		req := dto.CreatePlanRequest{
			Name:        "Empty Plan",
			Description: "A plan with no prices",
			Prices:      []dto.CreatePlanPriceRequest{},
		}

		resp, err := s.planService.CreatePlan(s.ctx, req)
		s.NoError(err)
		s.NotNil(resp)
	})

	// Test case: Invalid price with missing tier_mode for TIERED model
	s.Run("Invalid Price Missing TierMode", func() {
		req := dto.CreatePlanRequest{
			Name:        "Invalid Tiered Plan",
			Description: "A plan with a price missing tier_mode",
			Prices: []dto.CreatePlanPriceRequest{
				{
					CreatePriceRequest: &dto.CreatePriceRequest{
						Amount:       "100",
						Currency:     "USD",
						Type:         types.PRICE_TYPE_USAGE,
						BillingModel: types.BILLING_MODEL_TIERED,
						Description:  "Invalid Tiered Price",
					},
				},
			},
		}

		resp, err := s.planService.CreatePlan(s.ctx, req)
		s.Error(err)
		s.Nil(resp)
	})

	// Test case: Invalid price with negative amount
	s.Run("Invalid Price Negative Amount", func() {
		req := dto.CreatePlanRequest{
			Name:        "Invalid Negative Amount Plan",
			Description: "A plan with a price having negative amount",
			Prices: []dto.CreatePlanPriceRequest{
				{
					CreatePriceRequest: &dto.CreatePriceRequest{
						Amount:       "-100",
						Currency:     "USD",
						Type:         types.PRICE_TYPE_USAGE,
						BillingModel: types.BILLING_MODEL_FLAT_FEE,
						Description:  "Negative Price",
					},
				},
			},
		}

		resp, err := s.planService.CreatePlan(s.ctx, req)
		s.Error(err)
		s.Nil(resp)
	})

	// Test case: Invalid price with missing meter_id for USAGE type
	s.Run("Invalid Price Missing MeterID", func() {
		req := dto.CreatePlanRequest{
			Name:        "Invalid Missing MeterID Plan",
			Description: "A plan with a price missing meter_id",
			Prices: []dto.CreatePlanPriceRequest{
				{
					CreatePriceRequest: &dto.CreatePriceRequest{
						Amount:       "100",
						Currency:     "USD",
						Type:         types.PRICE_TYPE_USAGE,
						BillingModel: types.BILLING_MODEL_FLAT_FEE,
						Description:  "Missing MeterID",
					},
				},
			},
		}

		resp, err := s.planService.CreatePlan(s.ctx, req)
		s.Error(err)
		s.Nil(resp)
	})
}

func ConvertToCreatePriceTier(tiers []price.PriceTier) []dto.CreatePriceTier {
	var converted []dto.CreatePriceTier
	for _, tier := range tiers {
		converted = append(converted, dto.CreatePriceTier{
			UpTo:       tier.UpTo,
			UnitAmount: tier.UnitAmount.String(), // Convert decimal.Decimal to string
			FlatAmount: func(flatAmount *decimal.Decimal) *string {
				if flatAmount != nil {
					str := flatAmount.String()
					return &str
				}
				return nil
			}(tier.FlatAmount), // Convert *decimal.Decimal to *string
		})
	}
	return converted
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
