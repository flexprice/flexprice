package ent

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/flexprice/flexprice/internal/domain/addonpricesync"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/lib/pq"
)

type addonPriceSyncRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

func NewAddonPriceSyncRepository(client postgres.IClient, log *logger.Logger) addonpricesync.Repository {
	return &addonPriceSyncRepository{client: client, log: log}
}

// CurrentAddonSequence returns max(prices.sequence) for the addon's published
// usage prices. 0 means no qualifying prices.
func (r *addonPriceSyncRepository) CurrentAddonSequence(ctx context.Context, addonID string) (int64, error) {
	if addonID == "" {
		return 0, ierr.NewError("addon_id is required").Mark(ierr.ErrValidation)
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	query := fmt.Sprintf(`
		SELECT COALESCE(MAX(sequence), 0)
		FROM prices
		WHERE tenant_id = $1
		  AND environment_id = $2
		  AND status = '%s'
		  AND entity_type = '%s'
		  AND entity_id = $3
		  AND type = '%s'
	`,
		string(types.StatusPublished),
		string(types.PRICE_ENTITY_TYPE_ADDON),
		string(types.PRICE_TYPE_USAGE),
	)

	rows, err := r.client.Reader(ctx).QueryContext(ctx, query, tenantID, environmentID, addonID)
	if err != nil {
		return 0, ierr.WithError(err).
			WithHint("Failed to read current addon sequence").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var seq int64
	if rows.Next() {
		if scanErr := rows.Scan(&seq); scanErr != nil {
			return 0, ierr.WithError(scanErr).
				WithHint("Failed to scan addon sequence").
				Mark(ierr.ErrDatabase)
		}
	}
	return seq, rows.Err()
}

// ListBehindAssociations returns still-billing associations of the addon on
// active/trialing subscriptions whose synced_price_sequence is below TargetSeq.
// Still-billing means active, or cancelled with a future end date (cancel at
// period end). Ordered by (synced_price_sequence, id) to bound the LIMIT scan.
// Stamping advances the page — stamped associations fall out of the
// synced_price_sequence < TargetSeq filter on the next call.
func (r *addonPriceSyncRepository) ListBehindAssociations(
	ctx context.Context,
	p addonpricesync.ListBehindAssociationsParams,
) ([]addonpricesync.AddonAssociationRef, error) {
	if p.AddonID == "" {
		return nil, ierr.NewError("addon_id is required").Mark(ierr.ErrValidation)
	}
	limit := p.Limit
	if limit <= 0 {
		limit = DEFAULT_LIMIT
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	span := StartRepositorySpan(ctx, "addon_price_sync", "list_behind_associations", map[string]interface{}{
		"addon_id":   p.AddonID,
		"target_seq": p.TargetSeq,
		"limit":      limit,
	})
	defer FinishSpan(span)

	query := fmt.Sprintf(`
		SELECT aa.id, aa.entity_id, aa.end_date
		FROM addon_associations aa
		JOIN subscriptions s ON s.id = aa.entity_id
		  AND s.tenant_id = $1 AND s.environment_id = $2
		WHERE aa.tenant_id = $1
		  AND aa.environment_id = $2
		  AND aa.status = '%s'
		  AND aa.addon_id = $3
		  AND aa.entity_type = '%s'
		  AND (
		    (aa.addon_status = '%s' AND (aa.end_date IS NULL OR aa.end_date > NOW()))
		    OR (aa.addon_status = '%s' AND aa.end_date > NOW())
		  )
		  AND aa.synced_price_sequence < $4
		  AND s.status = '%s'
		  AND s.subscription_status IN ('%s', '%s')
		ORDER BY aa.synced_price_sequence, aa.id
		LIMIT $5
	`,
		string(types.StatusPublished),
		string(types.AddonAssociationEntityTypeSubscription),
		string(types.AddonStatusActive),
		string(types.AddonStatusCancelled),
		string(types.StatusPublished),
		string(types.SubscriptionStatusActive),
		string(types.SubscriptionStatusTrialing),
	)

	rows, qerr := r.client.Reader(ctx).QueryContext(ctx, query, tenantID, environmentID, p.AddonID, p.TargetSeq, limit)
	if qerr != nil {
		SetSpanError(span, qerr)
		return nil, ierr.WithError(qerr).
			WithHint("Failed to list behind addon associations").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var refs []addonpricesync.AddonAssociationRef
	for rows.Next() {
		var ref addonpricesync.AddonAssociationRef
		var endDate sql.NullTime
		if scanErr := rows.Scan(&ref.AssociationID, &ref.SubscriptionID, &endDate); scanErr != nil {
			SetSpanError(span, scanErr)
			return nil, ierr.WithError(scanErr).
				WithHint("Failed to scan behind addon association row").
				Mark(ierr.ErrDatabase)
		}
		if endDate.Valid {
			ref.EndDate = &endDate.Time
		}
		refs = append(refs, ref)
	}
	if rerr := rows.Err(); rerr != nil {
		SetSpanError(span, rerr)
		return nil, ierr.WithError(rerr).Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return refs, nil
}

// TerminateEndedAddonLineItems caps addon usage line items for the given
// associations to their catalog price's end date, shortening any line whose end
// date is null or later than the ended price (the latter covers a cancel-at-period-end
// line that still points at a now-superseded price). Idempotent.
func (r *addonPriceSyncRepository) TerminateEndedAddonLineItems(
	ctx context.Context,
	p addonpricesync.TerminateEndedAddonLineItemsParams,
) (int, error) {
	if p.AddonID == "" {
		return 0, ierr.NewError("addon_id is required").Mark(ierr.ErrValidation)
	}
	if len(p.AssociationIDs) == 0 {
		return 0, nil
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	userID := types.GetUserID(ctx)

	span := StartRepositorySpan(ctx, "addon_price_sync", "terminate_ended_line_items", map[string]interface{}{
		"addon_id":    p.AddonID,
		"assoc_count": len(p.AssociationIDs),
	})
	defer FinishSpan(span)

	query := fmt.Sprintf(`
		UPDATE subscription_line_items li
		SET end_date   = GREATEST(COALESCE(li.start_date, p.end_date), p.end_date),
		    updated_at = NOW(),
		    updated_by = $4
		FROM prices p, addon_associations aa
		WHERE li.tenant_id          = $1
		  AND li.environment_id     = $2
		  AND li.status             = '%s'
		  AND li.entity_type        = '%s'
		  AND li.addon_association_id = ANY($5)
		  AND li.price_id           = p.id
		  AND p.tenant_id           = $1
		  AND p.environment_id      = $2
		  AND p.entity_id           = $3
		  AND p.entity_type         = '%s'
		  AND p.type                = '%s'
		  AND p.end_date IS NOT NULL
		  AND aa.id                 = li.addon_association_id
		  AND aa.tenant_id          = $1
		  AND aa.environment_id     = $2
		  -- Open lines always close at the price's end. A line that outlives the ended
		  -- price is only shortened when its association is cancelled at period end,
		  -- so an override timeline on an active association is left untouched.
		  AND (li.end_date IS NULL OR (aa.end_date IS NOT NULL AND li.end_date > p.end_date))
	`,
		string(types.StatusPublished),
		string(types.SubscriptionLineItemEntityTypeAddon),
		string(types.PRICE_ENTITY_TYPE_ADDON),
		string(types.PRICE_TYPE_USAGE),
	)

	result, qerr := r.client.Writer(ctx).ExecContext(ctx, query, tenantID, environmentID, p.AddonID, userID, pq.Array(p.AssociationIDs))
	if qerr != nil {
		SetSpanError(span, qerr)
		return 0, ierr.WithError(qerr).
			WithHint("Failed to terminate ended addon line items").
			Mark(ierr.ErrDatabase)
	}
	n, err := result.RowsAffected()
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return int(n), nil
}

// StampAssociations forwards synced_price_sequence to TargetSeq for the given
// associations. Forward-only via GREATEST.
func (r *addonPriceSyncRepository) StampAssociations(
	ctx context.Context,
	p addonpricesync.StampAssociationsParams,
) (int, error) {
	if len(p.AssociationIDs) == 0 {
		return 0, nil
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	userID := types.GetUserID(ctx)

	span := StartRepositorySpan(ctx, "addon_price_sync", "stamp_associations", map[string]interface{}{
		"target_seq":  p.TargetSeq,
		"assoc_count": len(p.AssociationIDs),
	})
	defer FinishSpan(span)

	query := `
		UPDATE addon_associations
		SET synced_price_sequence = GREATEST(synced_price_sequence, $3),
		    updated_at = NOW(),
		    updated_by = $4
		WHERE tenant_id      = $1
		  AND environment_id = $2
		  AND id = ANY($5)
		  AND synced_price_sequence < $3
	`

	result, qerr := r.client.Writer(ctx).ExecContext(ctx, query, tenantID, environmentID, p.TargetSeq, userID, pq.Array(p.AssociationIDs))
	if qerr != nil {
		SetSpanError(span, qerr)
		return 0, ierr.WithError(qerr).
			WithHint("Failed to stamp addon associations").
			Mark(ierr.ErrDatabase)
	}
	n, err := result.RowsAffected()
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return int(n), nil
}

// CountUnsyncedAssociations returns how many of the addon's active recurring
// associations are behind targetSeq. Same eligibility filter as
// ListBehindAssociations, without paging.
func (r *addonPriceSyncRepository) CountUnsyncedAssociations(ctx context.Context, addonID string, targetSeq int64) (int64, error) {
	if addonID == "" {
		return 0, ierr.NewError("addon_id is required").Mark(ierr.ErrValidation)
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	span := StartRepositorySpan(ctx, "addon_price_sync", "count_unsynced_associations", map[string]interface{}{
		"addon_id":   addonID,
		"target_seq": targetSeq,
	})
	defer FinishSpan(span)

	query := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM addon_associations aa
		JOIN subscriptions s ON s.id = aa.entity_id
		  AND s.tenant_id = $1 AND s.environment_id = $2
		WHERE aa.tenant_id = $1
		  AND aa.environment_id = $2
		  AND aa.status = '%s'
		  AND aa.addon_id = $3
		  AND aa.entity_type = '%s'
		  AND (
		    (aa.addon_status = '%s' AND (aa.end_date IS NULL OR aa.end_date > NOW()))
		    OR (aa.addon_status = '%s' AND aa.end_date > NOW())
		  )
		  AND aa.synced_price_sequence < $4
		  AND s.status = '%s'
		  AND s.subscription_status IN ('%s', '%s')
	`,
		string(types.StatusPublished),
		string(types.AddonAssociationEntityTypeSubscription),
		string(types.AddonStatusActive),
		string(types.AddonStatusCancelled),
		string(types.StatusPublished),
		string(types.SubscriptionStatusActive),
		string(types.SubscriptionStatusTrialing),
	)

	rows, qerr := r.client.Reader(ctx).QueryContext(ctx, query, tenantID, environmentID, addonID, targetSeq)
	if qerr != nil {
		SetSpanError(span, qerr)
		return 0, ierr.WithError(qerr).
			WithHint("Failed to count unsynced addon associations").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var count int64
	if rows.Next() {
		if scanErr := rows.Scan(&count); scanErr != nil {
			SetSpanError(span, scanErr)
			return 0, ierr.WithError(scanErr).
				WithHint("Failed to scan unsynced addon association count").
				Mark(ierr.ErrDatabase)
		}
	}
	if rerr := rows.Err(); rerr != nil {
		SetSpanError(span, rerr)
		return 0, ierr.WithError(rerr).Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return count, nil
}
