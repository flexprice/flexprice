package ent

import (
	"context"
	"database/sql"
	"time"

	"github.com/lib/pq"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/collectionplaybook"
	"github.com/flexprice/flexprice/ent/collectionplaybookrun"
	"github.com/flexprice/flexprice/ent/collectionplaybooksteprun"
	"github.com/flexprice/flexprice/ent/collectiontask"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/collections"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

// Raw SQL below follows the board queries: $1 tenant, $2 environment, $3 the live record status.

// openInvoiceSQL is the WHERE clause of an invoice a playbook may still chase: finalized, unpaid, with a balance and
// a due date. It expects the invoice aliased i.
const openInvoiceSQL = `i.tenant_id = $1 AND i.environment_id = $2 AND i.status = $3
  AND i.invoice_status = 'FINALIZED' AND i.payment_status NOT IN ('SUCCEEDED', 'OVERPAID')
  AND i.amount_remaining > 0 AND i.due_date IS NOT NULL`

// playbookCandidatesSQL lists open invoices due in [$4, $5) that no run holds and that never ran a playbook, except
// a run that stopped because its playbook was deleted. $6..$11 are the audience: kind, metadata key, value, currency,
// amount, overdue-before instant. Ordered by due date, then id; $12 is the limit.
const playbookCandidatesSQL = `
SELECT i.id, COALESCE(i.invoice_number, ''), i.customer_id, COALESCE(c.name, ''), lower(i.currency), i.amount_remaining, i.due_date
FROM invoices i
LEFT JOIN customers c ON c.id = i.customer_id AND c.tenant_id = i.tenant_id AND c.environment_id = i.environment_id
WHERE ` + openInvoiceSQL + `
  AND i.due_date >= $4 AND i.due_date < $5
  AND NOT EXISTS (
    SELECT 1 FROM collection_playbook_runs r
    WHERE r.tenant_id = i.tenant_id AND r.environment_id = i.environment_id AND r.invoice_id = i.id AND r.status = $3
      AND NOT (r.state = 'stopped' AND r.stop_reason = 'playbook_deleted'))
  AND (
    $6 = 'all'
    OR ($6 = 'segment' AND c.metadata IS NOT NULL AND lower(c.metadata ->> $7) = lower($8))
    OR ($6 = 'amount_over' AND lower(i.currency) = $9 AND i.amount_remaining > $10::numeric)
    OR ($6 = 'days_overdue_over' AND i.due_date < $11))
ORDER BY i.due_date, i.id
LIMIT $12`

// stopFinishedRunsSQL stops every live run whose invoice is gone, no longer finalized, paid or settled. $4 is now.
const stopFinishedRunsSQL = `
UPDATE collection_playbook_runs r
SET state = 'stopped', stop_reason = f.reason, ended_at = $4, next_step_at = NULL, updated_at = $4
FROM (
  SELECT r2.id,
    CASE WHEN i.id IS NULL OR i.status <> $3 OR i.invoice_status <> 'FINALIZED' THEN 'voided' ELSE 'paid' END AS reason
  FROM collection_playbook_runs r2
  LEFT JOIN invoices i ON i.id = r2.invoice_id AND i.tenant_id = r2.tenant_id AND i.environment_id = r2.environment_id
  WHERE r2.tenant_id = $1 AND r2.environment_id = $2 AND r2.status = $3 AND r2.state IN ('active', 'paused')
    AND (i.id IS NULL OR i.status <> $3 OR i.invoice_status <> 'FINALIZED'
         OR i.payment_status IN ('SUCCEEDED', 'OVERPAID') OR i.amount_remaining <= 0)
) f
WHERE r.id = f.id AND r.tenant_id = $1 AND r.environment_id = $2
RETURNING r.id`

const skipStoppedStepsSQL = `
UPDATE collection_playbook_step_runs
SET state = 'skipped', skip_reason = $4, updated_at = $5
WHERE tenant_id = $1 AND environment_id = $2 AND status = $3 AND run_id = ANY($6) AND state = 'pending'`

// dueRunIDsSQL lists active runs of live playbooks with a pending step due by $4; $5 is the limit.
const dueRunIDsSQL = `
SELECT DISTINCT r.id
FROM collection_playbook_runs r
JOIN collection_playbooks p ON p.id = r.playbook_id AND p.tenant_id = r.tenant_id AND p.environment_id = r.environment_id
  AND p.status = $3 AND p.state = 'live'
JOIN collection_playbook_step_runs s ON s.run_id = r.id AND s.tenant_id = r.tenant_id AND s.environment_id = r.environment_id
  AND s.status = $3 AND s.state = 'pending' AND s.due_at <= $4
WHERE r.tenant_id = $1 AND r.environment_id = $2 AND r.status = $3 AND r.state = 'active'
ORDER BY r.id
LIMIT $5`

// runViewsSQL pages a playbook's runs in the states $5 with their invoices; $6 limit, $7 offset.
const runViewsSQL = `
SELECT r.id, COALESCE(i.invoice_number, ''), COALESCE(c.name, ''), COALESCE(lower(i.currency), ''),
  COALESCE(i.amount_remaining, 0), count(*) OVER ()
FROM collection_playbook_runs r
LEFT JOIN invoices i ON i.id = r.invoice_id AND i.tenant_id = r.tenant_id AND i.environment_id = r.environment_id
LEFT JOIN customers c ON c.id = r.customer_id AND c.tenant_id = r.tenant_id AND c.environment_id = r.environment_id
WHERE r.tenant_id = $1 AND r.environment_id = $2 AND r.status = $3 AND r.playbook_id = $4 AND r.state = ANY($5)
ORDER BY r.invoice_due_date, r.id
LIMIT $6 OFFSET $7`

const lastCustomerEmailSQL = `
SELECT max(s.executed_at)
FROM collection_playbook_step_runs s
JOIN collection_playbook_runs r ON r.id = s.run_id AND r.tenant_id = s.tenant_id AND r.environment_id = s.environment_id
WHERE s.tenant_id = $1 AND s.environment_id = $2 AND s.status = $3 AND r.customer_id = $4
  AND s.kind = 'email' AND s.state = 'done'`

// playbookInProgressSQL counts live runs per playbook in $4.
const playbookInProgressSQL = `
SELECT playbook_id, count(*) FROM collection_playbook_runs
WHERE tenant_id = $1 AND environment_id = $2 AND status = $3 AND playbook_id = ANY($4) AND state IN ('active', 'paused')
GROUP BY playbook_id`

// playbookRemindersSQL counts email steps run since $5 per playbook in $4.
const playbookRemindersSQL = `
SELECT r.playbook_id, count(*)
FROM collection_playbook_step_runs s
JOIN collection_playbook_runs r ON r.id = s.run_id AND r.tenant_id = s.tenant_id AND r.environment_id = s.environment_id
WHERE s.tenant_id = $1 AND s.environment_id = $2 AND s.status = $3 AND r.playbook_id = ANY($4)
  AND s.kind = 'email' AND s.state = 'done' AND s.executed_at >= $5
GROUP BY r.playbook_id`

// playbookRecoverySQL takes each invoice reminded by a playbook since $5 once, and whether it was paid within 7
// days after one of those reminders; it then counts and totals per playbook and currency.
const playbookRecoverySQL = `
WITH reminded AS (
  SELECT r.playbook_id, r.invoice_id, max(lower(i.currency)) AS currency, max(i.amount_paid) AS amount_paid,
    bool_or(i.paid_at IS NOT NULL AND i.payment_status IN ('SUCCEEDED', 'OVERPAID')
            AND i.paid_at >= s.executed_at AND i.paid_at <= s.executed_at + interval '7 days') AS paid_within_7d
  FROM collection_playbook_runs r
  JOIN collection_playbook_step_runs s ON s.run_id = r.id AND s.tenant_id = r.tenant_id AND s.environment_id = r.environment_id
    AND s.status = $3 AND s.kind = 'email' AND s.state = 'done' AND s.executed_at >= $5
  JOIN invoices i ON i.id = r.invoice_id AND i.tenant_id = r.tenant_id AND i.environment_id = r.environment_id
  WHERE r.tenant_id = $1 AND r.environment_id = $2 AND r.status = $3 AND r.playbook_id = ANY($4)
  GROUP BY r.playbook_id, r.invoice_id
)
SELECT playbook_id, currency, count(*), count(*) FILTER (WHERE paid_within_7d),
  COALESCE(sum(amount_paid) FILTER (WHERE paid_within_7d), 0)
FROM reminded
GROUP BY playbook_id, currency`

// liveEnvironmentsSQL is the scheduler's one cross-tenant read: the environments holding a live playbook.
const liveEnvironmentsSQL = `
SELECT DISTINCT tenant_id, COALESCE(environment_id, '')
FROM collection_playbooks
WHERE status = $1 AND state = 'live'
ORDER BY 1, 2`

type playbookRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

func NewPlaybookRepository(client postgres.IClient, log *logger.Logger) collections.PlaybookRepository {
	return &playbookRepository{client: client, log: log}
}

type playbookRunRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

func NewPlaybookRunRepository(client postgres.IClient, log *logger.Logger) collections.PlaybookRunRepository {
	return &playbookRunRepository{client: client, log: log}
}

func playbookNotFound(id string) error {
	return ierr.NewErrorf("playbook %s not found", id).
		WithHintf("Playbook %s was not found", id).
		WithReportableDetails(map[string]any{"playbook_id": id}).
		Mark(ierr.ErrNotFound)
}

func (r *playbookRepository) Create(ctx context.Context, p *collections.Playbook) error {
	span := StartRepositorySpan(ctx, "collection_playbook", "create", map[string]interface{}{"playbook_id": p.ID})
	defer FinishSpan(span)

	if p.EnvironmentID == "" {
		p.EnvironmentID = types.GetEnvironmentID(ctx)
	}
	created, err := r.client.Writer(ctx).CollectionPlaybook.Create().
		SetID(p.ID).
		SetTenantID(p.TenantID).
		SetEnvironmentID(p.EnvironmentID).
		SetStatus(string(types.StatusPublished)).
		SetCreatedAt(p.CreatedAt).
		SetUpdatedAt(p.UpdatedAt).
		SetCreatedBy(p.CreatedBy).
		SetUpdatedBy(p.UpdatedBy).
		SetName(p.Name).
		SetDescription(p.Description).
		SetState(p.State).
		SetPriority(p.Priority).
		SetTrigger(p.Trigger).
		SetAudience(p.Audience).
		SetSteps(lo.Ternary(p.Steps == nil, []types.PlaybookStep{}, p.Steps)).
		SetStops(p.Stops).
		SetMinGapDays(p.MinGapDays).
		SetSkipWeekends(p.SkipWeekends).
		SetTemplateID(p.TemplateID).
		SetVersion(p.Version).
		SetNillableActivatedAt(p.ActivatedAt).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return dbError(err, "Failed to save the playbook")
	}
	SetSpanSuccess(span)
	*p = *playbookFromEnt(created)
	return nil
}

