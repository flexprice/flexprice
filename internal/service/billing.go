package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/publisher"
	"github.com/flexprice/flexprice/internal/types"
	webhookPublisher "github.com/flexprice/flexprice/internal/webhook/publisher"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// BillingCalculationResult holds all calculated charges for a billing period
type BillingCalculationResult struct {
	FixedCharges []dto.CreateInvoiceLineItemRequest
	UsageCharges []dto.CreateInvoiceLineItemRequest
	TotalAmount  decimal.Decimal
	Currency     string
}

// BillingService handles all billing calculations
type BillingService interface {
	// CalculateFixedCharges calculates all fixed charges for a subscription
	CalculateFixedCharges(ctx context.Context, sub *subscription.Subscription, periodStart, periodEnd time.Time) ([]dto.CreateInvoiceLineItemRequest, decimal.Decimal, error)

	// CalculateUsageCharges calculates all usage-based charges
	CalculateUsageCharges(ctx context.Context, sub *subscription.Subscription, usage *dto.GetUsageBySubscriptionResponse, periodStart, periodEnd time.Time) ([]dto.CreateInvoiceLineItemRequest, decimal.Decimal, error)

	// CalculateAllCharges calculates both fixed and usage charges
	CalculateAllCharges(ctx context.Context, sub *subscription.Subscription, usage *dto.GetUsageBySubscriptionResponse, periodStart, periodEnd time.Time) (*BillingCalculationResult, error)

	// PrepareSubscriptionInvoiceRequest prepares a complete invoice request for a subscription period
	PrepareSubscriptionInvoiceRequest(ctx context.Context, sub *subscription.Subscription, periodStart, periodEnd time.Time, isPreview bool) (*dto.CreateInvoiceRequest, error)
}

type billingService struct {
	subscriptionRepo subscription.Repository
	planRepo         plan.Repository
	priceRepo        price.Repository
	eventRepo        events.Repository
	meterRepo        meter.Repository
	customerRepo     customer.Repository
	invoiceRepo      invoice.Repository
	entitlementRepo  entitlement.Repository
	featureRepo      feature.Repository
	eventPublisher   publisher.EventPublisher
	webhookPublisher webhookPublisher.WebhookPublisher
	logger           *logger.Logger
	db               postgres.IClient
	config           *config.Configuration
}

func NewBillingService(
	subscriptionRepo subscription.Repository,
	planRepo plan.Repository,
	priceRepo price.Repository,
	eventRepo events.Repository,
	meterRepo meter.Repository,
	customerRepo customer.Repository,
	invoiceRepo invoice.Repository,
	entitlementRepo entitlement.Repository,
	featureRepo feature.Repository,
	eventPublisher publisher.EventPublisher,
	webhookPublisher webhookPublisher.WebhookPublisher,
	db postgres.IClient,
	logger *logger.Logger,
	config *config.Configuration,
) BillingService {
	return &billingService{
		subscriptionRepo: subscriptionRepo,
		planRepo:         planRepo,
		priceRepo:        priceRepo,
		eventRepo:        eventRepo,
		meterRepo:        meterRepo,
		customerRepo:     customerRepo,
		invoiceRepo:      invoiceRepo,
		entitlementRepo:  entitlementRepo,
		featureRepo:      featureRepo,
		eventPublisher:   eventPublisher,
		webhookPublisher: webhookPublisher,
		db:               db,
		logger:           logger,
		config:           config,
	}
}

func (s *billingService) CalculateFixedCharges(
	ctx context.Context,
	sub *subscription.Subscription,
	periodStart,
	periodEnd time.Time,
) ([]dto.CreateInvoiceLineItemRequest, decimal.Decimal, error) {
	fixedCost := decimal.Zero
	fixedCostLineItems := make([]dto.CreateInvoiceLineItemRequest, 0)

	priceService := NewPriceService(s.priceRepo, s.meterRepo, s.logger)

	// Process fixed charges from line items
	for _, item := range sub.LineItems {
		if item.PriceType != types.PRICE_TYPE_FIXED {
			continue
		}

		price, err := priceService.GetPrice(ctx, item.PriceID)
		if err != nil {
			return nil, fixedCost, errors.WithOp(err, "price.get")
		}

		amount := priceService.CalculateCost(ctx, price.Price, item.Quantity)

		fixedCostLineItems = append(fixedCostLineItems, dto.CreateInvoiceLineItemRequest{
			PlanID:          lo.ToPtr(item.PlanID),
			PlanDisplayName: lo.ToPtr(item.PlanDisplayName),
			PriceID:         item.PriceID,
			PriceType:       lo.ToPtr(string(item.PriceType)),
			DisplayName:     lo.ToPtr(item.DisplayName),
			Amount:          amount,
			Quantity:        item.Quantity,
			PeriodStart:     lo.ToPtr(periodStart),
			PeriodEnd:       lo.ToPtr(periodEnd),
			Metadata: types.Metadata{
				"description": fmt.Sprintf("%s (Fixed Charge)", item.DisplayName),
			},
		})

		fixedCost = fixedCost.Add(amount)
	}

	return fixedCostLineItems, fixedCost, nil
}

