package testutil

import (
	"context"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// SeededUsage is one meter_usage row the store starts with.
type SeededUsage struct {
	EventID            string
	MeterID            string
	ExternalCustomerID string
	Timestamp          time.Time
	QtyTotal           decimal.Decimal
	Properties         string
}

// SeededEvent is one events row the store starts with. Rows that also appear in
// SeededUsage are the metered set; rows that do not are the unmetered set.
type SeededEvent struct {
	EventID            string
	ExternalCustomerID string
	Timestamp          time.Time
	Properties         string
}

// DeletedEventLog is one audit row the store wrote.
type DeletedEventLog struct {
	EventID            string
	MeterID            string
	ExternalCustomerID string
	QtyTotal           decimal.Decimal
	Properties         string
	DeletedAt          time.Time
	DeletedBy          string
	RequestID          string
}

// InMemoryDeletedEventsLogStore implements events.DeletedEventsLogRepository for
// tests. It holds the two source tables, applies the same scope the SQL applies,
// and records what it deleted so a test can assert the audit rows.
type InMemoryDeletedEventsLogStore struct {
	mu sync.Mutex

	usage  []SeededUsage
	stored []SeededEvent
	logs   []DeletedEventLog

	// RecordCalls counts RecordDeletedEvents calls, so a test can prove the audit
	// write happened before the deletes.
	RecordCalls int
	// MeterUsageMutations and EventMutations count the mutations issued, which is
	// how a test asserts exactly one per table per request.
	MeterUsageMutations int
	EventMutations      int

	// FailOn names a method that should return an error, for failure-path tests.
	// One of "resolve", "record", "delete_meter_usage", "delete_events".
	FailOn string
	// FailWith is the error FailOn returns. Defaults to a plain database error.
	FailWith error
}

func NewInMemoryDeletedEventsLogStore() *InMemoryDeletedEventsLogStore {
	return &InMemoryDeletedEventsLogStore{}
}

func (s *InMemoryDeletedEventsLogStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.usage = nil
	s.stored = nil
	s.logs = nil
	s.RecordCalls = 0
	s.MeterUsageMutations = 0
	s.EventMutations = 0
	s.FailOn = ""
	s.FailWith = nil
}

// SeedUsage adds meter_usage rows. Each one also implies an events row unless the
// test seeded a conflicting one already.
func (s *InMemoryDeletedEventsLogStore) SeedUsage(rows ...SeededUsage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.usage = append(s.usage, rows...)
	for _, row := range rows {
		if lo.ContainsBy(s.stored, func(e SeededEvent) bool { return e.EventID == row.EventID }) {
			continue
		}
		s.stored = append(s.stored, SeededEvent{
			EventID:            row.EventID,
			ExternalCustomerID: row.ExternalCustomerID,
			Timestamp:          row.Timestamp,
			Properties:         row.Properties,
		})
	}
}

// SeedEvents adds events rows with no meter_usage behind them.
func (s *InMemoryDeletedEventsLogStore) SeedEvents(rows ...SeededEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stored = append(s.stored, rows...)
}

// Logs returns the audit rows written so far.
func (s *InMemoryDeletedEventsLogStore) Logs() []DeletedEventLog {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]DeletedEventLog(nil), s.logs...)
}

// RemainingUsage returns the meter_usage rows that survived.
func (s *InMemoryDeletedEventsLogStore) RemainingUsage() []SeededUsage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]SeededUsage(nil), s.usage...)
}

// RemainingEvents returns the events rows that survived.
func (s *InMemoryDeletedEventsLogStore) RemainingEvents() []SeededEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]SeededEvent(nil), s.stored...)
}

func (s *InMemoryDeletedEventsLogStore) failure(method string) error {
	if s.FailOn != method {
		return nil
	}
	if s.FailWith != nil {
		return s.FailWith
	}
	return types.ErrMutationTimeout
}

// inScope applies the same predicate the SQL builds: customer, half-open period,
// the request's own event ids, and the meters when the scope names any.
func inScope(scope *events.MeterUsageQueryParams, customerID string, ts time.Time, eventID, meterID string) bool {
	if len(scope.ExternalCustomerIDs) > 0 && !lo.Contains(scope.ExternalCustomerIDs, customerID) {
		return false
	}
	if !scope.StartTime.IsZero() && ts.Before(scope.StartTime) {
		return false
	}
	if !scope.EndTime.IsZero() && !ts.Before(scope.EndTime) {
		return false
	}
	if len(scope.IDs) > 0 && !lo.Contains(scope.IDs, eventID) {
		return false
	}
	if meterID != "" && len(scope.MeterIDs) > 0 && !lo.Contains(scope.MeterIDs, meterID) {
		return false
	}
	return true
}

