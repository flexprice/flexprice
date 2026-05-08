package paddle

import (
	"context"
	"fmt"
	"strings"

	"github.com/PaddleHQ/paddle-go-sdk/v5"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// getPaddleSubscriptionID reads the paddle_subscription_id from the customer's entity
// integration mapping metadata. Returns "" if not found (not an error — means card not yet saved).
func (s *InvoiceSyncService) getPaddleSubscriptionID(ctx context.Context, customerID string) string {
	filter := &types.EntityIntegrationMappingFilter{
		EntityID:      customerID,
		EntityType:    types.IntegrationEntityTypeCustomer,
		ProviderTypes: []string{string(types.SecretProviderPaddle)},
	}
	mappings, err := s.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil || len(mappings) == 0 {
		return ""
	}
	if id, ok := mappings[0].Metadata["paddle_subscription_id"].(string); ok {
		return id
	}
	return ""
}

// isEligibleSubscription returns true when the subscription can be used as the anchor
// for a CreateSubscriptionCharge call. The subscription must be active or trialing and
// its currency must match the invoice currency.
func isEligibleSubscription(sub *paddle.Subscription, currency string) bool {
	if sub == nil {
		return false
	}
	status := sub.Status
	if status != paddle.SubscriptionStatusActive && status != paddle.SubscriptionStatusTrialing {
		return false
	}
	if currency != "" && strings.ToUpper(string(sub.CurrencyCode)) != strings.ToUpper(currency) {
		return false
	}
	return true
}

// buildSubscriptionCheckoutTransaction creates a Paddle transaction that, when completed by the
// customer, both collects payment and creates a Paddle subscription (so future invoices can use
// CreateSubscriptionCharge). The flow is identical for $0 and non-zero invoices:
//
//   - $0: reuses EnsureTrialCapturePrice — $0 subscription price with RequiresPaymentMethod=true
//   - non-zero: converts each invoice line item to a subscription-priced item
//
// Returns the created transaction (checkout.url carries the payment link).
func (s *InvoiceSyncService) buildSubscriptionCheckoutTransaction(
	ctx context.Context,
	flexInvoice *invoice.Invoice,
	paddleCustomerID, paddleAddressID string,
) (*paddle.Transaction, error) {
	var items []paddle.CreateTransactionItems

	if flexInvoice.Total.IsZero() {
		// $0: use the pre-created trial capture price (subscription, requires payment method)
		priceID, err := s.client.EnsureTrialCapturePrice(ctx)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to ensure trial capture price in Paddle").
				Mark(ierr.ErrInternal)
		}
		items = []paddle.CreateTransactionItems{
			*paddle.NewCreateTransactionItemsTransactionItemFromCatalog(&paddle.TransactionItemFromCatalog{
				PriceID:  priceID,
				Quantity: 1,
			}),
		}
	} else {
		// Non-zero: build subscription-priced items from invoice line items
		var err error
		items, err = s.buildSubscriptionTransactionItems(flexInvoice)
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			return nil, ierr.NewError("invoice has no line items").
				WithHint("Cannot create Paddle subscription transaction without line items").
				Mark(ierr.ErrValidation)
		}
	}

	currency := paddle.CurrencyCode(strings.ToUpper(flexInvoice.Currency))
	if currency == "" {
		currency = paddle.CurrencyCodeUSD
	}

	req := &paddle.CreateTransactionRequest{
		Items:          items,
		CustomerID:     paddle.PtrTo(paddleCustomerID),
		AddressID:      paddle.PtrTo(paddleAddressID),
		CurrencyCode:   paddle.PtrTo(currency),
		CollectionMode: paddle.PtrTo(paddle.CollectionModeAutomatic),
		CustomData: map[string]interface{}{
			"flexprice_invoice_id":  flexInvoice.ID,
			"flexprice_customer_id": flexInvoice.CustomerID,
			"environment_id":        types.GetEnvironmentID(ctx),
		},
	}

	if flexInvoice.InvoiceNumber != nil && *flexInvoice.InvoiceNumber != "" {
		req.CustomData["invoice_number"] = *flexInvoice.InvoiceNumber
	}
	if flexInvoice.SubscriptionCustomerID != nil && *flexInvoice.SubscriptionCustomerID != "" {
		req.CustomData["flexprice_subscription_customer_id"] = *flexInvoice.SubscriptionCustomerID
	}

	txn, err := s.client.CreateTransaction(ctx, req)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHintf("Failed to create subscription checkout transaction in Paddle: %s", err.Error()).
			WithReportableDetails(map[string]interface{}{
				"invoice_id": flexInvoice.ID,
			}).
			Mark(ierr.ErrInternal)
	}

	s.logger.Infow("created subscription checkout transaction in Paddle",
		"invoice_id", flexInvoice.ID,
		"paddle_transaction_id", txn.ID)
	return txn, nil
}

