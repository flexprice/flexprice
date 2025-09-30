package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/creditnote"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/idempotency"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

type CreditNoteService interface {
	CreateCreditNote(ctx context.Context, req *dto.CreateCreditNoteRequest) (*dto.CreditNoteResponse, error)
	GetCreditNote(ctx context.Context, id string) (*dto.CreditNoteResponse, error)
	ListCreditNotes(ctx context.Context, filter *types.CreditNoteFilter) (*dto.ListCreditNotesResponse, error)

	// This method is used to void a credit note
	// this can be done when credit note is a adjustment and not a refund so we can cancel the adjustment
	VoidCreditNote(ctx context.Context, id string) error

	// This method is used to finalize a credit note
	// this can be done when credit note is a adjustment and not a refund so we can cancel the adjustment
	FinalizeCreditNote(ctx context.Context, id string) error
}

type creditNoteService struct {
	ServiceParams
}

const (
	// CreditNoteNumberPrefix is the prefix for credit note numbers
	CreditNoteNumberPrefix = "CN"
	// CreditNoteNumberLength is the length of the random part of credit note number
	CreditNoteNumberLength = 8
)

func NewCreditNoteService(params ServiceParams) CreditNoteService {
	return &creditNoteService{
		ServiceParams: params,
	}
}

func (s *creditNoteService) CreateCreditNote(ctx context.Context, req *dto.CreateCreditNoteRequest) (*dto.CreditNoteResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	var creditNote *creditnote.CreditNote

	// Start transaction
	err := s.DB.WithTx(ctx, func(tx context.Context) error {
		s.Logger.Infow("creating credit note",
			"invoice_id", req.InvoiceID,
			"reason", req.Reason,
			"line_items_count", len(req.LineItems))

		// Validate credit note creation rules
		if err := s.ValidateCreditNoteCreation(tx, req); err != nil {
			return err
		}

		// Get invoice with line items
		inv, err := s.InvoiceRepo.Get(tx, req.InvoiceID)
		if err != nil {
			return err
		}

		// Determine credit note type based on invoice payment status
		creditNoteType, err := s.getCreditNoteType(inv)
		if err != nil {
			return err
		}

		// Generate credit note number if not provided
		if req.CreditNoteNumber == "" {
			req.CreditNoteNumber = types.GenerateShortIDWithPrefix(types.SHORT_ID_PREFIX_CREDIT_NOTE)
		}

		// Check if credit note number is unique
		if req.IdempotencyKey == nil {
			generator := idempotency.NewGenerator()
			key := generator.GenerateKey(idempotency.ScopeCreditNote, map[string]any{
				"invoice_id":         req.InvoiceID,
				"credit_note_number": req.CreditNoteNumber,
				"reason":             req.Reason,
				"credit_note_type":   creditNoteType,
			})
			req.IdempotencyKey = lo.ToPtr(key)
		}

		// Check if idempotency key is already used
		if existingCreditNote, err := s.CreditNoteRepo.GetByIdempotencyKey(tx, *req.IdempotencyKey); err == nil {
			// Return existing credit note for idempotent behavior
			s.Logger.Infow("returning existing credit note for idempotency key",
				"idempotency_key", *req.IdempotencyKey,
				"existing_credit_note_id", existingCreditNote.ID)
			creditNote = existingCreditNote
			return nil
		}

		// Convert request to domain model
		cn := req.ToCreditNote(tx, inv)

		// Set correct credit note type and status
		cn.CreditNoteType = creditNoteType
		cn.CreditNoteStatus = types.CreditNoteStatusDraft
		cn.SubscriptionID = inv.SubscriptionID
		cn.CustomerID = inv.CustomerID

		// Create credit note with line items in a single transaction
		if err := s.CreditNoteRepo.CreateWithLineItems(tx, cn); err != nil {
			return err
		}

		s.Logger.Infow(
			"credit note created successfully",
			"credit_note_id", cn.ID,
			"credit_note_number", req.CreditNoteNumber,
			"invoice_id", req.InvoiceID,
			"total_amount", cn.TotalAmount,
		)

		// Convert to response
		creditNote = cn
		return nil
	})

	if err != nil {
		s.Logger.Errorw(
			"failed to create credit note",
			"error", err,
			"invoice_id", req.InvoiceID,
			"reason", req.Reason,
		)
		return nil, err
	}

	// Publish webhook event after successful transaction
	s.publishInternalWebhookEvent(ctx, types.WebhookEventCreditNoteCreated, creditNote.ID)

	// Finalize the credit note if the flag is set
	if req.ProcessCreditNote {
		if err := s.FinalizeCreditNote(ctx, creditNote.ID); err != nil {
			return nil, err
		}
	}

	// Get the updated credit note after processing
	updatedCreditNote, err := s.CreditNoteRepo.Get(ctx, creditNote.ID)
	if err != nil {
		return nil, err
	}

	return &dto.CreditNoteResponse{
		CreditNote: updatedCreditNote,
	}, nil
}

