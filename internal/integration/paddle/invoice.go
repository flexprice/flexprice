package paddle

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/PaddleHQ/paddle-go-sdk/v4"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/golang-jwt/jwt/v4"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

const (
	defaultProductName = "Flexprice Invoice Item"
	defaultTaxCategory = paddle.TaxCategoryStandard
	defaultPaymentDays = 30
	intervalDay        = paddle.IntervalDay
)

// InvoiceSyncService handles synchronization of FlexPrice invoices with Paddle
type InvoiceSyncService struct {
	client                       PaddleClient
	customerSvc                  PaddleCustomerService
	invoiceRepo                  invoice.Repository
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	logger                       *logger.Logger
	authSecret                   string
}

// NewInvoiceSyncService creates a new Paddle invoice sync service
func NewInvoiceSyncService(
	client PaddleClient,
	customerSvc PaddleCustomerService,
	invoiceRepo invoice.Repository,
	entityIntegrationMappingRepo entityintegrationmapping.Repository,
	logger *logger.Logger,
	authSecret string,
) *InvoiceSyncService {
	return &InvoiceSyncService{
		client:                       client,
		customerSvc:                  customerSvc,
		invoiceRepo:                  invoiceRepo,
		entityIntegrationMappingRepo: entityIntegrationMappingRepo,
		logger:                       logger,
		authSecret:                   authSecret,
	}
}

