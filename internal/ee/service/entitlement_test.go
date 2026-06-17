package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
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
		Config:           s.GetConfig(),
		DB:               s.GetDB(),
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

	// Create a regular meter for the metered feature
	regularMeter := &meter.Meter{
		ID:        "meter-regular",
		Name:      "Regular Meter",
		EventName: "api_calls",
		Aggregation: meter.Aggregation{
			Type: types.AggregationSum,
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	meterStore := s.service.(*entitlementService).MeterRepo.(*testutil.InMemoryMeterStore)
	err = meterStore.CreateMeter(s.GetContext(), regularMeter)
	s.NoError(err)

	meteredFeature := &feature.Feature{
		ID:          "feat-metered",
		Name:        "Metered Feature",
		Description: "Test Metered Feature",
		Type:        types.FeatureTypeMetered,
		MeterID:     regularMeter.ID,
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
		s.Equal(types.ENTITLEMENT_ENTITY_TYPE_PLAN, resp.Entitlement.EntityType)
		s.Equal(testPlan.ID, resp.Entitlement.EntityID)
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
			UsageResetPeriod: types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY,
			IsSoftLimit:      true,
		}

		resp, err := s.service.CreateEntitlement(s.GetContext(), req)
		s.NoError(err)
		s.NotNil(resp)
		s.Equal(types.ENTITLEMENT_ENTITY_TYPE_PLAN, resp.Entitlement.EntityType)
		s.Equal(testPlan.ID, resp.Entitlement.EntityID)
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
		s.Equal(types.ENTITLEMENT_ENTITY_TYPE_PLAN, resp.Entitlement.EntityType)
		s.Equal(testPlan.ID, resp.Entitlement.EntityID)
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
		EntityType:  types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		EntityID:    testPlan.ID,
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
	s.Equal(ent.EntityID, resp.Entitlement.EntityID)

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

	// Create a regular meter for the metered feature
	regularMeterList := &meter.Meter{
		ID:        "meter-regular-list",
		Name:      "Regular Meter List",
		EventName: "api_calls_list",
		Aggregation: meter.Aggregation{
			Type: types.AggregationSum,
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	meterStore := s.service.(*entitlementService).MeterRepo.(*testutil.InMemoryMeterStore)
	err = meterStore.CreateMeter(s.GetContext(), regularMeterList)
	s.NoError(err)

	meteredFeature := &feature.Feature{
		ID:          "feat-2",
		Name:        "Metered Feature",
		Description: "Test Metered Feature",
		Type:        types.FeatureTypeMetered,
		MeterID:     regularMeterList.ID,
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
		EntityType:  types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		EntityID:    testPlan.ID,
		FeatureID:   boolFeature.ID,
		FeatureType: types.FeatureTypeBoolean,
		IsEnabled:   true,
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	_, err = s.GetStores().EntitlementRepo.Create(s.GetContext(), ent1)
	s.NoError(err)

	ent2 := &entitlement.Entitlement{
		ID:          "ent-2",
		EntityType:  types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		EntityID:    testPlan.ID,
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

	// Test filtering by entity ID
	filter.EntityIDs = []string{testPlan.ID}
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
		EntityType:  types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		EntityID:    testPlan.ID,
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
		EntityType:  types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		EntityID:    testPlan.ID,
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

func (s *EntitlementServiceSuite) TestCreateEntitlementWithBucketedMaxMeter() {
	// Create a bucketed max meter
	bucketedMaxMeter := &meter.Meter{
		ID:        "meter-bucketed-max",
		Name:      "Bucketed Max Meter",
		EventName: "api_calls_bucketed",
		Aggregation: meter.Aggregation{
			Type:       types.AggregationMax,
			BucketSize: "hour", // This makes it a bucketed max meter
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	meterStore := s.service.(*entitlementService).MeterRepo.(*testutil.InMemoryMeterStore)
	err := meterStore.CreateMeter(s.GetContext(), bucketedMaxMeter)
	s.NoError(err)

	// Create a regular max meter (without bucket size)
	regularMaxMeter := &meter.Meter{
		ID:        "meter-regular-max",
		Name:      "Regular Max Meter",
		EventName: "api_calls_regular",
		Aggregation: meter.Aggregation{
			Type: types.AggregationMax,
			// No BucketSize - this is a regular max meter
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	err = meterStore.CreateMeter(s.GetContext(), regularMaxMeter)
	s.NoError(err)

	// Create metered features
	bucketedMaxFeature := &feature.Feature{
		ID:          "feat-bucketed-max",
		Name:        "Bucketed Max Feature",
		Description: "Feature with bucketed max meter",
		Type:        types.FeatureTypeMetered,
		MeterID:     bucketedMaxMeter.ID,
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	err = s.GetStores().FeatureRepo.Create(s.GetContext(), bucketedMaxFeature)
	s.NoError(err)

	regularMaxFeature := &feature.Feature{
		ID:          "feat-regular-max",
		Name:        "Regular Max Feature",
		Description: "Feature with regular max meter",
		Type:        types.FeatureTypeMetered,
		MeterID:     regularMaxMeter.ID,
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	err = s.GetStores().FeatureRepo.Create(s.GetContext(), regularMaxFeature)
	s.NoError(err)

	// Create test plan
	testPlan := &plan.Plan{
		ID:          "plan-bucketed-test",
		Name:        "Test Plan for Bucketed Max",
		Description: "Test Plan Description",
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	err = s.GetStores().PlanRepo.Create(s.GetContext(), testPlan)
	s.NoError(err)

	// Test case: Should reject entitlement for bucketed max meter
	s.Run("Reject Entitlement for Bucketed Max Meter", func() {
		req := dto.CreateEntitlementRequest{
			PlanID:           testPlan.ID,
			FeatureID:        bucketedMaxFeature.ID,
			FeatureType:      types.FeatureTypeMetered,
			UsageLimit:       lo.ToPtr(int64(1000)),
			UsageResetPeriod: types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY,
			IsSoftLimit:      true,
		}

		resp, err := s.service.CreateEntitlement(s.GetContext(), req)
		s.Error(err)
		s.Nil(resp)
		s.Contains(err.Error(), "entitlements not supported for bucketed max meters")
	})

	// Test case: Should allow entitlement for regular max meter
	s.Run("Allow Entitlement for Regular Max Meter", func() {
		req := dto.CreateEntitlementRequest{
			PlanID:           testPlan.ID,
			FeatureID:        regularMaxFeature.ID,
			FeatureType:      types.FeatureTypeMetered,
			UsageLimit:       lo.ToPtr(int64(1000)),
			UsageResetPeriod: types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY,
			IsSoftLimit:      true,
		}

		resp, err := s.service.CreateEntitlement(s.GetContext(), req)
		s.NoError(err)
		s.NotNil(resp)
		s.Equal(testPlan.ID, resp.Entitlement.EntityID)
		s.Equal(regularMaxFeature.ID, resp.Entitlement.FeatureID)
		s.Equal(int64(1000), *resp.Entitlement.UsageLimit)
	})
}

func (s *EntitlementServiceSuite) TestCreateBulkEntitlementWithBucketedMaxMeter() {
	// Create a bucketed max meter
	bucketedMaxMeter := &meter.Meter{
		ID:        "meter-bucketed-max-bulk",
		Name:      "Bucketed Max Meter Bulk",
		EventName: "api_calls_bucketed_bulk",
		Aggregation: meter.Aggregation{
			Type:       types.AggregationMax,
			BucketSize: "minute", // This makes it a bucketed max meter
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	meterStore := s.service.(*entitlementService).MeterRepo.(*testutil.InMemoryMeterStore)
	err := meterStore.CreateMeter(s.GetContext(), bucketedMaxMeter)
	s.NoError(err)

	// Create metered feature with bucketed max meter
	bucketedMaxFeature := &feature.Feature{
		ID:          "feat-bucketed-max-bulk",
		Name:        "Bucketed Max Feature Bulk",
		Description: "Feature with bucketed max meter for bulk test",
		Type:        types.FeatureTypeMetered,
		MeterID:     bucketedMaxMeter.ID,
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	err = s.GetStores().FeatureRepo.Create(s.GetContext(), bucketedMaxFeature)
	s.NoError(err)

	// Create boolean feature for comparison
	boolFeature := &feature.Feature{
		ID:          "feat-bool-bulk-bucketed",
		Name:        "Boolean Feature Bulk Bucketed",
		Description: "Test Boolean Feature for Bulk with Bucketed",
		Type:        types.FeatureTypeBoolean,
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	err = s.GetStores().FeatureRepo.Create(s.GetContext(), boolFeature)
	s.NoError(err)

	// Create test plan
	testPlan := &plan.Plan{
		ID:          "plan-bulk-bucketed",
		Name:        "Test Plan Bulk Bucketed",
		Description: "Test Plan Description for Bucketed Bulk",
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	err = s.GetStores().PlanRepo.Create(s.GetContext(), testPlan)
	s.NoError(err)

	// Test case: Should reject bulk entitlement when one feature has bucketed max meter
	s.Run("Reject Bulk Entitlement with Bucketed Max Meter", func() {
		req := dto.CreateBulkEntitlementRequest{
			Items: []dto.CreateEntitlementRequest{
				{
					PlanID:      testPlan.ID,
					FeatureID:   boolFeature.ID,
					FeatureType: types.FeatureTypeBoolean,
					IsEnabled:   true,
				},
				{
					PlanID:           testPlan.ID,
					FeatureID:        bucketedMaxFeature.ID,
					FeatureType:      types.FeatureTypeMetered,
					UsageLimit:       lo.ToPtr(int64(1000)),
					UsageResetPeriod: types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY,
					IsSoftLimit:      true,
				},
			},
		}

		resp, err := s.service.CreateBulkEntitlement(s.GetContext(), req)
		s.Error(err)
		s.Nil(resp)
		s.Contains(err.Error(), "entitlements not supported for bucketed max meters")
	})
}

func (s *EntitlementServiceSuite) TestCreateBulkEntitlement() {
	// Setup test features with different types
	boolFeature := &feature.Feature{
		ID:          "feat-bool-bulk",
		Name:        "Boolean Feature Bulk",
		Description: "Test Boolean Feature for Bulk",
		Type:        types.FeatureTypeBoolean,
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	err := s.GetStores().FeatureRepo.Create(s.GetContext(), boolFeature)
	s.NoError(err)

	// Create a regular meter for the metered feature
	regularMeterBulk := &meter.Meter{
		ID:        "meter-regular-bulk",
		Name:      "Regular Meter Bulk",
		EventName: "api_calls_bulk",
		Aggregation: meter.Aggregation{
			Type: types.AggregationSum,
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	meterStore := s.service.(*entitlementService).MeterRepo.(*testutil.InMemoryMeterStore)
	err = meterStore.CreateMeter(s.GetContext(), regularMeterBulk)
	s.NoError(err)

	meteredFeature := &feature.Feature{
		ID:          "feat-metered-bulk",
		Name:        "Metered Feature Bulk",
		Description: "Test Metered Feature for Bulk",
		Type:        types.FeatureTypeMetered,
		MeterID:     regularMeterBulk.ID,
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	err = s.GetStores().FeatureRepo.Create(s.GetContext(), meteredFeature)
	s.NoError(err)

	staticFeature := &feature.Feature{
		ID:          "feat-static-bulk",
		Name:        "Static Feature Bulk",
		Description: "Test Static Feature for Bulk",
		Type:        types.FeatureTypeStatic,
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	err = s.GetStores().FeatureRepo.Create(s.GetContext(), staticFeature)
	s.NoError(err)

	// Create test plans
	testPlan1 := &plan.Plan{
		ID:          "plan-bulk-1",
		Name:        "Test Plan Bulk 1",
		Description: "Test Plan Description Bulk 1",
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	err = s.GetStores().PlanRepo.Create(s.GetContext(), testPlan1)
	s.NoError(err)

	testPlan2 := &plan.Plan{
		ID:          "plan-bulk-2",
		Name:        "Test Plan Bulk 2",
		Description: "Test Plan Description Bulk 2",
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	err = s.GetStores().PlanRepo.Create(s.GetContext(), testPlan2)
	s.NoError(err)

	s.Run("Valid Bulk Entitlement Creation", func() {
		req := dto.CreateBulkEntitlementRequest{
			Items: []dto.CreateEntitlementRequest{
				{
					PlanID:      testPlan1.ID,
					FeatureID:   boolFeature.ID,
					FeatureType: types.FeatureTypeBoolean,
					IsEnabled:   true,
				},
				{
					PlanID:           testPlan1.ID,
					FeatureID:        meteredFeature.ID,
					FeatureType:      types.FeatureTypeMetered,
					UsageLimit:       lo.ToPtr(int64(1000)),
					UsageResetPeriod: types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY,
					IsSoftLimit:      true,
				},
				{
					PlanID:      testPlan2.ID,
					FeatureID:   staticFeature.ID,
					FeatureType: types.FeatureTypeStatic,
					StaticValue: "premium",
				},
			},
		}

		resp, err := s.service.CreateBulkEntitlement(s.GetContext(), req)
		s.NoError(err)
		s.NotNil(resp)
		s.Len(resp.Items, 3)

		// Verify first entitlement (boolean)
		ent1 := resp.Items[0]
		s.Equal(types.ENTITLEMENT_ENTITY_TYPE_PLAN, ent1.Entitlement.EntityType)
		s.Equal(testPlan1.ID, ent1.Entitlement.EntityID)
		s.Equal(boolFeature.ID, ent1.Entitlement.FeatureID)
		s.Equal(types.FeatureTypeBoolean, ent1.Entitlement.FeatureType)
		s.True(ent1.Entitlement.IsEnabled)
		s.NotNil(ent1.Feature)
		s.Equal(boolFeature.ID, ent1.Feature.Feature.ID)
		s.NotNil(ent1.Plan)
		s.Equal(testPlan1.ID, ent1.Plan.Plan.ID)

		// Verify second entitlement (metered)
		ent2 := resp.Items[1]
		s.Equal(types.ENTITLEMENT_ENTITY_TYPE_PLAN, ent2.Entitlement.EntityType)
		s.Equal(testPlan1.ID, ent2.Entitlement.EntityID)
		s.Equal(meteredFeature.ID, ent2.Entitlement.FeatureID)
		s.Equal(types.FeatureTypeMetered, ent2.Entitlement.FeatureType)
		s.Equal(int64(1000), *ent2.Entitlement.UsageLimit)
		s.Equal(types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY, ent2.Entitlement.UsageResetPeriod)
		s.True(ent2.Entitlement.IsSoftLimit)
		s.NotNil(ent2.Feature)
		s.Equal(meteredFeature.ID, ent2.Feature.Feature.ID)
		s.NotNil(ent2.Plan)
		s.Equal(testPlan1.ID, ent2.Plan.Plan.ID)

		// Verify third entitlement (static)
		ent3 := resp.Items[2]
		s.Equal(types.ENTITLEMENT_ENTITY_TYPE_PLAN, ent3.Entitlement.EntityType)
		s.Equal(testPlan2.ID, ent3.Entitlement.EntityID)
		s.Equal(staticFeature.ID, ent3.Entitlement.FeatureID)
		s.Equal(types.FeatureTypeStatic, ent3.Entitlement.FeatureType)
		s.Equal("premium", ent3.Entitlement.StaticValue)
		s.NotNil(ent3.Feature)
		s.Equal(staticFeature.ID, ent3.Feature.Feature.ID)
		s.NotNil(ent3.Plan)
		s.Equal(testPlan2.ID, ent3.Plan.Plan.ID)
	})

	s.Run("Invalid Bulk Entitlement - Feature Type Mismatch", func() {
		req := dto.CreateBulkEntitlementRequest{
			Items: []dto.CreateEntitlementRequest{
				{
					PlanID:      testPlan1.ID,
					FeatureID:   boolFeature.ID,
					FeatureType: types.FeatureTypeMetered, // Wrong type
					IsEnabled:   true,
				},
			},
		}

		resp, err := s.service.CreateBulkEntitlement(s.GetContext(), req)
		s.Error(err)
		s.Nil(resp)
		s.Contains(err.Error(), "feature type mismatch")
	})

	s.Run("Invalid Bulk Entitlement - Non-existent Plan", func() {
		req := dto.CreateBulkEntitlementRequest{
			Items: []dto.CreateEntitlementRequest{
				{
					PlanID:      "non-existent-plan",
					FeatureID:   boolFeature.ID,
					FeatureType: types.FeatureTypeBoolean,
					IsEnabled:   true,
				},
			},
		}

		resp, err := s.service.CreateBulkEntitlement(s.GetContext(), req)
		s.Error(err)
		s.Nil(resp)
		s.Contains(err.Error(), "not found")
	})

	s.Run("Invalid Bulk Entitlement - Non-existent Feature", func() {
		req := dto.CreateBulkEntitlementRequest{
			Items: []dto.CreateEntitlementRequest{
				{
					PlanID:      testPlan1.ID,
					FeatureID:   "non-existent-feature",
					FeatureType: types.FeatureTypeBoolean,
					IsEnabled:   true,
				},
			},
		}

		resp, err := s.service.CreateBulkEntitlement(s.GetContext(), req)
		s.Error(err)
		s.Nil(resp)
		s.Contains(err.Error(), "not found")
	})

	s.Run("Invalid Bulk Entitlement - Empty Request", func() {
		req := dto.CreateBulkEntitlementRequest{
			Items: []dto.CreateEntitlementRequest{},
		}

		resp, err := s.service.CreateBulkEntitlement(s.GetContext(), req)
		s.Error(err)
		s.Nil(resp)
		s.Contains(err.Error(), "at least one entitlement is required")
	})

	s.Run("Invalid Bulk Entitlement - Too Many Entitlements", func() {
		entitlements := make([]dto.CreateEntitlementRequest, 101)
		for i := 0; i < 101; i++ {
			entitlements[i] = dto.CreateEntitlementRequest{
				PlanID:      testPlan1.ID,
				FeatureID:   boolFeature.ID,
				FeatureType: types.FeatureTypeBoolean,
				IsEnabled:   true,
			}
		}

		req := dto.CreateBulkEntitlementRequest{
			Items: entitlements,
		}

		resp, err := s.service.CreateBulkEntitlement(s.GetContext(), req)
		s.Error(err)
		s.Nil(resp)
		s.Contains(err.Error(), "too many entitlements in bulk request")
	})
}