func (s *InMemoryDeletedEventsLogStore) ResolveEventIDs(
	ctx context.Context,
	scope *events.MeterUsageQueryParams,
) (*events.DeletionTargets, error) {
	if err := s.failure("resolve"); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	targets := &events.DeletionTargets{Metered: make([]string, 0), Unmetered: make([]string, 0)}
	for _, row := range s.usage {
		if !inScope(scope, row.ExternalCustomerID, row.Timestamp, row.EventID, row.MeterID) {
			continue
		}
		if !lo.Contains(targets.Metered, row.EventID) {
			targets.Metered = append(targets.Metered, row.EventID)
		}
		targets.MeterUsageRows++
	}

	// A meter-narrowed scope never matches an event with no meter usage.
	if len(scope.MeterIDs) > 0 {
		return targets, nil
	}

	for _, row := range s.stored {
		if !inScope(scope, row.ExternalCustomerID, row.Timestamp, row.EventID, "") {
			continue
		}
		if lo.Contains(targets.Metered, row.EventID) || lo.Contains(targets.Unmetered, row.EventID) {
			continue
		}
		targets.Unmetered = append(targets.Unmetered, row.EventID)
	}

	return targets, nil
}

func (s *InMemoryDeletedEventsLogStore) FindStrandedMeters(
	ctx context.Context,
	scope *events.MeterUsageQueryParams,
	eventIDs []string,
) (*events.StrandedMeters, error) {
	if len(scope.MeterIDs) == 0 || len(eventIDs) == 0 {
		return &events.StrandedMeters{}, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	stranded := &events.StrandedMeters{}
	seen := make(map[string]struct{})
	for _, row := range s.usage {
		if !lo.Contains(eventIDs, row.EventID) || lo.Contains(scope.MeterIDs, row.MeterID) {
			continue
		}
		if !inScope(scope, row.ExternalCustomerID, row.Timestamp, row.EventID, "") {
			continue
		}
		if _, ok := seen[row.EventID]; !ok {
			seen[row.EventID] = struct{}{}
			stranded.EventCount++
		}
		if !lo.Contains(stranded.MetersInvolved, row.MeterID) {
			stranded.MetersInvolved = append(stranded.MetersInvolved, row.MeterID)
		}
	}
	return stranded, nil
}

func (s *InMemoryDeletedEventsLogStore) RecordDeletedEvents(
	ctx context.Context,
	scope *events.MeterUsageQueryParams,
	metered, unmetered []string,
	stamp events.DeletionStamp,
) error {
	if err := s.failure("record"); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.RecordCalls++

	properties := func(raw string) string {
		if stamp.SkipPayload {
			return ""
		}
		return raw
	}

	// One row per (event_id, meter_id).
	for _, row := range s.usage {
		if !lo.Contains(metered, row.EventID) {
			continue
		}
		if len(scope.MeterIDs) > 0 && !lo.Contains(scope.MeterIDs, row.MeterID) {
			continue
		}
		s.logs = append(s.logs, DeletedEventLog{
			EventID:            row.EventID,
			MeterID:            row.MeterID,
			ExternalCustomerID: row.ExternalCustomerID,
			QtyTotal:           row.QtyTotal,
			Properties:         properties(row.Properties),
			DeletedAt:          stamp.DeletedAt,
			DeletedBy:          stamp.DeletedBy,
			RequestID:          stamp.RequestID,
		})
	}

	// One row per unmetered event, meter_id '' and qty 0.
	for _, row := range s.stored {
		if !lo.Contains(unmetered, row.EventID) {
			continue
		}
		s.logs = append(s.logs, DeletedEventLog{
			EventID:            row.EventID,
			ExternalCustomerID: row.ExternalCustomerID,
			QtyTotal:           decimal.Zero,
			Properties:         properties(row.Properties),
			DeletedAt:          stamp.DeletedAt,
			DeletedBy:          stamp.DeletedBy,
			RequestID:          stamp.RequestID,
		})
	}

	return nil
}

func (s *InMemoryDeletedEventsLogStore) DeleteMeterUsage(
	ctx context.Context,
	scope *events.MeterUsageQueryParams,
	eventIDs []string,
) error {
	if err := s.failure("delete_meter_usage"); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.MeterUsageMutations++

	s.usage = lo.Filter(s.usage, func(row SeededUsage, _ int) bool {
		if !lo.Contains(eventIDs, row.EventID) {
			return true
		}
		return !inScope(scope, row.ExternalCustomerID, row.Timestamp, row.EventID, row.MeterID)
	})
	return nil
}

func (s *InMemoryDeletedEventsLogStore) DeleteEvents(
	ctx context.Context,
	scope *events.MeterUsageQueryParams,
	eventIDs []string,
) error {
	if err := s.failure("delete_events"); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.EventMutations++

	s.stored = lo.Filter(s.stored, func(row SeededEvent, _ int) bool {
		if !lo.Contains(eventIDs, row.EventID) {
			return true
		}
		return !inScope(scope, row.ExternalCustomerID, row.Timestamp, row.EventID, "")
	})
	return nil
}
