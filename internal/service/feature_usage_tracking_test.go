package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
)

type FeatureUsageTrackingSuite struct {
	suite.Suite
	ctx    context.Context
	impl   *featureUsageTrackingService
	pubsub *testutil.InMemoryPubSub
	logger *logger.Logger
	cfg    *config.Configuration
}

func TestFeatureUsageTrackingSuite(t *testing.T) {
	suite.Run(t, new(FeatureUsageTrackingSuite))
}

func (s *FeatureUsageTrackingSuite) SetupTest() {
	s.ctx = testutil.SetupContext()
	s.logger = logger.GetLogger()
	s.cfg = config.GetDefaultConfig()
	// ensure kafka config validates without real broker
	s.cfg.Kafka.Brokers = []string{"localhost:9092"}
	s.cfg.Kafka.ClientID = "test-client"

	// set distinct topics to avoid collisions in tests
	s.cfg.FeatureUsageTracking.TopicBackfill = "feature-usage-backfill"

	// use in-memory pubsub and manually construct service to avoid Kafka
	s.pubsub = testutil.NewInMemoryPubSub()
	s.impl = &featureUsageTrackingService{
		ServiceParams:  ServiceParams{Logger: s.logger, Config: s.cfg},
		backfillPubSub: s.pubsub,
	}
}

func (s *FeatureUsageTrackingSuite) TearDownTest() {
	if s.pubsub != nil {
		s.pubsub.ClearMessages()
	}
}

func (s *FeatureUsageTrackingSuite) TestPublishEvent_Backfill_TopicAndMetadata() {
	evt := events.NewEvent(
		"feature_used",
		types.GetTenantID(s.ctx),
		"cust-42",
		map[string]interface{}{"feature_id": "feat-abc"},
		time.Now().UTC(),
		"evt-abc",
		"",
		"test",
		types.GetEnvironmentID(s.ctx),
	)

	require.NoError(s.T(), s.impl.PublishEvent(s.ctx, evt, true))
	msgs := s.pubsub.GetMessages(s.cfg.FeatureUsageTracking.TopicBackfill)
	require.Len(s.T(), msgs, 1)
	var got events.Event
	require.NoError(s.T(), json.Unmarshal(msgs[0].Payload, &got))
	s.Equal(evt.ID, got.ID)
	s.Equal(types.GetTenantID(s.ctx), msgs[0].Metadata.Get("tenant_id"))
	s.Equal(types.GetEnvironmentID(s.ctx), msgs[0].Metadata.Get("environment_id"))
	s.Equal(types.GetTenantID(s.ctx)+":"+evt.ExternalCustomerID, msgs[0].Metadata.Get("partition_key"))
}

func (s *FeatureUsageTrackingSuite) TestPublishEvent_Backfill_PartitionKey_EmptyExternalID() {
	evt := events.NewEvent(
		"feature_used",
		types.GetTenantID(s.ctx),
		"",
		map[string]interface{}{"feature_id": "feat-abc"},
		time.Now().UTC(),
		"evt-empty",
		"",
		"test",
		types.GetEnvironmentID(s.ctx),
	)

	require.NoError(s.T(), s.impl.PublishEvent(s.ctx, evt, true))
	msgs := s.pubsub.GetMessages(s.cfg.FeatureUsageTracking.TopicBackfill)
	require.Len(s.T(), msgs, 1)
	s.Equal(types.GetTenantID(s.ctx), msgs[0].Metadata.Get("partition_key"))
}

func (s *FeatureUsageTrackingSuite) TestPublishEvent_NoBackfill_NoPublish() {
	evt := events.NewEvent(
		"feature_used",
		types.GetTenantID(s.ctx),
		"cust-42",
		map[string]interface{}{"feature_id": "feat-abc"},
		time.Now().UTC(),
		"evt-no-bf",
		"",
		"test",
		types.GetEnvironmentID(s.ctx),
	)

	require.NoError(s.T(), s.impl.PublishEvent(s.ctx, evt, false))
	msgs := s.pubsub.GetMessages(s.cfg.FeatureUsageTracking.TopicBackfill)
	s.Len(msgs, 0)
}

func (s *FeatureUsageTrackingSuite) TestPublishEvent_Backfill_NoPubSub_Error() {
	evt := events.NewEvent(
		"feature_used",
		types.GetTenantID(s.ctx),
		"cust-42",
		map[string]interface{}{"feature_id": "feat-abc"},
		time.Now().UTC(),
		"evt-err",
		"",
		"test",
		types.GetEnvironmentID(s.ctx),
	)

	// simulate missing pubsub
	s.impl.backfillPubSub = nil
	s.Error(s.impl.PublishEvent(s.ctx, evt, true))
}

