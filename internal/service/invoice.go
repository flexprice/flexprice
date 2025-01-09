package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/idempotency"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type InvoiceService interface {
	CreateInvoice(ctx context.Context, req dto.CreateInvoiceRequest) (*dto.InvoiceResponse, error)
	GetInvoice(ctx context.Context, id string) (*dto.InvoiceResponse, error)
	ListInvoices(ctx context.Context, filter *types.InvoiceFilter) (*dto.ListInvoicesResponse, error)
	FinalizeInvoice(ctx context.Context, id string) error
	VoidInvoice(ctx context.Context, id string) error
	UpdatePaymentStatus(ctx context.Context, id string, status types.InvoicePaymentStatus, amount *decimal.Decimal) error
	CreateSubscriptionInvoice(ctx context.Context, sub *subscription.Subscription, periodStart, periodEnd time.Time, usage *dto.GetUsageBySubscriptionResponse) (*dto.InvoiceResponse, error)
}

type invoiceService struct {
	db          postgres.IClient
	logger      *logger.Logger
	invoiceRepo invoice.Repository
	priceRepo   price.Repository
	meterRepo   meter.Repository
	planRepo    plan.Repository
	idempGen    *idempotency.Generator
}

func NewInvoiceService(
	invoiceRepo invoice.Repository,
	priceRepo price.Repository,
	meterRepo meter.Repository,
	planRepo plan.Repository,
	logger *logger.Logger,
	db postgres.IClient,
) InvoiceService {
	return &invoiceService{
		db:          db,
		logger:      logger,
		invoiceRepo: invoiceRepo,
		priceRepo:   priceRepo,
		meterRepo:   meterRepo,
		planRepo:    planRepo,
		idempGen:    idempotency.NewGenerator(),
	}
}

func (s *invoiceService) CreateInvoice(ctx context.Context, req dto.CreateInvoiceRequest) (*dto.InvoiceResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	var resp *dto.InvoiceResponse

	// Start transaction
	err := s.db.WithTx(ctx, func(tx context.Context) error {
		// 1. Generate idempotency key if not provided
		var idempKey string
		if req.IdempotencyKey == nil {
			params := map[string]interface{}{
				"tenant_id":    types.GetTenantID(ctx),
				"customer_id":  req.CustomerID,
				"period_start": req.PeriodStart,
				"period_end":   req.PeriodEnd,
			}
			scope := idempotency.ScopeOneOffInvoice
			if req.SubscriptionID != nil {
				scope = idempotency.ScopeSubscriptionInvoice
				params["subscription_id"] = req.SubscriptionID
			}
			idempKey = s.idempGen.GenerateKey(scope, params)
		} else {
			idempKey = *req.IdempotencyKey
		}

		// 2. Check for existing invoice with same idempotency key
		existing, err := s.invoiceRepo.GetByIdempotencyKey(tx, idempKey)
		if err != nil && !errors.Is(err, invoice.ErrInvoiceNotFound) {
			return fmt.Errorf("failed to check idempotency: %w", err)
		}
		if existing != nil {
			s.logger.Infow("returning existing invoice for idempotency key",
				"idempotency_key", idempKey,
				"invoice_id", existing.ID)
			return nil
		}

		// 3. For subscription invoices, validate period uniqueness and get billing sequence
		var billingSeq *int
		if req.SubscriptionID != nil {
			// Check period uniqueness
			exists, err := s.invoiceRepo.ExistsForPeriod(ctx, *req.SubscriptionID, *req.PeriodStart, *req.PeriodEnd)
			if err != nil {
				return fmt.Errorf("failed to check period uniqueness: %w", err)
			}
			if exists {
				return invoice.ErrInvoiceAlreadyExists
			}

			// Get billing sequence
			seq, err := s.invoiceRepo.GetNextBillingSequence(ctx, *req.SubscriptionID)
			if err != nil {
				return fmt.Errorf("failed to get billing sequence: %w", err)
			}
			billingSeq = &seq
		}

		// 4. Generate invoice number
		invoiceNumber, err := s.invoiceRepo.GetNextInvoiceNumber(ctx)
		if err != nil {
			return fmt.Errorf("failed to generate invoice number: %w", err)
		}

		// 5. Create invoice
		// Convert request to domain model
		inv, err := req.ToInvoice(ctx)
		if err != nil {
			return fmt.Errorf("failed to convert request to invoice: %w", err)
		}

		inv.InvoiceNumber = &invoiceNumber
		inv.IdempotencyKey = &idempKey
		inv.BillingSequence = billingSeq

		// Setting default values
		if req.InvoiceType == types.InvoiceTypeOneOff {
			if req.InvoiceStatus == nil {
				inv.InvoiceStatus = types.InvoiceStatusFinalized
			}
			if req.PaymentStatus == nil {
				inv.PaymentStatus = types.InvoicePaymentStatusSucceeded
			}
		} else if req.InvoiceType == types.InvoiceTypeSubscription {
			if req.InvoiceStatus == nil {
				inv.InvoiceStatus = types.InvoiceStatusDraft
			}
			if req.PaymentStatus == nil {
				inv.PaymentStatus = types.InvoicePaymentStatusPending
			}
		}

		if req.AmountPaid == nil {
			if req.PaymentStatus == nil {
				inv.AmountPaid = inv.AmountDue
			}
		}

		// Calculated Amount Remaining
		inv.AmountRemaining = inv.AmountDue.Sub(inv.AmountPaid)

		// Validate invoice
		if err := inv.Validate(); err != nil {
			return err
		}

		// Create invoice with line items in a single transaction
		if err := s.invoiceRepo.CreateWithLineItems(ctx, inv); err != nil {
			return fmt.Errorf("failed to create invoice: %w", err)
		}

		// Convert to response
		resp = dto.NewInvoiceResponse(inv)
		return nil
	})

	if err != nil {
		s.logger.Errorw("failed to create invoice",
			"error", err,
			"customer_id", req.CustomerID,
			"subscription_id", req.SubscriptionID)
		return nil, err
	}

	return resp, nil
}

