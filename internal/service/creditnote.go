package service

import (
	"context"

	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/creditnote"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

type CreditNoteService interface {
	CreateCreditNote(ctx context.Context, req *dto.CreateCreditNoteRequest) (*dto.CreditNoteResponse, error)
	GetCreditNote(ctx context.Context, id string) (*dto.CreditNoteResponse, error)
	ListCreditNotes(ctx context.Context, filter *types.CreditNoteFilter) (*dto.ListCreditNotesResponse, error)

	// This method is used to void a credit note
	// this can be done when credit note is a adjustment and not a refund so we can cancel the adjustment
	VoidCreditNote(ctx context.Context, id string) error

	ProcessDraftCreditNote(ctx context.Context, id string) error
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

		// Generate credit note number if not provided
		if req.CreditNoteNumber == "" {
			req.CreditNoteNumber = s.generateCreditNoteNumber()
		}

		// Convert request to domain model
		cn := req.ToCreditNote(tx, inv)

		// Set default status
		cn.CreditNoteStatus = types.CreditNoteStatusDraft

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

	// Process the credit note
	if err := s.ProcessDraftCreditNote(ctx, creditNote.ID); err != nil {
		return nil, err
	}

	return &dto.CreditNoteResponse{
		CreditNote: creditNote,
	}, nil
}

func (s *creditNoteService) GetCreditNote(ctx context.Context, id string) (*dto.CreditNoteResponse, error) {
	cn, err := s.CreditNoteRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	return &dto.CreditNoteResponse{
		CreditNote: cn,
	}, nil
}

func (s *creditNoteService) ListCreditNotes(ctx context.Context, filter *types.CreditNoteFilter) (*dto.ListCreditNotesResponse, error) {
	creditNotes, err := s.CreditNoteRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	total, err := s.CreditNoteRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	items := make([]*dto.CreditNoteResponse, len(creditNotes))
	for i, cn := range creditNotes {
		items[i] = &dto.CreditNoteResponse{
			CreditNote: cn,
		}
	}

	return &dto.ListCreditNotesResponse{
		Items:      items,
		Pagination: types.NewPaginationResponse(total, filter.GetLimit(), filter.GetOffset()),
	}, nil
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
	if cn.CreditNoteType == types.CreditNoteTypeRefund && cn.RefundStatus != nil {
		return ierr.NewError("refund credit note cannot be voided").
			WithHint("Credit note with refund status cannot be voided").
			WithReportableDetails(map[string]any{
				"refund_status": *cn.RefundStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	cn.CreditNoteStatus = types.CreditNoteStatusVoided

	if err := s.CreditNoteRepo.Update(ctx, cn); err != nil {
		return err
	}

	s.Logger.Infow("credit note voided successfully",
		"credit_note_id", id,
		"previous_status", cn.CreditNoteStatus)

	return nil
}

func (s *creditNoteService) ProcessDraftCreditNote(ctx context.Context, id string) error {
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
			inv, err := invoiceService.GetInvoice(tx, cn.InvoiceID)
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
					Name:       "Subscription Wallet",
					CustomerID: inv.CustomerID,
					Currency:   inv.Currency,
				}

				selectedWallet, err = walletService.CreateWallet(tx, walletReq)
				if err != nil {
					return err
				}
			}

			// Top up wallet using transaction context
			walletTxnReq := &dto.TopUpWalletRequest{
				Amount:            cn.TotalAmount.Neg(),
				TransactionReason: types.TransactionReasonCreditNoteRefund,
				Metadata:          types.Metadata{"credit_note_id": cn.ID},
			}

			_, err = walletService.TopUpWallet(tx, inv.CustomerID, walletTxnReq)
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
			WithHintf("Total credit note amount - %s is greater than max creditable amount - %s", totalCreditNoteAmount, maxCreditableAmount).
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

// generateCreditNoteNumber generates a unique credit note number
func (s *creditNoteService) generateCreditNoteNumber() string {
	// In production, you'd want proper sequence generation with database sequences
	// or a more sophisticated numbering scheme
	return CreditNoteNumberPrefix + "-" + types.GenerateUUIDWithPrefix("")[0:CreditNoteNumberLength]
}
