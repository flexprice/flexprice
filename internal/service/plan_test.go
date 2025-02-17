package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type PlanServiceSuite struct {
	testutil.BaseServiceTestSuite
	service PlanService
}

func TestPlanService(t *testing.T) {
	suite.Run(t, new(PlanServiceSuite))
}

func (s *PlanServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
}

func (s *PlanServiceSuite) setupService() {
	stores := s.GetStores()
	s.service = NewPlanService(
		s.GetDB(),
		stores.PlanRepo,
		stores.PriceRepo,
		stores.MeterRepo,
		stores.EntitlementRepo,
		stores.FeatureRepo,
		s.GetLogger(),
	)
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
						Currency:           "usd",
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

		resp, err := s.service.CreatePlan(s.GetContext(), req)
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
						Currency:           "usd",
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
						Currency:           "usd",
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

		resp, err := s.service.CreatePlan(s.GetContext(), req)
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

		resp, err := s.service.CreatePlan(s.GetContext(), req)
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
						Currency:     "usd",
						Type:         types.PRICE_TYPE_USAGE,
						BillingModel: types.BILLING_MODEL_TIERED,
						Description:  "Invalid Tiered Price",
					},
				},
			},
		}

		resp, err := s.service.CreatePlan(s.GetContext(), req)
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
						Currency:     "usd",
						Type:         types.PRICE_TYPE_USAGE,
						BillingModel: types.BILLING_MODEL_FLAT_FEE,
						Description:  "Negative Price",
					},
				},
			},
		}

		resp, err := s.service.CreatePlan(s.GetContext(), req)
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
						Currency:     "usd",
						Type:         types.PRICE_TYPE_USAGE,
						BillingModel: types.BILLING_MODEL_FLAT_FEE,
						Description:  "Missing MeterID",
					},
				},
			},
		}

		resp, err := s.service.CreatePlan(s.GetContext(), req)
		s.Error(err)
		s.Nil(resp)
	})
}

