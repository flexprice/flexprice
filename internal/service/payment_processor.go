package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type PaymentProcessorService interface {
	ProcessPayment(ctx context.Context, id string) (*payment.Payment, error)
}

type paymentProcessor struct {
	paymentRepo payment.Repository
	invoiceRepo invoice.Repository
	walletRepo  wallet.Repository
	logger      *logger.Logger
	db          postgres.IClient
}

func NewPaymentProcessorService(
	paymentRepo payment.Repository,
	invoiceRepo invoice.Repository,
	walletRepo wallet.Repository,
	db postgres.IClient,
	logger *logger.Logger,
) PaymentProcessorService {
	return &paymentProcessor{
		paymentRepo: paymentRepo,
		invoiceRepo: invoiceRepo,
		walletRepo:  walletRepo,
		logger:      logger,
		db:          db,
	}
}

func (p *paymentProcessor) ProcessPayment(ctx context.Context, id string) (*payment.Payment, error) {
	paymentObj, err := p.paymentRepo.Get(ctx, id)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeNotFound, "payment not found")
	}

	// If payment is not in pending state, return error
	if paymentObj.PaymentStatus != types.PaymentStatusPending {
		return paymentObj, errors.New(errors.ErrCodeInvalidOperation, fmt.Sprintf("payment is not in pending state: %s", paymentObj.PaymentStatus))
	}

	// Create a new payment attempt if tracking is enabled
	var attempt *payment.PaymentAttempt
	if paymentObj.TrackAttempts {
		attempt, err = p.createNewAttempt(ctx, paymentObj)
		if err != nil {
			return paymentObj, errors.Wrap(err, errors.ErrCodeInvalidOperation, "failed to create payment attempt")
		}
	}

	// Update payment status to processing
	paymentObj.PaymentStatus = types.PaymentStatusProcessing
	paymentObj.UpdatedAt = time.Now().UTC()
	if err := p.paymentRepo.Update(ctx, paymentObj); err != nil {
		return paymentObj, errors.Wrap(err, errors.ErrCodeInvalidOperation, "failed to update payment status to processing")
	}

	// Process payment based on payment method type
	var processErr error
	switch paymentObj.PaymentMethodType {
	case types.PaymentMethodTypeOffline:
		// For offline payments, we just mark them as succeeded immediately
		processErr = nil
	case types.PaymentMethodTypeCredits:
		processErr = p.handleCreditsPayment(ctx, paymentObj)
	case types.PaymentMethodTypeCard:
		// TODO: Implement card payment processing
		processErr = errors.New(errors.ErrCodeInvalidOperation, "card payment processing not implemented")
	case types.PaymentMethodTypeACH:
		// TODO: Implement ACH payment processing
		processErr = errors.New(errors.ErrCodeInvalidOperation, "ACH payment processing not implemented")
	default:
		processErr = errors.New(errors.ErrCodeInvalidOperation, fmt.Sprintf("unsupported payment method type: %s", paymentObj.PaymentMethodType))
	}

	// Update attempt status if tracking is enabled
	if paymentObj.TrackAttempts && attempt != nil {
		if processErr != nil {
			attempt.PaymentStatus = types.PaymentStatusFailed
			attempt.ErrorMessage = lo.ToPtr(processErr.Error())
		} else {
			attempt.PaymentStatus = types.PaymentStatusSucceeded
		}
		attempt.UpdatedAt = time.Now().UTC()
		if err := p.paymentRepo.UpdateAttempt(ctx, attempt); err != nil {
			p.logger.Errorw("failed to update payment attempt", "error", err)
		}
	}

	// Update payment status based on processing result
	if processErr != nil {
		paymentObj.PaymentStatus = types.PaymentStatusFailed
		errMsg := processErr.Error()
		paymentObj.ErrorMessage = lo.ToPtr(errMsg)
		failedAt := time.Now().UTC()
		paymentObj.FailedAt = &failedAt
	} else {
		paymentObj.PaymentStatus = types.PaymentStatusSucceeded
		succeededAt := time.Now().UTC()
		paymentObj.SucceededAt = &succeededAt
	}

	paymentObj.UpdatedAt = time.Now().UTC()
	if err := p.paymentRepo.Update(ctx, paymentObj); err != nil {
		return paymentObj, errors.Wrap(err, errors.ErrCodeInvalidOperation, "failed to update payment status")
	}

	// If payment succeeded, handle post-processing
	if paymentObj.PaymentStatus == types.PaymentStatusSucceeded {
		if err := p.handlePostProcessing(ctx, paymentObj); err != nil {
			p.logger.Errorw("failed to handle post-processing", "error", err, "payment_id", paymentObj.ID)
			// Note: We don't return this error as the payment itself was successful
		}
	}

	if processErr != nil {
		return paymentObj, errors.Wrap(processErr, errors.ErrCodeInvalidOperation, "failed to process payment")
	}

	return paymentObj, nil
}

