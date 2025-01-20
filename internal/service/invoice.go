package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/idempotency"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/publisher"
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
	CreateSubscriptionInvoice(ctx context.Context, sub *subscription.Subscription, periodStart, periodEnd time.Time) (*dto.InvoiceResponse, error)
	GetPreviewInvoice(ctx context.Context, req dto.GetPreviewInvoiceRequest) (*dto.InvoiceResponse, error)
	GetCustomerInvoiceSummary(ctx context.Context, customerID string, currency string) (*dto.CustomerInvoiceSummary, error)
}

type invoiceService struct {
	subscriptionRepo subscription.Repository
	planRepo         plan.Repository
	priceRepo        price.Repository
	eventRepo        events.Repository
	meterRepo        meter.Repository
	customerRepo     customer.Repository
	invoiceRepo      invoice.Repository
	publisher        publisher.EventPublisher
	logger           *logger.Logger
	db               postgres.IClient
	idempGen         *idempotency.Generator
}

func NewInvoiceService(
	subscriptionRepo subscription.Repository,
	planRepo plan.Repository,
	priceRepo price.Repository,
	eventRepo events.Repository,
	meterRepo meter.Repository,
	customerRepo customer.Repository,
	invoiceRepo invoice.Repository,
	publisher publisher.EventPublisher,
	db postgres.IClient,
	logger *logger.Logger,
) InvoiceService {
	return &invoiceService{
		subscriptionRepo: subscriptionRepo,
		planRepo:         planRepo,
		priceRepo:        priceRepo,
		eventRepo:        eventRepo,
		meterRepo:        meterRepo,
		customerRepo:     customerRepo,
		invoiceRepo:      invoiceRepo,
		publisher:        publisher,
		db:               db,
		logger:           logger,
		idempGen:         idempotency.NewGenerator(),
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

	for _, lineItem := range inv.LineItems {
		s.logger.Debugw("got invoice line item", "id", lineItem.ID, "display_name", lineItem.DisplayName)
	}

	// expand subscription
	subscriptionService := NewSubscriptionService(
		s.subscriptionRepo,
		s.planRepo,
		s.priceRepo,
		s.eventRepo,
		s.meterRepo,
		s.customerRepo,
		s.invoiceRepo,
		s.publisher,
		s.db,
		s.logger,
	)

	response := dto.NewInvoiceResponse(inv)

	if inv.InvoiceType == types.InvoiceTypeSubscription {
		subscription, err := subscriptionService.GetSubscription(ctx, *inv.SubscriptionID)
		if err != nil {
			return nil, fmt.Errorf("failed to get subscription: %w", err)
		}
		response.WithSubscription(subscription)
	}

	return response, nil
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
		Pagination: types.PaginationResponse{
			Total:  count,
			Limit:  filter.GetLimit(),
			Offset: filter.GetOffset(),
		},
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

func (s *invoiceService) CreateSubscriptionInvoice(ctx context.Context, sub *subscription.Subscription, periodStart, periodEnd time.Time) (*dto.InvoiceResponse, error) {
	s.logger.Infow("creating subscription invoice",
		"subscription_id", sub.ID,
		"period_start", periodStart,
		"period_end", periodEnd)

	billingService := NewBillingService(
		s.subscriptionRepo,
		s.planRepo,
		s.priceRepo,
		s.eventRepo,
		s.meterRepo,
		s.customerRepo,
		s.invoiceRepo,
		s.publisher,
		s.db,
		s.logger,
	)

	// Prepare invoice request using billing service
	req, err := billingService.PrepareSubscriptionInvoiceRequest(ctx, sub, periodStart, periodEnd, false)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare invoice request: %w", err)
	}

	// Create the invoice
	return s.CreateInvoice(ctx, *req)
}

func (s *invoiceService) GetPreviewInvoice(ctx context.Context, req dto.GetPreviewInvoiceRequest) (*dto.InvoiceResponse, error) {
	billingService := NewBillingService(
		s.subscriptionRepo,
		s.planRepo,
		s.priceRepo,
		s.eventRepo,
		s.meterRepo,
		s.customerRepo,
		s.invoiceRepo,
		s.publisher,
		s.db,
		s.logger,
	)

	sub, err := s.subscriptionRepo.Get(ctx, req.SubscriptionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get subscription: %w", err)
	}

	if req.PeriodStart == nil {
		req.PeriodStart = &sub.CurrentPeriodStart
	}

	if req.PeriodEnd == nil {
		req.PeriodEnd = &sub.CurrentPeriodEnd
	}

	// Prepare invoice request using billing service
	invReq, err := billingService.PrepareSubscriptionInvoiceRequest(
		ctx, sub, *req.PeriodStart, *req.PeriodEnd, true)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare invoice request: %w", err)
	}

	// Create a draft invoice object for preview
	inv, err := invReq.ToInvoice(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to convert request to invoice: %w", err)
	}

	// Return preview response
	return dto.NewInvoiceResponse(inv), nil
}

