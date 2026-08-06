package testutil

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/usagerecord"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// InMemoryUsageRecordStore is an in-memory implementation of usagerecord.Repository for tests.
type InMemoryUsageRecordStore struct {
	store *InMemoryStore[*usagerecord.UsageRecord]
}

func NewInMemoryUsageRecordStore() *InMemoryUsageRecordStore {
	return &InMemoryUsageRecordStore{
		store: NewInMemoryStore[*usagerecord.UsageRecord](),
	}
}

func (s *InMemoryUsageRecordStore) Create(ctx context.Context, rec *usagerecord.UsageRecord) error {
	if rec.EnvironmentID == "" {
		rec.EnvironmentID = types.GetEnvironmentID(ctx)
	}
	if rec.TenantID == "" {
		rec.TenantID = types.GetTenantID(ctx)
	}
	if rec.Syncs == nil {
		rec.Syncs = map[string]types.UsageRecordSyncEntry{}
	}

	// Enforces the table's unique key on (tenant, environment, subscription, period_start,
	// period_end), so callers relying on the already-exists error behave as they do against Postgres.
	exists, _ := s.ExistsForPeriod(ctx, rec.SubscriptionID, rec.PeriodStart, rec.PeriodEnd)
	if exists {
		return ierr.NewError("usage record already exists for this subscription and period").
			WithReportableDetails(map[string]any{
				"subscription_id": rec.SubscriptionID,
				"period_start":    rec.PeriodStart,
				"period_end":      rec.PeriodEnd,
			}).
			Mark(ierr.ErrAlreadyExists)
	}

	return s.store.Create(ctx, rec.ID, copyUsageRecord(rec))
}

func (s *InMemoryUsageRecordStore) ExistsForPeriod(ctx context.Context, subscriptionID string, periodStart, periodEnd time.Time) (bool, error) {
	filterFn := func(ctx context.Context, r *usagerecord.UsageRecord, _ interface{}) bool {
		return r.SubscriptionID == subscriptionID &&
			r.PeriodStart.Equal(periodStart) &&
			r.PeriodEnd.Equal(periodEnd) &&
			CheckTenantFilter(ctx, r.TenantID) &&
			CheckEnvironmentFilter(ctx, r.EnvironmentID) &&
			r.Status == types.StatusPublished
	}
	items, err := s.store.List(ctx, nil, filterFn, nil)
	if err != nil {
		return false, err
	}
	return len(items) > 0, nil
}

// unsyncedSubmissionWindow bounds unsynced rows to those a marketplace can still accept, as the
// repository does.
const unsyncedSubmissionWindow = 24 * time.Hour

func (s *InMemoryUsageRecordStore) ListUnsynced(ctx context.Context, tenantID, environmentID string) ([]*usagerecord.UsageRecord, error) {
	cutoff := time.Now().UTC().Add(-unsyncedSubmissionWindow)
	filterFn := func(_ context.Context, r *usagerecord.UsageRecord, _ interface{}) bool {
		return r.TenantID == tenantID &&
			r.EnvironmentID == environmentID &&
			!r.Synced &&
			r.Status == types.StatusPublished &&
			!r.PeriodEnd.Before(cutoff)
	}
	items, err := s.store.List(ctx, nil, filterFn, nil)
	if err != nil {
		return nil, err
	}
	result := make([]*usagerecord.UsageRecord, len(items))
	for i, item := range items {
		result[i] = copyUsageRecord(item)
	}
	return result, nil
}

