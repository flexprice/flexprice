package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/suite"
)

type EventDeletionWindowSuite struct {
	testutil.BaseServiceTestSuite
	requestRepo *testutil.InMemoryEventDeletionRequestStore
	eventRepo   *testutil.InMemoryEventStore
	publisher   *testutil.InMemoryPublisherService
	periodStart time.Time
	periodEnd   time.Time
}

func TestEventDeletionWindow(t *testing.T) {
	suite.Run(t, new(EventDeletionWindowSuite))
}

func (s *EventDeletionWindowSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.periodStart = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	s.periodEnd = time.Date(2026, 3, 28, 0, 0, 0, 0, time.UTC)
	s.requestRepo = testutil.NewInMemoryEventDeletionRequestStore()
	s.eventRepo = testutil.NewInMemoryEventStore()
	s.publisher = testutil.NewInMemoryEventPublisher(s.eventRepo).(*testutil.InMemoryPublisherService)
}

func (s *EventDeletionWindowSuite) window(repo events.DeletionRequestRepository) EventDeletionWindowService {
	return NewEventDeletionWindowService(ServiceParams{
		Logger:                   s.GetLogger(),
		Config:                   s.GetConfig(),
		EventDeletionRequestRepo: repo,
	})
}

func (s *EventDeletionWindowSuite) seedDeleting(externalCustomerID string) {
	s.NoError(s.requestRepo.CreateBulk(s.GetContext(), []*events.DeletionRequest{{
		ID:                 "evtdel_" + externalCustomerID,
		RequestID:          "req_1",
		ExternalCustomerID: externalCustomerID,
		PeriodStart:        s.periodStart,
		PeriodEnd:          s.periodEnd,
		DeletionStatus:     types.EventDeletionStatusDeleting,
		StartedAt:          time.Now().UTC(),
	}}))
}

// A process killed mid-deletion leaves a row nothing will ever close. Past the
// cutoff the fence stops honouring it, so the customer can ingest again.
func (s *EventDeletionWindowSuite) TestIsSuppressed_FalseOnceTheDeletionIsAbandoned() {
	s.NoError(s.requestRepo.CreateBulk(s.GetContext(), []*events.DeletionRequest{{
		ID:                 "evtdel_abandoned",
		RequestID:          "req_abandoned",
		ExternalCustomerID: "ext_1",
		PeriodStart:        s.periodStart,
		PeriodEnd:          s.periodEnd,
		DeletionStatus:     types.EventDeletionStatusDeleting,
		StartedAt:          time.Now().UTC().Add(-2 * staleDeletionAge),
	}}))

	w := s.window(s.requestRepo)

	s.False(w.IsSuppressed(s.GetContext(), "ext_1", s.periodStart.AddDate(0, 0, 10)))
}

// A deletion that is merely slow is not abandoned. Unfencing one still running
// would let events land inside the period it is deleting.
func (s *EventDeletionWindowSuite) TestIsSuppressed_TrueWhileTheDeletionIsStillWithinItsDeadline() {
	s.NoError(s.requestRepo.CreateBulk(s.GetContext(), []*events.DeletionRequest{{
		ID:                 "evtdel_slow",
		RequestID:          "req_slow",
		ExternalCustomerID: "ext_1",
		PeriodStart:        s.periodStart,
		PeriodEnd:          s.periodEnd,
		DeletionStatus:     types.EventDeletionStatusDeleting,
		StartedAt:          time.Now().UTC().Add(-deletionRunDeadline),
	}}))

	w := s.window(s.requestRepo)

	s.True(w.IsSuppressed(s.GetContext(), "ext_1", s.periodStart.AddDate(0, 0, 10)),
		"the cutoff must sit past the run deadline, never before it")
}

