package testutil

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/domain/revenuefact"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// provisionalGrainKey identifies the unique-per-row grain enforced by the
// revenue_facts partial unique index: tenant, environment, subscription,
// price, day, revenue_source, scoped to status=PROVISIONAL rows only.
type provisionalGrainKey struct {
	tenantID       string
	environmentID  string
	subscriptionID string
	priceID        string
	subLineItemID  string
	day            time.Time
	revenueSource  types.RevenueSource
}

func grainKeyFor(f *revenuefact.RevenueFact) provisionalGrainKey {
	var priceID, subLineItemID string
	if f.PriceID != nil {
		priceID = *f.PriceID
	}
	if f.SubLineItemID != nil {
		subLineItemID = *f.SubLineItemID
	}
	return provisionalGrainKey{
		tenantID:       f.TenantID,
		environmentID:  f.EnvironmentID,
		subscriptionID: f.SubscriptionID,
		priceID:        priceID,
		subLineItemID:  subLineItemID,
		day:            f.Day,
		revenueSource:  f.RevenueSource,
	}
}

// InMemoryRevenueFactStore implements revenuefact.Repository for testing,
// mirroring InMemoryAnalyticsViewStore's tenant/environment scoping.
type InMemoryRevenueFactStore struct {
	mu sync.RWMutex

	// facts is keyed by ID.
	facts map[string]*revenuefact.RevenueFact

	// provisionalIndex maps the provisional grain tuple to the fact ID
	// currently holding it, so upserts can find the row to bump instead of
	// inserting a duplicate. Entries are removed once a row flips to FINAL.
	provisionalIndex map[provisionalGrainKey]string
}

// NewInMemoryRevenueFactStore creates a new in-memory revenue fact store.
func NewInMemoryRevenueFactStore() *InMemoryRevenueFactStore {
	return &InMemoryRevenueFactStore{
		facts:            make(map[string]*revenuefact.RevenueFact),
		provisionalIndex: make(map[provisionalGrainKey]string),
	}
}

var _ revenuefact.Repository = (*InMemoryRevenueFactStore)(nil)

// UpsertProvisional inserts or updates facts on the provisional grain,
// always sourcing tenant/environment from ctx (mirroring the RLS guard the
// raw-SQL repo must apply manually) and bumping Version on conflict.
func (s *InMemoryRevenueFactStore) UpsertProvisional(ctx context.Context, facts []*revenuefact.RevenueFact) error {
	// Mirror the Postgres guard: a NULL/empty price_id would dodge the unique
	// index and duplicate rows, so reject it.
	for _, f := range facts {
		if f.PriceID == nil || *f.PriceID == "" {
			return ierr.NewError("revenue fact requires a non-empty price_id").
				WithHint("Provisional revenue facts must carry a non-empty price_id").
				Mark(ierr.ErrValidation)
		}
		if f.SubLineItemID == nil || *f.SubLineItemID == "" {
			return ierr.NewError("revenue fact requires a non-empty sub_line_item_id").
				WithHint("Provisional revenue facts must carry a non-empty sub_line_item_id").
				Mark(ierr.ErrValidation)
		}
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, f := range facts {
		f.TenantID = tenantID
		f.EnvironmentID = environmentID
		f.Status = types.FactProvisional
		// Normalize before the conflict branch so insert and conflict-update
		// write the same values the Postgres repo binds (it normalizes both
		// fields before building the statement).
		if f.Version == 0 {
			f.Version = 1
		}
		if f.ComputedAt.IsZero() {
			f.ComputedAt = time.Now().UTC()
		}

		key := grainKeyFor(f)
		if existingID, ok := s.provisionalIndex[key]; ok {
			existing := s.facts[existingID]

			// Preserve identity fields; copy over the mutable columns like the
			// raw-SQL DO UPDATE SET, then bump the version.
			updated := *f
			updated.ID = existing.ID
			updated.Version = existing.Version + 1
			s.facts[existing.ID] = &updated
			continue
		}

		if f.ID == "" {
			f.ID = types.GenerateUUIDWithPrefix("rf")
		}

		s.facts[f.ID] = f
		s.provisionalIndex[key] = f.ID
	}

	return nil
}

// FlipToFinal converts PROVISIONAL rows for a subscription/price/period to
// FINAL, stamping the invoice line item, and returns the rows affected.
func (s *InMemoryRevenueFactStore) FlipToFinal(ctx context.Context, subscriptionID, priceID string, periodStart, periodEnd time.Time, invoiceID, invoiceLineItemID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	affected := 0
	for _, f := range s.facts {
		if !CheckTenantFilter(ctx, f.TenantID) || !CheckEnvironmentFilter(ctx, f.EnvironmentID) {
			continue
		}
		if f.Status != types.FactProvisional {
			continue
		}
		if f.SubscriptionID != subscriptionID {
			continue
		}
		if !factPriceMatches(f, priceID) {
			continue
		}
		if f.Day.Before(periodStart) || f.Day.After(periodEnd) {
			continue
		}

		f.Status = types.FactFinal
		invID := invoiceID
		invLineID := invoiceLineItemID
		f.InvoiceID = &invID
		f.InvoiceLineItemID = &invLineID

		delete(s.provisionalIndex, grainKeyFor(f))

		affected++
	}

	return affected, nil
}

// ListBySubscriptionPeriod lists facts for a subscription within a period, filtered by status.
func (s *InMemoryRevenueFactStore) ListBySubscriptionPeriod(ctx context.Context, subscriptionID string, periodStart, periodEnd time.Time, status types.FactStatus) ([]*revenuefact.RevenueFact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*revenuefact.RevenueFact
	for _, f := range s.facts {
		if !CheckTenantFilter(ctx, f.TenantID) || !CheckEnvironmentFilter(ctx, f.EnvironmentID) {
			continue
		}
		if f.SubscriptionID != subscriptionID {
			continue
		}
		if f.Status != status {
			continue
		}
		if f.Day.Before(periodStart) || f.Day.After(periodEnd) {
			continue
		}
		result = append(result, f)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Day.Before(result[j].Day)
	})

	return result, nil
}

