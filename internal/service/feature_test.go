package service

import (
	"sort"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/group"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type FeatureServiceSuite struct {
	testutil.BaseServiceTestSuite
	service         FeatureService
	featureRepo     *testutil.InMemoryFeatureStore
	meterRepo       *testutil.InMemoryMeterStore
	entitlementRepo *testutil.InMemoryEntitlementStore
	groupRepo       *testutil.InMemoryGroupStore
	testData        struct {
		meters struct {
			apiCalls *meter.Meter
			storage  *meter.Meter
		}
		features struct {
			apiCalls *feature.Feature
			storage  *feature.Feature
			boolean  *feature.Feature
		}
	}
}

func TestFeatureService(t *testing.T) {
	suite.Run(t, new(FeatureServiceSuite))
}

func (s *FeatureServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
	s.setupTestData()
}

func (s *FeatureServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
	s.featureRepo.Clear()
	s.meterRepo.Clear()
	s.entitlementRepo.Clear()
	if s.groupRepo != nil {
		s.groupRepo.Clear()
	}
}

func (s *FeatureServiceSuite) setupService() {
	s.featureRepo = testutil.NewInMemoryFeatureStore()
	s.meterRepo = testutil.NewInMemoryMeterStore()
	s.entitlementRepo = testutil.NewInMemoryEntitlementStore()
	s.groupRepo = testutil.NewInMemoryGroupStore()

	s.service = NewFeatureService(ServiceParams{
		Logger:           s.GetLogger(),
		DB:               s.GetDB(),
		FeatureRepo:      s.featureRepo,
		MeterRepo:        s.meterRepo,
		EntitlementRepo:  s.entitlementRepo,
		GroupRepo:        s.groupRepo,
		WebhookPublisher: s.GetWebhookPublisher(),
	})
}