func (s *invoiceService) GetInvoice(ctx context.Context, id string) (*dto.InvoiceResponse, error) {
	inv, err := s.invoiceRepo.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get invoice: %w", err)
	}
	return dto.NewInvoiceResponse(inv), nil
}

func (s *invoiceService) ListInvoices(ctx context.Context, filter *types.InvoiceFilter) (*dto.ListInvoicesResponse, error) {
	invoices, err := s.invoiceRepo.List(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to list invoices: %w", err)
	}

	count, err := s.invoiceRepo.Count(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to count invoices: %w", err)
	}

	items := make([]*dto.InvoiceResponse, len(invoices))
	for i, inv := range invoices {
		items[i] = dto.NewInvoiceResponse(inv)
	}

	return &dto.ListInvoicesResponse{
		Items: items,
		Total: count,
	}, nil
}

func (s *invoiceService) FinalizeInvoice(ctx context.Context, id string) error {
	inv, err := s.invoiceRepo.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get invoice: %w", err)
	}

	if inv.InvoiceStatus != types.InvoiceStatusDraft {
		return fmt.Errorf("invoice is not in draft status")
	}

	now := time.Now().UTC()
	inv.InvoiceStatus = types.InvoiceStatusFinalized
	inv.FinalizedAt = &now

	if err := s.invoiceRepo.Update(ctx, inv); err != nil {
		return fmt.Errorf("failed to update invoice: %w", err)
	}

	// TODO: add publisher event for invoice finalized

	return nil
}