func (r *playbookRepository) Get(ctx context.Context, id string) (*collections.Playbook, error) {
	span := StartRepositorySpan(ctx, "collection_playbook", "get", map[string]interface{}{"playbook_id": id})
	defer FinishSpan(span)

	e, err := r.client.Reader(ctx).CollectionPlaybook.Query().
		Where(
			collectionplaybook.ID(id),
			collectionplaybook.TenantID(types.GetTenantID(ctx)),
			collectionplaybook.EnvironmentID(types.GetEnvironmentID(ctx)),
			collectionplaybook.Status(string(types.StatusPublished)),
		).
		Only(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, playbookNotFound(id)
		}
		return nil, dbError(err, "Failed to read the playbook")
	}
	SetSpanSuccess(span)
	return playbookFromEnt(e), nil
}

func (r *playbookRepository) List(ctx context.Context, filter collections.PlaybookFilter) ([]*collections.Playbook, error) {
	span := StartRepositorySpan(ctx, "collection_playbook", "list", nil)
	defer FinishSpan(span)

	q := r.client.Reader(ctx).CollectionPlaybook.Query().
		Where(
			collectionplaybook.TenantID(types.GetTenantID(ctx)),
			collectionplaybook.EnvironmentID(types.GetEnvironmentID(ctx)),
			collectionplaybook.Status(string(types.StatusPublished)),
		)
	if len(filter.States) > 0 {
		q = q.Where(collectionplaybook.StateIn(filter.States...))
	}
	list, err := q.Order(ent.Asc(collectionplaybook.FieldPriority), ent.Asc(collectionplaybook.FieldCreatedAt), ent.Asc(collectionplaybook.FieldID)).All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, dbError(err, "Failed to list playbooks")
	}
	SetSpanSuccess(span)
	return lo.Map(list, func(e *ent.CollectionPlaybook, _ int) *collections.Playbook { return playbookFromEnt(e) }), nil
}

