package ent

import (
	"context"
	"fmt"
	"strings"

	"github.com/flexprice/flexprice/internal/domain/planpricesync"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/lib/pq"
	"github.com/samber/lo"
)

// CurrentPlanSequence returns max(prices.sequence) for the plan's published,
// non-fixed prices. 0 means no qualifying prices.
func (r *planPriceSyncRepository) CurrentPlanSequence(
	ctx context.Context,
	planID string,
) (int64, error) {
	if planID == "" {
		return 0, ierr.NewError("plan_id is required").Mark(ierr.ErrValidation)
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
		string(types.PRICE_ENTITY_TYPE_PLAN),
		string(types.PRICE_TYPE_USAGE),
	)

	rows, err := r.client.Reader(ctx).QueryContext(ctx, query, tenantID, environmentID, planID)
	if err != nil {
		return 0, ierr.WithError(err).
			WithHint("Failed to read current plan sequence").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var seq int64
	if rows.Next() {
		if scanErr := rows.Scan(&seq); scanErr != nil {
			return 0, ierr.WithError(scanErr).
				WithHint("Failed to scan plan sequence").
				Mark(ierr.ErrDatabase)
		}
	}
	return seq, rows.Err()
}

// ListPlanLineItemsToCreateV2 returns missing (sub, price) pairs narrowed by
// sequence: only prices whose sequence is greater than each candidate sub's
// synced_price_sequence are considered. Pagination is driven by stamping —
// once subs are stamped to TargetSeq they fall out of the
// `synced_price_sequence < TargetSeq` filter on the next call. No cursor.
func (r *planPriceSyncRepository) ListPlanLineItemsToCreateV2(
	ctx context.Context,
	p planpricesync.ListPlanLineItemsToCreateV2Params,
) (items []planpricesync.PlanLineItemCreationDelta, staleSubIDs []string, err error) {
	if p.PlanID == "" {
		return nil, nil, ierr.NewError("plan_id is required").Mark(ierr.ErrValidation)
	}
	limit := p.Limit
	if limit <= 0 {
		limit = DEFAULT_LIMIT
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	span := StartRepositorySpan(ctx, "plan_price_sync_v2", "list_plan_line_items_to_create", map[string]interface{}{
		"plan_id":    p.PlanID,
		"target_seq": p.TargetSeq,
		"limit":      limit,
	})
	defer FinishSpan(span)

	// Two result sets in one round trip:
	//   kind='pair' — actual missing pair
	//   kind='sub'  — one row per stale sub in the page (for stamping;
	//                 includes subs that produced no pairs)
	query := fmt.Sprintf(`
		WITH stale_subs AS (
			SELECT id, customer_id, currency, billing_period, billing_period_count,
			       start_date, synced_price_sequence
			FROM subscriptions
			WHERE tenant_id = $1
			  AND environment_id = $2
			  AND status = '%s'
			  AND plan_id = $3
			  AND subscription_status IN ('%s','%s', '%s', '%s')
			  AND subscription_type IN (%s)
			  AND synced_price_sequence < $4
			ORDER BY id
			LIMIT $5
		),
		plan_prices AS (
			SELECT id, currency, billing_period, billing_period_count,
			       parent_price_id, end_date, sequence
			FROM prices
			WHERE tenant_id = $1
			  AND environment_id = $2
			  AND status = '%s'
			  AND entity_type = '%s'
			  AND entity_id = $3
			  AND type = '%s'
		),
		pairs AS (
			SELECT s.id AS subscription_id, p.id AS price_id, s.customer_id
			FROM stale_subs s
			JOIN plan_prices p
			  ON  p.currency             = s.currency
			  AND p.billing_period       = s.billing_period
			  AND p.billing_period_count = s.billing_period_count
			  AND p.sequence             > s.synced_price_sequence
			WHERE (p.end_date IS NULL OR s.start_date <= p.end_date)
			  AND NOT EXISTS (
			      SELECT 1 FROM prices sp
			      WHERE sp.tenant_id = $1 AND sp.environment_id = $2
			        AND sp.status = '%s'
			        AND sp.entity_type = '%s' AND sp.entity_id = s.id
			        AND ( sp.parent_price_id = p.id
			           OR (NULLIF(p.parent_price_id, '') IS NOT NULL AND sp.parent_price_id = p.parent_price_id) )
			  )
			  AND NOT EXISTS (
			      SELECT 1 FROM subscription_line_items li
			      WHERE li.tenant_id = $1 AND li.environment_id = $2
			        AND li.status = '%s'
			        AND li.subscription_id = s.id
			        AND li.price_id = p.id
			        AND li.entity_type = '%s'
			  )
		)
		SELECT 'pair'::text AS kind, subscription_id, price_id, customer_id FROM pairs
		UNION ALL
		SELECT 'sub'::text  AS kind, id, '', '' FROM stale_subs
	`,
		string(types.StatusPublished),
		string(types.SubscriptionStatusActive),
		string(types.SubscriptionStatusTrialing),
		string(types.SubscriptionStatusDraft),
		string(types.SubscriptionStatusIncomplete),
		// Sub types that own plan line items. Inherited is excluded by design
		strings.Join(lo.Map(types.SubscriptionTypesWithLineItems, func(t types.SubscriptionType, _ int) string { return fmt.Sprintf("'%s'", string(t)) }), ","),
		string(types.StatusPublished),
		string(types.PRICE_ENTITY_TYPE_PLAN),
		string(types.PRICE_TYPE_USAGE),
		string(types.StatusPublished),
		string(types.PRICE_ENTITY_TYPE_SUBSCRIPTION),
		string(types.StatusPublished),
		string(types.SubscriptionLineItemEntityTypePlan),
	)

	rows, qerr := r.client.Reader(ctx).QueryContext(
		ctx, query,
		tenantID, environmentID, p.PlanID, p.TargetSeq, limit,
	)
	if qerr != nil {
		SetSpanError(span, qerr)
		return nil, nil, ierr.WithError(qerr).
			WithHint("Failed to list V2 plan line items to create").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	for rows.Next() {
		var kind, sub, price, customer string
		if scanErr := rows.Scan(&kind, &sub, &price, &customer); scanErr != nil {
			SetSpanError(span, scanErr)
			return nil, nil, ierr.WithError(scanErr).
				WithHint("Failed to scan V2 sync row").
				Mark(ierr.ErrDatabase)
		}
		switch kind {
		case "pair":
			items = append(items, planpricesync.PlanLineItemCreationDelta{
				SubscriptionID: sub,
				PriceID:        price,
				CustomerID:     customer,
			})
		case "sub":
			staleSubIDs = append(staleSubIDs, sub)
		}
	}
	if rerr := rows.Err(); rerr != nil {
		SetSpanError(span, rerr)
		return nil, nil, ierr.WithError(rerr).
			WithHint("Failed to iterate V2 sync rows").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return items, staleSubIDs, nil
}

// TerminatePlanPricesLineItemsV2 sets end_date on live plan-derived line
// items belonging to the given subs whose price has been ended. Scoping to
// the page's sub set bounds the UPDATE so we don't lock thousands of rows
// in a single statement. Idempotent via `li.end_date IS NULL`.
func (r *planPriceSyncRepository) TerminatePlanPricesLineItemsV2(
	ctx context.Context,
	p planpricesync.TerminatePlanPricesLineItemsV2Params,
) (int, error) {
	if p.PlanID == "" {
		return 0, ierr.NewError("plan_id is required").Mark(ierr.ErrValidation)
	}
	if len(p.SubIDs) == 0 {
		return 0, nil
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	userID := types.GetUserID(ctx)

	span := StartRepositorySpan(ctx, "plan_price_sync_v2", "terminate_line_items", map[string]interface{}{
		"plan_id":   p.PlanID,
		"sub_count": len(p.SubIDs),
	})
	defer FinishSpan(span)

	query := fmt.Sprintf(`
		UPDATE subscription_line_items li
		SET end_date   = GREATEST(COALESCE(li.start_date, p.end_date), p.end_date),
		    updated_at = NOW(),
		    updated_by = $4
		FROM prices p
		WHERE li.tenant_id       = $1
		  AND li.environment_id  = $2
		  AND li.status          = '%s'
		  AND li.entity_type     = '%s'
		  AND li.end_date IS NULL
		  AND li.subscription_id = ANY($5)
		  AND li.price_id        = p.id
		  AND p.tenant_id        = $1
		  AND p.environment_id   = $2
		  AND p.entity_id        = $3
		  AND p.entity_type      = '%s'
		  AND p.end_date IS NOT NULL
	`,
		string(types.StatusPublished),
		string(types.SubscriptionLineItemEntityTypePlan),
		string(types.PRICE_ENTITY_TYPE_PLAN),
	)

	result, qerr := r.client.Writer(ctx).ExecContext(
		ctx, query,
		tenantID, environmentID, p.PlanID, userID, pq.Array(p.SubIDs),
	)
	if qerr != nil {
		SetSpanError(span, qerr)
		return 0, ierr.WithError(qerr).
			WithHint("Failed to terminate V2 plan line items").
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

// StampSubsAsSynced sets synced_price_sequence on the given subs. Forward-only
// (uses GREATEST so a concurrent newer stamp from a different worker isn't
// overwritten).
func (r *planPriceSyncRepository) StampSubsAsSynced(
	ctx context.Context,
	p planpricesync.StampSubsAsSyncedParams,
) (int, error) {
	if len(p.SubIDs) == 0 {
		return 0, nil
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	userID := types.GetUserID(ctx)

	span := StartRepositorySpan(ctx, "plan_price_sync_v2", "stamp_subs", map[string]interface{}{
		"target_seq": p.TargetSeq,
		"sub_count":  len(p.SubIDs),
	})
	defer FinishSpan(span)

	query := `
		UPDATE subscriptions
		SET synced_price_sequence = GREATEST(synced_price_sequence, $3),
		    updated_at = NOW(),
		    updated_by = $4
		WHERE tenant_id      = $1
		  AND environment_id = $2
		  AND id = ANY($5)
		  AND synced_price_sequence < $3
	`

	result, qerr := r.client.Writer(ctx).ExecContext(
		ctx, query,
		tenantID, environmentID, p.TargetSeq, userID, pq.Array(p.SubIDs),
	)
	if qerr != nil {
		SetSpanError(span, qerr)
		return 0, ierr.WithError(qerr).
			WithHint("Failed to stamp subs as synced").
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
