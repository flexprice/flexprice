package enterprise

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/idempotency"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// WalletService defines the enterprise wallet service (prepaid balance).
// Only enterprise methods: CreateWallet, GetWalletsByCustomerID, GetWalletByID,
// TopUpWallet, GetWalletBalance, GetCustomerWallets.
type WalletService interface {
	CreateWallet(ctx context.Context, req *dto.CreateWalletRequest) (*dto.WalletResponse, error)
	GetWalletsByCustomerID(ctx context.Context, customerID string) ([]*dto.WalletResponse, error)
	GetWalletByID(ctx context.Context, id string) (*dto.WalletResponse, error)
	TopUpWallet(ctx context.Context, walletID string, req *dto.TopUpWalletRequest) (*dto.TopUpWalletResponse, error)
	GetWalletBalance(ctx context.Context, walletID string) (*dto.WalletBalanceResponse, error)
	GetCustomerWallets(ctx context.Context, req *dto.GetCustomerWalletsRequest) ([]*dto.WalletBalanceResponse, error)
}

type walletService struct {
	EnterpriseParams
	idempGen *idempotency.Generator
}

// NewWalletService creates a new enterprise wallet service.
func NewWalletService(p EnterpriseParams) WalletService {
	return &walletService{
		EnterpriseParams: p,
		idempGen:         idempotency.NewGenerator(),
	}
}

