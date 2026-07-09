package razorpay

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/payment"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// razorpayMinExpireByBuffer is the minimum expire_by window applied when an
// invoice's due date is in the past or too close to now (e.g. wallet top-ups
// which are created with DueDate = time.Now()). 15 minutes is Razorpay's
// hard minimum requirement.
const razorpayMinExpireByBuffer = 15 * time.Minute

// autoChargeLockTTL is the Redis distributed lock TTL for auto-charge operations.
// 15 minutes covers the full charge-submission-to-webhook window for UPI Autopay;
// it is intentionally longer than a typical HTTP timeout because all gateway errors
// are treated as ambiguous (we cannot reliably classify network timeouts vs. definitive
// failures from the Razorpay client).
const autoChargeLockTTL = 15 * time.Minute

// SyncInvoiceResult is returned by SyncInvoice to let the service layer apply
// side-effects (e.g. persisting payment-link URLs) that require calling back
// into the service layer (which would create a circular import if done here).
type SyncInvoiceResult struct {
	// AutoCharged is true when the invoice was successfully submitted for
	// auto-charge via UPI Autopay. No further action is needed by the caller.
	AutoCharged bool
	// RazorpayInvoiceID and ShortURL are populated on the send-invoice path.
	// The caller should persist them to invoice metadata.
	RazorpayInvoiceID string
	ShortURL          string
}

// InvoiceSyncService handles synchronization of FlexPrice invoices with Razorpay.
// It owns both the send-invoice path (creates a Razorpay invoice with a payment
// link) and the auto-charge path (server-initiated UPI Autopay recurring charge).
//
// paymentSvc is wired via SetPaymentService after construction because
// PaymentService and InvoiceSyncService have a mutual dependency (InvoiceSyncService
// provides invoice mapping lookups to PaymentService; PaymentService provides
// AutoCharge to InvoiceSyncService).
type InvoiceSyncService struct {
	client                       RazorpayClient
	customerSvc                  *CustomerService
	invoiceRepo                  invoice.Repository
	paymentRepo                  payment.Repository
	entityIntegrationMappingRepo entityintegrationmapping.Repository
	locker                       cache.Locker
	paymentSvc                   *PaymentService // set via SetPaymentService
	logger                       *logger.Logger
}

// SetPaymentService wires the PaymentService dependency after both services are
// constructed. Must be called before any auto-charge operations.
func (s *InvoiceSyncService) SetPaymentService(svc *PaymentService) {
	s.paymentSvc = svc
}

// NewInvoiceSyncService creates a new Razorpay invoice sync service.
// paymentRepo and locker are required for the auto-charge path; pass nil to
// disable auto-charge (the service will always fall back to send-invoice).
func NewInvoiceSyncService(
	client RazorpayClient,
	customerSvc *CustomerService,
	invoiceRepo invoice.Repository,
	paymentRepo payment.Repository,
	entityIntegrationMappingRepo entityintegrationmapping.Repository,
	locker cache.Locker,
	logger *logger.Logger,
) *InvoiceSyncService {
	return &InvoiceSyncService{
		client:                       client,
		customerSvc:                  customerSvc,
		invoiceRepo:                  invoiceRepo,
		paymentRepo:                  paymentRepo,
		entityIntegrationMappingRepo: entityIntegrationMappingRepo,
		locker:                       locker,
		logger:                       logger,
	}
}