func (s *invoiceService) GetCustomerInvoiceSummary(ctx context.Context, customerID, currency string) (*dto.CustomerInvoiceSummary, error) {
	s.logger.Debugw("getting customer invoice summary",
		"customer_id", customerID,
		"currency", currency,
	)

	// Get all non-voided invoices for the customer
	filter := types.NewNoLimitInvoiceFilter()
	filter.QueryFilter.Status = lo.ToPtr(types.StatusPublished)
	filter.CustomerID = customerID
	filter.InvoiceStatus = []types.InvoiceStatus{types.InvoiceStatusDraft, types.InvoiceStatusFinalized}

	invoicesResp, err := s.ListInvoices(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to list invoices: %w", err)
	}

	summary := &dto.CustomerInvoiceSummary{
		CustomerID:          customerID,
		Currency:            currency,
		TotalRevenueAmount:  decimal.Zero,
		TotalUnpaidAmount:   decimal.Zero,
		TotalOverdueAmount:  decimal.Zero,
		TotalInvoiceCount:   0,
		UnpaidInvoiceCount:  0,
		OverdueInvoiceCount: 0,
		UnpaidUsageCharges:  decimal.Zero,
		UnpaidFixedCharges:  decimal.Zero,
	}

	now := time.Now().UTC()

	// Process each invoice
	for _, inv := range invoicesResp.Items {
		summary.TotalRevenueAmount = summary.TotalRevenueAmount.Add(inv.AmountDue)
		summary.TotalInvoiceCount++

		// Skip paid and void invoices
		if inv.PaymentStatus == types.InvoicePaymentStatusSucceeded {
			continue
		}

		// Skip invoices with different currency
		if !types.IsMatchingCurrency(inv.Currency, currency) {
			continue
		}

		summary.TotalUnpaidAmount = summary.TotalUnpaidAmount.Add(inv.AmountRemaining)
		summary.UnpaidInvoiceCount++

		// Check if invoice is overdue
		if inv.DueDate != nil && inv.DueDate.Before(now) {
			summary.TotalOverdueAmount = summary.TotalOverdueAmount.Add(inv.AmountRemaining)
			summary.OverdueInvoiceCount++
		}

		// Split charges by type
		for _, item := range inv.LineItems {
			if *item.PriceType == string(types.PRICE_TYPE_USAGE) {
				summary.UnpaidUsageCharges = summary.UnpaidUsageCharges.Add(item.Amount)
			} else {
				summary.UnpaidFixedCharges = summary.UnpaidFixedCharges.Add(item.Amount)
			}
		}
	}

	s.logger.Debugw("customer invoice summary calculated",
		"customer_id", customerID,
		"currency", currency,
		"total_revenue", summary.TotalRevenueAmount,
		"total_unpaid", summary.TotalUnpaidAmount,
		"total_overdue", summary.TotalOverdueAmount,
		"total_invoice_count", summary.TotalInvoiceCount,
		"unpaid_invoice_count", summary.UnpaidInvoiceCount,
		"overdue_invoice_count", summary.OverdueInvoiceCount,
		"unpaid_usage_charges", summary.UnpaidUsageCharges,
		"unpaid_fixed_charges", summary.UnpaidFixedCharges,
	)

	return summary, nil
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
