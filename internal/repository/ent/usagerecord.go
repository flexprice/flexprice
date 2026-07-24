package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/usagerecord"
	domainUsageRecord "github.com/flexprice/flexprice/internal/domain/usagerecord"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

// usageRecordRepository implements domainUsageRecord.Repository against the Ent client.
type usageRecordRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

// NewUsageRecordRepository creates a new usage record repository.
func NewUsageRecordRepository(client postgres.IClient, log *logger.Logger) domainUsageRecord.Repository {
	return &usageRecordRepository{
		client: client,
		log:    log,
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