// SyncInvoice is the unified entry point for Razorpay invoice syncing.
// It first attempts a server-initiated UPI Autopay charge (auto-charge path);
// if no usable token is available it falls back to creating a Razorpay invoice
// with a hosted payment link (send-invoice path).
//
// The function is fail-open for token probing: any error during customer/token
// resolution causes a silent fallback to the send-invoice path rather than a
// hard failure. Only definitive errors from the charge submission propagate.
//
// The caller is responsible for persisting SyncInvoiceResult.ShortURL and
// SyncInvoiceResult.RazorpayInvoiceID to invoice metadata (they require calling
// back into the service layer which would create a circular import from here).
func (s *InvoiceSyncService) SyncInvoice(
	ctx context.Context,
	inv *invoice.Invoice,
	customerService interfaces.CustomerService,
) (*SyncInvoiceResult, error) {
	charged, err := s.tryAutoCharge(ctx, inv)
	if err != nil {
		return nil, err
	}
	if charged {
		return &SyncInvoiceResult{AutoCharged: true}, nil
	}

	s.logger.Info(ctx, "no usable token found, sending Razorpay invoice",
		"invoice_id", inv.ID, "customer_id", inv.CustomerID)

	syncResponse, err := s.SyncInvoiceToRazorpay(ctx, RazorpayInvoiceSyncRequest{InvoiceID: inv.ID}, customerService)
	if err != nil {
		return nil, err
	}

	s.logger.Info(ctx, "successfully synced invoice to Razorpay",
		"invoice_id", inv.ID,
		"razorpay_invoice_id", syncResponse.RazorpayInvoiceID,
		"payment_url", syncResponse.ShortURL)

	return &SyncInvoiceResult{
		AutoCharged:       false,
		RazorpayInvoiceID: syncResponse.RazorpayInvoiceID,
		ShortURL:          syncResponse.ShortURL,
	}, nil
}

// findOrCreateAutoChargePayment returns the existing idempotent payment record for
// this auto-charge attempt, or creates a new one. The second return value (skip) is
// true when the payment is already in a terminal or in-flight state and no further
// action is needed.
func (s *InvoiceSyncService) findOrCreateAutoChargePayment(
	ctx context.Context,
	inv *invoice.Invoice,
) (pymnt *payment.Payment, skip bool, err error) {
	key := fmt.Sprintf("autocharge:%s", inv.ID)

	existing, err := s.paymentRepo.GetByIdempotencyKey(ctx, key)
	if err != nil && !ierr.IsNotFound(err) {
		return nil, false, err
	}

	if existing != nil {
		switch existing.PaymentStatus {
		case types.PaymentStatusInitiated, types.PaymentStatusPending:
			return existing, false, nil
		case types.PaymentStatusProcessing, types.PaymentStatusSucceeded, types.PaymentStatusOverpaid:
			return nil, true, nil
		case types.PaymentStatusFailed:
			s.logger.Info(ctx, "auto-charge payment previously failed, skipping",
				"invoice_id", inv.ID, "payment_id", existing.ID)
			return nil, true, nil
		default:
			// REFUNDED, PARTIALLY_REFUNDED, VOIDED, or any future status — skip to
			// avoid reusing a terminal record as the charge idempotency anchor.
			s.logger.Warn(ctx, "auto-charge payment in unexpected state, skipping",
				"invoice_id", inv.ID, "payment_id", existing.ID,
				"payment_status", existing.PaymentStatus)
			return nil, true, nil
		}
	}

	gatewayType := string(types.PaymentGatewayTypeRazorpay)
	newPayment := &payment.Payment{
		ID:                types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PAYMENT),
		IdempotencyKey:    key,
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     inv.ID,
		PaymentMethodType: types.PaymentMethodTypeUPI,
		PaymentGateway:    &gatewayType,
		Amount:            inv.AmountRemaining,
		Currency:          inv.Currency,
		PaymentStatus:     types.PaymentStatusInitiated,
		TrackAttempts:     true,
		EnvironmentID:     inv.EnvironmentID,
		BaseModel:         types.GetDefaultBaseModel(ctx),
	}

	if createErr := s.paymentRepo.Create(ctx, newPayment); createErr != nil {
		// Log the original error before the idempotency re-fetch so the cause is
		// preserved in observability if the re-fetch itself fails.
		s.logger.Warn(ctx, "auto-charge payment create failed, attempting idempotency re-fetch",
			"invoice_id", inv.ID, "error", createErr)
		retrieved, fetchErr := s.paymentRepo.GetByIdempotencyKey(ctx, key)
		if fetchErr != nil {
			return nil, false, fetchErr
		}
		return retrieved, false, nil
	}

	return newPayment, false, nil
}

