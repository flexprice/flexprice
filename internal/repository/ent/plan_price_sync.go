package ent

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/domain/planpricesync"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type planPriceSyncRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

func NewPlanPriceSyncRepository(client postgres.IClient, log *logger.Logger) planpricesync.Repository {
	return &planPriceSyncRepository{
		client: client,
		log:    log,
	}
}

const (
	DEFAULT_LIMIT = 1000
)

// TerminateExpiredPlanPricesLineItems terminates plan-derived line items whose end_date must be set to price.end_date.
//
// Batch:
// - If limit <= 0, a default limit is used.
func (r *planPriceSyncRepository) TerminateExpiredPlanPricesLineItems(
	ctx context.Context,
	p planpricesync.TerminateExpiredPlanPricesLineItemsParams,
) (numTerminated int, err error) {
	planID := p.PlanID
	limit := p.Limit

	if planID == "" {
		return 0, ierr.NewError("plan_id is required").
			WithReportableDetails(map[string]any{
				"plan_id": planID,
			}).
			Mark(ierr.ErrValidation)
	}

	if limit <= 0 {
		limit = DEFAULT_LIMIT
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	userID := types.GetUserID(ctx)

	span := StartRepositorySpan(ctx, "plan_price_sync", "terminate_expired_plan_prices_line_items", map[string]interface{}{
		"plan_id": planID,
		"limit":   limit,
	})
	defer FinishSpan(span)

	query := fmt.Sprintf(`
		WITH
			subs AS (
				SELECT
					id
				FROM
					subscriptions
				WHERE
					tenant_id = $1
					AND environment_id = $2
					AND status = '%s'
					AND plan_id = $3
					AND subscription_status IN ('%s', '%s')
			),
			ended_plan_prices AS (
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
					p.end_date AS target_end_date
				FROM
					subscription_line_items li
					JOIN subs s ON s.id = li.subscription_id
					JOIN ended_plan_prices p ON p.id = li.price_id
				WHERE
					li.tenant_id = $1
					AND li.environment_id = $2
					AND li.status = '%s'
					AND li.entity_type = '%s'
					AND li.end_date IS NULL
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
		string(types.SubscriptionStatusActive),
		string(types.SubscriptionStatusTrialing),
		string(types.StatusPublished),
		string(types.PRICE_ENTITY_TYPE_PLAN),
		string(types.PRICE_TYPE_FIXED),
		string(types.StatusPublished),
		string(types.SubscriptionLineItemEntityTypePlan),
	)

	result, qerr := r.client.Writer(ctx).ExecContext(
		ctx,
		query,
		tenantID,
		environmentID,
		planID,
		limit,
		userID,
	)
	if qerr != nil {
		r.log.Errorw("failed to execute termination query for plan line items",
			"plan_id", planID,
			"limit", limit,
			"error", qerr)
		SetSpanError(span, qerr)
		return 0, ierr.WithError(qerr).
			WithHint("Failed to terminate plan line items").
			WithReportableDetails(map[string]any{
				"plan_id": planID,
				"limit":   limit,
			}).
			Mark(ierr.ErrDatabase)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		r.log.Errorw("failed to get rows affected for terminated line items",
			"plan_id", planID,
			"limit", limit,
			"error", err)
		SetSpanError(span, err)
		return 0, ierr.
			WithError(err).
			WithReportableDetails(map[string]any{
				"plan_id": planID,
				"limit":   limit,
			}).
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return int(rowsAffected), nil
}

// ListPlanLineItemsToTerminate returns plan-derived line items whose end_date must be set to price.end_date.
//
// Batch:
// - If limit <= 0, a default limit is used.
func (r *planPriceSyncRepository) ListPlanLineItemsToTerminate(
	ctx context.Context,
	p planpricesync.ListPlanLineItemsToTerminateParams,
) (items []planpricesync.PlanLineItemTerminationDelta, err error) {
	planID := p.PlanID
	limit := p.Limit

	if planID == "" {
		return nil, ierr.NewError("plan_id is required").
			WithReportableDetails(map[string]any{
				"plan_id": planID,
			}).
			Mark(ierr.ErrValidation)
	}

	if limit <= 0 {
		limit = DEFAULT_LIMIT
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	span := StartRepositorySpan(ctx, "plan_price_sync", "list_line_items_to_terminate", map[string]interface{}{
		"plan_id": planID,
		"limit":   limit,
	})
	defer FinishSpan(span)

	query := fmt.Sprintf(`
		WITH
			subs AS (
				SELECT
					id
				FROM
					subscriptions
				WHERE
					tenant_id = $1
					AND environment_id = $2
					AND status = '%s'
					AND plan_id = $3
					AND subscription_status IN ('%s', '%s')
			),
			ended_plan_prices AS (
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
			p.end_date AS target_end_date
		FROM
			subscription_line_items li
			JOIN subs s ON s.id = li.subscription_id
			JOIN ended_plan_prices p ON p.id = li.price_id
		WHERE
			li.tenant_id = $1
			AND li.environment_id = $2
			AND li.status = '%s'
			AND li.entity_type = '%s'
			AND li.end_date IS NULL
		LIMIT
			$4
	`,
		string(types.StatusPublished),
		string(types.SubscriptionStatusActive),
		string(types.SubscriptionStatusTrialing),
		string(types.StatusPublished),
		string(types.PRICE_ENTITY_TYPE_PLAN),
		string(types.PRICE_TYPE_FIXED),
		string(types.StatusPublished),
		string(types.SubscriptionLineItemEntityTypePlan),
	)

	rows, qerr := r.client.Reader(ctx).
		QueryContext(
			ctx,
			query,
			tenantID,
			environmentID,
			planID,
			limit,
		)
	if qerr != nil {
		r.log.Errorw("failed to query plan line items to terminate",
			"plan_id", planID,
			"limit", limit,
			"error", qerr)
		SetSpanError(span, qerr)
		return nil, ierr.WithError(qerr).
			WithHint("Failed to list plan line items to terminate").
			WithReportableDetails(map[string]any{
				"plan_id": planID,
				"limit":   limit,
			}).
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	for rows.Next() {
		var d planpricesync.PlanLineItemTerminationDelta
		if scanErr := rows.Scan(&d.LineItemID, &d.SubscriptionID, &d.PriceID, &d.TargetEndDate); scanErr != nil {
			r.log.Errorw("failed to scan termination delta row",
				"plan_id", planID,
				"limit", limit,
				"error", scanErr)
			SetSpanError(span, scanErr)
			return nil, ierr.WithError(scanErr).
				WithHint("Failed to scan termination delta row").
				Mark(ierr.ErrDatabase)
		}
		items = append(items, d)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		r.log.Errorw("failed to iterate termination delta rows",
			"plan_id", planID,
			"limit", limit,
			"error", rowsErr)
		SetSpanError(span, rowsErr)
		return nil, ierr.WithError(rowsErr).
			WithHint("Failed to iterate termination delta rows").
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return items, nil
}

// ListPlanLineItemsToCreate returns missing (subscription_id, price_id) pairs for a plan.
//
// Batch:
// - If limit <= 0, a default limit is used.
func (r *planPriceSyncRepository) ListPlanLineItemsToCreate(
	ctx context.Context,
	p planpricesync.ListPlanLineItemsToCreateParams,
) (items []planpricesync.PlanLineItemCreationDelta, err error) {
	planID := p.PlanID
	limit := p.Limit

	if planID == "" {
		return nil, ierr.NewError("plan_id is required").
			WithReportableDetails(map[string]any{
				"plan_id": planID,
			}).
			Mark(ierr.ErrValidation)
	}
	if limit <= 0 {
		limit = DEFAULT_LIMIT
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cursorSubID := p.AfterSubID

	hasCursor := cursorSubID != ""

	span := StartRepositorySpan(ctx, "plan_price_sync", "list_plan_line_items_to_create", map[string]interface{}{
		"plan_id":       planID,
		"limit":         limit,
		"has_cursor":    hasCursor,
		"cursor_sub_id": cursorSubID,
	})
	defer FinishSpan(span)

	cursorCondition := "AND (p.last_sub_id = '' OR s.id >= p.last_sub_id) "

	query := fmt.Sprintf(`
		WITH
			params AS (
				SELECT $5::text AS last_sub_id
			),
			subs_batch AS (
				SELECT
					s.id,
					s.tenant_id,
					s.environment_id,
					s.currency,
					s.billing_period,
					s.billing_period_count
				FROM
					subscriptions s, params p
				WHERE
					s.tenant_id = $1
					AND s.environment_id = $2
					AND s.status = '%s'
					AND s.plan_id = $3
					AND s.subscription_status IN ('%s', '%s')
					%s
				ORDER BY s.id
				LIMIT $4
			),
			plan_prices AS (
				SELECT
					p.id,
					p.tenant_id,
					p.environment_id,
					p.currency,
					p.billing_period,
					p.billing_period_count
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
			p.id AS missing_price_id
		FROM
			subs_batch s
			JOIN plan_prices p ON lower(p.currency) = lower(s.currency)
				AND p.billing_period = s.billing_period
				AND p.billing_period_count = s.billing_period_count
		WHERE
			NOT EXISTS (
				SELECT
					1
				FROM
					prices sp
				WHERE
					sp.tenant_id = s.tenant_id
					AND sp.environment_id = s.environment_id
					AND sp.status = '%s'
					AND sp.entity_type = '%s'
					AND sp.entity_id = s.id
					AND sp.parent_price_id = p.id
			)
			AND NOT EXISTS (
				SELECT
					1
				FROM
					subscription_line_items li
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
		string(types.SubscriptionStatusActive),
		string(types.SubscriptionStatusTrialing),
		cursorCondition,
		string(types.StatusPublished),
		string(types.PRICE_ENTITY_TYPE_PLAN),
		string(types.PRICE_TYPE_FIXED),
		string(types.StatusPublished),
		string(types.PRICE_ENTITY_TYPE_SUBSCRIPTION),
		string(types.StatusPublished),
		string(types.SubscriptionLineItemEntityTypePlan),
	)

	cursorParam := ""
	if hasCursor {
		cursorParam = cursorSubID
	}
	args := []interface{}{
		tenantID,
		environmentID,
		planID,
		limit,
		cursorParam,
	}

	rows, qerr := r.client.Reader(ctx).QueryContext(
		ctx,
		query,
		args...,
	)
	if qerr != nil {
		r.log.Errorw("failed to query plan line items to create",
			"plan_id", planID,
			"limit", limit,
			"error", qerr)
		SetSpanError(span, qerr)
		return nil, ierr.WithError(qerr).
			WithHint("Failed to list plan line items to create").
			WithReportableDetails(map[string]any{
				"plan_id": planID,
				"limit":   limit,
			}).
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	for rows.Next() {
		var subID, priceID string
		if scanErr := rows.Scan(&subID, &priceID); scanErr != nil {
			r.log.Errorw("failed to scan creation delta row",
				"plan_id", planID,
				"limit", limit,
				"error", scanErr)
			SetSpanError(span, scanErr)
			return nil, ierr.WithError(scanErr).
				WithHint("Failed to scan creation delta row").
				Mark(ierr.ErrDatabase)
		}
		items = append(items, planpricesync.PlanLineItemCreationDelta{
			SubscriptionID: subID,
			PriceID:        priceID,
		})
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		r.log.Errorw("failed to iterate creation delta rows",
			"plan_id", planID,
			"limit", limit,
			"error", rowsErr)
		SetSpanError(span, rowsErr)
		return nil, ierr.WithError(rowsErr).
			WithHint("Failed to iterate creation delta rows").
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return items, nil
}

// GetLastSubscriptionIDInBatch returns the last subscription ID from the batch.
// Returns nil when cursor can't advance: batchLastSubID == "" (no more subscriptions) OR batchLastSubID == cursorSubID (cursor didn't advance).
// Returns pointer to subscription ID when can advance: batchLastSubID != "" && batchLastSubID != cursorSubID.
//
// Why cursorSubID == batchLastSubID means cursor didn't advance:
// With limit 1000 and max 1000 active prices per plan, a single subscription cannot have more than 1000 missing pairs.
// If batchLastSubID == cursorSubID, it means we've processed all subscriptions in the batch (up to limit 1000) and the last one
// matches our cursor, indicating no progress was made and we should stop.
func (r *planPriceSyncRepository) GetLastSubscriptionIDInBatch(
	ctx context.Context,
	p planpricesync.ListPlanLineItemsToCreateParams,
) (lastSubID *string, err error) {
	planID := p.PlanID
	limit := p.Limit

	if planID == "" {
		return nil, ierr.NewError("plan_id is required").
			WithReportableDetails(map[string]any{
				"plan_id": planID,
			}).
			Mark(ierr.ErrValidation)
	}
	if limit <= 0 {
		limit = DEFAULT_LIMIT
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cursorSubID := p.AfterSubID

	hasCursor := cursorSubID != ""

	cursorCondition := "AND (p.last_sub_id = '' OR s.id >= p.last_sub_id) "

	query := fmt.Sprintf(`
		WITH
			params AS (
				SELECT $5::text AS last_sub_id
			),
			subs_batch AS (
				SELECT
					s.id
				FROM
					subscriptions s, params p
				WHERE
					s.tenant_id = $1
					AND s.environment_id = $2
					AND s.status = '%s'
					AND s.plan_id = $3
					AND s.subscription_status IN ('%s', '%s')
					%s
				ORDER BY s.id
				LIMIT $4
			)
		SELECT
			COALESCE(MAX(s.id), '') AS last_sub_id
		FROM
			subs_batch s
		`,
		string(types.StatusPublished),
		string(types.SubscriptionStatusActive),
		string(types.SubscriptionStatusTrialing),
		cursorCondition,
	)

	cursorParam := ""
	if hasCursor {
		cursorParam = cursorSubID
	}
	args := []interface{}{
		tenantID,
		environmentID,
		planID,
		limit,
		cursorParam,
	}

	rows, qerr := r.client.Reader(ctx).QueryContext(ctx, query, args...)
	if qerr != nil {
		r.log.Errorw("failed to query last subscription ID in batch",
			"plan_id", planID,
			"limit", limit,
			"error", qerr)
		return nil, ierr.WithError(qerr).
			WithHint("Failed to get last subscription ID in batch").
			WithReportableDetails(map[string]any{
				"plan_id": planID,
				"limit":   limit,
			}).
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var batchLastSubID string
	if rows.Next() {
		if scanErr := rows.Scan(&batchLastSubID); scanErr != nil {
			r.log.Errorw("failed to scan last subscription ID",
				"plan_id", planID,
				"limit", limit,
				"error", scanErr)
			return nil, ierr.WithError(scanErr).
				WithHint("Failed to scan last subscription ID").
				Mark(ierr.ErrDatabase)
		}
	} else {
		batchLastSubID = ""
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		r.log.Errorw("failed to iterate rows for last subscription ID",
			"plan_id", planID,
			"limit", limit,
			"error", rowsErr)
		return nil, ierr.WithError(rowsErr).
			WithHint("Failed to iterate rows for last subscription ID").
			Mark(ierr.ErrDatabase)
	}

	if batchLastSubID == "" || batchLastSubID == cursorSubID {
		return nil, nil
	}
	return &batchLastSubID, nil
}
