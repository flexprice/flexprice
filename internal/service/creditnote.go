package service

import (
	"context"
	"fmt"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/creditnote"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/idempotency"
	"github.com/flexprice/flexprice/internal/types"
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

	// Finalize the credit note
	if err := s.FinalizeCreditNote(ctx, creditNote.ID); err != nil {
		return nil, err
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
		return ierr.NewError("credit note id is required").
			WithHint("Credit note ID cannot be empty").
			Mark(ierr.ErrValidation)
	}

	cn, err := s.CreditNoteRepo.Get(ctx, id)
	if err != nil {
		return err
	}

	// Only draft and finalized credit notes can be voided
	if cn.CreditNoteStatus != types.CreditNoteStatusDraft && cn.CreditNoteStatus != types.CreditNoteStatusFinalized {
		return ierr.NewError("credit note status is not allowed").
			WithHintf("Credit note status - %s is not allowed for voiding. Only draft or finalized credit notes can be voided", cn.CreditNoteStatus).
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
		return ierr.NewError("refund credit note cannot be voided").
			WithHint("Finalized refund credit note cannot be voided").
			WithReportableDetails(map[string]any{
				"credit_note_status": cn.CreditNoteStatus,
				"credit_note_type":   cn.CreditNoteType,
			}).
			Mark(ierr.ErrValidation)
	}

	cn.CreditNoteStatus = types.CreditNoteStatusVoided

	if err := s.CreditNoteRepo.Update(ctx, cn); err != nil {
		return err
	}

	// Recalculate invoice amounts
	invoiceService := NewInvoiceService(s.ServiceParams)
	if err := invoiceService.RecalculateInvoiceAmounts(ctx, cn.InvoiceID); err != nil {
		s.Logger.Errorw("failed to recalculate invoice amounts after credit note voiding",
			"error", err,
			"credit_note_id", id,
			"invoice_id", cn.InvoiceID)
	}

	s.Logger.Infow("credit note voided successfully",
		"credit_note_id", id,
		"previous_status", cn.CreditNoteStatus)

	return nil
}

func (s *creditNoteService) FinalizeCreditNote(ctx context.Context, id string) error {
	if id == "" {
		return ierr.NewError("credit note id is required").
			WithHint("Credit note ID cannot be empty").
			Mark(ierr.ErrValidation)
	}

	cn, err := s.CreditNoteRepo.Get(ctx, id)
	if err != nil {
		return err
	}

	if cn.CreditNoteStatus != types.CreditNoteStatusDraft {
		return ierr.NewError("credit note is not in draft status").
			WithHint("credit note must be in draft status to be processed").
			WithReportableDetails(map[string]any{
				"current_status":  cn.CreditNoteStatus,
				"required_status": types.CreditNoteStatusDraft,
			}).
			Mark(ierr.ErrValidation)
	}

	// Additional validation before processing
	if cn.TotalAmount.IsZero() || cn.TotalAmount.IsNegative() {
		return ierr.NewError("invalid credit note amount").
			WithHint("Credit note total amount must be positive").
			WithReportableDetails(map[string]any{
				"total_amount": cn.TotalAmount,
			}).
			Mark(ierr.ErrValidation)
	}

	invoiceService := NewInvoiceService(s.ServiceParams)
	walletService := NewWalletService(s.ServiceParams)

	// Process the credit note in transaction
	err = s.DB.WithTx(ctx, func(tx context.Context) error {
		// Update credit note status first
		cn.CreditNoteStatus = types.CreditNoteStatusFinalized
		if err := s.CreditNoteRepo.Update(tx, cn); err != nil {
			return err
		}

		// Handle refund credit notes
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

		return nil
	})

	if err != nil {
		return err
	}

	// Recalculate invoice amounts AFTER transaction is committed
	// This ensures the finalized credit note is visible to the recalculation query
	if cn.CreditNoteType == types.CreditNoteTypeAdjustment {
		err = invoiceService.RecalculateInvoiceAmounts(ctx, cn.InvoiceID)
		if err != nil {
			s.Logger.Errorw("failed to recalculate invoice amounts after credit note processing",
				"error", err,
				"credit_note_id", id,
				"invoice_id", cn.InvoiceID)
			return err
		}
	}

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
		return nil, ierr.NewError("invoice status is not allowed").
			WithHintf("Invoice must be finalized to issue a credit note").
			WithReportableDetails(map[string]any{
				"invoice_status": inv.InvoiceStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	// validate invoice payment status
	if inv.PaymentStatus == types.PaymentStatusRefunded {
		return nil, ierr.NewError("invoice payment status is not allowed").
			WithHintf("Credit note cannot be issued for a refunded invoice").
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
		return ierr.NewError("total credit note amount is greater than max creditable amount").
			WithHintf(
				"Credit note amount %s exceeds the available limit of %s. %s has already been credited against this invoice.",
				totalCreditNoteAmount, maxCreditableAmount, maxCreditableAmount.Sub(totalCreditNoteAmount),
			).
			WithReportableDetails(map[string]any{
				"total_credit_note_amount":    totalCreditNoteAmount,
				"max_creditable_amount":       maxCreditableAmount,
				"available_creditable_amount": maxCreditableAmount.Sub(totalCreditNoteAmount),
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

// calculateMaxCreditableAmount calculates the maximum amount that can be credited
func (s *creditNoteService) calculateMaxCreditableAmount(ctx context.Context, inv *invoice.Invoice) (decimal.Decimal, error) {
	creditNoteType, err := s.getCreditNoteType(inv)
	if err != nil {
		return decimal.Zero, err
	}

	// Get existing non-voided credit notes for this invoice in a single query
	creditNoteFilter := &types.CreditNoteFilter{
		InvoiceID:        inv.ID,
		CreditNoteType:   creditNoteType,
		CreditNoteStatus: []types.CreditNoteStatus{types.CreditNoteStatusFinalized},
	}

	creditNotes, err := s.CreditNoteRepo.List(ctx, creditNoteFilter)
	if err != nil {
		return decimal.Zero, err
	}

	// Calculate already credited amount (no need to filter for voided since query excludes them)
	alreadyCreditedAmount := decimal.Zero
	for _, creditNote := range creditNotes {
		alreadyCreditedAmount = alreadyCreditedAmount.Add(creditNote.TotalAmount)
	}

	// Calculate max creditable amount based on credit note type
	var maxCreditableAmount decimal.Decimal
	if creditNoteType == types.CreditNoteTypeRefund {
		maxCreditableAmount = inv.AmountPaid.Sub(alreadyCreditedAmount)
	}

	if creditNoteType == types.CreditNoteTypeAdjustment {
		maxCreditableAmount = inv.Total.Sub(alreadyCreditedAmount)
	}

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
			return decimal.Zero, ierr.NewError("invoice line item not found").
				WithHintf("Invoice line item - %s not found", creditNoteLineItem.InvoiceLineItemID).
				Mark(ierr.ErrValidation)
		}

		// Validate line item amount
		if creditNoteLineItem.Amount.GreaterThan(invLineItem.Amount) {
			return decimal.Zero, ierr.NewError("credit note line item amount is greater than invoice line item amount").
				WithHintf("Credit note line item amount - %s is greater than invoice line item amount - %s", creditNoteLineItem.Amount, invLineItem.Amount).
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
