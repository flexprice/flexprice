package ent

import (
	"context"
	"errors"
	"fmt"

	"entgo.io/ent/dialect/sql"

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
	client    postgres.IClient
	log       *logger.Logger
	queryOpts EmailLogQueryOptions
}

func NewEmailLogRepository(client postgres.IClient, log *logger.Logger) domainEmailLog.Repository {
	return &emailLogRepository{client: client, log: log, queryOpts: EmailLogQueryOptions{}}
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

func (r *emailLogRepository) List(ctx context.Context, filter *types.EmailLogFilter) ([]*domainEmailLog.EmailLog, error) {
	if filter == nil {
		filter = types.NewEmailLogFilter()
	}
	span := StartRepositorySpan(ctx, "email_log", "list", map[string]interface{}{"filter": filter})
	defer FinishSpan(span)

	query := r.queryOpts.applyEntityQueryOptions(filter, r.client.Reader(ctx).EmailLog.Query())
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	rows, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list email logs").
			WithReportableDetails(map[string]any{"filter": filter}).
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return domainEmailLog.FromEntList(rows), nil
}

func (r *emailLogRepository) Count(ctx context.Context, filter *types.EmailLogFilter) (int, error) {
	if filter == nil {
		filter = types.NewEmailLogFilter()
	}
	span := StartRepositorySpan(ctx, "email_log", "count", map[string]interface{}{"filter": filter})
	defer FinishSpan(span)

	query := ApplyBaseFilters(ctx, r.client.Reader(ctx).EmailLog.Query(), filter, r.queryOpts)
	query = r.queryOpts.applyEntityQueryOptions(filter, query)

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count email logs").
			WithReportableDetails(map[string]any{"filter": filter}).
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return count, nil
}

func (r *emailLogRepository) ListEnvironmentsWithQueuedEmails(ctx context.Context) ([]types.TenantEnvironment, error) {
	span := StartRepositorySpan(ctx, "email_log", "list_environments_with_queued", nil)
	defer FinishSpan(span)

	var envs []types.TenantEnvironment
	err := r.client.Reader(ctx).EmailLog.Query().
		Where(
			emaillog.Status(string(types.StatusPublished)),
			emaillog.DeliveryStatus(types.EmailDeliveryStatusQueued),
		).
		GroupBy(emaillog.FieldTenantID, emaillog.FieldEnvironmentID).
		Scan(ctx, &envs)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list environments with queued emails").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return envs, nil
}

func (r *emailLogRepository) ListQueued(ctx context.Context, limit int) ([]*domainEmailLog.EmailLog, error) {
	span := StartRepositorySpan(ctx, "email_log", "list_queued", map[string]interface{}{"limit": limit})
	defer FinishSpan(span)

	rows, err := r.client.Reader(ctx).EmailLog.Query().
		Where(
			emaillog.TenantID(types.GetTenantID(ctx)),
			emaillog.EnvironmentID(types.GetEnvironmentID(ctx)),
			emaillog.Status(string(types.StatusPublished)),
			emaillog.DeliveryStatus(types.EmailDeliveryStatusQueued),
		).
		Order(
			func(s *sql.Selector) {
				s.OrderExpr(sql.Expr(fmt.Sprintf("CASE WHEN %s = '%s' THEN 0 ELSE 1 END", s.C(emaillog.FieldTrigger), types.EmailTriggerManual)))
			},
			ent.Asc(emaillog.FieldCreatedAt),
		).
		Limit(limit).
		All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list queued emails").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainEmailLog.FromEntList(rows), nil
}

func (r *emailLogRepository) notFoundOrDatabase(err error, hint string, details map[string]any) error {
	if ent.IsNotFound(err) {
		return ierr.WithError(err).WithHint(hint).WithReportableDetails(details).Mark(ierr.ErrNotFound)
	}
	return ierr.WithError(err).WithHint("Failed to load email log").Mark(ierr.ErrDatabase)
}

type EmailLogQuery = *ent.EmailLogQuery

// EmailLogQueryOptions implements query options for email log filtering and sorting.
type EmailLogQueryOptions struct{}

func (o EmailLogQueryOptions) ApplyTenantFilter(ctx context.Context, query EmailLogQuery) EmailLogQuery {
	return query.Where(emaillog.TenantID(types.GetTenantID(ctx)))
}

func (o EmailLogQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query EmailLogQuery) EmailLogQuery {
	return query.Where(emaillog.EnvironmentID(types.GetEnvironmentID(ctx)))
}

func (o EmailLogQueryOptions) ApplyStatusFilter(query EmailLogQuery, status string) EmailLogQuery {
	if status == "" {
		return query.Where(emaillog.Status(string(types.StatusPublished)))
	}
	return query.Where(emaillog.Status(status))
}

func (o EmailLogQueryOptions) ApplySortFilter(query EmailLogQuery, field string, order string) EmailLogQuery {
	field = o.GetFieldName(field)
	if field == "" {
		field = emaillog.FieldCreatedAt
	}
	if order == types.OrderAsc {
		return query.Order(ent.Asc(field), ent.Asc(emaillog.FieldID))
	}
	return query.Order(ent.Desc(field), ent.Desc(emaillog.FieldID))
}

func (o EmailLogQueryOptions) ApplyPaginationFilter(query EmailLogQuery, limit int, offset int) EmailLogQuery {
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o EmailLogQueryOptions) GetFieldName(field string) string {
	if emaillog.ValidColumn(field) {
		return field
	}
	return ""
}

func (o EmailLogQueryOptions) applyEntityQueryOptions(f *types.EmailLogFilter, query EmailLogQuery) EmailLogQuery {
	if f.EntityType != "" {
		query = query.Where(emaillog.EntityType(f.EntityType))
	}
	if len(f.EntityIDs) > 0 {
		query = query.Where(emaillog.EntityIDIn(f.EntityIDs...))
	}
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil {
			query = query.Where(emaillog.CreatedAtGTE(*f.StartTime))
		}
		if f.EndTime != nil {
			query = query.Where(emaillog.CreatedAtLTE(*f.EndTime))
		}
	}
	return query
}