// List returns the usage records matching filter, reproducing the repository's semantics: always
// scoped to the caller's tenant and environment, defaulting to published rows, sorted and paginated
// per the embedded query filter.
func (s *InMemoryUsageRecordStore) List(ctx context.Context, filter *types.UsageRecordFilter) ([]*usagerecord.UsageRecord, error) {
	if filter == nil {
		filter = types.NewUsageRecordFilter()
	}

	filterFn := func(ctx context.Context, r *usagerecord.UsageRecord, _ interface{}) bool {
		if !CheckTenantFilter(ctx, r.TenantID) || !CheckEnvironmentFilter(ctx, r.EnvironmentID) {
			return false
		}
		// An unset status means published rather than any status, as the repository does. Nothing
		// archives a usage record, so every caller wants published rows.
		status := filter.GetStatus()
		if status == "" {
			status = string(types.StatusPublished)
		}
		if string(r.Status) != status {
			return false
		}
		if filter.SubscriptionID != "" && r.SubscriptionID != filter.SubscriptionID {
			return false
		}
		if filter.CustomerID != "" && r.CustomerID != filter.CustomerID {
			return false
		}
		if filter.CustomerExternalID != "" && r.CustomerExternalID != filter.CustomerExternalID {
			return false
		}
		if filter.PlanID != "" && r.PlanID != filter.PlanID {
			return false
		}
		if filter.Currency != "" && r.Currency != filter.Currency {
			return false
		}
		if filter.PeriodStart != nil && !r.PeriodStart.Equal(*filter.PeriodStart) {
			return false
		}
		if filter.PeriodEnd != nil && !r.PeriodEnd.Equal(*filter.PeriodEnd) {
			return false
		}
		if filter.Synced != nil && r.Synced != *filter.Synced {
			return false
		}
		// Bounds created_at, as the repository does.
		if filter.TimeRangeFilter != nil {
			if filter.StartTime != nil && r.CreatedAt.Before(*filter.StartTime) {
				return false
			}
			if filter.EndTime != nil && r.CreatedAt.After(*filter.EndTime) {
				return false
			}
		}
		return matchesUsageRecordDSLFilters(r, filter.Filters)
	}

	sortField, sortAsc := filter.GetSort(), filter.GetOrder() == types.OrderAsc
	if len(filter.Sort) > 0 {
		// A sort condition overrides the query filter's, since it is applied last.
		sortField = filter.Sort[0].Field
		sortAsc = filter.Sort[0].Direction == types.SortDirectionAsc
	}

	var sortFn func(i, j *usagerecord.UsageRecord) bool
	switch sortField {
	case "period_end":
		sortFn = func(i, j *usagerecord.UsageRecord) bool {
			if sortAsc {
				return i.PeriodEnd.Before(j.PeriodEnd)
			}
			return i.PeriodEnd.After(j.PeriodEnd)
		}
	case "period_start":
		sortFn = func(i, j *usagerecord.UsageRecord) bool {
			if sortAsc {
				return i.PeriodStart.Before(j.PeriodStart)
			}
			return i.PeriodStart.After(j.PeriodStart)
		}
	case "created_at":
		sortFn = func(i, j *usagerecord.UsageRecord) bool {
			if sortAsc {
				return i.CreatedAt.Before(j.CreatedAt)
			}
			return i.CreatedAt.After(j.CreatedAt)
		}
	}

	// Passing the filter through applies offset as well as limit. Slicing by limit here instead would
	// ignore a non-zero offset and return the first page where the database returns a later one.
	items, err := s.store.List(ctx, filter, filterFn, sortFn)
	if err != nil {
		return nil, err
	}

	result := make([]*usagerecord.UsageRecord, len(items))
	for i, item := range items {
		result[i] = copyUsageRecord(item)
	}
	return result, nil
}

func (s *InMemoryUsageRecordStore) MarkSynced(ctx context.Context, id string, syncs map[string]types.UsageRecordSyncEntry, synced bool) error {
	existing, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}

	updated := copyUsageRecord(existing)
	copied := make(map[string]types.UsageRecordSyncEntry, len(syncs))
	for k, v := range syncs {
		copied[k] = v
	}
	updated.Syncs = copied
	updated.Synced = synced
	updated.UpdatedAt = time.Now().UTC()

	return s.store.Update(ctx, id, updated)
}

func (s *InMemoryUsageRecordStore) Clear() {
	s.store.Clear()
}

func copyUsageRecord(r *usagerecord.UsageRecord) *usagerecord.UsageRecord {
	if r == nil {
		return nil
	}
	syncs := make(map[string]types.UsageRecordSyncEntry, len(r.Syncs))
	for k, v := range r.Syncs {
		syncs[k] = v
	}
	return &usagerecord.UsageRecord{
		ID:                 r.ID,
		CustomerID:         r.CustomerID,
		CustomerExternalID: r.CustomerExternalID,
		SubscriptionID:     r.SubscriptionID,
		PlanID:             r.PlanID,
		Quantity:           r.Quantity,
		Amount:             r.Amount,
		Currency:           r.Currency,
		PeriodStart:        r.PeriodStart,
		PeriodEnd:          r.PeriodEnd,
		Synced:             r.Synced,
		Syncs:              syncs,
		EnvironmentID:      r.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  r.TenantID,
			Status:    r.Status,
			CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt,
			CreatedBy: r.CreatedBy,
			UpdatedBy: r.UpdatedBy,
		},
	}
}

// matchesUsageRecordDSLFilters evaluates the generic DSL conditions the real repository hands to
// dsl.ApplyFilters. Only the date and string comparisons usage_records actually needs are supported;
// an unrecognised field or operator fails the match loudly rather than silently passing, so a test
// exercising an unsupported condition cannot quietly get wrong results.
func matchesUsageRecordDSLFilters(r *usagerecord.UsageRecord, conditions []*types.FilterCondition) bool {
	for _, c := range conditions {
		if c == nil || c.Field == nil || c.Operator == nil {
			continue
		}
		var field time.Time
		switch *c.Field {
		case "period_end":
			field = r.PeriodEnd
		case "period_start":
			field = r.PeriodStart
		case "created_at":
			field = r.CreatedAt
		default:
			return false // unsupported field — see doc comment
		}
		if c.Value == nil || c.Value.Date == nil {
			return false
		}
		want := *c.Value.Date
		switch *c.Operator {
		case types.GREATER_THAN_EQUAL:
			if field.Before(want) {
				return false
			}
		case types.GREATER_THAN, types.AFTER:
			if !field.After(want) {
				return false
			}
		case types.LESS_THAN, types.BEFORE:
			if !field.Before(want) {
				return false
			}
		case types.EQUAL:
			if !field.Equal(want) {
				return false
			}
		default:
			return false // unsupported operator — see doc comment
		}
	}
	return true
}
