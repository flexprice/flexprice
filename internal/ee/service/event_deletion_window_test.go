package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/suite"
)

type EventDeletionWindowSuite struct {
	testutil.BaseServiceTestSuite
	requestRepo *testutil.InMemoryEventDeletionRequestStore
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
		StartedAt:          s.periodStart,
	}}))
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
