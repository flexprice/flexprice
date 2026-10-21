package testutil

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryEventDeletionRequestStore implements events.DeletionRequestRepository for tests.
type InMemoryEventDeletionRequestStore struct {
	*InMemoryStore[*events.DeletionRequest]
}

func NewInMemoryEventDeletionRequestStore() *InMemoryEventDeletionRequestStore {
	return &InMemoryEventDeletionRequestStore{
		InMemoryStore: NewInMemoryStore[*events.DeletionRequest](),
	}
}

func eventDeletionRequestFilterFn(ctx context.Context, r *events.DeletionRequest, filter interface{}) bool {
	if r == nil {
		return false
	}
	f, ok := filter.(*types.EventDeletionRequestFilter)
	if !ok {
		return true
	}

	if !CheckTenantFilter(ctx, r.TenantID) {
		return false
	}
	if !CheckEnvironmentFilter(ctx, r.EnvironmentID) {
		return false
	}
	if r.Status != types.StatusPublished {
		return false
	}

	if len(f.ExternalCustomerIDs) > 0 && !lo.Contains(f.ExternalCustomerIDs, r.ExternalCustomerID) {
		return false
	}
	if f.DeletionStatus != "" && r.DeletionStatus != f.DeletionStatus {
		return false
	}
	// The overlap test: period_start <= bound AND period_end >= bound.
	if f.PeriodStartLTE != nil && r.PeriodStart.After(*f.PeriodStartLTE) {
		return false
	}
	if f.PeriodEndGTE != nil && r.PeriodEnd.Before(*f.PeriodEndGTE) {
		return false
	}

	return true
}

// CreateBulk mirrors the partial unique index: at most one DELETING row per customer.
func (s *InMemoryEventDeletionRequestStore) CreateBulk(ctx context.Context, requests []*events.DeletionRequest) error {
	for _, req := range requests {
		if err := req.Validate(); err != nil {
			return err
		}
	}

	inProgress := types.NewNoLimitEventDeletionRequestFilter()
	inProgress.DeletionStatus = types.EventDeletionStatusDeleting
	inProgress.ExternalCustomerIDs = lo.Map(requests, func(r *events.DeletionRequest, _ int) string {
		return r.ExternalCustomerID
	})
	existing, err := s.ListAll(ctx, inProgress)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return ierr.NewError("a deletion is already in progress for one of the customers in this request").
			WithReportableDetails(map[string]interface{}{
				"reason":               types.EventDeletionReasonDeletionInProgress,
				"external_customer_id": existing[0].ExternalCustomerID,
			}).
			Mark(ierr.ErrAlreadyExists)
	}

	for _, req := range requests {
		if req.TenantID == "" {
			req.TenantID = types.GetTenantID(ctx)
		}
		if req.EnvironmentID == "" {
			req.EnvironmentID = types.GetEnvironmentID(ctx)
		}
		if req.Status == "" {
			req.Status = types.StatusPublished
		}
		if err := s.InMemoryStore.Create(ctx, req.ID, req); err != nil {
			return err
		}
	}
	return nil
}

func (s *InMemoryEventDeletionRequestStore) ListAll(
	ctx context.Context,
	filter *types.EventDeletionRequestFilter,
) ([]*events.DeletionRequest, error) {
	if filter == nil {
		filter = types.NewNoLimitEventDeletionRequestFilter()
	}
	return s.InMemoryStore.List(ctx, filter, eventDeletionRequestFilterFn, nil)
}

func (s *InMemoryEventDeletionRequestStore) MarkTerminal(
	ctx context.Context,
	ids []string,
	status types.EventDeletionStatus,
	reason types.EventDeletionFailureReason,
) error {
	if len(ids) == 0 {
		return nil
	}

	rows, err := s.ListAll(ctx, types.NewNoLimitEventDeletionRequestFilter())
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	for _, row := range rows {
		if !lo.Contains(ids, row.ID) || row.DeletionStatus != types.EventDeletionStatusDeleting {
			continue
		}
		row.DeletionStatus = status
		if status == types.EventDeletionStatusCompleted {
			row.CompletedAt = &now
		} else {
			row.FailedAt = &now
			row.FailureReason = lo.ToPtr(reason.String())
		}
		if err := s.InMemoryStore.Update(ctx, row.ID, row); err != nil {
			return err
		}
	}
	return nil
}
