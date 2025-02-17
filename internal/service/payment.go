package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/idempotency"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

// PaymentService defines the interface for payment operations
type PaymentService interface {
	// Core payment operations
	CreatePayment(ctx context.Context, req dto.CreatePaymentRequest) (*dto.PaymentResponse, error)
	GetPayment(ctx context.Context, id string) (*dto.PaymentResponse, error)
	UpdatePayment(ctx context.Context, id string, req dto.UpdatePaymentRequest) (*dto.PaymentResponse, error)
	ListPayments(ctx context.Context, filter *types.PaymentFilter) (*dto.ListPaymentsResponse, error)
	DeletePayment(ctx context.Context, id string) error
}

type paymentService struct {
	paymentRepo payment.Repository
	invoiceRepo invoice.Repository
	walletRepo  wallet.Repository
	logger      *logger.Logger
	db          postgres.IClient
	config      *config.Configuration
	idempGen    *idempotency.Generator
}

// NewPaymentService creates a new payment service
func NewPaymentService(
	paymentRepo payment.Repository,
	invoiceRepo invoice.Repository,
	walletRepo wallet.Repository,
	db postgres.IClient,
	logger *logger.Logger,
	config *config.Configuration,
) PaymentService {
	return &paymentService{
		paymentRepo: paymentRepo,
		invoiceRepo: invoiceRepo,
		walletRepo:  walletRepo,
		logger:      logger,
		db:          db,
		config:      config,
		idempGen:    idempotency.NewGenerator(),
	}
}

// CreatePayment creates a new payment
func (s *paymentService) CreatePayment(ctx context.Context, req dto.CreatePaymentRequest) (*dto.PaymentResponse, error) {
	p, err := req.ToPayment(ctx)
	if err != nil {
		return nil, err
	}

	if p.DestinationType != types.PaymentDestinationTypeInvoice {
		return nil, errors.New(errors.ErrCodeValidation, "invalid destination type")
	}

	// validate the destination

	invoice, err := s.invoiceRepo.Get(ctx, p.DestinationID)
	if err != nil {
		return nil, err
	}

	// invoice validations
	if invoice.PaymentStatus == types.PaymentStatusSucceeded {
		return nil, errors.New(errors.ErrCodeValidation, "invoice is already paid")
	}

	if invoice.InvoiceStatus == types.InvoiceStatusVoided {
		return nil, errors.New(errors.ErrCodeValidation, "invoice is voided")
	}

	if !types.IsMatchingCurrency(invoice.Currency, p.Currency) {
		return nil, errors.New(errors.ErrCodeValidation, "invoice currency does not match payment currency")
	}

	// check if the payment method is credits
	if p.PaymentMethodType == types.PaymentMethodTypeCredits {
		// Find wallets for the customer
		wallets, err := s.walletRepo.GetWalletsByCustomerID(ctx, invoice.CustomerID)
		if err != nil {
			return nil, errors.Wrap(err, errors.ErrCodeInvalidOperation, "failed to find wallets")
		}

		if len(wallets) == 0 {
			return nil, errors.New(errors.ErrCodeNotFound, "no wallets found for customer")
		}

		// Find first active wallet with matching currency and sufficient balance
		var selectedWallet *wallet.Wallet
		for _, w := range wallets {
			if w.WalletStatus == types.WalletStatusActive &&
				w.Currency == p.Currency &&
				w.Balance.GreaterThanOrEqual(p.Amount) {
				selectedWallet = w
				break
			}
		}

		if selectedWallet == nil {
			return nil, errors.New(errors.ErrCodeInvalidOperation, "no wallet with sufficient balance found")
		}

		p.PaymentMethodID = selectedWallet.ID
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

	if err := s.paymentRepo.Create(ctx, p); err != nil {
		return nil, err
	}

	if req.ProcessPayment {
		paymentProcessor := NewPaymentProcessorService(s.paymentRepo, s.invoiceRepo, s.walletRepo, s.db, s.logger)
		p, err = paymentProcessor.ProcessPayment(ctx, p.ID)
		if err != nil {
			return nil, err
		}
	}

	return dto.NewPaymentResponse(p), nil
}

// GetPayment gets a payment by ID
func (s *paymentService) GetPayment(ctx context.Context, id string) (*dto.PaymentResponse, error) {
	p, err := s.paymentRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	return dto.NewPaymentResponse(p), nil
}

// UpdatePayment updates a payment
func (s *paymentService) UpdatePayment(ctx context.Context, id string, req dto.UpdatePaymentRequest) (*dto.PaymentResponse, error) {
	p, err := s.paymentRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	if req.PaymentStatus != nil {
		p.PaymentStatus = types.PaymentStatus(*req.PaymentStatus)
	}
	if req.Metadata != nil {
		p.Metadata = *req.Metadata
	}

	if err := s.paymentRepo.Update(ctx, p); err != nil {
		return nil, err
	}

	return dto.NewPaymentResponse(p), nil
}

// ListPayments lists payments based on filter
func (s *paymentService) ListPayments(ctx context.Context, filter *types.PaymentFilter) (*dto.ListPaymentsResponse, error) {
	payments, err := s.paymentRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	count, err := s.paymentRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	items := make([]*dto.PaymentResponse, len(payments))
	for i, p := range payments {
		items[i] = dto.NewPaymentResponse(p)
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
	return s.paymentRepo.Delete(ctx, id)
}
