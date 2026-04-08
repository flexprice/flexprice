package ent

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/domain/addonpricesync"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type addonPriceSyncRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

func NewAddonPriceSyncRepository(client postgres.IClient, log *logger.Logger) addonpricesync.Repository {
	return &addonPriceSyncRepository{
		client: client,
		log:    log,
	}
}

// TerminateExpiredAddonPricesLineItems terminates addon-derived line items whose price has ended.
func (r *addonPriceSyncRepository) TerminateExpiredAddonPricesLineItems(
	ctx context.Context,
	p addonpricesync.TerminateExpiredAddonPricesLineItemsParams,
) (numTerminated int, err error) {
	addonID := p.AddonID
	limit := p.Limit

	if addonID == "" {
		return 0, ierr.NewError("addon_id is required").
			WithReportableDetails(map[string]any{"addon_id": addonID}).
			Mark(ierr.ErrValidation)
	}
	if limit <= 0 {
		limit = DEFAULT_LIMIT
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	userID := types.GetUserID(ctx)

	span := StartRepositorySpan(ctx, "addon_price_sync", "terminate_expired_addon_prices_line_items", map[string]interface{}{
		"addon_id": addonID,
		"limit":    limit,
	})
	defer FinishSpan(span)

	query := fmt.Sprintf(`
		WITH
			subs AS (
				SELECT
					aa.entity_id AS id
				FROM
					addon_associations aa
					JOIN subscriptions sub ON sub.id = aa.entity_id
				WHERE
					aa.tenant_id = $1
					AND aa.environment_id = $2
					AND aa.status = '%s'
					AND aa.addon_id = $3
					AND aa.entity_type = '%s'
					AND aa.addon_status = '%s'
					AND sub.status = '%s'
					AND sub.subscription_status IN ('%s', '%s')
			),
			ended_addon_prices AS (
				SELECT
					id,
					end_date
				FROM
					prices
				WHERE
					tenant_id = $1
					AND environment_id = $2
					AND status = '%s'
					AND entity_type = '%s'
					AND entity_id = $3
					AND end_date IS NOT NULL
					AND type <> '%s'
			),
			targets AS (
				SELECT
					li.id AS line_item_id,
					GREATEST(COALESCE(li.start_date, p.end_date), p.end_date) AS target_end_date
				FROM
					subscription_line_items li
					JOIN subs s ON s.id = li.subscription_id
					JOIN ended_addon_prices p ON p.id = li.price_id
				WHERE
					li.tenant_id = $1
					AND li.environment_id = $2
					AND li.status = '%s'
					AND li.entity_type = '%s'
					AND li.end_date IS NULL
				ORDER BY li.id
				LIMIT $4
			)
		UPDATE
			subscription_line_items li
		SET
			end_date = t.target_end_date,
			updated_at = NOW(),
			updated_by = $5
		FROM
			targets t
		WHERE
			li.id = t.line_item_id
	`,
		string(types.StatusPublished),
		string(types.AddonAssociationEntityTypeSubscription),
		string(types.AddonStatusActive),
		string(types.StatusPublished),
		string(types.SubscriptionStatusActive),
		string(types.SubscriptionStatusTrialing),
		string(types.StatusPublished),
		string(types.PRICE_ENTITY_TYPE_ADDON),
		string(types.PRICE_TYPE_FIXED),
		string(types.StatusPublished),
		string(types.SubscriptionLineItemEntityTypeAddon),
	)

	result, qerr := r.client.Writer(ctx).ExecContext(
		ctx,
		query,
		tenantID,
		environmentID,
		addonID,
		limit,
		userID,
	)
	if qerr != nil {
		r.log.Errorw("failed to execute termination query for addon line items",
			"addon_id", addonID, "limit", limit, "error", qerr)
		SetSpanError(span, qerr)
		return 0, ierr.WithError(qerr).
			WithHint("Failed to terminate addon line items").
			WithReportableDetails(map[string]any{"addon_id": addonID, "limit": limit}).
			Mark(ierr.ErrDatabase)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		r.log.Errorw("failed to get rows affected for terminated addon line items",
			"addon_id", addonID, "limit", limit, "error", err)
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithReportableDetails(map[string]any{"addon_id": addonID, "limit": limit}).
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return int(rowsAffected), nil
}

// ListAddonLineItemsToTerminate returns addon-derived line items whose end_date must be set.
func (r *addonPriceSyncRepository) ListAddonLineItemsToTerminate(
	ctx context.Context,
	p addonpricesync.ListAddonLineItemsToTerminateParams,
) (items []addonpricesync.AddonLineItemTerminationDelta, err error) {
	addonID := p.AddonID
	limit := p.Limit

	if addonID == "" {
		return nil, ierr.NewError("addon_id is required").
			WithReportableDetails(map[string]any{"addon_id": addonID}).
			Mark(ierr.ErrValidation)
	}
	if limit <= 0 {
		limit = DEFAULT_LIMIT
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	span := StartRepositorySpan(ctx, "addon_price_sync", "list_addon_line_items_to_terminate", map[string]interface{}{
		"addon_id": addonID,
		"limit":    limit,
	})
	defer FinishSpan(span)

	query := fmt.Sprintf(`
		WITH
			subs AS (
				SELECT
					aa.entity_id AS id
				FROM
					addon_associations aa
					JOIN subscriptions sub ON sub.id = aa.entity_id
				WHERE
					aa.tenant_id = $1
					AND aa.environment_id = $2
					AND aa.status = '%s'
					AND aa.addon_id = $3
					AND aa.entity_type = '%s'
					AND aa.addon_status = '%s'
					AND sub.status = '%s'
					AND sub.subscription_status IN ('%s', '%s')
			),
			ended_addon_prices AS (
				SELECT
					id,
					end_date
				FROM
					prices
				WHERE
					tenant_id = $1
					AND environment_id = $2
					AND status = '%s'
					AND entity_type = '%s'
					AND entity_id = $3
					AND end_date IS NOT NULL
					AND type <> '%s'
			)
		SELECT
			li.id AS line_item_id,
			li.subscription_id AS subscription_id,
			li.price_id AS price_id,
			GREATEST(COALESCE(li.start_date, p.end_date), p.end_date) AS target_end_date
		FROM
			subscription_line_items li
			JOIN subs s ON s.id = li.subscription_id
			JOIN ended_addon_prices p ON p.id = li.price_id
		WHERE
			li.tenant_id = $1
			AND li.environment_id = $2
			AND li.status = '%s'
			AND li.entity_type = '%s'
			AND li.end_date IS NULL
		ORDER BY li.start_date, li.id
		LIMIT $4
	`,
		string(types.StatusPublished),
		string(types.AddonAssociationEntityTypeSubscription),
		string(types.AddonStatusActive),
		string(types.StatusPublished),
		string(types.SubscriptionStatusActive),
		string(types.SubscriptionStatusTrialing),
		string(types.StatusPublished),
		string(types.PRICE_ENTITY_TYPE_ADDON),
		string(types.PRICE_TYPE_FIXED),
		string(types.StatusPublished),
		string(types.SubscriptionLineItemEntityTypeAddon),
	)

	rows, qerr := r.client.Reader(ctx).QueryContext(ctx, query, tenantID, environmentID, addonID, limit)
	if qerr != nil {
		r.log.Errorw("failed to query addon line items to terminate",
			"addon_id", addonID, "limit", limit, "error", qerr)
		SetSpanError(span, qerr)
		return nil, ierr.WithError(qerr).
			WithHint("Failed to list addon line items to terminate").
			WithReportableDetails(map[string]any{"addon_id": addonID, "limit": limit}).
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	for rows.Next() {
		var delta addonpricesync.AddonLineItemTerminationDelta
		if scanErr := rows.Scan(&delta.LineItemID, &delta.SubscriptionID, &delta.PriceID, &delta.TargetEndDate); scanErr != nil {
			r.log.Errorw("failed to scan addon termination delta row",
				"addon_id", addonID, "error", scanErr)
			SetSpanError(span, scanErr)
			return nil, ierr.WithError(scanErr).
				WithHint("Failed to scan addon termination delta row").
				Mark(ierr.ErrDatabase)
		}
		items = append(items, delta)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		SetSpanError(span, rowsErr)
		return nil, ierr.WithError(rowsErr).Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return items, nil
}

// ListAddonLineItemsToCreate returns missing (subscription_id, price_id) pairs for an addon.
func (r *addonPriceSyncRepository) ListAddonLineItemsToCreate(
	ctx context.Context,
	p addonpricesync.ListAddonLineItemsToCreateParams,
) (items []addonpricesync.AddonLineItemCreationDelta, err error) {
	addonID := p.AddonID
	limit := p.Limit

	if addonID == "" {
		return nil, ierr.NewError("addon_id is required").
			WithReportableDetails(map[string]any{"addon_id": addonID}).
			Mark(ierr.ErrValidation)
	}
	if limit <= 0 {
		limit = DEFAULT_LIMIT
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cursorSubID := p.AfterSubID
	hasCursor := cursorSubID != ""

	span := StartRepositorySpan(ctx, "addon_price_sync", "list_addon_line_items_to_create", map[string]interface{}{
		"addon_id":      addonID,
		"limit":         limit,
		"has_cursor":    hasCursor,
		"cursor_sub_id": cursorSubID,
	})
	defer FinishSpan(span)

	cursorCondition := "AND (p.last_sub_id = '' OR aa.entity_id >= p.last_sub_id) "

	query := fmt.Sprintf(`
		WITH
			params AS (
				SELECT $5::text AS last_sub_id
			),
			subs_batch AS (
				SELECT
					aa.entity_id AS id,
					sub.customer_id,
					sub.tenant_id,
					sub.environment_id,
					sub.currency,
					sub.billing_period,
					sub.billing_period_count,
					sub.start_date
				FROM
					addon_associations aa
					JOIN subscriptions sub ON sub.id = aa.entity_id,
					params p
				WHERE
					aa.tenant_id = $1
					AND aa.environment_id = $2
					AND aa.status = '%s'
					AND aa.addon_id = $3
					AND aa.entity_type = '%s'
					AND aa.addon_status = '%s'
					AND sub.status = '%s'
					AND sub.subscription_status IN ('%s', '%s')
					%s
				ORDER BY aa.entity_id
				LIMIT $4
			),
			addon_prices AS (
				SELECT
					p.id,
					p.tenant_id,
					p.environment_id,
					p.currency,
					p.billing_period,
					p.billing_period_count,
					p.parent_price_id,
					p.end_date
				FROM
					prices p
				WHERE
					p.tenant_id = $1
					AND p.environment_id = $2
					AND p.status = '%s'
					AND p.entity_type = '%s'
					AND p.entity_id = $3
					AND p.type <> '%s'
			)
		SELECT
			s.id AS subscription_id,
			p.id AS missing_price_id,
			s.customer_id AS customer_id
		FROM
			subs_batch s
			JOIN addon_prices p ON lower(p.currency) = lower(s.currency)
				AND p.billing_period = s.billing_period
				AND p.billing_period_count = s.billing_period_count
		WHERE
			(p.end_date IS NULL OR s.start_date <= p.end_date)
			AND NOT EXISTS (
				SELECT 1
				FROM prices sp
				WHERE
					sp.tenant_id = s.tenant_id
					AND sp.environment_id = s.environment_id
					AND sp.status = '%s'
					AND sp.entity_type = '%s'
					AND sp.entity_id = s.id
					AND (
						sp.parent_price_id = p.id
						OR (p.parent_price_id IS NOT NULL AND sp.parent_price_id = p.parent_price_id)
					)
			)
			AND NOT EXISTS (
				SELECT 1
				FROM subscription_line_items li
				WHERE
					li.tenant_id = s.tenant_id
					AND li.environment_id = s.environment_id
					AND li.status = '%s'
					AND li.subscription_id = s.id
					AND li.price_id = p.id
					AND li.entity_type = '%s'
			)
		`,
		string(types.StatusPublished),
		string(types.AddonAssociationEntityTypeSubscription),
		string(types.AddonStatusActive),
		string(types.StatusPublished),
		string(types.SubscriptionStatusActive),
		string(types.SubscriptionStatusTrialing),
		cursorCondition,
		string(types.StatusPublished),
		string(types.PRICE_ENTITY_TYPE_ADDON),
		string(types.PRICE_TYPE_FIXED),
		string(types.StatusPublished),
		string(types.PRICE_ENTITY_TYPE_SUBSCRIPTION),
		string(types.StatusPublished),
		string(types.SubscriptionLineItemEntityTypeAddon),
	)

	cursorParam := ""
	if hasCursor {
		cursorParam = cursorSubID
	}
	args := []interface{}{tenantID, environmentID, addonID, limit, cursorParam}

	rows, qerr := r.client.Reader(ctx).QueryContext(ctx, query, args...)
	if qerr != nil {
		r.log.Errorw("failed to query addon line items to create",
			"addon_id", addonID, "limit", limit, "error", qerr)
		SetSpanError(span, qerr)
		return nil, ierr.WithError(qerr).
			WithHint("Failed to list addon line items to create").
			WithReportableDetails(map[string]any{"addon_id": addonID, "limit": limit}).
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	for rows.Next() {
		var subID, priceID, customerID string
		if scanErr := rows.Scan(&subID, &priceID, &customerID); scanErr != nil {
			r.log.Errorw("failed to scan addon creation delta row",
				"addon_id", addonID, "error", scanErr)
			SetSpanError(span, scanErr)
			return nil, ierr.WithError(scanErr).
				WithHint("Failed to scan addon creation delta row").
				Mark(ierr.ErrDatabase)
		}
		items = append(items, addonpricesync.AddonLineItemCreationDelta{
			SubscriptionID: subID,
			PriceID:        priceID,
			CustomerID:     customerID,
		})
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		SetSpanError(span, rowsErr)
		return nil, ierr.WithError(rowsErr).Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return items, nil
}

// GetLastSubscriptionIDInBatch returns the last subscription ID from the batch for cursor advancement.
// Note: this query intentionally omits the subscription_status filter that ListAddonLineItemsToCreate uses.
// The cursor is used only to advance the page boundary; ListAddonLineItemsToCreate will apply the full
// filter on the next page, so at worst this produces an extra empty-batch iteration (no correctness impact).
func (r *addonPriceSyncRepository) GetLastSubscriptionIDInBatch(
	ctx context.Context,
	p addonpricesync.ListAddonLineItemsToCreateParams,
) (lastSubID *string, err error) {
	addonID := p.AddonID
	limit := p.Limit

	if addonID == "" {
		return nil, ierr.NewError("addon_id is required").
			WithReportableDetails(map[string]any{"addon_id": addonID}).
			Mark(ierr.ErrValidation)
	}
	if limit <= 0 {
		limit = DEFAULT_LIMIT
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cursorSubID := p.AfterSubID
	hasCursor := cursorSubID != ""

	cursorCondition := "AND (p.last_sub_id = '' OR aa.entity_id >= p.last_sub_id) "

	query := fmt.Sprintf(`
		WITH
			params AS (
				SELECT $5::text AS last_sub_id
			),
			subs_batch AS (
				SELECT
					aa.entity_id AS id
				FROM
					addon_associations aa,
					params p
				WHERE
					aa.tenant_id = $1
					AND aa.environment_id = $2
					AND aa.status = '%s'
					AND aa.addon_id = $3
					AND aa.entity_type = '%s'
					AND aa.addon_status = '%s'
					%s
				ORDER BY aa.entity_id
				LIMIT $4
			)
		SELECT
			COALESCE(MAX(s.id), '') AS last_sub_id
		FROM
			subs_batch s
		`,
		string(types.StatusPublished),
		string(types.AddonAssociationEntityTypeSubscription),
		string(types.AddonStatusActive),
		cursorCondition,
	)

	cursorParam := ""
	if hasCursor {
		cursorParam = cursorSubID
	}
	args := []interface{}{tenantID, environmentID, addonID, limit, cursorParam}

	rows, qerr := r.client.Reader(ctx).QueryContext(ctx, query, args...)
	if qerr != nil {
		r.log.Errorw("failed to query last subscription ID in addon batch",
			"addon_id", addonID, "limit", limit, "error", qerr)
		return nil, ierr.WithError(qerr).
			WithHint("Failed to get last subscription ID in addon batch").
			WithReportableDetails(map[string]any{"addon_id": addonID, "limit": limit}).
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var batchLastSubID string
	if rows.Next() {
		if scanErr := rows.Scan(&batchLastSubID); scanErr != nil {
			r.log.Errorw("failed to scan last addon subscription ID",
				"addon_id", addonID, "error", scanErr)
			return nil, ierr.WithError(scanErr).
				WithHint("Failed to scan last addon subscription ID").
				Mark(ierr.ErrDatabase)
		}
	} else {
		batchLastSubID = ""
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		r.log.Errorw("failed to iterate rows for last addon subscription ID",
			"addon_id", addonID, "limit", limit, "error", rowsErr)
		return nil, ierr.WithError(rowsErr).Mark(ierr.ErrDatabase)
	}

	if batchLastSubID == "" || batchLastSubID == cursorSubID {
		return nil, nil
	}
	return &batchLastSubID, nil
}