func (s *EventDeletionWindowSuite) TestIsSuppressed() {
	s.seedDeleting("ext_1")
	w := s.window(s.requestRepo)
	ctx := s.GetContext()

	tests := []struct {
		name               string
		externalCustomerID string
		timestamp          time.Time
		want               bool
	}{
		{
			name:               "inside the period",
			externalCustomerID: "ext_1",
			timestamp:          s.periodStart.AddDate(0, 0, 10),
			want:               true,
		},
		{
			name:               "exactly at period_start is inside",
			externalCustomerID: "ext_1",
			timestamp:          s.periodStart,
			want:               true,
		},
		{
			// The period is half-open, so period_end itself is outside.
			name:               "exactly at period_end is outside",
			externalCustomerID: "ext_1",
			timestamp:          s.periodEnd,
			want:               false,
		},
		{
			name:               "before the period",
			externalCustomerID: "ext_1",
			timestamp:          s.periodStart.AddDate(0, 0, -1),
			want:               false,
		},
		{
			name:               "after the period",
			externalCustomerID: "ext_1",
			timestamp:          s.periodEnd.AddDate(0, 0, 1),
			want:               false,
		},
		{
			name:               "a customer with no deletion running",
			externalCustomerID: "ext_2",
			timestamp:          s.periodStart.AddDate(0, 0, 10),
			want:               false,
		},
		{
			name:               "no customer on the event",
			externalCustomerID: "",
			timestamp:          s.periodStart.AddDate(0, 0, 10),
			want:               false,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.Equal(tt.want, w.IsSuppressed(ctx, tt.externalCustomerID, tt.timestamp))
		})
	}
}

func (s *EventDeletionWindowSuite) TestIsSuppressed_FalseOnceTheDeletionIsTerminal() {
	s.seedDeleting("ext_1")
	ctx := s.GetContext()
	inside := s.periodStart.AddDate(0, 0, 10)

	w := s.window(s.requestRepo)
	s.True(w.IsSuppressed(ctx, "ext_1", inside))

	s.NoError(s.requestRepo.MarkTerminal(ctx, []string{"evtdel_ext_1"}, types.EventDeletionStatusCompleted, ""))

	// A fresh window reads the terminal row, so the hold is released.
	s.False(s.window(s.requestRepo).IsSuppressed(ctx, "ext_1", inside))
}

// The window is cached for suppressionCacheTTL, which is what keeps the check off
// the database on the per-event path.
func (s *EventDeletionWindowSuite) TestIsSuppressed_ServesFromCacheWithinTheInterval() {
	ctx := s.GetContext()
	inside := s.periodStart.AddDate(0, 0, 10)

	w := s.window(s.requestRepo)
	s.False(w.IsSuppressed(ctx, "ext_1", inside), "nothing in progress yet")

	s.seedDeleting("ext_1")

	s.False(w.IsSuppressed(ctx, "ext_1", inside),
		"the row landed after the load, so this instance still answers from its cached window")
	s.True(s.window(s.requestRepo).IsSuppressed(ctx, "ext_1", inside),
		"a fresh load sees it")
}

// A process that has never loaded successfully has nothing to fall back on, so
// it writes events rather than holding every one of them.
func (s *EventDeletionWindowSuite) TestIsSuppressed_FalseWhenTheFirstReadFails() {
	w := s.window(failingDeletionRequestRepo{})

	s.False(w.IsSuppressed(s.GetContext(), "ext_1", s.periodStart.AddDate(0, 0, 10)))
}

// Once a window has loaded, a failed reload must not blind the fence: the
// deletion is still running and still reporting success.
func (s *EventDeletionWindowSuite) TestIsSuppressed_KeepsTheLastWindowWhenAReloadFails() {
	s.seedDeleting("ext_1")
	ctx := s.GetContext()
	inside := s.periodStart.AddDate(0, 0, 10)

	repo := &flakyDeletionRequestRepo{inner: s.requestRepo}
	w := s.window(repo).(*eventDeletionWindowService)

	s.True(w.IsSuppressed(ctx, "ext_1", inside), "the first load succeeds")

	repo.fail = true
	for _, window := range w.windows {
		window.loadedAt = time.Now().Add(-2 * suppressionCacheTTL)
	}
	s.True(w.IsSuppressed(ctx, "ext_1", inside),
		"a failed reload must serve the last known window")
}

func (s *EventDeletionWindowSuite) TestIsSuppressed_FalseWithoutARepository() {
	w := NewEventDeletionWindowService(ServiceParams{
		Logger: s.GetLogger(),
		Config: s.GetConfig(),
	})

	s.False(w.IsSuppressed(s.GetContext(), "ext_1", s.periodStart.AddDate(0, 0, 10)))
}

