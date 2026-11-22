package ent

import (
	"context"

	"github.com/samber/lo"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/predicate"
	"github.com/flexprice/flexprice/ent/reminderdecision"
	"github.com/flexprice/flexprice/internal/clock"
	domain "github.com/flexprice/flexprice/internal/domain/collections"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type reminderDecisionRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

func NewReminderDecisionRepository(client postgres.IClient, log *logger.Logger) domain.ReminderDecisionRepository {
	return &reminderDecisionRepository{client: client, log: log}
}

func (r *reminderDecisionRepository) scoped(ctx context.Context) *ent.ReminderDecisionQuery {
	return r.client.Reader(ctx).ReminderDecision.Query().
		Where(
			reminderdecision.TenantID(types.GetTenantID(ctx)),
			reminderdecision.EnvironmentID(types.GetEnvironmentID(ctx)),
			reminderdecision.Status(string(types.StatusPublished)),
		)
}

func (r *reminderDecisionRepository) Create(ctx context.Context, d *domain.ReminderDecision) error {
	span := StartRepositorySpan(ctx, "reminder_decision", "create", map[string]interface{}{"reminder_decision_id": d.ID})
	defer FinishSpan(span)

	if d.EnvironmentID == "" {
		d.EnvironmentID = types.GetEnvironmentID(ctx)
	}
	created, err := r.client.Writer(ctx).ReminderDecision.Create().
		SetID(d.ID).
		SetCustomerID(d.CustomerID).
		SetCurrency(d.Currency).
		SetDraftStatus(string(d.Status)).
		SetHoldReason(d.HoldReason).
		SetNillableDecidedAt(d.DecidedAt).
		SetToneOverride(string(d.ToneOverride)).
		SetSubjectOverride(d.SubjectOverride).
		SetBodyOverride(d.BodyOverride).
		SetInvoiceIds(d.InvoiceIDs).
		SetInvoiceSetHash(d.InvoiceSetHash).
		SetFrozenSubject(d.FrozenSubject).
		SetFrozenBody(d.FrozenBody).
		SetFrozenTone(string(d.FrozenTone)).
		SetVersion(1).
		SetTenantID(d.TenantID).
		SetEnvironmentID(d.EnvironmentID).
		SetStatus(string(types.StatusPublished)).
		SetCreatedAt(d.CreatedAt).
		SetUpdatedAt(d.UpdatedAt).
		SetCreatedBy(d.CreatedBy).
		SetUpdatedBy(d.UpdatedBy).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsConstraintError(err) {
			return ierr.WithError(err).WithHint("This reminder was changed by someone else").Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).WithHint("Failed to save reminder decision").Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	*d = *reminderDecisionFromEnt(created)
	return nil
}

func (r *reminderDecisionRepository) ListByKeys(ctx context.Context, keys []domain.GroupKey) ([]*domain.ReminderDecision, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	span := StartRepositorySpan(ctx, "reminder_decision", "list_by_keys", map[string]interface{}{"groups": len(keys)})
	defer FinishSpan(span)

	byCustomer := map[string][]string{}
	for _, k := range keys {
		byCustomer[k.CustomerID] = append(byCustomer[k.CustomerID], k.Currency)
	}
	rows, err := r.scoped(ctx).
		Where(reminderdecision.CustomerIDIn(lo.Keys(byCustomer)...), reminderdecision.CurrencyIn(lo.Uniq(lo.Map(keys, func(k domain.GroupKey, _ int) string { return k.Currency }))...)).
		All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).WithHint("Failed to list reminder decisions").Mark(ierr.ErrDatabase)
	}
	decisions := make([]*domain.ReminderDecision, 0, len(rows))
	for _, row := range rows {
		if lo.Contains(byCustomer[row.CustomerID], row.Currency) {
			decisions = append(decisions, reminderDecisionFromEnt(row))
		}
	}
	SetSpanSuccess(span)
	return decisions, nil
}

