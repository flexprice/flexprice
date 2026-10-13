package testutil

import (
	"context"
	"strings"
	"time"

	fxrate "github.com/flexprice/flexprice/internal/domain/fxrate"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryFXRateStore implements fxrate.Repository for tests.
type InMemoryFXRateStore struct {
	*InMemoryStore[*fxrate.FXRate]
}

func NewInMemoryFXRateStore() *InMemoryFXRateStore {
	return &InMemoryFXRateStore{
		InMemoryStore: NewInMemoryStore[*fxrate.FXRate](),
	}
}

func fxRateFilterFn(ctx context.Context, fr *fxrate.FXRate, filter interface{}) bool {
	if fr == nil {
		return false
	}
	f, ok := filter.(*types.FXRateFilter)
	if !ok {
		return true
	}

	if tenantID, ok := ctx.Value(types.CtxTenantID).(string); ok {
		if fr.TenantID != tenantID {
			return false
		}
	}

	if !CheckEnvironmentFilter(ctx, fr.EnvironmentID) {
		return false
	}

	// Default to published-only, mirroring the ent ApplyStatusFilter.
	status := ""
	if f.QueryFilter != nil {
		status = f.GetStatus()
	}
	if status == "" {
		if fr.Status != types.StatusPublished {
			return false
		}
	} else if string(fr.Status) != status {
		return false
	}

	if len(f.FXRateIDs) > 0 && !lo.Contains(f.FXRateIDs, fr.ID) {
		return false
	}
	if f.Scope != nil && fr.Scope != *f.Scope {
		return false
	}
	if f.ScopeID != nil && fr.ScopeID != *f.ScopeID {
		return false
	}
	if f.FromCurrency != nil && !types.IsMatchingCurrency(fr.FromCurrency, *f.FromCurrency) {
		return false
	}
	if f.ToCurrency != nil && !types.IsMatchingCurrency(fr.ToCurrency, *f.ToCurrency) {
		return false
	}

	if f.TimeRangeFilter != nil {
		if f.StartTime != nil && fr.CreatedAt.Before(*f.StartTime) {
			return false
		}
		if f.EndTime != nil && fr.CreatedAt.After(*f.EndTime) {
			return false
		}
	}

	return true
}

func fxRateSortFn(i, j *fxrate.FXRate) bool {
	if i == nil || j == nil {
		return false
	}
	return i.CreatedAt.After(j.CreatedAt)
}

func (s *InMemoryFXRateStore) Create(ctx context.Context, fr *fxrate.FXRate) error {
	if fr == nil {
		return ierr.NewError("fx rate cannot be nil").WithHint("FX rate data is required").Mark(ierr.ErrValidation)
	}
	if fr.EnvironmentID == "" {
		fr.EnvironmentID = types.GetEnvironmentID(ctx)
	}
	if fr.Status == "" {
		fr.Status = types.StatusPublished
	}
	fr.FromCurrency = strings.ToLower(fr.FromCurrency)
	fr.ToCurrency = strings.ToLower(fr.ToCurrency)

	err := s.InMemoryStore.Create(ctx, fr.ID, fr)
	if err != nil {
		if ierr.IsAlreadyExists(err) {
			return ierr.WithError(err).
				WithHint("An FX rate with this identifier already exists").
				WithReportableDetails(map[string]any{"fx_rate_id": fr.ID}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).WithHint("Failed to create fx rate").Mark(ierr.ErrDatabase)
	}
	return nil
}

func (s *InMemoryFXRateStore) Get(ctx context.Context, id string) (*fxrate.FXRate, error) {
	fr, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		if ierr.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("FX rate with ID %s was not found", id).
				WithReportableDetails(map[string]any{"fx_rate_id": id}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).WithHint("Failed to get fx rate").Mark(ierr.ErrDatabase)
	}
	return fr, nil
}

func (s *InMemoryFXRateStore) List(ctx context.Context, filter *types.FXRateFilter) ([]*fxrate.FXRate, error) {
	rates, err := s.InMemoryStore.List(ctx, filter, fxRateFilterFn, fxRateSortFn)
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to list fx rates").Mark(ierr.ErrDatabase)
	}
	return rates, nil
}