func (r *playbookRepository) Update(ctx context.Context, p *collections.Playbook, previousVersion int) error {
	span := StartRepositorySpan(ctx, "collection_playbook", "update", map[string]interface{}{"playbook_id": p.ID})
	defer FinishSpan(span)

	updated, err := r.client.Writer(ctx).CollectionPlaybook.UpdateOneID(p.ID).
		Where(
			collectionplaybook.TenantID(types.GetTenantID(ctx)),
			collectionplaybook.EnvironmentID(types.GetEnvironmentID(ctx)),
			collectionplaybook.Status(string(types.StatusPublished)),
			collectionplaybook.Version(previousVersion),
		).
		SetName(p.Name).
		SetDescription(p.Description).
		SetState(p.State).
		SetPriority(p.Priority).
		SetTrigger(p.Trigger).
		SetAudience(p.Audience).
		SetSteps(lo.Ternary(p.Steps == nil, []types.PlaybookStep{}, p.Steps)).
		SetStops(p.Stops).
		SetMinGapDays(p.MinGapDays).
		SetSkipWeekends(p.SkipWeekends).
		SetNillableActivatedAt(p.ActivatedAt).
		SetVersion(previousVersion + 1).
		SetUpdatedAt(clock.Now()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return playbookConflict()
		}
		return dbError(err, "Failed to save the playbook")
	}
	SetSpanSuccess(span)
	*p = *playbookFromEnt(updated)
	return nil
}

func playbookConflict() error {
	return ierr.NewError("playbook changed").
		WithHint("This playbook was changed by someone else. Reload it and try again.").
		Mark(ierr.ErrVersionConflict)
}

func (r *playbookRepository) SetPriorities(ctx context.Context, priorities map[string]int) error {
	span := StartRepositorySpan(ctx, "collection_playbook", "set_priorities", map[string]interface{}{"playbooks": len(priorities)})
	defer FinishSpan(span)

	err := r.client.WithTx(ctx, func(txCtx context.Context) error {
		for id, priority := range priorities {
			_, err := r.client.Writer(txCtx).CollectionPlaybook.UpdateOneID(id).
				Where(
					collectionplaybook.TenantID(types.GetTenantID(txCtx)),
					collectionplaybook.EnvironmentID(types.GetEnvironmentID(txCtx)),
					collectionplaybook.Status(string(types.StatusPublished)),
				).
				SetPriority(priority).
				SetUpdatedAt(clock.Now()).
				SetUpdatedBy(types.GetUserID(txCtx)).
				Save(txCtx)
			if ent.IsNotFound(err) {
				return playbookNotFound(id)
			}
			if err != nil {
				return dbError(err, "Failed to reorder playbooks")
			}
		}
		return nil
	})
	if err != nil {
		SetSpanError(span, err)
		return err
	}
	SetSpanSuccess(span)
	return nil
}

func (r *playbookRepository) Delete(ctx context.Context, p *collections.Playbook) error {
	span := StartRepositorySpan(ctx, "collection_playbook", "delete", map[string]interface{}{"playbook_id": p.ID})
	defer FinishSpan(span)

	_, err := r.client.Writer(ctx).CollectionPlaybook.UpdateOneID(p.ID).
		Where(
			collectionplaybook.TenantID(types.GetTenantID(ctx)),
			collectionplaybook.EnvironmentID(types.GetEnvironmentID(ctx)),
			collectionplaybook.Status(string(types.StatusPublished)),
		).
		SetStatus(string(types.StatusDeleted)).
		SetUpdatedAt(clock.Now()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return playbookNotFound(p.ID)
		}
		return dbError(err, "Failed to delete the playbook")
	}
	SetSpanSuccess(span)
	return nil
}

