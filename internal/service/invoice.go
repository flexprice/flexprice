package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	pdf "github.com/flexprice/flexprice/internal/domain/pdf"
	taxapplied "github.com/flexprice/flexprice/internal/domain/taxapplied"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/idempotency"
	"github.com/flexprice/flexprice/internal/s3"
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
	ProcessDraftInvoice(ctx context.Context, id string) error
	UpdatePaymentStatus(ctx context.Context, id string, status types.PaymentStatus, amount *decimal.Decimal) error
	CreateSubscriptionInvoice(ctx context.Context, req *dto.CreateSubscriptionInvoiceRequest) (*dto.InvoiceResponse, error)
	GetPreviewInvoice(ctx context.Context, req dto.GetPreviewInvoiceRequest) (*dto.InvoiceResponse, error)
	GetCustomerInvoiceSummary(ctx context.Context, customerID string, currency string) (*dto.CustomerInvoiceSummary, error)
	GetCustomerMultiCurrencyInvoiceSummary(ctx context.Context, customerID string) (*dto.CustomerMultiCurrencyInvoiceSummary, error)
	AttemptPayment(ctx context.Context, id string) error
	GetInvoicePDF(ctx context.Context, id string) ([]byte, error)
	GetInvoicePDFUrl(ctx context.Context, id string) (string, error)
	RecalculateInvoice(ctx context.Context, id string, finalize bool) (*dto.InvoiceResponse, error)
	RecalculateInvoiceAmounts(ctx context.Context, invoiceID string) error
}

type invoiceService struct {
	ServiceParams
	idempGen *idempotency.Generator
}

func NewInvoiceService(params ServiceParams) InvoiceService {
	return &invoiceService{
		ServiceParams: params,
		idempGen:      idempotency.NewGenerator(),
	}
}