func (s *walletService) CreateWallet(ctx context.Context, req *dto.CreateWalletRequest) (*dto.WalletResponse, error) {
	response := &dto.WalletResponse{}

	if req.PriceUnit != nil {
		pu, err := s.PriceUnitRepo.GetByCode(ctx, *req.PriceUnit)
		if err != nil {
			return nil, err
		}

		if pu.Status != types.StatusPublished {
			return nil, ierr.NewError("price unit must be active").
				WithHint("The specified price unit is inactive").
				WithReportableDetails(map[string]interface{}{
					"price_unit": *req.PriceUnit,
					"status":     pu.Status,
				}).
				Mark(ierr.ErrValidation)
		}

		req.Currency = pu.BaseCurrency
		req.ConversionRate = pu.ConversionRate
	}

	if err := req.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid wallet request").
			Mark(ierr.ErrValidation)
	}

	if req.CustomerID == "" {
		customer, err := s.CustomerRepo.GetByLookupKey(ctx, req.ExternalCustomerID)
		if err != nil {
			return nil, err
		}
		req.CustomerID = customer.ID
	}

	existingWallets, err := s.WalletRepo.GetWalletsByCustomerID(ctx, req.CustomerID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to check existing wallets").
			WithReportableDetails(map[string]interface{}{
				"customer_id": req.CustomerID,
			}).
			Mark(ierr.ErrDatabase)
	}

	for _, w := range existingWallets {
		if w.WalletStatus == types.WalletStatusActive && w.Currency == req.Currency && w.WalletType == req.WalletType {
			return nil, ierr.NewError("customer already has an active wallet with the same currency and wallet type").
				WithHint("A customer can only have one active wallet per currency and wallet type").
				WithReportableDetails(map[string]interface{}{
					"customer_id": req.CustomerID,
					"wallet_id":   w.ID,
					"currency":    req.Currency,
					"wallet_type": req.WalletType,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
	}

	w := req.ToWallet(ctx)

	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		if err := s.WalletRepo.CreateWallet(ctx, w); err != nil {
			return err
		}
		response = dto.FromWallet(w)

		s.Logger.Debugw("created wallet",
			"wallet_id", w.ID,
			"customer_id", w.CustomerID,
			"currency", w.Currency,
			"conversion_rate", w.ConversionRate,
		)

		if req.InitialCreditsToLoad.GreaterThan(decimal.Zero) {
			idempotencyKey := s.idempGen.GenerateKey(idempotency.ScopeCreditGrant, map[string]interface{}{
				"wallet_id":          w.ID,
				"credits_to_add":     req.InitialCreditsToLoad,
				"transaction_reason": types.TransactionReasonFreeCredit,
				"timestamp":          time.Now().UTC().Format(time.RFC3339),
			})
			topUpResp, err := s.TopUpWallet(ctx, w.ID, &dto.TopUpWalletRequest{
				CreditsToAdd:      req.InitialCreditsToLoad,
				TransactionReason: types.TransactionReasonFreeCredit,
				ExpiryDate:        req.InitialCreditsToLoadExpiryDate,
				ExpiryDateUTC:     req.InitialCreditsExpiryDateUTC,
				IdempotencyKey:    &idempotencyKey,
			})

			if err != nil {
				return err
			}
			response = topUpResp.Wallet
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	s.publishInternalWalletWebhookEvent(ctx, types.WebhookEventWalletCreated, w.ID)

	return response, nil
}

func (s *walletService) GetWalletsByCustomerID(ctx context.Context, customerID string) ([]*dto.WalletResponse, error) {
	if customerID == "" {
		return nil, ierr.NewError("customer_id is required").
			WithHint("Customer ID is required").
			Mark(ierr.ErrValidation)
	}

	wallets, err := s.WalletRepo.GetWalletsByCustomerID(ctx, customerID)
	if err != nil {
		return nil, err
	}

	response := make([]*dto.WalletResponse, len(wallets))
	for i, w := range wallets {
		response[i] = dto.FromWallet(w)
	}

	return response, nil
}

func (s *walletService) GetWalletByID(ctx context.Context, id string) (*dto.WalletResponse, error) {
	if id == "" {
		return nil, ierr.NewError("wallet_id is required").
			WithHint("Wallet ID is required").
			Mark(ierr.ErrValidation)
	}

	w, err := s.WalletRepo.GetWalletByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return dto.FromWallet(w), nil
}

func (s *walletService) TopUpWallet(ctx context.Context, walletID string, req *dto.TopUpWalletRequest) (*dto.TopUpWalletResponse, error) {
	w, err := s.WalletRepo.GetWalletByID(ctx, walletID)
	if err != nil {
		return nil, ierr.NewError("Wallet not found").
			WithHint("Wallet not found").
			Mark(ierr.ErrNotFound)
	}

	if req.CreditsToAdd.IsZero() && !req.Amount.IsZero() {
		req.CreditsToAdd = s.getCreditsFromCurrencyAmount(req.Amount, w.TopupConversionRate)
	}

	if req.ExpiryDateUTC != nil && req.ExpiryDate == nil {
		expiryDate := req.ExpiryDateUTC.UTC()
		parsedDate, err := strconv.Atoi(expiryDate.Format("20060102"))
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("Invalid expiry date").
				Mark(ierr.ErrValidation)
		}
		req.ExpiryDate = &parsedDate
	}

	if err := req.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid top up wallet request").
			Mark(ierr.ErrValidation)
	}

	var idempotencyKey string
	if lo.FromPtr(req.IdempotencyKey) != "" {
		idempotencyKey = lo.FromPtr(req.IdempotencyKey)
	} else {
		idempotencyKey = s.idempGen.GenerateKey(idempotency.ScopeWalletTopUp, map[string]interface{}{
			"wallet_id":          walletID,
			"credits_to_add":     req.CreditsToAdd,
			"transaction_reason": req.TransactionReason,
			"timestamp":          time.Now().UTC().Format(time.RFC3339),
		})
	}

	if req.TransactionReason == types.TransactionReasonPurchasedCreditInvoiced {
		walletTransactionID, invoiceID, err := s.handlePurchasedCreditInvoicedTransaction(
			ctx,
			walletID,
			lo.ToPtr(idempotencyKey),
			req,
		)
		if err != nil {
			return nil, err
		}

		s.Logger.Debugw("created pending credit purchase with invoice",
			"wallet_id", walletID,
			"wallet_transaction_id", walletTransactionID,
			"invoice_id", invoiceID,
			"credits", req.CreditsToAdd.String(),
		)

		tx, err := s.WalletRepo.GetTransactionByID(ctx, walletTransactionID)
		if err != nil {
			return nil, err
		}

		walletResp, err := s.GetWalletByID(ctx, walletID)
		if err != nil {
			return nil, err
		}

		return &dto.TopUpWalletResponse{
			WalletTransaction: dto.FromWalletTransaction(tx),
			InvoiceID:         &invoiceID,
			Wallet:            walletResp,
		}, nil
	}

	referenceType := types.WalletTxReferenceTypeExternal
	referenceID := idempotencyKey

	creditReq := &wallet.WalletOperation{
		WalletID:          walletID,
		Type:              types.TransactionTypeCredit,
		CreditAmount:      req.CreditsToAdd,
		Description:       req.Description,
		Metadata:          req.Metadata,
		TransactionReason: req.TransactionReason,
		ReferenceType:     referenceType,
		ReferenceID:       referenceID,
		ExpiryDate:        req.ExpiryDate,
		IdempotencyKey:    idempotencyKey,
		Priority:          req.Priority,
	}

	err = s.processWalletOperation(ctx, creditReq)
	if err != nil {
		return nil, err
	}

	tx, err := s.WalletRepo.GetTransactionByIdempotencyKey(ctx, idempotencyKey)
	if err != nil {
		return nil, err
	}

	walletResp, err := s.GetWalletByID(ctx, walletID)
	if err != nil {
		return nil, err
	}

	return &dto.TopUpWalletResponse{
		WalletTransaction: dto.FromWalletTransaction(tx),
		InvoiceID:         nil,
		Wallet:            walletResp,
	}, nil
}

func (s *walletService) handlePurchasedCreditInvoicedTransaction(ctx context.Context, walletID string, idempotencyKey *string, req *dto.TopUpWalletRequest) (string, string, error) {
	sp := s.ServiceParams
	invoiceService := service.NewInvoiceService(sp)

	autoCompleteEnabled := false //invoiceConfig.(*dto.SettingResponse).Value.(types.InvoiceConfig).AutoCompletePurchasedCreditTransaction

	s.Logger.Debugw("processing purchased credit transaction",
		"wallet_id", walletID,
		"auto_complete_enabled", autoCompleteEnabled,
		"credits", req.CreditsToAdd.String(),
	)

	w, err := s.WalletRepo.GetWalletByID(ctx, walletID)
	if err != nil {
		return "", "", err
	}

	var walletTransactionID string
	var invoiceID string
	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		txStatus := types.TransactionStatusPending
		balanceAfter := w.CreditBalance
		var description string

		if autoCompleteEnabled {
			txStatus = types.TransactionStatusCompleted
			balanceAfter = w.CreditBalance.Add(req.CreditsToAdd)
			description = lo.Ternary(req.Description != "", req.Description, "Purchased credits - auto-completed")
		} else {
			description = lo.Ternary(req.Description != "", req.Description, "Purchased credits - pending payment")
		}

		txMetadata := req.Metadata
		if txMetadata == nil {
			txMetadata = types.Metadata{}
		}

		tx := &wallet.Transaction{
			ID:                  types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
			WalletID:            walletID,
			CustomerID:          w.CustomerID,
			Type:                types.TransactionTypeCredit,
			CreditAmount:        req.CreditsToAdd,
			Amount:              s.getCurrencyAmountFromCredits(req.CreditsToAdd, w.TopupConversionRate),
			TxStatus:            txStatus,
			ReferenceType:       types.WalletTxReferenceTypeExternal,
			ReferenceID:         lo.FromPtr(idempotencyKey),
			Description:         description,
			Metadata:            txMetadata,
			TransactionReason:   types.TransactionReasonPurchasedCreditInvoiced,
			Priority:            req.Priority,
			IdempotencyKey:      lo.FromPtr(idempotencyKey),
			EnvironmentID:       w.EnvironmentID,
			CreditBalanceBefore: w.CreditBalance,
			CreditBalanceAfter:  balanceAfter,
			Currency:            w.Currency,
			TopupConversionRate: lo.ToPtr(w.TopupConversionRate),
			ExpiryDate:          types.ParseYYYYMMDDToDate(req.ExpiryDate),
			BaseModel:           types.GetDefaultBaseModel(ctx),
		}

		tx.CreditsAvailable, err = tx.ComputeCreditsAvailable()
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to compute credits available").
				Mark(ierr.ErrInternal)
		}

		if err := s.WalletRepo.CreateTransaction(ctx, tx); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to create wallet transaction").
				Mark(ierr.ErrInternal)
		}

		if autoCompleteEnabled {
			finalBalance := w.Balance.Add(tx.Amount)
			if err := s.WalletRepo.UpdateWalletBalance(ctx, walletID, finalBalance, balanceAfter); err != nil {
				return ierr.WithError(err).
					WithHint("Failed to update wallet balance").
					Mark(ierr.ErrInternal)
			}

			s.Logger.Infow("auto-completed wallet credit transaction",
				"wallet_transaction_id", tx.ID,
				"wallet_id", walletID,
				"credits_added", req.CreditsToAdd.String(),
				"new_credit_balance", balanceAfter.String(),
			)
		}

		walletTransactionID = tx.ID

		amount := s.getCurrencyAmountFromCredits(req.CreditsToAdd, w.TopupConversionRate)
		invoiceMetadata := make(types.Metadata)

		if req.Metadata != nil {
			for key, value := range req.Metadata {
				invoiceMetadata[key] = value
			}
		}

		invoiceMetadata["auto_topup"] = lo.Ternary(req.Metadata != nil && req.Metadata["auto_topup"] == "true", "true", invoiceMetadata["auto_topup"])
		invoiceMetadata["wallet_transaction_id"] = walletTransactionID
		invoiceMetadata["wallet_id"] = walletID
		invoiceMetadata["credits_amount"] = req.CreditsToAdd.String()
		invoiceMetadata["auto_completed"] = fmt.Sprintf("%v", autoCompleteEnabled)

		if req.Description != "" {
			invoiceMetadata["description"] = req.Description
		}

		paymentStatus := types.PaymentStatusPending
		var amountPaid *decimal.Decimal
		if autoCompleteEnabled {
			paymentStatus = types.PaymentStatusSucceeded
			amountPaid = &amount
		}

		invoice, err := invoiceService.CreateInvoice(ctx, dto.CreateInvoiceRequest{
			CustomerID:     w.CustomerID,
			AmountDue:      amount,
			AmountPaid:     amountPaid,
			Subtotal:       amount,
			Total:          amount,
			Currency:       w.Currency,
			InvoiceType:    types.InvoiceTypeOneOff,
			DueDate:        lo.ToPtr(time.Now().UTC()),
			IdempotencyKey: idempotencyKey,
			InvoiceStatus:  lo.ToPtr(types.InvoiceStatusFinalized),
			LineItems: []dto.CreateInvoiceLineItemRequest{
				{
					Amount:      amount,
					Quantity:    decimal.NewFromInt(1),
					DisplayName: lo.ToPtr(fmt.Sprintf("Purchase %s Credits", req.CreditsToAdd.String())),
				},
			},
			PaymentStatus: lo.ToPtr(paymentStatus),
			Metadata:      invoiceMetadata,
		})
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to create invoice for purchased credits").
				Mark(ierr.ErrInternal)
		}

		invoiceID = invoice.ID

		if autoCompleteEnabled {
			s.Logger.Infow("created auto-completed credit purchase",
				"wallet_transaction_id", walletTransactionID,
				"invoice_id", invoice.ID,
				"wallet_id", walletID,
				"credits", req.CreditsToAdd.String(),
				"amount", amount.String(),
				"payment_status", paymentStatus,
			)
		} else {
			s.Logger.Infow("created pending credit purchase",
				"wallet_transaction_id", walletTransactionID,
				"invoice_id", invoice.ID,
				"wallet_id", walletID,
				"credits", req.CreditsToAdd.String(),
				"amount", amount.String(),
			)
		}

		return nil
	})

	if err != nil {
		return "", "", err
	}

	if autoCompleteEnabled {
		s.publishInternalTransactionWebhookEvent(ctx, types.WebhookEventWalletTransactionCreated, walletTransactionID)
	}

	return walletTransactionID, invoiceID, err
}

