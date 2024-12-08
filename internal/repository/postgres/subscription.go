package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type subscriptionRepository struct {
	db     *postgres.DB
	logger *logger.Logger
}

func NewSubscriptionRepository(db *postgres.DB, logger *logger.Logger) subscription.Repository {
	return &subscriptionRepository{db: db, logger: logger}
}

func (r *subscriptionRepository) Create(ctx context.Context, subscription *subscription.Subscription) error {
	query := `
		INSERT INTO subscriptions (
			id, 
			lookup_key, 
			customer_id, 
			plan_id, 
			subscription_status,
			currency,
			billing_anchor, 
			start_date, 
			end_date, 
			current_period_start,
			current_period_end, 
			cancelled_at, 
			cancel_at, 
			cancel_at_period_end,
			trial_start, 
			trial_end, 
			invoice_cadence,
			billing_cadence,
			billing_period,
			billing_period_unit,
			tenant_id, 
			status, 
			created_at, 
			updated_at, 
			created_by, 
			updated_by
		) VALUES (
			:id, 
			:lookup_key, 
			:customer_id, 
			:plan_id, 
			:subscription_status,
			:currency,
			:billing_anchor, 
			:start_date, 
			:end_date, 
			:current_period_start,
			:current_period_end, 
			:cancelled_at, 
			:cancel_at, 
			:cancel_at_period_end,
			:trial_start, 
			:trial_end, 
			:invoice_cadence,
			:billing_cadence,
			:billing_period,
			:billing_period_unit,
			:tenant_id, 
			:status, 
			:created_at, 
			:updated_at, 
			:created_by, 
			:updated_by
		)
	`

	_, err := r.db.NamedExecContext(ctx, query, subscription)

	if err != nil {
		return fmt.Errorf("failed to create subscription: %w", err)
	}

	return nil
}

func (r *subscriptionRepository) Get(ctx context.Context, id string) (*subscription.Subscription, error) {
	query := `
		SELECT * FROM subscriptions 
		WHERE 
			id = :id AND 
			tenant_id = :tenant_id
	`

	var sub subscription.Subscription
	rows, err := r.db.NamedQueryContext(ctx, query, map[string]interface{}{
		"id":        id,
		"tenant_id": types.GetTenantID(ctx),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get subscription: %w", err)
	}

	defer rows.Close()

	if !rows.Next() {
		return nil, fmt.Errorf("subscription not found")
	}

	if err := rows.StructScan(&sub); err != nil {
		return nil, fmt.Errorf("failed to scan subscription: %w", err)
	}

	return &sub, nil
}

func (r *subscriptionRepository) Update(ctx context.Context, subscription *subscription.Subscription) error {
	// TODO: Implement this after chalking out proper use cases here

	subscription.UpdatedAt = time.Now().UTC()
	subscription.UpdatedBy = types.GetUserID(ctx)

	query := `
		UPDATE subscriptions 
		SET 
			subscription_status = :subscription_status,
			cancelled_at = :cancelled_at,
			cancel_at = :cancel_at,
			cancel_at_period_end = :cancel_at_period_end,
			status = :status, 
			updated_at = :updated_at, 
			updated_by = :updated_by
		WHERE 
			id = :id AND 
			tenant_id = :tenant_id
	`

	_, err := r.db.NamedExecContext(ctx, query, subscription)
	if err != nil {
		return fmt.Errorf("failed to update subscription: %w", err)
	}
	return nil
}

func (r *subscriptionRepository) Delete(ctx context.Context, id string) error {
	query := `
		UPDATE subscriptions 
		SET 
			status = :status, 
			updated_at = :updated_at, 
			updated_by = :updated_by
		WHERE 
			id = :id AND 
			tenant_id = :tenant_id
	`

	_, err := r.db.NamedExecContext(ctx, query, map[string]interface{}{
		"status":     types.StatusDeleted,
		"updated_at": time.Now(),
		"updated_by": types.GetUserID(ctx),
		"id":         id,
		"tenant_id":  types.GetTenantID(ctx),
	})
	if err != nil {
		return fmt.Errorf("failed to delete subscription: %w", err)
	}
	return nil
}

func (r *subscriptionRepository) List(ctx context.Context, filter *types.SubscriptionFilter) ([]*subscription.Subscription, error) {
	query := `
		SELECT * FROM subscriptions 
		WHERE tenant_id = :tenant_id
	`
	params := filter.ToMap()
	params["tenant_id"] = types.GetTenantID(ctx)

	// Build dynamic where clauses
	if filter.CustomerID != "" {
		query += " AND customer_id = :customer_id"
	}
	if filter.Status != "" {
		query += " AND status = :status"
	}
	if filter.PlanID != "" {
		query += " AND plan_id = :plan_id"
	}
	if filter.SubscriptionStatus != "" {
		query += " AND subscription_status = :subscription_status"
	}

	// Add ordering and pagination
	query += " ORDER BY created_at DESC LIMIT :limit OFFSET :offset"

	rows, err := r.db.NamedQueryContext(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list subscriptions: %w", err)
	}
	defer rows.Close()

	var subscriptions []*subscription.Subscription
	for rows.Next() {
		var sub subscription.Subscription
		if err := rows.StructScan(&sub); err != nil {
			return nil, fmt.Errorf("failed to scan subscription: %w", err)
		}
		subscriptions = append(subscriptions, &sub)
	}

	return subscriptions, nil
}
