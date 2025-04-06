package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// SubscriptionUpdateOrchestrator manages the end-to-end process of updating subscriptions
type SubscriptionUpdateOrchestrator interface {
	// ProcessUpdateWithOperations handles updates with pre-created operations
	ProcessUpdateWithOperations(ctx context.Context, subID string,
		operations []SubscriptionChangeOperation, isPreview bool) (*dto.SubscriptionResponse, error)
}

// subscriptionUpdateOrchestrator implements the subscription.SubscriptionUpdateOrchestrator interface
type subscriptionUpdateOrchestrator struct {
	ServiceParams
	OperationFactory *SubscriptionOperationFactory
}

// NewSubscriptionUpdateOrchestrator creates a new orchestrator
func NewSubscriptionUpdateOrchestrator(params ServiceParams) SubscriptionUpdateOrchestrator {
	return &subscriptionUpdateOrchestrator{
		ServiceParams:    params,
		OperationFactory: NewSubscriptionOperationFactory(params),
	}
}

// ProcessUpdateWithOperations handles updates with pre-created operations
func (o *subscriptionUpdateOrchestrator) ProcessUpdateWithOperations(
	ctx context.Context,
	subID string,
	operations []SubscriptionChangeOperation,
	isPreview bool,
) (*dto.SubscriptionResponse, error) {
	// Get the subscription
	sub, lineItems, err := o.SubRepo.GetWithLineItems(ctx, subID)
	if err != nil {
		return nil, err
	}

	// Attach line items to subscription
	sub.LineItems = lineItems

	// Validate all operations before proceeding and add them to the operations list if valid
	validOperations := make([]SubscriptionChangeOperation, 0, len(operations))
	for _, op := range operations {
		if err := op.Validate(ctx, sub); err != nil {
			// skip the operation if it's invalid
			o.Logger.Errorw("skipping invalid operation while updating subscription",
				"operation", op,
				"error", err,
				"subscription_id", subID,
			)
			continue
		}

		validOperations = append(validOperations, op)
	}

	if len(validOperations) == 0 {
		return nil, ierr.NewError("no valid operations to apply").
			WithHint("No valid operations to be applied to the subscription").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": subID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Plan all operations
	plans, err := o.planOperations(ctx, sub, validOperations)
	if err != nil {
		return nil, err
	}

	// For preview mode, just return the planned changes
	if isPreview {
		return o.createPreviewResponse(sub, plans)
	}

	// Use consistent transaction pattern for actual execution
	var updatedSub *subscription.Subscription
	var invoice *dto.InvoiceResponse

	err = o.DB.WithTx(ctx, func(ctx context.Context) error {
		// Execute the operations in transaction context
		var execErr error
		updatedSub, invoice, execErr = o.executeOperations(ctx, sub, validOperations, plans)
		return execErr
	})

	if err != nil {
		return nil, err
	}

	// Create response
	return o.createResponse(updatedSub, invoice, plans)
}

// planOperations calculates the impact of all operations
func (o *subscriptionUpdateOrchestrator) planOperations(
	ctx context.Context,
	sub *subscription.Subscription,
	operations []SubscriptionChangeOperation,
) ([]*OperationPlan, error) {
	plans := make([]*OperationPlan, 0, len(operations))

	// Create a working copy of the subscription for planning
	workingSub := *sub // Make a copy

	for _, op := range operations {
		plan, err := op.Plan(ctx, &workingSub)
		if err != nil {
			return nil, err
		}

		plans = append(plans, plan)

		// Update the working subscription for next operations
		// This ensures operations are planned in the context of previous ones
		if plan.UpdatedSubscription != nil {
			workingSub = *plan.UpdatedSubscription
		}
	}

	return plans, nil
}

// executeOperations applies all operations
func (o *subscriptionUpdateOrchestrator) executeOperations(
	ctx context.Context,
	sub *subscription.Subscription,
	operations []SubscriptionChangeOperation,
	plans []*OperationPlan,
) (*subscription.Subscription, *dto.InvoiceResponse, error) {
	// Make a copy of the subscription to work with
	workingSub := *sub

	// Execute each operation
	for i, op := range operations {
		if err := op.Execute(ctx, &workingSub, plans[i], false); err != nil {
			return nil, nil, err
		}
	}

	// Get the latest invoice if one was created
	// This is a simplification - in a real implementation we would track
	// which operations created invoices and return them
	var invoice *dto.InvoiceResponse

	// Return the updated subscription and invoice
	return &workingSub, invoice, nil
}

// createPreviewResponse creates a response for preview mode
func (o *subscriptionUpdateOrchestrator) createPreviewResponse(
	sub *subscription.Subscription,
	plans []*OperationPlan,
) (*dto.SubscriptionResponse, error) {
	// In a real implementation, we would combine all the proration results
	// and create a preview invoice

	// For now, just return the updated subscription from the last plan
	var updatedSub *subscription.Subscription
	if len(plans) > 0 && plans[len(plans)-1].UpdatedSubscription != nil {
		updatedSub = plans[len(plans)-1].UpdatedSubscription
	} else {
		updatedSub = sub
	}

	return &dto.SubscriptionResponse{
		Subscription: updatedSub,
	}, nil
}

// createResponse creates a response for actual execution
func (o *subscriptionUpdateOrchestrator) createResponse(
	updatedSub *subscription.Subscription,
	_ *dto.InvoiceResponse,
	_ []*OperationPlan,
) (*dto.SubscriptionResponse, error) {
	// For now, just return the subscription
	// In a real implementation, we would include the invoice in a custom response type
	return &dto.SubscriptionResponse{
		Subscription: updatedSub,
	}, nil
}