func (s *invoiceService) VoidInvoice(ctx context.Context, id string) error {
	inv, err := s.invoiceRepo.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get invoice: %w", err)
	}

	allowedInvoiceStatuses := []types.InvoiceStatus{
		types.InvoiceStatusDraft,
		types.InvoiceStatusFinalized,
	}
	if !lo.Contains(allowedInvoiceStatuses, inv.InvoiceStatus) {
		return fmt.Errorf("invoice status - %s is not allowed", inv.InvoiceStatus)
	}

	now := time.Now().UTC()
	inv.InvoiceStatus = types.InvoiceStatusVoided
	inv.VoidedAt = &now

	if err := s.invoiceRepo.Update(ctx, inv); err != nil {
		return fmt.Errorf("failed to update invoice: %w", err)
	}

	// TODO: add publisher event for invoice voided

	return nil
}

func (s *invoiceService) UpdatePaymentStatus(ctx context.Context, id string, status types.InvoicePaymentStatus, amount *decimal.Decimal) error {
	inv, err := s.invoiceRepo.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get invoice: %w", err)
	}

	// Validate the invoice status
	allowedInvoiceStatuses := []types.InvoiceStatus{
		types.InvoiceStatusDraft,
		types.InvoiceStatusFinalized,
	}
	if !lo.Contains(allowedInvoiceStatuses, inv.InvoiceStatus) {
		return fmt.Errorf("invoice status - %s is not allowed", inv.InvoiceStatus)
	}

	// Validate the payment status transition
	if err := s.validatePaymentStatusTransition(inv.PaymentStatus, status); err != nil {
		return fmt.Errorf("invalid payment status transition: %w", err)
	}

	// Validate the request amount
	if amount != nil && amount.IsNegative() {
		return fmt.Errorf("amount must be non-negative")
	}

	now := time.Now().UTC()
	inv.PaymentStatus = status

	switch status {
	case types.InvoicePaymentStatusPending:
		if amount != nil {
			inv.AmountPaid = *amount
			inv.AmountRemaining = inv.AmountDue.Sub(*amount)
		}
	case types.InvoicePaymentStatusSucceeded:
		inv.AmountPaid = inv.AmountDue
		inv.AmountRemaining = decimal.Zero
		inv.PaidAt = &now
	case types.InvoicePaymentStatusFailed:
		inv.AmountPaid = decimal.Zero
		inv.AmountRemaining = inv.AmountDue
		inv.PaidAt = nil
	}

	// Validate the final state
	if err := inv.Validate(); err != nil {
		return fmt.Errorf("invalid invoice state: %w", err)
	}

	if err := s.invoiceRepo.Update(ctx, inv); err != nil {
		return fmt.Errorf("failed to update invoice: %w", err)
	}

	return nil
}