func (s *walletService) validateWalletOperation(w *wallet.Wallet, req *wallet.WalletOperation) error {
	if err := req.Validate(); err != nil {
		return err
	}

	conversionRate := w.ConversionRate
	if req.Type == types.TransactionTypeCredit {
		conversionRate = w.TopupConversionRate
	}

	switch {
	case req.Amount.GreaterThan(decimal.Zero):
		req.CreditAmount = s.getCreditsFromCurrencyAmount(req.Amount, conversionRate)

	case req.CreditAmount.GreaterThan(decimal.Zero):
		req.Amount = s.getCurrencyAmountFromCredits(req.CreditAmount, conversionRate)

	default:
		return ierr.NewError("amount or credit_amount is required").
			WithHint("Amount or credit amount is required").
			Mark(ierr.ErrValidation)
	}

	if req.CreditAmount.LessThanOrEqual(decimal.Zero) {
		return ierr.NewError("wallet transaction amount must be greater than 0").
			WithHint("Wallet transaction amount must be greater than 0").
			Mark(ierr.ErrValidation)
	}

	return nil
}

func (s *walletService) processDebitOperation(ctx context.Context, req *wallet.WalletOperation) error {
	credits := []*wallet.Transaction{}
	var err error
	if req.ParentCreditTxID != "" {
		parentCreditTx, err := s.WalletRepo.GetTransactionByID(ctx, req.ParentCreditTxID)
		if err != nil {
			return err
		}
		credits = append(credits, parentCreditTx)

	} else {
		timeReference := time.Now().UTC()
		if req.InvoiceID != nil && *req.InvoiceID != "" {
			invoice, err := s.InvoiceRepo.Get(ctx, *req.InvoiceID)
			if err != nil {
				return err
			}
			if invoice.PeriodEnd != nil {
				timeReference = lo.FromPtr(invoice.PeriodEnd)
			}
		}
		credits, err = s.WalletRepo.FindEligibleCredits(ctx, req.WalletID, req.CreditAmount, 100, timeReference)
		if err != nil {
			return err
		}
	}

	var totalAvailable decimal.Decimal
	for _, c := range credits {
		totalAvailable = totalAvailable.Add(c.CreditsAvailable)
		if totalAvailable.GreaterThanOrEqual(req.CreditAmount) {
			break
		}
	}

	if totalAvailable.LessThan(req.CreditAmount) {
		if req.TransactionReason != types.TransactionReasonManualBalanceDebit {
			return ierr.NewError("insufficient balance").
				WithHint("Insufficient balance to process debit operation").
				WithReportableDetails(map[string]interface{}{
					"wallet_id": req.WalletID,
					"amount":    req.CreditAmount,
				}).
				Mark(ierr.ErrInvalidOperation)
		}
	}

	if err := s.WalletRepo.ConsumeCredits(ctx, credits, req.CreditAmount); err != nil {
		return err
	}

	return nil
}

