package service

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
)

// RecordingPubSub is a lightweight stub to record published messages and peak concurrency
type RecordingPubSub struct {
	messages      map[string][]*message.Message
	maxConcurrent int
	current       int
}

func NewRecordingPubSub() *RecordingPubSub {
	return &RecordingPubSub{messages: make(map[string][]*message.Message)}
}

func (r *RecordingPubSub) Publish(ctx context.Context, topic string, msg *message.Message) error {
	// naive concurrency measurement
	r.current++
	if r.current > r.maxConcurrent {
		r.maxConcurrent = r.current
	}
	// simulate work to allow overlapping goroutines
	time.Sleep(5 * time.Millisecond)
	r.messages[topic] = append(r.messages[topic], msg)
	r.current--
	return nil
}

func (r *RecordingPubSub) Subscribe(ctx context.Context, topic string) (<-chan *message.Message, error) {
	ch := make(chan *message.Message)
	return ch, nil
}

func (r *RecordingPubSub) Close() error { return nil }

type EventPostProcessingSuite struct {
	suite.Suite
	ctx     context.Context
	service EventPostProcessingService
	impl    *eventPostProcessingService
	store   *testutil.InMemoryEventStore
	pubsub  *testutil.InMemoryPubSub
	logger  *logger.Logger
	cfg     *config.Configuration
}

func TestEventPostProcessingSuite(t *testing.T) {
	suite.Run(t, new(EventPostProcessingSuite))
}

func (s *EventPostProcessingSuite) SetupTest() {
	s.ctx = testutil.SetupContext()
	s.store = testutil.NewInMemoryEventStore()
	s.logger = logger.GetLogger()
	s.cfg = config.GetDefaultConfig()
	// ensure kafka config validates without real broker
	s.cfg.Kafka.Brokers = []string{"localhost:9092"}
	s.cfg.Kafka.ClientID = "test-client"

	// set distinct topics to avoid collisions in tests
	s.cfg.EventPostProcessing.Topic = "event-postproc"
	s.cfg.EventPostProcessing.TopicBackfill = "event-postproc-backfill"

	// use in-memory pubsub and manually construct service to avoid Kafka
	s.pubsub = testutil.NewInMemoryPubSub()
	s.impl = &eventPostProcessingService{
		ServiceParams:      ServiceParams{Logger: s.logger, Config: s.cfg},
		eventRepo:          s.store,
		processedEventRepo: nil,
		pubSub:             s.pubsub,
		backfillPubSub:     s.pubsub,
	}
	s.service = s.impl
}

func (s *EventPostProcessingSuite) TearDownTest() {
	if s.pubsub != nil {
		s.pubsub.ClearMessages()
	}
	s.store.Clear()
}

func (s *EventPostProcessingSuite) TestGenerateUniqueHash_CountUnique_UsesFieldValue() {
	m := &meter.Meter{Aggregation: meter.Aggregation{Type: types.AggregationCountUnique, Field: "endpoint"}}
	evt1 := events.NewEvent(
		"api_request",
		types.GetTenantID(s.ctx),
		"cust-1",
		map[string]interface{}{"endpoint": "/users"},
		time.Now().UTC(),
		"evt-same",
		"",
		"test",
		types.GetEnvironmentID(s.ctx),
	)
	evt2 := events.NewEvent(
		"api_request",
		types.GetTenantID(s.ctx),
		"cust-1",
		map[string]interface{}{"endpoint": "/orders"},
		time.Now().UTC(),
		"evt-same",
		"",
		"test",
		types.GetEnvironmentID(s.ctx),
	)

	h1 := s.impl.generateUniqueHash(evt1, m)
	h2 := s.impl.generateUniqueHash(evt2, m)
	s.NotEqual(h1, h2, "hash should incorporate unique field value for count_unique")

	// same field value should produce same hash
	evt3 := events.NewEvent(
		"api_request",
		types.GetTenantID(s.ctx),
		"cust-1",
		map[string]interface{}{"endpoint": "/users"},
		time.Now().UTC(),
		"evt-same",
		"",
		"test",
		types.GetEnvironmentID(s.ctx),
	)
	h3 := s.impl.generateUniqueHash(evt3, m)
	s.Equal(h1, h3)
}

