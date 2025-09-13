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
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/idempotency"
	"github.com/flexprice/flexprice/internal/s3"
	"github.com/flexprice/flexprice/internal/types"
	typesSettings "github.com/flexprice/flexprice/internal/types/settings"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type InvoiceService interface {
	CreateOneOffInvoice(ctx context.Context, req dto.CreateInvoiceRequest) (*dto.InvoiceResponse, error)
	CreateInvoice(ctx context.Context, req dto.CreateInvoiceRequest) (*dto.InvoiceResponse, error)
	GetInvoice(ctx context.Context, id string) (*dto.InvoiceResponse, error)
	ListInvoices(ctx context.Context, filter *types.InvoiceFilter) (*dto.ListInvoicesResponse, error)
	FinalizeInvoice(ctx context.Context, id string) error
	VoidInvoice(ctx context.Context, id string) error
	ProcessDraftInvoice(ctx context.Context, id string, paymentParams *dto.PaymentParameters, sub *subscription.Subscription, flowType types.InvoiceFlowType) error
	UpdatePaymentStatus(ctx context.Context, id string, status types.PaymentStatus, amount *decimal.Decimal) error
	ReconcilePaymentStatus(ctx context.Context, id string, status types.PaymentStatus, amount *decimal.Decimal) error
	CreateSubscriptionInvoice(ctx context.Context, req *dto.CreateSubscriptionInvoiceRequest, paymentParams *dto.PaymentParameters, flowType types.InvoiceFlowType) (*dto.InvoiceResponse, *subscription.Subscription, error)
	GetPreviewInvoice(ctx context.Context, req dto.GetPreviewInvoiceRequest) (*dto.InvoiceResponse, error)
	GetCustomerInvoiceSummary(ctx context.Context, customerID string, currency string) (*dto.CustomerInvoiceSummary, error)
	GetUnpaidInvoicesToBePaid(ctx context.Context, customerID string, currency string) ([]*dto.InvoiceResponse, decimal.Decimal, error)
	GetCustomerMultiCurrencyInvoiceSummary(ctx context.Context, customerID string) (*dto.CustomerMultiCurrencyInvoiceSummary, error)
	AttemptPayment(ctx context.Context, id string) error
	GetInvoicePDF(ctx context.Context, id string) ([]byte, error)
	GetInvoicePDFUrl(ctx context.Context, id string) (string, error)
	RecalculateInvoice(ctx context.Context, id string, finalize bool) (*dto.InvoiceResponse, error)
	RecalculateInvoiceAmounts(ctx context.Context, invoiceID string) error
	UpdateInvoice(ctx context.Context, id string, req dto.UpdateInvoiceRequest) (*dto.InvoiceResponse, error)
	CalculatePriceBreakdown(ctx context.Context, inv *dto.InvoiceResponse) (map[string][]dto.SourceUsageItem, error)
	CalculateUsageBreakdown(ctx context.Context, inv *dto.InvoiceResponse, groupBy []string) (map[string][]dto.UsageBreakdownItem, error)
	TriggerCommunication(ctx context.Context, id string) error
	HandleIncompleteSubscriptionPayment(ctx context.Context, invoice *invoice.Invoice) error
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

func (s *invoiceService) CreateOneOffInvoice(ctx context.Context, req dto.CreateInvoiceRequest) (*dto.InvoiceResponse, error) {

	// Here we validate all the coupons and then pass them to CreateInvoice Service.
	// This validation is here because we want to the createInvoice be independent of the coupon service.
	couponValidationService := NewCouponValidationService(s.ServiceParams)
	couponService := NewCouponService(s.ServiceParams)
	validCoupons := make([]dto.InvoiceCoupon, 0)
	for _, couponID := range req.Coupons {
		coupon, err := couponService.GetCoupon(ctx, couponID)
		if err != nil {
			s.Logger.Errorw("failed to get coupon", "error", err, "coupon_id", couponID)
			continue
		}
		if err := couponValidationService.ValidateCoupon(ctx, couponID, nil); err != nil {
			s.Logger.Errorw("failed to validate coupon", "error", err, "coupon_id", couponID)
			continue
		}
		validCoupons = append(validCoupons, dto.InvoiceCoupon{
			CouponID:      couponID,
			AmountOff:     coupon.AmountOff,
			PercentageOff: coupon.PercentageOff,
			Type:          coupon.Type,
		})
	}

	req.InvoiceCoupons = validCoupons

	// Validate tax rates
	taxService := NewTaxService(s.ServiceParams)
	finalTaxRates := make([]*dto.TaxRateResponse, 0)
	for _, taxRate := range req.TaxRates {
		taxRate, err := taxService.GetTaxRate(ctx, taxRate)
		if err != nil {
			return nil, err
		}
		finalTaxRates = append(finalTaxRates, taxRate)
	}

	req.PreparedTaxRates = finalTaxRates
	return s.CreateInvoice(ctx, req)
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
			settingsService := NewSettingsService(s.ServiceParams)
			invoiceConfigResponse, err := settingsService.GetSettingByKey(ctx, typesSettings.SettingKeyInvoiceConfig.String())
			if err != nil {
				return err
			}

			// Use the new optimized conversion method
			var config typesSettings.InvoiceConfig
			settingsInvoiceConfig, err := config.ToInvoiceConfig(invoiceConfigResponse.Value)
			if err != nil {
				return ierr.WithError(err).
					WithHint("Failed to parse invoice configuration").
					Mark(ierr.ErrValidation)
			}

			// Convert from settings InvoiceConfig to types InvoiceConfig
			invoiceConfig := &types.InvoiceConfig{
				InvoiceNumberPrefix:        settingsInvoiceConfig.Prefix,
				InvoiceNumberFormat:        types.InvoiceNumberFormat(settingsInvoiceConfig.Format),
				InvoiceNumberStartSequence: settingsInvoiceConfig.StartSequence,
				InvoiceNumberTimezone:      settingsInvoiceConfig.Timezone,
				InvoiceNumberSeparator:     settingsInvoiceConfig.Separator,
				InvoiceNumberSuffixLength:  settingsInvoiceConfig.SuffixLength,
				DueDateDays:                &settingsInvoiceConfig.DueDateDays,
			}

			invoiceNumber, err = s.InvoiceRepo.GetNextInvoiceNumber(ctx, invoiceConfig)
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

		// Set correct billing reason based on billing sequence for subscription invoices
		if req.SubscriptionID != nil && billingSeq != nil && lo.FromPtr(billingSeq) == 1 {
			inv.BillingReason = string(types.InvoiceBillingReasonSubscriptionCreate)
		}

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

		// Apply coupons first (invoice and line-item)
		if err := s.applyCouponsToInvoiceWithLineItems(ctx, inv, req); err != nil {
			return err
		}

		// Handle tax rate overrides
		if err := s.handleTaxRateOverrides(ctx, inv, req); err != nil {
			return err
		}
		// Update the invoice in the database
		if err := s.InvoiceRepo.Update(ctx, inv); err != nil {
			return err
		}

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
	taxService := NewTaxService(s.ServiceParams)
	filter := types.NewNoLimitTaxAppliedFilter()
	filter.EntityType = types.TaxRateEntityTypeInvoice
	filter.EntityID = inv.ID
	appliedTaxes, err := taxService.ListTaxApplied(ctx, filter)
	if err != nil {
		return nil, err
	}

	response.Taxes = appliedTaxes.Items

	return response, nil
}

// getBulkUsageAnalyticsForInvoice fetches analytics for all line items in a single ClickHouse call
// This replaces the previous approach of making N separate calls per line item
func (s *invoiceService) getBulkUsageAnalyticsForInvoice(ctx context.Context, usageBasedLineItems []*dto.InvoiceLineItemResponse, inv *dto.InvoiceResponse) (map[string][]dto.SourceUsageItem, error) {
	// Step 1: Collect all feature IDs and build line item metadata
	featureIDs := make([]string, 0, len(usageBasedLineItems))
	lineItemToFeatureMap := make(map[string]string)                   // lineItemID -> featureID
	lineItemMetadata := make(map[string]*dto.InvoiceLineItemResponse) // lineItemID -> lineItem

	for _, lineItem := range usageBasedLineItems {
		// Skip if essential fields are missing
		if lineItem.PriceID == nil || lineItem.MeterID == nil {
			s.Logger.Warnw("skipping line item with missing price_id or meter_id",
				"line_item_id", lineItem.ID,
				"price_id", lineItem.PriceID,
				"meter_id", lineItem.MeterID)
			continue
		}

		// Get feature ID from meter
		featureFilter := types.NewNoLimitFeatureFilter()
		featureFilter.MeterIDs = []string{*lineItem.MeterID}
		features, err := s.FeatureRepo.List(ctx, featureFilter)
		if err != nil || len(features) == 0 {
			s.Logger.Warnw("no feature found for meter",
				"meter_id", *lineItem.MeterID,
				"line_item_id", lineItem.ID)
			continue
		}

		featureID := features[0].ID
		featureIDs = append(featureIDs, featureID)
		lineItemToFeatureMap[lineItem.ID] = featureID
		lineItemMetadata[lineItem.ID] = lineItem
	}

	if len(featureIDs) == 0 {
		s.Logger.Warnw("no valid feature IDs found for any line items")
		return make(map[string][]dto.SourceUsageItem), nil
	}

	// Step 2: Get customer external ID
	customer, err := s.CustomerRepo.Get(ctx, inv.CustomerID)
	if err != nil {
		s.Logger.Errorw("failed to get customer for usage analytics",
			"customer_id", inv.CustomerID,
			"error", err)
		return nil, err
	}

	// Step 3: Use invoice period for usage calculation
	periodStart := inv.PeriodStart
	periodEnd := inv.PeriodEnd

	if periodStart == nil || periodEnd == nil {
		s.Logger.Warnw("missing period information in invoice",
			"invoice_id", inv.ID,
			"period_start", periodStart,
			"period_end", periodEnd)
		return make(map[string][]dto.SourceUsageItem), nil
	}

	// Step 4: Make SINGLE analytics request for ALL feature IDs, grouped by source AND feature_id
	analyticsReq := &dto.GetUsageAnalyticsRequest{
		ExternalCustomerID: customer.ExternalID,
		FeatureIDs:         featureIDs, // All feature IDs at once!
		StartTime:          *periodStart,
		EndTime:            *periodEnd,
		GroupBy:            []string{"source", "feature_id"}, // Group by BOTH source and feature_id
	}

	s.Logger.Infow("making bulk analytics request",
		"invoice_id", inv.ID,
		"feature_ids_count", len(featureIDs),
		"customer_id", customer.ExternalID)

	eventPostProcessingService := NewEventPostProcessingService(s.ServiceParams, s.EventRepo, s.ProcessedEventRepo)
	analyticsResponse, err := eventPostProcessingService.GetDetailedUsageAnalytics(ctx, analyticsReq)
	if err != nil {
		s.Logger.Errorw("failed to get bulk usage analytics",
			"invoice_id", inv.ID,
			"error", err)
		return nil, err
	}

	s.Logger.Infow("retrieved bulk usage analytics",
		"invoice_id", inv.ID,
		"analytics_items_count", len(analyticsResponse.Items))

	// Step 5: Map results back to line items and calculate costs
	return s.mapBulkAnalyticsToLineItems(ctx, analyticsResponse, lineItemToFeatureMap, lineItemMetadata)
}

// mapBulkAnalyticsToLineItems maps the bulk analytics response back to individual line items
// and calculates proportional costs for each source within each line item
func (s *invoiceService) mapBulkAnalyticsToLineItems(ctx context.Context, analyticsResponse *dto.GetUsageAnalyticsResponse, lineItemToFeatureMap map[string]string, lineItemMetadata map[string]*dto.InvoiceLineItemResponse) (map[string][]dto.SourceUsageItem, error) {
	usageAnalyticsResponse := make(map[string][]dto.SourceUsageItem)

	// Step 1: Group analytics by feature_id and source
	featureAnalyticsMap := make(map[string]map[string]dto.UsageAnalyticItem) // featureID -> source -> analytics

	for _, analyticsItem := range analyticsResponse.Items {
		if featureAnalyticsMap[analyticsItem.FeatureID] == nil {
			featureAnalyticsMap[analyticsItem.FeatureID] = make(map[string]dto.UsageAnalyticItem)
		}
		featureAnalyticsMap[analyticsItem.FeatureID][analyticsItem.Source] = analyticsItem
	}

	// Step 2: Process each line item
	for lineItemID, featureID := range lineItemToFeatureMap {
		lineItem := lineItemMetadata[lineItemID]
		sourceAnalytics, exists := featureAnalyticsMap[featureID]

		if !exists || len(sourceAnalytics) == 0 {
			// No usage data for this line item
			s.Logger.Debugw("no usage analytics found for line item",
				"line_item_id", lineItemID,
				"feature_id", featureID)
			usageAnalyticsResponse[lineItemID] = []dto.SourceUsageItem{}
			continue
		}

		// Step 3: Calculate total usage for this line item across all sources
		totalUsageForLineItem := decimal.Zero
		for _, analyticsItem := range sourceAnalytics {
			totalUsageForLineItem = totalUsageForLineItem.Add(analyticsItem.TotalUsage)
		}

		// Step 4: Calculate proportional costs for each source
		lineItemUsageAnalytics := make([]dto.SourceUsageItem, 0, len(sourceAnalytics))
		totalLineItemCost := lineItem.Amount

		for source, analyticsItem := range sourceAnalytics {
			// Calculate proportional cost based on usage
			var cost string
			if !totalLineItemCost.IsZero() && !totalUsageForLineItem.IsZero() {
				proportionalCost := analyticsItem.TotalUsage.Div(totalUsageForLineItem).Mul(totalLineItemCost)
				cost = proportionalCost.StringFixed(2)
			} else {
				cost = "0"
			}

			// Calculate percentage
			var percentage string
			if !totalUsageForLineItem.IsZero() {
				pct := analyticsItem.TotalUsage.Div(totalUsageForLineItem).Mul(decimal.NewFromInt(100))
				percentage = pct.StringFixed(2)
			} else {
				percentage = "0"
			}

			// Create usage analytics item
			usageItem := dto.SourceUsageItem{
				Source: source,
				Cost:   cost,
			}

			// Add optional fields
			if !analyticsItem.TotalUsage.IsZero() {
				usageStr := analyticsItem.TotalUsage.StringFixed(2)
				usageItem.Usage = &usageStr
			}

			if percentage != "0" {
				usageItem.Percentage = &percentage
			}

			if analyticsItem.EventCount > 0 {
				eventCount := int(analyticsItem.EventCount)
				usageItem.EventCount = &eventCount
			}

			lineItemUsageAnalytics = append(lineItemUsageAnalytics, usageItem)
		}

		usageAnalyticsResponse[lineItemID] = lineItemUsageAnalytics

		s.Logger.Debugw("mapped usage analytics for line item",
			"line_item_id", lineItemID,
			"feature_id", featureID,
			"sources_count", len(lineItemUsageAnalytics),
			"total_usage", totalUsageForLineItem.StringFixed(2))
	}

	return usageAnalyticsResponse, nil
}

func (s *invoiceService) CalculatePriceBreakdown(ctx context.Context, inv *dto.InvoiceResponse) (map[string][]dto.SourceUsageItem, error) {
	s.Logger.Infow("calculating price breakdown for invoice",
		"invoice_id", inv.ID,
		"period_start", inv.PeriodStart,
		"period_end", inv.PeriodEnd,
		"line_items_count", len(inv.LineItems))

	// Step 1: Get the line items which are metered (usage-based)
	usageBasedLineItems := make([]*dto.InvoiceLineItemResponse, 0)
	for _, lineItem := range inv.LineItems {
		if lineItem.PriceType != nil && *lineItem.PriceType == string(types.PRICE_TYPE_USAGE) {
			usageBasedLineItems = append(usageBasedLineItems, lineItem)
		}
	}

	s.Logger.Infow("found usage-based line items",
		"total_line_items", len(inv.LineItems),
		"usage_based_line_items", len(usageBasedLineItems))

	if len(usageBasedLineItems) == 0 {
		// No usage-based line items, return empty analytics
		return make(map[string][]dto.SourceUsageItem), nil
	}

	// OPTIMIZED: Use single ClickHouse call to get all analytics data grouped by source and feature_id
	return s.getBulkUsageAnalyticsForInvoice(ctx, usageBasedLineItems, inv)
}

func (s *invoiceService) ListInvoices(ctx context.Context, filter *types.InvoiceFilter) (*dto.ListInvoicesResponse, error) {
	if filter.GetLimit() == 0 {
		filter.Limit = lo.ToPtr(types.GetDefaultFilter().Limit)
	}
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

	if inv.Total.IsZero() {
		inv.PaymentStatus = types.PaymentStatusSucceeded
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

func (s *invoiceService) ProcessDraftInvoice(ctx context.Context, id string, paymentParams *dto.PaymentParameters, sub *subscription.Subscription, flowType types.InvoiceFlowType) error {
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

	// try to process payment for the invoice based on behavior and log any errors
	// Pass the subscription object to avoid extra DB call
	// Error handling logic is properly handled in attemptPaymentForSubscriptionInvoice
	if err := s.attemptPaymentForSubscriptionInvoice(ctx, inv, paymentParams, sub, flowType); err != nil {
		// Only return error if it's a blocking error (e.g., subscription creation with error_if_incomplete)
		return err
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

	// Validate that there shouldnt be any payments for this invoice (for manual updates)
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

// ReconcilePaymentStatus updates the invoice payment status and amounts for payment reconciliation
// This method bypasses the payment record validation since it's called during payment processing
func (s *invoiceService) ReconcilePaymentStatus(ctx context.Context, id string, status types.PaymentStatus, amount *decimal.Decimal) error {
	inv, err := s.InvoiceRepo.Get(ctx, id)
	if err != nil {
		return err
	}

	// Validate the invoice status
	allowedInvoiceStatuses := []types.InvoiceStatus{
		types.InvoiceStatusDraft, //check should we allow draft status as we dont allow payment to be take for draft invoices oayment can only be done for finzalized invoices
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
			inv.AmountPaid = inv.AmountPaid.Add(*amount)
			inv.AmountRemaining = inv.AmountDue.Sub(inv.AmountPaid)
		}
	case types.PaymentStatusSucceeded:
		if amount != nil {
			inv.AmountPaid = inv.AmountPaid.Add(*amount)
		} else {
			inv.AmountPaid = inv.AmountDue
		}

		// Check if invoice is overpaid
		if inv.AmountPaid.GreaterThan(inv.AmountDue) {
			inv.PaymentStatus = types.PaymentStatusOverpaid
			// For overpaid invoices, amount_remaining is always 0
			inv.AmountRemaining = decimal.Zero
		} else {
			inv.AmountRemaining = inv.AmountDue.Sub(inv.AmountPaid)
		}

		inv.PaidAt = &now

		// Check if this is the first invoice (billing_reason = subscription_create)
		if inv.BillingReason == string(types.InvoiceBillingReasonSubscriptionCreate) {
			s.HandleIncompleteSubscriptionPayment(ctx, inv)
		}

	case types.PaymentStatusOverpaid:
		// Handle additional payments to an already overpaid invoice
		if amount != nil {
			inv.AmountPaid = inv.AmountPaid.Add(*amount)
		}
		// For overpaid invoices, amount_remaining is always 0
		inv.AmountRemaining = decimal.Zero
		// Status remains OVERPAID
		if inv.PaidAt == nil {
			inv.PaidAt = &now
		}
		// Check if this is the first invoice (billing_reason = subscription_create)
		if inv.BillingReason == string(types.InvoiceBillingReasonSubscriptionCreate) {
			s.HandleIncompleteSubscriptionPayment(ctx, inv)
		}
	case types.PaymentStatusFailed:
		// Don't change amount_paid for failed payments
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

func (s *invoiceService) CreateSubscriptionInvoice(ctx context.Context, req *dto.CreateSubscriptionInvoiceRequest, paymentParams *dto.PaymentParameters, flowType types.InvoiceFlowType) (*dto.InvoiceResponse, *subscription.Subscription, error) {
	s.Logger.Infow("creating subscription invoice",
		"subscription_id", req.SubscriptionID,
		"period_start", req.PeriodStart,
		"period_end", req.PeriodEnd,
		"reference_point", req.ReferencePoint)

	if err := req.Validate(); err != nil {
		return nil, nil, err
	}

	billingService := NewBillingService(s.ServiceParams)

	// Get subscription with line items
	subscription, _, err := s.SubRepo.GetWithLineItems(ctx, req.SubscriptionID)
	if err != nil {
		return nil, nil, err
	}

	// Prepare invoice request using billing service
	invoiceReq, err := billingService.PrepareSubscriptionInvoiceRequest(ctx,
		subscription,
		req.PeriodStart,
		req.PeriodEnd,
		req.ReferencePoint,
	)
	if err != nil {
		return nil, nil, err
	}

	// Check if the invoice is zeroAmountInvoice
	if invoiceReq.Subtotal.IsZero() {
		return nil, subscription, nil
	}

	s.Logger.Infow("prepared invoice request for subscription",
		"invoice_request", invoiceReq)

	// Create the invoice
	inv, err := s.CreateInvoice(ctx, *invoiceReq)
	if err != nil {
		return nil, nil, err
	}

	// Process the invoice with payment behavior, passing subscription to avoid extra DB call
	if err := s.ProcessDraftInvoice(ctx, inv.ID, paymentParams, subscription, flowType); err != nil {
		return nil, nil, err
	}

	return inv, subscription, nil
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

	s.Logger.Infow("prepared invoice request for preview",
		"invoice_request", invReq)

	// Create a draft invoice object for preview; ToInvoice applies preview discounts and taxes
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

func (s *invoiceService) GetUnpaidInvoicesToBePaid(ctx context.Context, customerID string, currency string) ([]*dto.InvoiceResponse, decimal.Decimal, error) {
	unpaidInvoices := make([]*dto.InvoiceResponse, 0)
	unpaidAmount := decimal.Zero

	filter := types.NewNoLimitInvoiceFilter()
	filter.QueryFilter.Status = lo.ToPtr(types.StatusPublished)
	filter.CustomerID = customerID
	filter.InvoiceStatus = []types.InvoiceStatus{types.InvoiceStatusFinalized}
	filter.SkipLineItems = true

	invoicesResp, err := s.ListInvoices(ctx, filter)
	if err != nil {
		return nil, decimal.Zero, err
	}

	for _, inv := range invoicesResp.Items {
		// filter by currency
		if !types.IsMatchingCurrency(inv.Currency, currency) {
			continue
		}

		if inv.AmountRemaining.IsZero() {
			continue
		}

		// Skip paid and void invoices
		if inv.PaymentStatus == types.PaymentStatusSucceeded {
			continue
		}

		unpaidInvoices = append(unpaidInvoices, inv)
		unpaidAmount = unpaidAmount.Add(inv.AmountRemaining)
	}

	return unpaidInvoices, unpaidAmount, nil
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
			types.PaymentStatusOverpaid,
			types.PaymentStatusFailed,
		},
		types.PaymentStatusSucceeded: {
			types.PaymentStatusSucceeded,
			types.PaymentStatusOverpaid,
		},
		types.PaymentStatusOverpaid: {
			types.PaymentStatusOverpaid,
		},
		types.PaymentStatusFailed: {
			types.PaymentStatusPending,
			types.PaymentStatusFailed,
			types.PaymentStatusSucceeded,
			types.PaymentStatusOverpaid,
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

	// Use the new payment function with nil parameters to use subscription defaults
	return s.attemptPaymentForSubscriptionInvoice(ctx, inv, nil, nil, types.InvoiceFlowManual)
}

func (s *invoiceService) attemptPaymentForSubscriptionInvoice(ctx context.Context, inv *invoice.Invoice, paymentParams *dto.PaymentParameters, sub *subscription.Subscription, flowType types.InvoiceFlowType) error {
	// Get subscription to access payment settings if not provided
	if sub == nil && inv.SubscriptionID != nil {
		var err error
		sub, err = s.SubRepo.Get(ctx, *inv.SubscriptionID)
		if err != nil {
			s.Logger.Errorw("failed to get subscription for payment processing",
				"error", err,
				"subscription_id", *inv.SubscriptionID,
				"invoice_id", inv.ID)
			return err
		}
	}

	// Use parameters if provided, otherwise get from subscription
	var finalPaymentBehavior types.PaymentBehavior

	if paymentParams != nil && paymentParams.PaymentBehavior != nil {
		finalPaymentBehavior = *paymentParams.PaymentBehavior
	} else if sub != nil {
		finalPaymentBehavior = types.PaymentBehavior(sub.PaymentBehavior)
	} else {
		finalPaymentBehavior = types.PaymentBehaviorDefaultActive // default
	}

	// Handle payment based on collection method and payment behavior
	if sub != nil {
		paymentProcessor := NewSubscriptionPaymentProcessor(&s.ServiceParams)

		// Create invoice response for payment processing
		invoiceResponse := &dto.InvoiceResponse{
			ID:              inv.ID,
			AmountDue:       inv.AmountDue,
			AmountRemaining: inv.AmountRemaining,
			CustomerID:      inv.CustomerID,
			Currency:        inv.Currency,
			PaymentStatus:   inv.PaymentStatus,
		}

		// Delegate all payment behavior handling to the payment processor
		err := paymentProcessor.HandlePaymentBehavior(ctx, sub, invoiceResponse, finalPaymentBehavior, flowType)
		if err != nil {
			s.Logger.Errorw("failed to process payment for subscription invoice",
				"error", err.Error(),
				"invoice_id", inv.ID,
				"subscription_id", sub.ID,
				"flow_type", flowType)

			// For subscription creation flow, apply full payment behavior logic
			if flowType == types.InvoiceFlowSubscriptionCreation {
				// For error_if_incomplete behavior, payment failure should block invoice processing
				shouldReturnError := false
				if paymentParams != nil && paymentParams.PaymentBehavior != nil &&
					*paymentParams.PaymentBehavior == types.PaymentBehaviorErrorIfIncomplete {
					shouldReturnError = true
				} else if sub.PaymentBehavior == string(types.PaymentBehaviorErrorIfIncomplete) {
					shouldReturnError = true
				}

				if shouldReturnError {
					return err
				}
			}

			// For renewal flows (InvoiceFlowRenewal), manual flows, or cancel flows, payment failure is not a blocker
			// The invoice will remain in pending state and can be retried later
			s.Logger.Infow("payment failed but continuing with invoice processing for flow type",
				"invoice_id", inv.ID,
				"subscription_id", sub.ID,
				"flow_type", flowType,
				"error", err.Error())
		}
	} else if inv.AmountDue.GreaterThan(decimal.Zero) {
		// For non-subscription invoices, validate and use credits payment logic
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

		// Use credits payment logic
		paymentProcessor := NewSubscriptionPaymentProcessor(&s.ServiceParams)

		// Create invoice response for payment processing
		invoiceResponse := &dto.InvoiceResponse{
			ID:              inv.ID,
			AmountDue:       inv.AmountDue,
			AmountRemaining: inv.AmountRemaining,
			CustomerID:      inv.CustomerID,
			Currency:        inv.Currency,
			PaymentStatus:   inv.PaymentStatus,
		}

		amountPaid := paymentProcessor.ProcessCreditsPaymentForInvoice(ctx, invoiceResponse, nil)
		if amountPaid.GreaterThan(decimal.Zero) {
			s.Logger.Infow("credits payment successful for non-subscription invoice",
				"invoice_id", inv.ID,
				"amount_paid", amountPaid)
		} else {
			s.Logger.Infow("no credits payment made for non-subscription invoice",
				"invoice_id", inv.ID,
				"amount_due", inv.AmountDue)
		}
	}

	return nil
}

func (s *invoiceService) GetInvoicePDFUrl(ctx context.Context, id string) (string, error) {

	// get invoice
	inv, err := s.InvoiceRepo.Get(ctx, id)
	if err != nil {
		return "", err
	}

	if inv.InvoicePDFURL != nil {
		return lo.FromPtr(inv.InvoicePDFURL), nil
	}

	if s.S3 == nil {
		return "", ierr.NewError("s3 is not enabled").
			WithHint("s3 is not enabled but is required to generate invoice pdf url.").
			Mark(ierr.ErrSystem)
	}

	key := fmt.Sprintf("%s/%s", inv.TenantID, id)

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
	s.Logger.Infow("attempting to publish webhook event",
		"webhook_id", webhookEvent.ID,
		"event_name", eventName,
		"invoice_id", invoiceID,
		"tenant_id", webhookEvent.TenantID,
		"environment_id", webhookEvent.EnvironmentID,
	)

	if err := s.WebhookPublisher.PublishWebhook(ctx, webhookEvent); err != nil {
		s.Logger.Errorw("failed to publish webhook event",
			"error", err,
			"webhook_id", webhookEvent.ID,
			"event_name", eventName,
			"invoice_id", invoiceID,
		)
		return
	}

	s.Logger.Infow("webhook event published successfully",
		"webhook_id", webhookEvent.ID,
		"event_name", eventName,
		"invoice_id", invoiceID,
	)
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
				EntityID:        lineItemReq.EntityID,
				EntityType:      lineItemReq.EntityType,
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
		if err := s.RecalculateTaxesOnInvoice(txCtx, inv); err != nil {
			return err
		}

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
	// Only apply taxes to subscription invoices
	if inv.InvoiceType != types.InvoiceTypeSubscription || inv.SubscriptionID == nil {
		return nil
	}

	// Create a minimal request with subscription ID for tax preparation
	// This follows the principle of passing only what's needed
	req := dto.CreateInvoiceRequest{
		SubscriptionID: inv.SubscriptionID,
		CustomerID:     inv.CustomerID,
	}

	// Use tax service to prepare and apply taxes
	taxService := NewTaxService(s.ServiceParams)

	// Prepare tax rates for the invoice
	taxRates, err := taxService.PrepareTaxRatesForInvoice(ctx, req)
	if err != nil {
		s.Logger.Errorw("failed to prepare tax rates for invoice",
			"error", err,
			"invoice_id", inv.ID,
			"subscription_id", *inv.SubscriptionID)
		return err
	}

	// Apply taxes to the invoice
	taxResult, err := taxService.ApplyTaxesOnInvoice(ctx, inv, taxRates)
	if err != nil {
		return err
	}

	// Update the invoice with calculated tax amounts
	inv.TotalTax = taxResult.TotalTaxAmount
	// Discount-first-then-tax: total = subtotal - discount + tax
	inv.Total = inv.Subtotal.Sub(inv.TotalDiscount).Add(taxResult.TotalTaxAmount)
	if inv.Total.IsNegative() {
		inv.Total = decimal.Zero
	}

	// Update the invoice in the database
	if err := s.InvoiceRepo.Update(ctx, inv); err != nil {
		s.Logger.Errorw("failed to update invoice with tax amounts",
			"error", err,
			"invoice_id", inv.ID,
			"total_tax", taxResult.TotalTaxAmount,
			"new_total", inv.Total)
		return err
	}

	return nil
}

// applyCouponsToInvoiceWithLineItems handles both invoice-level and line item-level coupon application
func (s *invoiceService) applyCouponsToInvoiceWithLineItems(ctx context.Context, inv *invoice.Invoice, req dto.CreateInvoiceRequest) error {

	if len(req.InvoiceCoupons) == 0 && len(req.LineItemCoupons) == 0 {
		return nil
	}

	// Use coupon service to prepare and apply coupons
	couponApplicationService := NewCouponApplicationService(s.ServiceParams)

	// Apply both invoice-level and line item-level coupons
	couponResult, err := couponApplicationService.ApplyCouponsOnInvoiceWithLineItems(ctx, inv, req.InvoiceCoupons, req.LineItemCoupons)
	if err != nil {
		return err
	}

	// Update the invoice with calculated discount amounts
	inv.TotalDiscount = couponResult.TotalDiscountAmount

	// Calculate new total based on subtotal - discount (discount-first approach)
	// This ensures consistency with tax calculation which uses subtotal - discount
	originalSubtotal := inv.Subtotal
	newTotal := originalSubtotal.Sub(couponResult.TotalDiscountAmount)

	// Ensure total doesn't go negative
	if newTotal.LessThan(decimal.Zero) {
		s.Logger.Warnw("discount amount exceeds invoice subtotal, capping at zero",
			"invoice_id", inv.ID,
			"original_subtotal", originalSubtotal,
			"total_discount", couponResult.TotalDiscountAmount,
			"calculated_total", newTotal)
		newTotal = decimal.Zero
		// Adjust the total discount to not exceed the original subtotal
		inv.TotalDiscount = originalSubtotal
	}

	inv.Total = newTotal

	// Update AmountDue and AmountRemaining to reflect new total
	inv.AmountDue = newTotal
	inv.AmountRemaining = newTotal.Sub(inv.AmountPaid)

	s.Logger.Infow("successfully updated invoice with coupon discounts (including line items)",
		"invoice_id", inv.ID,
		"total_discount", couponResult.TotalDiscountAmount,
		"invoice_level_coupons", len(req.InvoiceCoupons),
		"line_item_level_coupons", len(req.LineItemCoupons),
		"new_total", inv.Total)

	return nil
}

// HandleTaxRateOverrides is deprecated. Use prepared tax rates passed via dto.CreateInvoiceRequest or
// resolve and apply taxes inline in CreateInvoice using TaxService.
func (s *invoiceService) handleTaxRateOverrides(ctx context.Context, inv *invoice.Invoice, req dto.CreateInvoiceRequest) error {
	if len(req.PreparedTaxRates) == 0 {
		return nil
	}

	s.Logger.Infow("applying taxes to invoice",
		"invoice_id", inv.ID,
		"subscription_id", inv.SubscriptionID,
		"customer_id", inv.CustomerID,
		"period_start", inv.PeriodStart,
		"period_end", inv.PeriodEnd,
	)
	taxService := NewTaxService(s.ServiceParams)
	taxRates := req.PreparedTaxRates
	taxResult, err := taxService.ApplyTaxesOnInvoice(ctx, inv, taxRates)
	if err != nil {
		return err
	}
	inv.TotalTax = taxResult.TotalTaxAmount
	// Discount-first-then-tax: total = subtotal - discount + tax
	inv.Total = inv.Subtotal.Sub(inv.TotalDiscount).Add(taxResult.TotalTaxAmount)
	if inv.Total.IsNegative() {
		inv.Total = decimal.Zero
	}
	inv.AmountDue = inv.Total
	inv.AmountRemaining = inv.Total.Sub(inv.AmountPaid)
	return nil
}

func (s *invoiceService) UpdateInvoice(ctx context.Context, id string, req dto.UpdateInvoiceRequest) (*dto.InvoiceResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Get the existing invoice
	inv, err := s.InvoiceRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Only allow updates for draft or finalized invoices
	if inv.InvoiceStatus != types.InvoiceStatusDraft && inv.InvoiceStatus != types.InvoiceStatusFinalized {
		return nil, ierr.NewError("cannot update invoice in current status").
			WithHint("Invoice can only be updated when in draft or finalized status").
			WithReportableDetails(map[string]any{
				"invoice_id":     id,
				"current_status": inv.InvoiceStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	// For paid invoices, only allow updates to safe fields (PDF URL and due date)
	if inv.PaymentStatus == types.PaymentStatusSucceeded {
		if !isSafeUpdateForPaidInvoice(req) {
			return nil, ierr.NewError("cannot update paid invoice").
				WithHint("Only PDF URL and due date can be updated for paid invoices").
				WithReportableDetails(map[string]any{
					"invoice_id":     id,
					"payment_status": inv.PaymentStatus,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	// Update invoice PDF URL if provided
	if req.InvoicePDFURL != nil {
		inv.InvoicePDFURL = req.InvoicePDFURL
	}

	// Update due date if provided
	if req.DueDate != nil {
		inv.DueDate = req.DueDate
		inv.UpdatedAt = time.Now().UTC()
	}

	// Update the invoice in the repository
	if err := s.InvoiceRepo.Update(ctx, inv); err != nil {
		return nil, err
	}

	// Publish webhook event for invoice update
	s.publishInternalWebhookEvent(ctx, types.WebhookEventInvoiceUpdate, id)

	// Return the updated invoice
	return s.GetInvoice(ctx, id)
}

func (s *invoiceService) TriggerCommunication(ctx context.Context, id string) error {
	// Get invoice to verify it exists
	inv, err := s.InvoiceRepo.Get(ctx, id)
	if err != nil {
		return err
	}

	// Publish webhook event
	s.publishInternalWebhookEvent(ctx, types.WebhookEventInvoiceCommunicationTriggered, inv.ID)
	return nil
}

// HandleIncompleteSubscriptionPayment checks if the paid invoice is the first invoice for a subscription
// and activates the subscription if it's currently in incomplete status
func (s *invoiceService) HandleIncompleteSubscriptionPayment(ctx context.Context, invoice *invoice.Invoice) error {
	// Only process subscription invoices that are fully paid
	if invoice.SubscriptionID == nil || !invoice.AmountRemaining.IsZero() {
		return nil
	}

	// Check if this is the first invoice (billing_reason = subscription_create)
	if invoice.BillingReason != string(types.InvoiceBillingReasonSubscriptionCreate) {
		return nil
	}

	s.Logger.Infow("processing first invoice payment for subscription activation",
		"invoice_id", invoice.ID,
		"subscription_id", *invoice.SubscriptionID,
		"billing_reason", invoice.BillingReason)

	// Get the subscription service
	subscriptionService := NewSubscriptionService(s.ServiceParams)

	// Activate the incomplete subscription
	err := subscriptionService.ActivateIncompleteSubscription(ctx, *invoice.SubscriptionID)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to activate incomplete subscription after first invoice payment").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": *invoice.SubscriptionID,
				"invoice_id":      invoice.ID,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	s.Logger.Infow("successfully activated subscription after first invoice payment",
		"invoice_id", invoice.ID,
		"subscription_id", *invoice.SubscriptionID)

	return nil
}

// convertProrationToLineItems converts proration results to invoice line items
func (s *invoiceService) convertProrationToLineItems(prorationResult *dto.ProrationResult) ([]dto.CreateInvoiceLineItemRequest, error) {
	var lineItems []dto.CreateInvoiceLineItemRequest

	for lineItemID, result := range prorationResult.LineItemResults {
		// Process credit items
		for _, creditItem := range result.CreditItems {
			lineItem := dto.CreateInvoiceLineItemRequest{
				EntityID:    &lineItemID,
				EntityType:  lo.ToPtr("subscription_line_item"),
				PriceID:     &creditItem.PriceID,
				DisplayName: &creditItem.Description,
				Amount:      creditItem.Amount, // Already negative for credits
				Quantity:    creditItem.Quantity,
				PeriodStart: &creditItem.StartDate,
				PeriodEnd:   &creditItem.EndDate,
				Metadata: types.Metadata{
					"proration_type":    "credit",
					"line_item_id":      lineItemID,
					"original_price_id": creditItem.PriceID,
				},
			}
			lineItems = append(lineItems, lineItem)
		}

		// Process charge items
		for _, chargeItem := range result.ChargeItems {
			lineItem := dto.CreateInvoiceLineItemRequest{
				EntityID:    &lineItemID,
				EntityType:  lo.ToPtr("subscription_line_item"),
				PriceID:     &chargeItem.PriceID,
				DisplayName: &chargeItem.Description,
				Amount:      chargeItem.Amount, // Positive for charges
				Quantity:    chargeItem.Quantity,
				PeriodStart: &chargeItem.StartDate,
				PeriodEnd:   &chargeItem.EndDate,
				Metadata: types.Metadata{
					"proration_type":    "charge",
					"line_item_id":      lineItemID,
					"original_price_id": chargeItem.PriceID,
				},
			}
			lineItems = append(lineItems, lineItem)
		}
	}

	return lineItems, nil
}

// generateProrationInvoiceDescription creates a description for proration invoices
func (s *invoiceService) generateProrationInvoiceDescription(cancellationType, cancellationReason string, totalAmount decimal.Decimal) string {
	if totalAmount.IsNegative() {
		// Credit invoice
		switch cancellationType {
		case "immediate":
			return fmt.Sprintf("Credit for unused time - immediate cancellation (%s)", cancellationReason)
		case "specific_date":
			return fmt.Sprintf("Credit for unused time - scheduled cancellation (%s)", cancellationReason)
		default:
			return fmt.Sprintf("Cancellation credit (%s)", cancellationReason)
		}
	} else {
		// Charge invoice (rare for cancellations, but possible)
		return fmt.Sprintf("Proration charges - cancellation (%s)", cancellationReason)
	}
}

// CalculateUsageBreakdown provides flexible usage breakdown with custom grouping
func (s *invoiceService) CalculateUsageBreakdown(ctx context.Context, inv *dto.InvoiceResponse, groupBy []string) (map[string][]dto.UsageBreakdownItem, error) {
	s.Logger.Infow("calculating usage breakdown for invoice",
		"invoice_id", inv.ID,
		"period_start", inv.PeriodStart,
		"period_end", inv.PeriodEnd,
		"line_items_count", len(inv.LineItems),
		"group_by", groupBy)

	// Validate groupBy parameter
	if len(groupBy) == 0 {
		return make(map[string][]dto.UsageBreakdownItem), nil
	}

	// Step 1: Get the line items which are metered (usage-based)
	usageBasedLineItems := make([]*dto.InvoiceLineItemResponse, 0)
	for _, lineItem := range inv.LineItems {
		if lineItem.PriceType != nil && *lineItem.PriceType == string(types.PRICE_TYPE_USAGE) {
			usageBasedLineItems = append(usageBasedLineItems, lineItem)
		}
	}

	s.Logger.Infow("found usage-based line items",
		"total_line_items", len(inv.LineItems),
		"usage_based_line_items", len(usageBasedLineItems))

	if len(usageBasedLineItems) == 0 {
		// No usage-based line items, return empty analytics
		return make(map[string][]dto.UsageBreakdownItem), nil
	}

	// Use flexible grouping analytics call
	return s.getFlexibleUsageBreakdownForInvoice(ctx, usageBasedLineItems, inv, groupBy)
}

// getFlexibleUsageBreakdownForInvoice gets usage breakdown with flexible grouping for invoice line items
// Groups line items by their billing periods for efficient analytics queries
func (s *invoiceService) getFlexibleUsageBreakdownForInvoice(ctx context.Context, usageBasedLineItems []*dto.InvoiceLineItemResponse, inv *dto.InvoiceResponse, groupBy []string) (map[string][]dto.UsageBreakdownItem, error) {
	// Step 1: Get customer external ID first
	customer, err := s.CustomerRepo.Get(ctx, inv.CustomerID)
	if err != nil {
		s.Logger.Errorw("failed to get customer for flexible usage breakdown",
			"customer_id", inv.CustomerID,
			"error", err)
		return nil, err
	}

	// Step 2: Batch feature retrieval for all line items
	meterIDs := make([]string, 0, len(usageBasedLineItems))
	meterToLineItemMap := make(map[string][]*dto.InvoiceLineItemResponse) // meterID -> list of line items using this meter
	lineItemMetadata := make(map[string]*dto.InvoiceLineItemResponse)     // lineItemID -> lineItem

	// First pass: collect all unique meter IDs and build mappings
	for _, lineItem := range usageBasedLineItems {
		// Skip if essential fields are missing
		if lineItem.PriceID == nil || lineItem.MeterID == nil {
			s.Logger.Warnw("skipping line item with missing price_id or meter_id",
				"line_item_id", lineItem.ID,
				"price_id", lineItem.PriceID,
				"meter_id", lineItem.MeterID)
			continue
		}

		meterID := *lineItem.MeterID
		lineItemMetadata[lineItem.ID] = lineItem

		// Add to meter mapping
		if meterToLineItemMap[meterID] == nil {
			meterToLineItemMap[meterID] = make([]*dto.InvoiceLineItemResponse, 0)
			meterIDs = append(meterIDs, meterID) // Only add unique meter IDs
		}
		meterToLineItemMap[meterID] = append(meterToLineItemMap[meterID], lineItem)
	}

	if len(meterIDs) == 0 {
		s.Logger.Warnw("no valid meter IDs found")
		return make(map[string][]dto.UsageBreakdownItem), nil
	}

	// Batch feature retrieval for all meters at once
	featureFilter := types.NewNoLimitFeatureFilter()
	featureFilter.MeterIDs = meterIDs
	features, err := s.FeatureRepo.List(ctx, featureFilter)
	if err != nil {
		s.Logger.Errorw("failed to get features for meters",
			"meter_ids_count", len(meterIDs),
			"error", err)
		return nil, err
	}

	// Build meterID -> featureID mapping
	meterToFeatureMap := make(map[string]string) // meterID -> featureID
	for _, feature := range features {
		meterToFeatureMap[feature.MeterID] = feature.ID
	}

	s.Logger.Infow("batched feature retrieval",
		"invoice_id", inv.ID,
		"total_meters", len(meterIDs),
		"features_found", len(features))

	// Step 3: Group line items by their billing periods using feature mapping
	type PeriodKey struct {
		Start time.Time
		End   time.Time
	}

	periodGroups := make(map[PeriodKey][]*dto.InvoiceLineItemResponse)
	lineItemToFeatureMap := make(map[string]string) // lineItemID -> featureID

	for _, lineItem := range usageBasedLineItems {
		// Skip if no meter ID or feature mapping not found
		if lineItem.MeterID == nil {
			continue
		}

		featureID, exists := meterToFeatureMap[*lineItem.MeterID]
		if !exists {
			s.Logger.Warnw("no feature found for meter",
				"meter_id", *lineItem.MeterID,
				"line_item_id", lineItem.ID)
			continue
		}

		lineItemToFeatureMap[lineItem.ID] = featureID

		// Determine the billing period for this line item
		var periodStart, periodEnd time.Time

		if lineItem.PeriodStart != nil && lineItem.PeriodEnd != nil {
			// Use line item specific period
			periodStart = *lineItem.PeriodStart
			periodEnd = *lineItem.PeriodEnd
		} else if inv.PeriodStart != nil && inv.PeriodEnd != nil {
			// Fall back to invoice period
			periodStart = *inv.PeriodStart
			periodEnd = *inv.PeriodEnd
		} else {
			s.Logger.Warnw("missing period information for line item",
				"line_item_id", lineItem.ID,
				"invoice_id", inv.ID)
			continue
		}

		periodKey := PeriodKey{Start: periodStart, End: periodEnd}
		if periodGroups[periodKey] == nil {
			periodGroups[periodKey] = make([]*dto.InvoiceLineItemResponse, 0)
		}
		periodGroups[periodKey] = append(periodGroups[periodKey], lineItem)
	}

	if len(periodGroups) == 0 {
		s.Logger.Warnw("no valid line items found with periods")
		return make(map[string][]dto.UsageBreakdownItem), nil
	}

	s.Logger.Infow("grouped line items by periods",
		"invoice_id", inv.ID,
		"period_groups_count", len(periodGroups),
		"total_line_items", len(usageBasedLineItems))

	// Step 3: Make analytics requests for each period group
	allAnalyticsItems := make([]dto.UsageAnalyticItem, 0)
	eventPostProcessingService := NewEventPostProcessingService(s.ServiceParams, s.EventRepo, s.ProcessedEventRepo)

	for periodKey, lineItemsInPeriod := range periodGroups {
		// Collect feature IDs for this period
		featureIDsForPeriod := make([]string, 0, len(lineItemsInPeriod))
		for _, lineItem := range lineItemsInPeriod {
			if featureID, exists := lineItemToFeatureMap[lineItem.ID]; exists {
				featureIDsForPeriod = append(featureIDsForPeriod, featureID)
			}
		}

		if len(featureIDsForPeriod) == 0 {
			continue
		}

		// Make analytics request for this period
		analyticsReq := &dto.GetUsageAnalyticsRequest{
			ExternalCustomerID: customer.ExternalID,
			FeatureIDs:         featureIDsForPeriod,
			StartTime:          periodKey.Start,
			EndTime:            periodKey.End,
			GroupBy:            groupBy,
		}

		s.Logger.Infow("making period-specific analytics request",
			"invoice_id", inv.ID,
			"period_start", periodKey.Start.Format(time.RFC3339),
			"period_end", periodKey.End.Format(time.RFC3339),
			"feature_ids_count", len(featureIDsForPeriod),
			"line_items_count", len(lineItemsInPeriod),
			"group_by", groupBy)

		analyticsResponse, err := eventPostProcessingService.GetDetailedUsageAnalytics(ctx, analyticsReq)
		if err != nil {
			s.Logger.Errorw("failed to get period-specific usage analytics",
				"invoice_id", inv.ID,
				"period_start", periodKey.Start.Format(time.RFC3339),
				"period_end", periodKey.End.Format(time.RFC3339),
				"error", err)
			return nil, err
		}

		// Collect all analytics items
		allAnalyticsItems = append(allAnalyticsItems, analyticsResponse.Items...)

		s.Logger.Debugw("retrieved period-specific analytics",
			"invoice_id", inv.ID,
			"period_start", periodKey.Start.Format(time.RFC3339),
			"period_end", periodKey.End.Format(time.RFC3339),
			"analytics_items_count", len(analyticsResponse.Items))
	}

	// Step 4: Create combined response and map to line items
	combinedResponse := &dto.GetUsageAnalyticsResponse{
		Items: allAnalyticsItems,
	}

	s.Logger.Infow("combined all period analytics",
		"invoice_id", inv.ID,
		"total_analytics_items", len(allAnalyticsItems))

	// Step 5: Map results back to line items with flexible grouping
	return s.mapFlexibleAnalyticsToLineItems(ctx, combinedResponse, lineItemToFeatureMap, lineItemMetadata, groupBy)
}

// mapFlexibleAnalyticsToLineItems maps analytics response to line items with flexible grouping
func (s *invoiceService) mapFlexibleAnalyticsToLineItems(ctx context.Context, analyticsResponse *dto.GetUsageAnalyticsResponse, lineItemToFeatureMap map[string]string, lineItemMetadata map[string]*dto.InvoiceLineItemResponse, groupBy []string) (map[string][]dto.UsageBreakdownItem, error) {
	usageBreakdownResponse := make(map[string][]dto.UsageBreakdownItem)

	// Step 1: Group analytics by feature_id for line item mapping
	featureAnalyticsMap := make(map[string][]dto.UsageAnalyticItem) // featureID -> list of analytics items

	for _, analyticsItem := range analyticsResponse.Items {
		if featureAnalyticsMap[analyticsItem.FeatureID] == nil {
			featureAnalyticsMap[analyticsItem.FeatureID] = make([]dto.UsageAnalyticItem, 0)
		}
		featureAnalyticsMap[analyticsItem.FeatureID] = append(featureAnalyticsMap[analyticsItem.FeatureID], analyticsItem)
	}

	// Step 2: Process each line item
	for lineItemID, featureID := range lineItemToFeatureMap {
		lineItem := lineItemMetadata[lineItemID]
		analyticsItems, exists := featureAnalyticsMap[featureID]

		if !exists || len(analyticsItems) == 0 {
			// No usage data for this line item
			s.Logger.Debugw("no usage analytics found for line item",
				"line_item_id", lineItemID,
				"feature_id", featureID)
			usageBreakdownResponse[lineItemID] = []dto.UsageBreakdownItem{}
			continue
		}

		// Step 3: Calculate total usage for this line item across all groups
		totalUsageForLineItem := decimal.Zero
		for _, analyticsItem := range analyticsItems {
			totalUsageForLineItem = totalUsageForLineItem.Add(analyticsItem.TotalUsage)
		}

		// Step 4: Calculate proportional costs for each group
		lineItemUsageBreakdown := make([]dto.UsageBreakdownItem, 0, len(analyticsItems))
		totalLineItemCost := lineItem.Amount

		for _, analyticsItem := range analyticsItems {
			// Calculate proportional cost based on usage
			var cost string
			if !totalLineItemCost.IsZero() && !totalUsageForLineItem.IsZero() {
				proportionalCost := analyticsItem.TotalUsage.Div(totalUsageForLineItem).Mul(totalLineItemCost)
				cost = proportionalCost.StringFixed(2)
			} else {
				cost = "0"
			}

			// Calculate percentage
			var percentage string
			if !totalUsageForLineItem.IsZero() {
				pct := analyticsItem.TotalUsage.Div(totalUsageForLineItem).Mul(decimal.NewFromInt(100))
				percentage = pct.StringFixed(2)
			} else {
				percentage = "0"
			}

			// Build grouped_by map from the analytics item
			groupedBy := make(map[string]string)
			if analyticsItem.FeatureID != "" {
				groupedBy["feature_id"] = analyticsItem.FeatureID
			}
			if analyticsItem.Source != "" {
				groupedBy["source"] = analyticsItem.Source
			}
			// Add properties from the analytics item
			if analyticsItem.Properties != nil {
				for key, value := range analyticsItem.Properties {
					groupedBy[key] = value
				}
			}

			// Create usage breakdown item
			breakdownItem := dto.UsageBreakdownItem{
				Cost:      cost,
				GroupedBy: groupedBy,
			}

			// Add optional fields
			if !analyticsItem.TotalUsage.IsZero() {
				usageStr := analyticsItem.TotalUsage.StringFixed(2)
				breakdownItem.Usage = &usageStr
			}

			if percentage != "0" {
				breakdownItem.Percentage = &percentage
			}

			if analyticsItem.EventCount > 0 {
				eventCount := int(analyticsItem.EventCount)
				breakdownItem.EventCount = &eventCount
			}

			lineItemUsageBreakdown = append(lineItemUsageBreakdown, breakdownItem)
		}

		usageBreakdownResponse[lineItemID] = lineItemUsageBreakdown

		s.Logger.Debugw("mapped flexible usage breakdown for line item",
			"line_item_id", lineItemID,
			"feature_id", featureID,
			"groups_count", len(lineItemUsageBreakdown),
			"total_usage", totalUsageForLineItem.StringFixed(2))
	}

	return usageBreakdownResponse, nil
}

// isSafeUpdateForPaidInvoice checks if the update request contains only safe fields for paid invoices
func isSafeUpdateForPaidInvoice(req dto.UpdateInvoiceRequest) bool {
	// Currently, UpdateInvoiceRequest only contains InvoicePDFURL and DueDate
	// Both of these are considered safe for paid invoices
	// In the future, if more fields are added, they should be categorized here

	// For now, all fields in UpdateInvoiceRequest are safe
	// This function is here for future extensibility
	return true
}