func (s *walletService) processWalletOperation(ctx context.Context, req *wallet.WalletOperation) error {
	s.Logger.Debugw("Processing wallet operation", "req", req)

	var w *wallet.Wallet
	var tx *wallet.Transaction
	var newCreditBalance decimal.Decimal
	var finalBalance decimal.Decimal

	err := s.DB.WithTx(ctx, func(ctx context.Context) error {
		if err := s.DB.LockWithWait(ctx, postgres.LockRequest{Key: req.WalletID}); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to acquire wallet lock").
				Mark(ierr.ErrInternal)
		}

		var err error
		w, err = s.WalletRepo.GetWalletByID(ctx, req.WalletID)
		if err != nil {
			return err
		}

		if err := s.validateWalletOperation(w, req); err != nil {
			return err
		}

		if req.Type == types.TransactionTypeDebit {
			newCreditBalance = w.CreditBalance.Sub(req.CreditAmount)
			if err := s.processDebitOperation(ctx, req); err != nil {
				return err
			}
		} else {
			newCreditBalance = w.CreditBalance.Add(req.CreditAmount)
		}

		finalBalance = s.getCurrencyAmountFromCredits(newCreditBalance, w.ConversionRate)

		tx = &wallet.Transaction{
			ID:                  types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_TRANSACTION),
			WalletID:            req.WalletID,
			CustomerID:          w.CustomerID,
			Type:                req.Type,
			Amount:              req.Amount,
			CreditAmount:        req.CreditAmount,
			ReferenceType:       req.ReferenceType,
			ReferenceID:         req.ReferenceID,
			Description:         req.Description,
			Metadata:            req.Metadata,
			TxStatus:            types.TransactionStatusCompleted,
			TransactionReason:   req.TransactionReason,
			ExpiryDate:          types.ParseYYYYMMDDToDate(req.ExpiryDate),
			Priority:            req.Priority,
			CreditBalanceBefore: w.CreditBalance,
			CreditBalanceAfter:  newCreditBalance,
			Currency:            w.Currency,
			EnvironmentID:       types.GetEnvironmentID(ctx),
			IdempotencyKey:      req.IdempotencyKey,
			BaseModel:           types.GetDefaultBaseModel(ctx),
		}

		tx.CreditsAvailable, err = tx.ComputeCreditsAvailable()
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to compute credits available").
				Mark(ierr.ErrInternal)
		}

		if req.Type == types.TransactionTypeCredit {
			tx.TopupConversionRate = lo.ToPtr(w.TopupConversionRate)
			if req.ExpiryDate != nil {
				tx.ExpiryDate = types.ParseYYYYMMDDToDate(req.ExpiryDate)
			}
		} else if req.Type == types.TransactionTypeDebit {
			tx.ConversionRate = lo.ToPtr(w.ConversionRate)
		}

		if err := s.WalletRepo.CreateTransaction(ctx, tx); err != nil {
			return err
		}

		if err := s.WalletRepo.UpdateWalletBalance(ctx, req.WalletID, finalBalance, newCreditBalance); err != nil {
			return err
		}

		s.Logger.Debugw("Wallet operation completed")
		return nil
	})
	if err != nil {
		return err
	}

	s.publishInternalTransactionWebhookEvent(ctx, types.WebhookEventWalletTransactionCreated, tx.ID)

	walletBalanceAlertService := service.NewWalletBalanceAlertService(s.ServiceParams)
	event := &wallet.WalletBalanceAlertEvent{
		ID:                    types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET_ALERT),
		Timestamp:             time.Now().UTC(),
		Source:                service.EventSourceWalletTransaction,
		CustomerID:            w.CustomerID,
		ForceCalculateBalance: true,
		TenantID:              types.GetTenantID(ctx),
		EnvironmentID:         types.GetEnvironmentID(ctx),
		WalletID:              req.WalletID,
	}
	if err := walletBalanceAlertService.PublishEvent(ctx, event); err != nil {
		s.Logger.Errorw("failed to publish wallet balance alert event",
			"error", err,
			"customer_id", w.CustomerID,
			"wallet_id", req.WalletID,
		)
	}

	if err := s.logCreditBalanceAlert(ctx, w, newCreditBalance); err != nil {
		s.Logger.Errorw("failed to log credit balance alert after wallet operation",
			"error", err,
			"wallet_id", w.ID,
		)
	}

	return nil
}