func (p *paymentProcessor) handleCreditsPayment(ctx context.Context, paymentObj *payment.Payment) error {
	// In case of credits, we need to find the wallet by the set payment method id
	selectedWallet, err := p.walletRepo.GetWalletByID(ctx, paymentObj.PaymentMethodID)
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeNotFound, "wallet not found")
	}

	if selectedWallet.WalletStatus != types.WalletStatusActive {
		return errors.New(errors.ErrCodeInvalidOperation, "wallet is not active")
	}

	if !types.IsMatchingCurrency(selectedWallet.Currency, paymentObj.Currency) {
		return errors.New(errors.ErrCodeInvalidOperation, "wallet currency does not match payment currency")
	}

	if selectedWallet.Balance.LessThan(paymentObj.Amount) {
		return errors.New(errors.ErrCodeInvalidOperation, "wallet balance is less than payment amount")
	}

	// Update payment method ID with selected wallet
	paymentObj.PaymentMethodID = selectedWallet.ID

	// Create wallet operation
	operation := &wallet.WalletOperation{
		WalletID:      selectedWallet.ID,
		Type:          types.TransactionTypeDebit,
		Amount:        paymentObj.Amount,
		ReferenceType: "PAYMENT",
		ReferenceID:   paymentObj.ID,
		Description:   fmt.Sprintf("Payment for invoice %s", paymentObj.DestinationID),
		Metadata: types.Metadata{
			"payment_id":     paymentObj.ID,
			"invoice_id":     paymentObj.DestinationID,
			"payment_method": string(paymentObj.PaymentMethodType),
		},
	}

	// Transactional workflow begins here
	err = p.db.WithTx(ctx, func(ctx context.Context) error {
		// Debit wallet
		if err := p.walletRepo.DebitWallet(ctx, operation); err != nil {
			return errors.Wrap(err, errors.ErrCodeSystemError, "failed to debit wallet")
		}

		// Update payment with wallet ID
		if err := p.paymentRepo.Update(ctx, paymentObj); err != nil {
			return errors.Wrap(err, errors.ErrCodeSystemError, "failed to update payment")
		}

		return nil
	})

	if err != nil {
		return errors.Wrap(err, errors.ErrCodeSystemError, "failed to process payment")
	}

	return nil
}

func (p *paymentProcessor) handlePostProcessing(ctx context.Context, paymentObj *payment.Payment) error {
	switch paymentObj.DestinationType {
	case types.PaymentDestinationTypeInvoice:
		return p.handleInvoicePostProcessing(ctx, paymentObj)
	default:
		return errors.New(errors.ErrCodeInvalidOperation, fmt.Sprintf("unsupported destination type: %s", paymentObj.DestinationType))
	}
}

func (p *paymentProcessor) handleInvoicePostProcessing(ctx context.Context, paymentObj *payment.Payment) error {
	// Get the invoice
	invoice, err := p.invoiceRepo.Get(ctx, paymentObj.DestinationID)
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeNotFound, "invoice not found")
	}

	// Get all successful payments for this invoice
	filter := &types.PaymentFilter{
		DestinationType: lo.ToPtr(string(types.PaymentDestinationTypeInvoice)),
		DestinationID:   lo.ToPtr(invoice.ID),
		PaymentStatus:   lo.ToPtr(string(types.PaymentStatusSucceeded)),
	}
	payments, err := p.paymentRepo.List(ctx, filter)
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeInvalidOperation, "failed to list payments")
	}

	// Calculate total paid amount
	totalPaid := decimal.Zero
	for _, p := range payments {
		if p.Currency != invoice.Currency {
			return errors.New(errors.ErrCodeValidation, fmt.Sprintf("payment currency (%s) does not match invoice currency (%s)", p.Currency, invoice.Currency))
		}
		totalPaid = totalPaid.Add(p.Amount)
	}

	// Update invoice payment status and amounts
	invoice.AmountPaid = totalPaid
	invoice.AmountRemaining = invoice.AmountDue.Sub(totalPaid)
	if invoice.AmountRemaining.IsZero() {
		paidAt := time.Now().UTC()
		invoice.PaidAt = &paidAt
	}

	// Ensure amount remaining is not less than zero even in case of excess payments.
	// TODO: think of a better way to handle Partial payments
	if invoice.AmountRemaining.LessThan(decimal.Zero) {
		invoice.AmountRemaining = decimal.Zero
	}

	if invoice.AmountRemaining.IsZero() {
		invoice.PaymentStatus = types.PaymentStatusSucceeded
	} else if invoice.AmountRemaining.LessThan(invoice.AmountDue) {
		invoice.PaymentStatus = types.PaymentStatusPending // Partial payment still keeps it pending
	}

	// Update the invoice
	if err := p.invoiceRepo.Update(ctx, invoice); err != nil {
		return errors.Wrap(err, errors.ErrCodeInvalidOperation, "failed to update invoice")
	}

	return nil
}

func (p *paymentProcessor) createNewAttempt(ctx context.Context, paymentObj *payment.Payment) (*payment.PaymentAttempt, error) {
	// Get latest attempt to determine attempt number
	latestAttempt, err := p.paymentRepo.GetLatestAttempt(ctx, paymentObj.ID)
	if err != nil && !errors.IsNotFound(err) {
		return nil, fmt.Errorf("failed to get latest attempt: %w", err)
	}

	attemptNumber := 1
	if latestAttempt != nil {
		attemptNumber = latestAttempt.AttemptNumber + 1
	}

	attempt := &payment.PaymentAttempt{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PAYMENT_ATTEMPT),
		PaymentID:     paymentObj.ID,
		AttemptNumber: attemptNumber,
		PaymentStatus: types.PaymentStatusProcessing,
		Metadata:      types.Metadata{},
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}

	if err := p.paymentRepo.CreateAttempt(ctx, attempt); err != nil {
		return nil, fmt.Errorf("failed to create payment attempt: %w", err)
	}

	return attempt, nil
}