func (r *playbookRepository) MaxPriority(ctx context.Context) (int, error) {
	list, err := r.client.Reader(ctx).CollectionPlaybook.Query().
		Where(
			collectionplaybook.TenantID(types.GetTenantID(ctx)),
			collectionplaybook.EnvironmentID(types.GetEnvironmentID(ctx)),
			collectionplaybook.Status(string(types.StatusPublished)),
		).
		Order(ent.Desc(collectionplaybook.FieldPriority)).
		Limit(1).
		All(ctx)
	if err != nil {
		return 0, dbError(err, "Failed to read playbook priorities")
	}
	if len(list) == 0 {
		return 0, nil
	}
	return list[0].Priority, nil
}

func (r *playbookRepository) ListLiveEnvironments(ctx context.Context) ([]collections.PlaybookEnvironment, error) {
	span := StartRepositorySpan(ctx, "collection_playbook", "list_live_environments", nil)
	defer FinishSpan(span)

	rows, err := r.client.Reader(ctx).QueryContext(ctx, liveEnvironmentsSQL, string(types.StatusPublished))
	if err != nil {
		SetSpanError(span, err)
		return nil, dbError(err, "Failed to list environments with live playbooks")
	}
	var out []collections.PlaybookEnvironment
	err = scanRows(rows, func() error {
		var env collections.PlaybookEnvironment
		if err := rows.Scan(&env.TenantID, &env.EnvironmentID); err != nil {
			return err
		}
		out = append(out, env)
		return nil
	})
	if err != nil {
		SetSpanError(span, err)
		return nil, dbError(err, "Failed to list environments with live playbooks")
	}
	SetSpanSuccess(span)
	return out, nil
}

func (r *playbookRepository) Stats(ctx context.Context, playbookIDs []string, now time.Time) (map[string]collections.PlaybookStats, error) {
	out := make(map[string]collections.PlaybookStats, len(playbookIDs))
	if len(playbookIDs) == 0 {
		return out, nil
	}
	span := StartRepositorySpan(ctx, "collection_playbook", "stats", map[string]interface{}{"playbooks": len(playbookIDs)})
	defer FinishSpan(span)

	db := r.client.Reader(ctx)
	ids := pq.Array(playbookIDs)
	since := now.Add(-30 * 24 * time.Hour)
	read := func(query string, args []any, scan func(rows *sql.Rows) error) error {
		rows, err := db.QueryContext(ctx, query, append(scopeArgs(ctx), args...)...)
		if err != nil {
			return err
		}
		return scanRows(rows, func() error { return scan(rows) })
	}

	err := read(playbookInProgressSQL, []any{ids}, func(rows *sql.Rows) error {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return err
		}
		s := out[id]
		s.InProgress = n
		out[id] = s
		return nil
	})
	if err == nil {
		err = read(playbookRemindersSQL, []any{ids, since}, func(rows *sql.Rows) error {
			var id string
			var n int
			if err := rows.Scan(&id, &n); err != nil {
				return err
			}
			s := out[id]
			s.RemindersSent30d = n
			out[id] = s
			return nil
		})
	}
	if err == nil {
		err = read(playbookRecoverySQL, []any{ids, since}, func(rows *sql.Rows) error {
			var id, currency string
			var reminded, paid int
			var amount decimal.Decimal
			if err := rows.Scan(&id, &currency, &reminded, &paid, &amount); err != nil {
				return err
			}
			s := out[id]
			s.RemindedInvoices30d += reminded
			s.PaidWithin7d30d += paid
			if amount.IsPositive() {
				s.Recovered30d = append(s.Recovered30d, collections.CurrencyTotal{Currency: currency, Amount: amount})
			}
			out[id] = s
			return nil
		})
	}
	if err != nil {
		SetSpanError(span, err)
		return nil, dbError(err, "Failed to read playbook stats")
	}
	SetSpanSuccess(span)
	return out, nil
}

