package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	entDeletion "github.com/flexprice/flexprice/ent/eventdeletionrequest"
	domainEvents "github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type eventDeletionRequestRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

func NewEventDeletionRequestRepository(client postgres.IClient, log *logger.Logger) domainEvents.DeletionRequestRepository {
	return &eventDeletionRequestRepository{client: client, log: log}
}

func (r *eventDeletionRequestRepository) CreateBulk(ctx context.Context, requests []*domainEvents.DeletionRequest) error {
	if len(requests) == 0 {
		return nil
	}

	span := StartRepositorySpan(ctx, "event_deletion_request", "create_bulk", map[string]interface{}{
		"request_id": requests[0].RequestID,
		"count":      len(requests),
	})
	defer FinishSpan(span)

	client := r.client.Writer(ctx)
	builders := make([]*ent.EventDeletionRequestCreate, len(requests))
	for i, req := range requests {
		if err := req.Validate(); err != nil {
			return err
		}
		if req.EnvironmentID == "" {
			req.EnvironmentID = types.GetEnvironmentID(ctx)
		}
		builders[i] = client.EventDeletionRequest.Create().
			SetID(req.ID).
			SetTenantID(types.GetTenantID(ctx)).
			SetEnvironmentID(req.EnvironmentID).
			SetRequestID(req.RequestID).
			SetExternalCustomerID(req.ExternalCustomerID).
			SetPeriodStart(req.PeriodStart).
			SetPeriodEnd(req.PeriodEnd).
			SetDeletionStatus(req.DeletionStatus).
			SetStartedAt(req.StartedAt).
			SetNillableCompletedAt(req.CompletedAt).
			SetNillableFailedAt(req.FailedAt).
			SetNillableFailureReason(req.FailureReason).
			SetSkipPayload(req.SkipPayload).
			SetStatus(string(types.StatusPublished)).
			SetCreatedBy(types.GetUserID(ctx)).
			SetUpdatedBy(types.GetUserID(ctx))
	}

	if _, err := client.EventDeletionRequest.CreateBulk(builders...).Save(ctx); err != nil {
		SetSpanError(span, err)
		// The table carries one unique index, the partial one on DELETING, so any
		// constraint violation here means a deletion is already in flight.
		if ent.IsConstraintError(err) {
			return ierr.WithError(err).
				WithHint("A deletion is already in progress for one of the customers in this request").
				WithReportableDetails(map[string]interface{}{
					"reason": types.EventDeletionReasonDeletionInProgress,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to create the event deletion request").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

func (r *eventDeletionRequestRepository) ListAll(
	ctx context.Context,
	filter *types.EventDeletionRequestFilter,
) ([]*domainEvents.DeletionRequest, error) {
	if filter == nil {
		filter = types.NewNoLimitEventDeletionRequestFilter()
	}
	if err := filter.Validate(); err != nil {
		return nil, err
	}

	span := StartRepositorySpan(ctx, "event_deletion_request", "list_all", map[string]interface{}{
		"customer_count":  len(filter.ExternalCustomerIDs),
		"deletion_status": filter.DeletionStatus,
	})
	defer FinishSpan(span)

	query := r.client.Reader(ctx).EventDeletionRequest.Query().
		Where(
			entDeletion.TenantID(types.GetTenantID(ctx)),
			entDeletion.EnvironmentID(types.GetEnvironmentID(ctx)),
			entDeletion.Status(string(types.StatusPublished)),
		)
	query = r.applyFilters(query, filter)

	rows, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list the event deletion requests").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainEvents.DeletionRequestListFromEnt(rows), nil
}

// applyFilters narrows the query. PeriodStartLT with PeriodEndGT is the period
// overlap test, shared by the deletion guard and the finalization gate.
func (r *eventDeletionRequestRepository) applyFilters(
	query *ent.EventDeletionRequestQuery,
	filter *types.EventDeletionRequestFilter,
) *ent.EventDeletionRequestQuery {
	if len(filter.ExternalCustomerIDs) > 0 {
		query = query.Where(entDeletion.ExternalCustomerIDIn(filter.ExternalCustomerIDs...))
	}
	if filter.DeletionStatus != "" {
		query = query.Where(entDeletion.DeletionStatus(filter.DeletionStatus))
	}
	if filter.PeriodStartLT != nil {
		query = query.Where(entDeletion.PeriodStartLT(*filter.PeriodStartLT))
	}
	if filter.PeriodEndGT != nil {
		query = query.Where(entDeletion.PeriodEndGT(*filter.PeriodEndGT))
	}
	if filter.StartedAtGT != nil {
		query = query.Where(entDeletion.StartedAtGT(*filter.StartedAtGT))
	}
	if filter.StartedAtLT != nil {
		query = query.Where(entDeletion.StartedAtLT(*filter.StartedAtLT))
	}
	return query
}

func (r *eventDeletionRequestRepository) MarkTerminal(
	ctx context.Context,
	ids []string,
	status types.EventDeletionStatus,
	reason types.EventDeletionFailureReason,
) error {
	if len(ids) == 0 {
		return nil
	}

	span := StartRepositorySpan(ctx, "event_deletion_request", "mark_terminal", map[string]interface{}{
		"row_count":       len(ids),
		"deletion_status": status,
	})
	defer FinishSpan(span)

	now := time.Now().UTC()
	update := r.client.Writer(ctx).EventDeletionRequest.Update().
		Where(
			entDeletion.TenantID(types.GetTenantID(ctx)),
			entDeletion.EnvironmentID(types.GetEnvironmentID(ctx)),
			entDeletion.IDIn(ids...),
			entDeletion.DeletionStatus(types.EventDeletionStatusDeleting),
		).
		SetDeletionStatus(status).
		SetUpdatedBy(types.GetUserID(ctx))

	if status == types.EventDeletionStatusCompleted {
		update = update.SetCompletedAt(now)
	} else {
		update = update.SetFailedAt(now).SetFailureReason(reason.String())
	}

	if _, err := update.Save(ctx); err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to close the event deletion request").
			WithReportableDetails(map[string]interface{}{"ids": ids}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}