func (s *EventPostProcessingSuite) TestGenerateUniqueHash_FieldMissing_IgnoresProperty() {
	m := &meter.Meter{Aggregation: meter.Aggregation{Type: types.AggregationCountUnique, Field: "missing"}}
	evtA := events.NewEvent(
		"login",
		types.GetTenantID(s.ctx),
		"cust-2",
		map[string]interface{}{"present": true},
		time.Now().UTC(),
		"evt-const",
		"",
		"test",
		types.GetEnvironmentID(s.ctx),
	)
	evtB := events.NewEvent(
		"login",
		types.GetTenantID(s.ctx),
		"cust-2",
		map[string]interface{}{"present": false},
		time.Now().UTC(),
		"evt-const",
		"",
		"test",
		types.GetEnvironmentID(s.ctx),
	)

	s.Equal(s.impl.generateUniqueHash(evtA, m), s.impl.generateUniqueHash(evtB, m), "missing field should not affect hash")
}

func (s *EventPostProcessingSuite) TestGenerateUniqueHash_Count_IgnoresField() {
	m := &meter.Meter{Aggregation: meter.Aggregation{Type: types.AggregationCount}}
	evtA := events.NewEvent(
		"download",
		types.GetTenantID(s.ctx),
		"cust-3",
		map[string]interface{}{"size": 10},
		time.Now().UTC(),
		"evt-const",
		"",
		"test",
		types.GetEnvironmentID(s.ctx),
	)
	evtB := events.NewEvent(
		"download",
		types.GetTenantID(s.ctx),
		"cust-3",
		map[string]interface{}{"size": 999},
		time.Now().UTC(),
		"evt-const",
		"",
		"test",
		types.GetEnvironmentID(s.ctx),
	)

	s.Equal(s.impl.generateUniqueHash(evtA, m), s.impl.generateUniqueHash(evtB, m), "count aggregation should ignore field values")
}

func (s *EventPostProcessingSuite) TestGenerateUniqueHash_CountUnique_MapOrderStable() {
	m := &meter.Meter{Aggregation: meter.Aggregation{Type: types.AggregationCountUnique, Field: "payload"}}
	props1 := map[string]interface{}{"a": 1, "b": 2}
	props2 := map[string]interface{}{"b": 2, "a": 1}
	evt1 := events.NewEvent("map_evt", types.GetTenantID(s.ctx), "cust", map[string]interface{}{"payload": props1}, time.Now().UTC(), "evt-id", "", "test", types.GetEnvironmentID(s.ctx))
	evt2 := events.NewEvent("map_evt", types.GetTenantID(s.ctx), "cust", map[string]interface{}{"payload": props2}, time.Now().UTC(), "evt-id", "", "test", types.GetEnvironmentID(s.ctx))
	h1 := s.impl.generateUniqueHash(evt1, m)
	h2 := s.impl.generateUniqueHash(evt2, m)
	s.Equal(h1, h2)
}

func (s *EventPostProcessingSuite) TestGenerateUniqueHash_CountUnique_ArrayStableAndDistinct() {
	m := &meter.Meter{Aggregation: meter.Aggregation{Type: types.AggregationCountUnique, Field: "list"}}
	evtA := events.NewEvent("arr_evt", types.GetTenantID(s.ctx), "cust", map[string]interface{}{"list": []int{1, 2}}, time.Now().UTC(), "evt-id", "", "test", types.GetEnvironmentID(s.ctx))
	evtB := events.NewEvent("arr_evt", types.GetTenantID(s.ctx), "cust", map[string]interface{}{"list": []int{1, 2}}, time.Now().UTC(), "evt-id", "", "test", types.GetEnvironmentID(s.ctx))
	evtC := events.NewEvent("arr_evt", types.GetTenantID(s.ctx), "cust", map[string]interface{}{"list": []int{2, 1}}, time.Now().UTC(), "evt-id", "", "test", types.GetEnvironmentID(s.ctx))
	s.Equal(s.impl.generateUniqueHash(evtA, m), s.impl.generateUniqueHash(evtB, m), "same array produces same hash")
	s.NotEqual(s.impl.generateUniqueHash(evtA, m), s.impl.generateUniqueHash(evtC, m), "different array order/content produces different hash")
}