// tryAutoCharge resolves the customer's Razorpay tokens; if a usable UPI token
// exists it calls executeAutoCharge and returns (true, nil). If token probing
// fails for any reason it logs and returns (false, nil) so the caller falls
// through to SyncInvoiceToRazorpay. Only hard errors from executeAutoCharge are
// propagated.
func (s *InvoiceSyncService) tryAutoCharge(
	ctx context.Context,
	inv *invoice.Invoice,
) (charged bool, err error) {
	if s.paymentRepo == nil || s.locker == nil {
		s.logger.Debug(ctx, "auto-charge disabled (paymentRepo or locker not configured)",
			"invoice_id", inv.ID)
		return false, nil
	}

	razorpayCustomerID, err := s.customerSvc.GetRazorpayCustomerID(ctx, inv.CustomerID)
	if err != nil {
		s.logger.Warn(ctx, "failed to resolve Razorpay customer ID, falling through to send invoice",
			"invoice_id", inv.ID, "customer_id", inv.CustomerID, "error", err)
		return false, nil
	}

	rawTokens, err := s.client.GetCustomerTokens(ctx, razorpayCustomerID)
	if err != nil {
		s.logger.Warn(ctx, "failed to list customer tokens, falling through to send invoice",
			"invoice_id", inv.ID, "error", err)
		return false, nil
	}

	tokens := lo.FilterMap(rawTokens, func(raw map[string]interface{}, _ int) (*interfaces.ProviderPaymentMethod, bool) {
		pm, normErr := NormalizeRazorpayToken(raw)
		return pm, normErr == nil && pm != nil
	})

	token, ok := SelectUsableToken(tokens, types.PaymentMethodTypeUPI, inv.AmountRemaining)
	if !ok {
		s.logger.Debug(ctx, "no usable UPI token found, falling through to send invoice",
			"invoice_id", inv.ID, "customer_id", inv.CustomerID,
			"tokens_inspected", len(tokens))
		return false, nil
	}

	s.logger.Info(ctx, "usable UPI token found, attempting auto-charge",
		"invoice_id", inv.ID, "customer_id", inv.CustomerID,
		"token_id", token.GatewayMethodID)

	if execErr := s.executeAutoCharge(ctx, inv, razorpayCustomerID, token.GatewayMethodID); execErr != nil {
		return false, execErr
	}
	return true, nil
}

