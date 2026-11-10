package journal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/flexprice/flexprice/ent"
	entJournalEntry "github.com/flexprice/flexprice/ent/journalentry"
	entJournalLine "github.com/flexprice/flexprice/ent/journalline"
	"github.com/flexprice/flexprice/ent/predicate"
	"github.com/flexprice/flexprice/ent/schema"
	domainJournal "github.com/flexprice/flexprice/internal/domain/journal"
	"github.com/flexprice/flexprice/internal/dsl"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	entRepo "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/lib/pq"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type repository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts queryOptions
}

func NewRepository(client postgres.IClient, log *logger.Logger) Repository {
	return &repository{
		client:    client,
		log:       log,
		queryOpts: queryOptions{},
	}
}

// CreateWithLines needs its own transaction: a duplicate key aborts the enclosing one.
func (r *repository) CreateWithLines(ctx context.Context, e *domainJournal.Entry) error {
	span := entRepo.StartRepositorySpan(ctx, "journal_entry", "create_with_lines", map[string]interface{}{
		"journal_entry_id": e.ID(),
		"event_type":       e.EventType(),
		"source_entity_id": e.SourceEntityID(),
	})
	defer entRepo.FinishSpan(span)

	if err := domainJournal.ValidateEntry(e.Lines()); err != nil {
		entRepo.SetSpanError(span, err)
		return err
	}

	err := r.client.WithTx(ctx, func(txCtx context.Context) error {
		tenantID, environmentID, userID := types.GetTenantID(txCtx), types.GetEnvironmentID(txCtx), types.GetUserID(txCtx)
		_, err := r.client.Writer(txCtx).JournalEntry.Create().
			SetID(e.ID()).
			SetTenantID(tenantID).
			SetEnvironmentID(environmentID).
			SetStatus(string(types.StatusPublished)).
			SetEventType(string(e.EventType())).
			SetSourceEntityType(string(e.SourceEntityType())).
			SetSourceEntityID(e.SourceEntityID()).
			SetNillableIdempotencyKey(lo.EmptyableToPtr(e.IdempotencyKey())).
			SetAccountingPeriodID(e.AccountingPeriodID()).
			SetBusinessDate(e.BusinessDate()).
			SetCustomerID(e.CustomerID()).
			SetNillableContractID(lo.EmptyableToPtr(e.ContractID())).
			SetCurrency(e.Currency()).
			SetNillableReversalOf(lo.EmptyableToPtr(e.ReversalOf())).
			SetCreatedBy(userID).
			SetUpdatedBy(userID).
			SetCreatedAt(e.CreatedAt()).
			SetUpdatedAt(e.CreatedAt()).
			Save(txCtx)
		if err != nil {
			return err
		}

		lines := lo.Map(e.Lines(), func(l *domainJournal.Line, _ int) *ent.JournalLineCreate {
			return r.client.Writer(txCtx).JournalLine.Create().
				SetID(l.ID()).
				SetTenantID(tenantID).
				SetEnvironmentID(environmentID).
				SetStatus(string(types.StatusPublished)).
				SetJournalEntryID(e.ID()).
				SetAccount(string(l.Account())).
				SetDirection(string(l.Direction())).
				SetAmount(l.Amount()).
				SetNillablePobID(lo.EmptyableToPtr(l.POBID())).
				SetCreatedBy(userID).
				SetUpdatedBy(userID).
				SetCreatedAt(e.CreatedAt()).
				SetUpdatedAt(e.CreatedAt())
		})
		_, err = r.client.Writer(txCtx).JournalLine.CreateBulk(lines...).Save(txCtx)
		return err
	})
	if err != nil {
		entRepo.SetSpanError(span, err)
		var pqErr *pq.Error
		if ent.IsConstraintError(err) && errors.As(err, &pqErr) &&
			pqErr.Constraint == schema.JournalEntryIdempotencyKeyConstraint {
			return ierr.WithError(err).
				WithHint("Journal entry is already posted").
				WithReportableDetails(map[string]any{
					"idempotency_key": e.IdempotencyKey(),
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to create journal entry").
			Mark(ierr.ErrDatabase)
	}

	entRepo.SetSpanSuccess(span)
	return nil
}

func (r *repository) GetIDByIdempotencyKey(ctx context.Context, key string) (string, error) {
	span := entRepo.StartRepositorySpan(ctx, "journal_entry", "get_id_by_idempotency_key", map[string]interface{}{
		"idempotency_key": key,
	})
	defer entRepo.FinishSpan(span)

	id, err := r.client.Reader(ctx).JournalEntry.Query().
		Where(
			entJournalEntry.TenantID(types.GetTenantID(ctx)),
			entJournalEntry.EnvironmentID(types.GetEnvironmentID(ctx)),
			entJournalEntry.IdempotencyKey(key),
		).
		OnlyID(ctx)
	if err != nil {
		entRepo.SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return "", ierr.WithError(err).
				WithHintf("Journal entry with idempotency key %s was not found", key).
				Mark(ierr.ErrNotFound)
		}
		return "", ierr.WithError(err).
			WithHint("Failed to get journal entry").
			Mark(ierr.ErrDatabase)
	}

	entRepo.SetSpanSuccess(span)
	return id, nil
}

func (r *repository) Get(ctx context.Context, id string) (*domainJournal.Entry, error) {
	span := entRepo.StartRepositorySpan(ctx, "journal_entry", "get", map[string]interface{}{
		"journal_entry_id": id,
	})
	defer entRepo.FinishSpan(span)

	e, err := r.client.Reader(ctx).JournalEntry.Query().
		Where(
			entJournalEntry.ID(id),
			entJournalEntry.TenantID(types.GetTenantID(ctx)),
			entJournalEntry.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)
	if err != nil {
		entRepo.SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Journal entry with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"journal_entry_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHintf("Failed to get journal entry with ID %s", id).
			Mark(ierr.ErrDatabase)
	}

	lines, err := r.listLines(ctx, []string{id})
	if err != nil {
		entRepo.SetSpanError(span, err)
		return nil, err
	}

	entRepo.SetSpanSuccess(span)
	return FromEnt(e, lines[id]), nil
}

func (r *repository) List(ctx context.Context, filter *Filter) ([]*domainJournal.Entry, error) {
	if filter == nil {
		filter = NewFilter()
	}

	span := entRepo.StartRepositorySpan(ctx, "journal_entry", "list", map[string]interface{}{
		"filter": filter,
	})
	defer entRepo.FinishSpan(span)

	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, r.client.Reader(ctx).JournalEntry.Query())
	if err != nil {
		return nil, err
	}
	query = entRepo.ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	entries, err := query.All(ctx)
	if err != nil {
		entRepo.SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list journal entries").
			Mark(ierr.ErrDatabase)
	}

	lines, err := r.listLines(ctx, lo.Map(entries, func(e *ent.JournalEntry, _ int) string { return e.ID }))
	if err != nil {
		entRepo.SetSpanError(span, err)
		return nil, err
	}

	entRepo.SetSpanSuccess(span)
	return lo.Map(entries, func(e *ent.JournalEntry, _ int) *domainJournal.Entry {
		return FromEnt(e, lines[e.ID])
	}), nil
}

// listLines returns the lines of the given entries, grouped by entry ID.
func (r *repository) listLines(ctx context.Context, entryIDs []string) (map[string][]*ent.JournalLine, error) {
	if len(entryIDs) == 0 {
		return nil, nil
	}
	lines, err := r.client.Reader(ctx).JournalLine.Query().
		Where(
			entJournalLine.TenantID(types.GetTenantID(ctx)),
			entJournalLine.EnvironmentID(types.GetEnvironmentID(ctx)),
			entJournalLine.JournalEntryIDIn(entryIDs...),
		).
		Order(ent.Asc(entJournalLine.FieldID)).
		All(ctx)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list journal lines").
			Mark(ierr.ErrDatabase)
	}
	return lo.GroupBy(lines, func(l *ent.JournalLine) string { return l.JournalEntryID }), nil
}