func (s *EventPostProcessingSuite) TestGenerateUniqueHash_CountUnique_BoolAndNil() {
	m := &meter.Meter{Aggregation: meter.Aggregation{Type: types.AggregationCountUnique, Field: "flag"}}
	evtTrue := events.NewEvent("bool_evt", types.GetTenantID(s.ctx), "cust", map[string]interface{}{"flag": true}, time.Now().UTC(), "evt-id", "", "test", types.GetEnvironmentID(s.ctx))
	evtFalse := events.NewEvent("bool_evt", types.GetTenantID(s.ctx), "cust", map[string]interface{}{"flag": false}, time.Now().UTC(), "evt-id", "", "test", types.GetEnvironmentID(s.ctx))
	evtNil := events.NewEvent("bool_evt", types.GetTenantID(s.ctx), "cust", map[string]interface{}{"flag": nil}, time.Now().UTC(), "evt-id", "", "test", types.GetEnvironmentID(s.ctx))
	s.NotEqual(s.impl.generateUniqueHash(evtTrue, m), s.impl.generateUniqueHash(evtFalse, m), "true vs false should differ")
	// nil should produce deterministic string "null" via stableString(JSON), so compare against itself
	s.Equal(s.impl.generateUniqueHash(evtNil, m), s.impl.generateUniqueHash(evtNil, m))
}

func (s *EventPostProcessingSuite) TestExtractQuantityFromEvent_Sum_Types() {
	m := &meter.Meter{Aggregation: meter.Aggregation{Type: types.AggregationSum, Field: "value"}}
	cases := []struct {
		props    map[string]interface{}
		wantDec string
		wantStr string
	}{
		{map[string]interface{}{"value": float64(1.5)}, "1.5", "1.500000"},
		{map[string]interface{}{"value": float32(2.25)}, "2.25", "2.250000"},
		{map[string]interface{}{"value": int(2)}, "2", "2"},
		{map[string]interface{}{"value": uint(3)}, "3", "3"},
		{map[string]interface{}{"value": int64(4)}, "4", "4"},
		{map[string]interface{}{"value": int32(7)}, "7", "7"},
		{map[string]interface{}{"value": json.Number("5.75")}, "5.75", "5.75"},
		{map[string]interface{}{"value": "6.5"}, "6.5", "6.5"},
	}

	for _, tc := range cases {
		evt := events.NewEvent("sum_evt", types.GetTenantID(s.ctx), "cust-x", tc.props, time.Now().UTC(), "evt-q", "", "test", types.GetEnvironmentID(s.ctx))
		dec, str := s.impl.extractQuantityFromEvent(evt, m)
		require.Equal(s.T(), tc.wantDec, dec.String())
		require.Equal(s.T(), tc.wantStr, str)
	}

	// missing field
	evtMissing := events.NewEvent("sum_evt", types.GetTenantID(s.ctx), "cust-x", map[string]interface{}{}, time.Now().UTC(), "evt-q", "", "test", types.GetEnvironmentID(s.ctx))
	dec, str := s.impl.extractQuantityFromEvent(evtMissing, m)
	s.True(dec.IsZero())
	s.Equal("", str)

	// invalid string returns zero decimal and original string
	evtInvalid := events.NewEvent("sum_evt", types.GetTenantID(s.ctx), "cust-x", map[string]interface{}{"value": "abc"}, time.Now().UTC(), "evt-q", "", "test", types.GetEnvironmentID(s.ctx))
	dec, str = s.impl.extractQuantityFromEvent(evtInvalid, m)
	s.True(dec.IsZero())
	s.Equal("abc", str)
}