// executeAutoCharge acquires a distributed lock, re-validates invoice/payment state,
// submits the charge to Razorpay via PaymentSvc.AutoCharge, and marks the payment
// as PROCESSING. Called only after tryAutoCharge has confirmed a usable token.
func (s *InvoiceSyncService) executeAutoCharge(
	ctx context.Context,
	inv *invoice.Invoice,
	razorpayCustomerID string,
	tokenID string,
) error {
	if s.paymentSvc == nil {
		s.logger.Warn(ctx, "paymentSvc not set on InvoiceSyncService, skipping auto-charge",
			"invoice_id", inv.ID)
		return nil
	}

	pymnt, skip, err := s.findOrCreateAutoChargePayment(ctx, inv)
	if err != nil {
		return err
	}
	if skip {
		s.logger.Info(ctx, "auto-charge payment already in terminal/processing state, skipping",
			"invoice_id", inv.ID)
		return nil
	}

	lockKey := fmt.Sprintf("razorpay:autocharge:%s:%s:%s", types.GetTenantID(ctx), inv.EnvironmentID, inv.ID)
	lock, err := s.locker.AcquireLock(ctx, lockKey, autoChargeLockTTL)
	if err != nil {
		s.logger.Error(ctx, "failed to acquire auto-charge lock", "invoice_id", inv.ID, "error", err)
		return err
	}
	if !lock.AcquiredSuccessfully() {
		s.logger.Info(ctx, "auto-charge already in progress for this invoice, skipping",
			"invoice_id", inv.ID)
		return nil
	}
	defer func() {
		if releaseErr := lock.Release(ctx); releaseErr != nil {
			s.logger.Error(ctx, "failed to release auto-charge lock", "invoice_id", inv.ID, "error", releaseErr)
		}
	}()

	freshInv, err := s.invoiceRepo.Get(ctx, inv.ID)
	if err != nil {
		return err
	}
	if freshInv.PaymentStatus == types.PaymentStatusSucceeded ||
		freshInv.PaymentStatus == types.PaymentStatusOverpaid {
		s.logger.Info(ctx, "invoice paid between token probe and lock acquisition, skipping auto-charge",
			"invoice_id", inv.ID)
		return nil
	}

	freshPayment, err := s.paymentRepo.Get(ctx, pymnt.ID)
	if err != nil {
		return err
	}
	if freshPayment.PaymentStatus == types.PaymentStatusProcessing ||
		freshPayment.PaymentStatus == types.PaymentStatusSucceeded ||
		freshPayment.PaymentStatus == types.PaymentStatusOverpaid {
		s.logger.Info(ctx, "payment already progressed, skipping auto-charge",
			"invoice_id", inv.ID, "payment_status", freshPayment.PaymentStatus)
		return nil
	}

	result, err := s.paymentSvc.AutoCharge(ctx, AutoChargeRequest{
		InvoiceID:          inv.ID,
		RazorpayCustomerID: razorpayCustomerID,
		TokenID:            tokenID,
		Amount:             freshInv.AmountRemaining,
		Currency:           freshInv.Currency,
		FlexPricePaymentID: pymnt.ID,
	})
	if err != nil {
		s.logger.Error(ctx, "Razorpay auto-charge submission failed",
			"invoice_id", inv.ID, "payment_id", pymnt.ID, "error", err)
		return err
	}

	freshPayment.PaymentStatus = types.PaymentStatusProcessing
	// Reconcile the amount to the freshInv snapshot — the payment record may have
	// been created against a stale AmountRemaining if a concurrent partial payment
	// landed between the outer read and the lock.
	freshPayment.Amount = freshInv.AmountRemaining
	if result.RazorpayPaymentID != "" {
		freshPayment.GatewayPaymentID = &result.RazorpayPaymentID
	}
	if updateErr := s.paymentRepo.Update(ctx, freshPayment); updateErr != nil {
		s.logger.Error(ctx, "failed to mark payment as PROCESSING (charge already submitted)",
			"invoice_id", inv.ID,
			"payment_id", pymnt.ID,
			"razorpay_payment_id", result.RazorpayPaymentID,
			"error", updateErr)
		// Do not return — charge is submitted; webhook reconciles via flexprice_payment_id.
	}

	// Persist audit mappings (non-fatal — charge is already submitted).
	s.createAutoChargeMappings(ctx, inv.ID, pymnt.ID, result)

	s.logger.Info(ctx, "auto-charge submitted successfully",
		"invoice_id", inv.ID,
		"payment_id", pymnt.ID,
		"razorpay_payment_id", result.RazorpayPaymentID,
		"already_submitted", result.AlreadySubmitted)

	return nil
}

// createAutoChargeMappings persists two EntityIntegrationMapping records after a
// successful auto-charge submission:
//
//   - invoice → Razorpay order  (provider_entity_type=order in metadata to distinguish
//     from the invoice→razorpay_invoice mapping created on the send-invoice path)
//   - payment → Razorpay payment (pay_xxx, omitted when AlreadySubmitted and no ID)
//
// Both writes are best-effort: failures are logged but do not abort the charge.
// The primary reconciliation path (notes.flexprice_payment_id on the webhook) is
// independent of these mappings.
func (s *InvoiceSyncService) createAutoChargeMappings(
	ctx context.Context,
	invoiceID string,
	paymentID string,
	result *AutoChargeResult,
) {
	if s.entityIntegrationMappingRepo == nil {
		return
	}

	environmentID := types.GetEnvironmentID(ctx)

	// 1. Invoice → Razorpay order
	if result.RazorpayOrderID != "" {
		orderMapping := &entityintegrationmapping.EntityIntegrationMapping{
			ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
			EntityType:       types.IntegrationEntityTypeInvoice,
			EntityID:         invoiceID,
			ProviderType:     string(types.SecretProviderRazorpay),
			ProviderEntityID: result.RazorpayOrderID,
			Metadata: map[string]interface{}{
				"provider_entity_type": "order",
			},
			EnvironmentID: environmentID,
			BaseModel:     types.GetDefaultBaseModel(ctx),
		}
		if err := s.entityIntegrationMappingRepo.Create(ctx, orderMapping); err != nil {
			s.logger.Warn(ctx, "failed to create invoice→order mapping (non-fatal)",
				"invoice_id", invoiceID,
				"razorpay_order_id", result.RazorpayOrderID,
				"error", err)
		}
	}

	// 2. Payment → Razorpay payment
	if result.RazorpayPaymentID != "" {
		paymentMapping := &entityintegrationmapping.EntityIntegrationMapping{
			ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
			EntityType:       types.IntegrationEntityTypePayment,
			EntityID:         paymentID,
			ProviderType:     string(types.SecretProviderRazorpay),
			ProviderEntityID: result.RazorpayPaymentID,
			EnvironmentID:    environmentID,
			BaseModel:        types.GetDefaultBaseModel(ctx),
		}
		if err := s.entityIntegrationMappingRepo.Create(ctx, paymentMapping); err != nil {
			s.logger.Warn(ctx, "failed to create payment→razorpay_payment mapping (non-fatal)",
				"payment_id", paymentID,
				"razorpay_payment_id", result.RazorpayPaymentID,
				"error", err)
		}
	}
}