func (r *reminderDecisionRepository) Update(ctx context.Context, d *domain.ReminderDecision) error {
	span := StartRepositorySpan(ctx, "reminder_decision", "update", map[string]interface{}{"reminder_decision_id": d.ID})
	defer FinishSpan(span)

	update := r.client.Writer(ctx).ReminderDecision.UpdateOneID(d.ID).
		Where(r.currentVersion(ctx, d)...).
		SetDraftStatus(string(d.Status)).
		SetHoldReason(d.HoldReason).
		SetNillableDecidedAt(d.DecidedAt).
		SetToneOverride(string(d.ToneOverride)).
		SetSubjectOverride(d.SubjectOverride).
		SetBodyOverride(d.BodyOverride).
		SetInvoiceIds(d.InvoiceIDs).
		SetInvoiceSetHash(d.InvoiceSetHash).
		SetFrozenSubject(d.FrozenSubject).
		SetFrozenBody(d.FrozenBody).
		SetFrozenTone(string(d.FrozenTone)).
		AddVersion(1).
		SetUpdatedAt(clock.Now()).
		SetUpdatedBy(types.GetUserID(ctx))
	if d.DecidedAt == nil {
		update = update.ClearDecidedAt()
	}

	updated, err := update.Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return r.mapWriteError(err, "update")
	}
	SetSpanSuccess(span)
	*d = *reminderDecisionFromEnt(updated)
	return nil
}

func (r *reminderDecisionRepository) Delete(ctx context.Context, d *domain.ReminderDecision) error {
	span := StartRepositorySpan(ctx, "reminder_decision", "delete", map[string]interface{}{"reminder_decision_id": d.ID})
	defer FinishSpan(span)

	_, err := r.client.Writer(ctx).ReminderDecision.UpdateOneID(d.ID).
		Where(r.currentVersion(ctx, d)...).
		SetStatus(string(types.StatusDeleted)).
		AddVersion(1).
		SetUpdatedAt(clock.Now()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return r.mapWriteError(err, "delete")
	}
	SetSpanSuccess(span)
	return nil
}

func (r *reminderDecisionRepository) currentVersion(ctx context.Context, d *domain.ReminderDecision) []predicate.ReminderDecision {
	return []predicate.ReminderDecision{
		reminderdecision.TenantID(types.GetTenantID(ctx)),
		reminderdecision.EnvironmentID(types.GetEnvironmentID(ctx)),
		reminderdecision.Status(string(types.StatusPublished)),
		reminderdecision.Version(d.Version),
	}
}

func (r *reminderDecisionRepository) mapWriteError(err error, action string) error {
	if ent.IsNotFound(err) {
		return ierr.WithError(err).WithHint("This reminder was changed by someone else. Reload and try again.").Mark(ierr.ErrVersionConflict)
	}
	return ierr.WithError(err).WithHintf("Failed to %s reminder decision", action).Mark(ierr.ErrDatabase)
}

func reminderDecisionFromEnt(e *ent.ReminderDecision) *domain.ReminderDecision {
	return &domain.ReminderDecision{
		ID:              e.ID,
		CustomerID:      e.CustomerID,
		Currency:        e.Currency,
		Status:          types.ReminderStatus(e.DraftStatus),
		HoldReason:      e.HoldReason,
		DecidedAt:       e.DecidedAt,
		ToneOverride:    types.ReminderTone(e.ToneOverride),
		SubjectOverride: e.SubjectOverride,
		BodyOverride:    e.BodyOverride,
		InvoiceIDs:      e.InvoiceIds,
		InvoiceSetHash:  e.InvoiceSetHash,
		FrozenSubject:   e.FrozenSubject,
		FrozenBody:      e.FrozenBody,
		FrozenTone:      types.ReminderTone(e.FrozenTone),
		Version:         e.Version,
		EnvironmentID:   e.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
		},
	}
}