// buildSubscriptionTransactionItems converts non-zero invoice line items to Paddle subscription-priced
// transaction items (billing_cycle = monthly/1). Paddle will create a subscription when the customer
// completes checkout. The webhook then schedules a pause to prevent auto-renewal.
func (s *InvoiceSyncService) buildSubscriptionTransactionItems(flexInvoice *invoice.Invoice) ([]paddle.CreateTransactionItems, error) {
	var items []paddle.CreateTransactionItems

	for _, item := range flexInvoice.LineItems {
		quantity := 1
		if !item.Quantity.IsZero() {
			if q := item.Quantity.IntPart(); q > 0 {
				quantity = int(q)
			}
		}

		unitAmount := item.Amount
		if quantity > 1 {
			unitAmount = item.Amount.Div(decimal.NewFromInt(int64(quantity)))
		}

		amountInCents := unitAmount.Mul(decimal.NewFromInt(100)).IntPart()
		if amountInCents < 0 {
			amountInCents = 0
		}

		currency := strings.ToUpper(item.Currency)
		if currency == "" {
			currency = strings.ToUpper(flexInvoice.Currency)
		}
		if currency == "" {
			currency = "USD"
		}

		priceQuantity := paddle.PriceQuantity{Minimum: 1, Maximum: 100}
		if quantity > 100 {
			priceQuantity.Maximum = quantity
		}

		// Subscription price (billing_cycle = monthly/1): Paddle will create a subscription
		// when this checkout is completed. The webhook schedules a pause to prevent auto-renewal.
		txnItem := paddle.NewCreateTransactionItemsTransactionItemCreateWithProduct(&paddle.TransactionItemCreateWithProduct{
			Quantity: quantity,
			Price: paddle.TransactionPriceCreateWithProduct{
				Description: s.getLineItemDescription(item),
				UnitPrice: paddle.Money{
					Amount:       fmt.Sprintf("%d", amountInCents),
					CurrencyCode: paddle.CurrencyCode(currency),
				},
				Quantity: priceQuantity,
				BillingCycle: &paddle.Duration{
					Interval:  paddle.IntervalMonth,
					Frequency: 1,
				},
				Product: paddle.TransactionSubscriptionProductCreate{
					Name:        s.getLineItemName(item),
					TaxCategory: defaultTaxCategory,
				},
			},
		})
		items = append(items, *txnItem)
	}

	return items, nil
}

