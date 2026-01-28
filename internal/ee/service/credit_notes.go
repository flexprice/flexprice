package enterprise

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
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

// CreditNoteService defines the enterprise credit note service interface.
type CreditNoteService interface {
	CreateCreditNote(ctx context.Context, req *dto.CreateCreditNoteRequest) (*dto.CreditNoteResponse, error)
	GetCreditNote(ctx context.Context, id string) (*dto.CreditNoteResponse, error)
	ListCreditNotes(ctx context.Context, filter *types.CreditNoteFilter) (*dto.ListCreditNotesResponse, error)
	VoidCreditNote(ctx context.Context, id string) error
	FinalizeCreditNote(ctx context.Context, id string) error
}

type creditNoteService struct {
	EnterpriseParams
}

// NewCreditNoteService creates a new enterprise credit note service.
func NewCreditNoteService(p EnterpriseParams) CreditNoteService {
	return &creditNoteService{EnterpriseParams: p}
}

func (s *creditNoteService) CreateCreditNote(ctx context.Context, req *dto.CreateCreditNoteRequest) (*dto.CreditNoteResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	var creditNote *creditnote.CreditNote

	err := s.DB.WithTx(ctx, func(tx context.Context) error {
		s.Logger.Infow("creating credit note",
			"invoice_id", req.InvoiceID,
			"reason", req.Reason,
			"line_items_count", len(req.LineItems))

		if err := s.validateCreditNoteCreation(tx, req); err != nil {
			return err
		}

		inv, err := s.InvoiceRepo.Get(tx, req.InvoiceID)
		if err != nil {
			return err
		}

		creditNoteType, err := s.getCreditNoteType(inv)
		if err != nil {
			return err
		}

		if req.CreditNoteNumber == "" {
			req.CreditNoteNumber = types.GenerateShortIDWithPrefix(types.SHORT_ID_PREFIX_CREDIT_NOTE)
		}

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

		if existingCreditNote, err := s.CreditNoteRepo.GetByIdempotencyKey(tx, *req.IdempotencyKey); err == nil {
			s.Logger.Infow("returning existing credit note for idempotency key",
				"idempotency_key", *req.IdempotencyKey,
				"existing_credit_note_id", existingCreditNote.ID)
			creditNote = existingCreditNote
			return nil
		}

		cn := req.ToCreditNote(tx, inv)
		cn.CreditNoteType = creditNoteType
		cn.CreditNoteStatus = types.CreditNoteStatusDraft
		cn.SubscriptionID = inv.SubscriptionID
		cn.CustomerID = inv.CustomerID

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

	s.publishInternalWebhookEvent(ctx, types.WebhookEventCreditNoteCreated, creditNote.ID)

	if req.ProcessCreditNote {
		if err := s.FinalizeCreditNote(ctx, creditNote.ID); err != nil {
			return nil, err
		}
	}

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

	sp := s.ServiceParams
	invoiceService := service.NewInvoiceService(sp)
	subscriptionService := service.NewSubscriptionService(sp)
	customerService := service.NewCustomerService(sp)

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

	response := &dto.ListCreditNotesResponse{
		Items: make([]*dto.CreditNoteResponse, len(creditNotes)),
	}

	sp := s.ServiceParams
	invoiceService := service.NewInvoiceService(sp)
	subscriptionService := service.NewSubscriptionService(sp)
	customerService := service.NewCustomerService(sp)

	var invoicesByID map[string]*dto.InvoiceResponse
	if filter.GetExpand().Has(types.ExpandInvoice) && len(creditNotes) > 0 {
		invoiceIDs := make([]string, 0)
		invoiceIDMap := make(map[string]bool)
		for _, cn := range creditNotes {
			if cn.InvoiceID != "" && !invoiceIDMap[cn.InvoiceID] {
				invoiceIDs = append(invoiceIDs, cn.InvoiceID)
				invoiceIDMap[cn.InvoiceID] = true
			}
		}

		if len(invoiceIDs) > 0 {
			invoiceFilter := &types.InvoiceFilter{InvoiceIDs: invoiceIDs}
			invoicesResponse, err := invoiceService.ListInvoices(ctx, invoiceFilter)
			if err != nil {
				return nil, err
			}
			invoicesByID = make(map[string]*dto.InvoiceResponse, len(invoicesResponse.Items))
			for _, inv := range invoicesResponse.Items {
				invoicesByID[inv.ID] = inv
			}
		}
	}

	var subscriptionsByID map[string]*dto.SubscriptionResponse
	if filter.GetExpand().Has(types.ExpandSubscription) && len(creditNotes) > 0 {
		subscriptionIDs := make([]string, 0)
		subscriptionIDMap := make(map[string]bool)
		for _, cn := range creditNotes {
			if cn.SubscriptionID != nil && *cn.SubscriptionID != "" && !subscriptionIDMap[*cn.SubscriptionID] {
				subscriptionIDs = append(subscriptionIDs, *cn.SubscriptionID)
				subscriptionIDMap[*cn.SubscriptionID] = true
			}
		}

		if len(subscriptionIDs) > 0 {
			subscriptionFilter := &types.SubscriptionFilter{SubscriptionIDs: subscriptionIDs}
			subscriptionsResponse, err := subscriptionService.ListSubscriptions(ctx, subscriptionFilter)
			if err != nil {
				return nil, err
			}
			subscriptionsByID = make(map[string]*dto.SubscriptionResponse, len(subscriptionsResponse.Items))
			for _, sub := range subscriptionsResponse.Items {
				subscriptionsByID[sub.ID] = sub
			}
		}
	}

	var customersByID map[string]*dto.CustomerResponse
	if filter.GetExpand().Has(types.ExpandCustomer) && len(creditNotes) > 0 {
		customerIDs := make([]string, 0)
		customerIDMap := make(map[string]bool)
		for _, cn := range creditNotes {
			if cn.CustomerID != "" && !customerIDMap[cn.CustomerID] {
				customerIDs = append(customerIDs, cn.CustomerID)
				customerIDMap[cn.CustomerID] = true
			}
		}

		if len(customerIDs) > 0 {
			customerFilter := &types.CustomerFilter{CustomerIDs: customerIDs}
			customersResponse, err := customerService.GetCustomers(ctx, customerFilter)
			if err != nil {
				return nil, err
			}
			customersByID = make(map[string]*dto.CustomerResponse, len(customersResponse.Items))
			for _, cust := range customersResponse.Items {
				customersByID[cust.ID] = cust
			}
		}
	}

	for i, cn := range creditNotes {
		response.Items[i] = &dto.CreditNoteResponse{CreditNote: cn}
		if filter.GetExpand().Has(types.ExpandInvoice) && cn.InvoiceID != "" {
			if inv, ok := invoicesByID[cn.InvoiceID]; ok {
				response.Items[i].Invoice = inv
			}
		}
		if filter.GetExpand().Has(types.ExpandSubscription) && cn.SubscriptionID != nil && *cn.SubscriptionID != "" {
			if sub, ok := subscriptionsByID[*cn.SubscriptionID]; ok {
				response.Items[i].Subscription = sub
			}
		}
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

	if cn.CreditNoteStatus != types.CreditNoteStatusDraft && cn.CreditNoteStatus != types.CreditNoteStatusFinalized {
		return ierr.NewError("cannot void this credit note").
			WithHintf("This credit note is %s and cannot be voided.", cn.CreditNoteStatus).
			WithReportableDetails(map[string]any{
				"current_status": cn.CreditNoteStatus,
				"allowed_statuses": []types.CreditNoteStatus{
					types.CreditNoteStatusDraft,
					types.CreditNoteStatusFinalized,
				},
			}).
			Mark(ierr.ErrValidation)
	}

	if cn.CreditNoteType == types.CreditNoteTypeRefund && cn.CreditNoteStatus == types.CreditNoteStatusFinalized {
		return ierr.NewError("cannot void completed refund").
			WithHint("This refund has already been processed.").
			Mark(ierr.ErrValidation)
	}

	originalStatus := cn.CreditNoteStatus
	cn.CreditNoteStatus = types.CreditNoteStatusVoided

	if err := s.CreditNoteRepo.Update(ctx, cn); err != nil {
		return err
	}

	if originalStatus == types.CreditNoteStatusFinalized {
		sp := s.ServiceParams
		invoiceService := service.NewInvoiceService(sp)
		if err := invoiceService.RecalculateInvoiceAmounts(ctx, cn.InvoiceID); err != nil {
			s.Logger.Errorw("failed to recalculate invoice amounts after credit note void",
				"error", err, "credit_note_id", cn.ID, "invoice_id", cn.InvoiceID)
		}
	}

	s.publishInternalWebhookEvent(ctx, types.WebhookEventCreditNoteUpdated, cn.ID)
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
			WithHintf("This credit note is %s.", cn.CreditNoteStatus).
			Mark(ierr.ErrValidation)
	}

	if cn.TotalAmount.IsZero() || cn.TotalAmount.IsNegative() {
		return ierr.NewError("credit note has no amount").
			WithHintf("Amount is %s.", cn.TotalAmount).
			Mark(ierr.ErrValidation)
	}

	sp := s.ServiceParams
	walletService := service.NewWalletService(sp)

	err = s.DB.WithTx(ctx, func(tx context.Context) error {
		cn.CreditNoteStatus = types.CreditNoteStatusFinalized
		if err := s.CreditNoteRepo.Update(tx, cn); err != nil {
			return err
		}

		if cn.CreditNoteType == types.CreditNoteTypeRefund {
			inv, err := s.InvoiceRepo.Get(tx, cn.InvoiceID)
			if err != nil {
				return err
			}

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
				walletReq := &dto.CreateWalletRequest{
					Name:           "Subscription Wallet",
					CustomerID:     inv.CustomerID,
					Currency:       inv.Currency,
					ConversionRate: decimal.NewFromInt(1),
					WalletType:     types.WalletTypePrePaid,
				}
				selectedWallet, err = walletService.CreateWallet(tx, walletReq)
				if err != nil {
					return err
				}
			}

			walletTxnReq := &dto.TopUpWalletRequest{
				Amount:            cn.TotalAmount,
				TransactionReason: types.TransactionReasonCreditNote,
				Metadata:          types.Metadata{"credit_note_id": cn.ID},
				IdempotencyKey:    &cn.ID,
				Description:       fmt.Sprintf("Credit note refund: %s", cn.CreditNoteNumber),
			}
			if _, err = walletService.TopUpWallet(tx, selectedWallet.ID, walletTxnReq); err != nil {
				return err
			}
		}

		inv, err := s.InvoiceRepo.Get(ctx, cn.InvoiceID)
		if err != nil {
			return err
		}
		return s.recalculateInvoiceAmountsForCreditNote(ctx, inv, cn)
	})

	if err != nil {
		return err
	}

	s.publishInternalWebhookEvent(ctx, types.WebhookEventCreditNoteUpdated, cn.ID)
	return nil
}

func (s *creditNoteService) validateCreditNoteCreation(ctx context.Context, req *dto.CreateCreditNoteRequest) error {
	inv, err := s.validateInvoiceEligibility(ctx, req.InvoiceID)
	if err != nil {
		return err
	}
	return s.validateCreditNoteAmounts(ctx, req, inv)
}

func (s *creditNoteService) validateInvoiceEligibility(ctx context.Context, invoiceID string) (*invoice.Invoice, error) {
	inv, err := s.InvoiceRepo.Get(ctx, invoiceID)
	if err != nil {
		return nil, err
	}
	if inv.InvoiceStatus != types.InvoiceStatusFinalized {
		return nil, ierr.NewError("invoice not ready for credit note").
			WithHintf("Invoice is %s.", inv.InvoiceStatus).
			Mark(ierr.ErrValidation)
	}
	if inv.PaymentStatus == types.PaymentStatusRefunded {
		return nil, ierr.NewError("cannot create credit note for fully refunded invoice").
			Mark(ierr.ErrValidation)
	}
	return inv, nil
}

func (s *creditNoteService) validateCreditNoteAmounts(ctx context.Context, req *dto.CreateCreditNoteRequest, inv *invoice.Invoice) error {
	maxCreditableAmount, err := s.calculateMaxCreditableAmount(ctx, inv)
	if err != nil {
		return err
	}
	totalCreditNoteAmount, err := s.validateLineItems(req, inv)
	if err != nil {
		return err
	}
	if totalCreditNoteAmount.GreaterThan(maxCreditableAmount) {
		creditNoteType, _ := s.getCreditNoteType(inv)
		return ierr.NewError("credit amount exceeds available limit").
			WithHintf("Requested %s, max %s.", totalCreditNoteAmount, maxCreditableAmount).
			WithReportableDetails(map[string]any{
				"requested_amount": totalCreditNoteAmount,
				"maximum_allowed":  maxCreditableAmount,
				"credit_note_type": creditNoteType,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (s *creditNoteService) calculateMaxCreditableAmount(ctx context.Context, inv *invoice.Invoice) (decimal.Decimal, error) {
	creditNoteType, err := s.getCreditNoteType(inv)
	if err != nil {
		return decimal.Zero, err
	}
	var maxCreditableAmount decimal.Decimal
	if creditNoteType == types.CreditNoteTypeRefund {
		maxCreditableAmount = inv.AmountPaid.Sub(inv.RefundedAmount)
	} else {
		maxCreditableAmount = inv.Total.Sub(inv.AdjustmentAmount).Sub(inv.AmountPaid)
	}
	if maxCreditableAmount.LessThan(decimal.Zero) {
		maxCreditableAmount = decimal.Zero
	}
	return maxCreditableAmount, nil
}

func (s *creditNoteService) validateLineItems(req *dto.CreateCreditNoteRequest, inv *invoice.Invoice) (decimal.Decimal, error) {
	invoiceLineItemMap := make(map[string]*invoice.InvoiceLineItem)
	for _, lineItem := range inv.LineItems {
		invoiceLineItemMap[lineItem.ID] = lineItem
	}
	totalCreditNoteAmount := decimal.Zero
	for _, creditNoteLineItem := range req.LineItems {
		invLineItem, ok := invoiceLineItemMap[creditNoteLineItem.InvoiceLineItemID]
		if !ok {
			return decimal.Zero, ierr.NewError("invalid line item selected").
				WithHintf("Line item %s doesn't exist on this invoice.", creditNoteLineItem.InvoiceLineItemID).
				Mark(ierr.ErrValidation)
		}
		if creditNoteLineItem.Amount.GreaterThan(invLineItem.Amount) {
			return decimal.Zero, ierr.NewError("credit amount too high for line item").
				WithHintf("Crediting %s but line item was %s.", creditNoteLineItem.Amount, invLineItem.Amount).
				Mark(ierr.ErrValidation)
		}
		totalCreditNoteAmount = totalCreditNoteAmount.Add(creditNoteLineItem.Amount)
	}
	return totalCreditNoteAmount, nil
}

func (s *creditNoteService) getCreditNoteType(inv *invoice.Invoice) (types.CreditNoteType, error) {
	switch inv.PaymentStatus {
	case types.PaymentStatusSucceeded, types.PaymentStatusPartiallyRefunded:
		return types.CreditNoteTypeRefund, nil
	case types.PaymentStatusFailed:
		return types.CreditNoteTypeAdjustment, nil
	case types.PaymentStatusPending, types.PaymentStatusProcessing:
		return types.CreditNoteTypeAdjustment, nil
	default:
		return "", ierr.NewError("unknown payment status").
			WithHintf("Status: %s", inv.PaymentStatus).
			Mark(ierr.ErrValidation)
	}
}

func (s *creditNoteService) recalculateInvoiceAmountsForCreditNote(ctx context.Context, inv *invoice.Invoice, cn *creditnote.CreditNote) error {
	if inv.InvoiceStatus != types.InvoiceStatusFinalized {
		return nil
	}
	if cn.CreditNoteType == types.CreditNoteTypeRefund {
		inv.RefundedAmount = inv.RefundedAmount.Add(cn.TotalAmount)
		if inv.RefundedAmount.Equal(inv.AmountPaid) {
			inv.PaymentStatus = types.PaymentStatusRefunded
		} else if inv.RefundedAmount.GreaterThan(decimal.Zero) {
			inv.PaymentStatus = types.PaymentStatusPartiallyRefunded
		}
	} else if cn.CreditNoteType == types.CreditNoteTypeAdjustment {
		inv.AdjustmentAmount = inv.AdjustmentAmount.Add(cn.TotalAmount)
		inv.AmountDue = inv.Total.Sub(inv.AdjustmentAmount)
		inv.AmountRemaining = decimal.Max(inv.AmountDue.Sub(inv.AmountPaid), decimal.Zero)
		if inv.AmountRemaining.Equal(decimal.Zero) {
			inv.PaymentStatus = types.PaymentStatusSucceeded
		}
	}
	return s.InvoiceRepo.Update(ctx, inv)
}

func (s *creditNoteService) publishInternalWebhookEvent(ctx context.Context, eventType string, creditNoteID string) {
	webhookPayload, err := json.Marshal(webhookDto.InternalCreditNoteEvent{
		CreditNoteID: creditNoteID,
		TenantID:     types.GetTenantID(ctx),
	})
	if err != nil {
		s.Logger.Errorw("failed to marshal webhook payload", "error", err)
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
	if err := s.WebhookPublisher.PublishWebhook(ctx, webhookEvent); err != nil {
		s.Logger.Errorw("failed to publish webhook event", "error", err, "event_name", eventType)
	}
}