func (s *walletService) logCreditBalanceAlert(ctx context.Context, w *wallet.Wallet, newCreditBalance decimal.Decimal) error {
	var thresholdValue decimal.Decimal
	var alertStatus types.AlertState

	if newCreditBalance.LessThanOrEqual(thresholdValue) {
		alertStatus = types.AlertStateInAlarm
	} else {
		alertStatus = types.AlertStateOk
	}

	alertInfo := types.AlertInfo{
		AlertSettings: &types.AlertSettings{
			Critical: &types.AlertThreshold{
				Threshold: thresholdValue,
				Condition: types.AlertConditionBelow,
			},
			AlertEnabled: lo.ToPtr(true),
		},
		ValueAtTime: newCreditBalance,
		Timestamp:   time.Now().UTC(),
	}

	alertService := service.NewAlertLogsService(s.ServiceParams)
	var customerID *string
	if w.CustomerID != "" {
		customerID = lo.ToPtr(w.CustomerID)
	}

	logAlertReq := &service.LogAlertRequest{
		EntityType:  types.AlertEntityTypeWallet,
		EntityID:    w.ID,
		CustomerID:  customerID,
		AlertType:   types.AlertTypeLowCreditBalance,
		AlertStatus: alertStatus,
		AlertInfo:   alertInfo,
	}

	if err := alertService.LogAlert(ctx, logAlertReq); err != nil {
		s.Logger.Errorw("failed to log credit balance alert",
			"error", err,
			"wallet_id", w.ID,
			"new_credit_balance", newCreditBalance,
			"threshold", thresholdValue,
			"alert_status", alertStatus,
		)
		return err
	}

	s.Logger.Infow("credit balance alert logged successfully",
		"wallet_id", w.ID,
		"new_credit_balance", newCreditBalance,
		"threshold", thresholdValue,
		"alert_status", alertStatus,
	)
	return nil
}