// --- Analytics validation tests ---

func (s *FeatureUsageTrackingSuite) TestValidateAnalyticsRequest_MissingExternalID_ValidationError() {
	req := &dto.GetUsageAnalyticsRequest{}
	err := s.impl.validateAnalyticsRequest(req)
	s.Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *FeatureUsageTrackingSuite) TestValidateAnalyticsRequest_WindowSize_Invalid_ValidationError() {
	req := &dto.GetUsageAnalyticsRequest{
		ExternalCustomerID: "cust-1",
		WindowSize:         types.WindowSize("INVALID"),
	}
	err := s.impl.validateAnalyticsRequest(req)
	s.Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *FeatureUsageTrackingSuite) TestValidateAnalyticsRequest_WindowSize_Empty_Ok() {
	req := &dto.GetUsageAnalyticsRequest{
		ExternalCustomerID: "cust-1",
		WindowSize:         "",
	}
	s.NoError(s.impl.validateAnalyticsRequest(req))
}

func (s *FeatureUsageTrackingSuite) TestValidateAnalyticsRequest_WindowSize_Valid_Ok() {
	req := &dto.GetUsageAnalyticsRequest{
		ExternalCustomerID: "cust-1",
		WindowSize:         types.WindowSize15Min,
	}
	s.NoError(s.impl.validateAnalyticsRequest(req))
}

func (s *FeatureUsageTrackingSuite) TestCreateAnalyticsParams_MapsFields() {
	req := &dto.GetUsageAnalyticsRequest{
		ExternalCustomerID: "cust-123",
		FeatureIDs:         []string{"feat-a", "feat-b"},
		Sources:            []string{"api", "sdk"},
		StartTime:          time.Now().Add(-24 * time.Hour).UTC(),
		EndTime:            time.Now().UTC(),
		GroupBy:            []string{"source", "feature_id", "properties.org_id"},
		WindowSize:         types.WindowSize15Min,
		PropertyFilters:    map[string][]string{"org_id": {"acme"}},
	}

	params := s.impl.createAnalyticsParams(s.ctx, req)

	s.Equal(types.GetTenantID(s.ctx), params.TenantID)
	s.Equal(types.GetEnvironmentID(s.ctx), params.EnvironmentID)
	s.Equal(req.ExternalCustomerID, params.ExternalCustomerID)
	s.Equal(req.FeatureIDs, params.FeatureIDs)
	s.Equal(req.Sources, params.Sources)
	s.Equal(req.StartTime, params.StartTime)
	s.Equal(req.EndTime, params.EndTime)
	s.Equal(req.GroupBy, params.GroupBy)
	s.Equal(req.WindowSize, params.WindowSize)
	s.Equal(req.PropertyFilters, params.PropertyFilters)
}

func (s *FeatureUsageTrackingSuite) TestToGetUsageAnalyticsResponseDTO_SumAggregation_BasicMapping() {
	analytic := &events.DetailedUsageAnalytic{
		FeatureID:       "feat-1",
		FeatureName:     "API Calls",
		EventName:       "api_request",
		Source:          "api",
		MeterID:         "meter-1",
		PriceID:         "price-1",
		SubLineItemID:   "subli-1",
		SubscriptionID:  "sub-1",
		AggregationType: types.AggregationSum,
		Unit:            "request",
		UnitPlural:      "requests",
		TotalUsage:      decimal.NewFromInt(10),
		TotalCost:       decimal.NewFromInt(50),
		Currency:        "usd",
		EventCount:      3,
		Properties:      map[string]string{"org_id": "acme"},
	}

	data := &AnalyticsData{Analytics: []*events.DetailedUsageAnalytic{analytic}}
	req := &dto.GetUsageAnalyticsRequest{}

	resp, err := s.impl.ToGetUsageAnalyticsResponseDTO(s.ctx, data, req)
	s.NoError(err)
	s.Len(resp.Items, 1)

	item := resp.Items[0]
	s.Equal("feat-1", item.FeatureID)
	s.Equal("price-1", item.PriceID)
	s.Equal("meter-1", item.MeterID)
	s.Equal("subli-1", item.SubLineItemID)
	s.Equal("sub-1", item.SubscriptionID)
	s.Equal("API Calls", item.FeatureName)
	s.Equal("api_request", item.EventName)
	s.Equal("api", item.Source)
	s.Equal(types.AggregationSum, item.AggregationType)
	s.Equal(decimal.NewFromInt(10), item.TotalUsage)
	s.Equal(decimal.NewFromInt(50), item.TotalCost)
	s.Equal("usd", resp.Currency)
	s.Equal(uint64(3), item.EventCount)
	s.Equal(map[string]string{"org_id": "acme"}, item.Properties)
	s.Empty(item.Points)
}

