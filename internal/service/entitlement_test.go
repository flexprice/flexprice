package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/suite"
)

type EntitlementServiceSuite struct {
	testutil.BaseServiceTestSuite
	service EntitlementService
}

func TestEntitlementService(t *testing.T) {
	suite.Run(t, new(EntitlementServiceSuite))
}

func (s *EntitlementServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
}

func (s *EntitlementServiceSuite) setupService() {
	stores := s.GetStores()
	s.service = NewEntitlementService(ServiceParams{
		Logger:           s.GetLogger(),
		EntitlementRepo:  stores.EntitlementRepo,
		PlanRepo:         stores.PlanRepo,
		FeatureRepo:      stores.FeatureRepo,
		MeterRepo:        testutil.NewInMemoryMeterStore(),
		WebhookPublisher: s.GetWebhookPublisher(),
	})
}

func (s *EntitlementServiceSuite) TestCreateEntitlement() {
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

	// Create a test plan
	testPlan := &plan.Plan{
		ID:          "plan-1",
		Name:        "Test Plan",
		Description: "Test Plan Description",
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	err = s.GetStores().PlanRepo.Create(s.GetContext(), testPlan)
	s.NoError(err)

	// Test case: Valid boolean entitlement
	s.Run("Valid Boolean Entitlement", func() {
		req := dto.CreateEntitlementRequest{
			PlanID:      testPlan.ID,
			FeatureID:   boolFeature.ID,
			FeatureType: types.FeatureTypeBoolean,
			IsEnabled:   true,
		}

		resp, err := s.service.CreateEntitlement(s.GetContext(), req)
		s.NoError(err)
		s.NotNil(resp)
		s.Equal(req.PlanID, resp.Entitlement.PlanID)
		s.Equal(req.FeatureID, resp.Entitlement.FeatureID)
		s.Equal(req.IsEnabled, resp.Entitlement.IsEnabled)
	})

	// Test case: Valid metered entitlement
	s.Run("Valid Metered Entitlement", func() {
		req := dto.CreateEntitlementRequest{
			PlanID:           testPlan.ID,
			FeatureID:        meteredFeature.ID,
			FeatureType:      types.FeatureTypeMetered,
			UsageLimit:       lo.ToPtr(int64(1000)),
			UsageResetPeriod: types.BILLING_PERIOD_MONTHLY,
			IsSoftLimit:      true,
		}

		resp, err := s.service.CreateEntitlement(s.GetContext(), req)
		s.NoError(err)
		s.NotNil(resp)
		s.Equal(req.PlanID, resp.Entitlement.PlanID)
		s.Equal(req.FeatureID, resp.Entitlement.FeatureID)
		s.Equal(*req.UsageLimit, *resp.Entitlement.UsageLimit)
		s.Equal(req.UsageResetPeriod, resp.Entitlement.UsageResetPeriod)
	})

	// Test case: Valid static entitlement
	s.Run("Valid Static Entitlement", func() {
		req := dto.CreateEntitlementRequest{
			PlanID:      testPlan.ID,
			FeatureID:   staticFeature.ID,
			FeatureType: types.FeatureTypeStatic,
			StaticValue: "premium",
		}

		resp, err := s.service.CreateEntitlement(s.GetContext(), req)
		s.NoError(err)
		s.NotNil(resp)
		s.Equal(req.PlanID, resp.Entitlement.PlanID)
		s.Equal(req.FeatureID, resp.Entitlement.FeatureID)
		s.Equal(req.StaticValue, resp.Entitlement.StaticValue)
	})

	// Test case: Invalid feature ID
	s.Run("Invalid Feature ID", func() {
		req := dto.CreateEntitlementRequest{
			PlanID:      testPlan.ID,
			FeatureID:   "nonexistent",
			FeatureType: types.FeatureTypeBoolean,
			IsEnabled:   true,
		}

		resp, err := s.service.CreateEntitlement(s.GetContext(), req)
		s.Error(err)
		s.Nil(resp)
	})

	// Test case: Missing static value for static feature
	s.Run("Missing Static Value", func() {
		req := dto.CreateEntitlementRequest{
			PlanID:      testPlan.ID,
			FeatureID:   staticFeature.ID,
			FeatureType: types.FeatureTypeStatic,
		}

		resp, err := s.service.CreateEntitlement(s.GetContext(), req)
		s.Error(err)
		s.Nil(resp)
	})
}

func (s *EntitlementServiceSuite) TestGetEntitlement() {
	// Create test feature
	testFeature := &feature.Feature{
		ID:          "feat-1",
		Name:        "Test Feature",
		Description: "Test Feature Description",
		Type:        types.FeatureTypeBoolean,
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	err := s.GetStores().FeatureRepo.Create(s.GetContext(), testFeature)
	s.NoError(err)

	// Create test plan
	testPlan := &plan.Plan{
		ID:          "plan-1",
		Name:        "Test Plan",
		Description: "Test Plan Description",
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	err = s.GetStores().PlanRepo.Create(s.GetContext(), testPlan)
	s.NoError(err)

	// Create an entitlement
	ent := &entitlement.Entitlement{
		ID:          "ent-1",
		PlanID:      testPlan.ID,
		FeatureID:   testFeature.ID,
		FeatureType: types.FeatureTypeBoolean,
		IsEnabled:   true,
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	_, err = s.GetStores().EntitlementRepo.Create(s.GetContext(), ent)
	s.NoError(err)

	resp, err := s.service.GetEntitlement(s.GetContext(), "ent-1")
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(ent.PlanID, resp.Entitlement.PlanID)

	// Non-existent entitlement
	resp, err = s.service.GetEntitlement(s.GetContext(), "nonexistent")
	s.Error(err)
	s.Nil(resp)
}

func (s *EntitlementServiceSuite) TestListEntitlements() {
	// Create test features
	boolFeature := &feature.Feature{
		ID:          "feat-1",
		Name:        "Boolean Feature",
		Description: "Test Boolean Feature",
		Type:        types.FeatureTypeBoolean,
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	err := s.GetStores().FeatureRepo.Create(s.GetContext(), boolFeature)
	s.NoError(err)

	meteredFeature := &feature.Feature{
		ID:          "feat-2",
		Name:        "Metered Feature",
		Description: "Test Metered Feature",
		Type:        types.FeatureTypeMetered,
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	err = s.GetStores().FeatureRepo.Create(s.GetContext(), meteredFeature)
	s.NoError(err)

	// Create test plan
	testPlan := &plan.Plan{
		ID:          "plan-1",
		Name:        "Test Plan",
		Description: "Test Plan Description",
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	err = s.GetStores().PlanRepo.Create(s.GetContext(), testPlan)
	s.NoError(err)

	// Create multiple entitlements
	ent1 := &entitlement.Entitlement{
		ID:          "ent-1",
		PlanID:      testPlan.ID,
		FeatureID:   boolFeature.ID,
		FeatureType: types.FeatureTypeBoolean,
		IsEnabled:   true,
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	_, err = s.GetStores().EntitlementRepo.Create(s.GetContext(), ent1)
	s.NoError(err)

	ent2 := &entitlement.Entitlement{
		ID:          "ent-2",
		PlanID:      testPlan.ID,
		FeatureID:   meteredFeature.ID,
		FeatureType: types.FeatureTypeMetered,
		UsageLimit:  lo.ToPtr(int64(1000)),
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	_, err = s.GetStores().EntitlementRepo.Create(s.GetContext(), ent2)
	s.NoError(err)

	// Test listing with pagination
	filter := types.NewDefaultEntitlementFilter()
	filter.QueryFilter.Limit = lo.ToPtr(10)
	resp, err := s.service.ListEntitlements(s.GetContext(), filter)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(2, resp.Pagination.Total)

	// Test filtering by plan ID
	filter.PlanIDs = []string{testPlan.ID}
	resp, err = s.service.ListEntitlements(s.GetContext(), filter)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(2, len(resp.Items))

	// Test filtering by feature type
	filter = types.NewDefaultEntitlementFilter()
	featureType := types.FeatureTypeBoolean
	filter.FeatureType = &featureType
	resp, err = s.service.ListEntitlements(s.GetContext(), filter)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(1, len(resp.Items))
}

func (s *EntitlementServiceSuite) TestUpdateEntitlement() {
	// Create test feature
	testFeature := &feature.Feature{
		ID:          "feat-1",
		Name:        "Test Feature",
		Description: "Test Feature Description",
		Type:        types.FeatureTypeBoolean,
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	err := s.GetStores().FeatureRepo.Create(s.GetContext(), testFeature)
	s.NoError(err)

	// Create test plan
	testPlan := &plan.Plan{
		ID:          "plan-1",
		Name:        "Test Plan",
		Description: "Test Plan Description",
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	err = s.GetStores().PlanRepo.Create(s.GetContext(), testPlan)
	s.NoError(err)

	// Create an entitlement
	ent := &entitlement.Entitlement{
		ID:          "ent-1",
		PlanID:      testPlan.ID,
		FeatureID:   testFeature.ID,
		FeatureType: types.FeatureTypeBoolean,
		IsEnabled:   false,
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	_, err = s.GetStores().EntitlementRepo.Create(s.GetContext(), ent)
	s.NoError(err)

	// Update the entitlement
	isEnabled := true
	req := dto.UpdateEntitlementRequest{
		IsEnabled: &isEnabled,
	}

	resp, err := s.service.UpdateEntitlement(s.GetContext(), "ent-1", req)
	s.NoError(err)
	s.NotNil(resp)
	s.True(resp.Entitlement.IsEnabled)

	// Test updating non-existent entitlement
	resp, err = s.service.UpdateEntitlement(s.GetContext(), "nonexistent", req)
	s.Error(err)
	s.Nil(resp)
}

func (s *EntitlementServiceSuite) TestDeleteEntitlement() {
	// Create test feature
	testFeature := &feature.Feature{
		ID:          "feat-1",
		Name:        "Test Feature",
		Description: "Test Feature Description",
		Type:        types.FeatureTypeBoolean,
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	err := s.GetStores().FeatureRepo.Create(s.GetContext(), testFeature)
	s.NoError(err)

	// Create test plan
	testPlan := &plan.Plan{
		ID:          "plan-1",
		Name:        "Test Plan",
		Description: "Test Plan Description",
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	err = s.GetStores().PlanRepo.Create(s.GetContext(), testPlan)
	s.NoError(err)

	// Create an entitlement
	ent := &entitlement.Entitlement{
		ID:          "ent-1",
		PlanID:      testPlan.ID,
		FeatureID:   testFeature.ID,
		FeatureType: types.FeatureTypeBoolean,
		IsEnabled:   true,
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	_, err = s.GetStores().EntitlementRepo.Create(s.GetContext(), ent)
	s.NoError(err)

	// Delete the entitlement
	err = s.service.DeleteEntitlement(s.GetContext(), "ent-1")
	s.NoError(err)

	// Verify the entitlement is deleted
	_, err = s.GetStores().EntitlementRepo.Get(s.GetContext(), "ent-1")
	s.Error(err)
}