func playbookFromEnt(e *ent.CollectionPlaybook) *collections.Playbook {
	return &collections.Playbook{
		ID:            e.ID,
		Name:          e.Name,
		Description:   e.Description,
		State:         e.State,
		Priority:      e.Priority,
		Trigger:       e.Trigger,
		Audience:      e.Audience,
		Steps:         e.Steps,
		Stops:         e.Stops,
		MinGapDays:    e.MinGapDays,
		SkipWeekends:  e.SkipWeekends,
		TemplateID:    e.TemplateID,
		Version:       e.Version,
		ActivatedAt:   e.ActivatedAt,
		EnvironmentID: e.EnvironmentID,
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

// --- runs ---

func (r *playbookRunRepository) ListCandidates(ctx context.Context, p *collections.Playbook, now time.Time, limit int) ([]collections.PlaybookCandidate, error) {
	span := StartRepositorySpan(ctx, "collection_playbook_run", "list_candidates", map[string]interface{}{"playbook_id": p.ID})
	defer FinishSpan(span)

	dueFrom, dueBefore := collections.CandidateDueWindow(p, now)
	a := p.Audience
	amount := decimal.Zero
	if a.Amount != nil {
		amount = *a.Amount
	}
	overdueBefore := types.UTCDay(now).AddDate(0, 0, -a.Days)

	args := append(scopeArgs(ctx), dueFrom, dueBefore, string(a.Kind), a.Key, a.Value, a.Currency, amount, overdueBefore, limit)
	rows, err := r.client.Reader(ctx).QueryContext(ctx, playbookCandidatesSQL, args...)
	if err != nil {
		SetSpanError(span, err)
		return nil, dbError(err, "Failed to find invoices for the playbook")
	}
	var out []collections.PlaybookCandidate
	err = scanRows(rows, func() error {
		var c collections.PlaybookCandidate
		if err := rows.Scan(&c.InvoiceID, &c.InvoiceNumber, &c.CustomerID, &c.CustomerName, &c.Currency, &c.AmountRemaining, &c.DueDate); err != nil {
			return err
		}
		out = append(out, c)
		return nil
	})
	if err != nil {
		SetSpanError(span, err)
		return nil, dbError(err, "Failed to find invoices for the playbook")
	}
	SetSpanSuccess(span)
	return out, nil
}

func (r *playbookRunRepository) CreateRun(ctx context.Context, run *collections.PlaybookRun, steps []*collections.PlaybookStepRun) error {
	span := StartRepositorySpan(ctx, "collection_playbook_run", "create", map[string]interface{}{"invoice_id": run.InvoiceID})
	defer FinishSpan(span)

	err := r.client.WithTx(ctx, func(txCtx context.Context) error {
		if run.EnvironmentID == "" {
			run.EnvironmentID = types.GetEnvironmentID(txCtx)
		}
		created, err := r.client.Writer(txCtx).CollectionPlaybookRun.Create().
			SetID(run.ID).
			SetTenantID(run.TenantID).
			SetEnvironmentID(run.EnvironmentID).
			SetStatus(string(types.StatusPublished)).
			SetCreatedAt(run.CreatedAt).
			SetUpdatedAt(run.UpdatedAt).
			SetCreatedBy(run.CreatedBy).
			SetUpdatedBy(run.UpdatedBy).
			SetPlaybookID(run.PlaybookID).
			SetInvoiceID(run.InvoiceID).
			SetCustomerID(run.CustomerID).
			SetState(run.State).
			SetInvoiceDueDate(run.InvoiceDueDate).
			SetNillableNextStepAt(run.NextStepAt).
			SetStartedAt(run.StartedAt).
			SetNillableEndedAt(run.EndedAt).
			Save(txCtx)
		if err != nil {
			if ent.IsConstraintError(err) {
				return ierr.WithError(err).WithHint("This invoice is already in a playbook").Mark(ierr.ErrAlreadyExists)
			}
			return dbError(err, "Failed to start the playbook for the invoice")
		}
		*run = *runFromEnt(created)
		return r.createStepRuns(txCtx, steps)
	})
	if err != nil {
		SetSpanError(span, err)
		return err
	}
	SetSpanSuccess(span)
	return nil
}

func (r *playbookRunRepository) GetRun(ctx context.Context, id string) (*collections.PlaybookRun, error) {
	e, err := r.client.Reader(ctx).CollectionPlaybookRun.Query().
		Where(
			collectionplaybookrun.ID(id),
			collectionplaybookrun.TenantID(types.GetTenantID(ctx)),
			collectionplaybookrun.EnvironmentID(types.GetEnvironmentID(ctx)),
			collectionplaybookrun.Status(string(types.StatusPublished)),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.NewErrorf("playbook run %s not found", id).WithHintf("Playbook run %s was not found", id).Mark(ierr.ErrNotFound)
		}
		return nil, dbError(err, "Failed to read the playbook run")
	}
	return runFromEnt(e), nil
}

func (r *playbookRunRepository) UpdateRun(ctx context.Context, run *collections.PlaybookRun) error {
	update := r.client.Writer(ctx).CollectionPlaybookRun.UpdateOneID(run.ID).
		Where(
			collectionplaybookrun.TenantID(types.GetTenantID(ctx)),
			collectionplaybookrun.EnvironmentID(types.GetEnvironmentID(ctx)),
			collectionplaybookrun.Status(string(types.StatusPublished)),
			collectionplaybookrun.UpdatedAt(run.UpdatedAt),
		).
		SetState(run.State).
		SetStopReason(run.StopReason).
		SetPausedReason(run.PausedReason).
		SetNillableNextStepAt(run.NextStepAt).
		SetNillableEndedAt(run.EndedAt).
		SetUpdatedAt(clock.Now()).
		SetUpdatedBy(types.GetUserID(ctx))
	if run.NextStepAt == nil {
		update = update.ClearNextStepAt()
	}
	updated, err := update.Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return ierr.WithError(err).WithHint("This playbook run was changed meanwhile. Reload and try again.").Mark(ierr.ErrVersionConflict)
		}
		if ent.IsConstraintError(err) {
			return ierr.WithError(err).WithHint("This invoice is already in another playbook").Mark(ierr.ErrAlreadyExists)
		}
		return dbError(err, "Failed to update the playbook run")
	}
	*run = *runFromEnt(updated)
	return nil
}