func (r *repository) Count(ctx context.Context, filter *Filter) (int, error) {
	span := entRepo.StartRepositorySpan(ctx, "journal_entry", "count", map[string]interface{}{
		"filter": filter,
	})
	defer entRepo.FinishSpan(span)

	query := entRepo.ApplyBaseFilters(ctx, r.client.Reader(ctx).JournalEntry.Query(), filter, r.queryOpts)
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		return 0, err
	}

	count, err := query.Count(ctx)
	if err != nil {
		entRepo.SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count journal entries").
			Mark(ierr.ErrDatabase)
	}

	entRepo.SetSpanSuccess(span)
	return count, nil
}

// SumLines nets lines per period, account, event type and the requested group keys; earlier periods fold into one opening row.
// e.g. Feb deferred → (opening, 1,100), (Feb, invoice_finalized, +100), (Feb, revenue_offset, −200).
func (r *repository) SumLines(ctx context.Context, f *SumFilter) ([]*LineSum, error) {
	span := entRepo.StartRepositorySpan(ctx, "journal_line", "sum", map[string]interface{}{
		"filter": f,
	})
	defer entRepo.FinishSpan(span)

	args := []any{
		types.GetTenantID(ctx), types.GetEnvironmentID(ctx), string(types.StatusPublished), f.Currency,
		f.PeriodStart.Format(time.DateOnly), f.PeriodEnd.Format(time.DateOnly), string(types.JournalDirectionCredit),
	}
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	where := []string{
		"l.tenant_id = $1", "l.environment_id = $2", "e.status = $3", "e.currency = $4", "p.period_start <= $6::date",
	}
	if len(f.Accounts) > 0 {
		where = append(where, "l.account = ANY("+arg(pq.Array(lo.Map(f.Accounts, func(a types.LedgerAccountKey, _ int) string {
			return string(a)
		})))+")")
	}
	if len(f.CustomerIDs) > 0 {
		where = append(where, "e.customer_id = ANY("+arg(pq.Array(f.CustomerIDs))+")")
	}
	if len(f.ContractIDs) > 0 {
		where = append(where, "e.contract_id = ANY("+arg(pq.Array(f.ContractIDs))+")")
	}
	if len(f.POBIDs) > 0 {
		where = append(where, "l.pob_id = ANY("+arg(pq.Array(f.POBIDs))+")")
	}
	if f.CreatedAtLTE != nil {
		where = append(where, "e.created_at <= "+arg(*f.CreatedAtLTE))
	}
	if f.ExcludeOpening {
		where = append(where, "p.period_start >= $5::date")
	}
	// An ungrouped key selects NULL, so it adds no split.
	groupColumn := func(key types.RevenueReportGroupBy, column string) string {
		return lo.Ternary(lo.Contains(f.GroupBy, key), column, "NULL::varchar")
	}

	query := fmt.Sprintf(`
		SELECT
			CASE WHEN p.period_start < $5::date THEN NULL ELSE p.period_start END,
			l.account,
			CASE WHEN p.period_start < $5::date THEN NULL ELSE e.event_type END,
			%s, %s, %s,
			SUM(CASE WHEN l.direction = $7 THEN l.amount ELSE -l.amount END)
		FROM journal_lines l
		JOIN journal_entries e
			ON e.id = l.journal_entry_id AND e.tenant_id = l.tenant_id AND e.environment_id = l.environment_id
		JOIN accounting_periods p
			ON p.id = e.accounting_period_id AND p.tenant_id = e.tenant_id AND p.environment_id = e.environment_id
		WHERE %s
		GROUP BY 1, 2, 3, 4, 5, 6
		ORDER BY 1 NULLS FIRST, 2, 3, 4, 5, 6`,
		groupColumn(types.RevenueReportGroupByCustomer, "e.customer_id"),
		groupColumn(types.RevenueReportGroupByContract, "e.contract_id"),
		groupColumn(types.RevenueReportGroupByPOB, "l.pob_id"),
		strings.Join(where, " AND "),
	)

	rows, err := r.client.Reader(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		entRepo.SetSpanError(span, err)
		return nil, ierr.WithError(err).WithHint("Failed to sum journal lines").Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var sums []*LineSum
	for rows.Next() {
		var (
			periodStart                   sql.NullTime
			account                       string
			eventType                     sql.NullString
			customerID, contractID, pobID sql.NullString
			net                           decimal.Decimal
		)
		if err := rows.Scan(&periodStart, &account, &eventType, &customerID, &contractID, &pobID, &net); err != nil {
			entRepo.SetSpanError(span, err)
			return nil, ierr.WithError(err).WithHint("Failed to scan journal line sum").Mark(ierr.ErrDatabase)
		}
		sums = append(sums, &LineSum{
			PeriodStart: lo.Ternary(periodStart.Valid, &periodStart.Time, nil),
			Account:     types.LedgerAccountKey(account),
			EventType:   types.JournalEventType(eventType.String),
			CustomerID:  lo.Ternary(customerID.Valid, &customerID.String, nil),
			ContractID:  lo.Ternary(contractID.Valid, &contractID.String, nil),
			POBID:       lo.Ternary(pobID.Valid, &pobID.String, nil),
			Net:         net,
		})
	}
	if err := rows.Err(); err != nil {
		entRepo.SetSpanError(span, err)
		return nil, ierr.WithError(err).WithHint("Failed to read journal line sums").Mark(ierr.ErrDatabase)
	}

	entRepo.SetSpanSuccess(span)
	return sums, nil
}

type journalEntryQuery = *ent.JournalEntryQuery

// queryOptions implements query options for journal entry filtering and sorting
type queryOptions struct{}

func (o queryOptions) ApplyTenantFilter(ctx context.Context, query journalEntryQuery) journalEntryQuery {
	return query.Where(entJournalEntry.TenantID(types.GetTenantID(ctx)))
}

func (o queryOptions) ApplyEnvironmentFilter(ctx context.Context, query journalEntryQuery) journalEntryQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(entJournalEntry.EnvironmentIDEQ(environmentID))
	}
	return query
}