// The window is keyed per tenant, so one tenant's deletion never suppresses
// another's ingestion.
func (s *EventDeletionWindowSuite) TestIsSuppressed_IsScopedPerTenant() {
	s.seedDeleting("ext_1")
	inside := s.periodStart.AddDate(0, 0, 10)

	w := s.window(s.requestRepo)
	s.True(w.IsSuppressed(s.GetContext(), "ext_1", inside))

	otherTenant := context.WithValue(context.Background(), types.CtxTenantID, "tenant_other")
	s.False(w.IsSuppressed(otherTenant, "ext_1", inside))
}

type failingDeletionRequestRepo struct{}

func (failingDeletionRequestRepo) CreateBulk(context.Context, []*events.DeletionRequest) error {
	return ierr.NewError("unavailable").Mark(ierr.ErrDatabase)
}

func (failingDeletionRequestRepo) ListAll(
	context.Context,
	*types.EventDeletionRequestFilter,
) ([]*events.DeletionRequest, error) {
	return nil, ierr.NewError("unavailable").Mark(ierr.ErrDatabase)
}

func (failingDeletionRequestRepo) MarkTerminal(
	context.Context,
	[]string,
	types.EventDeletionStatus,
	types.EventDeletionFailureReason,
) error {
	return ierr.NewError("unavailable").Mark(ierr.ErrDatabase)
}

// flakyDeletionRequestRepo serves the real store until fail is set.
type flakyDeletionRequestRepo struct {
	inner events.DeletionRequestRepository
	fail  bool
}

func (r *flakyDeletionRequestRepo) CreateBulk(ctx context.Context, requests []*events.DeletionRequest) error {
	return r.inner.CreateBulk(ctx, requests)
}

func (r *flakyDeletionRequestRepo) ListAll(
	ctx context.Context,
	filter *types.EventDeletionRequestFilter,
) ([]*events.DeletionRequest, error) {
	if r.fail {
		return nil, ierr.NewError("unavailable").Mark(ierr.ErrDatabase)
	}
	return r.inner.ListAll(ctx, filter)
}

func (r *flakyDeletionRequestRepo) MarkTerminal(
	ctx context.Context,
	ids []string,
	status types.EventDeletionStatus,
	reason types.EventDeletionFailureReason,
) error {
	return r.inner.MarkTerminal(ctx, ids, status, reason)
}

// ingestThroughTheRealWindow drives ingestion the way production does: the real
// event service, the real window, and the rows in the repository deciding the
// outcome. A fresh window per call, because one caches for an interval.
func (s *EventDeletionWindowSuite) ingestThroughTheRealWindow(eventID, externalCustomerID string, ts time.Time) error {
	eventSvc := NewEventService(s.eventRepo, nil, s.publisher, s.GetLogger(), s.GetConfig(), nil, s.window(s.requestRepo))
	return eventSvc.CreateEvent(s.GetContext(), &dto.IngestEventRequest{
		EventID:            eventID,
		ExternalCustomerID: externalCustomerID,
		EventName:          "api_request",
		Timestamp:          ts,
	})
}

func (s *EventDeletionWindowSuite) bulkIngestThroughTheRealWindow(batched bool, reqs ...*dto.IngestEventRequest) error {
	cfg := s.GetConfig()
	cfg.Event.BulkPublishEnabled = batched
	eventSvc := NewEventService(s.eventRepo, nil, s.publisher, s.GetLogger(), cfg, nil, s.window(s.requestRepo))
	return eventSvc.BulkCreateEvents(s.GetContext(), &dto.BulkIngestEventRequest{Events: reqs})
}

// A DELETING row in the repository is what refuses the event. Refused at
// ingestion so it never reaches Kafka, where a held message would block the
// partition for everyone sharing it.
func (s *EventDeletionWindowSuite) TestCreateEvent_RefusedWhileARowIsDeleting() {
	s.seedDeleting("cust_deleting")

	err := s.ingestThroughTheRealWindow("evt_in_period", "cust_deleting", s.periodStart.AddDate(0, 0, 10))

	s.Error(err)
	s.False(s.publisher.HasEvent("evt_in_period"), "nothing may reach Kafka for that customer and period")
}

