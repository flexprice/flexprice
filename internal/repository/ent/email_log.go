package ent

import (
	"context"
	"errors"

	"github.com/lib/pq"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/emaillog"
	"github.com/flexprice/flexprice/ent/schema"
	"github.com/flexprice/flexprice/internal/clock"
	domainEmailLog "github.com/flexprice/flexprice/internal/domain/emaillog"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type emailLogRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

func NewEmailLogRepository(client postgres.IClient, log *logger.Logger) domainEmailLog.Repository {
	return &emailLogRepository{client: client, log: log}
}

func (r *emailLogRepository) Create(ctx context.Context, e *domainEmailLog.EmailLog) error {
	span := StartRepositorySpan(ctx, "email_log", "create", map[string]interface{}{
		"email_log_id": e.ID,
		"entity_id":    e.EntityID,
	})
	defer FinishSpan(span)

	if e.EnvironmentID == "" {
		e.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	_, err := r.client.Writer(ctx).EmailLog.Create().
		SetID(e.ID).
		SetEntityType(e.EntityType).
		SetEntityID(e.EntityID).
		SetCustomerID(e.CustomerID).
		SetTemplateKey(e.TemplateKey).
		SetTrigger(e.Trigger).
		SetFromAddress(e.FromAddress).
		SetReplyTo(e.ReplyTo).
		SetToAddresses(e.ToAddresses).
		SetCcAddresses(e.CCAddresses).
		SetSubject(e.Subject).
		SetProvider(e.Provider).
		SetNillableProviderMessageID(e.ProviderMessageID).
		SetDeliveryStatus(e.DeliveryStatus).
		SetNillableStatusReason(e.StatusReason).
		SetNillableSentAt(e.SentAt).
		SetNillableDeliveredAt(e.DeliveredAt).
		SetMetadata(e.Metadata).
		SetStatus(string(e.Status)).
		SetTenantID(e.TenantID).
		SetEnvironmentID(e.EnvironmentID).
		SetCreatedAt(e.CreatedAt).
		SetUpdatedAt(e.UpdatedAt).
		SetCreatedBy(e.CreatedBy).
		SetUpdatedBy(e.UpdatedBy).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		var pqErr *pq.Error
		if ent.IsConstraintError(err) && errors.As(err, &pqErr) && pqErr.Constraint == schema.Idx_email_log_auto_once {
			return ierr.WithError(err).
				WithHint("An automatic email was already sent for this document").
				WithReportableDetails(map[string]any{"entity_id": e.EntityID, "template_key": e.TemplateKey}).
				Mark(ierr.ErrAlreadyExists)
		}
		r.log.Error(ctx, "error creating email log", "error", err, "email_log_id", e.ID, "entity_id", e.EntityID)
		return ierr.WithError(err).
			WithHint("Failed to create email log").
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return nil
}

func (r *emailLogRepository) Get(ctx context.Context, id string) (*domainEmailLog.EmailLog, error) {
	span := StartRepositorySpan(ctx, "email_log", "get", map[string]interface{}{"email_log_id": id})
	defer FinishSpan(span)

	row, err := r.client.Reader(ctx).EmailLog.Query().
		Where(
			emaillog.ID(id),
			emaillog.TenantID(types.GetTenantID(ctx)),
			emaillog.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, r.notFoundOrDatabase(err, "Email log was not found", map[string]any{"email_log_id": id})
	}
	SetSpanSuccess(span)
	return domainEmailLog.FromEnt(row), nil
}

func (r *emailLogRepository) Update(ctx context.Context, e *domainEmailLog.EmailLog) error {
	span := StartRepositorySpan(ctx, "email_log", "update", map[string]interface{}{"email_log_id": e.ID})
	defer FinishSpan(span)

	_, err := r.client.Writer(ctx).EmailLog.UpdateOneID(e.ID).
		Where(
			emaillog.TenantID(types.GetTenantID(ctx)),
			emaillog.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetNillableProviderMessageID(e.ProviderMessageID).
		SetDeliveryStatus(e.DeliveryStatus).
		SetNillableStatusReason(e.StatusReason).
		SetNillableSentAt(e.SentAt).
		SetNillableDeliveredAt(e.DeliveredAt).
		SetMetadata(e.Metadata).
		SetUpdatedAt(clock.Now()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return r.notFoundOrDatabase(err, "Email log was not found", map[string]any{"email_log_id": e.ID})
	}
	SetSpanSuccess(span)
	return nil
}

func (r *emailLogRepository) notFoundOrDatabase(err error, hint string, details map[string]any) error {
	if ent.IsNotFound(err) {
		return ierr.WithError(err).WithHint(hint).WithReportableDetails(details).Mark(ierr.ErrNotFound)
	}
	return ierr.WithError(err).WithHint("Failed to load email log").Mark(ierr.ErrDatabase)
}
