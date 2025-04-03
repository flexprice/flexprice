package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/proration"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// SubscriptionChangeOperation represents a single atomic change to a subscription
type SubscriptionChangeOperation interface {
	// Validate checks if the operation can be applied to the subscription
	Validate(ctx context.Context, sub *subscription.Subscription) error

	// Plan calculates the details and impact of this operation without executing it
	Plan(ctx context.Context, sub *subscription.Subscription) (*OperationPlan, error)

	// Execute applies the planned changes to the subscription
	// If isPreview is true, it won't persist any changes
	Execute(ctx context.Context, sub *subscription.Subscription, plan *OperationPlan, isPreview bool) error
}

// OperationPlan contains the details of a planned subscription change
type OperationPlan struct {
	ProrationResult     *proration.ProrationResult
	InvoiceLineItems    []interface{} // Will be dto.CreateInvoiceLineItemRequest in implementation
	UpdatedSubscription *subscription.Subscription
	Errors              []error
}

// CancellationOperation implements the subscription.SubscriptionChangeOperation interface
// for canceling a subscription
type CancellationOperation struct {
	CancelAtPeriodEnd bool
	ProrationParams   *proration.ProrationParams
	ProrationService  ProrationService
	SubRepo           subscription.Repository
}

// NewCancellationOperation creates a new cancellation operation
func NewCancellationOperation(
	cancelAtPeriodEnd bool,
	prorationParams *proration.ProrationParams,
	prorationService ProrationService,
	subRepo subscription.Repository,
) *CancellationOperation {
	// If proration params are nil, create default params
	if prorationParams == nil {
		prorationParams = proration.GetDefaultProrationParams()
	}

	// Ensure the action is set to cancellation
	prorationParams.Action = types.ProrationActionCancellation

	return &CancellationOperation{
		CancelAtPeriodEnd: cancelAtPeriodEnd,
		ProrationParams:   prorationParams,
		ProrationService:  prorationService,
		SubRepo:           subRepo,
	}
}

// Plan calculates the impact of canceling a subscription without actually doing it
func (o *CancellationOperation) Plan(
	ctx context.Context,
	sub *subscription.Subscription,
) (*OperationPlan, error) {
	// Create a copy of the subscription to simulate changes
	updatedSub := sub.Copy()

	// Calculate what would happen if we cancel
	now := time.Now().UTC()
	updatedSub.CancelledAt = &now

	if o.CancelAtPeriodEnd {
		updatedSub.CancelAtPeriodEnd = true
		updatedSub.CancelAt = lo.ToPtr(sub.CurrentPeriodEnd)
	} else {
		updatedSub.SubscriptionStatus = types.SubscriptionStatusCancelled
		updatedSub.CancelAt = nil
	}

	// Calculate proration if immediate cancellation and proration is enabled
	var prorationResult *proration.ProrationResult
	var err error

	if !o.CancelAtPeriodEnd {
		prorationResult, err = o.ProrationService.CalculateProration(ctx, sub, o.ProrationParams)
		if err != nil {
			return nil, err
		}
	}

	// Create the operation plan
	plan := &OperationPlan{
		ProrationResult:     prorationResult,
		UpdatedSubscription: updatedSub,
	}

	return plan, nil
}

// Execute applies the cancellation to the subscription
func (o *CancellationOperation) Execute(
	ctx context.Context,
	sub *subscription.Subscription,
	plan *OperationPlan,
	isPreview bool,
) error {
	if isPreview {
		// No actual execution needed for preview
		return nil
	}

	// Apply the changes from the plan to the subscription
	// Instead of duplicating the update logic, use the updated subscription from the plan
	if plan.UpdatedSubscription != nil {
		// Copy the updated fields from the plan to the subscription
		sub.SubscriptionStatus = plan.UpdatedSubscription.SubscriptionStatus
		sub.CancelledAt = plan.UpdatedSubscription.CancelledAt
		sub.CancelAt = plan.UpdatedSubscription.CancelAt
		sub.CancelAtPeriodEnd = plan.UpdatedSubscription.CancelAtPeriodEnd
	}

	// Apply proration if needed
	if !o.CancelAtPeriodEnd && plan.ProrationResult != nil {
		if err := o.ProrationService.ApplyProration(ctx, sub, plan.ProrationResult, o.ProrationParams.ProrationBehavior); err != nil {
			return err
		}
	}

	// Update the subscription in the database
	if err := o.SubRepo.Update(ctx, sub); err != nil {
		return err
	}

	return nil
}