func (s *creditNoteService) GetCreditNote(ctx context.Context, id string) (*dto.CreditNoteResponse, error) {
	cn, err := s.CreditNoteRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	invoiceService := NewInvoiceService(s.ServiceParams)
	subscriptionService := NewSubscriptionService(s.ServiceParams)
	customerService := NewCustomerService(s.ServiceParams)

	invoiceResponse, err := invoiceService.GetInvoice(ctx, cn.InvoiceID)
	if err != nil {
		return nil, err
	}

	var subscription *dto.SubscriptionResponse
	if cn.SubscriptionID != nil && lo.FromPtr(cn.SubscriptionID) != "" {
		sub, err := subscriptionService.GetSubscription(ctx, *cn.SubscriptionID)
		if err != nil {
			return nil, err
		}
		subscription = sub
	}

	var customerResp *dto.CustomerResponse
	if cn.CustomerID != "" {
		customerResp, err = customerService.GetCustomer(ctx, cn.CustomerID)
		if err != nil {
			return nil, err
		}
	}

	var customerData *customer.Customer
	if customerResp != nil {
		customerData = customerResp.Customer
	}

	return &dto.CreditNoteResponse{
		CreditNote:   cn,
		Invoice:      invoiceResponse,
		Subscription: subscription,
		Customer:     customerData,
	}, nil
}