// SyncInvoiceToRazorpay syncs a FlexPrice invoice to Razorpay
// This creates an invoice in Razorpay with all line items in a single API call
func (s *InvoiceSyncService) SyncInvoiceToRazorpay(
	ctx context.Context,
	req RazorpayInvoiceSyncRequest,
	customerService interfaces.CustomerService,
) (*RazorpayInvoiceSyncResponse, error) {
	s.logger.Info(ctx, "starting Razorpay invoice sync",
		"invoice_id", req.InvoiceID)

	// Step 1: Check if Razorpay connection exists
	if !s.client.HasRazorpayConnection(ctx) {
		return nil, ierr.NewError("Razorpay connection not available").
			WithHint("Razorpay integration must be configured for invoice sync").
			Mark(ierr.ErrNotFound)
	}

	// Step 2: Get FlexPrice invoice
	flexInvoice, err := s.invoiceRepo.Get(ctx, req.InvoiceID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get FlexPrice invoice").
			Mark(ierr.ErrDatabase)
	}

	// Step 3: Check if invoice is already synced to avoid duplicates
	existingMapping, err := s.GetExistingRazorpayMapping(ctx, req.InvoiceID)
	if err != nil && !ierr.IsNotFound(err) {
		return nil, err
	}

	if existingMapping != nil {
		razorpayInvoiceID := existingMapping.ProviderEntityID
		s.logger.Info(ctx, "invoice already synced to Razorpay",
			"invoice_id", req.InvoiceID,
			"razorpay_invoice_id", razorpayInvoiceID)

		// Fetch existing invoice details and return
		return s.fetchInvoiceResponse(ctx, razorpayInvoiceID)
	}

	// Step 4: Ensure customer is synced to Razorpay
	flexpriceCustomer, err := s.customerSvc.EnsureCustomerSyncedToRazorpay(ctx, flexInvoice.CustomerID, customerService)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to sync customer to Razorpay").
			Mark(ierr.ErrInternal)
	}

	razorpayCustomerID := flexpriceCustomer.Metadata["razorpay_customer_id"]
	s.logger.Info(ctx, "customer synced to Razorpay",
		"customer_id", flexInvoice.CustomerID,
		"razorpay_customer_id", razorpayCustomerID)

	// Step 5: Build invoice data with inline line items
	invoiceData, err := s.buildInvoiceData(ctx, flexInvoice, razorpayCustomerID)
	if err != nil {
		return nil, err
	}

	// Step 6: Create invoice in Razorpay (single API call)
	razorpayInvoice, err := s.client.CreateInvoice(ctx, invoiceData)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to create invoice in Razorpay").
			Mark(ierr.ErrInternal)
	}

	razorpayInvoiceID := razorpayInvoice["id"].(string)
	s.logger.Info(ctx, "successfully created invoice in Razorpay",
		"invoice_id", req.InvoiceID,
		"razorpay_invoice_id", razorpayInvoiceID)

	// Extract short URL for storage in mapping
	razorpayShortURL := ""
	if shortURL, ok := razorpayInvoice["short_url"].(string); ok {
		razorpayShortURL = shortURL
	}

	// Step 7: Create entity integration mapping with short URL
	if err := s.createInvoiceMapping(ctx, req.InvoiceID, razorpayInvoiceID, razorpayShortURL, flexInvoice.EnvironmentID); err != nil {
		s.logger.Error(ctx, "failed to create invoice mapping",
			"error", err,
			"invoice_id", req.InvoiceID,
			"razorpay_invoice_id", razorpayInvoiceID)
		// Don't fail the sync, just log the error
	}

	// Step 8: Build and return response
	return s.buildSyncResponse(razorpayInvoice), nil
}