// chargeSubscriptionForInvoice fires a subscription charge against the customer's saved payment
// method and writes a mapping (ProviderEntityID = subscription_id) so the
// transaction.completed webhook can look up the invoice by sub_id and mark it paid.
//
// Flow:
//  1. Build non-recurring charge items from invoice line items
//  2. Call CreateSubscriptionCharge
//  3. Persist invoice metadata and an entity mapping keyed on sub_id
//  4. Return status "charge_initiated" — webhook handles final payment processing
func (s *InvoiceSyncService) chargeSubscriptionForInvoice(
	ctx context.Context,
	flexInvoice *invoice.Invoice,
	subID string,
) (*PaddleInvoiceSyncResponse, error) {
	items, err := s.buildChargeItems(flexInvoice)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ierr.NewError("invoice has no chargeable line items").
			WithHint("Cannot create subscription charge without line items").
			Mark(ierr.ErrValidation)
	}

	onFailure := paddle.SubscriptionOnPaymentFailurePreventChange
	if _, err = s.client.CreateSubscriptionCharge(ctx, &paddle.CreateSubscriptionChargeRequest{
		SubscriptionID:   subID,
		EffectiveFrom:    paddle.EffectiveFromImmediately,
		Items:            items,
		OnPaymentFailure: &onFailure,
	}); err != nil {
		return nil, ierr.WithError(err).
			WithHintf("Paddle subscription charge failed for sub %s: %s", subID, err.Error()).
			WithReportableDetails(map[string]interface{}{
				"subscription_id": subID,
				"invoice_id":      flexInvoice.ID,
			}).
			Mark(ierr.ErrInternal)
	}

	// Persist invoice metadata — secondary idempotency guard on Temporal retry.
	if flexInvoice.Metadata == nil {
		flexInvoice.Metadata = make(types.Metadata)
	}
	flexInvoice.Metadata["paddle_subscription_id"] = subID
	flexInvoice.Metadata["paddle_charge_mode"] = "auto_collect"
	if err := s.invoiceRepo.Update(ctx, flexInvoice); err != nil {
		s.logger.Warnw("failed to persist auto-charge metadata on invoice — proceeding",
			"error", err, "invoice_id", flexInvoice.ID)
	}

	// Write a plain mapping keyed on subscription_id. The webhook looks up by sub_id
	// when no txn_id mapping exists, finds this entry, and marks the invoice paid.
	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityType:       types.IntegrationEntityTypeInvoice,
		EntityID:         flexInvoice.ID,
		ProviderType:     string(types.SecretProviderPaddle),
		ProviderEntityID: subID,
		Metadata: map[string]interface{}{
			"paddle_subscription_id": subID,
			"paddle_charge_mode":     "auto_collect",
		},
		EnvironmentID: flexInvoice.EnvironmentID,
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	if err := s.entityIntegrationMappingRepo.Create(ctx, mapping); err != nil {
		s.logger.Errorw("failed to create subscription charge mapping",
			"error", err, "invoice_id", flexInvoice.ID, "subscription_id", subID)
		return nil, ierr.WithError(err).
			WithHint("Charge was sent to Paddle but the local mapping could not be saved. Retry will recover.").
			Mark(ierr.ErrDatabase)
	}

	s.logger.Infow("subscription charge initiated — waiting for transaction.completed webhook",
		"invoice_id", flexInvoice.ID, "subscription_id", subID)

	return &PaddleInvoiceSyncResponse{Status: "charge_initiated"}, nil
}