// SyncInvoiceToPaddle syncs a FlexPrice invoice to Paddle by creating a transaction
func (s *InvoiceSyncService) SyncInvoiceToPaddle(
	ctx context.Context,
	req PaddleInvoiceSyncRequest,
	customerService interfaces.CustomerService,
) (*PaddleInvoiceSyncResponse, error) {
	s.logger.Infow("starting Paddle invoice sync", "invoice_id", req.InvoiceID)

	if !s.client.HasPaddleConnection(ctx) {
		s.logger.Warnw("invoice and customer not synced to Paddle: Paddle connection not available in client",
			"invoice_id", req.InvoiceID,
			"reason", "no_paddle_connection")
		return nil, ierr.NewError("Paddle connection not available").
			WithHint("Paddle integration must be configured for invoice sync").
			Mark(ierr.ErrNotFound)
	}

	flexInvoice, err := s.invoiceRepo.Get(ctx, req.InvoiceID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get FlexPrice invoice").
			Mark(ierr.ErrDatabase)
	}

	existingMapping, err := s.getExistingPaddleMapping(ctx, req.InvoiceID)
	if err != nil && !ierr.IsNotFound(err) {
		return nil, err
	}
	if existingMapping != nil {
		s.logger.Infow("invoice already synced to Paddle",
			"invoice_id", req.InvoiceID,
			"paddle_transaction_id", existingMapping.ProviderEntityID)
		resp := s.buildResponseFromMapping(existingMapping)
		s.appendCheckoutToken(ctx, resp)
		if err := s.updateFlexPriceInvoiceFromPaddle(ctx, flexInvoice, resp); err != nil {
			s.logger.Warnw("failed to update FlexPrice invoice metadata with Paddle URLs",
				"error", err,
				"invoice_id", req.InvoiceID)
		}
		return resp, nil
	}

	// Secondary idempotency guard: if the mapping save previously failed but the invoice
	// metadata update succeeded, we can recover the transaction ID from invoice metadata and
	// avoid creating a duplicate Paddle transaction on retry.
	if paddleTxnID := flexInvoice.Metadata["paddle_transaction_id"]; paddleTxnID != "" {
		s.logger.Infow("invoice already has Paddle transaction ID in metadata (mapping may have been lost), skipping transaction creation",
			"invoice_id", req.InvoiceID,
			"paddle_transaction_id", paddleTxnID)
		resp := &PaddleInvoiceSyncResponse{
			PaddleTransactionID: paddleTxnID,
			CheckoutURL:         flexInvoice.Metadata["paddle_checkout_url"],
		}
		return resp, nil
	}

	flexpriceCustomer, err := s.customerSvc.EnsureCustomerSyncedToPaddle(ctx, flexInvoice.CustomerID, customerService)
	if err != nil {
		s.logger.Errorw("invoice and customer not synced to Paddle: customer sync failed",
			"invoice_id", req.InvoiceID,
			"customer_id", flexInvoice.CustomerID,
			"error", err,
			"reason", "customer_sync_failed")
		return nil, ierr.WithError(err).
			WithHint("Failed to sync customer to Paddle").
			Mark(ierr.ErrInternal)
	}

	paddleCustomerID := flexpriceCustomer.Metadata["paddle_customer_id"]
	if paddleCustomerID == "" {
		s.logger.Errorw("invoice and customer not synced to Paddle: Paddle customer ID not found after sync",
			"invoice_id", req.InvoiceID,
			"customer_id", flexInvoice.CustomerID,
			"reason", "paddle_customer_id_missing")
		return nil, ierr.NewError("Paddle customer ID not found").
			WithHint("Customer must be synced to Paddle before invoice sync").
			WithReportableDetails(map[string]interface{}{
				"customer_id": flexInvoice.CustomerID,
			}).
			Mark(ierr.ErrValidation)
	}

	paddleAddressID := s.getPaddleAddressID(ctx, flexInvoice.CustomerID)
	if paddleAddressID == "" {
		s.logger.Errorw("invoice and customer not synced to Paddle: customer address not synced to Paddle",
			"invoice_id", req.InvoiceID,
			"customer_id", flexInvoice.CustomerID,
			"reason", "paddle_address_id_missing")
		return nil, ierr.NewError("Paddle address ID not found").
			WithHint("Customer must have an address synced to Paddle for invoice creation. Add address to the customer.").
			WithReportableDetails(map[string]interface{}{
				"customer_id": flexInvoice.CustomerID,
			}).
			Mark(ierr.ErrValidation)
	}

	s.logger.Infow("customer and address synced to Paddle",
		"customer_id", flexInvoice.CustomerID,
		"paddle_customer_id", paddleCustomerID,
		"paddle_address_id", paddleAddressID)

	// Preview the transaction to get Paddle-accurate tax BEFORE creating the real one.
	// This ensures the FlexPrice invoice reflects the exact per-line and aggregate tax that
	// Paddle will charge, preventing the "overpaid" situation after payment.
	if err := s.previewAndSyncTax(ctx, flexInvoice, paddleCustomerID, paddleAddressID); err != nil {
		// Non-fatal: log and proceed. The transaction is still created and the invoice
		// totals will read from the created transaction as a fallback below.
		s.logger.Warnw("Paddle tax preview failed, proceeding without pre-sync",
			"error", err,
			"invoice_id", req.InvoiceID)
	}

	createReq, err := s.buildCreateTransactionRequest(ctx, flexInvoice, paddleCustomerID, paddleAddressID)
	if err != nil {
		return nil, err
	}

	txn, err := s.client.CreateTransaction(ctx, createReq)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to create transaction in Paddle").
			Mark(ierr.ErrInternal)
	}

	s.logger.Infow("successfully created transaction in Paddle",
		"invoice_id", req.InvoiceID,
		"paddle_transaction_id", txn.ID)

	syncResp := s.buildSyncResponse(txn)
	s.appendCheckoutToken(ctx, syncResp)

	// Write invoice metadata FIRST so that if the mapping save below fails and Temporal retries,
	// the secondary idempotency guard above catches paddle_transaction_id and skips re-creation.
	if err := s.updateFlexPriceInvoiceFromPaddle(ctx, flexInvoice, syncResp); err != nil {
		s.logger.Warnw("failed to update FlexPrice invoice metadata with Paddle URLs",
			"error", err,
			"invoice_id", req.InvoiceID)
	}

	if err := s.createInvoiceMapping(ctx, req.InvoiceID, txn, flexInvoice.EnvironmentID, syncResp); err != nil {
		s.logger.Errorw("failed to create invoice mapping",
			"error", err,
			"invoice_id", req.InvoiceID,
			"paddle_transaction_id", txn.ID)
		return nil, ierr.WithError(err).
			WithHint("Invoice was synced to Paddle but the local mapping could not be saved. Retry will recover from invoice metadata.").
			Mark(ierr.ErrDatabase)
	}

	return syncResp, nil
}