// buildInvoiceData constructs the Razorpay invoice creation payload
func (s *InvoiceSyncService) buildInvoiceData(
	ctx context.Context,
	flexInvoice *invoice.Invoice,
	razorpayCustomerID string,
) (map[string]interface{}, error) {
	// Build line items array
	lineItems, err := s.buildLineItems(flexInvoice)
	if err != nil {
		return nil, err
	}

	if len(lineItems) == 0 {
		return nil, ierr.NewError("invoice has no line items").
			WithHint("Cannot create Razorpay invoice without line items").
			Mark(ierr.ErrValidation)
	}

	// Build description
	description := s.buildInvoiceDescription(flexInvoice)

	// Build notes with metadata
	notes := map[string]interface{}{
		"flexprice_invoice_id":     flexInvoice.ID,
		"flexprice_customer_id":    flexInvoice.CustomerID,
		"flexprice_environment_id": flexInvoice.EnvironmentID,
		"sync_source":              "flexprice",
	}

	// Add invoice number to notes if available
	if flexInvoice.InvoiceNumber != nil {
		notes["invoice_number"] = *flexInvoice.InvoiceNumber
	}

	// Construct invoice data according to Razorpay API format
	// Use invoice currency (convert to uppercase as Razorpay expects uppercase currency codes)
	invoiceCurrency := strings.ToUpper(flexInvoice.Currency)
	invoiceData := map[string]interface{}{
		"type":         "invoice",
		"customer_id":  razorpayCustomerID,
		"line_items":   lineItems,
		"currency":     invoiceCurrency,
		"description":  description,
		"email_notify": true,  // Enable email notifications
		"sms_notify":   false, // Disable SMS notifications
		"notes":        notes,
	}

	// Add due date if available (Unix timestamp in seconds).
	// Razorpay requires expire_by to be at least 15 minutes in the future.
	// Wallet top-up invoices have DueDate = time.Now(), so we clamp upward.
	if flexInvoice.DueDate != nil {
		minExpireBy := time.Now().UTC().Add(razorpayMinExpireByBuffer)
		expireBy := *flexInvoice.DueDate
		if expireBy.Before(minExpireBy) {
			expireBy = minExpireBy
		}
		invoiceData["expire_by"] = expireBy.Unix()
	}

	s.logger.Info(ctx, "built invoice data for Razorpay",
		"invoice_id", flexInvoice.ID,
		"line_items_count", len(lineItems),
		"currency", flexInvoice.Currency,
		"has_due_date", flexInvoice.DueDate != nil)

	return invoiceData, nil
}

// buildLineItems converts FlexPrice line items to Razorpay format
func (s *InvoiceSyncService) buildLineItems(flexInvoice *invoice.Invoice) (map[string]interface{}, error) {
	lineItems := make(map[string]interface{})
	lineItemIndex := 0

	for _, item := range flexInvoice.LineItems {
		// Skip zero-amount items
		if item.Amount.IsZero() {
			s.logger.Debug(context.Background(), "skipping zero-amount line item",
				"invoice_id", flexInvoice.ID,
				"line_item_index", lineItemIndex)
			continue
		}

		// Get item name with fallback
		itemName := s.getLineItemName(item)

		// Get item description (entity type for clarity)
		itemDescription := s.getLineItemDescription(item)

		// Keep quantity as 1 and use total line item amount
		quantity := 1

		// Convert total line item amount to smallest currency unit (paise/cents)
		// Razorpay expects integer amount in smallest unit
		amountInSmallestUnit := item.Amount.Mul(decimal.NewFromInt(100)).IntPart()

		// Build Razorpay line item
		// Use line item currency (convert to uppercase as Razorpay expects uppercase currency codes)
		// Fallback to invoice currency if line item currency is empty
		lineItemCurrency := strings.ToUpper(item.Currency)
		if lineItemCurrency == "" {
			lineItemCurrency = strings.ToUpper(flexInvoice.Currency)
		}
		razorpayLineItem := RazorpayLineItem{
			Name:        itemName,
			Description: itemDescription,
			Amount:      amountInSmallestUnit,
			Currency:    lineItemCurrency,
			Quantity:    quantity,
		}

		// Add to line items map (Razorpay expects sequential indexed map)
		// Use separate counter to ensure sequential indices even when items are skipped
		lineItems[fmt.Sprintf("%d", lineItemIndex)] = razorpayLineItem
		lineItemIndex++
	}

	return lineItems, nil
}