func (s *billingService) CalculateUsageCharges(
	ctx context.Context,
	sub *subscription.Subscription,
	usage *dto.GetUsageBySubscriptionResponse,
	periodStart,
	periodEnd time.Time,
) ([]dto.CreateInvoiceLineItemRequest, decimal.Decimal, error) {
	if usage == nil {
		return nil, decimal.Zero, nil
	}

	usageCharges := make([]dto.CreateInvoiceLineItemRequest, 0)
	totalUsageCost := decimal.Zero

	// Process usage charges from line items
	for _, item := range sub.LineItems {
		if item.PriceType != types.PRICE_TYPE_USAGE {
			continue
		}

		// Find matching usage charge
		var matchingCharge *dto.SubscriptionUsageByMetersResponse
		for _, charge := range usage.Charges {
			if charge.Price.ID == item.PriceID {
				matchingCharge = charge
				break
			}
		}

		if matchingCharge == nil {
			s.logger.Debugw("no matching charge found for usage line item",
				"subscription_id", sub.ID,
				"line_item_id", item.ID,
				"price_id", item.PriceID)
			continue
		}

		lineItemAmount := decimal.NewFromFloat(matchingCharge.Amount)
		totalUsageCost = totalUsageCost.Add(lineItemAmount)

		usageCharges = append(usageCharges, dto.CreateInvoiceLineItemRequest{
			PlanID:           lo.ToPtr(item.PlanID),
			PlanDisplayName:  lo.ToPtr(item.PlanDisplayName),
			PriceType:        lo.ToPtr(string(item.PriceType)),
			PriceID:          item.PriceID,
			MeterID:          lo.ToPtr(item.MeterID),
			MeterDisplayName: lo.ToPtr(item.MeterDisplayName),
			DisplayName:      lo.ToPtr(item.DisplayName),
			Amount:           lineItemAmount,
			Quantity:         decimal.NewFromFloat(matchingCharge.Quantity),
			PeriodStart:      lo.ToPtr(periodStart),
			PeriodEnd:        lo.ToPtr(periodEnd),
			Metadata: types.Metadata{
				"description": fmt.Sprintf("%s (Usage Charge)", item.DisplayName),
			},
		})
	}

	if !totalUsageCost.Equal(decimal.NewFromFloat(usage.Amount)) {
		return usageCharges, totalUsageCost, fmt.Errorf("usage cost calculation mismatch at line item level")
	}

	return usageCharges, totalUsageCost, nil
}

func (s *billingService) CalculateAllCharges(
	ctx context.Context,
	sub *subscription.Subscription,
	usage *dto.GetUsageBySubscriptionResponse,
	periodStart,
	periodEnd time.Time,
) (*BillingCalculationResult, error) {
	// Calculate fixed charges
	fixedCharges, fixedTotal, err := s.CalculateFixedCharges(ctx, sub, periodStart, periodEnd)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate fixed charges: %w", err)
	}

	// Calculate usage charges
	usageCharges, usageTotal, err := s.CalculateUsageCharges(ctx, sub, usage, periodStart, periodEnd)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate usage charges: %w", err)
	}

	return &BillingCalculationResult{
		FixedCharges: fixedCharges,
		UsageCharges: usageCharges,
		TotalAmount:  fixedTotal.Add(usageTotal),
		Currency:     sub.Currency,
	}, nil
}

func (s *billingService) PrepareSubscriptionInvoiceRequest(
	ctx context.Context,
	sub *subscription.Subscription,
	periodStart,
	periodEnd time.Time,
	isPreview bool,
) (*dto.CreateInvoiceRequest, error) {
	s.logger.Infow("preparing subscription invoice request",
		"subscription_id", sub.ID,
		"period_start", periodStart,
		"period_end", periodEnd,
		"is_preview", isPreview)

	// Get usage for the period
	subscriptionService := NewSubscriptionService(
		s.subscriptionRepo,
		s.planRepo,
		s.priceRepo,
		s.eventRepo,
		s.meterRepo,
		s.customerRepo,
		s.invoiceRepo,
		s.entitlementRepo,
		s.featureRepo,
		s.eventPublisher,
		s.webhookPublisher,
		s.db,
		s.logger,
		s.config,
	)

	usage, err := subscriptionService.GetUsageBySubscription(ctx, &dto.GetUsageBySubscriptionRequest{
		SubscriptionID: sub.ID,
		StartTime:      periodStart,
		EndTime:        periodEnd,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get usage data: %w", err)
	}

	// Calculate all charges
	result, err := s.CalculateAllCharges(ctx, sub, usage, periodStart, periodEnd)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate charges: %w", err)
	}

	// Prepare invoice due date
	invoiceDueDate := periodEnd.Add(24 * time.Hour * types.InvoiceDefaultDueDays)

	// Prepare description based on preview status
	description := fmt.Sprintf("Invoice for subscription %s", sub.ID)
	if isPreview {
		description = fmt.Sprintf("Preview invoice for subscription %s", sub.ID)
	}

	// Create invoice request
	req := &dto.CreateInvoiceRequest{
		CustomerID:     sub.CustomerID,
		SubscriptionID: lo.ToPtr(sub.ID),
		InvoiceType:    types.InvoiceTypeSubscription,
		InvoiceStatus:  lo.ToPtr(types.InvoiceStatusDraft),
		PaymentStatus:  lo.ToPtr(types.PaymentStatusPending),
		Currency:       sub.Currency,
		AmountDue:      result.TotalAmount,
		Description:    description,
		DueDate:        lo.ToPtr(invoiceDueDate),
		BillingPeriod:  lo.ToPtr(string(sub.BillingPeriod)),
		PeriodStart:    &periodStart,
		PeriodEnd:      &periodEnd,
		BillingReason:  types.InvoiceBillingReasonSubscriptionCycle,
		Metadata:       types.Metadata{},
		LineItems:      append(result.FixedCharges, result.UsageCharges...),
	}

	s.logger.Infow("prepared invoice request",
		"subscription_id", sub.ID,
		"total_amount", result.TotalAmount,
		"fixed_line_items", len(result.FixedCharges),
		"usage_line_items", len(result.UsageCharges))

	return req, nil
}