func (s *InMemoryFXRateStore) Count(ctx context.Context, filter *types.FXRateFilter) (int, error) {
	count, err := s.InMemoryStore.Count(ctx, filter, fxRateFilterFn)
	if err != nil {
		return 0, ierr.WithError(err).WithHint("Failed to count fx rates").Mark(ierr.ErrDatabase)
	}
	return count, nil
}

func (s *InMemoryFXRateStore) Update(ctx context.Context, fr *fxrate.FXRate) error {
	if fr == nil {
		return ierr.NewError("fx rate cannot be nil").WithHint("FX rate data is required").Mark(ierr.ErrValidation)
	}
	// Mirror the ent repo: only a published row is updatable, so a row archived concurrently
	// surfaces as not-found instead of being revived.
	if existing, gerr := s.InMemoryStore.Get(ctx, fr.ID); gerr == nil && existing.Status != types.StatusPublished {
		return ierr.NewErrorf("FX rate with ID %s was not found", fr.ID).
			WithReportableDetails(map[string]any{"fx_rate_id": fr.ID}).
			Mark(ierr.ErrNotFound)
	}
	err := s.InMemoryStore.Update(ctx, fr.ID, fr)
	if err != nil {
		if ierr.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("FX rate with ID %s was not found", fr.ID).
				WithReportableDetails(map[string]any{"fx_rate_id": fr.ID}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).WithHint("Failed to update fx rate").Mark(ierr.ErrDatabase)
	}
	return nil
}

// Delete soft-archives the rate, matching the ent repository.
func (s *InMemoryFXRateStore) Delete(ctx context.Context, fr *fxrate.FXRate) error {
	if fr == nil {
		return ierr.NewError("fx rate cannot be nil").WithHint("FX rate data is required").Mark(ierr.ErrValidation)
	}
	existing, err := s.InMemoryStore.Get(ctx, fr.ID)
	if err != nil {
		if ierr.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("FX rate with ID %s was not found", fr.ID).
				WithReportableDetails(map[string]any{"fx_rate_id": fr.ID}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).WithHint("Failed to delete fx rate").Mark(ierr.ErrDatabase)
	}
	existing.Status = types.StatusArchived
	return s.InMemoryStore.Update(ctx, existing.ID, existing)
}

func (s *InMemoryFXRateStore) GetTenantRate(ctx context.Context, from, to string) (*fxrate.FXRate, error) {
	scope := types.FXRateScopeTenant
	scopeID := types.FXRateScopeIDTenant
	filter := &types.FXRateFilter{
		QueryFilter:  types.NewNoLimitQueryFilter(),
		Scope:        &scope,
		ScopeID:      &scopeID,
		FromCurrency: &from,
		ToCurrency:   &to,
	}
	rates, err := s.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	if len(rates) == 0 {
		return nil, ierr.NewError("fx rate not found").
			WithHintf("No tenant FX rate configured for %s → %s", from, to).
			Mark(ierr.ErrNotFound)
	}
	return rates[0], nil
}

func (s *InMemoryFXRateStore) FindOverlapping(ctx context.Context, scope types.FXRateScope, scopeID, from, to string, startDate, endDate *time.Time, excludeID string) ([]*fxrate.FXRate, error) {
	filter := &types.FXRateFilter{
		QueryFilter:  types.NewNoLimitQueryFilter(),
		Scope:        &scope,
		ScopeID:      &scopeID,
		FromCurrency: &from,
		ToCurrency:   &to,
	}
	rates, err := s.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	overlapping := make([]*fxrate.FXRate, 0)
	for _, r := range rates {
		if r.ID == excludeID {
			continue
		}
		if types.FXRateWindowsOverlap(r.StartDate, r.EndDate, startDate, endDate) {
			overlapping = append(overlapping, r)
		}
	}
	return overlapping, nil
}

func (s *InMemoryFXRateStore) Clear() {
	s.InMemoryStore.Clear()
}