func (s *creditNoteService) ListCreditNotes(ctx context.Context, filter *types.CreditNoteFilter) (*dto.ListCreditNotesResponse, error) {

	if err := filter.GetExpand().Validate(types.CreditNoteExpandConfig); err != nil {
		return nil, err
	}

	creditNotes, err := s.CreditNoteRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	total, err := s.CreditNoteRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Build response
	response := &dto.ListCreditNotesResponse{
		Items: make([]*dto.CreditNoteResponse, len(creditNotes)),
	}

	// Initialize service instances for expansion
	invoiceService := NewInvoiceService(s.ServiceParams)
	subscriptionService := NewSubscriptionService(s.ServiceParams)
	customerService := NewCustomerService(s.ServiceParams)

	// If invoices are requested to be expanded, fetch all invoices in one query
	var invoicesByID map[string]*dto.InvoiceResponse
	if filter.GetExpand().Has(types.ExpandInvoice) && len(creditNotes) > 0 {
		// Extract unique invoice IDs
		invoiceIDs := make([]string, 0)
		invoiceIDMap := make(map[string]bool)
		for _, cn := range creditNotes {
			if cn.InvoiceID != "" && !invoiceIDMap[cn.InvoiceID] {
				invoiceIDs = append(invoiceIDs, cn.InvoiceID)
				invoiceIDMap[cn.InvoiceID] = true
			}
		}

		if len(invoiceIDs) > 0 {
			// Fetch all invoices in one query using filter with IDs
			invoiceFilter := &types.InvoiceFilter{
				InvoiceIDs: invoiceIDs,
			}
			invoicesResponse, err := invoiceService.ListInvoices(ctx, invoiceFilter)
			if err != nil {
				return nil, err
			}

			// Create a map for quick invoice lookup
			invoicesByID = make(map[string]*dto.InvoiceResponse, len(invoicesResponse.Items))
			for _, inv := range invoicesResponse.Items {
				invoicesByID[inv.ID] = inv
			}

			s.Logger.Debugw("fetched invoices for credit notes", "count", len(invoicesResponse.Items))
		}
	}

	// If subscriptions are requested to be expanded, fetch all subscriptions in one query
	var subscriptionsByID map[string]*dto.SubscriptionResponse
	if filter.GetExpand().Has(types.ExpandSubscription) && len(creditNotes) > 0 {
		// Extract unique subscription IDs
		subscriptionIDs := make([]string, 0)
		subscriptionIDMap := make(map[string]bool)
		for _, cn := range creditNotes {
			if cn.SubscriptionID != nil && *cn.SubscriptionID != "" && !subscriptionIDMap[*cn.SubscriptionID] {
				subscriptionIDs = append(subscriptionIDs, *cn.SubscriptionID)
				subscriptionIDMap[*cn.SubscriptionID] = true
			}
		}

		if len(subscriptionIDs) > 0 {
			// Fetch all subscriptions in one query using filter with IDs
			subscriptionFilter := &types.SubscriptionFilter{
				SubscriptionIDs: subscriptionIDs,
			}
			subscriptionsResponse, err := subscriptionService.ListSubscriptions(ctx, subscriptionFilter)
			if err != nil {
				return nil, err
			}

			// Create a map for quick subscription lookup
			subscriptionsByID = make(map[string]*dto.SubscriptionResponse, len(subscriptionsResponse.Items))
			for _, sub := range subscriptionsResponse.Items {
				subscriptionsByID[sub.ID] = sub
			}

			s.Logger.Debugw("fetched subscriptions for credit notes", "count", len(subscriptionsResponse.Items))
		}
	}

	// If customers are requested to be expanded, fetch all customers in one query
	var customersByID map[string]*dto.CustomerResponse
	if filter.GetExpand().Has(types.ExpandCustomer) && len(creditNotes) > 0 {
		// Extract unique customer IDs
		customerIDs := make([]string, 0)
		customerIDMap := make(map[string]bool)
		for _, cn := range creditNotes {
			if cn.CustomerID != "" && !customerIDMap[cn.CustomerID] {
				customerIDs = append(customerIDs, cn.CustomerID)
				customerIDMap[cn.CustomerID] = true
			}
		}

		if len(customerIDs) > 0 {
			// Fetch all customers in one query using filter with IDs
			customerFilter := &types.CustomerFilter{
				CustomerIDs: customerIDs,
			}
			customersResponse, err := customerService.GetCustomers(ctx, customerFilter)
			if err != nil {
				return nil, err
			}

			// Create a map for quick customer lookup
			customersByID = make(map[string]*dto.CustomerResponse, len(customersResponse.Items))
			for _, cust := range customersResponse.Items {
				customersByID[cust.ID] = cust
			}

			s.Logger.Debugw("fetched customers for credit notes", "count", len(customersResponse.Items))
		}
	}

	// Build response with expanded fields
	for i, cn := range creditNotes {
		response.Items[i] = &dto.CreditNoteResponse{
			CreditNote: cn,
		}

		// Add invoice if requested and available
		if filter.GetExpand().Has(types.ExpandInvoice) && cn.InvoiceID != "" {
			if inv, ok := invoicesByID[cn.InvoiceID]; ok {
				response.Items[i].Invoice = inv
			}
		}

		// Add subscription if requested and available
		if filter.GetExpand().Has(types.ExpandSubscription) && cn.SubscriptionID != nil && *cn.SubscriptionID != "" {
			if sub, ok := subscriptionsByID[*cn.SubscriptionID]; ok {
				response.Items[i].Subscription = sub
			}
		}

		// Add customer if requested and available
		if filter.GetExpand().Has(types.ExpandCustomer) && cn.CustomerID != "" {
			if cust, ok := customersByID[cn.CustomerID]; ok {
				response.Items[i].Customer = cust.Customer
			}
		}
	}

	response.Pagination = types.NewPaginationResponse(total, filter.GetLimit(), filter.GetOffset())

	return response, nil
}

