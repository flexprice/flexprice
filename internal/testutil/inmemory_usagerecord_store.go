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

	// Mirrors the unique index on (tenant_id, environment_id, subscription_id, period_start,
	// period_end) the ent schema no longer has (dropped per the "every sub has its own agreement,
	// concurrent overlap is prevented by SCHEDULE_OVERLAP_POLICY_SKIP" call) — kept here anyway
	// since it costs nothing in-memory and exercises snapshotSubscription's ErrAlreadyExists path.
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

func (s *InMemoryUsageRecordStore) ListUnsynced(ctx context.Context, tenantID, environmentID string) ([]*usagerecord.UsageRecord, error) {
	filterFn := func(_ context.Context, r *usagerecord.UsageRecord, _ interface{}) bool {
		return r.TenantID == tenantID &&
			r.EnvironmentID == environmentID &&
			!r.AllProvidersSynced &&
			r.Status == types.StatusPublished
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

func (s *InMemoryUsageRecordStore) UpdateSyncResult(
	ctx context.Context,
	id string,
	marketplace usagerecord.Marketplace,
	entry usagerecord.MarketplaceSyncEntry,
	allProvidersSynced bool,
) error {
	existing, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}

	updated := copyUsageRecord(existing)
	if updated.Syncs == nil {
		updated.Syncs = make(map[usagerecord.Marketplace]usagerecord.MarketplaceSyncEntry)
	}
	updated.Syncs[marketplace] = entry
	updated.AllProvidersSynced = allProvidersSynced
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
	syncs := make(map[usagerecord.Marketplace]usagerecord.MarketplaceSyncEntry, len(r.Syncs))
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
		Syncs:              syncs,
		AllProvidersSynced: r.AllProvidersSynced,
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
