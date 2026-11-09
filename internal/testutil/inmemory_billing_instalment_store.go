package testutil

import (
	"context"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/billingterm"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryBillingInstalmentStore implements billingterm.InstalmentRepository in memory.
type InMemoryBillingInstalmentStore struct {
	*InMemoryStore[*billingterm.Instalment]

	// writeMu makes each multi-row write atomic, as the ent repository's single UPDATE is.
	writeMu sync.Mutex
}

// NewInMemoryBillingInstalmentStore returns an empty in-memory billing instalment store.
func NewInMemoryBillingInstalmentStore() *InMemoryBillingInstalmentStore {
	return &InMemoryBillingInstalmentStore{
		InMemoryStore: NewInMemoryStore[*billingterm.Instalment](),
	}
}

// billingInstalmentInScope mirrors the ent repository's scope: tenant, environment and published.
func billingInstalmentInScope(ctx context.Context, in *billingterm.Instalment) bool {
	return in != nil &&
		in.Status == types.StatusPublished &&
		CheckTenantFilter(ctx, in.TenantID) &&
		CheckEnvironmentFilter(ctx, in.EnvironmentID)
}

// billingInstalmentFilterFn mirrors the predicates List builds in the ent repository.
func billingInstalmentFilterFn(ctx context.Context, in *billingterm.Instalment, filter interface{}) bool {
	if !billingInstalmentInScope(ctx, in) {
		return false
	}

	f, ok := filter.(*billingterm.InstalmentFilter)
	if !ok || f == nil {
		return true
	}

	switch {
	case f.OwnerType != "" && f.OwnerType != in.OwnerType,
		len(f.OwnerIDs) > 0 && !lo.Contains(f.OwnerIDs, in.OwnerID),
		len(f.SubscriptionIDs) > 0 && (in.SubscriptionID == nil || !lo.Contains(f.SubscriptionIDs, *in.SubscriptionID)),
		len(f.Statuses) > 0 && !lo.Contains(f.Statuses, in.InstalmentStatus),
		len(f.Deliveries) > 0 && !lo.Contains(f.Deliveries, in.Delivery),
		f.BillOnOrBefore != nil && in.BillOn.After(*f.BillOnOrBefore),
		len(f.InvoiceIDs) > 0 && (in.InvoiceID == nil || !lo.Contains(f.InvoiceIDs, *in.InvoiceID)),
		len(f.IDs) > 0 && !lo.Contains(f.IDs, in.ID):
		return false
	}

	return true
}

func billingInstalmentSortFn(i, j *billingterm.Instalment) bool {
	if !i.BillOn.Equal(j.BillOn) {
		return i.BillOn.Before(j.BillOn)
	}

	return i.ID < j.ID
}

func (s *InMemoryBillingInstalmentStore) CreateMany(ctx context.Context, instalments []*billingterm.Instalment) ([]*billingterm.Instalment, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	created := make([]*billingterm.Instalment, 0, len(instalments))
	for _, in := range instalments {
		row := *in
		if row.EnvironmentID == "" {
			row.EnvironmentID = types.GetEnvironmentID(ctx)
		}

		if err := s.InMemoryStore.Create(ctx, row.ID, &row); err != nil {
			// The bulk insert is all or nothing, so the rows already added go too.
			for _, c := range created {
				_ = s.InMemoryStore.Delete(ctx, c.ID)
			}

			return nil, err
		}

		created = append(created, lo.ToPtr(row))
	}

	return created, nil
}

func (s *InMemoryBillingInstalmentStore) Get(ctx context.Context, id string) (*billingterm.Instalment, error) {
	row, err := s.InMemoryStore.Get(ctx, id)
	if err != nil || !billingInstalmentInScope(ctx, row) {
		return nil, ierr.NewError("billing instalment not found").
			WithHintf("Billing instalment %s not found", id).
			Mark(ierr.ErrNotFound)
	}

	return lo.ToPtr(*row), nil
}

func (s *InMemoryBillingInstalmentStore) List(ctx context.Context, filter *billingterm.InstalmentFilter) ([]*billingterm.Instalment, error) {
	if filter == nil {
		filter = &billingterm.InstalmentFilter{}
	}

	rows, err := s.InMemoryStore.List(ctx, filter, billingInstalmentFilterFn, billingInstalmentSortFn)
	if err != nil {
		return nil, err
	}

	return lo.Map(rows, func(in *billingterm.Instalment, _ int) *billingterm.Instalment { return lo.ToPtr(*in) }), nil
}

func (s *InMemoryBillingInstalmentStore) ListInvoiced(
	ctx context.Context,
	ownerType types.BillingTermOwnerType,
	ownerIDs []string,
	period types.Period,
) ([]*billingterm.Instalment, error) {
	invoicedIn := func(ctx context.Context, in *billingterm.Instalment, _ interface{}) bool {
		return billingInstalmentInScope(ctx, in) &&
			in.OwnerType == ownerType &&
			lo.Contains(ownerIDs, in.OwnerID) &&
			in.InstalmentStatus == types.InstalmentStatusInvoiced &&
			in.InvoicedAt != nil && !in.InvoicedAt.Before(period.Start) && in.InvoicedAt.Before(period.End)
	}

	rows, err := s.InMemoryStore.List(ctx, nil, invoicedIn, func(i, j *billingterm.Instalment) bool {
		if !i.InvoicedAt.Equal(*j.InvoicedAt) {
			return i.InvoicedAt.Before(*j.InvoicedAt)
		}

		return i.ID < j.ID
	})
	if err != nil {
		return nil, err
	}

	return lo.Map(rows, func(in *billingterm.Instalment, _ int) *billingterm.Instalment { return lo.ToPtr(*in) }), nil
}

func (s *InMemoryBillingInstalmentStore) Update(ctx context.Context, in *billingterm.Instalment) (*billingterm.Instalment, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	updated, err := s.Get(ctx, in.ID)
	if err != nil {
		return nil, err
	}

	if updated.InstalmentStatus != types.InstalmentStatusScheduled {
		return nil, ierr.NewErrorf("billing instalment %s is %s", in.ID, updated.InstalmentStatus).
			WithHintf("Billing instalment %s is no longer scheduled", in.ID).
			Mark(ierr.ErrInvalidOperation)
	}

	updated.BillOn = in.BillOn
	updated.TriggeredAt = in.TriggeredAt
	updated.UpdatedAt = clock.Now()
	updated.UpdatedBy = types.GetUserID(ctx)

	if err := s.InMemoryStore.Update(ctx, updated.ID, updated); err != nil {
		return nil, err
	}

	return lo.ToPtr(*updated), nil
}

func (s *InMemoryBillingInstalmentStore) Cancel(ctx context.Context, ownerType types.BillingTermOwnerType, ownerID string, statuses []types.InstalmentStatus) ([]*billingterm.Instalment, error) {
	if len(statuses) == 0 {
		return nil, ierr.Validation("At least one instalment status is required to cancel instalments")
	}

	return s.transition(ctx,
		func(in *billingterm.Instalment) bool {
			return in.OwnerType == ownerType &&
				in.OwnerID == ownerID &&
				lo.Contains(statuses, in.InstalmentStatus) &&
				in.InstalmentStatus != types.InstalmentStatusCancelled
		},
		func(in *billingterm.Instalment) {
			in.InstalmentStatus = types.InstalmentStatusCancelled
		},
	)
}

func (s *InMemoryBillingInstalmentStore) Claim(ctx context.Context, invoiceID string, invoicedAt time.Time, ids []string) ([]*billingterm.Instalment, error) {
	if len(ids) == 0 {
		return []*billingterm.Instalment{}, nil
	}

	return s.transition(ctx,
		func(in *billingterm.Instalment) bool {
			return lo.Contains(ids, in.ID) && in.InstalmentStatus == types.InstalmentStatusScheduled
		},
		func(in *billingterm.Instalment) {
			in.InstalmentStatus = types.InstalmentStatusInvoiced
			in.InvoiceID = lo.ToPtr(invoiceID)
			in.InvoicedAt = lo.ToPtr(invoicedAt)
		},
	)
}

func (s *InMemoryBillingInstalmentStore) Reschedule(ctx context.Context, invoiceID string) ([]*billingterm.Instalment, error) {
	return s.transition(ctx,
		func(in *billingterm.Instalment) bool {
			return in.InvoiceID != nil && *in.InvoiceID == invoiceID && in.InstalmentStatus == types.InstalmentStatusInvoiced
		},
		func(in *billingterm.Instalment) {
			in.InstalmentStatus = types.InstalmentStatusScheduled
			in.InvoiceID = nil
			in.InvoicedAt = nil
		},
	)
}

// transition applies change to every in-scope instalment that matches and returns the changed rows.
func (s *InMemoryBillingInstalmentStore) transition(ctx context.Context, matches func(*billingterm.Instalment) bool, change func(*billingterm.Instalment)) ([]*billingterm.Instalment, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	rows, err := s.InMemoryStore.List(ctx, nil, func(ctx context.Context, in *billingterm.Instalment, _ interface{}) bool {
		return billingInstalmentInScope(ctx, in) && matches(in)
	}, billingInstalmentSortFn)
	if err != nil {
		return nil, err
	}

	changed := make([]*billingterm.Instalment, 0, len(rows))
	for _, row := range rows {
		updated := *row
		change(&updated)
		updated.UpdatedAt = clock.Now()
		updated.UpdatedBy = types.GetUserID(ctx)

		if err := s.InMemoryStore.Update(ctx, updated.ID, &updated); err != nil {
			return nil, err
		}

		changed = append(changed, lo.ToPtr(updated))
	}

	return changed, nil
}