func (s *creditNoteService) VoidCreditNote(ctx context.Context, id string) error {
	if id == "" {
		return ierr.NewError("missing credit note ID").
			WithHint("Please provide a valid credit note ID to void.").
			Mark(ierr.ErrValidation)
	}

	cn, err := s.CreditNoteRepo.Get(ctx, id)
	if err != nil {
		return err
	}

	// Only draft and finalized credit notes can be voided
	if cn.CreditNoteStatus != types.CreditNoteStatusDraft && cn.CreditNoteStatus != types.CreditNoteStatusFinalized {
		return ierr.NewError("cannot void this credit note").
			WithHintf("This credit note is %s and cannot be voided. You can only void draft or finalized credit notes.", cn.CreditNoteStatus).
			WithReportableDetails(map[string]any{
				"current_status": cn.CreditNoteStatus,
				"allowed_statuses": []types.CreditNoteStatus{
					types.CreditNoteStatusDraft,
					types.CreditNoteStatusFinalized,
				},
			}).
			Mark(ierr.ErrValidation)
	}

	// Check if this is a refund type credit note that has already been processed
	if cn.CreditNoteType == types.CreditNoteTypeRefund && cn.CreditNoteStatus == types.CreditNoteStatusFinalized {
		return ierr.NewError("cannot void completed refund").
			WithHint("This refund has already been processed and money has been added to the customer's wallet. Refunds cannot be voided once completed.").
			WithReportableDetails(map[string]any{
				"credit_note_status": cn.CreditNoteStatus,
				"credit_note_type":   cn.CreditNoteType,
			}).
			Mark(ierr.ErrValidation)
	}

	// Store original status for logging
	originalStatus := cn.CreditNoteStatus

	// Update credit note status to voided
	cn.CreditNoteStatus = types.CreditNoteStatusVoided

	if err := s.CreditNoteRepo.Update(ctx, cn); err != nil {
		return err
	}

	// Recalculate invoice amounts after credit note void
	// This is needed to update the adjustment and refunded amounts
	if originalStatus == types.CreditNoteStatusFinalized {
		invoiceService := NewInvoiceService(s.ServiceParams)
		if err := invoiceService.RecalculateInvoiceAmounts(ctx, cn.InvoiceID); err != nil {
			s.Logger.Errorw("failed to recalculate invoice amounts after credit note void",
				"error", err,
				"credit_note_id", cn.ID,
				"invoice_id", cn.InvoiceID)
		}
	}

	// Publish webhook event after successful update
	s.publishInternalWebhookEvent(ctx, types.WebhookEventCreditNoteUpdated, cn.ID)

	s.Logger.Infow("credit note voided successfully",
		"credit_note_id", id,
		"previous_status", originalStatus)

	return nil
}

func (s *creditNoteService) FinalizeCreditNote(ctx context.Context, id string) error {
	if id == "" {
		return ierr.NewError("missing credit note ID").
			WithHint("Please provide a valid credit note ID to finalize.").
			Mark(ierr.ErrValidation)
	}

	cn, err := s.CreditNoteRepo.Get(ctx, id)
	if err != nil {
		return err
	}

	if cn.CreditNoteStatus != types.CreditNoteStatusDraft {
		return ierr.NewError("credit note already processed").
			WithHintf("This credit note is %s and cannot be processed again. Only draft credit notes can be finalized.", cn.CreditNoteStatus).
			WithReportableDetails(map[string]any{
				"current_status":  cn.CreditNoteStatus,
				"required_status": types.CreditNoteStatusDraft,
			}).
			Mark(ierr.ErrValidation)
	}

	// Additional validation before processing
	if cn.TotalAmount.IsZero() || cn.TotalAmount.IsNegative() {
		return ierr.NewError("credit note has no amount").
			WithHintf("This credit note has an amount of %s, but credit notes must have a positive amount to be processed.", cn.TotalAmount).
			WithReportableDetails(map[string]any{
				"total_amount": cn.TotalAmount,
			}).
			Mark(ierr.ErrValidation)
	}

	walletService := NewWalletService(s.ServiceParams)

	// Process the credit note in transaction
	err = s.DB.WithTx(ctx, func(tx context.Context) error {
		// Update credit note status first
		cn.CreditNoteStatus = types.CreditNoteStatusFinalized
		if err := s.CreditNoteRepo.Update(tx, cn); err != nil {
			return err
		}

		// Handle refund credit notes (wallet top-up logic)
		if cn.CreditNoteType == types.CreditNoteTypeRefund {
			// Get invoice using transaction context
			inv, err := s.InvoiceRepo.Get(tx, cn.InvoiceID)
			if err != nil {
				return err
			}

			// Find or create wallet using transaction context
			wallets, err := walletService.GetWalletsByCustomerID(tx, inv.CustomerID)
			if err != nil {
				return err
			}

			var selectedWallet *dto.WalletResponse
			for _, w := range wallets {
				if types.IsMatchingCurrency(w.Currency, inv.Currency) {
					selectedWallet = w
					break
				}
			}
			if selectedWallet == nil {
				// Create new wallet using transaction context
				walletReq := &dto.CreateWalletRequest{
					Name:           "Subscription Wallet",
					CustomerID:     inv.CustomerID,
					Currency:       inv.Currency,
					ConversionRate: decimal.NewFromInt(1), // Set default conversion rate to avoid division by zero
					WalletType:     types.WalletTypePrePaid,
				}

				selectedWallet, err = walletService.CreateWallet(tx, walletReq)
				if err != nil {
					return err
				}
			}

			// Top up wallet using transaction context
			walletTxnReq := &dto.TopUpWalletRequest{
				Amount:            cn.TotalAmount,
				TransactionReason: types.TransactionReasonCreditNote,
				Metadata:          types.Metadata{"credit_note_id": cn.ID},
				IdempotencyKey:    &cn.ID, // Use credit note ID as idempotency key
				Description:       fmt.Sprintf("Credit note refund: %s", cn.CreditNoteNumber),
			}

			_, err = walletService.TopUpWallet(tx, selectedWallet.ID, walletTxnReq)
			if err != nil {
				return err
			}
		}

		// Recalculate invoice amounts after credit note finalization
		// This is needed to update the adjustment and refunded amounts
		inv, err := s.InvoiceRepo.Get(ctx, cn.InvoiceID)
		if err != nil {
			return err
		}

		if err := s.RecalculateInvoiceAmountsForCreditNote(ctx, inv, cn); err != nil {
			s.Logger.Errorw("failed to recalculate invoice amounts after credit note finalization",
				"error", err,
				"credit_note_id", cn.ID,
				"invoice_id", cn.InvoiceID)
		}

		return nil
	})

	if err != nil {
		return err
	}

	// Publish webhook event after successful transaction
	s.publishInternalWebhookEvent(ctx, types.WebhookEventCreditNoteUpdated, cn.ID)

	s.Logger.Infow("credit note processed successfully",
		"credit_note_id", id,
		"total_amount", cn.TotalAmount)

	return nil
}