func (s *walletService) GetWalletBalance(ctx context.Context, walletID string) (*dto.WalletBalanceResponse, error) {
	if walletID == "" {
		return nil, ierr.NewError("wallet_id is required").
			WithHint("Wallet ID is required").
			Mark(ierr.ErrValidation)
	}

	w, err := s.WalletRepo.GetWalletByID(ctx, walletID)
	if err != nil {
		return nil, err
	}

	if w.WalletStatus != types.WalletStatusActive {
		return &dto.WalletBalanceResponse{
			Wallet:                w,
			RealTimeBalance:       lo.ToPtr(decimal.Zero),
			RealTimeCreditBalance: lo.ToPtr(decimal.Zero),
			BalanceUpdatedAt:      lo.ToPtr(w.UpdatedAt),
			CurrentPeriodUsage:    lo.ToPtr(decimal.Zero),
		}, nil
	}

	shouldIncludeUsage := len(w.Config.AllowedPriceTypes) == 0 ||
		lo.Contains(w.Config.AllowedPriceTypes, types.WalletConfigPriceTypeUsage) ||
		lo.Contains(w.Config.AllowedPriceTypes, types.WalletConfigPriceTypeAll)

	currentPeriodUsage := decimal.Zero
	totalPendingCharges := decimal.Zero

	if shouldIncludeUsage {
		sp := s.ServiceParams
		subscriptionService := service.NewSubscriptionService(sp)
		subscriptions, err := subscriptionService.ListByCustomerID(ctx, w.CustomerID)
		if err != nil {
			return nil, err
		}

		filteredSubscriptions := make([]*subscription.Subscription, 0)
		for _, sub := range subscriptions {
			if sub.Currency == w.Currency {
				filteredSubscriptions = append(filteredSubscriptions, sub)
				s.Logger.Infow("found matching subscription",
					"subscription_id", sub.ID,
					"currency", sub.Currency,
					"period_start", sub.CurrentPeriodStart,
					"period_end", sub.CurrentPeriodEnd)
			}
		}

		billingService := service.NewBillingService(sp)

		for _, sub := range filteredSubscriptions {
			periodStart := sub.CurrentPeriodStart
			periodEnd := sub.CurrentPeriodEnd

			usage, err := subscriptionService.GetUsageBySubscription(ctx, &dto.GetUsageBySubscriptionRequest{
				SubscriptionID: sub.ID,
				StartTime:      periodStart,
				EndTime:        periodEnd,
			})

			if err != nil {
				return nil, err
			}

			usageCharges, usageTotal, err := billingService.CalculateUsageCharges(ctx, sub, usage, periodStart, periodEnd)
			if err != nil {
				return nil, err
			}

			s.Logger.Infow("subscription charges details",
				"subscription_id", sub.ID,
				"usage_total", usageTotal,
				"num_usage_charges", len(usageCharges))

			currentPeriodUsage = currentPeriodUsage.Add(usageTotal)
		}
	}

	sp := s.ServiceParams
	invoiceService := service.NewInvoiceService(sp)

	resp, err := invoiceService.GetUnpaidInvoicesToBePaid(ctx, dto.GetUnpaidInvoicesToBePaidRequest{
		CustomerID: w.CustomerID,
		Currency:   w.Currency,
	})

	if err != nil {
		return nil, err
	}

	totalPendingCharges = currentPeriodUsage.Add(resp.TotalUnpaidUsageCharges)

	realTimeBalance := w.Balance.Sub(totalPendingCharges)

	s.Logger.Debugw("detailed balance calculation",
		"wallet_id", w.ID,
		"current_balance", w.Balance,
		"pending_charges", totalPendingCharges,
		"real_time_balance", realTimeBalance,
		"credit_balance", w.CreditBalance)

	realTimeCreditBalance := s.getCreditsFromCurrencyAmount(realTimeBalance, w.ConversionRate)

	return &dto.WalletBalanceResponse{
		Wallet:                w,
		RealTimeBalance:       lo.ToPtr(realTimeBalance),
		RealTimeCreditBalance: lo.ToPtr(realTimeCreditBalance),
		BalanceUpdatedAt:      lo.ToPtr(w.UpdatedAt),
		CurrentPeriodUsage:    lo.ToPtr(totalPendingCharges),
		UnpaidInvoicesAmount:  lo.ToPtr(resp.TotalUnpaidUsageCharges),
	}, nil
}

