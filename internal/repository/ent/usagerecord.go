package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/predicate"
	"github.com/flexprice/flexprice/ent/usagerecord"
	domainUsageRecord "github.com/flexprice/flexprice/internal/domain/usagerecord"
	"github.com/flexprice/flexprice/internal/dsl"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

// usageRecordRepository implements domainUsageRecord.Repository against the Ent client.
type usageRecordRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts UsageRecordQueryOptions
}

// NewUsageRecordRepository creates a new usage record repository.
func NewUsageRecordRepository(client postgres.IClient, log *logger.Logger) domainUsageRecord.Repository {
	return &usageRecordRepository{
		client:    client,
		log:       log,
		queryOpts: UsageRecordQueryOptions{},
	}
}

func (r *usageRecordRepository) Create(ctx context.Context, rec *domainUsageRecord.UsageRecord) error {
	client := r.client.Writer(ctx)

	r.log.Debug(ctx, "creating usage record",
		"usage_record_id", rec.ID,
		"tenant_id", rec.TenantID,
		"subscription_id", rec.SubscriptionID,
	)

	span := StartRepositorySpan(ctx, "usage_record", "create", map[string]interface{}{
		"usage_record_id": rec.ID,
		"subscription_id": rec.SubscriptionID,
	})
	defer FinishSpan(span)

	if rec.EnvironmentID == "" {
		rec.EnvironmentID = types.GetEnvironmentID(ctx)
	}
	if rec.Syncs == nil {
		rec.Syncs = map[string]types.UsageRecordSyncEntry{}
	}

	created, err := client.UsageRecord.Create().
		SetID(rec.ID).
		SetTenantID(rec.TenantID).
		SetCustomerID(rec.CustomerID).
		SetCustomerExternalID(rec.CustomerExternalID).
		SetSubscriptionID(rec.SubscriptionID).
		SetPlanID(rec.PlanID).
		SetQuantity(rec.Quantity).
		SetAmount(rec.Amount).
		SetCurrency(rec.Currency).
		SetPeriodStart(rec.PeriodStart).
		SetPeriodEnd(rec.PeriodEnd).
		SetSynced(rec.Synced).
		SetSyncs(rec.Syncs).
		SetStatus(string(rec.Status)).
		SetCreatedAt(rec.CreatedAt).
		SetUpdatedAt(rec.UpdatedAt).
		SetCreatedBy(rec.CreatedBy).
		SetUpdatedBy(rec.UpdatedBy).
		SetEnvironmentID(rec.EnvironmentID).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsConstraintError(err) {
			return ierr.WithError(err).
				WithHint("Failed to create usage record").
				WithReportableDetails(map[string]any{
					"subscription_id": rec.SubscriptionID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to create usage record").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	*rec = *domainUsageRecord.FromEnt(created)
	return nil
}

func (r *usageRecordRepository) ExistsForPeriod(ctx context.Context, subscriptionID string, periodStart, periodEnd time.Time) (bool, error) {
	client := r.client.Reader(ctx)

	span := StartRepositorySpan(ctx, "usage_record", "exists_for_period", map[string]interface{}{
		"subscription_id": subscriptionID,
		"period_start":    periodStart,
		"period_end":      periodEnd,
	})
	defer FinishSpan(span)

	exists, err := client.UsageRecord.Query().
		Where(
			usagerecord.TenantID(types.GetTenantID(ctx)),
			usagerecord.EnvironmentID(types.GetEnvironmentID(ctx)),
			usagerecord.SubscriptionID(subscriptionID),
			usagerecord.PeriodStart(periodStart),
			usagerecord.PeriodEnd(periodEnd),
			usagerecord.StatusEQ(string(types.StatusPublished)),
		).
		Exist(ctx)

	if err != nil {
		SetSpanError(span, err)
		return false, ierr.WithError(err).
			WithHint("Failed to check for an existing usage record").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return exists, nil
}

// submissionWindow bounds ListUnsynced to rows a marketplace can still accept. None of AWS, GCP or
// Azure take a report older than this, so an older row would be fetched and rejected on every run.
const submissionWindow = 24 * time.Hour

func (r *usageRecordRepository) ListUnsynced(ctx context.Context, tenantID, environmentID string) ([]*domainUsageRecord.UsageRecord, error) {
	client := r.client.Reader(ctx)

	span := StartRepositorySpan(ctx, "usage_record", "list_unsynced", map[string]interface{}{
		"tenant_id":      tenantID,
		"environment_id": environmentID,
	})
	defer FinishSpan(span)

	records, err := client.UsageRecord.Query().
		Where(
			usagerecord.TenantID(tenantID),
			usagerecord.EnvironmentID(environmentID),
			usagerecord.Synced(false),
			usagerecord.StatusEQ(string(types.StatusPublished)),
			usagerecord.PeriodEndGTE(time.Now().UTC().Add(-submissionWindow)),
		).
		All(ctx)

	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list unsynced usage records").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainUsageRecord.FromEntList(records), nil
}

// MarkSynced writes the record's syncs map and the synced flag. Plain read-modify-write is safe:
// the reporting cron processes records sequentially and Temporal's SKIP overlap policy stops two
// runs touching the same rows at once, so there are no concurrent writers to race.
func (r *usageRecordRepository) MarkSynced(ctx context.Context, id string, syncs map[string]types.UsageRecordSyncEntry, synced bool) error {
	client := r.client.Writer(ctx)

	span := StartRepositorySpan(ctx, "usage_record", "mark_synced", map[string]interface{}{
		"usage_record_id": id,
	})
	defer FinishSpan(span)

	if syncs == nil {
		syncs = map[string]types.UsageRecordSyncEntry{}
	}

	affected, err := client.UsageRecord.Update().
		Where(
			usagerecord.ID(id),
			usagerecord.TenantID(types.GetTenantID(ctx)),
			usagerecord.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetSyncs(syncs).
		SetSynced(synced).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to mark usage record as synced").
			Mark(ierr.ErrDatabase)
	}
	if affected == 0 {
		return ierr.NewErrorf("usage record %s was not found", id).
			WithHintf("Usage record with ID %s was not found", id).
			Mark(ierr.ErrNotFound)
	}

	SetSpanSuccess(span)
	return nil
}

// UsageRecordQuery type alias for better readability.
type UsageRecordQuery = *ent.UsageRecordQuery

// UsageRecordQueryOptions implements BaseQueryOptions for usage record queries.
type UsageRecordQueryOptions struct{}

func (o UsageRecordQueryOptions) ApplyTenantFilter(ctx context.Context, query UsageRecordQuery) UsageRecordQuery {
	return query.Where(usagerecord.TenantIDEQ(types.GetTenantID(ctx)))
}

func (o UsageRecordQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query UsageRecordQuery) UsageRecordQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(usagerecord.EnvironmentIDEQ(environmentID))
	}
	return query
}

func (o UsageRecordQueryOptions) ApplyStatusFilter(query UsageRecordQuery, status string) UsageRecordQuery {
	if status == "" {
		return query.Where(usagerecord.StatusEQ(string(types.StatusPublished)))
	}
	return query.Where(usagerecord.StatusEQ(status))
}

func (o UsageRecordQueryOptions) ApplySortFilter(query UsageRecordQuery, field string, order string) UsageRecordQuery {
	// Guard the resolved name rather than the caller's: an unknown column resolves to "", and ordering
	// by "" attaches an error to the whole query instead of leaving it unsorted.
	fieldName := o.GetFieldName(field)
	if fieldName == "" {
		return query
	}
	if order == types.OrderDesc {
		return query.Order(ent.Desc(fieldName))
	}
	return query.Order(ent.Asc(fieldName))
}

func (o UsageRecordQueryOptions) ApplyPaginationFilter(query UsageRecordQuery, limit int, offset int) UsageRecordQuery {
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

// GetFieldName resolves a caller-supplied field to a usage_records column, returning "" if no such
// column exists. Validation is delegated to the generated schema so new columns need no change here.
func (o UsageRecordQueryOptions) GetFieldName(field string) string {
	if usagerecord.ValidColumn(field) {
		return field
	}
	return ""
}

func (o UsageRecordQueryOptions) GetFieldResolver(field string) (string, error) {
	fieldName := o.GetFieldName(field)
	if fieldName == "" {
		return "", ierr.NewErrorf("unknown field name '%s' in usage record query", field).
			Mark(ierr.ErrValidation)
	}
	return fieldName, nil
}

// applyEntityQueryOptions applies usage-record-specific filters: the owning entity columns, currency,
// an exact window, and synced — plus the generic DSL Filters/Sort, which is how range predicates such
// as the marketplaces' submission-window floor are expressed.
func (o UsageRecordQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.UsageRecordFilter, query UsageRecordQuery) (UsageRecordQuery, error) {
	if f == nil {
		return query, nil
	}
	if f.SubscriptionID != "" {
		query = query.Where(usagerecord.SubscriptionIDEQ(f.SubscriptionID))
	}
	if f.CustomerID != "" {
		query = query.Where(usagerecord.CustomerIDEQ(f.CustomerID))
	}
	if f.CustomerExternalID != "" {
		query = query.Where(usagerecord.CustomerExternalIDEQ(f.CustomerExternalID))
	}
	if f.PlanID != "" {
		query = query.Where(usagerecord.PlanIDEQ(f.PlanID))
	}
	if f.Currency != "" {
		query = query.Where(usagerecord.CurrencyEQ(f.Currency))
	}
	if f.PeriodStart != nil {
		query = query.Where(usagerecord.PeriodStartEQ(*f.PeriodStart))
	}
	if f.PeriodEnd != nil {
		query = query.Where(usagerecord.PeriodEndEQ(*f.PeriodEnd))
	}
	if f.Synced != nil {
		query = query.Where(usagerecord.SyncedEQ(*f.Synced))
	}
	// Bounds when the row was written, not the usage window it covers: that is PeriodStart/PeriodEnd
	// above, and range comparisons over it go through the DSL filters below.
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil {
			query = query.Where(usagerecord.CreatedAtGTE(*f.StartTime))
		}
		if f.EndTime != nil {
			query = query.Where(usagerecord.CreatedAtLTE(*f.EndTime))
		}
	}

	var err error
	if f.Filters != nil {
		query, err = dsl.ApplyFilters[UsageRecordQuery, predicate.UsageRecord](
			query,
			f.Filters,
			o.GetFieldResolver,
			func(p dsl.Predicate) predicate.UsageRecord { return predicate.UsageRecord(p) },
		)
		if err != nil {
			return nil, err
		}
	}

	if f.Sort != nil {
		query, err = dsl.ApplySorts[UsageRecordQuery, usagerecord.OrderOption](
			query,
			f.Sort,
			o.GetFieldResolver,
			func(o dsl.OrderFunc) usagerecord.OrderOption { return usagerecord.OrderOption(o) },
		)
		if err != nil {
			return nil, err
		}
	}

	return query, nil
}

// List returns the usage records matching filter.
func (r *usageRecordRepository) List(ctx context.Context, filter *types.UsageRecordFilter) ([]*domainUsageRecord.UsageRecord, error) {
	client := r.client.Reader(ctx)

	span := StartRepositorySpan(ctx, "usage_record", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	query := client.UsageRecord.Query()
	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	query = ApplyPagination(query, filter, r.queryOpts)
	query = ApplySorting(query, filter, r.queryOpts)

	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return nil, err
	}

	records, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list usage records").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainUsageRecord.FromEntList(records), nil
}
