package service

import (
	"context"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
)

// EventDeletionWindowService answers whether an arriving event falls inside a
// deletion in progress. The DELETING rows are cached per tenant and environment,
// because a per-event Postgres read would bottleneck ingestion.
type EventDeletionWindowService interface {
	// IsSuppressed must be consulted before anything else the consumer does for
	// the event: a dropped event fires no webhook, publishes nothing, runs no
	// side effects and takes no dedup lock.
	IsSuppressed(ctx context.Context, externalCustomerID string, timestamp time.Time) bool
}

type eventDeletionWindowService struct {
	ServiceParams

	mu sync.RWMutex
	// windows is keyed by "tenant:environment"; the fence read is tenant-scoped
	// and one consumer process serves many tenants.
	windows map[string]*deletionWindow
}

// deletionWindow is one tenant's cached fence. At most one request per customer,
// which the unique partial index guarantees.
type deletionWindow struct {
	loadedAt   time.Time
	byCustomer map[string]*events.DeletionRequest
}

func NewEventDeletionWindowService(params ServiceParams) EventDeletionWindowService {
	return &eventDeletionWindowService{
		ServiceParams: params,
		windows:       make(map[string]*deletionWindow),
	}
}

func (s *eventDeletionWindowService) IsSuppressed(ctx context.Context, externalCustomerID string, timestamp time.Time) bool {
	if s.EventDeletionRequestRepo == nil || externalCustomerID == "" {
		return false
	}
	return s.currentWindow(ctx).byCustomer[externalCustomerID].IncludesTimestamp(timestamp)
}

// currentWindow returns the ctx tenant's window, reloading it when stale. No lock
// is held across the read: one instance serves every consumer goroutine for every
// tenant, so holding one would stall all ingestion behind a slow query.
func (s *eventDeletionWindowService) currentWindow(ctx context.Context) *deletionWindow {
	key := types.GetTenantID(ctx) + ":" + types.GetEnvironmentID(ctx)

	s.mu.RLock()
	window, ok := s.windows[key]
	s.mu.RUnlock()
	if ok && time.Since(window.loadedAt) < suppressionCacheTTL {
		return window
	}

	return s.loadWindow(ctx, key)
}

// loadWindow reads the deletions in progress and caches them.
func (s *eventDeletionWindowService) loadWindow(ctx context.Context, key string) *deletionWindow {
	// Through the writer: the run waits exactly one TTL before resolving, so a
	// window loaded from a lagging replica could miss the row and let in-period
	// events through for another interval.
	requests, err := s.EventDeletionRequestRepo.ListAll(types.WithForceWriter(ctx), inProgressDeletionFilter(nil))
	if err != nil {
		s.Logger.Error(ctx, "failed to reload the event deletion suppression window, serving the last known one",
			"error", err,
			"tenant_id", types.GetTenantID(ctx),
			"environment_id", types.GetEnvironmentID(ctx),
		)

		// Keep what the last good read found: a deletion running an interval ago is
		// almost certainly still running. loadedAt still advances, so a down database
		// is retried once per interval rather than once per event.
		s.mu.Lock()
		window := &deletionWindow{loadedAt: types.Now()}
		if previous, ok := s.windows[key]; ok {
			window.byCustomer = previous.byCustomer
		}
		s.windows[key] = window
		s.mu.Unlock()
		return window
	}

	byCustomer := make(map[string]*events.DeletionRequest, len(requests))
	for _, request := range requests {
		byCustomer[request.ExternalCustomerID] = request
	}
	window := &deletionWindow{loadedAt: types.Now(), byCustomer: byCustomer}

	s.mu.Lock()
	s.windows[key] = window
	s.mu.Unlock()

	if len(byCustomer) > 0 {
		s.Logger.Info(ctx, "event deletion suppression window loaded",
			"customer_count", len(byCustomer),
			"tenant_id", types.GetTenantID(ctx),
			"environment_id", types.GetEnvironmentID(ctx),
		)
	}
	return window
}