func (s *walletService) GetCustomerWallets(ctx context.Context, req *dto.GetCustomerWalletsRequest) ([]*dto.WalletBalanceResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	var customerID string
	if req.ID != "" {
		customerID = req.ID
		_, err := s.CustomerRepo.Get(ctx, customerID)
		if err != nil {
			return nil, err
		}
	} else {
		customer, err := s.CustomerRepo.GetByLookupKey(ctx, req.LookupKey)
		if err != nil {
			return nil, err
		}
		customerID = customer.ID
	}

	wallets, err := s.WalletRepo.GetWalletsByCustomerID(ctx, customerID)
	if err != nil {
		return nil, err
	}

	if len(wallets) == 0 {
		return []*dto.WalletBalanceResponse{}, nil
	}

	response := make([]*dto.WalletBalanceResponse, len(wallets))

	if req.IncludeRealTimeBalance {
		for i, w := range wallets {
			balance, err := s.GetWalletBalance(ctx, w.ID)
			if err != nil {
				return nil, err
			}
			response[i] = balance
		}
	} else {
		for i, w := range wallets {
			response[i] = &dto.WalletBalanceResponse{
				Wallet: w,
			}
		}
	}
	return response, nil
}

func (s *walletService) publishInternalWalletWebhookEvent(ctx context.Context, eventName string, walletID string) {
	webhookPayload, err := json.Marshal(webhookDto.InternalWalletEvent{
		WalletID:  walletID,
		TenantID:  types.GetTenantID(ctx),
		EventType: eventName,
	})

	if err != nil {
		s.Logger.Errorw("failed to marshal webhook payload", "error", err)
		return
	}

	webhookEvent := &types.WebhookEvent{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WEBHOOK_EVENT),
		EventName:     eventName,
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
		UserID:        types.GetUserID(ctx),
		Timestamp:     time.Now().UTC(),
		Payload:       json.RawMessage(webhookPayload),
	}
	if err := s.WebhookPublisher.PublishWebhook(ctx, webhookEvent); err != nil {
		s.Logger.Errorf("failed to publish %s event: %v", webhookEvent.EventName, err)
	}
}