func (r *playbookRunRepository) ListLiveRuns(ctx context.Context, playbookID, customerID string) ([]*collections.PlaybookRun, error) {
	q := r.client.Reader(ctx).CollectionPlaybookRun.Query().
		Where(
			collectionplaybookrun.TenantID(types.GetTenantID(ctx)),
			collectionplaybookrun.EnvironmentID(types.GetEnvironmentID(ctx)),
			collectionplaybookrun.Status(string(types.StatusPublished)),
			collectionplaybookrun.StateIn(types.LivePlaybookRunStates...),
		)
	if playbookID != "" {
		q = q.Where(collectionplaybookrun.PlaybookID(playbookID))
	}
	if customerID != "" {
		q = q.Where(collectionplaybookrun.CustomerID(customerID))
	}
	list, err := q.Order(ent.Asc(collectionplaybookrun.FieldInvoiceDueDate), ent.Asc(collectionplaybookrun.FieldID)).All(ctx)
	if err != nil {
		return nil, dbError(err, "Failed to list playbook runs")
	}
	return lo.Map(list, func(e *ent.CollectionPlaybookRun, _ int) *collections.PlaybookRun { return runFromEnt(e) }), nil
}

func (r *playbookRunRepository) ListRunViews(ctx context.Context, playbookID string, states []types.PlaybookRunState, offset, limit int) ([]collections.PlaybookRunView, int, error) {
	span := StartRepositorySpan(ctx, "collection_playbook_run", "list_views", map[string]interface{}{"playbook_id": playbookID})
	defer FinishSpan(span)

	stateArgs := lo.Map(states, func(s types.PlaybookRunState, _ int) string { return string(s) })
	rows, err := r.client.Reader(ctx).QueryContext(ctx, runViewsSQL, append(scopeArgs(ctx), playbookID, pq.Array(stateArgs), limit, offset)...)
	if err != nil {
		SetSpanError(span, err)
		return nil, 0, dbError(err, "Failed to list playbook runs")
	}
	type row struct {
		id, number, customer, currency string
		remaining                      decimal.Decimal
	}
	var list []row
	total := 0
	err = scanRows(rows, func() error {
		var x row
		if err := rows.Scan(&x.id, &x.number, &x.customer, &x.currency, &x.remaining, &total); err != nil {
			return err
		}
		list = append(list, x)
		return nil
	})
	if err != nil {
		SetSpanError(span, err)
		return nil, 0, dbError(err, "Failed to list playbook runs")
	}
	if len(list) == 0 {
		SetSpanSuccess(span)
		return []collections.PlaybookRunView{}, total, nil
	}

	ids := lo.Map(list, func(x row, _ int) string { return x.id })
	runs, err := r.client.Reader(ctx).CollectionPlaybookRun.Query().
		Where(
			collectionplaybookrun.IDIn(ids...),
			collectionplaybookrun.TenantID(types.GetTenantID(ctx)),
			collectionplaybookrun.EnvironmentID(types.GetEnvironmentID(ctx)),
		).All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, 0, dbError(err, "Failed to list playbook runs")
	}
	byID := lo.SliceToMap(runs, func(e *ent.CollectionPlaybookRun) (string, *ent.CollectionPlaybookRun) { return e.ID, e })
	steps, err := r.ListStepRuns(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	next := collections.NextStepByRun(steps)

	out := make([]collections.PlaybookRunView, 0, len(list))
	for _, x := range list {
		e, ok := byID[x.id]
		if !ok {
			continue
		}
		out = append(out, collections.PlaybookRunView{
			Run: *runFromEnt(e), InvoiceNumber: x.number, CustomerName: x.customer, Currency: x.currency,
			AmountRemaining: x.remaining, NextStep: next[x.id],
		})
	}
	SetSpanSuccess(span)
	return out, total, nil
}

func (r *playbookRunRepository) StopFinishedRuns(ctx context.Context, now time.Time) (int, error) {
	span := StartRepositorySpan(ctx, "collection_playbook_run", "stop_finished", nil)
	defer FinishSpan(span)

	var stopped []string
	err := r.client.WithTx(ctx, func(txCtx context.Context) error {
		db := r.client.Writer(txCtx)
		rows, err := db.QueryContext(txCtx, stopFinishedRunsSQL, append(scopeArgs(txCtx), now)...)
		if err != nil {
			return err
		}
		if err := scanRows(rows, func() error {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			stopped = append(stopped, id)
			return nil
		}); err != nil {
			return err
		}
		if len(stopped) == 0 {
			return nil
		}
		_, err = db.ExecContext(txCtx, skipStoppedStepsSQL, append(scopeArgs(txCtx), string(types.PlaybookSkipRunStopped), now, pq.Array(stopped))...)
		return err
	})
	if err != nil {
		SetSpanError(span, err)
		return 0, dbError(err, "Failed to stop finished playbook runs")
	}
	SetSpanSuccess(span)
	return len(stopped), nil
}

func (r *playbookRunRepository) ListStepRuns(ctx context.Context, runIDs []string) ([]*collections.PlaybookStepRun, error) {
	if len(runIDs) == 0 {
		return nil, nil
	}
	list, err := r.client.Reader(ctx).CollectionPlaybookStepRun.Query().
		Where(
			collectionplaybooksteprun.RunIDIn(runIDs...),
			collectionplaybooksteprun.TenantID(types.GetTenantID(ctx)),
			collectionplaybooksteprun.EnvironmentID(types.GetEnvironmentID(ctx)),
			collectionplaybooksteprun.Status(string(types.StatusPublished)),
		).
		Order(ent.Asc(collectionplaybooksteprun.FieldDueAt), ent.Asc(collectionplaybooksteprun.FieldCreatedAt), ent.Asc(collectionplaybooksteprun.FieldID)).
		All(ctx)
	if err != nil {
		return nil, dbError(err, "Failed to read playbook steps")
	}
	return lo.Map(list, func(e *ent.CollectionPlaybookStepRun, _ int) *collections.PlaybookStepRun { return stepRunFromEnt(e) }), nil
}

