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

	r.log.Debugw("terminating plan line items",
		"plan_id", planID,
		"limit", limit,
		"tenant_id", tenantID,
		"environment_id", environmentID,
	)

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
		userID, // updated_by
	)
	if qerr != nil {
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
		// Just log warining or ignore, but strictly we can return error or count as 0 with error
		// For now let's treat it as DB error but it depends on driver
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

	r.log.Debugw("listing plan line items to terminate",
		"plan_id", planID,
		"limit", limit,
		"tenant_id", tenantID,
		"environment_id", environmentID,
	)

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
		ORDER BY
			li.id
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
			SetSpanError(span, scanErr)
			return nil, ierr.WithError(scanErr).
				WithHint("Failed to scan termination delta row").
				Mark(ierr.ErrDatabase)
		}
		items = append(items, d)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
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

	r.log.Debugw("listing plan line items to create",
		"plan_id", planID,
		"limit", limit,
		"tenant_id", tenantID,
		"environment_id", environmentID,
	)

	span := StartRepositorySpan(ctx, "plan_price_sync", "list_plan_line_items_to_create", map[string]interface{}{
		"plan_id": planID,
		"limit":   limit,
	})
	defer FinishSpan(span)

	query := fmt.Sprintf(`
		WITH
			subs AS (
				SELECT
					id,
					tenant_id,
					environment_id,
					currency,
					billing_period,
					billing_period_count
				FROM
					subscriptions
				WHERE
					tenant_id = $1
					AND environment_id = $2
					AND status = '%s'
					AND plan_id = $3
					AND subscription_status IN ('%s', '%s')
			),
			plan_prices AS (
				SELECT
					id,
					tenant_id,
					environment_id,
					currency,
					billing_period,
					billing_period_count
				FROM
					prices
				WHERE
					tenant_id = $1
					AND environment_id = $2
					AND status = '%s'
					AND entity_type = '%s'
					AND entity_id = $3
					AND type <> '%s'
			)
		SELECT
			s.id AS subscription_id,
			p.id AS missing_price_id
		FROM
			subs s
			JOIN plan_prices p ON p.currency = s.currency
			AND p.billing_period = s.billing_period
			AND p.billing_period_count = s.billing_period_count
		WHERE
			-- ever-overridden
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
			-- missing: no plan LI exists at all for (sub, price)
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
		ORDER BY
			s.id,
			p.id
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
		string(types.PRICE_ENTITY_TYPE_SUBSCRIPTION),
		string(types.StatusPublished),
		string(types.SubscriptionLineItemEntityTypePlan),
	)

	rows, qerr := r.client.Reader(ctx).QueryContext(
		ctx,
		query,
		tenantID,
		environmentID,
		planID,
		limit,
	)
	if qerr != nil {
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
		SetSpanError(span, rowsErr)
		return nil, ierr.WithError(rowsErr).
			WithHint("Failed to iterate creation delta rows").
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return items, nil
}
