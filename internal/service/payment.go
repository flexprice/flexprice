package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/idempotency"
	"github.com/flexprice/flexprice/internal/types"
)

// PaymentService defines the interface for payment operations
type PaymentService interface {
	// Core payment operations
	CreatePayment(ctx context.Context, req *dto.CreatePaymentRequest) (*dto.PaymentResponse, error)
	GetPayment(ctx context.Context, id string) (*dto.PaymentResponse, error)
	UpdatePayment(ctx context.Context, id string, req dto.UpdatePaymentRequest) (*dto.PaymentResponse, error)
	ListPayments(ctx context.Context, filter *types.PaymentFilter) (*dto.ListPaymentsResponse, error)
	DeletePayment(ctx context.Context, id string) error
}

type paymentService struct {
	ServiceParams
	idempGen *idempotency.Generator
}

// NewPaymentService creates a new payment service
func NewPaymentService(params ServiceParams) PaymentService {
	return &paymentService{
		ServiceParams: params,
		idempGen:      idempotency.NewGenerator(),
	}
}

// CreatePayment creates a new payment
func (s *paymentService) CreatePayment(ctx context.Context, req *dto.CreatePaymentRequest) (*dto.PaymentResponse, error) {
	p, err := req.ToPayment(ctx)
	if err != nil {
		return nil, err // Already using ierr in the DTO
	}

	if p.DestinationType != types.PaymentDestinationTypeInvoice {
		return nil, ierr.NewError("invalid destination type").
			WithHint("Only invoice destination type is supported").
			WithReportableDetails(map[string]interface{}{
				"destination_type": p.DestinationType,
			}).
			Mark(ierr.ErrValidation)
	}

	// validate the destination
	invoice, err := s.InvoiceRepo.Get(ctx, p.DestinationID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to validate invoice").
			WithReportableDetails(map[string]interface{}{
				"invoice_id": p.DestinationID,
			}).
			Mark(ierr.ErrValidation)
	}

	// validate the invoice payment eligibility
	if err := s.validateInvoicePaymentEligibility(ctx, invoice, req); err != nil {
		return nil, err
	}

	// select the wallet for the payment in case of credits payment where wallet id is not provided
	if p.PaymentMethodType == types.PaymentMethodTypeCredits && p.PaymentMethodID == "" {
		selectedWallet, err := s.selectWalletForPayment(ctx, invoice, req)
		if err != nil {
			return nil, err
		}

		p.PaymentMethodID = selectedWallet.ID

		// Add wallet information to metadata
		if p.Metadata == nil {
			p.Metadata = types.Metadata{}
		}
		p.Metadata["wallet_type"] = string(selectedWallet.WalletType)
		p.Metadata["wallet_id"] = selectedWallet.ID
	}

	// Generate idempotency key
	if p.IdempotencyKey == "" {
		p.IdempotencyKey = s.idempGen.GenerateKey(idempotency.ScopePayment, map[string]interface{}{
			"invoice_id": p.DestinationID,
			"amount":     p.Amount,
			"currency":   p.Currency,
			// TODO: think of a better way to generate this key rather than using the current timestamp
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
	}

	// validate the payment object before creating it
	if err := p.Validate(); err != nil {
		return nil, err
	}

	if err := s.PaymentRepo.Create(ctx, p); err != nil {
		return nil, err
	}

	if req.ProcessPayment {
		paymentProcessor := NewPaymentProcessorService(s.ServiceParams)
		p, err = paymentProcessor.ProcessPayment(ctx, p.ID)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to process payment").
				WithReportableDetails(map[string]interface{}{
					"payment_id": p.ID,
				}).
				Mark(ierr.ErrInvalidOperation)
		}
	}

	response := dto.NewPaymentResponse(p)
	// response.InvoiceNo = invoice.InvoiceNumber
	return response, nil
}

func (s *paymentService) validateInvoicePaymentEligibility(_ context.Context, invoice *invoice.Invoice, p *dto.CreatePaymentRequest) error {
	// invoice validations
	if invoice.PaymentStatus == types.PaymentStatusSucceeded {
		return ierr.NewError("invoice is already paid").
			WithHint("Cannot create payment for an already paid invoice").
			WithReportableDetails(map[string]interface{}{
				"invoice_id": p.DestinationID,
			}).
			Mark(ierr.ErrValidation)
	}

	if invoice.InvoiceStatus == types.InvoiceStatusVoided {
		return ierr.NewError("invoice is voided").
			WithHint("Cannot create payment for a voided invoice").
			WithReportableDetails(map[string]interface{}{
				"invoice_id": invoice.ID,
			}).
			Mark(ierr.ErrValidation)
	}

	if !types.IsMatchingCurrency(invoice.Currency, p.Currency) {
		return ierr.NewError("invoice currency does not match payment currency").
			WithHint("Payment currency must match invoice currency").
			WithReportableDetails(map[string]interface{}{
				"invoice_currency": invoice.Currency,
				"payment_currency": p.Currency,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

func (s *paymentService) selectWalletForPayment(ctx context.Context, invoice *invoice.Invoice, p *dto.CreatePaymentRequest) (*wallet.Wallet, error) {
	// Use the wallet payment service to find a suitable wallet
	walletPaymentService := NewWalletPaymentService(s.ServiceParams)

	// Use default options (promotional wallets first, then prepaid)
	options := DefaultWalletPaymentOptions()
	options.AdditionalMetadata = p.Metadata
	options.MaxWalletsToUse = 1 // Only need one wallet for this payment

	// Get wallets suitable for payment
	wallets, err := walletPaymentService.GetWalletsForPayment(ctx, invoice.CustomerID, p.Currency, options)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to find customer wallets").
			WithReportableDetails(map[string]interface{}{
				"customer_id": invoice.CustomerID,
			}).
			Mark(ierr.ErrDatabase)
	}

	if len(wallets) == 0 || len(wallets) > 1 {
		return nil, ierr.NewError("no wallets found for customer").
			WithHint("Customer must have at least one wallet to use credits").
			WithReportableDetails(map[string]interface{}{
				"customer_id": invoice.CustomerID,
			}).
			Mark(ierr.ErrNotFound)
	}

	// Find first wallet with sufficient balance
	var selectedWallet *wallet.Wallet
	for _, w := range wallets {
		if w.Balance.GreaterThanOrEqual(p.Amount) {
			selectedWallet = w
			break
		}
	}

	if selectedWallet == nil {
		return nil, ierr.NewError("no wallet with sufficient balance found").
			WithHint("Customer does not have an active wallet with sufficient balance").
			WithReportableDetails(map[string]interface{}{
				"customer_id": invoice.CustomerID,
				"amount":      p.Amount,
				"currency":    p.Currency,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	return selectedWallet, nil
}

// GetPayment gets a payment by ID
func (s *paymentService) GetPayment(ctx context.Context, id string) (*dto.PaymentResponse, error) {
	if id == "" {
		return nil, ierr.NewError("payment_id is required").
			WithHint("Payment ID is required").
			Mark(ierr.ErrValidation)
	}

	p, err := s.PaymentRepo.Get(ctx, id)
	if err != nil {
		return nil, err // Repository already using ierr
	}

	response := dto.NewPaymentResponse(p)
	if p.DestinationType == types.PaymentDestinationTypeInvoice {
		invoice, err := s.InvoiceRepo.Get(ctx, p.DestinationID)
		if err == nil && invoice != nil {
			response.InvoiceNumber = invoice.InvoiceNumber
		}
	}
	return response, nil
}

// UpdatePayment updates a payment
func (s *paymentService) UpdatePayment(ctx context.Context, id string, req dto.UpdatePaymentRequest) (*dto.PaymentResponse, error) {
	if id == "" {
		return nil, ierr.NewError("payment_id is required").
			WithHint("Payment ID is required").
			Mark(ierr.ErrValidation)
	}

	p, err := s.PaymentRepo.Get(ctx, id)
	if err != nil {
		return nil, err // Repository already using ierr
	}

	if req.PaymentStatus != nil {
		p.PaymentStatus = types.PaymentStatus(*req.PaymentStatus)
	}
	if req.Metadata != nil {
		p.Metadata = *req.Metadata
	}

	if err := s.PaymentRepo.Update(ctx, p); err != nil {
		return nil, err // Repository already using ierr
	}

	return dto.NewPaymentResponse(p), nil
}

// ListPayments lists payments based on filter
func (s *paymentService) ListPayments(ctx context.Context, filter *types.PaymentFilter) (*dto.ListPaymentsResponse, error) {
	if filter == nil {
		filter = &types.PaymentFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	payments, err := s.PaymentRepo.List(ctx, filter)
	if err != nil {
		return nil, err // Repository already using ierr
	}

	count, err := s.PaymentRepo.Count(ctx, filter)
	if err != nil {
		return nil, err // Repository already using ierr
	}

	// Collect all invoice IDs from payments
	invoiceIDs := make([]string, 0)
	for _, p := range payments {
		if p.DestinationType == types.PaymentDestinationTypeInvoice {
			invoiceIDs = append(invoiceIDs, p.DestinationID)
		}
	}

	// Create a map of invoice ID to invoice number
	invoiceNumberMap := make(map[string]*string)
	if len(invoiceIDs) > 0 {
		// Fetch all invoices in a single query
		invoiceFilter := &types.InvoiceFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
			InvoiceIDs:  invoiceIDs,
		}
		invoices, err := s.InvoiceRepo.List(ctx, invoiceFilter)
		if err == nil {
			for _, inv := range invoices {
				invoiceNumberMap[inv.ID] = inv.InvoiceNumber
			}
		}
	}

	// Create payment responses with invoice numbers
	items := make([]*dto.PaymentResponse, len(payments))
	for i, p := range payments {
		response := dto.NewPaymentResponse(p)
		if p.DestinationType == types.PaymentDestinationTypeInvoice {
			if invoiceNumber, exists := invoiceNumberMap[p.DestinationID]; exists {
				response.InvoiceNumber = invoiceNumber
			}
		}
		items[i] = response
	}

	return &dto.ListPaymentsResponse{
		Items: items,
		Pagination: types.NewPaginationResponse(
			count,
			filter.GetLimit(),
			filter.GetOffset(),
		),
	}, nil
}

// DeletePayment deletes a payment
func (s *paymentService) DeletePayment(ctx context.Context, id string) error {
	if id == "" {
		return ierr.NewError("payment_id is required").
			WithHint("Payment ID is required").
			Mark(ierr.ErrValidation)
	}

	return s.PaymentRepo.Delete(ctx, id) // Repository already using ierr
}