func (s *InvoiceSyncService) buildCreateTransactionRequest(
	ctx context.Context,
	flexInvoice *invoice.Invoice,
	paddleCustomerID, paddleAddressID string,
) (*paddle.CreateTransactionRequest, error) {
	items, err := s.buildTransactionItems(flexInvoice)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ierr.NewError("invoice has no line items").
			WithHint("Cannot create Paddle transaction without line items").
			Mark(ierr.ErrValidation)
	}

	currency := paddle.CurrencyCode(strings.ToUpper(flexInvoice.Currency))
	if currency == "" {
		currency = paddle.CurrencyCodeUSD
	}

	paymentDays := defaultPaymentDays
	if flexInvoice.DueDate != nil {
		days := int(flexInvoice.DueDate.Sub(flexInvoice.CreatedAt).Hours() / 24)
		if days > 0 {
			paymentDays = days
		}
	}

	req := &paddle.CreateTransactionRequest{
		Items:          items,
		CustomerID:     paddle.PtrTo(paddleCustomerID),
		AddressID:      paddle.PtrTo(paddleAddressID),
		CurrencyCode:   paddle.PtrTo(currency),
		CollectionMode: paddle.PtrTo(paddle.CollectionModeManual),
		Status:         paddle.PtrTo(paddle.TransactionStatusBilled),
		CustomData: map[string]interface{}{
			"flexprice_invoice_id":  flexInvoice.ID,
			"flexprice_customer_id": flexInvoice.CustomerID,
			"environment_id":        types.GetEnvironmentID(ctx),
		},
		BillingDetails: &paddle.BillingDetails{
			EnableCheckout: true,
			PaymentTerms: paddle.Duration{
				Interval:  intervalDay,
				Frequency: paymentDays,
			},
		},
	}

	if flexInvoice.InvoiceNumber != nil && *flexInvoice.InvoiceNumber != "" {
		req.CustomData["invoice_number"] = *flexInvoice.InvoiceNumber
	}

	return req, nil
}

func (s *InvoiceSyncService) buildTransactionItems(flexInvoice *invoice.Invoice) ([]paddle.CreateTransactionItems, error) {
	var items []paddle.CreateTransactionItems

	for _, item := range flexInvoice.LineItems {
		if item.Amount.IsZero() {
			s.logger.Debugw("skipping zero-amount line item",
				"invoice_id", flexInvoice.ID,
				"line_item_id", item.ID)
			continue
		}

		txnItem, err := s.buildSingleTransactionItem(flexInvoice, item)
		if err != nil {
			return nil, err
		}
		items = append(items, *txnItem)
	}

	return items, nil
}