// buildChargeItems converts invoice line items to non-recurring SubscriptionChargeItemCreateWithProduct
// items. Non-recurring prices (no billing_cycle) are required by CreateSubscriptionCharge.
func (s *InvoiceSyncService) buildChargeItems(flexInvoice *invoice.Invoice) ([]paddle.CreateSubscriptionChargeItems, error) {
	var items []paddle.CreateSubscriptionChargeItems

	for _, item := range flexInvoice.LineItems {
		quantity := 1
		if !item.Quantity.IsZero() {
			if q := item.Quantity.IntPart(); q > 0 {
				quantity = int(q)
			}
		}

		unitAmount := item.Amount
		if quantity > 1 {
			unitAmount = item.Amount.Div(decimal.NewFromInt(int64(quantity)))
		}

		amountInCents := unitAmount.Mul(decimal.NewFromInt(100)).IntPart()
		if amountInCents < 0 {
			amountInCents = 0
		}

		currency := strings.ToUpper(item.Currency)
		if currency == "" {
			currency = strings.ToUpper(flexInvoice.Currency)
		}
		if currency == "" {
			currency = "USD"
		}

		priceQuantity := paddle.PriceQuantity{Minimum: 1, Maximum: 100}
		if quantity > 100 {
			priceQuantity.Maximum = quantity
		}

		chargeItem := paddle.NewCreateSubscriptionChargeItemsSubscriptionChargeItemCreateWithProduct(
			&paddle.SubscriptionChargeItemCreateWithProduct{
				Quantity: quantity,
				Price: paddle.SubscriptionChargeCreateWithProduct{
					Description: s.getLineItemDescription(item),
					Name:        paddle.PtrTo(s.getLineItemName(item)),
					UnitPrice: paddle.Money{
						Amount:       fmt.Sprintf("%d", amountInCents),
						CurrencyCode: paddle.CurrencyCode(currency),
					},
					Quantity: priceQuantity,
					Product: paddle.TransactionSubscriptionProductCreate{
						Name:        s.getLineItemName(item),
						TaxCategory: defaultTaxCategory,
					},
				},
			},
		)
		items = append(items, *chargeItem)
	}

	return items, nil
}

// persistSyncResult saves the Paddle transaction data to the FlexPrice invoice and creates the
// entity integration mapping. It is the common tail for both the charge path and (if needed) the
// checkout path when a transaction is already complete.
//
// mode is a short label stored in mapping metadata (e.g. "auto_collect" or "subscription_checkout").
func (s *InvoiceSyncService) persistSyncResult(
	ctx context.Context,
	flexInvoice *invoice.Invoice,
	txn *paddle.Transaction,
	mode string,
) (*PaddleInvoiceSyncResponse, error) {
	// Update invoice totals from Paddle's authoritative transaction response.
	if txn.Details.Totals.GrandTotal != "" {
		grandTotal := parsePaddleCents(txn.Details.Totals.GrandTotal)
		if grandTotal.IsPositive() {
			flexInvoice.Total = grandTotal
			flexInvoice.AmountDue = grandTotal
			flexInvoice.AmountRemaining = grandTotal.Sub(flexInvoice.AmountPaid)
			if flexInvoice.AmountRemaining.IsNegative() {
				flexInvoice.AmountRemaining = decimal.Zero
			}
		}
		if txn.Details.Totals.Tax != "" {
			flexInvoice.TotalTax = parsePaddleCents(txn.Details.Totals.Tax)
		}
		if flexInvoice.Metadata == nil {
			flexInvoice.Metadata = make(types.Metadata)
		}
		flexInvoice.Metadata["paddle_grand_total"] = txn.Details.Totals.GrandTotal
		flexInvoice.Metadata["paddle_tax_amount"] = txn.Details.Totals.Tax
		flexInvoice.Metadata["paddle_charge_mode"] = mode
	}

	syncResp := s.buildSyncResponse(txn)

	// Write invoice metadata first — secondary idempotency guard for Temporal retries.
	if err := s.updateFlexPriceInvoiceFromPaddle(ctx, flexInvoice, syncResp); err != nil {
		s.logger.Warnw("failed to update FlexPrice invoice metadata with Paddle data",
			"error", err,
			"invoice_id", flexInvoice.ID)
	}

	if err := s.createInvoiceMapping(ctx, flexInvoice.ID, txn, flexInvoice.EnvironmentID, syncResp); err != nil {
		s.logger.Errorw("failed to create invoice mapping",
			"error", err,
			"invoice_id", flexInvoice.ID,
			"paddle_transaction_id", txn.ID)
		return nil, ierr.WithError(err).
			WithHint("Invoice was synced to Paddle but the local mapping could not be saved. Retry will recover from invoice metadata.").
			Mark(ierr.ErrDatabase)
	}

	return syncResp, nil
}