func (s *creditNoteService) ValidateCreditNoteCreation(ctx context.Context, req *dto.CreateCreditNoteRequest) error {
	// Validate invoice status and payment status
	inv, err := s.validateInvoiceEligibility(ctx, req.InvoiceID)
	if err != nil {
		return err
	}

	// Validate credit note amounts and line items
	if err := s.validateCreditNoteAmounts(ctx, req, inv); err != nil {
		return err
	}

	return nil
}

// validateInvoiceEligibility validates that the invoice is eligible for credit note creation
func (s *creditNoteService) validateInvoiceEligibility(ctx context.Context, invoiceID string) (*invoice.Invoice, error) {
	inv, err := s.InvoiceRepo.Get(ctx, invoiceID)
	if err != nil {
		return nil, err
	}

	// validate invoice status
	if inv.InvoiceStatus != types.InvoiceStatusFinalized {
		return nil, ierr.NewError("invoice not ready for credit note").
			WithHintf("You can only create credit notes for finalized invoices. This invoice is currently %s.", inv.InvoiceStatus).
			WithReportableDetails(map[string]any{
				"invoice_status": inv.InvoiceStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	// validate invoice payment status
	if inv.PaymentStatus == types.PaymentStatusRefunded {
		return nil, ierr.NewError("cannot create credit note for fully refunded invoice").
			WithHintf("This invoice has already been fully refunded, so no additional credit notes can be created.").
			WithReportableDetails(map[string]any{
				"invoice_payment_status": inv.PaymentStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	return inv, nil
}

// validateCreditNoteAmounts validates credit note amounts and line items
func (s *creditNoteService) validateCreditNoteAmounts(ctx context.Context, req *dto.CreateCreditNoteRequest, inv *invoice.Invoice) error {
	// Determine the max creditable amount
	maxCreditableAmount, err := s.calculateMaxCreditableAmount(ctx, inv)
	if err != nil {
		return err
	}

	// Validate line items and calculate total amount
	totalCreditNoteAmount, err := s.validateLineItems(req, inv)
	if err != nil {
		return err
	}

	// Check if total amount exceeds max creditable amount
	if totalCreditNoteAmount.GreaterThan(maxCreditableAmount) {
		// Determine credit note type for better messaging
		creditNoteType, _ := s.getCreditNoteType(inv)

		var messageTemplate string
		if creditNoteType == types.CreditNoteTypeRefund {
			messageTemplate = "You can only refund up to %s for this invoice. You're trying to refund %s, but only %s is available based on payments received."
		} else if creditNoteType == types.CreditNoteTypeAdjustment {
			messageTemplate = "You can only credit up to %s for this invoice. You're trying to credit %s, but only %s is available after accounting for payments and existing credits."
		}

		return ierr.NewError("credit amount exceeds available limit").
			WithHintf(
				messageTemplate,
				maxCreditableAmount, totalCreditNoteAmount, maxCreditableAmount,
			).
			WithReportableDetails(map[string]any{
				"requested_amount":    totalCreditNoteAmount,
				"maximum_allowed":     maxCreditableAmount,
				"credit_note_type":    creditNoteType,
				"invoice_total":       inv.Total,
				"invoice_amount_paid": inv.AmountPaid,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

// calculateMaxCreditableAmount calculates the maximum amount that can be credited using stored amounts
func (s *creditNoteService) calculateMaxCreditableAmount(ctx context.Context, inv *invoice.Invoice) (decimal.Decimal, error) {
	creditNoteType, err := s.getCreditNoteType(inv)
	if err != nil {
		return decimal.Zero, err
	}

	// Calculate max creditable amount based on credit note type using stored amounts
	var maxCreditableAmount decimal.Decimal
	if creditNoteType == types.CreditNoteTypeRefund {
		// For refunds: max = amount_paid - already_refunded
		// Example: Invoice total=$100, paid=$100, refunded=$30 → max refund=$70
		maxCreditableAmount = inv.AmountPaid.Sub(inv.RefundedAmount)
	} else if creditNoteType == types.CreditNoteTypeAdjustment {
		// For adjustments: max = total - already_adjusted - amount_paid
		// Example: Invoice total=$100, paid=$40, adjusted=$0 → max adjustment=$60
		// This prevents over-crediting: customer paid $40, max adjustment is $60,
		// resulting in effective invoice amount of $40 (which matches what was paid)
		maxCreditableAmount = inv.Total.Sub(inv.AdjustmentAmount).Sub(inv.AmountPaid)
	}

	// Ensure max creditable amount is not negative
	if maxCreditableAmount.LessThan(decimal.Zero) {
		maxCreditableAmount = decimal.Zero
	}

	s.Logger.Debugw("calculated max creditable amount using stored fields",
		"invoice_id", inv.ID,
		"credit_note_type", creditNoteType,
		"invoice_total", inv.Total,
		"invoice_amount_paid", inv.AmountPaid,
		"invoice_adjustment_amount", inv.AdjustmentAmount,
		"invoice_refunded_amount", inv.RefundedAmount,
		"max_creditable_amount", maxCreditableAmount)

	return maxCreditableAmount, nil
}

// validateLineItems validates credit note line items against invoice line items
func (s *creditNoteService) validateLineItems(req *dto.CreateCreditNoteRequest, inv *invoice.Invoice) (decimal.Decimal, error) {
	// Create map of invoice line item id to invoice line item
	invoiceLineItemMap := make(map[string]*invoice.InvoiceLineItem)
	for _, lineItem := range inv.LineItems {
		invoiceLineItemMap[lineItem.ID] = lineItem
	}

	// Validate each credit note line item and calculate total
	totalCreditNoteAmount := decimal.Zero
	for _, creditNoteLineItem := range req.LineItems {
		invLineItem, ok := invoiceLineItemMap[creditNoteLineItem.InvoiceLineItemID]

		if !ok {
			return decimal.Zero, ierr.NewError("invalid line item selected").
				WithHintf("The line item you're trying to credit (%s) doesn't exist on this invoice.", creditNoteLineItem.InvoiceLineItemID).
				Mark(ierr.ErrValidation)
		}

		// Validate line item amount
		if creditNoteLineItem.Amount.GreaterThan(invLineItem.Amount) {
			return decimal.Zero, ierr.NewError("credit amount too high for line item").
				WithHintf("You're trying to credit %s for this line item, but it was only charged %s on the original invoice.", creditNoteLineItem.Amount, invLineItem.Amount).
				WithReportableDetails(map[string]any{
					"credit_note_line_item_id":     creditNoteLineItem.InvoiceLineItemID,
					"credit_note_line_item_amount": creditNoteLineItem.Amount,
					"invoice_line_item_id":         invLineItem.ID,
					"invoice_line_item_amount":     invLineItem.Amount,
				}).
				Mark(ierr.ErrValidation)
		}

		totalCreditNoteAmount = totalCreditNoteAmount.Add(creditNoteLineItem.Amount)
	}

	return totalCreditNoteAmount, nil
}

func (s *creditNoteService) getCreditNoteType(inv *invoice.Invoice) (types.CreditNoteType, error) {
	// Determine credit note type based on invoice payment status
	switch inv.PaymentStatus {
	case types.PaymentStatusSucceeded:
		// Full payment received - can issue refund
		return types.CreditNoteTypeRefund, nil
	case types.PaymentStatusPartiallyRefunded:
		// Partial refund already issued - can issue additional refund
		return types.CreditNoteTypeRefund, nil
	case types.PaymentStatusFailed:
		// Payment failed - adjustment to reduce invoice amount
		return types.CreditNoteTypeAdjustment, nil
	case types.PaymentStatusPending, types.PaymentStatusProcessing:
		// Payment not yet complete - adjustment is appropriate
		return types.CreditNoteTypeAdjustment, nil
	default:
		return "", ierr.NewError("unknown payment status").
			WithHintf("Unknown payment status - %s", inv.PaymentStatus).
			WithReportableDetails(map[string]any{
				"payment_status": inv.PaymentStatus,
			}).
			Mark(ierr.ErrValidation)
	}
}

func (c *creditNoteService) RecalculateInvoiceAmountsForCreditNote(ctx context.Context, inv *invoice.Invoice, cn *creditnote.CreditNote) error {
	// Validate invoice status
	if inv.InvoiceStatus != types.InvoiceStatusFinalized {
		c.Logger.Infow("invoice is not finalized, skipping recalculation", "invoice_id", inv.ID)
		return nil
	}

	// Update amounts and payment status based on credit note type
	if cn.CreditNoteType == types.CreditNoteTypeRefund {
		inv.RefundedAmount = inv.RefundedAmount.Add(cn.TotalAmount)

		// Update payment status based on refund amount
		if inv.RefundedAmount.Equal(inv.AmountPaid) {
			inv.PaymentStatus = types.PaymentStatusRefunded
		} else if inv.RefundedAmount.GreaterThan(decimal.Zero) {
			inv.PaymentStatus = types.PaymentStatusPartiallyRefunded
		}

	} else if cn.CreditNoteType == types.CreditNoteTypeAdjustment {
		inv.AdjustmentAmount = inv.AdjustmentAmount.Add(cn.TotalAmount)
		inv.AmountDue = inv.Total.Sub(inv.AdjustmentAmount)

		// Recalculate remaining amount (ensure it doesn't go negative)
		inv.AmountRemaining = decimal.Max(inv.AmountDue.Sub(inv.AmountPaid), decimal.Zero)

		// Update payment status if invoice is now fully satisfied
		if inv.AmountRemaining.Equal(decimal.Zero) {
			inv.PaymentStatus = types.PaymentStatusSucceeded
		}
	}

	if err := c.InvoiceRepo.Update(ctx, inv); err != nil {
		return err
	}

	// Log the changes made
	c.Logger.Infow("invoice amounts recalculated after credit note",
		"invoice_id", inv.ID,
		"credit_note_id", cn.ID,
	)

	return nil
}

func (c *creditNoteService) publishInternalWebhookEvent(ctx context.Context, eventType string, creditNoteID string) {
	webhookPayload, err := json.Marshal(webhookDto.InternalCreditNoteEvent{
		CreditNoteID: creditNoteID,
		TenantID:     types.GetTenantID(ctx),
	})
	if err != nil {
		c.Logger.Errorw("failed to marshal webhook payload", "error", err)
		return
	}

	webhookEvent := &types.WebhookEvent{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WEBHOOK_EVENT),
		EventName:     eventType,
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
		UserID:        types.GetUserID(ctx),
		Timestamp:     time.Now().UTC(),
		Payload:       json.RawMessage(webhookPayload),
	}
	c.Logger.Infow("attempting to publish webhook event",
		"webhook_id", webhookEvent.ID,
		"event_name", eventType,
		"credit_note_id", creditNoteID,
		"tenant_id", webhookEvent.TenantID,
		"environment_id", webhookEvent.EnvironmentID,
	)

	if err := c.WebhookPublisher.PublishWebhook(ctx, webhookEvent); err != nil {
		c.Logger.Errorw("failed to publish webhook event",
			"error", err,
			"webhook_id", webhookEvent.ID,
			"event_name", eventType,
			"credit_note_id", creditNoteID,
		)
		return
	}

	c.Logger.Infow("webhook event published successfully",
		"webhook_id", webhookEvent.ID,
		"event_name", eventType,
		"credit_note_id", creditNoteID,
	)

}