// buildSingleTransactionItem converts a single FlexPrice line item into the Paddle transaction item format.
func (s *InvoiceSyncService) buildSingleTransactionItem(flexInvoice *invoice.Invoice, item *invoice.InvoiceLineItem) (*paddle.CreateTransactionItems, error) {
	quantity := 1
	if !item.Quantity.IsZero() {
		q := item.Quantity.IntPart()
		if q > 0 {
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

	description := s.getLineItemDescription(item)
	productName := s.getLineItemName(item)

	// Paddle requires price.quantity (min/max) for custom prices - defaults to 1-100
	priceQuantity := paddle.PriceQuantity{Minimum: 1, Maximum: 100}
	if quantity > 100 {
		priceQuantity.Maximum = quantity
	}

	txnItem := paddle.NewCreateTransactionItemsTransactionItemCreateWithProduct(&paddle.TransactionItemCreateWithProduct{
		Quantity: quantity,
		Price: paddle.TransactionPriceCreateWithProduct{
			Description: description,
			UnitPrice: paddle.Money{
				Amount:       fmt.Sprintf("%d", amountInCents),
				CurrencyCode: paddle.CurrencyCode(currency),
			},
			Quantity: priceQuantity,
			Product: paddle.TransactionSubscriptionProductCreate{
				Name:        productName,
				TaxCategory: defaultTaxCategory,
			},
		},
	})
	return txnItem, nil
}

// buildPreviewItems builds the preview items for the Paddle PreviewTransaction call,
// preserving the same order as non-zero FlexPrice line items so that
// preview.Details.LineItems[i] maps back to that same line item by index.
func (s *InvoiceSyncService) buildPreviewItems(flexInvoice *invoice.Invoice) ([]paddle.TransactionPreviewByCustomerItems, []*invoice.InvoiceLineItem) {
	var previewItems []paddle.TransactionPreviewByCustomerItems
	var includedLineItems []*invoice.InvoiceLineItem

	for _, item := range flexInvoice.LineItems {
		if item.Amount.IsZero() {
			continue // same filter as buildTransactionItems
		}

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

		previewItem := paddle.NewTransactionPreviewByCustomerItemsTransactionPreviewItemCreateWithProduct(
			&paddle.TransactionPreviewItemCreateWithProduct{
				Quantity:        quantity,
				IncludeInTotals: true,
				Price: paddle.TransactionPriceCreateWithProduct{
					Description: s.getLineItemDescription(item),
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
		previewItems = append(previewItems, *previewItem)
		includedLineItems = append(includedLineItems, item)
	}

	return previewItems, includedLineItems
}

// parsePaddleCents converts a Paddle amount string (cents) to a decimal in the major currency unit.
// Paddle returns all monetary values as strings in the lowest denomination (e.g. "160" = $1.60).
func parsePaddleCents(s string) decimal.Decimal {
	if s == "" {
		return decimal.Zero
	}
	v, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero
	}
	return v.Div(decimal.NewFromInt(100))
}

// previewAndSyncTax calls the Paddle transactions/preview endpoint to get the exact
// tax breakdown before creating the real transaction, then updates the FlexPrice invoice
// so that its totals match what Paddle will actually charge the customer.
//
// Per-line taxes are stored in each InvoiceLineItem.Metadata["paddle_tax_amount"].
// Aggregate tax and grand total are stored in the invoice-level fields and in Metadata.
//
// IMPORTANT: We always use Paddle's canonical aggregates (Details.Totals.Tax / GrandTotal)
// for the invoice header — never re-sum per-line taxes ourselves. This avoids any floating-point
// or rounding discrepancy between the two sides.
func (s *InvoiceSyncService) previewAndSyncTax(
	ctx context.Context,
	flexInvoice *invoice.Invoice,
	paddleCustomerID, paddleAddressID string,
) error {
	previewItems, includedLineItems := s.buildPreviewItems(flexInvoice)
	if len(previewItems) == 0 {
		s.logger.Debugw("no preview items to sync tax for", "invoice_id", flexInvoice.ID)
		return nil
	}

	currency := paddle.CurrencyCode(strings.ToUpper(flexInvoice.Currency))
	if currency == "" {
		currency = paddle.CurrencyCodeUSD
	}

	previewReq := paddle.NewPreviewTransactionCreateRequestTransactionPreviewByCustomer(
		&paddle.TransactionPreviewByCustomer{
			CustomerID:   paddle.PtrTo(paddleCustomerID),
			AddressID:    paddleAddressID,
			CurrencyCode: paddle.PtrTo(currency),
			Items:        previewItems,
		},
	)

	preview, err := s.client.PreviewTransaction(ctx, previewReq)
	if err != nil {
		return err
	}

	s.logger.Infow("received Paddle tax preview",
		"invoice_id", flexInvoice.ID,
		"tax_cents", preview.Details.Totals.Tax,
		"grand_total_cents", preview.Details.Totals.GrandTotal,
		"line_items_count", len(preview.Details.LineItems))

	// --- Per-line-item tax ---
	// preview.Details.LineItems is ordered the same as our previewItems input.
	// We map back to FlexPrice line items using the includedLineItems index slice.
	for i, previewLineItem := range preview.Details.LineItems {
		if i >= len(includedLineItems) {
			break
		}
		flexLineItem := includedLineItems[i]
		lineTax := parsePaddleCents(previewLineItem.Totals.Tax)

		if flexLineItem.Metadata == nil {
			flexLineItem.Metadata = make(types.Metadata)
		}
		// Store Paddle-calculated per-line tax for display/audit purposes
		flexLineItem.Metadata["paddle_tax_amount"] = lineTax.String()
		flexLineItem.Metadata["paddle_tax_rate"] = previewLineItem.TaxRate

		s.logger.Debugw("per-line Paddle tax synced",
			"invoice_id", flexInvoice.ID,
			"line_item_id", flexLineItem.ID,
			"line_tax", lineTax,
			"tax_rate", previewLineItem.TaxRate)
	}

	// --- Invoice-level aggregates ---
	// ALWAYS use Paddle's own aggregate totals — never re-sum from line items.
	// This guarantees FlexPrice amount_due == what Paddle charges == no overpaid.
	aggTax := parsePaddleCents(preview.Details.Totals.Tax)
	grandTotal := parsePaddleCents(preview.Details.Totals.GrandTotal)

	if grandTotal.IsPositive() {
		flexInvoice.TotalTax = aggTax
		flexInvoice.Total = grandTotal
		flexInvoice.AmountDue = grandTotal
		flexInvoice.AmountRemaining = grandTotal.Sub(flexInvoice.AmountPaid)
		if flexInvoice.AmountRemaining.IsNegative() {
			flexInvoice.AmountRemaining = decimal.Zero
		}

		if flexInvoice.Metadata == nil {
			flexInvoice.Metadata = make(types.Metadata)
		}
		flexInvoice.Metadata["paddle_tax_amount"] = aggTax.String()
		flexInvoice.Metadata["paddle_grand_total"] = grandTotal.String()
		flexInvoice.Metadata["paddle_subtotal"] = parsePaddleCents(preview.Details.Totals.Subtotal).String()

		if err := s.invoiceRepo.Update(ctx, flexInvoice); err != nil {
			s.logger.Errorw("failed to persist tax-synced invoice totals",
				"error", err,
				"invoice_id", flexInvoice.ID)
			return err
		}

		s.logger.Infow("successfully synced Paddle tax to FlexPrice invoice",
			"invoice_id", flexInvoice.ID,
			"total_tax", aggTax,
			"grand_total", grandTotal,
			"amount_due", flexInvoice.AmountDue)
	}

	return nil
}

func (s *InvoiceSyncService) getLineItemName(item *invoice.InvoiceLineItem) string {
	if item.DisplayName != nil && *item.DisplayName != "" {
		return *item.DisplayName
	}
	if item.PlanDisplayName != nil && *item.PlanDisplayName != "" {
		return *item.PlanDisplayName
	}
	return defaultProductName
}

func (s *InvoiceSyncService) getLineItemDescription(item *invoice.InvoiceLineItem) string {
	if item.DisplayName != nil && *item.DisplayName != "" {
		return *item.DisplayName
	}
	if item.EntityType != nil {
		switch *item.EntityType {
		case string(types.InvoiceLineItemEntityTypePlan):
			return "Subscription Plan"
		case string(types.InvoiceLineItemEntityTypeAddon):
			return "Add-on"
		}
	}
	return "Service"
}

func (s *InvoiceSyncService) getPaddleAddressID(ctx context.Context, customerID string) string {
	filter := &types.EntityIntegrationMappingFilter{
		EntityID:      customerID,
		EntityType:    types.IntegrationEntityTypeCustomer,
		ProviderTypes: []string{string(types.SecretProviderPaddle)},
	}
	mappings, err := s.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil || len(mappings) == 0 {
		return ""
	}
	if id, ok := mappings[0].Metadata["paddle_address_id"].(string); ok {
		return id
	}
	return ""
}

func (s *InvoiceSyncService) getExistingPaddleMapping(ctx context.Context, flexInvoiceID string) (*entityintegrationmapping.EntityIntegrationMapping, error) {
	filter := &types.EntityIntegrationMappingFilter{
		EntityType:    types.IntegrationEntityTypeInvoice,
		EntityID:      flexInvoiceID,
		ProviderTypes: []string{string(types.SecretProviderPaddle)},
	}
	mappings, err := s.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to check existing invoice mapping").
			Mark(ierr.ErrDatabase)
	}
	if len(mappings) == 0 {
		return nil, ierr.NewError("invoice not synced to Paddle").Mark(ierr.ErrNotFound)
	}
	return mappings[0], nil
}

func (s *InvoiceSyncService) createInvoiceMapping(
	ctx context.Context,
	flexInvoiceID string,
	txn *paddle.Transaction,
	environmentID string,
	syncResp *PaddleInvoiceSyncResponse,
) error {
	metadata := map[string]interface{}{
		"paddle_transaction_id": txn.ID,
		"synced_at":             txn.CreatedAt,
	}
	// Use final payment link (with _success appended) from syncResp, fallback to raw URL
	checkoutURL := syncResp.CheckoutURL
	if checkoutURL == "" && txn.Checkout != nil {
		checkoutURL = lo.FromPtrOr(txn.Checkout.URL, "")
	}
	if checkoutURL != "" {
		metadata["paddle_checkout_url"] = checkoutURL
	}
	if txn.InvoiceNumber != nil {
		metadata["invoice_number"] = *txn.InvoiceNumber
	}

	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityType:       types.IntegrationEntityTypeInvoice,
		EntityID:         flexInvoiceID,
		ProviderType:     string(types.SecretProviderPaddle),
		ProviderEntityID: txn.ID,
		Metadata:         metadata,
		EnvironmentID:    environmentID,
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}

	if err := s.entityIntegrationMappingRepo.Create(ctx, mapping); err != nil {
		return err
	}

	s.logger.Infow("created invoice mapping",
		"invoice_id", flexInvoiceID,
		"paddle_transaction_id", txn.ID)
	return nil
}

func (s *InvoiceSyncService) buildSyncResponse(txn *paddle.Transaction) *PaddleInvoiceSyncResponse {
	resp := &PaddleInvoiceSyncResponse{
		PaddleTransactionID: txn.ID,
		Status:              string(txn.Status),
		Currency:            string(txn.CurrencyCode),
	}
	if txn.InvoiceNumber != nil {
		resp.InvoiceNumber = *txn.InvoiceNumber
	}
	if txn.Checkout != nil {
		resp.CheckoutURL = lo.FromPtrOr(txn.Checkout.URL, "")
	}
	// Capture subtotal (pre-tax) for reference
	if txn.Details.Totals.Subtotal != "" {
		resp.Amount = parsePaddleCents(txn.Details.Totals.Subtotal)
	}
	// Capture tax and grand total from created transaction for mismatch logging only.
	// These do NOT override the invoice — previewAndSyncTax is the source of truth.
	if txn.Details.Totals.Tax != "" {
		resp.TaxAmount = parsePaddleCents(txn.Details.Totals.Tax)
	}
	if txn.Details.Totals.GrandTotal != "" {
		resp.GrandTotal = parsePaddleCents(txn.Details.Totals.GrandTotal)
	}
	return resp
}

// appendCheckoutToken generates a JWT containing client_side_token and success_url, then appends
// it as &token=<JWT> to the CheckoutURL. The frontend decodes the JWT to initialize Paddle.js
// and open the overlay checkout.
func (s *InvoiceSyncService) appendCheckoutToken(ctx context.Context, syncResp *PaddleInvoiceSyncResponse) {
	if syncResp == nil || syncResp.CheckoutURL == "" {
		return
	}

	paddleConfig, err := s.client.GetPaddleConfig(ctx)
	if err != nil || paddleConfig == nil || paddleConfig.ClientSideToken == "" {
		s.logger.Debugw("skipping checkout token: client_side_token not configured")
		return
	}

	conn, err := s.client.GetConnection(ctx)
	if err != nil || conn == nil || conn.Metadata == nil {
		return
	}
	successURL, _ := conn.Metadata["redirect_url"].(string)

	claims := jwt.MapClaims{
		"client_side_token": paddleConfig.ClientSideToken,
		"success_url":       successURL,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signedToken, err := token.SignedString([]byte(s.authSecret))
	if err != nil {
		s.logger.Warnw("failed to sign Paddle checkout token", "error", err)
		return
	}

	parsed, err := url.Parse(syncResp.CheckoutURL)
	if err != nil {
		s.logger.Warnw("failed to parse Paddle checkout URL for token append",
			"error", err,
			"checkout_url", syncResp.CheckoutURL)
		return
	}
	q := parsed.Query()
	q.Set("token", signedToken)
	parsed.RawQuery = q.Encode()
	syncResp.CheckoutURL = parsed.String()
	s.logger.Debugw("appended checkout token to Paddle checkout URL")
}

func (s *InvoiceSyncService) buildResponseFromMapping(mapping *entityintegrationmapping.EntityIntegrationMapping) *PaddleInvoiceSyncResponse {
	resp := &PaddleInvoiceSyncResponse{
		PaddleTransactionID: mapping.ProviderEntityID,
	}
	if url, ok := mapping.Metadata["paddle_checkout_url"].(string); ok {
		resp.CheckoutURL = url
	}
	if num, ok := mapping.Metadata["invoice_number"].(string); ok {
		resp.InvoiceNumber = num
	}
	return resp
}

// updateFlexPriceInvoiceFromPaddle persists the Paddle transaction metadata (ID, checkout URL)
// to the FlexPrice invoice. Tax totals are already set by previewAndSyncTax before this runs
// — we do NOT override them here. We only log a warning if the created transaction's grand total
// differs from what the preview set, for observability.
func (s *InvoiceSyncService) updateFlexPriceInvoiceFromPaddle(ctx context.Context, flexInvoice *invoice.Invoice, syncResp *PaddleInvoiceSyncResponse) error {
	if syncResp == nil || syncResp.PaddleTransactionID == "" {
		return nil
	}
	if flexInvoice.Metadata == nil {
		flexInvoice.Metadata = make(types.Metadata)
	}
	flexInvoice.Metadata["paddle_transaction_id"] = syncResp.PaddleTransactionID
	if syncResp.CheckoutURL != "" {
		flexInvoice.Metadata["paddle_checkout_url"] = syncResp.CheckoutURL
	}

	// Log a warning if the created transaction's grand total differs from what the preview set.
	// This should not happen in practice but helps detect edge cases (e.g. tax rate changed
	// between preview and create, or preview used stale address data).
	if syncResp.GrandTotal.IsPositive() && !flexInvoice.AmountDue.Equal(syncResp.GrandTotal) {
		s.logger.Warnw("Paddle tax mismatch: invoice amount_due differs from created transaction grand_total",
			"invoice_id", flexInvoice.ID,
			"invoice_amount_due", flexInvoice.AmountDue,
			"paddle_grand_total", syncResp.GrandTotal,
			"invoice_total_tax", flexInvoice.TotalTax,
			"paddle_tax", syncResp.TaxAmount)
	}

	return s.invoiceRepo.Update(ctx, flexInvoice)
}

// GetFlexPriceInvoiceID retrieves the FlexPrice invoice ID from a Paddle transaction ID
func (s *InvoiceSyncService) GetFlexPriceInvoiceID(ctx context.Context, paddleTransactionID string) (string, error) {
	if s.entityIntegrationMappingRepo == nil {
		return "", ierr.NewError("entity integration mapping repository not available").
			Mark(ierr.ErrNotFound)
	}

	filter := &types.EntityIntegrationMappingFilter{
		ProviderEntityIDs: []string{paddleTransactionID},
		EntityType:        types.IntegrationEntityTypeInvoice,
		ProviderTypes:     []string{string(types.SecretProviderPaddle)},
		QueryFilter:       types.NewDefaultQueryFilter(),
	}

	mappings, err := s.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		return "", ierr.WithError(err).
			WithHint("Failed to look up invoice mapping").
			Mark(ierr.ErrDatabase)
	}

	if len(mappings) == 0 {
		return "", ierr.NewError("flexprice invoice mapping not found").
			Mark(ierr.ErrNotFound)
	}

	return mappings[0].EntityID, nil
}
