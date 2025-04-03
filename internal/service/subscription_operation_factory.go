package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/proration"
	"github.com/shopspring/decimal"
)

// SubscriptionOperationFactory implements the OperationFactory interface
type SubscriptionOperationFactory struct {
	ServiceParams
	ProrationService ProrationService
}

// NewSubscriptionOperationFactory creates a new factory
func NewSubscriptionOperationFactory(params ServiceParams) *SubscriptionOperationFactory {
	return &SubscriptionOperationFactory{
		ServiceParams:    params,
		ProrationService: NewProrationService(params),
	}
}

// CreateFromRequest creates operations from an API request
func (f *SubscriptionOperationFactory) CreateFromRequest(ctx context.Context, updateReq *dto.UpdateSubscriptionRequest) ([]SubscriptionChangeOperation, error) {
	operations := make([]SubscriptionChangeOperation, 0)

	if updateReq.Items == nil {
		return operations, nil
	}

	// Create proration params
	prorationParams := proration.GetDefaultProrationParams()

	// If a proration date was specified, use it
	if updateReq.ProrationDate != nil {
		prorationParams.ProrationDate = *updateReq.ProrationDate
	} else {
		prorationParams.ProrationDate = time.Now().UTC()
	}

	// Set proration behavior and strategy
	prorationParams.ProrationBehavior = updateReq.ProrationBehavior
	prorationParams.ProrationStrategy = updateReq.ProrationStrategy

	for _, item := range updateReq.Items {
		// Create a copy of the proration params for each item
		itemProrationParams := *prorationParams

		// Handle add, remove, and update operations
		if item.ID == "" {
			// This is an add operation
			if item.PriceID == nil || item.Quantity == nil {
				// Skip invalid items
				continue
			}

			// Create the add operation
			addOp := f.CreateAddLineItemOperation(
				*item.PriceID,
				decimal.NewFromInt(*item.Quantity),
				item.DisplayName,
				item.Metadata,
				&itemProrationParams,
			)

			operations = append(operations, addOp)
		} else if item.Deleted {
			// This is a remove operation
			removeOp := f.CreateRemoveLineItemOperation(
				item.ID,
				&itemProrationParams,
			)

			operations = append(operations, removeOp)
		} else if item.Quantity != nil || item.PriceID != nil {
			// This is an update operation
			var quantity *decimal.Decimal
			if item.Quantity != nil {
				decimalQuantity := decimal.NewFromInt(*item.Quantity)
				quantity = &decimalQuantity
			}

			// Create the update operation
			updateOp := f.CreateUpdateLineItemOperation(
				item.ID,
				item.PriceID,
				quantity,
				&itemProrationParams,
			)

			operations = append(operations, updateOp)
		}
	}

	// Future: Handle metadata updates when implementing Phase 4

	return operations, nil
}

// CreateAddLineItemOperation creates an operation to add a line item
func (f *SubscriptionOperationFactory) CreateAddLineItemOperation(
	priceID string,
	quantity decimal.Decimal,
	displayName *string,
	metadata map[string]string,
	prorationOpts *proration.ProrationParams,
) SubscriptionChangeOperation {
	return NewAddLineItemOperation(
		priceID,
		quantity,
		displayName,
		metadata,
		prorationOpts,
		f.ProrationService,
		f.SubRepo,
		f.PriceRepo,
	)
}

// CreateRemoveLineItemOperation creates an operation to remove a line item
func (f *SubscriptionOperationFactory) CreateRemoveLineItemOperation(
	lineItemID string,
	prorationOpts *proration.ProrationParams,
) SubscriptionChangeOperation {
	return NewRemoveLineItemOperation(
		lineItemID,
		prorationOpts,
		f.ProrationService,
		f.SubRepo,
	)
}

// CreateUpdateLineItemOperation creates an operation to update a line item's quantity
func (f *SubscriptionOperationFactory) CreateUpdateLineItemOperation(
	lineItemID string,
	newPriceID *string,
	newQuantity *decimal.Decimal,
	prorationOpts *proration.ProrationParams,
) SubscriptionChangeOperation {
	return NewUpdateLineItemOperation(
		lineItemID,
		newPriceID,
		newQuantity,
		prorationOpts,
		f.ProrationService,
		f.SubRepo,
		f.PriceRepo,
	)
}

// CreateCancellationOperation creates an operation to cancel a subscription
func (f *SubscriptionOperationFactory) CreateCancellationOperation(
	cancelAtPeriodEnd bool,
	prorationOpts *proration.ProrationParams,
) SubscriptionChangeOperation {
	return NewCancellationOperation(
		cancelAtPeriodEnd,
		prorationOpts,
		f.ProrationService,
		f.SubRepo,
	)
}
