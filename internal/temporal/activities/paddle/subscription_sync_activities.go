package paddle

import (
	"context"
	"fmt"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration"
	paddleint "github.com/flexprice/flexprice/internal/integration/paddle"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/temporal"
)

// SubscriptionSyncActivities handles Paddle subscription sync activities.
type SubscriptionSyncActivities struct {
	integrationFactory *integration.Factory
	logger             *logger.Logger
}

// NewSubscriptionSyncActivities creates a new SubscriptionSyncActivities.
func NewSubscriptionSyncActivities(
	integrationFactory *integration.Factory,
	log *logger.Logger,
) *SubscriptionSyncActivities {
	return &SubscriptionSyncActivities{
		integrationFactory: integrationFactory,
		logger:             log,
	}
}

// SyncSubscriptionToPaddle creates the $0 bootstrap Paddle transaction for a subscription.
// Non-retryable on validation errors (missing address, no line items, no Paddle connection).
func (a *SubscriptionSyncActivities) SyncSubscriptionToPaddle(
	ctx context.Context,
	input models.PaddleSubscriptionSyncWorkflowInput,
) error {
	if err := input.Validate(); err != nil {
		return temporal.NewNonRetryableApplicationError(err.Error(), "ValidationError", err)
	}

	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	a.logger.Infow("syncing subscription to Paddle",
		"subscription_id", input.SubscriptionID,
		"customer_id", input.CustomerID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	paddleIntegration, err := a.integrationFactory.GetPaddleIntegration(ctx)
	if err != nil {
		if ierr.IsNotFound(err) {
			return temporal.NewNonRetryableApplicationError(
				"Paddle connection not configured",
				"ConnectionNotFound",
				err,
			)
		}
		a.logger.Errorw("failed to get Paddle integration for subscription sync",
			"error", err,
			"subscription_id", input.SubscriptionID)
		return err
	}

	// Load the full subscription with line items.
	sub, _, fetchErr := paddleIntegration.SyncSvc.GetSubscriptionWithLineItems(ctx, input.SubscriptionID)
	if fetchErr != nil {
		return fmt.Errorf("fetching subscription: %w", fetchErr)
	}

	// Sync all line item prices to Paddle products.
	productItems := make([]paddleint.EnsureBulkProductSyncedItem, 0, len(sub.LineItems))
	for _, li := range sub.LineItems {
		if li == nil || li.PriceID == "" {
			continue
		}
		name := li.PriceID
		if li.DisplayName != "" {
			name = li.DisplayName
		}
		productItems = append(productItems, paddleint.EnsureBulkProductSyncedItem{
			PriceID: li.PriceID,
			Name:    name,
		})
	}

	productsResp, prodErr := paddleIntegration.SyncSvc.EnsureBulkProductSynced(ctx, paddleint.EnsureBulkProductSyncedRequest{Items: productItems})
	if prodErr != nil {
		if ierr.IsValidation(prodErr) {
			return temporal.NewNonRetryableApplicationError(prodErr.Error(), "ValidationError", prodErr)
		}
		return fmt.Errorf("syncing products: %w", prodErr)
	}

	_, err = paddleIntegration.SyncSvc.EnsureSubscriptionSynced(ctx, paddleint.EnsureSubscriptionSyncedRequest{
		Subscription:       sub,
		PriceIDToProductID: productsResp.PriceIDToPaddleProductID,
	})
	if err != nil {
		if ierr.IsValidation(err) {
			return temporal.NewNonRetryableApplicationError(err.Error(), "ValidationError", err)
		}
		a.logger.Errorw("failed to ensure subscription synced to Paddle",
			"error", err,
			"subscription_id", input.SubscriptionID)
		return err
	}

	a.logger.Infow("successfully synced subscription to Paddle",
		"subscription_id", input.SubscriptionID,
		"customer_id", input.CustomerID)
	return nil
}

// CheckSubscriptionSyncStatus checks whether the subscription linked to the given invoice
// has an activated Paddle mapping.
// It returns a SubscriptionSyncStatusResult with Status "activated" or "not_synced",
// plus the resolved SubscriptionID and CustomerID so the parent workflow can pass them
// directly to the child PaddleSubscriptionSyncWorkflow.
func (a *SubscriptionSyncActivities) CheckSubscriptionSyncStatus(
	ctx context.Context,
	input models.PaddleInvoiceSyncWorkflowInput,
) (*models.SubscriptionSyncStatusResult, error) {
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	paddleIntegration, err := a.integrationFactory.GetPaddleIntegration(ctx)
	if err != nil {
		if ierr.IsNotFound(err) {
			// No Paddle connection — treat as activated (no sub sync needed).
			a.logger.Warnw("Paddle connection not configured, treating subscription as activated",
				"invoice_id", input.InvoiceID)
			return &models.SubscriptionSyncStatusResult{Status: "activated"}, nil
		}
		return nil, err
	}

	// Resolve subscription_id and customer_id: prefer input fields, fall back to invoice lookup.
	subID := input.SubscriptionID
	customerID := input.CustomerID
	if subID == "" || customerID == "" {
		inv, invErr := paddleIntegration.SyncSvc.GetInvoiceByID(ctx, input.InvoiceID)
		if invErr != nil {
			return nil, fmt.Errorf("fetching invoice to resolve subscription_id: %w", invErr)
		}
		if subID == "" {
			if inv.SubscriptionID == nil || *inv.SubscriptionID == "" {
				// Invoice not linked to a subscription — no sub sync needed.
				a.logger.Infow("invoice has no subscription_id, treating as activated",
					"invoice_id", input.InvoiceID)
				return &models.SubscriptionSyncStatusResult{
					Status:     "activated",
					CustomerID: inv.CustomerID,
				}, nil
			}
			subID = *inv.SubscriptionID
		}
		if customerID == "" {
			customerID = inv.CustomerID
		}
	}

	activated, err := paddleIntegration.SyncSvc.GetSubscriptionMappingStatus(ctx, subID)
	if err != nil {
		return nil, err
	}
	if activated {
		a.logger.Infow("Paddle subscription mapping exists, status: activated",
			"subscription_id", subID,
			"invoice_id", input.InvoiceID)
		return &models.SubscriptionSyncStatusResult{
			Status:         "activated",
			SubscriptionID: subID,
			CustomerID:     customerID,
		}, nil
	}
	a.logger.Infow("no Paddle subscription mapping found, status: not_synced",
		"subscription_id", subID,
		"invoice_id", input.InvoiceID)
	return &models.SubscriptionSyncStatusResult{
		Status:         "not_synced",
		SubscriptionID: subID,
		CustomerID:     customerID,
	}, nil
}