// Validate checks if the cancellation operation can be applied to the subscription
func (o *CancellationOperation) Validate(ctx context.Context, sub *subscription.Subscription) error {
	// Check if subscription is already cancelled
	if sub.SubscriptionStatus == types.SubscriptionStatusCancelled {
		return ierr.NewError("subscription is already cancelled").
			WithHint("The subscription is already in a cancelled state").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": sub.ID,
				"status":          sub.SubscriptionStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	// Check if subscription is already scheduled for cancellation
	if sub.CancelAtPeriodEnd {
		return ierr.NewError("subscription is already scheduled for cancellation").
			WithHint("The subscription is already scheduled to be cancelled at the end of the current billing period").
			WithReportableDetails(map[string]interface{}{
				"subscription_id":      sub.ID,
				"cancel_at_period_end": sub.CancelAtPeriodEnd,
				"cancel_at":            sub.CancelAt,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

// UpdateLineItemOperation implements the SubscriptionChangeOperation interface
// for updating a line item's quantity
type UpdateLineItemOperation struct {
	LineItemID       string
	NewQuantity      *decimal.Decimal
	NewPriceID       *string
	ProrationParams  *proration.ProrationParams
	ProrationService ProrationService
	SubRepo          subscription.Repository
	PriceRepo        price.Repository
}

// NewUpdateLineItemOperation creates a new operation for updating a line item
func NewUpdateLineItemOperation(
	lineItemID string,
	newPriceID *string,
	newQuantity *decimal.Decimal,
	prorationParams *proration.ProrationParams,
	prorationService ProrationService,
	subRepo subscription.Repository,
	priceRepo price.Repository,
) *UpdateLineItemOperation {
	// If proration params are nil, create default params
	if prorationParams == nil {
		prorationParams = proration.GetDefaultProrationParams()
	}

	// Create a copy of newQuantity for the proration params
	quantityCopy := newQuantity
	priceIDCopy := newPriceID

	prorationParams.NewPriceID = priceIDCopy
	prorationParams.NewQuantity = quantityCopy
	prorationParams.LineItemID = lineItemID

	updateLineItemOperation := &UpdateLineItemOperation{
		LineItemID:       lineItemID,
		NewQuantity:      prorationParams.NewQuantity,
		NewPriceID:       prorationParams.NewPriceID,
		ProrationParams:  prorationParams,
		ProrationService: prorationService,
		SubRepo:          subRepo,
		PriceRepo:        priceRepo,
	}

	// if price id is nil, set it to the price id of the line item
	if prorationParams.NewPriceID != nil {
		prorationParams.Action = types.ProrationActionPlanChange
	} else {
		// Ensure the action is set to quantity change
		prorationParams.Action = types.ProrationActionQuantityChange
	}

	return updateLineItemOperation
}

// Validate checks if the update operation can be applied to the subscription
func (o *UpdateLineItemOperation) Validate(ctx context.Context, sub *subscription.Subscription) error {
	// Check if the line item exists
	lineItem := sub.GetLineItemByID(o.LineItemID)
	if lineItem == nil {
		return ierr.NewError("line item not found").
			WithHint("The specified line item does not exist in this subscription").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": sub.ID,
				"line_item_id":    o.LineItemID,
			}).
			Mark(ierr.ErrNotFound)
	}

	if o.NewQuantity == nil && o.NewPriceID == nil {
		return ierr.NewError("no changes detected").
			WithHint("Either price or quantity must change").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": sub.ID,
				"line_item_id":    o.LineItemID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Check if the quantity is valid (greater than zero)
	if o.NewQuantity != nil {
		// first check if the quantity is positive
		if o.NewQuantity.LessThanOrEqual(decimal.Zero) {
			return ierr.NewError("invalid quantity").
				WithHint("Quantity must be greater than zero").
				WithReportableDetails(map[string]interface{}{
					"subscription_id": sub.ID,
					"line_item_id":    o.LineItemID,
					"quantity":        o.NewQuantity,
				}).
				Mark(ierr.ErrValidation)
		}

		// then check if the quantity is changing if there is no price id change
		if o.NewPriceID == nil || lo.FromPtr(o.NewPriceID) == lineItem.PriceID {
			if o.NewQuantity.Equal(lineItem.Quantity) {
				return ierr.NewError("quantity unchanged").
					WithHint("The new quantity is the same as the current quantity").
					WithReportableDetails(map[string]interface{}{
						"subscription_id": sub.ID,
						"line_item_id":    o.LineItemID,
						"new_quantity":    o.NewQuantity,
						"old_quantity":    lineItem.Quantity,
					}).
					Mark(ierr.ErrValidation)
			}
		}
	}

	// Check if the new price exists and is compatible with the subscription
	if o.NewPriceID != nil && *o.NewPriceID != "" {
		// if the price id is the same as the line item price id,
		// we don't need to check if the price exists
		// and since we have already checked if the quantity is changing,
		// we can return an error in case of no changes
		if lineItem.PriceID == *o.NewPriceID {
			return ierr.NewError("no changes detected").
				WithHint("Either price or quantity must change").
				WithReportableDetails(map[string]interface{}{
					"subscription_id": sub.ID,
					"line_item_id":    o.LineItemID,
				}).
				Mark(ierr.ErrValidation)
		}

		price, err := o.PriceRepo.Get(ctx, *o.NewPriceID)
		if err != nil {
			return ierr.WithError(err).
				WithHint("The specified price does not exist").
				WithReportableDetails(map[string]interface{}{
					"price_id": *o.NewPriceID,
				}).
				Mark(ierr.ErrNotFound)
		}

		// price types should be same for the line item and the new price
		if lineItem.PriceType != price.Type {
			return ierr.NewError("price type mismatch").
				WithHint("The price type must match the line item price type").
				WithReportableDetails(map[string]interface{}{
					"subscription_id": sub.ID,
					"line_item_id":    o.LineItemID,
					"old_price_type":  lineItem.PriceType,
					"new_price_type":  price.Type,
				}).
				Mark(ierr.ErrValidation)
		}

		// Check if the price currency matches the subscription currency
		if price.Currency != sub.Currency {
			return ierr.NewError("currency mismatch").
				WithHint("The price currency must match the subscription currency").
				WithReportableDetails(map[string]interface{}{
					"subscription_id":       sub.ID,
					"subscription_currency": sub.Currency,
					"price_id":              *o.NewPriceID,
					"price_currency":        price.Currency,
				}).
				Mark(ierr.ErrValidation)
		}

		// Check if the price billing period matches the subscription billing period
		// TODO: This is a temporary check to ensure that the price billing period matches the subscription billing period
		// We will remove this check in future phases when we support different billing periods for different line items
		// and in that case we will also need to update the subscription billing period and billing period count
		if price.BillingPeriod != sub.BillingPeriod || price.BillingPeriodCount != sub.BillingPeriodCount {
			return ierr.NewError("billing period mismatch").
				WithHint("The price billing period must match the subscription billing period").
				WithReportableDetails(map[string]interface{}{
					"subscription_id":             sub.ID,
					"subscription_billing_period": sub.BillingPeriod,
					"subscription_period_count":   sub.BillingPeriodCount,
					"price_id":                    *o.NewPriceID,
					"price_billing_period":        price.BillingPeriod,
					"price_period_count":          price.BillingPeriodCount,
				}).
				Mark(ierr.ErrValidation)
		}

	}

	return nil
}

// Plan calculates the impact of updating a line item without actually doing it
func (o *UpdateLineItemOperation) Plan(
	ctx context.Context,
	sub *subscription.Subscription,
) (*OperationPlan, error) {
	// Find the line item
	lineItem := sub.GetLineItemByID(o.LineItemID)
	if lineItem == nil {
		return nil, ierr.NewError("line item not found").Mark(ierr.ErrNotFound)
	}

	// Create a copy of the subscription to simulate changes
	updatedSub := sub.Copy()

	// Update the line item in the copy
	for i := range updatedSub.LineItems {
		if updatedSub.LineItems[i].ID == o.LineItemID {
			// Set the old quantity and price ID in the proration params
			oldQuantity := updatedSub.LineItems[i].Quantity
			oldPriceID := updatedSub.LineItems[i].PriceID
			o.ProrationParams.OldQuantity = &oldQuantity
			o.ProrationParams.OldPriceID = &oldPriceID

			// Update the quantity if provided
			if o.NewQuantity != nil {
				updatedSub.LineItems[i].Quantity = *o.NewQuantity
			}

			// Update the price ID if provided
			if o.NewPriceID != nil {
				// Get the new price details
				newPrice, err := o.PriceRepo.Get(ctx, *o.NewPriceID)
				if err != nil {
					return nil, err
				}

				updatedSub.LineItems[i].PriceID = *o.NewPriceID
				updatedSub.LineItems[i].PlanID = newPrice.PlanID
				updatedSub.LineItems[i].PriceType = newPrice.Type
				// TODO: handle cases of change in invoice cadence in case we need to
				// handle anything else apart from proration
				updatedSub.LineItems[i].InvoiceCadence = newPrice.InvoiceCadence
				updatedSub.LineItems[i].BillingPeriod = newPrice.BillingPeriod
				// TODO: handle what if the new price has a trial period?
				updatedSub.LineItems[i].TrialPeriod = newPrice.TrialPeriod
				updatedSub.LineItems[i].MeterID = newPrice.MeterID
				// TODO: update meter display name and plan display name
			}
			break
		}
	}

	// Calculate proration
	var prorationResult *proration.ProrationResult
	var err error

	prorationResult, err = o.ProrationService.CalculateProration(ctx, sub, o.ProrationParams)
	if err != nil {
		return nil, err
	}

	// Create the operation plan
	plan := &OperationPlan{
		ProrationResult:     prorationResult,
		UpdatedSubscription: updatedSub,
	}

	return plan, nil
}

// Execute applies the update to the line item
func (o *UpdateLineItemOperation) Execute(
	ctx context.Context,
	sub *subscription.Subscription,
	plan *OperationPlan,
	isPreview bool,
) error {
	if isPreview {
		// No actual execution needed for preview
		return nil
	}

	// Find the line item
	lineItem := sub.GetLineItemByID(o.LineItemID)
	if lineItem == nil {
		return ierr.NewError("line item not found").Mark(ierr.ErrNotFound)
	}

	// Apply proration if needed
	if plan.ProrationResult != nil {
		if err := o.ProrationService.ApplyProration(ctx, sub, plan.ProrationResult, o.ProrationParams.ProrationBehavior); err != nil {
			return err
		}
	}

	// Apply the changes from the plan to the subscription and line items
	if plan.UpdatedSubscription != nil {
		if err := o.SubRepo.Update(ctx, plan.UpdatedSubscription); err != nil {
			return err
		}
	}

	return nil
}

// AddLineItemOperation implements the SubscriptionChangeOperation interface
// for adding a new line item to a subscription
type AddLineItemOperation struct {
	PriceID          string
	Quantity         decimal.Decimal
	DisplayName      *string
	Metadata         map[string]string
	ProrationParams  *proration.ProrationParams
	ProrationService ProrationService
	SubRepo          subscription.Repository
	PriceRepo        price.Repository
}

// NewAddLineItemOperation creates a new operation for adding a line item
func NewAddLineItemOperation(
	priceID string,
	quantity decimal.Decimal,
	displayName *string,
	metadata map[string]string,
	prorationParams *proration.ProrationParams,
	prorationService ProrationService,
	subRepo subscription.Repository,
	priceRepo price.Repository,
) *AddLineItemOperation {
	// If proration params are nil, create default params
	if prorationParams == nil {
		prorationParams = proration.GetDefaultProrationParams()
	}

	// Ensure the action is set to add item
	prorationParams.Action = types.ProrationActionAddItem

	// Set the new price ID and quantity in the proration params
	newPriceID := priceID
	newQuantity := quantity
	prorationParams.NewPriceID = &newPriceID
	prorationParams.NewQuantity = &newQuantity

	return &AddLineItemOperation{
		PriceID:          priceID,
		Quantity:         quantity,
		DisplayName:      displayName,
		Metadata:         metadata,
		ProrationParams:  prorationParams,
		ProrationService: prorationService,
		SubRepo:          subRepo,
		PriceRepo:        priceRepo,
	}
}

// Validate checks if the add operation can be applied to the subscription
func (o *AddLineItemOperation) Validate(ctx context.Context, sub *subscription.Subscription) error {
	// Check if the price exists
	price, err := o.PriceRepo.Get(ctx, o.PriceID)
	if err != nil {
		return ierr.WithError(err).
			WithHint("The specified price does not exist").
			WithReportableDetails(map[string]interface{}{
				"price_id": o.PriceID,
			}).
			Mark(ierr.ErrNotFound)
	}

	// Check if the quantity is valid (greater than zero)
	if o.Quantity.LessThanOrEqual(decimal.Zero) {
		return ierr.NewError("invalid quantity").
			WithHint("Quantity must be greater than zero").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": sub.ID,
				"price_id":        o.PriceID,
				"quantity":        o.Quantity,
			}).
			Mark(ierr.ErrValidation)
	}

	// Check if the price currency matches the subscription currency
	if price.Currency != sub.Currency {
		return ierr.NewError("currency mismatch").
			WithHint("The price currency must match the subscription currency").
			WithReportableDetails(map[string]interface{}{
				"subscription_id":       sub.ID,
				"subscription_currency": sub.Currency,
				"price_id":              o.PriceID,
				"price_currency":        price.Currency,
			}).
			Mark(ierr.ErrValidation)
	}

	// Check if the price billing period matches the subscription billing period
	if price.BillingPeriod != sub.BillingPeriod || price.BillingPeriodCount != sub.BillingPeriodCount {
		return ierr.NewError("billing period mismatch").
			WithHint("The price billing period must match the subscription billing period").
			WithReportableDetails(map[string]interface{}{
				"subscription_id":             sub.ID,
				"subscription_billing_period": sub.BillingPeriod,
				"subscription_period_count":   sub.BillingPeriodCount,
				"price_id":                    o.PriceID,
				"price_billing_period":        price.BillingPeriod,
				"price_period_count":          price.BillingPeriodCount,
			}).
			Mark(ierr.ErrValidation)
	}

	// Check if the price has a trial period
	if price.TrialPeriod > 0 {
		return ierr.NewError("price has a trial period").
			WithHint("The price has a trial period, which is not supported for add item operations").
			WithReportableDetails(map[string]interface{}{
				"price_id":     o.PriceID,
				"trial_period": price.TrialPeriod,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

// Plan calculates the impact of adding a line item without actually doing it
func (o *AddLineItemOperation) Plan(
	ctx context.Context,
	sub *subscription.Subscription,
) (*OperationPlan, error) {
	// Get the price details
	price, err := o.PriceRepo.Get(ctx, o.PriceID)
	if err != nil {
		return nil, err
	}

	// Create a copy of the subscription to simulate changes
	updatedSub := sub.Copy()

	// Create a new line item
	newLineItem := &subscription.SubscriptionLineItem{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID: sub.ID,
		CustomerID:     sub.CustomerID,
		EnvironmentID:  sub.EnvironmentID,
		PriceID:        o.PriceID,
		Quantity:       o.Quantity,
		PriceType:      price.Type,
		Currency:       price.Currency,
		PlanID:         price.PlanID,
		BillingPeriod:  price.BillingPeriod,
		InvoiceCadence: price.InvoiceCadence,
		TrialPeriod:    price.TrialPeriod,
		StartDate:      time.Now().UTC(),
		DisplayName:    lo.FromPtr(o.DisplayName),
		MeterID:        price.MeterID,
		Metadata:       o.Metadata,
		BaseModel:      types.GetDefaultBaseModel(ctx),
		// TODO: handle these
		// MeterDisplayName: price.MeterDisplayName,
		// PlanDisplayName:  price.PlanDisplayName,
	}

	// Add the new line item to the subscription
	updatedSub.LineItems = append(updatedSub.LineItems, newLineItem)

	// Calculate proration
	var prorationResult *proration.ProrationResult
	prorationResult, err = o.ProrationService.CalculateProration(ctx, sub, o.ProrationParams)
	if err != nil {
		return nil, err
	}

	// Create the operation plan
	plan := &OperationPlan{
		ProrationResult:     prorationResult,
		UpdatedSubscription: updatedSub,
	}

	return plan, nil
}

// Execute applies the add operation to the subscription
func (o *AddLineItemOperation) Execute(
	ctx context.Context,
	sub *subscription.Subscription,
	plan *OperationPlan,
	isPreview bool,
) error {
	if isPreview {
		// No actual execution needed for preview
		return nil
	}

	// Apply proration if needed
	if plan.ProrationResult != nil {
		if err := o.ProrationService.ApplyProration(ctx, sub, plan.ProrationResult, o.ProrationParams.ProrationBehavior); err != nil {
			return err
		}
	}

	if plan.UpdatedSubscription != nil {
		if err := o.SubRepo.Update(ctx, plan.UpdatedSubscription); err != nil {
			return err
		}
	}

	return nil
}

// RemoveLineItemOperation implements the SubscriptionChangeOperation interface
// for removing a line item from a subscription
type RemoveLineItemOperation struct {
	LineItemID       string
	ProrationParams  *proration.ProrationParams
	ProrationService ProrationService
	SubRepo          subscription.Repository
}

// NewRemoveLineItemOperation creates a new operation for removing a line item
func NewRemoveLineItemOperation(
	lineItemID string,
	prorationParams *proration.ProrationParams,
	prorationService ProrationService,
	subRepo subscription.Repository,
) *RemoveLineItemOperation {
	// If proration params are nil, create default params
	if prorationParams == nil {
		prorationParams = proration.GetDefaultProrationParams()
	}

	// Ensure the action is set to remove item
	prorationParams.Action = types.ProrationActionRemoveItem
	prorationParams.LineItemID = lineItemID

	return &RemoveLineItemOperation{
		LineItemID:       lineItemID,
		ProrationParams:  prorationParams,
		ProrationService: prorationService,
		SubRepo:          subRepo,
	}
}

// Validate checks if the remove operation can be applied to the subscription
func (o *RemoveLineItemOperation) Validate(ctx context.Context, sub *subscription.Subscription) error {
	// Check if the line item exists
	lineItem := sub.GetLineItemByID(o.LineItemID)
	if lineItem == nil {
		return ierr.NewError("line item not found").
			WithHint("The specified line item does not exist in this subscription").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": sub.ID,
				"line_item_id":    o.LineItemID,
			}).
			Mark(ierr.ErrNotFound)
	}

	// Check if this is the only line item in the subscription
	if len(sub.LineItems) == 1 {
		return ierr.NewError("cannot remove the only line item").
			WithHint("A subscription must have at least one line item").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": sub.ID,
				"line_item_id":    o.LineItemID,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

// Plan calculates the impact of removing a line item without actually doing it
func (o *RemoveLineItemOperation) Plan(
	ctx context.Context,
	sub *subscription.Subscription,
) (*OperationPlan, error) {
	// Find the line item
	lineItem := sub.GetLineItemByID(o.LineItemID)
	if lineItem == nil {
		return nil, ierr.NewError("line item not found").Mark(ierr.ErrNotFound)
	}

	// Create a copy of the subscription to simulate changes
	updatedSub := sub.Copy()

	// Calculate proration
	var prorationResult *proration.ProrationResult
	var err error

	// Set the old price ID in the proration params
	o.ProrationParams.OldPriceID = &lineItem.PriceID
	o.ProrationParams.OldQuantity = &lineItem.Quantity

	prorationResult, err = o.ProrationService.CalculateProration(ctx, sub, o.ProrationParams)
	if err != nil {
		return nil, err
	}

	// Create the operation plan
	plan := &OperationPlan{
		ProrationResult:     prorationResult,
		UpdatedSubscription: updatedSub,
	}

	return plan, nil
}

// Execute applies the remove operation to the subscription
func (o *RemoveLineItemOperation) Execute(
	ctx context.Context,
	sub *subscription.Subscription,
	plan *OperationPlan,
	isPreview bool,
) error {
	if isPreview {
		// No actual execution needed for preview
		return nil
	}

	// Find the line item
	lineItem := sub.GetLineItemByID(o.LineItemID)
	if lineItem == nil {
		return ierr.NewError("line item not found").Mark(ierr.ErrNotFound)
	}

	// Apply proration if needed
	if plan.ProrationResult != nil {
		if err := o.ProrationService.ApplyProration(ctx, sub, plan.ProrationResult, o.ProrationParams.ProrationBehavior); err != nil {
			return err
		}
	}

	// Remove the line item from the subscription
	newLineItems := make([]*subscription.SubscriptionLineItem, 0, len(sub.LineItems)-1)
	for _, item := range sub.LineItems {
		if item.ID != o.LineItemID {
			newLineItems = append(newLineItems, item)
		}
	}
	sub.LineItems = newLineItems

	// Update the subscription in the database
	if err := o.SubRepo.Update(ctx, sub); err != nil {
		return err
	}

	return nil
}