func (s *invoiceService) CreateSubscriptionInvoice(ctx context.Context, sub *subscription.Subscription, periodStart, periodEnd time.Time, usage *dto.GetUsageBySubscriptionResponse) (*dto.InvoiceResponse, error) {
	s.logger.Infow("creating subscription invoice",
		"subscription_id", sub.ID,
		"period_start", periodStart,
		"period_end", periodEnd)

	if usage == nil {
		return nil, fmt.Errorf("usage is required")
	}

	// Get all prices for the subscription's plan
	priceService := NewPriceService(s.priceRepo, s.meterRepo, s.logger)
	pricesResponse, err := priceService.GetPricesByPlanID(ctx, sub.PlanID)
	if err != nil {
		return nil, fmt.Errorf("failed to get prices: %w", err)
	}

	// Fetch Plan info as well
	plan, err := s.planRepo.Get(ctx, sub.PlanID)
	if err != nil {
		return nil, fmt.Errorf("failed to get plan: %w", err)
	}

	// Filter prices valid for this subscription
	validPrices := filterValidPricesForSubscription(pricesResponse.Prices, sub)

	// Calculate total amount from usage
	usageAmount := decimal.NewFromFloat(usage.Amount)

	// Calculate fixed charges
	var fixedCost decimal.Decimal
	var fixedCostLineItems []dto.CreateInvoiceLineItemRequest

	for _, price := range validPrices {
		if price.Price.Type == types.PRICE_TYPE_FIXED {
			quantity := decimal.NewFromInt(1)
			amount := priceService.CalculateCost(ctx, price.Price, quantity)
			fixedCost = fixedCost.Add(amount)

			fixedCostLineItems = append(fixedCostLineItems, dto.CreateInvoiceLineItemRequest{
				PlanID:          &price.Price.PlanID,
				PlanDisplayName: lo.ToPtr(plan.Name),
				PriceID:         price.Price.ID,
				PriceType:       lo.ToPtr(string(price.Price.Type)),
				Amount:          amount,
				Quantity:        quantity,
				PeriodStart:     &periodStart,
				PeriodEnd:       &periodEnd,
				Metadata: types.Metadata{
					"description": fmt.Sprintf("%s (Fixed Charge)", price.Price.LookupKey),
				},
			})
		}
	}

	// Total amount is sum of usage and fixed charges
	amountDue := usageAmount.Add(fixedCost)
	invoiceDueDate := periodEnd.Add(24 * time.Hour * types.InvoiceDefaultDueDays)

	// Create invoice using CreateInvoice
	req := dto.CreateInvoiceRequest{
		CustomerID:     sub.CustomerID,
		SubscriptionID: lo.ToPtr(sub.ID),
		InvoiceType:    types.InvoiceTypeSubscription,
		InvoiceStatus:  lo.ToPtr(types.InvoiceStatusDraft),
		PaymentStatus:  lo.ToPtr(types.InvoicePaymentStatusPending),
		Currency:       sub.Currency,
		AmountDue:      amountDue,
		Description:    fmt.Sprintf("Invoice for subscription %s", sub.ID),
		DueDate:        lo.ToPtr(invoiceDueDate),
		PeriodStart:    &periodStart,
		PeriodEnd:      &periodEnd,
		BillingReason:  types.InvoiceBillingReasonSubscriptionCycle,
		Metadata:       types.Metadata{},
	}

	// Add fixed charge line items
	req.LineItems = append(req.LineItems, fixedCostLineItems...)

	// Add usage charge line items
	for _, item := range usage.Charges {
		lineItemAmount := decimal.NewFromFloat(item.Amount)
		req.LineItems = append(req.LineItems, dto.CreateInvoiceLineItemRequest{
			PlanID:           &item.Price.PlanID,
			PlanDisplayName:  lo.ToPtr(plan.Name),
			PriceType:        lo.ToPtr(string(item.Price.Type)),
			PriceID:          item.Price.ID,
			MeterID:          &item.Price.MeterID,
			MeterDisplayName: lo.ToPtr(item.MeterDisplayName),
			Amount:           lineItemAmount,
			Quantity:         decimal.NewFromFloat(item.Quantity),
			PeriodStart:      &periodStart,
			PeriodEnd:        &periodEnd,
			Metadata: types.Metadata{
				"description": fmt.Sprintf("%s (Usage Charge)", item.Price.LookupKey),
			},
		})
	}

	s.logger.Infow("creating invoice with fixed and usage charges",
		"subscription_id", sub.ID,
		"usage_amount", usageAmount,
		"fixed_amount", fixedCost,
		"total_amount", amountDue,
		"fixed_line_items", len(fixedCostLineItems),
		"usage_line_items", len(usage.Charges))

	return s.CreateInvoice(ctx, req)
}

func (s *invoiceService) validatePaymentStatusTransition(from, to types.InvoicePaymentStatus) error {
	// Define allowed transitions
	allowedTransitions := map[types.InvoicePaymentStatus][]types.InvoicePaymentStatus{
		types.InvoicePaymentStatusPending: {
			types.InvoicePaymentStatusPending,
			types.InvoicePaymentStatusSucceeded,
			types.InvoicePaymentStatusFailed,
		},
		types.InvoicePaymentStatusFailed: {
			types.InvoicePaymentStatusPending,
		},
	}

	allowed, ok := allowedTransitions[from]
	if !ok {
		return fmt.Errorf("invalid current payment status: %s", from)
	}

	for _, status := range allowed {
		if status == to {
			return nil
		}
	}

	return fmt.Errorf("invalid payment status transition from %s to %s", from, to)
}