func (s *FeatureUsageTrackingSuite) TestToGetUsageAnalyticsResponseDTO_MaxAggregation_UsesTotalUsage() {
	analytic := &events.DetailedUsageAnalytic{
		FeatureID:       "feat-2",
		AggregationType: types.AggregationMax,
		TotalUsage:      decimal.NewFromInt(20),
		MaxUsage:        decimal.NewFromInt(7),
		Currency:        "usd",
	}

	data := &AnalyticsData{Analytics: []*events.DetailedUsageAnalytic{analytic}}
	req := &dto.GetUsageAnalyticsRequest{}

	resp, err := s.impl.ToGetUsageAnalyticsResponseDTO(s.ctx, data, req)
	s.NoError(err)
	s.Len(resp.Items, 1)
	s.Equal(decimal.NewFromInt(20), resp.Items[0].TotalUsage)
}

func (s *FeatureUsageTrackingSuite) TestToGetUsageAnalyticsResponseDTO_Expand_Meter_Feature_Price() {
	// Minimal domain objects to validate expansion mapping
	m := &meter.Meter{
		ID:        "meter-1",
		EventName: "api_event",
		Name:      "API Meter",
		Aggregation: meter.Aggregation{
			Type: types.AggregationSum,
		},
	}
	f := &feature.Feature{
		ID:           "feat-1",
		Name:         "API",
		UnitSingular: "request",
		UnitPlural:   "requests",
	}
	p := &price.Price{
		ID:         "price-1",
		Currency:   "usd",
		EntityType: types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:   "plan-1",
		BaseModel:  types.BaseModel{Status: types.StatusPublished},
	}

	analytic := &events.DetailedUsageAnalytic{
		FeatureID:  "feat-1",
		MeterID:    "meter-1",
		PriceID:    "price-1",
		TotalUsage: decimal.NewFromInt(1),
		Currency:   "usd",
	}

	data := &AnalyticsData{
		Analytics: []*events.DetailedUsageAnalytic{analytic},
		Meters:    map[string]*meter.Meter{"meter-1": m},
		Features:  map[string]*feature.Feature{"feat-1": f},
		Prices:    map[string]*price.Price{"price-1": p},
	}

	req := &dto.GetUsageAnalyticsRequest{Expand: []string{"feature", "meter", "price"}}

	resp, err := s.impl.ToGetUsageAnalyticsResponseDTO(s.ctx, data, req)
	s.NoError(err)
	s.Len(resp.Items, 1)

	item := resp.Items[0]
	s.NotNil(item.Feature)
	s.NotNil(item.Meter)
	s.NotNil(item.Price)
	s.Equal("plan-1", item.PlanID)
}

func (s *FeatureUsageTrackingSuite) TestToGetUsageAnalyticsResponseDTO_WindowSize_PointsMapping_Max() {
	now := time.Now().UTC()
	points := []events.UsageAnalyticPoint{
		{Timestamp: now.Add(-30 * time.Minute), MaxUsage: decimal.NewFromInt(7), Cost: decimal.Zero},
		{Timestamp: now.Add(-15 * time.Minute), MaxUsage: decimal.NewFromInt(3), Cost: decimal.Zero},
	}

	analytic := &events.DetailedUsageAnalytic{
		AggregationType: types.AggregationMax,
		Points:          points,
		Currency:        "usd",
	}

	data := &AnalyticsData{Analytics: []*events.DetailedUsageAnalytic{analytic}}
	req := &dto.GetUsageAnalyticsRequest{WindowSize: types.WindowSize15Min}

	resp, err := s.impl.ToGetUsageAnalyticsResponseDTO(s.ctx, data, req)
	s.NoError(err)
	s.Len(resp.Items, 1)
	s.Len(resp.Items[0].Points, 2)
	s.Equal(decimal.NewFromInt(7), resp.Items[0].Points[0].Usage)
	s.Equal(decimal.NewFromInt(3), resp.Items[0].Points[1].Usage)
}