func (r *playbookRunRepository) ListDueRunIDs(ctx context.Context, now time.Time, limit int) ([]string, error) {
	rows, err := r.client.Reader(ctx).QueryContext(ctx, dueRunIDsSQL, append(scopeArgs(ctx), now, limit)...)
	if err != nil {
		return nil, dbError(err, "Failed to find due playbook steps")
	}
	var ids []string
	err = scanRows(rows, func() error {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
		return nil
	})
	if err != nil {
		return nil, dbError(err, "Failed to find due playbook steps")
	}
	return ids, nil
}

func (r *playbookRunRepository) CreateStepRuns(ctx context.Context, steps []*collections.PlaybookStepRun) error {
	return r.createStepRuns(ctx, steps)
}

func (r *playbookRunRepository) createStepRuns(ctx context.Context, steps []*collections.PlaybookStepRun) error {
	if len(steps) == 0 {
		return nil
	}
	builders := make([]*ent.CollectionPlaybookStepRunCreate, 0, len(steps))
	for _, s := range steps {
		if s.EnvironmentID == "" {
			s.EnvironmentID = types.GetEnvironmentID(ctx)
		}
		builders = append(builders, r.client.Writer(ctx).CollectionPlaybookStepRun.Create().
			SetID(s.ID).
			SetTenantID(s.TenantID).
			SetEnvironmentID(s.EnvironmentID).
			SetStatus(string(types.StatusPublished)).
			SetCreatedAt(s.CreatedAt).
			SetUpdatedAt(s.UpdatedAt).
			SetCreatedBy(types.GetUserID(ctx)).
			SetUpdatedBy(types.GetUserID(ctx)).
			SetRunID(s.RunID).
			SetStepID(s.StepID).
			SetKind(s.Kind).
			SetDueAt(s.DueAt).
			SetState(s.State).
			SetSkipReason(s.SkipReason).
			SetResultRef(s.ResultRef).
			SetAttempts(s.Attempts).
			SetError(s.Error).
			SetNillableExecutedAt(s.ExecutedAt))
	}
	if _, err := r.client.Writer(ctx).CollectionPlaybookStepRun.CreateBulk(builders...).Save(ctx); err != nil {
		if ent.IsConstraintError(err) {
			return ierr.WithError(err).WithHint("This step is already scheduled for the invoice").Mark(ierr.ErrAlreadyExists)
		}
		return dbError(err, "Failed to schedule playbook steps")
	}
	return nil
}

func (r *playbookRunRepository) UpdateStepRun(ctx context.Context, step *collections.PlaybookStepRun, previousState types.PlaybookStepRunState) error {
	updated, err := r.client.Writer(ctx).CollectionPlaybookStepRun.UpdateOneID(step.ID).
		Where(
			collectionplaybooksteprun.TenantID(types.GetTenantID(ctx)),
			collectionplaybooksteprun.EnvironmentID(types.GetEnvironmentID(ctx)),
			collectionplaybooksteprun.Status(string(types.StatusPublished)),
			collectionplaybooksteprun.State(previousState),
		).
		SetDueAt(step.DueAt).
		SetKind(step.Kind).
		SetState(step.State).
		SetSkipReason(step.SkipReason).
		SetResultRef(step.ResultRef).
		SetAttempts(step.Attempts).
		SetError(step.Error).
		SetNillableExecutedAt(step.ExecutedAt).
		SetUpdatedAt(clock.Now()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return ierr.WithError(err).WithHint("This playbook step was changed meanwhile").Mark(ierr.ErrVersionConflict)
		}
		return dbError(err, "Failed to update the playbook step")
	}
	*step = *stepRunFromEnt(updated)
	return nil
}

func (r *playbookRunRepository) LastCustomerEmailAt(ctx context.Context, customerID string) (*time.Time, error) {
	var last sql.NullTime
	rows, err := r.client.Reader(ctx).QueryContext(ctx, lastCustomerEmailSQL, append(scopeArgs(ctx), customerID)...)
	if err != nil {
		return nil, dbError(err, "Failed to read the customer's last reminder")
	}
	if err := scanRows(rows, func() error { return rows.Scan(&last) }); err != nil {
		return nil, dbError(err, "Failed to read the customer's last reminder")
	}
	return nullTime(last), nil
}

func (r *playbookRunRepository) CreateTask(ctx context.Context, t *collections.CollectionTask) error {
	if t.EnvironmentID == "" {
		t.EnvironmentID = types.GetEnvironmentID(ctx)
	}
	created, err := r.client.Writer(ctx).CollectionTask.Create().
		SetID(t.ID).
		SetTenantID(t.TenantID).
		SetEnvironmentID(t.EnvironmentID).
		SetStatus(string(types.StatusPublished)).
		SetCreatedAt(t.CreatedAt).
		SetUpdatedAt(t.UpdatedAt).
		SetCreatedBy(t.CreatedBy).
		SetUpdatedBy(t.UpdatedBy).
		SetInvoiceID(t.InvoiceID).
		SetCustomerID(t.CustomerID).
		SetRunID(t.RunID).
		SetStepID(t.StepID).
		SetAssigneeUserID(t.AssigneeUserID).
		SetAssigneeEmail(t.AssigneeEmail).
		SetNote(t.Note).
		SetState(t.State).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return ierr.WithError(err).WithHint("This task was already created").Mark(ierr.ErrAlreadyExists)
		}
		return dbError(err, "Failed to create the task")
	}
	*t = *taskFromEnt(created)
	return nil
}

