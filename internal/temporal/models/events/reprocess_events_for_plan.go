package models

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// MissingPair represents a (subscription_id, price_id, customer_id) pair for reprocess events for plan workflow.
type MissingPair struct {
	SubscriptionID string `json:"subscription_id"`
	PriceID        string `json:"price_id"`
	CustomerID     string `json:"customer_id"`
}

// ReprocessEventsForPlanWorkflowInput represents the input for the reprocess events for plan workflow.
type ReprocessEventsForPlanWorkflowInput struct {
	MissingPairs  []MissingPair `json:"missing_pairs"`
	TenantID      string        `json:"tenant_id"`
	EnvironmentID string        `json:"environment_id"`
	UserID        string        `json:"user_id"`
}

// Validate validates the reprocess events for plan workflow input.
func (i *ReprocessEventsForPlanWorkflowInput) Validate() error {
	if i.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}
	if i.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("Environment ID is required").
			Mark(ierr.ErrValidation)
	}
	// Allow empty MissingPairs (no-op)
	return nil
}

// ReprocessEventsForPlanWorkflowResult represents the result of the reprocess events for plan workflow.
type ReprocessEventsForPlanWorkflowResult struct {
	CustomerIDs     []string  `json:"customer_ids"`
	SubscriptionIDs []string  `json:"subscription_ids"`
	PriceIDs        []string  `json:"price_ids"`
	PairCount       int       `json:"pair_count"`
	Message         string    `json:"message"`
	CompletedAt     time.Time `json:"completed_at"`
}
