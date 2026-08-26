package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// ---------------------------------------------------------------------------
// Suite
// ---------------------------------------------------------------------------

type RawEventsReprocessingSuite struct {
	testutil.BaseServiceTestSuite
	svc     *rawEventsReprocessingService
	pubSub  *testutil.InMemoryPubSub
	rawRepo *fakeRawEventRepo
	topic   string
}

func TestRawEventsReprocessingService(t *testing.T) {
	suite.Run(t, new(RawEventsReprocessingSuite))
}

func (s *RawEventsReprocessingSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()

	s.pubSub = testutil.NewInMemoryPubSub()
	s.rawRepo = &fakeRawEventRepo{}
	s.topic = "prod_events_v4"

	params := ServiceParams{
		Logger:       s.GetLogger(),
		Config:       s.GetConfig(),
		DB:           s.GetDB(),
		RawEventRepo: s.rawRepo,
	}
	s.GetConfig().RawEventsReprocessing.OutputTopic = s.topic

	s.svc = &rawEventsReprocessingService{
		ServiceParams: params,
		rawEventRepo:  s.rawRepo,
		pubSub:        s.pubSub,
	}
}

// ---------------------------------------------------------------------------
// Fake RawEventRepository — returns one batch of crafted raw events, then empty.
// ---------------------------------------------------------------------------

type fakeRawEventRepo struct {
	batch  []*events.RawEvent
	served bool
}

func (f *fakeRawEventRepo) FindRawEvents(ctx context.Context, params *events.FindRawEventsParams) ([]*events.RawEvent, error) {
	if f.served {
		return nil, nil
	}
	f.served = true
	return f.batch, nil
}

func (f *fakeRawEventRepo) FindUnprocessedRawEvents(ctx context.Context, params *events.FindRawEventsParams) ([]*events.RawEvent, *events.KeysetCursor, error) {
	if f.served {
		return nil, nil, nil
	}
	f.served = true
	return f.batch, nil, nil
}

// bentoPayload builds a minimal valid Bento payload whose createdAt (which the
// transformer maps to Event.Timestamp) is `createdAt`.
func bentoPayload(eventID, createdAt string) string {
	return `{"orgId":"` + testTenantID + `","id":"` + eventID +
		`","methodName":"CHAT_COMPLETION","providerName":"openai","createdAt":"` + createdAt + `"}`
}

// rawEvent crafts a RawEvent whose DB Timestamp column is `ts`, wrapping a
// payload whose createdAt is `payloadCreatedAt` (drives the transformed ts).
func (s *RawEventsReprocessingSuite) rawEvent(id string, ts time.Time, payloadCreatedAt string) *events.RawEvent {
	return &events.RawEvent{
		ID:                 id,
		TenantID:           testTenantID,
		EnvironmentID:      testEnvironmentID,
		ExternalCustomerID: "cust_1",
		EventName:          "chat",
		Payload:            bentoPayload(id, payloadCreatedAt),
		Timestamp:          ts,
	}
}

// ---------------------------------------------------------------------------
// R1 guard tests
// ---------------------------------------------------------------------------

// Healthy reprocess: RawEvent.Timestamp matches the payload createdAt that the
// transformer produces → guard is a no-op → event is published normally.
func (s *RawEventsReprocessingSuite) TestReprocess_TimestampUnchanged_Publishes() {
	ts := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	s.rawRepo.batch = []*events.RawEvent{
		s.rawEvent("evt_healthy", ts, "2024-01-15T10:00:00Z"),
	}

	res, err := s.svc.ReprocessRawEvents(context.Background(), &events.ReprocessRawEventsParams{
		BatchSize: 100,
	})
	require.NoError(s.T(), err)

	require.Equal(s.T(), 1, res.TotalEventsPublished, "healthy event must be published")
	require.Equal(s.T(), 0, res.TotalR1Violations, "no R1 violation expected")
	require.Len(s.T(), s.pubSub.GetMessages(s.topic), 1, "exactly one message published")
}

// R1 violation: same event_id, but reprocessing produces a DIFFERENT timestamp
// than the original RawEvent.Timestamp → skip publish + count violation.
func (s *RawEventsReprocessingSuite) TestReprocess_TimestampChanged_SkippedAndCounted() {
	origTs := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	s.rawRepo.batch = []*events.RawEvent{
		// original ts is 10:00, but the payload createdAt (transformed ts) is 11:00
		s.rawEvent("evt_violation", origTs, "2024-01-15T11:00:00Z"),
	}

	res, err := s.svc.ReprocessRawEvents(context.Background(), &events.ReprocessRawEventsParams{
		BatchSize: 100,
	})
	require.NoError(s.T(), err, "batch must not fail — violation is per-event skip")

	require.Equal(s.T(), 1, res.TotalR1Violations, "one R1 violation expected")
	require.Equal(s.T(), 0, res.TotalEventsPublished, "violating event must NOT be published")
	require.Len(s.T(), s.pubSub.GetMessages(s.topic), 0, "nothing published")
}