// RevertByInvoice writes a contra row for every FINAL, non-revert fact stamped
// with invoiceID, mirroring the Postgres repo: idempotent, all-or-nothing.
func (s *InMemoryRevenueFactStore) RevertByInvoice(ctx context.Context, invoiceID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var originals []*revenuefact.RevenueFact
	for _, f := range s.facts {
		if !CheckTenantFilter(ctx, f.TenantID) || !CheckEnvironmentFilter(ctx, f.EnvironmentID) {
			continue
		}
		if f.InvoiceID == nil || *f.InvoiceID != invoiceID || f.Status != types.FactFinal {
			continue
		}
		if f.IsRevert {
			// Already reverted (retried void hook) — nothing to do.
			return 0, nil
		}
		originals = append(originals, f)
	}

	now := time.Now().UTC()
	for _, f := range originals {
		rev := revenuefact.NewRevert(f, now)
		s.facts[rev.ID] = rev
	}
	return len(originals), nil
}

func factPriceMatches(f *revenuefact.RevenueFact, priceID string) bool {
	if f.PriceID == nil {
		return priceID == ""
	}
	return *f.PriceID == priceID
}

// Clear removes all facts from the store.
func (s *InMemoryRevenueFactStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.facts = make(map[string]*revenuefact.RevenueFact)
	s.provisionalIndex = make(map[provisionalGrainKey]string)
}

// ListByInvoiceID lists every fact stamped with the invoice, reverts included.
func (s *InMemoryRevenueFactStore) ListByInvoiceID(ctx context.Context, invoiceID string) ([]*revenuefact.RevenueFact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*revenuefact.RevenueFact
	for _, f := range s.facts {
		if !CheckTenantFilter(ctx, f.TenantID) || !CheckEnvironmentFilter(ctx, f.EnvironmentID) {
			continue
		}
		if f.InvoiceID == nil || *f.InvoiceID != invoiceID {
			continue
		}
		result = append(result, f)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Day.Before(result[j].Day) })
	return result, nil
}

// ListForExport pages facts recomputed after the watermark, ordered by
// (computed_at, id) for stable resumption.
func (s *InMemoryRevenueFactStore) ListForExport(ctx context.Context, computedAfter time.Time, afterID string, limit int) ([]*revenuefact.RevenueFact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*revenuefact.RevenueFact
	for _, f := range s.facts {
		if !CheckTenantFilter(ctx, f.TenantID) || !CheckEnvironmentFilter(ctx, f.EnvironmentID) {
			continue
		}
		switch {
		case afterID != "" && f.ComputedAt.Equal(computedAfter):
			if f.ID <= afterID {
				continue
			}
		case !computedAfter.IsZero() && !f.ComputedAt.After(computedAfter):
			continue
		}
		result = append(result, f)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ComputedAt.Equal(result[j].ComputedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].ComputedAt.Before(result[j].ComputedAt)
	})
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}