func (r *playbookRunRepository) GetTask(ctx context.Context, id string) (*collections.CollectionTask, error) {
	e, err := r.client.Reader(ctx).CollectionTask.Query().
		Where(
			collectiontask.ID(id),
			collectiontask.TenantID(types.GetTenantID(ctx)),
			collectiontask.EnvironmentID(types.GetEnvironmentID(ctx)),
			collectiontask.Status(string(types.StatusPublished)),
		).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.NewErrorf("task %s not found", id).WithHintf("Task %s was not found", id).Mark(ierr.ErrNotFound)
		}
		return nil, dbError(err, "Failed to read the task")
	}
	return taskFromEnt(e), nil
}

func (r *playbookRunRepository) UpdateTask(ctx context.Context, t *collections.CollectionTask) error {
	update := r.client.Writer(ctx).CollectionTask.UpdateOneID(t.ID).
		Where(
			collectiontask.TenantID(types.GetTenantID(ctx)),
			collectiontask.EnvironmentID(types.GetEnvironmentID(ctx)),
			collectiontask.Status(string(types.StatusPublished)),
			collectiontask.UpdatedAt(t.UpdatedAt),
		).
		SetState(t.State).
		SetNillableDoneAt(t.DoneAt).
		SetDoneBy(t.DoneBy).
		SetUpdatedAt(clock.Now()).
		SetUpdatedBy(types.GetUserID(ctx))
	if t.DoneAt == nil {
		update = update.ClearDoneAt()
	}
	updated, err := update.Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return ierr.WithError(err).WithHint("This task was changed meanwhile. Reload and try again.").Mark(ierr.ErrVersionConflict)
		}
		return dbError(err, "Failed to update the task")
	}
	*t = *taskFromEnt(updated)
	return nil
}

func (r *playbookRunRepository) ListTasks(ctx context.Context, f collections.TaskFilter, offset, limit int) ([]*collections.CollectionTask, int, error) {
	q := r.client.Reader(ctx).CollectionTask.Query().
		Where(
			collectiontask.TenantID(types.GetTenantID(ctx)),
			collectiontask.EnvironmentID(types.GetEnvironmentID(ctx)),
			collectiontask.Status(string(types.StatusPublished)),
		)
	if f.InvoiceID != "" {
		q = q.Where(collectiontask.InvoiceID(f.InvoiceID))
	}
	if f.CustomerID != "" {
		q = q.Where(collectiontask.CustomerID(f.CustomerID))
	}
	if f.AssigneeUserID != "" {
		q = q.Where(collectiontask.AssigneeUserID(f.AssigneeUserID))
	}
	if len(f.States) > 0 {
		q = q.Where(collectiontask.StateIn(f.States...))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, dbError(err, "Failed to list tasks")
	}
	list, err := q.Order(ent.Desc(collectiontask.FieldCreatedAt), ent.Desc(collectiontask.FieldID)).Offset(offset).Limit(limit).All(ctx)
	if err != nil {
		return nil, 0, dbError(err, "Failed to list tasks")
	}
	return lo.Map(list, func(e *ent.CollectionTask, _ int) *collections.CollectionTask { return taskFromEnt(e) }), total, nil
}

func runFromEnt(e *ent.CollectionPlaybookRun) *collections.PlaybookRun {
	return &collections.PlaybookRun{
		ID: e.ID, PlaybookID: e.PlaybookID, InvoiceID: e.InvoiceID, CustomerID: e.CustomerID, State: e.State,
		StopReason: e.StopReason, PausedReason: e.PausedReason, InvoiceDueDate: e.InvoiceDueDate, NextStepAt: e.NextStepAt,
		StartedAt: e.StartedAt, EndedAt: e.EndedAt, EnvironmentID: e.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID: e.TenantID, Status: types.Status(e.Status), CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
			CreatedBy: e.CreatedBy, UpdatedBy: e.UpdatedBy,
		},
	}
}

func stepRunFromEnt(e *ent.CollectionPlaybookStepRun) *collections.PlaybookStepRun {
	return &collections.PlaybookStepRun{
		ID: e.ID, RunID: e.RunID, StepID: e.StepID, Kind: e.Kind, DueAt: e.DueAt, State: e.State,
		SkipReason: e.SkipReason, ResultRef: e.ResultRef, Attempts: e.Attempts, Error: e.Error, ExecutedAt: e.ExecutedAt,
		TenantID: e.TenantID, EnvironmentID: e.EnvironmentID, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}
}

func taskFromEnt(e *ent.CollectionTask) *collections.CollectionTask {
	return &collections.CollectionTask{
		ID: e.ID, InvoiceID: e.InvoiceID, CustomerID: e.CustomerID, RunID: e.RunID, StepID: e.StepID,
		AssigneeUserID: e.AssigneeUserID, AssigneeEmail: e.AssigneeEmail, Note: e.Note, State: e.State,
		DoneAt: e.DoneAt, DoneBy: e.DoneBy, EnvironmentID: e.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID: e.TenantID, Status: types.Status(e.Status), CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
			CreatedBy: e.CreatedBy, UpdatedBy: e.UpdatedBy,
		},
	}
}