func (s *invoiceService) CreateInvoice(ctx context.Context, req dto.CreateInvoiceRequest) (*dto.InvoiceResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	var resp *dto.InvoiceResponse

	// Start transaction
	err := s.DB.WithTx(ctx, func(tx context.Context) error {
		// 1. Generate idempotency key if not provided
		var idempKey string
		if req.IdempotencyKey == nil {
			params := map[string]interface{}{
				"tenant_id":    types.GetTenantID(ctx),
				"customer_id":  req.CustomerID,
				"period_start": req.PeriodStart,
				"period_end":   req.PeriodEnd,
				"timestamp":    time.Now().UTC(), // TODO: rethink this
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
		existing, err := s.InvoiceRepo.GetByIdempotencyKey(tx, idempKey)

		if err != nil && !ierr.IsNotFound(err) {
			return ierr.WithError(err).WithHint("failed to check idempotency").Mark(ierr.ErrDatabase)
		}
		if existing != nil {
			s.Logger.Infof("invoice already exists, returning existing invoice")
			err = ierr.NewError("invoice already exists").WithHint("invoice already exists").Mark(ierr.ErrAlreadyExists)
			return err
		}

		// 3. For subscription invoices, validate period uniqueness and get billing sequence
		var billingSeq *int
		if req.SubscriptionID != nil {
			// Check period uniqueness
			exists, err := s.InvoiceRepo.ExistsForPeriod(ctx, *req.SubscriptionID, *req.PeriodStart, *req.PeriodEnd)
			if err != nil {
				return err
			}
			if exists {
				s.Logger.Infow("another invoice for same subscription period exists",
					"subscription_id", *req.SubscriptionID,
					"period_start", *req.PeriodStart,
					"period_end", *req.PeriodEnd)
				// do nothing, just log and continue
			}

			// Get billing sequence
			seq, err := s.InvoiceRepo.GetNextBillingSequence(ctx, *req.SubscriptionID)
			if err != nil {
				return err
			}
			billingSeq = &seq
		}

		// 4. Generate invoice number
		var invoiceNumber string
		if req.InvoiceNumber != nil {
			invoiceNumber = *req.InvoiceNumber
		} else {
			invoiceNumber, err = s.InvoiceRepo.GetNextInvoiceNumber(ctx)
			if err != nil {
				return err
			}
		}

		// 5. Create invoice
		// Convert request to domain model
		inv, err := req.ToInvoice(ctx)
		if err != nil {
			return err
		}

		inv.InvoiceNumber = &invoiceNumber
		inv.IdempotencyKey = &idempKey
		inv.BillingSequence = billingSeq

		// Setting default values
		if req.InvoiceType == types.InvoiceTypeOneOff || req.InvoiceType == types.InvoiceTypeCredit {
			if req.InvoiceStatus == nil {
				inv.InvoiceStatus = types.InvoiceStatusFinalized
			}
		} else if req.InvoiceType == types.InvoiceTypeSubscription {
			if req.InvoiceStatus == nil {
				inv.InvoiceStatus = types.InvoiceStatusDraft
			}
		}

		if req.AmountPaid == nil {
			if req.PaymentStatus == nil {
				inv.AmountPaid = inv.AmountDue
			}
		}

		// Calculated Amount Remaining
		inv.AmountRemaining = inv.AmountDue.Sub(inv.AmountPaid)

		if req.PaymentStatus == nil || lo.FromPtr(req.PaymentStatus) == "" {
			if inv.AmountRemaining.IsZero() {
				inv.PaymentStatus = types.PaymentStatusSucceeded
			} else {
				inv.PaymentStatus = types.PaymentStatusPending
			}
		}

		// Validate invoice
		if err := inv.Validate(); err != nil {
			return err
		}

		// Create invoice with line items in a single transaction
		if err := s.InvoiceRepo.CreateWithLineItems(ctx, inv); err != nil {
			return err
		}

		// Apply taxes if this is a subscription invoice
		s.Logger.Infow("applying taxes to invoice",
			"invoice_id", inv.ID,
			"subscription_id", inv.SubscriptionID,
			"customer_id", inv.CustomerID,
			"period_start", inv.PeriodStart,
			"period_end", inv.PeriodEnd,
		)
		if err := s.handleTaxRateOverrides(ctx, inv, req); err != nil {
			return err
		}

		// Convert to response
		resp = dto.NewInvoiceResponse(inv)
		return nil
	})

	if err != nil {
		s.Logger.Errorw("failed to create invoice",
			"error", err,
			"customer_id", req.CustomerID,
			"subscription_id", req.SubscriptionID)
		return nil, err
	}

	eventName := types.WebhookEventInvoiceCreateDraft
	if resp.InvoiceStatus == types.InvoiceStatusFinalized {
		eventName = types.WebhookEventInvoiceUpdateFinalized
	}

	s.publishInternalWebhookEvent(ctx, eventName, resp.ID)
	return resp, nil
}

func (s *invoiceService) GetInvoice(ctx context.Context, id string) (*dto.InvoiceResponse, error) {
	inv, err := s.InvoiceRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	for _, lineItem := range inv.LineItems {
		s.Logger.Debugw("got invoice line item", "id", lineItem.ID, "display_name", lineItem.DisplayName)
	}

	// expand subscription
	subscriptionService := NewSubscriptionService(s.ServiceParams)

	response := dto.NewInvoiceResponse(inv)

	if inv.InvoiceType == types.InvoiceTypeSubscription {
		subscription, err := subscriptionService.GetSubscription(ctx, *inv.SubscriptionID)
		if err != nil {
			return nil, err
		}
		response.WithSubscription(subscription)
		if subscription.Customer != nil {
			response.Customer = subscription.Customer
		}
	}

	// Get customer information if not already set
	if response.Customer == nil {
		customer, err := s.CustomerRepo.Get(ctx, inv.CustomerID)
		if err != nil {
			return nil, err
		}
		response.WithCustomer(&dto.CustomerResponse{Customer: customer})
	}

	// get tax applied records
	filter := types.NewNoLimitTaxAppliedFilter()
	filter.EntityType = types.TaxrateEntityTypeInvoice
	filter.EntityID = inv.ID
	appliedTaxes, err := s.TaxAppliedRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Debug logging
	s.Logger.Infow("Found tax applied records for invoice",
		"invoice_id", inv.ID,
		"tax_applied_count", len(appliedTaxes),
		"entity_type", types.TaxrateEntityTypeInvoice,
		"entity_id", inv.ID,
	)

	if len(appliedTaxes) > 0 {
		taxes := lo.Map(appliedTaxes, func(tax *taxapplied.TaxApplied, _ int) *dto.TaxAppliedResponse {
			return &dto.TaxAppliedResponse{TaxApplied: *tax}
		})
		s.Logger.Infow("Taxes slice before setting on response", "taxes", taxes)
		response = response.WithTaxes(taxes)
		s.Logger.Infow("Added tax applied records to response",
			"invoice_id", inv.ID,
			"tax_count", len(taxes),
		)
	}

	return response, nil
}

func (s *invoiceService) ListInvoices(ctx context.Context, filter *types.InvoiceFilter) (*dto.ListInvoicesResponse, error) {

	if filter.ExternalCustomerID != "" {
		customer, err := s.CustomerRepo.GetByLookupKey(ctx, filter.ExternalCustomerID)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("failed to get customer by external customer id").
				Mark(ierr.ErrNotFound)
		}
		filter.CustomerID = customer.ID
	}

	invoices, err := s.InvoiceRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	count, err := s.InvoiceRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	customerMap := make(map[string]*customer.Customer)
	items := make([]*dto.InvoiceResponse, len(invoices))
	for i, inv := range invoices {
		items[i] = dto.NewInvoiceResponse(inv)
		customerMap[inv.CustomerID] = nil
	}

	customerFilter := types.NewNoLimitCustomerFilter()
	customerFilter.CustomerIDs = lo.Keys(customerMap)
	customers, err := s.CustomerRepo.List(ctx, customerFilter)
	if err != nil {
		return nil, err
	}

	for _, cust := range customers {
		customerMap[cust.ID] = cust
	}

	// Get customer information for each invoice
	for _, inv := range items {
		customer, ok := customerMap[inv.CustomerID]
		if !ok {
			continue
		}
		inv.WithCustomer(&dto.CustomerResponse{Customer: customer})
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
	inv, err := s.InvoiceRepo.Get(ctx, id)
	if err != nil {
		return err
	}

	return s.performFinalizeInvoiceActions(ctx, inv)
}

func (s *invoiceService) performFinalizeInvoiceActions(ctx context.Context, inv *invoice.Invoice) error {
	if inv.InvoiceStatus != types.InvoiceStatusDraft {
		return ierr.NewError("invoice is not in draft status").WithHint("invoice must be in draft status to be finalized").Mark(ierr.ErrValidation)
	}

	now := time.Now().UTC()
	inv.InvoiceStatus = types.InvoiceStatusFinalized
	inv.FinalizedAt = &now

	if err := s.InvoiceRepo.Update(ctx, inv); err != nil {
		return err
	}

	s.publishInternalWebhookEvent(ctx, types.WebhookEventInvoiceUpdateFinalized, inv.ID)
	return nil
}

func (s *invoiceService) VoidInvoice(ctx context.Context, id string) error {
	inv, err := s.InvoiceRepo.Get(ctx, id)
	if err != nil {
		return err
	}

	allowedInvoiceStatuses := []types.InvoiceStatus{
		types.InvoiceStatusDraft,
		types.InvoiceStatusFinalized,
	}
	if !lo.Contains(allowedInvoiceStatuses, inv.InvoiceStatus) {
		return ierr.NewError("invoice status is not allowed").
			WithHintf("invoice status - %s is not allowed", inv.InvoiceStatus).
			WithReportableDetails(map[string]any{
				"allowed_statuses": allowedInvoiceStatuses,
			}).
			Mark(ierr.ErrValidation)
	}

	allowedPaymentStatuses := []types.PaymentStatus{
		types.PaymentStatusPending,
		types.PaymentStatusFailed,
	}
	if !lo.Contains(allowedPaymentStatuses, inv.PaymentStatus) {
		return ierr.NewError("invoice payment status is not allowed").
			WithHintf("invoice payment status - %s is not allowed", inv.PaymentStatus).
			WithReportableDetails(map[string]any{
				"allowed_statuses": allowedPaymentStatuses,
			}).
			Mark(ierr.ErrValidation)
	}

	now := time.Now().UTC()
	inv.InvoiceStatus = types.InvoiceStatusVoided
	inv.VoidedAt = &now

	if err := s.InvoiceRepo.Update(ctx, inv); err != nil {
		return err
	}

	s.publishInternalWebhookEvent(ctx, types.WebhookEventInvoiceUpdateVoided, inv.ID)
	return nil
}

func (s *invoiceService) ProcessDraftInvoice(ctx context.Context, id string) error {
	inv, err := s.InvoiceRepo.Get(ctx, id)
	if err != nil {
		return err
	}

	if inv.InvoiceStatus != types.InvoiceStatusDraft {
		return ierr.NewError("invoice is not in draft status").WithHint("invoice must be in draft status to be processed").Mark(ierr.ErrValidation)
	}

	// try to finalize the invoice
	if err := s.performFinalizeInvoiceActions(ctx, inv); err != nil {
		return err
	}

	// try to process payment for the invoice and log any errors
	// this is not a blocker for the invoice to be processed
	if err := s.performPaymentAttemptActions(ctx, inv); err != nil {
		s.Logger.Errorw("failed to process payment for invoice",
			"error", err.Error(),
			"invoice_id", inv.ID)
	}

	return nil
}

func (s *invoiceService) UpdatePaymentStatus(ctx context.Context, id string, status types.PaymentStatus, amount *decimal.Decimal) error {
	inv, err := s.InvoiceRepo.Get(ctx, id)
	if err != nil {
		return err
	}

	// Validate the invoice status
	allowedInvoiceStatuses := []types.InvoiceStatus{
		types.InvoiceStatusDraft,
		types.InvoiceStatusFinalized,
	}
	if !lo.Contains(allowedInvoiceStatuses, inv.InvoiceStatus) {
		return ierr.NewError("invoice status is not allowed").
			WithHintf("invoice status - %s is not allowed", inv.InvoiceStatus).
			WithReportableDetails(map[string]any{
				"allowed_statuses": allowedInvoiceStatuses,
			}).
			Mark(ierr.ErrValidation)
	}

	// Validate that there shouldnt be any payments for this invoice
	paymentService := NewPaymentService(s.ServiceParams)
	filter := types.NewNoLimitPaymentFilter()
	filter.DestinationID = lo.ToPtr(id)
	filter.Status = lo.ToPtr(types.StatusPublished)
	filter.PaymentStatus = lo.ToPtr(string(types.PaymentStatusSucceeded))
	filter.DestinationType = lo.ToPtr(string(types.PaymentDestinationTypeInvoice))
	filter.Limit = lo.ToPtr(1)
	payments, err := paymentService.ListPayments(ctx, filter)
	if err != nil {
		return err
	}

	if len(payments.Items) > 0 {
		return ierr.NewError("invoice has active payment records").
			WithHint("Manual payment status updates are disabled for payment-based invoices.").
			Mark(ierr.ErrInvalidOperation)
	}

	// Validate the payment status transition
	if err := s.validatePaymentStatusTransition(inv.PaymentStatus, status); err != nil {
		return err
	}

	// Validate the request amount
	if amount != nil && amount.IsNegative() {
		return ierr.NewError("amount must be non-negative").
			WithHint("amount must be non-negative").
			Mark(ierr.ErrValidation)
	}

	now := time.Now().UTC()
	inv.PaymentStatus = status

	switch status {
	case types.PaymentStatusPending:
		if amount != nil {
			inv.AmountPaid = *amount
			inv.AmountRemaining = inv.AmountDue.Sub(*amount)
		}
	case types.PaymentStatusSucceeded:
		inv.AmountPaid = inv.AmountDue
		inv.AmountRemaining = decimal.Zero
		inv.PaidAt = &now
	case types.PaymentStatusFailed:
		inv.AmountPaid = decimal.Zero
		inv.AmountRemaining = inv.AmountDue
		inv.PaidAt = nil
	}

	// Validate the final state
	if err := inv.Validate(); err != nil {
		return err
	}

	if err := s.InvoiceRepo.Update(ctx, inv); err != nil {
		return err
	}

	// Publish webhook events
	s.publishInternalWebhookEvent(ctx, types.WebhookEventInvoiceUpdatePayment, inv.ID)

	return nil
}

func (s *invoiceService) CreateSubscriptionInvoice(ctx context.Context, req *dto.CreateSubscriptionInvoiceRequest) (*dto.InvoiceResponse, error) {
	s.Logger.Infow("creating subscription invoice",
		"subscription_id", req.SubscriptionID,
		"period_start", req.PeriodStart,
		"period_end", req.PeriodEnd,
		"reference_point", req.ReferencePoint)

	if err := req.Validate(); err != nil {
		return nil, err
	}

	billingService := NewBillingService(s.ServiceParams)

	// Get subscription with line items
	subscription, _, err := s.SubRepo.GetWithLineItems(ctx, req.SubscriptionID)
	if err != nil {
		return nil, err
	}

	// Prepare invoice request using billing service
	invoiceReq, err := billingService.PrepareSubscriptionInvoiceRequest(ctx,
		subscription,
		req.PeriodStart,
		req.PeriodEnd,
		req.ReferencePoint,
	)
	if err != nil {
		return nil, err
	}

	// Create the invoice
	inv, err := s.CreateInvoice(ctx, *invoiceReq)
	if err != nil {
		return nil, err
	}

	// Process the invoice
	if err := s.ProcessDraftInvoice(ctx, inv.ID); err != nil {
		return nil, err
	}

	return inv, nil
}

func (s *invoiceService) GetPreviewInvoice(ctx context.Context, req dto.GetPreviewInvoiceRequest) (*dto.InvoiceResponse, error) {
	billingService := NewBillingService(s.ServiceParams)

	sub, _, err := s.SubRepo.GetWithLineItems(ctx, req.SubscriptionID)
	if err != nil {
		return nil, err
	}

	if req.PeriodStart == nil {
		req.PeriodStart = &sub.CurrentPeriodStart
	}

	if req.PeriodEnd == nil {
		req.PeriodEnd = &sub.CurrentPeriodEnd
	}

	// Prepare invoice request using billing service with the preview reference point
	invReq, err := billingService.PrepareSubscriptionInvoiceRequest(
		ctx, sub, *req.PeriodStart, *req.PeriodEnd, types.ReferencePointPreview)
	if err != nil {
		return nil, err
	}

	// Create a draft invoice object for preview
	inv, err := invReq.ToInvoice(ctx)
	if err != nil {
		return nil, err
	}

	// Create preview response
	response := dto.NewInvoiceResponse(inv)

	// Get customer information
	customer, err := s.CustomerRepo.Get(ctx, inv.CustomerID)
	if err != nil {
		return nil, err
	}
	response.WithCustomer(&dto.CustomerResponse{Customer: customer})

	return response, nil
}

func (s *invoiceService) GetCustomerInvoiceSummary(ctx context.Context, customerID, currency string) (*dto.CustomerInvoiceSummary, error) {
	s.Logger.Debugw("getting customer invoice summary",
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
		return nil, err
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
		// Skip invoices with different currency
		if !types.IsMatchingCurrency(inv.Currency, currency) {
			continue
		}

		summary.TotalRevenueAmount = summary.TotalRevenueAmount.Add(inv.AmountDue)
		summary.TotalInvoiceCount++

		// Skip paid and void invoices
		if inv.PaymentStatus == types.PaymentStatusSucceeded {
			continue
		}

		summary.TotalUnpaidAmount = summary.TotalUnpaidAmount.Add(inv.AmountRemaining)
		summary.UnpaidInvoiceCount++

		// Check if invoice is overdue
		if inv.DueDate != nil && inv.DueDate.Before(now) {
			summary.TotalOverdueAmount = summary.TotalOverdueAmount.Add(inv.AmountRemaining)
			summary.OverdueInvoiceCount++

			// Publish webhook event
			s.publishInternalWebhookEvent(ctx, types.WebhookEventInvoicePaymentOverdue, inv.ID)
		}

		// Split charges by type
		for _, item := range inv.LineItems {
			if lo.FromPtr(item.PriceType) == string(types.PRICE_TYPE_USAGE) {
				summary.UnpaidUsageCharges = summary.UnpaidUsageCharges.Add(item.Amount)
			} else {
				summary.UnpaidFixedCharges = summary.UnpaidFixedCharges.Add(item.Amount)
			}
		}
	}

	s.Logger.Debugw("customer invoice summary calculated",
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

func (s *invoiceService) GetCustomerMultiCurrencyInvoiceSummary(ctx context.Context, customerID string) (*dto.CustomerMultiCurrencyInvoiceSummary, error) {
	subscriptionFilter := types.NewNoLimitSubscriptionFilter()
	subscriptionFilter.CustomerID = customerID
	subscriptionFilter.QueryFilter.Status = lo.ToPtr(types.StatusPublished)
	subscriptionFilter.SubscriptionStatusNotIn = []types.SubscriptionStatus{types.SubscriptionStatusCancelled}

	subs, err := s.SubRepo.List(ctx, subscriptionFilter)
	if err != nil {
		return nil, err
	}

	currencies := make([]string, 0, len(subs))
	for _, sub := range subs {
		currencies = append(currencies, sub.Currency)

	}
	currencies = lo.Uniq(currencies)

	if len(currencies) == 0 {
		return &dto.CustomerMultiCurrencyInvoiceSummary{
			CustomerID: customerID,
			Summaries:  []*dto.CustomerInvoiceSummary{},
		}, nil
	}

	defaultCurrency := currencies[0] // fallback to first currency

	summaries := make([]*dto.CustomerInvoiceSummary, 0, len(currencies))
	for _, currency := range currencies {
		summary, err := s.GetCustomerInvoiceSummary(ctx, customerID, currency)
		if err != nil {
			s.Logger.Errorw("failed to get customer invoice summary",
				"error", err,
				"customer_id", customerID,
				"currency", currency)
			continue
		}

		summaries = append(summaries, summary)
	}

	return &dto.CustomerMultiCurrencyInvoiceSummary{
		CustomerID:      customerID,
		DefaultCurrency: defaultCurrency,
		Summaries:       summaries,
	}, nil
}

func (s *invoiceService) validatePaymentStatusTransition(from, to types.PaymentStatus) error {
	// Define allowed transitions
	allowedTransitions := map[types.PaymentStatus][]types.PaymentStatus{
		types.PaymentStatusPending: {
			types.PaymentStatusPending,
			types.PaymentStatusSucceeded,
			types.PaymentStatusFailed,
		},
		types.PaymentStatusFailed: {
			types.PaymentStatusPending,
			types.PaymentStatusFailed,
			types.PaymentStatusSucceeded,
		},
	}

	allowed, ok := allowedTransitions[from]
	if !ok {
		return ierr.NewError("invalid current payment status").
			WithHintf("invalid current payment status: %s", from).
			WithReportableDetails(map[string]any{
				"allowed_statuses": allowedTransitions[from],
			}).
			Mark(ierr.ErrValidation)
	}

	for _, status := range allowed {
		if status == to {
			return nil
		}
	}

	return ierr.NewError("invalid payment status transition").
		WithHintf("invalid payment status transition from %s to %s", from, to).
		WithReportableDetails(map[string]any{
			"allowed_statuses": allowedTransitions[from],
		}).
		Mark(ierr.ErrValidation)
}

// AttemptPayment attempts to pay an invoice using available wallets
func (s *invoiceService) AttemptPayment(ctx context.Context, id string) error {

	// Get invoice
	inv, err := s.InvoiceRepo.Get(ctx, id)
	if err != nil {
		return err
	}

	return s.performPaymentAttemptActions(ctx, inv)
}

func (s *invoiceService) performPaymentAttemptActions(ctx context.Context, inv *invoice.Invoice) error {
	// Validate invoice status
	if inv.InvoiceStatus != types.InvoiceStatusFinalized {
		return ierr.NewError("invoice must be finalized").
			WithHint("Invoice must be finalized before attempting payment").
			Mark(ierr.ErrValidation)
	}

	// Validate payment status
	if inv.PaymentStatus == types.PaymentStatusSucceeded {
		return ierr.NewError("invoice is already paid by payment status").
			WithHint("Invoice is already paid").
			WithReportableDetails(map[string]any{
				"invoice_id":     inv.ID,
				"payment_status": inv.PaymentStatus,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	// Check if there's any amount remaining to pay
	if inv.AmountRemaining.LessThanOrEqual(decimal.Zero) {
		return ierr.NewError("invoice has no remaining amount to pay").
			WithHint("Invoice has no remaining amount to pay").
			Mark(ierr.ErrValidation)
	}

	// Use the wallet payment service to process the payment
	walletPaymentService := NewWalletPaymentService(s.ServiceParams)

	// Use default options (promotional wallets first, then prepaid)
	options := DefaultWalletPaymentOptions()

	// Add any additional metadata specific to this payment attempt
	options.AdditionalMetadata = types.Metadata{
		"payment_source": "automatic_attempt",
	}

	amountPaid, err := walletPaymentService.ProcessInvoicePaymentWithWallets(ctx, inv, options)
	if err != nil {
		return err
	}

	if amountPaid.IsZero() {
		s.Logger.Infow("no payments processed for invoice",
			"invoice_id", inv.ID,
			"amount_remaining", inv.AmountRemaining)
	} else if amountPaid.Equal(inv.AmountRemaining) {
		s.Logger.Infow("invoice fully paid with wallets",
			"invoice_id", inv.ID,
			"amount_paid", amountPaid)
	} else {
		s.Logger.Infow("invoice partially paid with wallets",
			"invoice_id", inv.ID,
			"amount_paid", amountPaid,
			"amount_remaining", inv.AmountRemaining.Sub(amountPaid))
	}

	return nil
}

func (s *invoiceService) GetInvoicePDFUrl(ctx context.Context, id string) (string, error) {
	if s.S3 == nil {
		return "", ierr.NewError("s3 is not enabled").
			WithHint("s3 is not enabled but is required to generate invoice pdf url.").
			Mark(ierr.ErrSystem)
	}

	tenantId := types.GetTenantID(ctx)

	key := fmt.Sprintf("%s/%s", tenantId, id)

	exists, err := s.S3.Exists(ctx, key, s3.DocumentTypeInvoice)
	if err != nil {
		return "", err
	}

	if !exists {
		data, err := s.GetInvoicePDF(ctx, id)
		if err != nil {
			return "", err
		}

		err = s.S3.UploadDocument(ctx, s3.NewPdfDocument(key, data, s3.DocumentTypeInvoice))
		if err != nil {
			return "", err
		}
	}

	url, err := s.S3.GetPresignedUrl(ctx, key, s3.DocumentTypeInvoice)
	if err != nil {
		return "", err
	}

	return url, nil
}

// GetInvoicePDF implements InvoiceService.
func (s *invoiceService) GetInvoicePDF(ctx context.Context, id string) ([]byte, error) {
	// get invoice by id
	inv, err := s.InvoiceRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// fetch customer info
	customer, err := s.CustomerRepo.Get(ctx, inv.CustomerID)
	if err != nil {
		return nil, err
	}

	// fetch biller info - tenant info from tenant id
	tenant, err := s.TenantRepo.GetByID(ctx, inv.TenantID)
	if err != nil {
		return nil, err
	}

	invoiceData, err := s.getInvoiceDataForPDFGen(ctx, inv, customer, tenant)
	if err != nil {
		return nil, err
	}

	// generate pdf
	return s.PDFGenerator.RenderInvoicePdf(ctx, invoiceData)

}

func (s *invoiceService) getInvoiceDataForPDFGen(
	_ context.Context,
	inv *invoice.Invoice,
	customer *customer.Customer,
	tenant *tenant.Tenant,
) (*pdf.InvoiceData, error) {
	invoiceNum := ""
	if inv.InvoiceNumber != nil {
		invoiceNum = *inv.InvoiceNumber
	}

	amountDue, _ := inv.AmountDue.Float64()
	// Convert to InvoiceData
	data := &pdf.InvoiceData{
		ID:            inv.ID,
		InvoiceNumber: invoiceNum,
		InvoiceStatus: string(inv.InvoiceStatus),
		Currency:      types.GetCurrencySymbol(inv.Currency),
		AmountDue:     amountDue,
		BillingReason: inv.BillingReason,
		Notes:         "",  // resolved from invoice metadata
		VAT:           0.0, // resolved from invoice metadata
		Biller:        s.getBillerInfo(tenant),
		Recipient:     s.getRecipientInfo(customer),
	}

	// Convert dates
	if inv.DueDate != nil {
		data.DueDate = pdf.CustomTime{Time: *inv.DueDate}
	}

	if inv.FinalizedAt != nil {
		data.IssuingDate = pdf.CustomTime{Time: *inv.FinalizedAt}
	}

	// Parse metadata if available
	if inv.Metadata != nil {
		// Try to extract notes from metadata
		if notes, ok := inv.Metadata["notes"]; ok {
			data.Notes = notes
		}

		// Try to extract VAT from metadata
		if vat, ok := inv.Metadata["vat"]; ok {
			vatValue, err := strconv.ParseFloat(vat, 64)
			if err != nil {
				return nil, ierr.WithError(err).WithHintf("failed to parse VAT %s", vat).Mark(ierr.ErrDatabase)
			}
			data.VAT = vatValue
		}
	}

	data.LineItems = make([]pdf.LineItemData, len(inv.LineItems))

	for i, item := range inv.LineItems {
		planDisplayName := ""
		if item.PlanDisplayName != nil {
			planDisplayName = *item.PlanDisplayName
		}
		displayName := ""
		if item.DisplayName != nil {
			displayName = *item.DisplayName
		}

		amount, _ := item.Amount.Float64()
		quantity, _ := item.Quantity.Float64()

		description := ""
		if item.Metadata != nil {
			if desc, ok := item.Metadata["description"]; ok {
				description = desc
			}
		}

		lineItem := pdf.LineItemData{
			PlanDisplayName: planDisplayName,
			DisplayName:     displayName,
			Description:     description,
			Amount:          amount,
			Quantity:        quantity,
			Currency:        types.GetCurrencySymbol(item.Currency),
		}

		if lineItem.PlanDisplayName == "" {
			lineItem.PlanDisplayName = lineItem.DisplayName
		}

		if item.PeriodStart != nil {
			lineItem.PeriodStart = pdf.CustomTime{Time: *item.PeriodStart}
		}
		if item.PeriodEnd != nil {
			lineItem.PeriodEnd = pdf.CustomTime{Time: *item.PeriodEnd}
		}

		data.LineItems[i] = lineItem
	}

	return data, nil
}

func (s *invoiceService) getRecipientInfo(c *customer.Customer) *pdf.RecipientInfo {
	if c == nil {
		return nil
	}

	name := fmt.Sprintf("Customer %s", c.ID)
	if c.Name != "" {
		name = c.Name
	}

	result := &pdf.RecipientInfo{
		Name: name,
		Address: pdf.AddressInfo{
			Street:     "--",
			City:       "--",
			PostalCode: "--",
		},
	}

	if c.Email != "" {
		result.Email = c.Email
	}

	if c.AddressLine1 != "" {
		result.Address.Street = c.AddressLine1
	}
	if c.AddressLine2 != "" {
		result.Address.Street += "\n" + c.AddressLine2
	}
	if c.AddressCity != "" {
		result.Address.City = c.AddressCity
	}
	if c.AddressState != "" {
		result.Address.State = c.AddressState
	}
	if c.AddressPostalCode != "" {
		result.Address.PostalCode = c.AddressPostalCode
	}
	if c.AddressCountry != "" {
		result.Address.Country = c.AddressCountry
	}

	return result
}

func (s *invoiceService) getBillerInfo(t *tenant.Tenant) *pdf.BillerInfo {
	if t == nil {
		return nil
	}

	billerInfo := pdf.BillerInfo{
		Name: t.Name,
		Address: pdf.AddressInfo{
			Street:     "--",
			City:       "--",
			PostalCode: "--",
		},
	}

	if t.BillingDetails != (tenant.TenantBillingDetails{}) {
		billingDetails := t.BillingDetails
		billerInfo.Email = billingDetails.Email
		// billerInfo.Website = billingDetails.Website //TODO: Add this
		billerInfo.HelpEmail = billingDetails.HelpEmail
		// billerInfo.PaymentInstructions = billingDetails.PaymentInstructions //TODO: Add this
		billerInfo.Address = pdf.AddressInfo{
			Street:     strings.Join([]string{billingDetails.Address.Line1, billingDetails.Address.Line2}, "\n"),
			City:       billingDetails.Address.City,
			PostalCode: billingDetails.Address.PostalCode,
			Country:    billingDetails.Address.Country,
			State:      billingDetails.Address.State,
		}
	}

	return &billerInfo
}

func (s *invoiceService) RecalculateInvoiceAmounts(ctx context.Context, invoiceID string) error {
	inv, err := s.InvoiceRepo.Get(ctx, invoiceID)
	if err != nil {
		return err
	}

	// Validate invoice status
	if inv.InvoiceStatus != types.InvoiceStatusFinalized {
		s.Logger.Infow("invoice is not finalized, skipping recalculation", "invoice_id", invoiceID)
		return nil
	}

	// Get all adjustment credit notes for the invoice
	filter := &types.CreditNoteFilter{
		InvoiceID:        inv.ID,
		CreditNoteStatus: []types.CreditNoteStatus{types.CreditNoteStatusFinalized},
		QueryFilter:      types.NewNoLimitPublishedQueryFilter(),
	}

	creditNotes, err := s.CreditNoteRepo.List(ctx, filter)
	if err != nil {
		return err
	}

	totalAdjustmentAmount := decimal.Zero
	totalRefundAmount := decimal.Zero
	for _, creditNote := range creditNotes {
		if creditNote.CreditNoteType == types.CreditNoteTypeRefund {
			totalRefundAmount = totalRefundAmount.Add(creditNote.TotalAmount)
		} else {
			totalAdjustmentAmount = totalAdjustmentAmount.Add(creditNote.TotalAmount)
		}
	}

	// Calculate total adjustment credits
	inv.AdjustmentAmount = totalAdjustmentAmount
	inv.RefundedAmount = totalRefundAmount
	inv.AmountDue = inv.Total.Sub(totalAdjustmentAmount)
	remaining := inv.AmountDue.Sub(inv.AmountPaid)
	if remaining.IsPositive() {
		inv.AmountRemaining = remaining
	} else {
		inv.AmountRemaining = decimal.Zero
	}

	// Update the payment status if the invoice is fully paid
	if inv.AmountRemaining.Equal(decimal.Zero) {
		s.Logger.Infow("invoice is fully paid, updating payment status to succeeded", "invoice_id", inv.ID)
		inv.PaymentStatus = types.PaymentStatusSucceeded
	}

	if err := s.InvoiceRepo.Update(ctx, inv); err != nil {
		return err
	}

	// Apply taxes after amount recalculation
	if err := s.RecalculateTaxesOnInvoice(ctx, inv); err != nil {
		return err
	}

	return nil
}
func (s *invoiceService) publishInternalWebhookEvent(ctx context.Context, eventName string, invoiceID string) {
	webhookPayload, err := json.Marshal(struct {
		InvoiceID string `json:"invoice_id"`
		TenantID  string `json:"tenant_id"`
	}{
		InvoiceID: invoiceID,
		TenantID:  types.GetTenantID(ctx),
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

func (s *invoiceService) RecalculateInvoice(ctx context.Context, id string, finalize bool) (*dto.InvoiceResponse, error) {
	s.Logger.Infow("recalculating invoice", "invoice_id", id)

	// Get the invoice
	inv, err := s.InvoiceRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Validate invoice is in draft state
	if inv.InvoiceStatus != types.InvoiceStatusDraft {
		return nil, ierr.NewError("invoice is not in draft status").
			WithHint("Only draft invoices can be recalculated").
			WithReportableDetails(map[string]interface{}{
				"invoice_id":     inv.ID,
				"current_status": inv.InvoiceStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	// Validate this is a subscription invoice
	if inv.InvoiceType != types.InvoiceTypeSubscription || inv.SubscriptionID == nil {
		return nil, ierr.NewError("invoice is not a subscription invoice").
			WithHint("Only subscription invoices can be recalculated").
			WithReportableDetails(map[string]interface{}{
				"invoice_id":   inv.ID,
				"invoice_type": inv.InvoiceType,
			}).
			Mark(ierr.ErrValidation)
	}

	// Validate period dates are available
	if inv.PeriodStart == nil || inv.PeriodEnd == nil {
		return nil, ierr.NewError("invoice period dates are missing").
			WithHint("Invoice must have period start and end dates for recalculation").
			Mark(ierr.ErrValidation)
	}

	// Get subscription with line items
	subscription, _, err := s.SubRepo.GetWithLineItems(ctx, *inv.SubscriptionID)
	if err != nil {
		return nil, err
	}

	// Start transaction to update invoice atomically
	err = s.DB.WithTx(ctx, func(txCtx context.Context) error {
		// STEP 1: Remove existing line items FIRST to ensure fresh calculation
		// This is crucial - we need to "archive" existing line items before calling
		// PrepareSubscriptionInvoiceRequest so it treats this as a fresh calculation
		existingLineItemIDs := make([]string, len(inv.LineItems))
		for i, item := range inv.LineItems {
			existingLineItemIDs[i] = item.ID
		}

		if len(existingLineItemIDs) > 0 {
			if err := s.InvoiceRepo.RemoveLineItems(txCtx, inv.ID, existingLineItemIDs); err != nil {
				return err
			}
			s.Logger.Infow("archived existing line items for fresh recalculation",
				"invoice_id", inv.ID,
				"archived_items", len(existingLineItemIDs))
		}

		// STEP 2: Now call PrepareSubscriptionInvoiceRequest for fresh calculation
		// Since we removed existing line items, the billing service will see no already
		// invoiced items and will recalculate everything completely
		billingService := NewBillingService(s.ServiceParams)

		// Use period_end reference point to include both arrear and advance charges
		referencePoint := types.ReferencePointPeriodEnd

		newInvoiceReq, err := billingService.PrepareSubscriptionInvoiceRequest(txCtx,
			subscription,
			*inv.PeriodStart,
			*inv.PeriodEnd,
			referencePoint,
		)
		if err != nil {
			return err
		}

		// STEP 3: Update invoice totals and metadata
		inv.AmountDue = newInvoiceReq.AmountDue
		inv.AmountRemaining = newInvoiceReq.AmountDue.Sub(inv.AmountPaid)
		inv.Description = newInvoiceReq.Description
		if newInvoiceReq.Metadata != nil {
			inv.Metadata = newInvoiceReq.Metadata
		}

		// Update payment status if amount due changed
		if inv.AmountRemaining.IsZero() {
			inv.PaymentStatus = types.PaymentStatusSucceeded
		} else if inv.AmountPaid.IsZero() {
			inv.PaymentStatus = types.PaymentStatusPending
		} else {
			inv.PaymentStatus = types.PaymentStatusPending // Partially paid
		}

		// STEP 4: Create new line items from the fresh calculation
		newLineItems := make([]*invoice.InvoiceLineItem, len(newInvoiceReq.LineItems))
		for i, lineItemReq := range newInvoiceReq.LineItems {
			lineItem := &invoice.InvoiceLineItem{
				ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM),
				InvoiceID:       inv.ID,
				CustomerID:      inv.CustomerID,
				PlanID:          lineItemReq.PlanID,
				PlanDisplayName: lineItemReq.PlanDisplayName,
				PriceID:         lineItemReq.PriceID,
				PriceType:       lineItemReq.PriceType,
				DisplayName:     lineItemReq.DisplayName,
				Amount:          lineItemReq.Amount,
				Quantity:        lineItemReq.Quantity,
				Currency:        inv.Currency,
				PeriodStart:     lineItemReq.PeriodStart,
				PeriodEnd:       lineItemReq.PeriodEnd,
				Metadata:        lineItemReq.Metadata,
				EnvironmentID:   inv.EnvironmentID,
				BaseModel:       types.GetDefaultBaseModel(txCtx),
			}
			newLineItems[i] = lineItem
		}

		// STEP 5: Add the newly calculated line items
		if len(newLineItems) > 0 {
			if err := s.InvoiceRepo.AddLineItems(txCtx, inv.ID, newLineItems); err != nil {
				return err
			}
		}

		// STEP 6: Update the invoice
		if err := s.InvoiceRepo.Update(txCtx, inv); err != nil {
			return err
		}

		// STEP 7: Apply taxes after recalculation
		s.RecalculateTaxesOnInvoice(txCtx, inv)

		s.Logger.Infow("successfully recalculated invoice with fresh calculation",
			"invoice_id", inv.ID,
			"subscription_id", *inv.SubscriptionID,
			"old_amount_due", inv.AmountDue,
			"new_amount_due", newInvoiceReq.AmountDue,
			"old_line_items", len(existingLineItemIDs),
			"new_line_items", len(newLineItems),
			"recalculation_type", "complete_fresh_calculation")

		return nil
	})

	if err != nil {
		s.Logger.Errorw("failed to recalculate invoice",
			"error", err,
			"invoice_id", inv.ID,
			"subscription_id", *inv.SubscriptionID)
		return nil, err
	}

	// Publish webhook event for invoice recalculation
	s.publishInternalWebhookEvent(ctx, types.WebhookEventInvoiceCreateDraft, inv.ID)

	// Finalize the invoice if requested
	if finalize {
		if err := s.FinalizeInvoice(ctx, id); err != nil {
			s.Logger.Errorw("failed to finalize invoice after recalculation",
				"error", err,
				"invoice_id", id)
			return nil, err
		}
		s.Logger.Infow("successfully finalized invoice after recalculation", "invoice_id", id)
	}

	// Return updated invoice
	return s.GetInvoice(ctx, id)
}

// RecalculateTaxesOnInvoice recalculates taxes on an invoice if it's a subscription invoice
func (s *invoiceService) RecalculateTaxesOnInvoice(ctx context.Context, inv *invoice.Invoice) error {
	// Get the invoice to check if it's a subscription invoice

	// Only apply taxes to subscription invoices
	if inv.InvoiceType != types.InvoiceTypeSubscription || inv.SubscriptionID == nil {
		return nil
	}

	taxService := NewTaxService(s.ServiceParams)
	if err := taxService.ApplyTaxOnInvoice(ctx, inv.ID); err != nil {
		s.Logger.Errorw("failed to apply taxes on invoice",
			"error", err,
			"invoice_id", inv.ID,
			"subscription_id", *inv.SubscriptionID)
		return err
	}

	return nil
}

func (s *invoiceService) handleTaxRateOverrides(ctx context.Context, inv *invoice.Invoice, req dto.CreateInvoiceRequest) error {
	if len(req.TaxRateOverrides) > 0 {
		// Handle tax rate overrides for the invoice
		s.Logger.Infow("processing tax rate overrides for invoice",
			"invoice_id", inv.ID,
			"overrides_count", len(req.TaxRateOverrides))

		// Step 1: Convert tax overrides to tax entity associations
		taxLinkingRequests := make([]*dto.CreateEntityTaxAssociation, 0, len(req.TaxRateOverrides))
		for _, taxRateOverride := range req.TaxRateOverrides {
			taxRateLink := taxRateOverride.ToTaxEntityAssociation(ctx, inv.ID, types.TaxrateEntityTypeInvoice)
			taxLinkingRequests = append(taxLinkingRequests, taxRateLink)
		}

		// Step 2: Resolve or create tax rates
		taxAssociationService := NewTaxAssociationService(s.ServiceParams)
		resolvedTaxRates, err := taxAssociationService.ResolveOrCreateTaxRates(ctx, taxLinkingRequests)
		if err != nil {
			s.Logger.Errorw("failed to resolve or create tax rates",
				"error", err,
				"invoice_id", inv.ID)
			return err
		}

		// Step 3: Collect all tax rate IDs
		taxRateIDs := make([]string, 0, len(resolvedTaxRates))
		for _, resolvedTaxRate := range resolvedTaxRates {
			taxRateIDs = append(taxRateIDs, resolvedTaxRate.TaxRateID)
		}

		// Step 4: Fetch all tax rates using the collected IDs
		taxService := NewTaxService(s.ServiceParams)
		taxRates, err := taxService.ListTaxRates(ctx, &types.TaxRateFilter{
			TaxRateIDs: taxRateIDs,
		})
		if err != nil {
			s.Logger.Errorw("failed to fetch tax rates",
				"error", err,
				"invoice_id", inv.ID,
				"tax_rate_ids", taxRateIDs)
			return err
		}

		// Step 5: Apply taxes and calculate total tax amount
		taxableAmount := inv.Subtotal
		totalTaxAmount := decimal.Zero

		for _, taxRate := range taxRates.Items {
			var taxAmount decimal.Decimal

			// Calculate tax amount based on tax rate type
			switch taxRate.TaxRateType {
			case types.TaxRateTypePercentage:
				// For percentage tax: taxable_amount * (percentage / 100)
				taxAmount = taxableAmount.Mul(*taxRate.PercentageValue).Div(decimal.NewFromInt(100))
			case types.TaxRateTypeFixed:
				// For fixed tax: use the fixed value directly
				taxAmount = *taxRate.FixedValue
			default:
				s.Logger.Warnw("unknown tax rate type, skipping",
					"tax_rate_id", taxRate.ID,
					"tax_rate_type", taxRate.TaxRateType,
				)
				continue
			}

			// Add tax amount to total
			totalTaxAmount = totalTaxAmount.Add(taxAmount)

			// Create a tax applied record
			taxAppliedRecord := &dto.CreateTaxAppliedRequest{
				TaxRateID:     taxRate.ID,
				EntityType:    types.TaxrateEntityTypeInvoice,
				EntityID:      inv.ID,
				TaxableAmount: taxableAmount,
				TaxAmount:     taxAmount,
				Currency:      inv.Currency,
			}

			// Create the tax applied record
			_, err := taxService.CreateTaxApplied(ctx, *taxAppliedRecord)
			if err != nil {
				s.Logger.Errorw("failed to create tax applied record",
					"error", err,
					"tax_rate_id", taxRate.ID,
					"invoice_id", inv.ID)
				return err
			}

			s.Logger.Infow("created tax applied record for override",
				"tax_rate_id", taxRate.ID,
				"tax_rate_code", taxRate.Code,
				"tax_amount", taxAmount,
				"taxable_amount", taxableAmount,
				"invoice_id", inv.ID,
			)
		}

		// Step 7: Update the invoice with the total tax and recalculate the total
		inv.TotalTax = totalTaxAmount
		inv.Total = inv.Subtotal.Add(totalTaxAmount)

		// Update the invoice
		if err := s.InvoiceRepo.Update(ctx, inv); err != nil {
			s.Logger.Errorw("failed to update invoice with tax amounts",
				"error", err,
				"invoice_id", inv.ID,
				"total_tax", totalTaxAmount,
				"new_total", inv.Total)
			return err
		}

		s.Logger.Infow("successfully applied tax overrides to invoice",
			"invoice_id", inv.ID,
			"total_tax", totalTaxAmount,
			"new_total", inv.Total,
			"overrides_processed", len(req.TaxRateOverrides))

	} else {
		// Handle subscription tax rate overrides (existing logic)
		taxService := NewTaxService(s.ServiceParams)
		if err := taxService.ApplyTaxOnInvoice(ctx, inv.ID); err != nil {
			return err
		}
	}

	return nil
}