func (s *EventPostProcessingSuite) TestExtractQuantityFromEvent_Sum_EdgeCases() {
	m := &meter.Meter{Aggregation: meter.Aggregation{Type: types.AggregationSum, Field: "value"}}
	// unknown type (slice)
	evtSlice := events.NewEvent("sum_evt", types.GetTenantID(s.ctx), "cust-x", map[string]interface{}{"value": []int{1, 2}}, time.Now().UTC(), "evt-q", "", "test", types.GetEnvironmentID(s.ctx))
	dec, str := s.impl.extractQuantityFromEvent(evtSlice, m)
	s.True(dec.IsZero())
	s.Equal("[1 2]", str)
	// uint64 max
	max := uint64(math.MaxUint64)
	evtMax := events.NewEvent("sum_evt", types.GetTenantID(s.ctx), "cust-x", map[string]interface{}{"value": max}, time.Now().UTC(), "evt-q", "", "test", types.GetEnvironmentID(s.ctx))
	dec, str = s.impl.extractQuantityFromEvent(evtMax, m)
	s.Equal("18446744073709551615", str)
	s.Equal("18446744073709551615", dec.String())
	// json.Number invalid
	evtBadJSONNum := events.NewEvent("sum_evt", types.GetTenantID(s.ctx), "cust-x", map[string]interface{}{"value": json.Number("notnum")}, time.Now().UTC(), "evt-q", "", "test", types.GetEnvironmentID(s.ctx))
	dec, str = s.impl.extractQuantityFromEvent(evtBadJSONNum, m)
	s.True(dec.IsZero())
	s.Equal("notnum", str)
	// empty field name
	mEmpty := &meter.Meter{Aggregation: meter.Aggregation{Type: types.AggregationSum, Field: ""}}
	dec, str = s.impl.extractQuantityFromEvent(evtMax, mEmpty)
	s.True(dec.IsZero())
	s.Equal("", str)
	// unsupported aggregation type
	mUnsupported := &meter.Meter{Aggregation: meter.Aggregation{Type: types.AggregationMax, Field: "value"}}
	dec, str = s.impl.extractQuantityFromEvent(evtMax, mUnsupported)
	s.True(dec.IsZero())
	s.Equal("", str)
}

func (s *EventPostProcessingSuite) TestExtractQuantityFromEvent_Count() {
	m := &meter.Meter{Aggregation: meter.Aggregation{Type: types.AggregationCount}}
	evt := events.NewEvent("count_evt", types.GetTenantID(s.ctx), "cust-x", map[string]interface{}{}, time.Now().UTC(), "evt-q", "", "test", types.GetEnvironmentID(s.ctx))
	dec, str := s.impl.extractQuantityFromEvent(evt, m)
	s.Equal("1", dec.String())
	s.Equal("", str)
}

func (s *EventPostProcessingSuite) TestStableString_Formats() {
	// arrays are marshaled to JSON with commas
	s.Equal("[1,2]", stableString([]int{1, 2}))

	// nil becomes JSON null
	s.Equal("null", stableString(nil))

	// booleans marshal to JSON booleans
	s.Equal("true", stableString(true))
	s.Equal("false", stableString(false))

	// strings and json.Number are used as-is
	s.Equal("abc", stableString("abc"))
	s.Equal("123.45", stableString(json.Number("123.45")))

	// numeric types use decimal-backed canonical strings
	s.Equal("1.25", stableString(float64(1.25)))
	s.Equal("1.25", stableString(float32(1.25)))
	s.Equal("42", stableString(int32(42)))
	s.Equal("42", stableString(int64(42)))
	s.Equal("42", stableString(uint64(42)))

	// maps produce stable JSON; order independence is preserved
	m1 := map[string]int{"a": 1, "b": 2}
	m2 := map[string]int{"b": 2, "a": 1}
	s.Equal(stableString(m1), stableString(m2))

	// unsupported types fall back to fmt.Sprintf
	s.Equal("(1+2i)", stableString(complex64(1+2i)))
}