func (s *walletService) publishInternalTransactionWebhookEvent(ctx context.Context, eventName string, transactionID string) {
	webhookPayload, err := json.Marshal(webhookDto.InternalTransactionEvent{
		TransactionID: transactionID,
		TenantID:      types.GetTenantID(ctx),
	})

	if err != nil {
		s.Logger.Errorw("failed to marshal webhook payload", "error", err)
		return
	}

	webhookEvent := &types.WebhookEvent{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WEBHOOK_EVENT),
		EventName:     eventName,
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
		UserID:        types.GetUserID(ctx),
		Timestamp:     time.Now().UTC(),
		Payload:       json.RawMessage(webhookPayload),
	}
	if err := s.WebhookPublisher.PublishWebhook(ctx, webhookEvent); err != nil {
		s.Logger.Errorf("failed to publish %s event: %v", webhookEvent.EventName, err)
	}
}

func (s *walletService) getCurrencyAmountFromCredits(credits decimal.Decimal, conversionRate decimal.Decimal) decimal.Decimal {
	return credits.Mul(conversionRate)
}

func (s *walletService) getCreditsFromCurrencyAmount(amount decimal.Decimal, conversionRate decimal.Decimal) decimal.Decimal {
	return amount.Div(conversionRate)
}

// GetSettingWithParams retrieves a setting by key using the given service params.
// Used by enterprise and other packages that have ServiceParams but not a settingsService instance.
func (s *walletService) GetSetting(ctx context.Context, key types.SettingKey) (*dto.SettingResponse, error) {
	sp := s.ServiceParams
	settingsService := service.NewSettingsService(sp)
	return settingsService.GetSettingByKey(ctx, key)
}