// The block is the deleted period only, on both edges: period_start is inclusive
// and period_end is exclusive.
func (s *EventDeletionWindowSuite) TestCreateEvent_AcceptedOutsideTheDeletedPeriod() {
	s.seedDeleting("cust_deleting")

	s.Error(s.ingestThroughTheRealWindow("evt_at_start", "cust_deleting", s.periodStart))

	s.NoError(s.ingestThroughTheRealWindow("evt_at_end", "cust_deleting", s.periodEnd))
	s.True(s.publisher.HasEvent("evt_at_end"))

	s.NoError(s.ingestThroughTheRealWindow("evt_before", "cust_deleting", s.periodStart.Add(-time.Second)))
	s.True(s.publisher.HasEvent("evt_before"))
}

// The block is that one customer only. The rest of the tenant keeps ingesting
// inside the deleted period.
func (s *EventDeletionWindowSuite) TestCreateEvent_AcceptedForAnotherCustomerOfTheSameTenant() {
	s.seedDeleting("cust_deleting")

	s.NoError(s.ingestThroughTheRealWindow("evt_other", "cust_other", s.periodStart.AddDate(0, 0, 10)))
	s.True(s.publisher.HasEvent("evt_other"))
}

// The fence lifts as soon as the request reaches a terminal status, so a
// finished deletion never keeps a customer locked out.
func (s *EventDeletionWindowSuite) TestCreateEvent_AcceptedOnceTheDeletionCompletes() {
	s.seedDeleting("cust_deleting")
	inPeriod := s.periodStart.AddDate(0, 0, 10)
	s.Error(s.ingestThroughTheRealWindow("evt_while_deleting", "cust_deleting", inPeriod))

	s.NoError(s.requestRepo.MarkTerminal(s.GetContext(), []string{"evtdel_cust_deleting"},
		types.EventDeletionStatusCompleted, ""))

	s.NoError(s.ingestThroughTheRealWindow("evt_after_completion", "cust_deleting", inPeriod))
	s.True(s.publisher.HasEvent("evt_after_completion"))
}

// A row left DELETING by a dead process stops being honoured past the cutoff,
// so the customer is not fenced out forever.
func (s *EventDeletionWindowSuite) TestCreateEvent_AcceptedOnceTheDeletionIsAbandoned() {
	s.NoError(s.requestRepo.CreateBulk(s.GetContext(), []*events.DeletionRequest{{
		ID:                 "evtdel_abandoned",
		RequestID:          "req_abandoned",
		ExternalCustomerID: "cust_abandoned",
		PeriodStart:        s.periodStart,
		PeriodEnd:          s.periodEnd,
		DeletionStatus:     types.EventDeletionStatusDeleting,
		StartedAt:          time.Now().UTC().Add(-staleDeletionAge - time.Minute),
	}}))

	s.NoError(s.ingestThroughTheRealWindow("evt_past_cutoff", "cust_abandoned", s.periodStart.AddDate(0, 0, 10)))
	s.True(s.publisher.HasEvent("evt_past_cutoff"))
}

// Both bulk paths take the same fence, and one refused event fails the whole
// request rather than poisoning a batch the consumer would retry whole.
func (s *EventDeletionWindowSuite) TestBulkCreateEvents_RefusedWhileARowIsDeleting() {
	s.seedDeleting("cust_deleting")
	inPeriod := s.periodStart.AddDate(0, 0, 10)

	for _, batched := range []bool{false, true} {
		s.publisher.Clear()

		err := s.bulkIngestThroughTheRealWindow(batched,
			&dto.IngestEventRequest{EventID: "evt_bulk_other", ExternalCustomerID: "cust_other", EventName: "api_request", Timestamp: inPeriod},
			&dto.IngestEventRequest{EventID: "evt_bulk_deleting", ExternalCustomerID: "cust_deleting", EventName: "api_request", Timestamp: inPeriod},
		)

		s.Error(err, "batched=%v", batched)
		s.False(s.publisher.HasEvent("evt_bulk_deleting"), "batched=%v", batched)
	}
}