// getLineItemName extracts the display name for a line item
func (s *InvoiceSyncService) getLineItemName(item *invoice.InvoiceLineItem) string {
	// Priority: DisplayName > PlanDisplayName > Default
	if item.DisplayName != nil && *item.DisplayName != "" {
		return *item.DisplayName
	}

	if item.PlanDisplayName != nil && *item.PlanDisplayName != "" {
		return *item.PlanDisplayName
	}

	return DefaultItemName
}

// getLineItemDescription builds a description based on entity type
func (s *InvoiceSyncService) getLineItemDescription(item *invoice.InvoiceLineItem) string {
	if item.EntityType == nil {
		return "Service"
	}

	switch *item.EntityType {
	case string(types.InvoiceLineItemEntityTypePlan):
		return "Subscription Plan"
	case string(types.InvoiceLineItemEntityTypeAddon):
		return "Add-on"
	default:
		return "Service"
	}
}

// buildInvoiceDescription creates a concise description for the invoice
func (s *InvoiceSyncService) buildInvoiceDescription(flexInvoice *invoice.Invoice) string {
	// Use invoice number if available
	if flexInvoice.InvoiceNumber != nil && *flexInvoice.InvoiceNumber != "" {
		return fmt.Sprintf("Invoice %s", *flexInvoice.InvoiceNumber)
	}

	// Fallback to generic description with item count
	itemCount := len(flexInvoice.LineItems)
	if itemCount == 1 {
		return "Invoice for 1 item"
	}

	return fmt.Sprintf("Invoice for %d items", itemCount)
}

// buildSyncResponse constructs the sync response from Razorpay invoice data
func (s *InvoiceSyncService) buildSyncResponse(razorpayInvoice map[string]interface{}) *RazorpayInvoiceSyncResponse {
	response := &RazorpayInvoiceSyncResponse{
		RazorpayInvoiceID: lo.FromPtrOr(extractString(razorpayInvoice, "id"), ""),
		InvoiceNumber:     lo.FromPtrOr(extractString(razorpayInvoice, "invoice_number"), ""),
		ShortURL:          lo.FromPtrOr(extractString(razorpayInvoice, "short_url"), ""),
		Status:            lo.FromPtrOr(extractString(razorpayInvoice, "status"), ""),
		Currency:          lo.FromPtrOr(extractString(razorpayInvoice, "currency"), ""),
	}

	// Extract amounts (Razorpay returns in smallest unit)
	if amount, ok := razorpayInvoice["amount"].(float64); ok {
		response.Amount = decimal.NewFromFloat(amount).Div(decimal.NewFromInt(100))
	}

	if amountDue, ok := razorpayInvoice["amount_due"].(float64); ok {
		response.AmountDue = decimal.NewFromFloat(amountDue).Div(decimal.NewFromInt(100))
	}

	// Extract timestamp
	if createdAt, ok := razorpayInvoice["created_at"].(float64); ok {
		response.CreatedAt = int64(createdAt)
	}

	return response
}

// fetchInvoiceResponse fetches existing invoice and builds response
func (s *InvoiceSyncService) fetchInvoiceResponse(ctx context.Context, razorpayInvoiceID string) (*RazorpayInvoiceSyncResponse, error) {
	razorpayInvoice, err := s.client.GetInvoice(ctx, razorpayInvoiceID)
	if err != nil {
		return nil, err
	}

	return s.buildSyncResponse(razorpayInvoice), nil
}

