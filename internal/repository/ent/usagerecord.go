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
//
// NOTE: this file references the generated ent.UsageRecord / ent/usagerecord client, which does
// not exist until `make generate-ent` is run against ent/schema/usagerecord.go. It intentionally
// will not compile until then — see ent/schema/usagerecord.go and the FLE-981 design doc.
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
		SetSyncs(domainUsageRecord.SyncsToMap(rec.Syncs)).
		SetAllProvidersSynced(rec.AllProvidersSynced).
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
			usagerecord.AllProvidersSynced(false),
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

func (r *usageRecordRepository) UpdateSyncResult(
	ctx context.Context,
	id string,
	marketplace domainUsageRecord.Marketplace,
	entry domainUsageRecord.MarketplaceSyncEntry,
	allProvidersSynced bool,
) error {
	client := r.client.Writer(ctx)

	span := StartRepositorySpan(ctx, "usage_record", "update_sync_result", map[string]interface{}{
		"usage_record_id": id,
		"marketplace":     marketplace,
	})
	defer FinishSpan(span)

	existing, err := client.UsageRecord.Query().
		Where(
			usagerecord.ID(id),
			usagerecord.TenantID(types.GetTenantID(ctx)),
			usagerecord.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Usage record with ID %s was not found", id).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to get usage record for sync update").
			Mark(ierr.ErrDatabase)
	}

	syncs := domainUsageRecord.FromEnt(existing).Syncs
	if syncs == nil {
		syncs = make(map[domainUsageRecord.Marketplace]domainUsageRecord.MarketplaceSyncEntry)
	}
	syncs[marketplace] = entry

	_, err = client.UsageRecord.UpdateOneID(id).
		Where(
			usagerecord.TenantID(types.GetTenantID(ctx)),
			usagerecord.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetSyncs(domainUsageRecord.SyncsToMap(syncs)).
		SetAllProvidersSynced(allProvidersSynced).
		SetUpdatedAt(time.Now().UTC()).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Usage record with ID %s was not found", id).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update usage record sync result").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}