func (s *FeatureServiceSuite) setupTestData() {
	// Clear any existing data
	s.BaseServiceTestSuite.ClearStores()

	// Create test meters
	s.testData.meters.apiCalls = &meter.Meter{
		ID:        "meter_api_calls",
		Name:      "API Calls",
		EventName: "api_call",
		Aggregation: meter.Aggregation{
			Type: types.AggregationCount,
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.meterRepo.CreateMeter(s.GetContext(), s.testData.meters.apiCalls))

	s.testData.meters.storage = &meter.Meter{
		ID:        "meter_storage",
		Name:      "Storage",
		EventName: "storage_usage",
		Aggregation: meter.Aggregation{
			Type:  types.AggregationSum,
			Field: "bytes_used",
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.meterRepo.CreateMeter(s.GetContext(), s.testData.meters.storage))

	now := time.Now().UTC()
	// Create test features
	s.testData.features.apiCalls = &feature.Feature{
		ID:          "feature_api_calls",
		Name:        "API Calls Feature",
		Description: "Track API usage",
		LookupKey:   "api_calls",
		Type:        types.FeatureTypeMetered,
		MeterID:     s.testData.meters.apiCalls.ID,
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
		ReportingUnit: testutil.NewReportingUnit("thousand tokens", "thousands of tokens", "0.001"),
	}
	s.testData.features.apiCalls.CreatedAt = now
	s.NoError(s.featureRepo.Create(s.GetContext(), s.testData.features.apiCalls))

	s.testData.features.storage = &feature.Feature{
		ID:          "feature_storage",
		Name:        "Storage Feature",
		Description: "Track storage usage",
		LookupKey:   "storage",
		Type:        types.FeatureTypeMetered,
		MeterID:     s.testData.meters.storage.ID,
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	s.testData.features.storage.CreatedAt = now.Add(-time.Hour * 12)
	s.NoError(s.featureRepo.Create(s.GetContext(), s.testData.features.storage))

	s.testData.features.boolean = &feature.Feature{
		ID:          "feature_boolean",
		Name:        "Boolean Feature",
		Description: "Simple boolean feature",
		LookupKey:   "boolean_feature",
		Type:        types.FeatureTypeBoolean,
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	s.testData.features.boolean.CreatedAt = now.Add(-time.Hour * 24)
	s.NoError(s.featureRepo.Create(s.GetContext(), s.testData.features.boolean))
}

func (s *FeatureServiceSuite) TestCreateFeature() {
	// Create an archived meter for testing
	archivedMeter := &meter.Meter{
		ID:        "meter_archived",
		Name:      "Archived Meter",
		EventName: "archived_event",
		Aggregation: meter.Aggregation{
			Type: types.AggregationCount,
		},
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	archivedMeter.Status = types.StatusArchived
	s.NoError(s.meterRepo.CreateMeter(s.GetContext(), archivedMeter))

	tests := []struct {
		name      string
		req       dto.CreateFeatureRequest
		wantErr   bool
		errString string
	}{
		{
			name: "successful creation of metered feature",
			req: dto.CreateFeatureRequest{
				Name:        "Test Feature",
				Description: "Test Description",
				LookupKey:   "test_key",
				Type:        types.FeatureTypeMetered,
				MeterID:     s.testData.meters.apiCalls.ID,
				Metadata:    map[string]string{"key": "value"},
			},
		},
		{
			name: "successful creation of boolean feature",
			req: dto.CreateFeatureRequest{
				Name:        "Boolean Feature",
				Description: "Test Description",
				LookupKey:   "boolean_key",
				Type:        types.FeatureTypeBoolean,
				Metadata:    map[string]string{"key": "value"},
			},
		},
		{
			name: "error - missing meter ID and  for metered feature",
			req: dto.CreateFeatureRequest{
				Name:        "Test Feature",
				Description: "Test Description",
				LookupKey:   "test_key",
				Type:        types.FeatureTypeMetered,
			},
			wantErr:   true,
			errString: "either meter_id or meter must be provided",
		},
		{
			name: "error - missing name",
			req: dto.CreateFeatureRequest{
				Description: "Test Description",
				LookupKey:   "test_key",
				Type:        types.FeatureTypeBoolean,
			},
			wantErr: true,
		},
		{
			name: "error - non-existent meter",
			req: dto.CreateFeatureRequest{
				Name:        "Test Feature",
				Description: "Test Description",
				LookupKey:   "test_key",
				Type:        types.FeatureTypeMetered,
				MeterID:     "non_existent_meter",
			},
			wantErr:   true,
			errString: "item not found",
		},
		{
			name: "error - archived meter",
			req: dto.CreateFeatureRequest{
				Name:        "Test Feature",
				Description: "Test Description",
				LookupKey:   "test_key",
				Type:        types.FeatureTypeMetered,
				MeterID:     archivedMeter.ID,
			},
			wantErr:   true,
			errString: "invalid meter status",
		},
		{
			name: "successful creation with reporting_unit",
			req: dto.CreateFeatureRequest{
				Name:        "Tokens Feature",
				Description: "Token usage",
				LookupKey:   "tokens_key",
				Type:        types.FeatureTypeMetered,
				MeterID:     s.testData.meters.apiCalls.ID,
				UnitSingular: "token",
				UnitPlural:   "tokens",
				ReportingUnit: testutil.NewReportingUnit("thousand tokens", "thousands of tokens", "0.001"),
			},
		},
		{
			name: "error - reporting_unit missing unit_singular",
			req: dto.CreateFeatureRequest{
				Name:        "Test Feature",
				LookupKey:   "test_key",
				Type:        types.FeatureTypeMetered,
				MeterID:     s.testData.meters.apiCalls.ID,
				ReportingUnit: &types.ReportingUnit{
					UnitSingular:   "",
					UnitPlural:     "thousands of tokens",
					ConversionRate: lo.ToPtr(decimal.RequireFromString("0.001")),
				},
			},
			wantErr:   true,
			errString: "unit_singular",
		},
		{
			name: "error - reporting_unit missing conversion_rate",
			req: dto.CreateFeatureRequest{
				Name:        "Test Feature",
				LookupKey:   "test_key",
				Type:        types.FeatureTypeMetered,
				MeterID:     s.testData.meters.apiCalls.ID,
				ReportingUnit: &types.ReportingUnit{
					UnitSingular:   "thousand tokens",
					UnitPlural:     "thousands of tokens",
					ConversionRate: nil,
				},
			},
			wantErr:   true,
			errString: "conversion_rate",
		},
		{
			name: "error - non-existent group_id",
			req: dto.CreateFeatureRequest{
				Name:        "Feature With Bad Group",
				LookupKey:   "bad_group_feature",
				Type:        types.FeatureTypeBoolean,
				GroupID:     "group_nonexistent",
			},
			wantErr:   true,
			errString: "not found",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			resp, err := s.service.CreateFeature(s.GetContext(), tt.req)
			if tt.wantErr {
				s.Error(err)
				if tt.errString != "" {
					s.Contains(err.Error(), tt.errString)
				}
				return
			}

			s.NoError(err)
			s.NotNil(resp)
			s.Equal(tt.req.Name, resp.Name)
			s.Equal(tt.req.Description, resp.Description)
			s.Equal(tt.req.LookupKey, resp.LookupKey)
			s.Equal(tt.req.Type, resp.Type)
			s.Equal(tt.req.Metadata, resp.Metadata)
			if tt.req.Type == types.FeatureTypeMetered {
				s.Equal(tt.req.MeterID, resp.MeterID)
			}
			if tt.req.ReportingUnit != nil {
				s.Require().NotNil(resp.ReportingUnit)
				s.Equal(tt.req.ReportingUnit.UnitSingular, resp.ReportingUnit.UnitSingular)
				s.Equal(tt.req.ReportingUnit.UnitPlural, resp.ReportingUnit.UnitPlural)
				s.Require().NotNil(tt.req.ReportingUnit.ConversionRate)
				s.Require().NotNil(resp.ReportingUnit.ConversionRate)
				s.True(tt.req.ReportingUnit.ConversionRate.Equal(*resp.ReportingUnit.ConversionRate))
			}
		})
	}
}

func (s *FeatureServiceSuite) TestCreateFeature_InvalidGroupValidation() {
	// Create a group with entity_type "price" (wrong for features)
	priceGroup := &group.Group{
		ID:            "group_price_type",
		Name:          "Price Group",
		EntityType:    types.GroupEntityTypePrice,
		LookupKey:     "price_group_key",
		EnvironmentID: types.GetEnvironmentID(s.GetContext()),
		BaseModel:     types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.groupRepo.Create(s.GetContext(), priceGroup))

	req := dto.CreateFeatureRequest{
		Name:        "Feature With Wrong Group Type",
		LookupKey:   "wrong_group_type_feature",
		Type:        types.FeatureTypeBoolean,
		GroupID:     priceGroup.ID,
	}
	_, err := s.service.CreateFeature(s.GetContext(), req)
	s.Error(err)
	s.Contains(err.Error(), "invalid group type")
}

func (s *FeatureServiceSuite) TestCreateFeature_WithGroupID() {
	// Create a group of entity_type feature first
	grp := &group.Group{
		ID:            "group_feature_test",
		Name:          "Feature Group",
		EntityType:    types.GroupEntityTypeFeature,
		LookupKey:     "feature_group_key",
		EnvironmentID: types.GetEnvironmentID(s.GetContext()),
		BaseModel:     types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.groupRepo.Create(s.GetContext(), grp))

	req := dto.CreateFeatureRequest{
		Name:        "Grouped Feature",
		Description: "Feature in a group",
		LookupKey:   "grouped_feature",
		Type:        types.FeatureTypeBoolean,
		GroupID:     grp.ID,
	}
	resp, err := s.service.CreateFeature(s.GetContext(), req)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(req.Name, resp.Name)
	s.Equal(grp.ID, resp.GroupID)
	s.NotNil(resp.Group, "response should include group object when feature has group_id")
	s.Equal(grp.ID, resp.Group.ID)
	s.Equal(grp.Name, resp.Group.Name)
	s.Equal(string(grp.EntityType), resp.Group.EntityType)
}

func (s *FeatureServiceSuite) TestGetFeature() {
	tests := []struct {
		name      string
		id        string
		wantErr   bool
		errString string
	}{
		{
			name: "successful retrieval of metered feature",
			id:   s.testData.features.apiCalls.ID,
		},
		{
			name: "successful retrieval of boolean feature",
			id:   s.testData.features.boolean.ID,
		},
		{
			name:      "error - feature not found",
			id:        "nonexistent-id",
			wantErr:   true,
			errString: "not found",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			resp, err := s.service.GetFeature(s.GetContext(), tt.id)
			if tt.wantErr {
				s.Error(err)
				if tt.errString != "" {
					s.Contains(err.Error(), tt.errString)
				}
				return
			}

			s.NoError(err)
			s.NotNil(resp)

			// Get the expected feature
			var expectedFeature *feature.Feature
			switch tt.id {
			case s.testData.features.apiCalls.ID:
				expectedFeature = s.testData.features.apiCalls
			case s.testData.features.boolean.ID:
				expectedFeature = s.testData.features.boolean
			}

			s.Equal(expectedFeature.Name, resp.Name)
			s.Equal(expectedFeature.Description, resp.Description)
			s.Equal(expectedFeature.LookupKey, resp.LookupKey)
			s.Equal(expectedFeature.Type, resp.Type)
			if expectedFeature.Type == types.FeatureTypeMetered {
				s.Equal(expectedFeature.MeterID, resp.MeterID)
				s.NotNil(resp.Meter)
				s.Equal(expectedFeature.MeterID, resp.Meter.ID)
			} else {
				s.Empty(resp.MeterID)
				s.Nil(resp.Meter)
			}
		})
	}
}

func (s *FeatureServiceSuite) TestGetFeature_WithGroup() {
	grp := &group.Group{
		ID:            "group_get_feature",
		Name:          "Get Feature Group",
		EntityType:    types.GroupEntityTypeFeature,
		LookupKey:     "get_feature_group",
		EnvironmentID: types.GetEnvironmentID(s.GetContext()),
		BaseModel:     types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.groupRepo.Create(s.GetContext(), grp))

	createReq := dto.CreateFeatureRequest{
		Name:        "Feature With Group",
		LookupKey:   "feature_with_group",
		Type:        types.FeatureTypeBoolean,
		GroupID:     grp.ID,
	}
	created, err := s.service.CreateFeature(s.GetContext(), createReq)
	s.NoError(err)
	s.Require().NotNil(created)

	resp, err := s.service.GetFeature(s.GetContext(), created.ID)
	s.NoError(err)
	s.Require().NotNil(resp)
	s.Equal(grp.ID, resp.GroupID)
	s.NotNil(resp.Group, "GetFeature should return group object when feature has group_id")
	s.Equal(grp.ID, resp.Group.ID)
	s.Equal(grp.Name, resp.Group.Name)
}

func (s *FeatureServiceSuite) TestGetFeatures() {
	tests := []struct {
		name           string
		filter         *types.FeatureFilter
		expectedTotal  int
		expectedIDs    []string
		expectExpanded bool
		wantErr        bool
		errString      string
	}{
		{
			name:          "get all features",
			filter:        types.NewNoLimitFeatureFilter(),
			expectedTotal: 3,
			expectedIDs: []string{
				s.testData.features.apiCalls.ID,
				s.testData.features.storage.ID,
				s.testData.features.boolean.ID,
			},
		},
		{
			name: "get features with pagination",
			filter: &types.FeatureFilter{
				QueryFilter: &types.QueryFilter{
					Limit:  lo.ToPtr(1),
					Offset: lo.ToPtr(0),
				},
			},
			expectedTotal: 3,
			expectedIDs:   []string{s.testData.features.apiCalls.ID},
		},
		{
			name: "get features with meter expansion",
			filter: &types.FeatureFilter{
				QueryFilter: &types.QueryFilter{
					Expand: lo.ToPtr("meters"),
				},
			},
			expectedTotal: 3,
			expectedIDs: []string{
				s.testData.features.apiCalls.ID,
				s.testData.features.storage.ID,
				s.testData.features.boolean.ID,
			},
			expectExpanded: true,
		},
		{
			name: "get features by IDs",
			filter: &types.FeatureFilter{
				FeatureIDs: []string{s.testData.features.apiCalls.ID},
			},
			expectedTotal: 1,
			expectedIDs:   []string{s.testData.features.apiCalls.ID},
		},
		{
			name: "get features by lookup key",
			filter: &types.FeatureFilter{
				LookupKey: s.testData.features.storage.LookupKey,
			},
			expectedTotal: 1,
			expectedIDs:   []string{s.testData.features.storage.ID},
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			resp, err := s.service.GetFeatures(s.GetContext(), tt.filter)
			if tt.wantErr {
				s.Error(err)
				if tt.errString != "" {
					s.Contains(err.Error(), tt.errString)
				}
				return
			}

			s.NoError(err)
			s.NotNil(resp)
			s.Equal(tt.expectedTotal, resp.Pagination.Total)
			s.Len(resp.Items, len(tt.expectedIDs))

			// Sort both expected and actual IDs for comparison
			sort.Strings(tt.expectedIDs)
			actualIDs := make([]string, len(resp.Items))
			for i, item := range resp.Items {
				actualIDs[i] = item.ID
			}
			sort.Strings(actualIDs)
			s.Equal(tt.expectedIDs, actualIDs)

			// Check meter expansion
			if tt.expectExpanded {
				for _, item := range resp.Items {
					if item.Type == types.FeatureTypeMetered {
						s.NotNil(item.Meter)
						s.Equal(item.MeterID, item.Meter.ID)
					} else {
						s.Nil(item.Meter)
					}
				}
			}
		})
	}
}

func (s *FeatureServiceSuite) TestGetFeaturesWithFiltersAndSorting() {
	tests := []struct {
		name           string
		filter         *types.FeatureFilter
		expectedTotal  int
		expectedIDs    []string
		expectExpanded bool
		wantErr        bool
		errString      string
	}{
		{
			name: "sort by created_at descending",
			filter: &types.FeatureFilter{
				QueryFilter: &types.QueryFilter{
					Sort:  lo.ToPtr("created_at"),
					Order: lo.ToPtr("desc"),
				},
			},
			expectedTotal: 3,
			expectedIDs: []string{
				s.testData.features.apiCalls.ID,
				s.testData.features.storage.ID,
				s.testData.features.boolean.ID,
			},
		},
		{
			name: "sort by name ascending",
			filter: &types.FeatureFilter{
				QueryFilter: &types.QueryFilter{
					Sort:  lo.ToPtr("name"),
					Order: lo.ToPtr("asc"),
				},
			},
			expectedTotal: 3,
			expectedIDs: []string{
				s.testData.features.apiCalls.ID,
				s.testData.features.boolean.ID,
				s.testData.features.storage.ID,
			},
		},
		{
			name: "filter by lookup key",
			filter: &types.FeatureFilter{
				LookupKey: s.testData.features.storage.LookupKey,
			},
			expectedTotal: 1,
			expectedIDs:   []string{s.testData.features.storage.ID},
		},
		{
			name: "filter by meter ID",
			filter: &types.FeatureFilter{
				MeterIDs: []string{s.testData.meters.apiCalls.ID},
			},
			expectedTotal: 1,
			expectedIDs:   []string{s.testData.features.apiCalls.ID},
		},
		{
			name: "filter by feature IDs",
			filter: &types.FeatureFilter{
				FeatureIDs: []string{
					s.testData.features.apiCalls.ID,
					s.testData.features.boolean.ID,
				},
			},
			expectedTotal: 2,
			expectedIDs: []string{
				s.testData.features.apiCalls.ID,
				s.testData.features.boolean.ID,
			},
		},
		{
			name: "filter with meter expansion",
			filter: &types.FeatureFilter{
				QueryFilter: &types.QueryFilter{
					Expand: lo.ToPtr("meters"),
				},
			},
			expectedTotal: 3,
			expectedIDs: []string{
				s.testData.features.apiCalls.ID,
				s.testData.features.storage.ID,
				s.testData.features.boolean.ID,
			},
			expectExpanded: true,
		},
		{
			name: "pagination with limit and offset",
			filter: &types.FeatureFilter{
				QueryFilter: &types.QueryFilter{
					Limit:  lo.ToPtr(1),
					Offset: lo.ToPtr(1),
					Sort:   lo.ToPtr("created_at"),
					Order:  lo.ToPtr("desc"),
				},
			},
			expectedTotal: 3,
			expectedIDs:   []string{s.testData.features.storage.ID},
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			resp, err := s.service.GetFeatures(s.GetContext(), tt.filter)
			if tt.wantErr {
				s.Error(err)
				if tt.errString != "" {
					s.Contains(err.Error(), tt.errString)
				}
				return
			}

			s.NoError(err)
			s.NotNil(resp)
			s.Equal(tt.expectedTotal, resp.Pagination.Total)
			s.Len(resp.Items, len(tt.expectedIDs))

			// Sort both expected and actual IDs for comparison
			sort.Strings(tt.expectedIDs)
			actualIDs := make([]string, len(resp.Items))
			for i, item := range resp.Items {
				actualIDs[i] = item.ID
			}
			sort.Strings(actualIDs)
			s.Equal(tt.expectedIDs, actualIDs)

			// Check meter expansion
			if tt.expectExpanded {
				for _, item := range resp.Items {
					if item.Type == types.FeatureTypeMetered {
						s.NotNil(item.Meter)
						s.Equal(item.MeterID, item.Meter.ID)
					} else {
						s.Nil(item.Meter)
					}
				}
			}
		})
	}
}

func (s *FeatureServiceSuite) TestUpdateFeature() {
	tests := []struct {
		name                        string
		id                          string
		req                         dto.UpdateFeatureRequest
		wantErr                     bool
		errString                   string
		expectReportingUnitPreserved bool
	}{
		{
			name: "successful update of metered feature",
			id:   s.testData.features.apiCalls.ID,
			req: dto.UpdateFeatureRequest{
				Name:        lo.ToPtr("Updated API Calls"),
				Description: lo.ToPtr("Updated Description"),
				Metadata:    lo.ToPtr(types.Metadata{"updated": "true"}),
			},
		},
		{
			name: "successful update of boolean feature",
			id:   s.testData.features.boolean.ID,
			req: dto.UpdateFeatureRequest{
				Name:        lo.ToPtr("Updated Boolean"),
				Description: lo.ToPtr("Updated Description"),
				Metadata:    lo.ToPtr(types.Metadata{"updated": "true"}),
			},
		},
		{
			name: "successful update with alert settings",
			id:   s.testData.features.apiCalls.ID,
			req: dto.UpdateFeatureRequest{
				Name:        lo.ToPtr("Updated API Calls"),
				Description: lo.ToPtr("Updated Description"),
				AlertSettings: &types.AlertSettings{
					Critical: &types.AlertThreshold{
						Threshold: decimal.NewFromInt(100),
						Condition: types.AlertConditionBelow,
					},
					Warning: &types.AlertThreshold{
						Threshold: decimal.NewFromInt(500),
						Condition: types.AlertConditionBelow,
					},
				},
			},
		},
		{
			name: "error - invalid alert settings (warning > critical for below condition)",
			id:   s.testData.features.apiCalls.ID,
			req: dto.UpdateFeatureRequest{
				AlertSettings: &types.AlertSettings{
					Critical: &types.AlertThreshold{
						Threshold: decimal.NewFromInt(1000),
						Condition: types.AlertConditionBelow,
					},
					Warning: &types.AlertThreshold{
						Threshold: decimal.NewFromInt(100),
						Condition: types.AlertConditionBelow,
					},
				},
			},
			wantErr:   true,
			errString: "warning threshold must be greater than critical threshold",
		},
		{
			name: "success - alert settings with only warning (keeps existing critical)",
			id:   s.testData.features.apiCalls.ID, // Use apiCalls which has existing alert settings
			req: dto.UpdateFeatureRequest{
				AlertSettings: &types.AlertSettings{
					Warning: &types.AlertThreshold{
						Threshold: decimal.NewFromInt(200),
						Condition: types.AlertConditionBelow,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "success - alert settings with only critical (keeps existing warning)",
			id:   s.testData.features.apiCalls.ID, // Use apiCalls which has existing alert settings
			req: dto.UpdateFeatureRequest{
				AlertSettings: &types.AlertSettings{
					Critical: &types.AlertThreshold{
						Threshold: decimal.NewFromInt(50),
						Condition: types.AlertConditionBelow,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "success - alert settings with alert_enabled explicitly set to false",
			id:   s.testData.features.apiCalls.ID,
			req: dto.UpdateFeatureRequest{
				AlertSettings: &types.AlertSettings{
					Critical: &types.AlertThreshold{
						Threshold: decimal.NewFromInt(100),
						Condition: types.AlertConditionBelow,
					},
					Warning: &types.AlertThreshold{
						Threshold: decimal.NewFromInt(500),
						Condition: types.AlertConditionBelow,
					},
					AlertEnabled: lo.ToPtr(false),
				},
			},
			wantErr: false,
		},
		{
			name: "success - alert settings without alert_enabled (defaults to false)",
			id:   s.testData.features.apiCalls.ID,
			req: dto.UpdateFeatureRequest{
				AlertSettings: &types.AlertSettings{
					Critical: &types.AlertThreshold{
						Threshold: decimal.NewFromInt(100),
						Condition: types.AlertConditionBelow,
					},
					Warning: &types.AlertThreshold{
						Threshold: decimal.NewFromInt(500),
						Condition: types.AlertConditionBelow,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "success - alert settings with all three thresholds (critical + warning + info)",
			id:   s.testData.features.apiCalls.ID,
			req: dto.UpdateFeatureRequest{
				AlertSettings: &types.AlertSettings{
					Critical: &types.AlertThreshold{
						Threshold: decimal.NewFromInt(100),
						Condition: types.AlertConditionBelow,
					},
					Warning: &types.AlertThreshold{
						Threshold: decimal.NewFromInt(500),
						Condition: types.AlertConditionBelow,
					},
					Info: &types.AlertThreshold{
						Threshold: decimal.NewFromInt(1000),
						Condition: types.AlertConditionBelow,
					},
					AlertEnabled: lo.ToPtr(true),
				},
			},
			wantErr: false,
		},
		{
			name: "success - alert settings with info-only (standalone)",
			id:   s.testData.features.apiCalls.ID,
			req: dto.UpdateFeatureRequest{
				AlertSettings: &types.AlertSettings{
					Info: &types.AlertThreshold{
						Threshold: decimal.NewFromInt(1000),
						Condition: types.AlertConditionBelow,
					},
					AlertEnabled: lo.ToPtr(true),
				},
			},
			wantErr: false,
		},
		{
			name: "success - alert settings with critical + info (no warning)",
			id:   s.testData.features.apiCalls.ID,
			req: dto.UpdateFeatureRequest{
				AlertSettings: &types.AlertSettings{
					Critical: &types.AlertThreshold{
						Threshold: decimal.NewFromInt(100),
						Condition: types.AlertConditionBelow,
					},
					Info: &types.AlertThreshold{
						Threshold: decimal.NewFromInt(1000),
						Condition: types.AlertConditionBelow,
					},
					AlertEnabled: lo.ToPtr(true),
				},
			},
			wantErr: false,
		},
		{
			name: "error - invalid info ordering (info < warning for below condition)",
			id:   s.testData.features.apiCalls.ID,
			req: dto.UpdateFeatureRequest{
				AlertSettings: &types.AlertSettings{
					Critical: &types.AlertThreshold{
						Threshold: decimal.NewFromInt(100),
						Condition: types.AlertConditionBelow,
					},
					Warning: &types.AlertThreshold{
						Threshold: decimal.NewFromInt(500),
						Condition: types.AlertConditionBelow,
					},
					Info: &types.AlertThreshold{
						Threshold: decimal.NewFromInt(200), // Wrong: should be > warning (500)
						Condition: types.AlertConditionBelow,
					},
					AlertEnabled: lo.ToPtr(true),
				},
			},
			wantErr:   true,
			errString: "info threshold must be greater than warning threshold",
		},
		{
			name: "error - alert_enabled true without any thresholds",
			id:   s.testData.features.storage.ID, // Use storage feature which has no alert settings
			req: dto.UpdateFeatureRequest{
				AlertSettings: &types.AlertSettings{
					AlertEnabled: lo.ToPtr(true),
				},
			},
			wantErr:   true,
			errString: "at least one threshold (critical, warning, or info) is required when alert_enabled is true",
		},
		{
			name: "success - partial update with info only (keeps existing critical/warning)",
			id:   s.testData.features.apiCalls.ID,
			req: dto.UpdateFeatureRequest{
				AlertSettings: &types.AlertSettings{
					Info: &types.AlertThreshold{
						Threshold: decimal.NewFromInt(1500),
						Condition: types.AlertConditionBelow,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "partial update - reporting_unit preserved when not in request",
			id:   s.testData.features.apiCalls.ID,
			req: dto.UpdateFeatureRequest{
				Name: lo.ToPtr("API Calls Feature Updated"),
			},
			expectReportingUnitPreserved: true,
		},
		{
			name: "successful update with reporting_unit",
			id:   s.testData.features.apiCalls.ID,
			req: dto.UpdateFeatureRequest{
				Name: lo.ToPtr("API Calls Feature"),
				ReportingUnit: testutil.NewReportingUnit("million tokens", "millions of tokens", "0.000001"),
			},
		},
		{
			name: "error - reporting_unit missing unit_plural",
			id:   s.testData.features.apiCalls.ID,
			req: dto.UpdateFeatureRequest{
				ReportingUnit: &types.ReportingUnit{
					UnitSingular:   "token",
					UnitPlural:     "",
					ConversionRate: lo.ToPtr(decimal.RequireFromString("1")),
				},
			},
			wantErr:   true,
			errString: "unit_plural",
		},
		{
			name: "error - feature not found",
			id:   "nonexistent-id",
			req: dto.UpdateFeatureRequest{
				Name: lo.ToPtr("Updated Name"),
			},
			wantErr:   true,
			errString: "not found",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			resp, err := s.service.UpdateFeature(s.GetContext(), tt.id, tt.req)
			if tt.wantErr {
				s.Error(err)
				if tt.errString != "" {
					s.Contains(err.Error(), tt.errString)
				}
				return
			}

			s.NoError(err)
			s.NotNil(resp)
			if tt.req.Name != nil {
				s.Equal(*tt.req.Name, resp.Name)
			}
			if tt.req.Description != nil {
				s.Equal(*tt.req.Description, resp.Description)
			}
			if tt.req.Metadata != nil {
				s.Equal(*tt.req.Metadata, resp.Metadata)
			}
			if tt.req.ReportingUnit != nil {
				s.Require().NotNil(resp.ReportingUnit)
				s.Equal(tt.req.ReportingUnit.UnitSingular, resp.ReportingUnit.UnitSingular)
				s.Equal(tt.req.ReportingUnit.UnitPlural, resp.ReportingUnit.UnitPlural)
				s.Require().NotNil(tt.req.ReportingUnit.ConversionRate)
				s.Require().NotNil(resp.ReportingUnit.ConversionRate)
				s.True(tt.req.ReportingUnit.ConversionRate.Equal(*resp.ReportingUnit.ConversionRate))
			}
			if tt.expectReportingUnitPreserved {
				orig := s.testData.features.apiCalls.ReportingUnit
				s.Require().NotNil(orig, "test data apiCalls must have ReportingUnit")
				s.Require().NotNil(resp.ReportingUnit, "response should preserve ReportingUnit")
				s.Equal(orig.UnitSingular, resp.ReportingUnit.UnitSingular)
				s.Equal(orig.UnitPlural, resp.ReportingUnit.UnitPlural)
				s.Require().NotNil(orig.ConversionRate)
				s.Require().NotNil(resp.ReportingUnit.ConversionRate)
				s.True(orig.ConversionRate.Equal(*resp.ReportingUnit.ConversionRate))
			}
			if tt.req.AlertSettings != nil {
				s.NotNil(resp.AlertSettings)

				// For partial updates, check that provided values are updated and others remain unchanged
				if tt.req.AlertSettings.Critical != nil {
					s.NotNil(resp.AlertSettings.Critical, "critical should be present")
					s.Equal(tt.req.AlertSettings.Critical.Threshold, resp.AlertSettings.Critical.Threshold, "critical threshold should be updated")
					s.Equal(tt.req.AlertSettings.Critical.Condition, resp.AlertSettings.Critical.Condition, "critical condition should be updated")
				}
				if tt.req.AlertSettings.Warning != nil {
					s.NotNil(resp.AlertSettings.Warning, "warning should be present")
					s.Equal(tt.req.AlertSettings.Warning.Threshold, resp.AlertSettings.Warning.Threshold, "warning threshold should be updated")
					s.Equal(tt.req.AlertSettings.Warning.Condition, resp.AlertSettings.Warning.Condition, "warning condition should be updated")
				}
				if tt.req.AlertSettings.Info != nil {
					s.NotNil(resp.AlertSettings.Info, "info should be present")
					s.Equal(tt.req.AlertSettings.Info.Threshold, resp.AlertSettings.Info.Threshold, "info threshold should be updated")
					s.Equal(tt.req.AlertSettings.Info.Condition, resp.AlertSettings.Info.Condition, "info condition should be updated")
				}
				if tt.req.AlertSettings.AlertEnabled != nil {
					s.Equal(tt.req.AlertSettings.AlertEnabled, resp.AlertSettings.AlertEnabled, "alert_enabled should be updated")
				}
			}
		})
	}
}

func (s *FeatureServiceSuite) TestDeleteFeature() {
	tests := []struct {
		name      string
		id        string
		wantErr   bool
		errString string
	}{
		{
			name: "successful deletion of metered feature",
			id:   s.testData.features.apiCalls.ID,
		},
		{
			name: "successful deletion of boolean feature",
			id:   s.testData.features.boolean.ID,
		},
		{
			name:      "error - feature not found",
			id:        "nonexistent-id",
			wantErr:   true,
			errString: "Feature with ID nonexistent-id was not found",
		},
		{
			name:      "error - empty feature ID",
			id:        "",
			wantErr:   true,
			errString: "feature ID is required",
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			err := s.service.DeleteFeature(s.GetContext(), tt.id)
			if tt.wantErr {
				s.Error(err)
				if tt.errString != "" {
					s.Contains(err.Error(), tt.errString)
				}
				return
			}

			s.NoError(err)
		})
	}
}

func (s *FeatureServiceSuite) TestFeature_ToReportingValue() {
	// apiCalls feature has ReportingUnit: thousand tokens, conversion_rate 0.001
	// Formula: display = unit_value / conversion_rate, rounded to 2 decimals
	f := s.testData.features.apiCalls
	s.Require().NotNil(f)
	s.Require().NotNil(f.ReportingUnit)
	s.Require().NotNil(f.ReportingUnit.ConversionRate)

	tests := []struct {
		name        string
		feature     *feature.Feature
		unitValue   decimal.Decimal
		wantDisplay decimal.Decimal // expected converted value (rounded to 2 decimals)
		wantErr     bool
		errContains string
	}{
		{
			name:        "converts base to display - 1000 base with rate 0.001 = 1000000 display",
			feature:     f,
			unitValue:   decimal.NewFromInt(1000),
			wantDisplay: decimal.RequireFromString("1000000"),
			wantErr:     false,
		},
		{
			name:        "converts and rounds to 2 decimals",
			feature:     f,
			unitValue:   decimal.RequireFromString("1234.5678"),
			wantDisplay: decimal.RequireFromString("1234567.8"), // 1234.5678 / 0.001 = 1234567.8
			wantErr:     false,
		},
		{
			name:        "nil feature returns error",
			feature:     nil,
			unitValue:   decimal.Zero,
			wantErr:     true,
			errContains: "reporting_unit is required",
		},
		{
			name: "feature with nil ReportingUnit returns error",
			feature: &feature.Feature{
				ID:            "no-ru",
				Name:          "No RU",
				ReportingUnit: nil,
			},
			unitValue:   decimal.Zero,
			wantErr:     true,
			errContains: "reporting_unit is required",
		},
		{
			name: "feature with nil ConversionRate returns error",
			feature: &feature.Feature{
				ID: "no-rate",
				ReportingUnit: &types.ReportingUnit{
					UnitSingular:   "x",
					UnitPlural:     "xs",
					ConversionRate: nil,
				},
			},
			unitValue:   decimal.NewFromInt(1),
			wantErr:     true,
			errContains: "conversion_rate is required",
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			got, err := tt.feature.ToReportingValue(tt.unitValue)
			if tt.wantErr {
				s.Error(err)
				if tt.errContains != "" {
					s.Contains(err.Error(), tt.errContains)
				}
				s.Nil(got)
				return
			}
			s.NoError(err)
			s.Require().NotNil(got)
			s.True(tt.wantDisplay.Equal(*got), "ToReportingValue(unit_value) = display rounded to 2 decimals; got %s", got.String())
		})
	}
}