func (o queryOptions) ApplyStatusFilter(query journalEntryQuery, status string) journalEntryQuery {
	if status == "" {
		return query.Where(entJournalEntry.StatusEQ(string(types.StatusPublished)))
	}
	return query.Where(entJournalEntry.Status(status))
}

func (o queryOptions) ApplySortFilter(query journalEntryQuery, field string, order string) journalEntryQuery {
	orderFunc := ent.Desc
	if order == "asc" {
		orderFunc = ent.Asc
	}
	return query.Order(orderFunc(o.GetFieldName(field)))
}

func (o queryOptions) ApplyPaginationFilter(query journalEntryQuery, limit int, offset int) journalEntryQuery {
	query = query.Limit(limit)
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o queryOptions) GetFieldName(field string) string {
	if entJournalEntry.ValidColumn(field) {
		return field
	}
	return ""
}

func (o queryOptions) GetFieldResolver(st string) (string, error) {
	fieldName := o.GetFieldName(st)
	if fieldName == "" {
		return "", ierr.NewErrorf("unknown field name '%s' in journal entry query", st).
			WithHintf("Unknown field name '%s' in journal entry query", st).
			Mark(ierr.ErrValidation)
	}
	return fieldName, nil
}

func (o queryOptions) applyEntityQueryOptions(ctx context.Context, f *Filter, query journalEntryQuery) (journalEntryQuery, error) {
	if f == nil {
		return query, nil
	}

	if len(f.JournalEntryIDs) > 0 {
		query = query.Where(entJournalEntry.IDIn(f.JournalEntryIDs...))
	}
	if len(f.AccountingPeriodIDs) > 0 {
		query = query.Where(entJournalEntry.AccountingPeriodIDIn(f.AccountingPeriodIDs...))
	}
	if len(f.EventTypes) > 0 {
		query = query.Where(entJournalEntry.EventTypeIn(lo.Map(f.EventTypes, func(t types.JournalEventType, _ int) string {
			return string(t)
		})...))
	}
	if len(f.CustomerIDs) > 0 {
		query = query.Where(entJournalEntry.CustomerIDIn(f.CustomerIDs...))
	}
	if len(f.ContractIDs) > 0 {
		query = query.Where(entJournalEntry.ContractIDIn(f.ContractIDs...))
	}
	if f.SourceEntityType != "" {
		query = query.Where(entJournalEntry.SourceEntityType(string(f.SourceEntityType)))
	}
	if len(f.SourceEntityIDs) > 0 {
		query = query.Where(entJournalEntry.SourceEntityIDIn(f.SourceEntityIDs...))
	}
	if len(f.Accounts) > 0 || len(f.POBIDs) > 0 {
		query = query.Where(linesPredicate(ctx, f.Accounts, f.POBIDs))
	}
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil {
			query = query.Where(entJournalEntry.BusinessDateGTE(*f.StartTime))
		}
		if f.EndTime != nil {
			query = query.Where(entJournalEntry.BusinessDateLT(*f.EndTime))
		}
	}

	var err error
	if f.Filters != nil {
		query, err = dsl.ApplyFilters[journalEntryQuery, predicate.JournalEntry](
			query,
			f.Filters,
			o.GetFieldResolver,
			func(p dsl.Predicate) predicate.JournalEntry { return predicate.JournalEntry(p) },
		)
		if err != nil {
			return nil, err
		}
	}
	if f.Sort != nil {
		query, err = dsl.ApplySorts[journalEntryQuery, entJournalEntry.OrderOption](
			query,
			f.Sort,
			o.GetFieldResolver,
			func(o dsl.OrderFunc) entJournalEntry.OrderOption { return entJournalEntry.OrderOption(o) },
		)
		if err != nil {
			return nil, err
		}
	}

	return query, nil
}

// linesPredicate matches entries with a line on one of the accounts and, when given, one of the POBs.
func linesPredicate(ctx context.Context, accounts []types.LedgerAccountKey, pobIDs []string) predicate.JournalEntry {
	return func(s *entsql.Selector) {
		t := entsql.Table(entJournalLine.Table)
		conds := []*entsql.Predicate{
			entsql.EQ(t.C(entJournalLine.FieldTenantID), types.GetTenantID(ctx)),
			entsql.EQ(t.C(entJournalLine.FieldEnvironmentID), types.GetEnvironmentID(ctx)),
		}
		if len(accounts) > 0 {
			conds = append(conds, entsql.In(t.C(entJournalLine.FieldAccount), lo.Map(accounts, func(a types.LedgerAccountKey, _ int) any {
				return string(a)
			})...))
		}
		if len(pobIDs) > 0 {
			conds = append(conds, entsql.In(t.C(entJournalLine.FieldPobID), lo.ToAnySlice(pobIDs)...))
		}
		s.Where(entsql.In(s.C(entJournalEntry.FieldID),
			entsql.Select(t.C(entJournalLine.FieldJournalEntryID)).
				From(t).
				Where(entsql.And(conds...)),
		))
	}
}
