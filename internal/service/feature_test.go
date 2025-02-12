package service

import (
	"sort"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/suite"
)

type FeatureServiceSuite struct {
	testutil.BaseServiceTestSuite
	service     FeatureService
	featureRepo *testutil.InMemoryFeatureStore
	meterRepo   *testutil.InMemoryMeterStore
	testData    struct {
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
}

func (s *FeatureServiceSuite) setupService() {
	s.featureRepo = testutil.NewInMemoryFeatureStore()
	s.meterRepo = testutil.NewInMemoryMeterStore()

	s.service = NewFeatureService(
		s.featureRepo,
		s.meterRepo,
		s.GetLogger(),
	)
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
			name: "error - missing meter ID for metered feature",
			req: dto.CreateFeatureRequest{
				Name:        "Test Feature",
				Description: "Test Description",
				LookupKey:   "test_key",
				Type:        types.FeatureTypeMetered,
			},
			wantErr:   true,
			errString: "meter_id is required for meter features",
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
			name: "error - missing lookup key",
			req: dto.CreateFeatureRequest{
				Name:        "Test Feature",
				Description: "Test Description",
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
			errString: "failed to get meter",
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
			errString: "cannot create feature with archived meter",
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
		})
	}
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

func (s *FeatureServiceSuite) TestUpdateFeature() {
	tests := []struct {
		name      string
		id        string
		req       dto.UpdateFeatureRequest
		wantErr   bool
		errString string
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
			s.Equal(*tt.req.Name, resp.Name)
			s.Equal(*tt.req.Description, resp.Description)
			s.Equal(*tt.req.Metadata, resp.Metadata)
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
			errString: "not found",
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