// createInvoiceMapping creates entity integration mapping to track the sync
func (s *InvoiceSyncService) createInvoiceMapping(
	ctx context.Context,
	flexInvoiceID string,
	razorpayInvoiceID string,
	razorpayShortURL string,
	environmentID string,
) error {
	metadata := make(map[string]interface{})
	if razorpayShortURL != "" {
		metadata["razorpay_payment_url"] = razorpayShortURL
	}

	mapping := &entityintegrationmapping.EntityIntegrationMapping{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITY_INTEGRATION_MAPPING),
		EntityType:       types.IntegrationEntityTypeInvoice,
		EntityID:         flexInvoiceID,
		ProviderType:     string(types.SecretProviderRazorpay),
		ProviderEntityID: razorpayInvoiceID,
		Metadata:         metadata,
		EnvironmentID:    environmentID,
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}

	if err := s.entityIntegrationMappingRepo.Create(ctx, mapping); err != nil {
		// If duplicate key error, invoice is already tracked (race condition)
		s.logger.Info(context.Background(), "failed to create entity integration mapping (may already exist)",
			"error", err,
			"invoice_id", flexInvoiceID,
			"razorpay_invoice_id", razorpayInvoiceID)
		return err
	}

	s.logger.Info(ctx, "created invoice mapping",
		"invoice_id", flexInvoiceID,
		"razorpay_invoice_id", razorpayInvoiceID)

	return nil
}

// GetExistingRazorpayMapping checks if invoice is already synced to Razorpay
func (s *InvoiceSyncService) GetExistingRazorpayMapping(
	ctx context.Context,
	flexInvoiceID string,
) (*entityintegrationmapping.EntityIntegrationMapping, error) {
	filter := &types.EntityIntegrationMappingFilter{
		EntityType:    types.IntegrationEntityTypeInvoice,
		EntityID:      flexInvoiceID,
		ProviderTypes: []string{string(types.SecretProviderRazorpay)},
	}

	mappings, err := s.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to check existing invoice mapping").
			Mark(ierr.ErrDatabase)
	}

	if len(mappings) == 0 {
		return nil, ierr.NewError("invoice not synced to Razorpay").
			Mark(ierr.ErrNotFound)
	}

	return mappings[0], nil
}

// GetFlexPriceInvoiceID retrieves the FlexPrice invoice ID from a Razorpay invoice ID (reverse lookup)
// This is used when processing external Razorpay payments to find the corresponding FlexPrice invoice
func (s *InvoiceSyncService) GetFlexPriceInvoiceID(ctx context.Context, razorpayInvoiceID string) (string, error) {
	if s.entityIntegrationMappingRepo == nil {
		return "", ierr.NewError("entity integration mapping repository not available").
			Mark(ierr.ErrNotFound)
	}

	s.logger.Debug(ctx, "looking up FlexPrice invoice ID from Razorpay invoice ID",
		"razorpay_invoice_id", razorpayInvoiceID)

	filter := &types.EntityIntegrationMappingFilter{
		ProviderEntityIDs: []string{razorpayInvoiceID},
		EntityType:        types.IntegrationEntityTypeInvoice,
		ProviderTypes:     []string{string(types.SecretProviderRazorpay)},
		QueryFilter:       types.NewDefaultQueryFilter(),
	}

	mappings, err := s.entityIntegrationMappingRepo.List(ctx, filter)
	if err != nil {
		s.logger.Debug(ctx, "failed to query entity integration mapping",
			"error", err,
			"razorpay_invoice_id", razorpayInvoiceID)
		return "", ierr.WithError(err).
			WithHint("Failed to look up invoice mapping").
			Mark(ierr.ErrDatabase)
	}

	if len(mappings) == 0 {
		s.logger.Debug(ctx, "no FlexPrice invoice mapping found for Razorpay invoice",
			"razorpay_invoice_id", razorpayInvoiceID)
		return "", ierr.NewError("flexprice invoice mapping not found").
			Mark(ierr.ErrNotFound)
	}

	flexpriceInvoiceID := mappings[0].EntityID
	s.logger.Info(ctx, "found FlexPrice invoice mapping",
		"razorpay_invoice_id", razorpayInvoiceID,
		"flexprice_invoice_id", flexpriceInvoiceID)

	return flexpriceInvoiceID, nil
}

// extractString safely extracts a string value from map
func extractString(data map[string]interface{}, key string) *string {
	if val, ok := data[key].(string); ok {
		return &val
	}
	return nil
}