func (s *EventPostProcessingSuite) TestPublishEvent_TopicAndMetadata() {
	evt := events.NewEvent(
		"file_upload",
		types.GetTenantID(s.ctx),
		"cust-42",
		map[string]interface{}{"size": 128},
		time.Now().UTC(),
		"evt-abc",
		"",
		"test",
		types.GetEnvironmentID(s.ctx),
	)

	// regular publish
	require.NoError(s.T(), s.impl.PublishEvent(s.ctx, evt, false))
	msgs := s.pubsub.GetMessages(s.cfg.EventPostProcessing.Topic)
	require.Len(s.T(), msgs, 1)
	var got events.Event
	require.NoError(s.T(), json.Unmarshal(msgs[0].Payload, &got))
	s.Equal(evt.ID, got.ID)
	s.Equal(types.GetTenantID(s.ctx), msgs[0].Metadata.Get("tenant_id"))
	s.Equal(types.GetEnvironmentID(s.ctx), msgs[0].Metadata.Get("environment_id"))
	s.Equal(types.GetTenantID(s.ctx)+":"+evt.ExternalCustomerID, msgs[0].Metadata.Get("partition_key"))

	// backfill publish
	require.NoError(s.T(), s.impl.PublishEvent(s.ctx, evt, true))
	msgsBackfill := s.pubsub.GetMessages(s.cfg.EventPostProcessing.TopicBackfill)
	require.Len(s.T(), msgsBackfill, 1)
}

func (s *EventPostProcessingSuite) TestPublishEvent_ValidationAndPartitionKey() {
	// nil event
	s.Error(s.impl.PublishEvent(s.ctx, nil, false))
	// missing required fields
	bad := &events.Event{}
	s.Error(s.impl.PublishEvent(s.ctx, bad, false))
	// empty external customer id => partition key = tenantID
	evt := events.NewEvent("upload", types.GetTenantID(s.ctx), "", map[string]interface{}{}, time.Now().UTC(), "evt-x", "", "test", types.GetEnvironmentID(s.ctx))
	require.NoError(s.T(), s.impl.PublishEvent(s.ctx, evt, false))
	msgs := s.pubsub.GetMessages(s.cfg.EventPostProcessing.Topic)
	require.Len(s.T(), msgs, 1)
	s.Equal(types.GetTenantID(s.ctx), msgs[0].Metadata.Get("partition_key"))
}

func (s *EventPostProcessingSuite) TestReprocessEvents_PublishingAndConcurrency() {
	// prepare events in store
	tenantID := types.GetTenantID(s.ctx)
	envID := types.GetEnvironmentID(s.ctx)
	cust := "cust-rep"
	name := "event-rep"
	var batch []*events.Event
	start := time.Now().Add(-1 * time.Hour).UTC()
	for i := 0; i < 20; i++ {
		id := "evt-" + time.Now().Add(time.Duration(i)*time.Millisecond).Format("150405.000000")
		batch = append(batch, events.NewEvent(name, tenantID, cust, map[string]interface{}{}, start.Add(time.Duration(i)*time.Minute), id, "", "test", envID))
	}
	require.NoError(s.T(), s.store.BulkInsertEvents(context.Background(), batch))

	// use recording pubsub to track concurrency and messages
	rec := NewRecordingPubSub()
	s.impl.backfillPubSub = rec
	// set low worker limit to test bounded concurrency
	s.cfg.EventPostProcessing.RateLimitBackfill = 2

	params := &events.ReprocessEventsParams{
		ExternalCustomerID: cust,
		EventName:          name,
		StartTime:          start.Add(-1 * time.Minute),
		EndTime:            start.Add(40 * time.Minute),
		BatchSize:          10,
	}

	require.NoError(s.T(), s.impl.ReprocessEvents(s.ctx, params))
	// verify messages published to backfill topic equals total events
	require.Equal(s.T(), 20, len(rec.messages[s.cfg.EventPostProcessing.TopicBackfill]))
	// verify bounded concurrency respected
	s.Equal(2, rec.maxConcurrent)
}

func (s *EventPostProcessingSuite) TestReprocessEvents_NoEvents() {
	rec := NewRecordingPubSub()
	s.impl.backfillPubSub = rec
	params := &events.ReprocessEventsParams{
		ExternalCustomerID: "cust-none",
		EventName:          "event-none",
		StartTime:          time.Now().Add(-1 * time.Hour).UTC(),
		EndTime:            time.Now().UTC(),
		BatchSize:          10,
	}
	require.NoError(s.T(), s.impl.ReprocessEvents(s.ctx, params))
	require.Nil(s.T(), rec.messages[s.cfg.EventPostProcessing.TopicBackfill])
}