func (s *PlanServiceSuite) TestCreatePlanWithEntitlements() {
	// Setup test features with different types
	boolFeature := &feature.Feature{
		ID:          "feat-bool",
		Name:        "Boolean Feature",
		Description: "Test Boolean Feature",
		Type:        types.FeatureTypeBoolean,
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	err := s.GetStores().FeatureRepo.Create(s.GetContext(), boolFeature)
	s.NoError(err)

	meteredFeature := &feature.Feature{
		ID:          "feat-metered",
		Name:        "Metered Feature",
		Description: "Test Metered Feature",
		Type:        types.FeatureTypeMetered,
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	err = s.GetStores().FeatureRepo.Create(s.GetContext(), meteredFeature)
	s.NoError(err)

	staticFeature := &feature.Feature{
		ID:          "feat-static",
		Name:        "Static Feature",
		Description: "Test Static Feature",
		Type:        types.FeatureTypeStatic,
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	err = s.GetStores().FeatureRepo.Create(s.GetContext(), staticFeature)
	s.NoError(err)

	s.Run("Plan with Boolean Entitlement", func() {
		s.ClearStores() // Clear all stores before test

		// Recreate the boolean feature
		err = s.GetStores().FeatureRepo.Create(s.GetContext(), boolFeature)
		s.NoError(err)

		req := dto.CreatePlanRequest{
			Name:        "Plan with Boolean Feature",
			Description: "A plan with a boolean feature",
			Entitlements: []dto.CreatePlanEntitlementRequest{
				{
					CreateEntitlementRequest: &dto.CreateEntitlementRequest{
						FeatureID:   boolFeature.ID,
						FeatureType: types.FeatureTypeBoolean,
						IsEnabled:   true,
					},
				},
			},
		}

		resp, err := s.service.CreatePlan(s.GetContext(), req)
		s.NoError(err)
		s.NotNil(resp)

		filter := types.NewDefaultEntitlementFilter()
		filter.PlanIDs = []string{resp.Plan.ID}
		entitlements, err := s.GetStores().EntitlementRepo.List(s.GetContext(), filter)
		s.NoError(err)
		s.Equal(1, len(entitlements))
		s.Equal(boolFeature.ID, entitlements[0].FeatureID)
		s.True(entitlements[0].IsEnabled)
	})

	s.Run("Plan with Metered Entitlement", func() {
		s.ClearStores() // Clear all stores before test

		// Recreate the metered feature
		err = s.GetStores().FeatureRepo.Create(s.GetContext(), meteredFeature)
		s.NoError(err)

		req := dto.CreatePlanRequest{
			Name:        "Plan with Metered Feature",
			Description: "A plan with a metered feature",
			Entitlements: []dto.CreatePlanEntitlementRequest{
				{
					CreateEntitlementRequest: &dto.CreateEntitlementRequest{
						FeatureID:        meteredFeature.ID,
						FeatureType:      types.FeatureTypeMetered,
						UsageLimit:       lo.ToPtr(int64(1000)),
						UsageResetPeriod: types.BILLING_PERIOD_MONTHLY,
						IsSoftLimit:      true,
					},
				},
			},
		}

		resp, err := s.service.CreatePlan(s.GetContext(), req)
		s.NoError(err)
		s.NotNil(resp)

		filter := types.NewDefaultEntitlementFilter()
		filter.PlanIDs = []string{resp.Plan.ID}
		entitlements, err := s.GetStores().EntitlementRepo.List(s.GetContext(), filter)
		s.NoError(err)
		s.Equal(1, len(entitlements))
		s.Equal(meteredFeature.ID, entitlements[0].FeatureID)
		s.Equal(int64(1000), *entitlements[0].UsageLimit)
	})

	s.Run("Plan with Static Entitlement", func() {
		s.ClearStores() // Clear all stores before test

		// Recreate the static feature
		err = s.GetStores().FeatureRepo.Create(s.GetContext(), staticFeature)
		s.NoError(err)

		req := dto.CreatePlanRequest{
			Name:        "Plan with Static Feature",
			Description: "A plan with a static feature",
			Entitlements: []dto.CreatePlanEntitlementRequest{
				{
					CreateEntitlementRequest: &dto.CreateEntitlementRequest{
						FeatureID:   staticFeature.ID,
						FeatureType: types.FeatureTypeStatic,
						StaticValue: "premium",
					},
				},
			},
		}

		resp, err := s.service.CreatePlan(s.GetContext(), req)
		s.NoError(err)
		s.NotNil(resp)

		filter := types.NewDefaultEntitlementFilter()
		filter.PlanIDs = []string{resp.Plan.ID}
		entitlements, err := s.GetStores().EntitlementRepo.List(s.GetContext(), filter)
		s.NoError(err)
		s.Equal(1, len(entitlements))
		s.Equal(staticFeature.ID, entitlements[0].FeatureID)
		s.Equal("premium", entitlements[0].StaticValue)
	})

	// TODO: add feature validations - maybe by cerating a bulk create method
	// in the entitlement service that can own this for create and updates

	// s.Run("Plan with Invalid Feature ID", func() {
	// 	s.ClearStores() // Clear all stores before test

	// 	req := dto.CreatePlanRequest{
	// 		Name:        "Plan with Invalid Feature",
	// 		Description: "A plan with an invalid feature",
	// 		Entitlements: []dto.CreatePlanEntitlementRequest{
	// 			{
	// 				CreateEntitlementRequest: &dto.CreateEntitlementRequest{
	// 					FeatureID:   "nonexistent",
	// 					FeatureType: types.FeatureTypeBoolean,
	// 					IsEnabled:   true,
	// 				},
	// 			},
	// 		},
	// 	}

	// 	resp, err := s.service.CreatePlan(s.GetContext(), req)
	// 	s.Error(err)
	// 	s.Nil(resp)

	// 	filter := types.NewDefaultEntitlementFilter()
	// 	entitlements, err := s.GetStores().EntitlementRepo.List(s.GetContext(), filter)
	// 	s.NoError(err)
	// 	s.Equal(0, len(entitlements))
	// })
}

func (s *PlanServiceSuite) TestUpdatePlanEntitlements() {
	// Create a plan with an entitlement
	testFeature := &feature.Feature{
		ID:          "feat-1",
		Name:        "Test Feature",
		Description: "Test Feature Description",
		Type:        types.FeatureTypeBoolean,
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	err := s.GetStores().FeatureRepo.Create(s.GetContext(), testFeature)
	s.NoError(err)

	testPlan := &plan.Plan{
		ID:          "plan-1",
		Name:        "Test Plan",
		Description: "Test Plan Description",
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	err = s.GetStores().PlanRepo.Create(s.GetContext(), testPlan)
	s.NoError(err)

	testEntitlement := &entitlement.Entitlement{
		ID:          "ent-1",
		PlanID:      testPlan.ID,
		FeatureID:   testFeature.ID,
		FeatureType: types.FeatureTypeBoolean,
		IsEnabled:   false,
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	_, err = s.GetStores().EntitlementRepo.Create(s.GetContext(), testEntitlement)
	s.NoError(err)

	// Update the plan's entitlement
	req := dto.UpdatePlanRequest{
		Entitlements: []dto.UpdatePlanEntitlementRequest{
			{
				ID: testEntitlement.ID,
				CreatePlanEntitlementRequest: &dto.CreatePlanEntitlementRequest{
					CreateEntitlementRequest: &dto.CreateEntitlementRequest{
						IsEnabled: true,
					},
				},
			},
		},
	}

	resp, err := s.service.UpdatePlan(s.GetContext(), testPlan.ID, req)
	s.NoError(err)
	s.NotNil(resp)

	// Verify the entitlement was updated
	updatedEntitlement, err := s.GetStores().EntitlementRepo.Get(s.GetContext(), testEntitlement.ID)
	s.NoError(err)
	s.True(updatedEntitlement.IsEnabled)
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
	_ = s.GetStores().PlanRepo.Create(s.GetContext(), plan)

	resp, err := s.service.GetPlan(s.GetContext(), "plan-1")
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(plan.Name, resp.Plan.Name)

	// Non-existent plan
	resp, err = s.service.GetPlan(s.GetContext(), "nonexistent-id")
	s.Error(err)
	s.Nil(resp)
}

func (s *PlanServiceSuite) TestGetPlans() {
	// Prepopulate the repository with plans
	_ = s.GetStores().PlanRepo.Create(s.GetContext(), &plan.Plan{
		ID:        "plan-1",
		Name:      "Plan One",
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	})
	_ = s.GetStores().PlanRepo.Create(s.GetContext(), &plan.Plan{
		ID:        "plan-2",
		Name:      "Plan Two",
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	})

	planFilter := types.NewPlanFilter()
	planFilter.QueryFilter.Offset = lo.ToPtr(0)
	planFilter.QueryFilter.Limit = lo.ToPtr(10)
	resp, err := s.service.GetPlans(s.GetContext(), planFilter)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(2, resp.Pagination.Total)

	planFilter.QueryFilter.Offset = lo.ToPtr(10)
	planFilter.QueryFilter.Limit = lo.ToPtr(10)
	resp, err = s.service.GetPlans(s.GetContext(), planFilter)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(0, len(resp.Items))
}

func (s *PlanServiceSuite) TestUpdatePlan() {
	// Create a plan
	plan := &plan.Plan{
		ID:          "plan-1",
		Name:        "Old Plan",
		Description: "Old Description",
	}
	_ = s.GetStores().PlanRepo.Create(s.GetContext(), plan)

	req := dto.UpdatePlanRequest{
		Name:        lo.ToPtr("New Plan"),
		Description: lo.ToPtr("New Description"),
	}

	resp, err := s.service.UpdatePlan(s.GetContext(), "plan-1", req)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(*req.Name, resp.Plan.Name)
}

func (s *PlanServiceSuite) TestDeletePlan() {
	// Create a plan
	plan := &plan.Plan{ID: "plan-1", Name: "Plan to Delete"}
	_ = s.GetStores().PlanRepo.Create(s.GetContext(), plan)

	err := s.service.DeletePlan(s.GetContext(), "plan-1")
	s.NoError(err)

	// Ensure the plan no longer exists
	_, err = s.GetStores().PlanRepo.Get(s.GetContext(), "plan-1")
	s.Error(err)
}
