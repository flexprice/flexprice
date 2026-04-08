package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	pdf "github.com/flexprice/flexprice/internal/domain/pdf"
	domainPrice "github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/idempotency"
	"github.com/flexprice/flexprice/internal/integration/chargebee"
	integrationevents "github.com/flexprice/flexprice/internal/integration/events"
	"github.com/flexprice/flexprice/internal/integration/quickbooks"
	"github.com/flexprice/flexprice/internal/integration/razorpay"
	"github.com/flexprice/flexprice/internal/integration/stripe"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/s3"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type InvoiceService interface {
	// Embed the basic interface from interfaces package
	interfaces.InvoiceService

	// Additional methods specific to this service
	CreateOneOffInvoice(ctx context.Context, req dto.CreateInvoiceRequest) (*dto.InvoiceResponse, error)
	CreateEmptyDraftInvoice(ctx context.Context, req dto.CreateDraftInvoiceRequest) (*dto.InvoiceResponse, error)
	FinalizeInvoice(ctx context.Context, id string) error
	VoidInvoice(ctx context.Context, id string, req dto.InvoiceVoidRequest) error
	ProcessDraftInvoice(ctx context.Context, id string, paymentParams *dto.PaymentParameters, sub *subscription.Subscription, flowType types.InvoiceFlowType) error
	UpdatePaymentStatus(ctx context.Context, id string, status types.PaymentStatus, amount *decimal.Decimal) error
	CreateSubscriptionInvoice(ctx context.Context, req *dto.CreateSubscriptionInvoiceRequest, paymentParams *dto.PaymentParameters, flowType types.InvoiceFlowType, isDraftSubscription bool) (*dto.InvoiceResponse, *subscription.Subscription, error)
	CreateDraftInvoiceForSubscription(ctx context.Context, subscriptionID string, periodStart, periodEnd time.Time, referencePoint types.InvoiceReferencePoint) (*dto.InvoiceResponse, error)
	ComputeInvoice(ctx context.Context, invoiceID string, req *dto.InvoiceComputeRequest) (skipped bool, err error)
	GetPreviewInvoice(ctx context.Context, req dto.GetPreviewInvoiceRequest) (*dto.InvoiceResponse, error)
	GetCustomerInvoiceSummary(ctx context.Context, customerID string, currency string) (*dto.CustomerInvoiceSummary, error)
	GetUnpaidInvoicesToBePaid(ctx context.Context, req dto.GetUnpaidInvoicesToBePaidRequest) (*dto.GetUnpaidInvoicesToBePaidResponse, error)
	GetCustomerMultiCurrencyInvoiceSummary(ctx context.Context, customerID string) (*dto.CustomerMultiCurrencyInvoiceSummary, error)
	AttemptPayment(ctx context.Context, id string) error
	GetInvoicePDF(ctx context.Context, id string) ([]byte, error)
	GetInvoicePDFUrl(ctx context.Context, id string, forceGenerate bool) (string, error)
	RecalculateInvoice(ctx context.Context, id string) (*dto.InvoiceResponse, error)
	RecalculateInvoiceV2(ctx context.Context, id string, finalize bool) (*dto.InvoiceResponse, error)
	RecalculateInvoiceAmounts(ctx context.Context, invoiceID string) error
	CalculatePriceBreakdown(ctx context.Context, inv *dto.InvoiceResponse) (map[string][]dto.SourceUsageItem, error)
	CalculateUsageBreakdown(ctx context.Context, inv *dto.InvoiceResponse, groupBy []string, forceRealtimeRecalculation bool) (map[string][]dto.UsageBreakdownItem, error)
	GetInvoiceWithBreakdown(ctx context.Context, req dto.GetInvoiceWithBreakdownRequest) (*dto.InvoiceResponse, error)
	TriggerCommunication(ctx context.Context, id string) error
	TriggerWebhook(ctx context.Context, invoiceID string, eventName types.WebhookEventName) error
	HandleIncompleteSubscriptionPayment(ctx context.Context, invoice *invoice.Invoice) error

	// Cron methods
	SyncInvoiceToExternalVendors(ctx context.Context, invoiceID string) error
	SyncInvoiceToStripeIfEnabled(ctx context.Context, invoiceID string, collectionMethod string) error
	SyncInvoiceToRazorpayIfEnabled(ctx context.Context, invoiceID string) error
	SyncInvoiceToChargebeeIfEnabled(ctx context.Context, invoiceID string) error
	SyncInvoiceToQuickBooksIfEnabled(ctx context.Context, invoiceID string) error
	IsFinalizationDue(ctx context.Context, invoiceID string) (bool, error)
	ListAllTenantDraftInvoices(ctx context.Context, batchSize, offset int) ([]*invoice.Invoice, error)

	DistributeInvoiceLevelDiscount(ctx context.Context, lineItems []*invoice.InvoiceLineItem, invoiceDiscountAmount decimal.Decimal) error
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

	// Validate coupons
	couponValidationService := NewCouponValidationService(s.ServiceParams)
	validCoupons := make([]dto.InvoiceCoupon, 0)
	for _, couponID := range req.Coupons {
		coupon, err := s.CouponRepo.Get(ctx, couponID)
		if err != nil {
			s.Logger.ErrorwCtx(ctx, "failed to get coupon", "error", err, "coupon_id", couponID)
			continue
		}
		if err := couponValidationService.ValidateCoupon(ctx, *coupon, nil); err != nil {
			s.Logger.ErrorwCtx(ctx, "failed to validate coupon", "error", err, "coupon_id", couponID)
			continue
		}
		validCoupons = append(validCoupons, dto.InvoiceCoupon{
			CouponID: couponID,
		})
	}
	req.InvoiceCoupons = validCoupons

	// Prepare tax rates
	taxService := NewTaxService(s.ServiceParams)
	finalTaxRates := make([]*dto.TaxRateResponse, 0)
	if len(req.TaxRateOverrides) > 0 {
		preparedTaxRates, err := taxService.PrepareTaxRatesForInvoice(ctx, req)
		if err != nil {
			s.Logger.ErrorwCtx(ctx, "failed to prepare tax rates from overrides", "error", err)
			return nil, err
		}
		finalTaxRates = preparedTaxRates
	} else if len(req.TaxRates) > 0 {
		for _, taxRateID := range req.TaxRates {
			tr, err := taxService.GetTaxRate(ctx, taxRateID)
			if err != nil {
				return nil, err
			}
			finalTaxRates = append(finalTaxRates, tr)
		}
	}
	req.PreparedTaxRates = finalTaxRates

	// Delegate to CreateInvoice which handles draft-first flow: create draft, compute, finalize, webhook
	return s.CreateInvoice(ctx, req)
}

// CreateEmptyDraftInvoice creates a zero-dollar draft invoice without line items or invoice number.
// The invoice is created with status DRAFT, zero amounts, and no invoice number assigned.
// Use ComputeInvoice to populate line items (for subscription) and apply coupons/taxes.
// Use FinalizeInvoice to assign the invoice number and seal the invoice.
func (s *invoiceService) CreateEmptyDraftInvoice(ctx context.Context, req dto.CreateDraftInvoiceRequest) (*dto.InvoiceResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	var resp *dto.InvoiceResponse

	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		// 1. Generate idempotency key if not provided
		var idempKey string
		if req.IdempotencyKey == nil {
			periodStart := req.PeriodStart
			periodEnd := req.PeriodEnd

			// To handle potential race conditions and prevent the creation of duplicate subscription invoices
			// (e.g., when multiple cancellation or update requests are processed simultaneously),
			// we truncate the billing period's start and end times to minute-level precision for
			// idempotency key generation. This ensures that any additional requests for the same
			// billing period within the same minute will generate an identical idempotency key,
			// allowing the system to correctly identify and deduplicate them.
			if periodStart != nil && !periodStart.IsZero() {
				t := periodStart.Truncate(time.Minute)
				periodStart = &t
			}
			if periodEnd != nil && !periodEnd.IsZero() {
				t := periodEnd.Truncate(time.Minute)
				periodEnd = &t
			}

			params := map[string]interface{}{
				"tenant_id":      types.GetTenantID(ctx),
				"environment_id": types.GetEnvironmentID(ctx),
				"customer_id":    req.CustomerID,
				"period_start":   periodStart,
				"period_end":     periodEnd,
				// Including a timestamp here would always generate a new idempotency key
				// for the same invoice, so it is intentionally omitted.
				// "timestamp":    time.Now().UTC(),
			}
			scope := idempotency.ScopeOneOffInvoice
			if req.SubscriptionID != nil {
				scope = idempotency.ScopeSubscriptionInvoice
				params["subscription_id"] = *req.SubscriptionID
				params["billing_reason"] = string(req.BillingReason)
			} else {
				// For one-off invoices, a timestamp is required to ensure uniqueness
				params["timestamp"] = time.Now().UTC()
			}
			idempKey = s.idempGen.GenerateKey(scope, params)
		} else {
			idempKey = *req.IdempotencyKey
		}

		// 2. Check for existing invoice with same idempotency key (idempotent draft creation)
		existing, err := s.InvoiceRepo.GetByIdempotencyKey(txCtx, idempKey)
		if err != nil && !ierr.IsNotFound(err) {
			return ierr.WithError(err).WithHint("failed to check idempotency").Mark(ierr.ErrDatabase)
		}
		if existing != nil {
			if existing.InvoiceStatus == types.InvoiceStatusDraft || existing.InvoiceStatus == types.InvoiceStatusSkipped {
				s.Logger.Infow("draft/skipped invoice already exists for idempotency key, returning existing", "invoice_id", existing.ID)
				resp = dto.NewInvoiceResponse(existing)
				return nil
			}
			s.Logger.Infof("invoice already exists, returning existing invoice")
			return ierr.NewError("invoice already exists").WithHint("invoice already exists").Mark(ierr.ErrAlreadyExists)
		}

		// 3. For subscription invoices, check period uniqueness and get billing sequence
		var billingSeq *int
		if req.SubscriptionID != nil {
			existingForPeriod, err := s.InvoiceRepo.GetForPeriod(txCtx, *req.SubscriptionID, *req.PeriodStart, *req.PeriodEnd, string(req.BillingReason))
			if err != nil && !ierr.IsNotFound(err) {
				return err
			}
			if existingForPeriod != nil {
				if existingForPeriod.InvoiceStatus == types.InvoiceStatusDraft || existingForPeriod.InvoiceStatus == types.InvoiceStatusSkipped {
					s.Logger.Infow("draft/skipped invoice already exists for period, returning existing",
						"invoice_id", existingForPeriod.ID,
						"subscription_id", *req.SubscriptionID,
						"period_start", *req.PeriodStart,
						"period_end", *req.PeriodEnd)
					resp = dto.NewInvoiceResponse(existingForPeriod)
					return nil
				}
				s.Logger.Infow("invoice already exists for subscription period",
					"invoice_id", existingForPeriod.ID,
					"subscription_id", *req.SubscriptionID,
					"period_start", *req.PeriodStart,
					"period_end", *req.PeriodEnd)
				return ierr.NewError("invoice already exists").WithHint("invoice already exists for this period").Mark(ierr.ErrAlreadyExists)
			}

			// Get billing sequence
			seq, err := s.InvoiceRepo.GetNextBillingSequence(txCtx, *req.SubscriptionID)
			if err != nil {
				return err
			}
			billingSeq = &seq
		}

		// 4. Create invoice (no invoice number for draft) — ToDraftInvoice returns zero amounts
		inv, err := req.ToDraftInvoice(txCtx)
		if err != nil {
			return err
		}

		inv.IdempotencyKey = &idempKey
		inv.BillingSequence = billingSeq
		inv.InvoiceStatus = types.InvoiceStatusDraft

		// Validate invoice
		if err := inv.Validate(); err != nil {
			return err
		}

		// Create invoice with line items (must use txCtx so it participates in this transaction)
		if err := s.InvoiceRepo.Create(txCtx, inv); err != nil {
			return err
		}

		resp = dto.NewInvoiceResponse(inv)
		return nil
	})

	if err != nil {
		s.Logger.ErrorwCtx(ctx, "failed to create invoice",
			"error", err,
			"customer_id", req.CustomerID,
			"subscription_id", req.SubscriptionID)
		return nil, err
	}

	if resp.InvoiceStatus == types.InvoiceStatusFinalized {
		s.publishSystemEvent(ctx, types.WebhookEventInvoiceUpdateFinalized, resp.ID)
	}

	return resp, nil
}

// This wrapper delegates to the draft-first flow. Invoice number is assigned during FinalizeInvoice.
func (s *invoiceService) CreateInvoice(ctx context.Context, req dto.CreateInvoiceRequest) (*dto.InvoiceResponse, error) {
	// Delegate to draft-first flow
	draftReq := req.ToDraftRequest()
	draft, err := s.CreateEmptyDraftInvoice(ctx, draftReq)
	if err != nil {
		return nil, err
	}

	// Compute to assign invoice number and apply coupons/taxes
	computeReq := req.ToComputeRequest()
	skipped, err := s.ComputeInvoice(ctx, draft.ID, &computeReq)
	if err != nil {
		return nil, err
	}

	// If invoice was skipped (zero-dollar), return it without further processing
	if skipped {
		inv, err := s.InvoiceRepo.Get(ctx, draft.ID)
		if err != nil {
			return nil, err
		}
		return dto.NewInvoiceResponse(inv), nil
	}

	// Check if we need to finalize (original CreateInvoice would auto-finalize for one-off/credit)
	shouldFinalize := req.InvoiceType == types.InvoiceTypeOneOff || req.InvoiceType == types.InvoiceTypeCredit
	if req.InvoiceStatus != nil && *req.InvoiceStatus == types.InvoiceStatusFinalized {
		shouldFinalize = true
	}

	if shouldFinalize {
		if err := s.FinalizeInvoice(ctx, draft.ID); err != nil {
			return nil, err
		}
	}

	// Get the updated invoice
	inv, err := s.InvoiceRepo.Get(ctx, draft.ID)
	if err != nil {
		return nil, err
	}

	return dto.NewInvoiceResponse(inv), nil
}

// CreateDraftInvoiceForSubscription creates a zero-dollar draft invoice without line items for a subscription period.
// No invoice number is assigned. Use ComputeInvoice to populate line items and FinalizeInvoice to assign the number.
func (s *invoiceService) CreateDraftInvoiceForSubscription(ctx context.Context, subscriptionID string, periodStart, periodEnd time.Time, referencePoint types.InvoiceReferencePoint) (*dto.InvoiceResponse, error) {
	sub, _, err := s.SubRepo.GetWithLineItems(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	billingPeriodStr := string(sub.BillingPeriod)
	invoicingCustomerID := sub.GetInvoicingCustomerID()
	req := dto.CreateDraftInvoiceRequest{
		CustomerID:     invoicingCustomerID,
		SubscriptionID: lo.ToPtr(sub.ID),
		InvoiceType:    types.InvoiceTypeSubscription,
		Currency:       sub.Currency,
		BillingPeriod:  &billingPeriodStr,
		PeriodStart:    &periodStart,
		PeriodEnd:      &periodEnd,
		BillingReason:  types.InvoiceBillingReasonSubscriptionCycle,
	}
	if referencePoint == types.ReferencePointCancel {
		req.BillingReason = types.InvoiceBillingReasonProration
	}
	return s.CreateEmptyDraftInvoice(ctx, req)
}

// ComputeInvoice computes a draft (or previously-skipped) invoice: computes line items (subscription),
// applies credits/coupons/taxes, or marks SKIPPED if zero-dollar. Re-runnable on draft and skipped invoices.
// Invoice number is NOT assigned here — it is assigned during FinalizeInvoice.
// For one-off/credit invoices, pass the original request to apply coupons/taxes; for subscription invoices, pass nil.
// Always sets LastComputedAt on successful computation.
//
// Expensive computation (e.g. PrepareSubscriptionInvoiceRequest which queries ClickHouse) is performed
// OUTSIDE the row-level lock to avoid lock timeouts. Only DB writes happen under the lock.
func (s *invoiceService) ComputeInvoice(ctx context.Context, invoiceID string, req *dto.InvoiceComputeRequest) (skipped bool, err error) {
	// 1. Read invoice WITHOUT lock to determine type and gather details for computation.
	//    This avoids holding the row lock during expensive ClickHouse queries.
	inv, err := s.InvoiceRepo.Get(ctx, invoiceID)
	if err != nil {
		return false, err
	}

	// Early return for finalized/voided — no lock needed, these are immutable.
	if inv.InvoiceStatus != types.InvoiceStatusDraft && inv.InvoiceStatus != types.InvoiceStatusSkipped {
		return false, nil
	}

	// 2. Compute the request OUTSIDE the lock (expensive for subscription invoices).
	var applyReq *dto.InvoiceComputeRequest

	if inv.InvoiceType == types.InvoiceTypeSubscription && inv.SubscriptionID != nil {
		// Subscription: compute line items from billing service
		if inv.PeriodStart == nil || inv.PeriodEnd == nil {
			return false, ierr.NewError("subscription invoice missing period dates").
				WithHint("PeriodStart and PeriodEnd are required for subscription invoices").
				WithReportableDetails(map[string]interface{}{
					"invoice_id": inv.ID,
				}).
				Mark(ierr.ErrValidation)
		}
		sub, err := s.SubRepo.Get(ctx, *inv.SubscriptionID)
		if err != nil {
			return false, err
		}
		refPoint := types.ReferencePointPeriodEnd
		if inv.BillingReason == string(types.InvoiceBillingReasonProration) {
			refPoint = types.ReferencePointCancel
		}
		billingService := NewBillingService(s.ServiceParams)
		subInvReq, err := billingService.PrepareSubscriptionInvoiceRequest(ctx, sub, *inv.PeriodStart, *inv.PeriodEnd, refPoint, inv.ID)
		if err != nil {
			return false, err
		}
		computeReq := subInvReq.ToComputeRequest()
		applyReq = &computeReq
	} else if req != nil {
		// One-off or credit: line items and amounts come from the caller's request
		applyReq = req
	}

	// 3. Take the lock — only for DB writes (line items, credits, coupons, taxes, status update).
	err = s.DB.WithTx(ctx, func(txCtx context.Context) error {
		inv, err = s.InvoiceRepo.GetForUpdate(txCtx, invoiceID)
		if err != nil {
			return err
		}

		// Re-check status under lock: allow SKIPPED invoices to be re-computed
		// (usage may have accumulated since the invoice was first marked SKIPPED).
		if inv.InvoiceStatus == types.InvoiceStatusSkipped {
			inv.InvoiceStatus = types.InvoiceStatusDraft
		} else if inv.InvoiceStatus != types.InvoiceStatusDraft {
			// Finalized/voided between our initial read and the lock — nothing to do.
			return nil
		}

		// Populate invoice from the computed request (uniform for all invoice types)
		if applyReq != nil {
			lineItemDomains := make([]*invoice.InvoiceLineItem, 0, len(applyReq.LineItems))
			for _, item := range applyReq.LineItems {
				lineItemDomains = append(lineItemDomains, item.ToInvoiceLineItem(txCtx, inv))
			}

			// Always replace line items on re-compute: remove old, insert new.
			// Amounts/quantities may change between computes even if the count stays the same.
			if len(inv.LineItems) > 0 {
				itemIDs := lo.Map(inv.LineItems, func(item *invoice.InvoiceLineItem, _ int) string { return item.ID })
				if err := s.InvoiceRepo.RemoveLineItems(txCtx, inv.ID, itemIDs); err != nil {
					return err
				}
			}
			if len(lineItemDomains) > 0 {
				if err := s.InvoiceRepo.AddLineItems(txCtx, inv.ID, lineItemDomains); err != nil {
					return err
				}
			}

			inv.Subtotal = applyReq.Subtotal
			inv.Total = applyReq.Total
			inv.AmountDue = applyReq.AmountDue
			inv.AmountRemaining = inv.AmountDue.Sub(inv.AmountPaid)
			inv.Description = applyReq.Description
			inv.DueDate = applyReq.DueDate
			inv.LineItems = lineItemDomains
		}

		if inv.InvoiceType == types.InvoiceTypeSubscription && inv.Subtotal.IsZero() {
			now := time.Now().UTC()
			inv.LastComputedAt = &now
			inv.InvoiceStatus = types.InvoiceStatusSkipped
			if err := s.InvoiceRepo.Update(txCtx, inv); err != nil {
				return err
			}
			skipped = true
			return nil
		}

		// Apply coupons/discounts. For one-off and credit invoices, also apply
		// credits and taxes here because they are created and finalized in one
		// shot and RecalculateTaxesOnInvoice is a no-op for non-subscription
		// invoices. For subscription invoices, credits and taxes are deferred
		// to the finalization step so wallet debits only happen when the
		// invoice is actually sealed.
		if applyReq != nil {
			if inv.InvoiceType == types.InvoiceTypeOneOff || inv.InvoiceType == types.InvoiceTypeCredit {
				// One-off / credit: apply coupons + credits + taxes now
				if err := s.applyCreditsAndCouponsToInvoice(txCtx, inv, *applyReq); err != nil {
					return err
				}
				if err := s.applyTaxesToInvoice(txCtx, inv, *applyReq); err != nil {
					return err
				}
			} else {
				// Subscription: coupons only — credits and taxes deferred to finalization
				if err := s.applyCouponsToInvoice(txCtx, inv, *applyReq); err != nil {
					return err
				}
			}
		}

		now := time.Now().UTC()
		inv.LastComputedAt = &now

		return s.InvoiceRepo.Update(txCtx, inv)
	})
	return skipped, err
}

// getInvoiceWithLineItems fetches an invoice and populates its LineItems from the dedicated repo.
func (s *invoiceService) getInvoiceWithLineItems(ctx context.Context, id string) (*invoice.Invoice, error) {
	inv, err := s.InvoiceRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	lineItems, err := s.InvoiceLineItemRepo.ListByInvoiceID(ctx, id)
	if err != nil {
		return nil, err
	}
	inv.LineItems = lineItems
	return inv, nil
}

func (s *invoiceService) GetInvoice(ctx context.Context, id string) (*dto.InvoiceResponse, error) {
	inv, err := s.getInvoiceWithLineItems(ctx, id)
	if err != nil {
		return nil, err
	}

	for _, lineItem := range inv.LineItems {
		s.Logger.DebugwCtx(ctx, "got invoice line item", "id", lineItem.ID, "display_name", lineItem.DisplayName)
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
			response.WithCustomer(subscription.Customer)
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

	response.WithTaxes(appliedTaxes.Items)

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
			s.Logger.WarnwCtx(ctx, "skipping line item with missing price_id or meter_id",
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
			s.Logger.WarnwCtx(ctx, "no feature found for meter",
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
		s.Logger.WarnwCtx(ctx, "no valid feature IDs found for any line items")
		return make(map[string][]dto.SourceUsageItem), nil
	}

	// Step 2: Get customer external ID
	customer, err := s.CustomerRepo.Get(ctx, inv.CustomerID)
	if err != nil {
		s.Logger.ErrorwCtx(ctx, "failed to get customer for usage analytics",
			"customer_id", inv.CustomerID,
			"error", err)
		return nil, err
	}

	// Step 3: Use invoice period for usage calculation
	periodStart := inv.PeriodStart
	periodEnd := inv.PeriodEnd

	if periodStart == nil || periodEnd == nil {
		s.Logger.WarnwCtx(ctx, "missing period information in invoice",
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

	s.Logger.InfowCtx(ctx, "making bulk analytics request",
		"invoice_id", inv.ID,
		"feature_ids_count", len(featureIDs),
		"customer_id", customer.ExternalID)

	featureUsageTrackingService := NewFeatureUsageTrackingService(s.ServiceParams, s.EventRepo, s.FeatureUsageRepo)
	analyticsResponse, err := featureUsageTrackingService.GetDetailedUsageAnalytics(ctx, analyticsReq)
	if err != nil {
		s.Logger.ErrorwCtx(ctx, "failed to get bulk usage analytics",
			"invoice_id", inv.ID,
			"error", err)
		return nil, err
	}

	s.Logger.InfowCtx(ctx, "retrieved bulk usage analytics",
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
			s.Logger.DebugwCtx(ctx, "no usage analytics found for line item",
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

		s.Logger.DebugwCtx(ctx, "mapped usage analytics for line item",
			"line_item_id", lineItemID,
			"feature_id", featureID,
			"sources_count", len(lineItemUsageAnalytics),
			"total_usage", totalUsageForLineItem.StringFixed(2))
	}

	return usageAnalyticsResponse, nil
}

func (s *invoiceService) CalculatePriceBreakdown(ctx context.Context, inv *dto.InvoiceResponse) (map[string][]dto.SourceUsageItem, error) {
	s.Logger.InfowCtx(ctx, "calculating price breakdown for invoice",
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

	s.Logger.InfowCtx(ctx, "found usage-based line items",
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

	if err := s.performFinalizeInvoiceActions(ctx, inv); err != nil {
		return err
	}

	return nil
}

func (s *invoiceService) performFinalizeInvoiceActions(ctx context.Context, inv *invoice.Invoice) error {
	if inv.InvoiceStatus == types.InvoiceStatusSkipped {
		// No-op: skipped invoices are not finalized
		return nil
	}
	if inv.InvoiceStatus != types.InvoiceStatusDraft {
		return ierr.NewError("invoice is not in draft status").WithHint("invoice must be in draft status to be finalized").Mark(ierr.ErrValidation)
	}

	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		// Lock invoice to prevent concurrent finalization
		lockedInv, err := s.InvoiceRepo.GetForUpdate(txCtx, inv.ID)
		if err != nil {
			return err
		}
		// Re-check status after acquiring lock
		if lockedInv.InvoiceStatus != types.InvoiceStatusDraft {
			return ierr.NewError("invoice is not in draft status").WithHint("invoice was finalized concurrently").Mark(ierr.ErrValidation)
		}

		// ====================================================================
		// Apply prepaid credits and taxes for subscription invoices.
		// One-off and credit invoices already have credits and taxes applied
		// during ComputeInvoice, so we skip them here.
		// For subscription invoices, credits and taxes are deferred to this
		// step so wallet debits only happen when the invoice is sealed.
		// ====================================================================

		if lockedInv.InvoiceType == types.InvoiceTypeSubscription {
			// Load line items — ApplyCreditsToInvoice needs them
			lineItems, err := s.InvoiceLineItemRepo.ListByInvoiceID(txCtx, lockedInv.ID)
			if err != nil {
				return err
			}
			lockedInv.LineItems = lineItems

			if len(lockedInv.LineItems) > 0 {
				// Apply credits — this debits wallets and updates line items
				creditAdjustmentService := NewCreditAdjustmentService(s.ServiceParams)
				creditResult, err := creditAdjustmentService.ApplyCreditsToInvoice(txCtx, lockedInv)
				if err != nil {
					return err
				}
				lockedInv.TotalPrepaidCreditsApplied = creditResult.TotalPrepaidCreditsApplied

				// Recalculate total with credits applied
				newTotal := lockedInv.Subtotal.Sub(lockedInv.TotalDiscount).Sub(lockedInv.TotalPrepaidCreditsApplied)
				if newTotal.IsNegative() {
					newTotal = decimal.Zero
				}
				lockedInv.Total = newTotal
				lockedInv.AmountDue = lockedInv.Total
				lockedInv.AmountRemaining = lockedInv.Total.Sub(lockedInv.AmountPaid)

				// Persist credit-adjusted totals before tax recalculation
				if err := s.InvoiceRepo.Update(txCtx, lockedInv); err != nil {
					return err
				}

				// Recalculate taxes with credits factored in
				if err := s.RecalculateTaxesOnInvoice(txCtx, lockedInv); err != nil {
					return err
				}
			}
		}

		// ====================================================================
		// Finalize the invoice
		// ====================================================================

		// Assign invoice number if not already assigned (idempotent)
		if lockedInv.InvoiceNumber == nil || *lockedInv.InvoiceNumber == "" {
			settingsSvc := NewSettingsService(s.ServiceParams).(*settingsService)
			invoiceConfig, err := GetSetting[types.InvoiceConfig](settingsSvc, txCtx, types.SettingKeyInvoiceConfig)
			if err != nil {
				return ierr.WithError(err).WithHint("Failed to get invoice configuration").Mark(ierr.ErrValidation)
			}
			invoiceNumber, err := s.InvoiceRepo.GetNextInvoiceNumber(txCtx, &invoiceConfig)
			if err != nil {
				return err
			}
			lockedInv.InvoiceNumber = &invoiceNumber
		}

		if lockedInv.Total.IsZero() {
			lockedInv.PaymentStatus = types.PaymentStatusSucceeded
		}

		// Handle auto_complete_purchased_credit_transaction: if the wallet service
		// flagged this invoice as auto-completed via metadata, mark it as paid.
		if lockedInv.Metadata != nil && lockedInv.Metadata["auto_completed"] == "true" {
			lockedInv.PaymentStatus = types.PaymentStatusSucceeded
			lockedInv.AmountPaid = lockedInv.AmountDue
			lockedInv.AmountRemaining = decimal.Zero
		}

		now := time.Now().UTC()
		lockedInv.InvoiceStatus = types.InvoiceStatusFinalized
		lockedInv.FinalizedAt = &now

		if err := s.InvoiceRepo.Update(txCtx, lockedInv); err != nil {
			return err
		}

		// Update the caller's reference so downstream code sees the finalized state
		*inv = *lockedInv
		return nil
	})
	if err != nil {
		return err
	}

	s.publishSystemEvent(ctx, types.WebhookEventInvoiceUpdateFinalized, inv.ID)

	return nil
}

// IsFinalizationDue checks whether a draft invoice's finalization delay has elapsed.
// Returns true if the invoice should be finalized now (delay elapsed or no delay configured).
// Returns false if the invoice is not a draft or the delay has not yet elapsed.
func (s *invoiceService) IsFinalizationDue(ctx context.Context, invoiceID string) (bool, error) {
	inv, err := s.InvoiceRepo.Get(ctx, invoiceID)
	if err != nil {
		return false, err
	}

	if inv.InvoiceStatus != types.InvoiceStatusDraft {
		return false, nil
	}

	// Only finalize invoices that have been computed (LastComputedAt set by ComputeInvoice)
	if inv.LastComputedAt == nil {
		return false, nil
	}

	if inv.LastComputedAt != nil && inv.LastComputedAt.Before(*inv.PeriodEnd) && inv.BillingReason == string(types.InvoiceBillingReasonSubscriptionCycle) {
		return false, nil
	}

	settingsSvc := NewSettingsService(s.ServiceParams).(*settingsService)
	invoiceConfig, err := GetSetting[types.InvoiceConfig](settingsSvc, ctx, types.SettingKeyInvoiceConfig)
	if err != nil {
		// Fail-closed: if we can't load config, don't finalize — the cron will retry next cycle
		return false, ierr.WithError(err).WithHint("failed to load invoice config for finalization check").Mark(ierr.ErrDatabase)
	}

	if invoiceConfig.FinalizationDelaySeconds == 0 {
		return true, nil
	}

	dueAt := inv.LastComputedAt.Add(time.Duration(invoiceConfig.FinalizationDelaySeconds) * time.Second)
	return time.Now().UTC().After(dueAt), nil
}

// ListAllTenantDraftInvoices returns draft invoices across all tenants with LastComputedAt set.
// Used by the scheduled finalization job to find invoices ready for finalization.
func (s *invoiceService) ListAllTenantDraftInvoices(ctx context.Context, batchSize, offset int) ([]*invoice.Invoice, error) {
	filter := &types.InvoiceFilter{
		QueryFilter: &types.QueryFilter{
			Limit:  lo.ToPtr(batchSize),
			Offset: lo.ToPtr(offset),
			Status: lo.ToPtr(types.StatusPublished),
		},
		InvoiceStatus: []types.InvoiceStatus{types.InvoiceStatusDraft},
	}
	return s.InvoiceRepo.ListAllTenant(ctx, filter)
}

// updateMetadata merges the request metadata with the existing invoice metadata.
// This function performs a selective update where:
// - Existing metadata keys not mentioned in the request are preserved
// - Keys present in both existing and request metadata are updated with request values
// - New keys from the request are added to the metadata
// - If the invoice has no existing metadata, a new metadata map is created
func (s *invoiceService) updateMetadata(inv *invoice.Invoice, req dto.InvoiceVoidRequest) error {

	// Start with existing metadata
	metadata := inv.Metadata
	if metadata == nil {
		metadata = make(types.Metadata)
	}

	// Merge request metadata into existing metadata
	// Request values will override existing values
	for key, value := range req.Metadata {
		metadata[key] = value
	}

	inv.Metadata = metadata
	return nil
}

func (s *invoiceService) VoidInvoice(ctx context.Context, id string, req dto.InvoiceVoidRequest) error {

	if err := req.Validate(); err != nil {
		return err
	}

	inv, err := s.InvoiceRepo.Get(ctx, id)
	if err != nil {
		return err
	}

	allowedInvoiceStatuses := []types.InvoiceStatus{
		types.InvoiceStatusDraft,
		types.InvoiceStatusFinalized,
		types.InvoiceStatusSkipped,
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
		types.PaymentStatusSucceeded,
		types.PaymentStatusPartiallyRefunded,
		types.PaymentStatusOverpaid,
	}
	if !lo.Contains(allowedPaymentStatuses, inv.PaymentStatus) {
		return ierr.NewError("invoice payment status is not allowed").
			WithHintf("invoice payment status - %s is not allowed", inv.PaymentStatus).
			WithReportableDetails(map[string]any{
				"allowed_statuses": allowedPaymentStatuses,
			}).
			Mark(ierr.ErrValidation)
	}

	err = s.DB.WithTx(ctx, func(tx context.Context) error {
		now := time.Now().UTC()
		inv.InvoiceStatus = types.InvoiceStatusVoided
		inv.VoidedAt = &now
		if req.Metadata != nil {
			if err := s.updateMetadata(inv, req); err != nil {
				return err
			}
		}

		// Refund AmountPaid + TotalPrepaidCreditsApplied back to the customer's wallet.
		// Both represent value the customer already provided for this invoice.
		refundAmount := inv.AmountPaid.Add(inv.TotalPrepaidCreditsApplied)
		if refundAmount.IsPositive() {
			walletService := NewWalletService(s.ServiceParams)

			wallets, err := walletService.GetWalletsByCustomerID(tx, inv.CustomerID)
			if err != nil {
				return err
			}

			var selectedWallet *dto.WalletResponse
			for _, w := range wallets {
				if types.IsMatchingCurrency(w.Currency, inv.Currency) && w.WalletType == types.WalletTypePrePaid {
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
				Amount:            refundAmount,
				TransactionReason: types.TransactionReasonInvoiceVoidRefund,
				Metadata:          types.Metadata{"invoice_id": inv.ID},
				IdempotencyKey:    lo.ToPtr(inv.ID),
				Description:       fmt.Sprintf("Refund for voided invoice: %s", lo.FromPtrOr(inv.InvoiceNumber, inv.ID)),
			}
			if _, err = walletService.TopUpWallet(tx, selectedWallet.ID, walletTxnReq); err != nil {
				return err
			}

			inv.RefundedAmount = inv.RefundedAmount.Add(refundAmount)
			inv.PaymentStatus = types.PaymentStatusRefunded
		}

		return s.InvoiceRepo.Update(tx, inv)
	})
	if err != nil {
		return err
	}

	s.publishSystemEvent(ctx, types.WebhookEventInvoiceUpdateVoided, inv.ID)
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

	if inv.LastComputedAt == nil {
		return ierr.NewError("invoice has not been computed").WithHint("Invoice must be computed before processing").Mark(ierr.ErrValidation)
	}

	// try to finalize the invoice
	if err := s.performFinalizeInvoiceActions(ctx, inv); err != nil {
		return err
	}

	// Integration invoice sync is triggered by the shared system event
	// invoice.update.finalized (published from performFinalizeInvoiceActions).

	// try to process payment for the invoice based on behavior and log any errors
	// Pass the subscription object to avoid extra DB call
	// Error handling logic is properly handled in attemptPaymentForSubscriptionInvoice
	if err := s.attemptPaymentForSubscriptionInvoice(ctx, inv, paymentParams, sub, flowType); err != nil {
		// Only return error if it's a blocking error (e.g., subscription creation with error_if_incomplete)
		return err
	}

	return nil
}

// SyncInvoiceToStripeIfEnabled syncs the invoice to Stripe if Stripe connection is enabled.
func (s *invoiceService) SyncInvoiceToStripeIfEnabled(ctx context.Context, invoiceID string, collectionMethod string) error {
	inv, err := s.InvoiceRepo.Get(ctx, invoiceID)
	if err != nil {
		return err
	}

	// Keep Stripe sync working for both subscription and non-subscription invoices.
	// For invoices without subscription context, default to send_invoice.
	sub := &subscription.Subscription{
		CollectionMethod: collectionMethod,
	}
	if inv.SubscriptionID != nil {
		sub.ID = *inv.SubscriptionID
		if sub.CollectionMethod == "" {
			storedSub, err := s.SubRepo.Get(ctx, *inv.SubscriptionID)
			if err != nil {
				return err
			}
			sub = storedSub
		}
	} else if sub.CollectionMethod == "" {
		sub.CollectionMethod = string(types.CollectionMethodSendInvoice)
	}

	// Check if Stripe connection exists
	conn, err := s.ConnectionRepo.GetByProvider(ctx, types.SecretProviderStripe)
	if err != nil || conn == nil {
		s.Logger.DebugwCtx(ctx, "Stripe connection not available, skipping invoice sync",
			"invoice_id", inv.ID,
			"error", err)
		return nil // Not an error, just skip sync
	}

	// Check if invoice sync is enabled for this connection
	if !conn.IsInvoiceOutboundEnabled() {
		s.Logger.DebugwCtx(ctx, "invoice sync disabled for Stripe connection, skipping invoice sync",
			"invoice_id", inv.ID,
			"connection_id", conn.ID)
		return nil // Not an error, just skip sync
	}

	// Get Stripe integration
	stripeIntegration, err := s.IntegrationFactory.GetStripeIntegration(ctx)
	if err != nil {
		s.Logger.ErrorwCtx(ctx, "failed to get Stripe integration, skipping invoice sync",
			"invoice_id", inv.ID,
			"error", err)
		return nil // Don't fail the entire process, just skip invoice sync
	}

	// Ensure customer is synced to Stripe before syncing invoice
	customerService := NewCustomerService(s.ServiceParams)
	customerResp, err := stripeIntegration.CustomerSvc.EnsureCustomerSyncedToStripe(ctx, inv.CustomerID, customerService)
	if err != nil {
		s.Logger.ErrorwCtx(ctx, "failed to ensure customer is synced to Stripe, skipping invoice sync",
			"invoice_id", inv.ID,
			"customer_id", inv.CustomerID,
			"error", err)
		return nil // Don't fail the entire process, just skip invoice sync
	}

	s.Logger.InfowCtx(ctx, "customer synced to Stripe, proceeding with invoice sync",
		"invoice_id", inv.ID,
		"customer_id", inv.CustomerID,
		"stripe_customer_id", customerResp.Customer.Metadata["stripe_customer_id"])

	s.Logger.InfowCtx(ctx, "syncing invoice to Stripe",
		"invoice_id", inv.ID,
		"subscription_id", sub.ID,
		"collection_method", sub.CollectionMethod)

	// Determine collection method from subscription
	stripeCollectionMethod := types.CollectionMethod(sub.CollectionMethod)

	// Create sync request using the integration package's DTO
	syncRequest := stripe.StripeInvoiceSyncRequest{
		InvoiceID:        inv.ID,
		CollectionMethod: string(stripeCollectionMethod),
	}

	// Perform the sync
	syncResponse, err := stripeIntegration.InvoiceSyncSvc.SyncInvoiceToStripe(ctx, syncRequest, customerService)
	if err != nil {
		return err
	}

	s.Logger.InfowCtx(ctx, "successfully synced invoice to Stripe",
		"invoice_id", inv.ID,
		"stripe_invoice_id", syncResponse.StripeInvoiceID,
		"status", syncResponse.Status)

	return nil
}

// SyncInvoiceToRazorpayIfEnabled syncs the invoice to Razorpay if Razorpay connection is enabled.
func (s *invoiceService) SyncInvoiceToRazorpayIfEnabled(ctx context.Context, invoiceID string) error {
	inv, err := s.InvoiceRepo.Get(ctx, invoiceID)
	if err != nil {
		return err
	}

	// Check if Razorpay connection exists
	conn, err := s.ConnectionRepo.GetByProvider(ctx, types.SecretProviderRazorpay)
	if err != nil || conn == nil {
		s.Logger.DebugwCtx(ctx, "Razorpay connection not available, skipping invoice sync",
			"invoice_id", inv.ID,
			"error", err)
		return nil // Not an error, just skip sync
	}

	// Check if invoice sync is enabled for this connection
	if !conn.IsInvoiceOutboundEnabled() {
		s.Logger.DebugwCtx(ctx, "invoice sync disabled for Razorpay connection, skipping invoice sync",
			"invoice_id", inv.ID,
			"connection_id", conn.ID)
		return nil // Not an error, just skip sync
	}

	// Get Razorpay integration
	razorpayIntegration, err := s.IntegrationFactory.GetRazorpayIntegration(ctx)
	if err != nil {
		s.Logger.ErrorwCtx(ctx, "failed to get Razorpay integration, skipping invoice sync",
			"invoice_id", inv.ID,
			"error", err)
		return nil // Don't fail the entire process, just skip invoice sync
	}

	s.Logger.InfowCtx(ctx, "syncing invoice to Razorpay",
		"invoice_id", inv.ID,
		"customer_id", inv.CustomerID)

	// Create customer service instance
	customerService := NewCustomerService(s.ServiceParams)

	// Create sync request
	syncRequest := razorpay.RazorpayInvoiceSyncRequest{
		InvoiceID: inv.ID,
	}

	// Perform the sync
	syncResponse, err := razorpayIntegration.InvoiceSyncSvc.SyncInvoiceToRazorpay(ctx, syncRequest, customerService)
	if err != nil {
		return err
	}

	s.Logger.InfowCtx(ctx, "successfully synced invoice to Razorpay",
		"invoice_id", inv.ID,
		"razorpay_invoice_id", syncResponse.RazorpayInvoiceID,
		"status", syncResponse.Status,
		"payment_url", syncResponse.ShortURL)

	// Save Razorpay URLs in invoice metadata
	if syncResponse.ShortURL != "" {
		metadata := inv.Metadata
		if metadata == nil {
			metadata = types.Metadata{}
		}

		metadata["razorpay_invoice_id"] = syncResponse.RazorpayInvoiceID
		metadata["razorpay_payment_url"] = syncResponse.ShortURL

		// Update invoice with new metadata
		updateReq := dto.UpdateInvoiceRequest{
			Metadata: &metadata,
		}

		_, err = s.UpdateInvoice(ctx, inv.ID, updateReq)
		if err != nil {
			s.Logger.WarnwCtx(ctx, "failed to update invoice metadata with Razorpay URLs",
				"error", err,
				"invoice_id", inv.ID)
			// Don't fail the sync, just log the warning
		} else {
			s.Logger.InfowCtx(ctx, "saved Razorpay URLs in invoice metadata",
				"invoice_id", inv.ID,
				"razorpay_invoice_id", syncResponse.RazorpayInvoiceID,
				"payment_url", syncResponse.ShortURL)
		}
	}

	return nil
}

// SyncInvoiceToChargebeeIfEnabled syncs the invoice to Chargebee if Chargebee connection is enabled.
func (s *invoiceService) SyncInvoiceToChargebeeIfEnabled(ctx context.Context, invoiceID string) error {
	inv, err := s.InvoiceRepo.Get(ctx, invoiceID)
	if err != nil {
		return err
	}

	// Check if Chargebee connection exists
	conn, err := s.ConnectionRepo.GetByProvider(ctx, types.SecretProviderChargebee)
	if err != nil || conn == nil {
		s.Logger.DebugwCtx(ctx, "Chargebee connection not available, skipping invoice sync",
			"invoice_id", inv.ID,
			"error", err)
		return nil // Not an error, just skip sync
	}

	// Check if invoice sync is enabled for this connection
	if !conn.IsInvoiceOutboundEnabled() {
		s.Logger.DebugwCtx(ctx, "invoice sync disabled for Chargebee connection, skipping invoice sync",
			"invoice_id", inv.ID,
			"connection_id", conn.ID)
		return nil // Not an error, just skip sync
	}

	// Get Chargebee integration
	chargebeeIntegration, err := s.IntegrationFactory.GetChargebeeIntegration(ctx)
	if err != nil {
		s.Logger.ErrorwCtx(ctx, "failed to get Chargebee integration, skipping invoice sync",
			"invoice_id", inv.ID,
			"error", err)
		return nil // Don't fail the entire process, just skip invoice sync
	}

	s.Logger.InfowCtx(ctx, "syncing invoice to Chargebee",
		"invoice_id", inv.ID,
		"customer_id", inv.CustomerID)

	// Create sync request
	syncRequest := chargebee.ChargebeeInvoiceSyncRequest{
		InvoiceID: inv.ID,
	}

	// Perform the sync
	syncResponse, err := chargebeeIntegration.InvoiceSvc.SyncInvoiceToChargebee(ctx, syncRequest)
	if err != nil {
		return err
	}

	s.Logger.InfowCtx(ctx, "successfully synced invoice to Chargebee",
		"invoice_id", inv.ID,
		"chargebee_invoice_id", syncResponse.ChargebeeInvoiceID,
		"status", syncResponse.Status,
		"total", syncResponse.Total,
		"amount_due", syncResponse.AmountDue)

	return nil
}

// SyncInvoiceToQuickBooksIfEnabled syncs the invoice to QuickBooks if QuickBooks connection is enabled.
func (s *invoiceService) SyncInvoiceToQuickBooksIfEnabled(ctx context.Context, invoiceID string) error {
	inv, err := s.InvoiceRepo.Get(ctx, invoiceID)
	if err != nil {
		return err
	}

	// Check if QuickBooks connection exists
	conn, err := s.ConnectionRepo.GetByProvider(ctx, types.SecretProviderQuickBooks)
	if err != nil || conn == nil {
		// If connection doesn't exist (not found), this is expected - just skip sync
		if err != nil && !ierr.IsNotFound(err) {
			// Actual error occurred (not just missing connection)
			s.Logger.ErrorwCtx(ctx, "failed to check QuickBooks connection, skipping invoice sync",
				"invoice_id", inv.ID,
				"error", err)
			return nil // Don't fail invoice creation, just skip sync
		}
		// Connection not found - this is expected, log at debug level
		s.Logger.DebugwCtx(ctx, "QuickBooks connection not available, skipping invoice sync",
			"invoice_id", inv.ID)
		return nil // Not an error, just skip sync
	}

	// Check if invoice sync is enabled - only proceed if outbound is true
	if !conn.IsInvoiceOutboundEnabled() {
		s.Logger.DebugwCtx(ctx, "invoice sync disabled, skipping",
			"invoice_id", inv.ID)
		return nil
	}

	// Get QuickBooks integration
	qbIntegration, err := s.IntegrationFactory.GetQuickBooksIntegration(ctx)
	if err != nil {
		s.Logger.ErrorwCtx(ctx, "failed to get QuickBooks integration, skipping invoice sync",
			"invoice_id", inv.ID,
			"error", err)
		return nil // Don't fail the entire process, just skip invoice sync
	}

	s.Logger.InfowCtx(ctx, "syncing invoice to QuickBooks",
		"invoice_id", inv.ID,
		"customer_id", inv.CustomerID)

	// Create sync request
	syncRequest := quickbooks.QuickBooksInvoiceSyncRequest{
		InvoiceID: inv.ID,
	}

	// Perform the sync
	syncResponse, err := qbIntegration.InvoiceSvc.SyncInvoiceToQuickBooks(ctx, syncRequest)
	if err != nil {
		return err
	}

	s.Logger.InfowCtx(ctx, "successfully synced invoice to QuickBooks",
		"invoice_id", inv.ID,
		"quickbooks_invoice_id", syncResponse.QuickBooksInvoiceID)

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

	// If invoice is for a purchased credit (has wallet_transaction_id in metadata) and the payment status transitioned to succeeded,
	// complete the wallet transaction to credit the wallet
	if status == types.PaymentStatusSucceeded {
		// Check if this invoice is for a purchased credit (has wallet_transaction_id in metadata)
		if inv.Metadata != nil {
			if walletTransactionID, ok := inv.Metadata["wallet_transaction_id"]; ok && walletTransactionID != "" {
				walletService := NewWalletService(s.ServiceParams)
				if err := walletService.CompletePurchasedCreditTransactionWithRetry(ctx, walletTransactionID); err != nil {
					s.Logger.ErrorwCtx(ctx, "failed to complete purchased credit transaction",
						"error", err,
						"invoice_id", inv.ID,
						"wallet_transaction_id", walletTransactionID,
					)
				} else {
					s.Logger.DebugwCtx(ctx, "successfully completed purchased credit transaction",
						"invoice_id", inv.ID,
						"wallet_transaction_id", walletTransactionID,
					)
				}
			}
		}
	}

	// Publish webhook events
	s.publishSystemEvent(ctx, types.WebhookEventInvoiceUpdatePayment, inv.ID)

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

	// Check if this invoice is for a purchased credit (has wallet_transaction_id in metadata)
	// If so, complete the wallet transaction to credit the wallet
	if inv.Metadata != nil {
		if walletTransactionID, ok := inv.Metadata["wallet_transaction_id"]; ok && walletTransactionID != "" {
			// Only complete the transaction if payment is fully succeeded
			if status == types.PaymentStatusSucceeded || status == types.PaymentStatusOverpaid {
				walletService := NewWalletService(s.ServiceParams)
				if err := walletService.CompletePurchasedCreditTransactionWithRetry(ctx, walletTransactionID); err != nil {
					s.Logger.ErrorwCtx(ctx, "failed to complete purchased credit transaction",
						"error", err,
						"invoice_id", inv.ID,
						"wallet_transaction_id", walletTransactionID,
					)
					// Don't fail the payment, but log the error
					// The transaction can be manually completed later
				} else {
					s.Logger.InfowCtx(ctx, "successfully completed purchased credit transaction",
						"invoice_id", inv.ID,
						"wallet_transaction_id", walletTransactionID,
					)
				}
			}
		}
	}

	// Publish webhook events
	s.publishSystemEvent(ctx, types.WebhookEventInvoiceUpdatePayment, inv.ID)

	return nil
}

func (s *invoiceService) CreateSubscriptionInvoice(ctx context.Context, req *dto.CreateSubscriptionInvoiceRequest, paymentParams *dto.PaymentParameters, flowType types.InvoiceFlowType, isDraftSubscription bool) (*dto.InvoiceResponse, *subscription.Subscription, error) {
	s.Logger.InfowCtx(ctx, "creating subscription invoice",
		"subscription_id", req.SubscriptionID,
		"period_start", req.PeriodStart,
		"period_end", req.PeriodEnd,
		"reference_point", req.ReferencePoint)

	if err := req.Validate(); err != nil {
		return nil, nil, err
	}

	// Get subscription with line items
	subscription, _, err := s.SubRepo.GetWithLineItems(ctx, req.SubscriptionID)
	if err != nil {
		return nil, nil, err
	}

	// Reject invoice creation for draft subscriptions (unless isDraftSubscription is true)
	if !isDraftSubscription && subscription.SubscriptionStatus == types.SubscriptionStatusDraft {
		return nil, nil, ierr.NewError("cannot create invoice for draft subscription").
			WithHint("Draft subscriptions must be activated before invoice creation").
			WithReportableDetails(map[string]interface{}{
				"subscription_id":     req.SubscriptionID,
				"subscription_status": subscription.SubscriptionStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	// Draft-first: create zero-dollar draft (idempotent; returns existing if same period)
	// Use ToDraftRequest to build from pre-fetched subscription, avoiding redundant DB fetch
	draftReq := req.ToDraftRequest(
		subscription.GetInvoicingCustomerID(),
		subscription.ID,
		subscription.Currency,
		string(subscription.BillingPeriod),
	)
	if flowType == types.InvoiceFlowSubscriptionCreation {
		draftReq.BillingReason = types.InvoiceBillingReasonSubscriptionCreate
	}
	draft, err := s.CreateEmptyDraftInvoice(ctx, draftReq)
	if err != nil {
		return nil, nil, err
	}

	// Populate draft with usage and line items; if zero-dollar, marked SKIPPED
	// Pass nil for subscription invoices - coupons/taxes come from billing service
	skipped, err := s.ComputeInvoice(ctx, draft.ID, nil)
	if err != nil {
		return nil, nil, err
	}
	if skipped {
		return nil, subscription, nil
	}

	// Process: finalize, sync, attempt payment
	if err := s.ProcessDraftInvoice(ctx, draft.ID, paymentParams, subscription, flowType); err != nil {
		return nil, nil, err
	}

	// Return populated invoice (re-fetch for final state)
	inv, err := s.InvoiceRepo.Get(ctx, draft.ID)
	if err != nil {
		return nil, nil, err
	}
	return dto.NewInvoiceResponse(inv), subscription, nil
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
		ctx, sub, *req.PeriodStart, *req.PeriodEnd, types.ReferencePointPreview, "")
	if err != nil {
		return nil, err
	}

	s.Logger.InfowCtx(ctx, "prepared invoice request for preview",
		"invoice_request", invReq)

	if req.HideZeroChargesLineItems {
		invReq.LineItems = lo.Filter(invReq.LineItems, func(item dto.CreateInvoiceLineItemRequest, _ int) bool {
			return !item.Amount.IsZero()
		})
	}

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
	s.Logger.DebugwCtx(ctx, "getting customer invoice summary",
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
			s.publishSystemEvent(ctx, types.WebhookEventInvoicePaymentOverdue, inv.ID)
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

	s.Logger.DebugwCtx(ctx, "customer invoice summary calculated",
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

func (s *invoiceService) GetUnpaidInvoicesToBePaid(ctx context.Context, req dto.GetUnpaidInvoicesToBePaidRequest) (*dto.GetUnpaidInvoicesToBePaidResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	unpaidInvoices := make([]*dto.InvoiceResponse, 0)
	unpaidAmount := decimal.Zero
	unpaidUsageCharges := decimal.Zero
	unpaidFixedCharges := decimal.Zero
	totalInvoiceAmountPaid := decimal.Zero

	// Include both Draft and Finalized invoices. With the draft-first flow and
	// finalization delay, computed drafts already have committed charges (credits applied,
	// line items populated). Excluding them would overstate the wallet's available balance.
	// Empty/uncomputed drafts are harmless — they have zero AmountRemaining and are
	// skipped by the AmountRemaining.IsZero() check below.
	filter := types.NewNoLimitInvoiceFilter()
	filter.QueryFilter.Status = lo.ToPtr(types.StatusPublished)
	filter.CustomerID = req.CustomerID
	filter.InvoiceStatus = []types.InvoiceStatus{types.InvoiceStatusDraft, types.InvoiceStatusFinalized}

	invoicesResp, err := s.ListInvoices(ctx, filter)
	if err != nil {
		return nil, err
	}

	for _, inv := range invoicesResp.Items {
		// For uncomputed subscription drafts, compute inline to get accurate amounts.
		// Without this, uncomputed drafts have zero AmountRemaining and would be skipped,
		// understating pending charges and overstating available wallet balance.
		if inv.InvoiceStatus == types.InvoiceStatusDraft && inv.LastComputedAt == nil {
			_, computeErr := s.ComputeInvoice(ctx, inv.ID, nil)
			if computeErr != nil {
				s.Logger.ErrorwCtx(ctx, "failed to compute draft invoice for wallet balance",
					"invoice_id", inv.ID, "error", computeErr)
				continue
			}
			// Re-fetch to get computed amounts (use repo directly to avoid
			// dependency on InvoiceLineItemRepo which may not be injected in tests)
			computedInv, fetchErr := s.InvoiceRepo.Get(ctx, inv.ID)
			if fetchErr != nil {
				continue
			}
			inv = dto.NewInvoiceResponse(computedInv)
		}

		// Skip draft invoices whose billing period hasn't ended yet —
		// their charges are not yet due and should not reduce wallet balance.
		if inv.InvoiceStatus == types.InvoiceStatusDraft && inv.PeriodEnd != nil && inv.PeriodEnd.After(time.Now().UTC()) {
			continue
		}

		// filter by currency
		if !types.IsMatchingCurrency(inv.Currency, req.Currency) {
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
		totalInvoiceAmountPaid = totalInvoiceAmountPaid.Add(inv.AmountPaid)

		for _, item := range inv.LineItems {
			if lo.FromPtr(item.PriceType) == string(types.PRICE_TYPE_USAGE) {
				unpaidUsageCharges = unpaidUsageCharges.Add(item.Amount).Sub(item.PrepaidCreditsApplied).Sub(item.LineItemDiscount)
			} else {
				unpaidFixedCharges = unpaidFixedCharges.Add(item.Amount)
			}
		}
	}

	return &dto.GetUnpaidInvoicesToBePaidResponse{
		Invoices:                unpaidInvoices,
		TotalUnpaidAmount:       unpaidAmount,
		TotalUnpaidUsageCharges: unpaidUsageCharges,
		TotalUnpaidFixedCharges: unpaidFixedCharges,
		TotalPaidInvoiceAmount:  totalInvoiceAmountPaid,
	}, nil
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
			s.Logger.ErrorwCtx(ctx, "failed to get customer invoice summary",
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
	if inv.InvoiceStatus == types.InvoiceStatusSkipped {
		return nil // No-op for zero-dollar skipped invoices
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
			s.Logger.ErrorwCtx(ctx, "failed to get subscription for payment processing",
				"error", err,
				"subscription_id", *inv.SubscriptionID,
				"invoice_id", inv.ID)
			return err
		}
	}

	// If Stripe outbound invoice sync is enabled for this tenant/environment,
	// skip automatic payment and let Stripe/record-payment flows be the source of truth.
	conn, connErr := s.ConnectionRepo.GetByProvider(ctx, types.SecretProviderStripe)
	if connErr == nil && conn != nil && conn.IsInvoiceOutboundEnabled() {
		s.Logger.InfowCtx(ctx, "stripe invoice sync enabled, skipping automatic payment processing",
			"invoice_id", inv.ID,
			"subscription_id", lo.FromPtr(inv.SubscriptionID),
			"flow_type", flowType)
		return nil
	}

	// Fallback guard: if mapping already exists in Stripe, also skip automatic payment.
	stripeIntegration, err := s.IntegrationFactory.GetStripeIntegration(ctx)
	if err == nil && stripeIntegration.InvoiceSyncSvc.IsInvoiceSyncedToStripe(ctx, inv.ID) {
		s.Logger.InfowCtx(ctx, "invoice is synced to Stripe, skipping automatic payment processing",
			"invoice_id", inv.ID,
			"subscription_id", lo.FromPtr(inv.SubscriptionID),
			"flow_type", flowType)
		return nil
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
			Invoice: lo.FromPtr(inv),
		}

		// Delegate all payment behavior handling to the payment processor
		err := paymentProcessor.HandlePaymentBehavior(ctx, sub, invoiceResponse, finalPaymentBehavior, flowType)
		if err != nil {
			s.Logger.ErrorwCtx(ctx, "failed to process payment for subscription invoice",
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
			s.Logger.InfowCtx(ctx, "payment failed but continuing with invoice processing for flow type",
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
			Invoice: lo.FromPtr(inv),
		}

		amountPaid := paymentProcessor.ProcessCreditsPaymentForInvoice(ctx, invoiceResponse, nil)
		if amountPaid.GreaterThan(decimal.Zero) {
			s.Logger.InfowCtx(ctx, "credits payment successful for non-subscription invoice",
				"invoice_id", inv.ID,
				"amount_paid", amountPaid)
		} else {
			s.Logger.InfowCtx(ctx, "no credits payment made for non-subscription invoice",
				"invoice_id", inv.ID,
				"amount_due", inv.AmountDue)
		}
	}

	return nil
}

func (s *invoiceService) GetInvoicePDFUrl(ctx context.Context, id string, forceGenerate bool) (string, error) {

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

	if !forceGenerate {
		// Check if the file already exists in S3 and return a presigned URL without regenerating
		exists, err := s.S3.Exists(ctx, key, s3.DocumentTypeInvoice)
		if err != nil {
			return "", err
		}
		if exists {
			return s.S3.GetPresignedUrl(ctx, key, s3.DocumentTypeInvoice)
		}
	}

	// Generate the PDF and upload to S3
	data, err := s.GetInvoicePDF(ctx, id)
	if err != nil {
		return "", err
	}

	err = s.S3.UploadDocument(ctx, s3.NewPdfDocument(key, data, s3.DocumentTypeInvoice))
	if err != nil {
		return "", err
	}

	return s.S3.GetPresignedUrl(ctx, key, s3.DocumentTypeInvoice)
}

// GetInvoicePDF implements InvoiceService.
func (s *invoiceService) GetInvoicePDF(ctx context.Context, id string) ([]byte, error) {

	settingsSvc := NewSettingsService(s.ServiceParams).(*settingsService)
	pdfConfig, err := GetSetting[types.InvoicePDFConfig](
		settingsSvc,
		ctx,
		types.SettingKeyInvoicePDFConfig,
	)
	if err != nil {
		return nil, err
	}

	// validate request
	req := dto.GetInvoiceWithBreakdownRequest{ID: id}

	// Use typed config directly
	req.GroupBy = pdfConfig.GroupBy
	templateName := pdfConfig.TemplateName
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// get invoice by id
	inv, err := s.GetInvoiceWithBreakdown(ctx, req)
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
	return s.PDFGenerator.RenderInvoicePdf(ctx, invoiceData, lo.ToPtr(templateName))

}

func (s *invoiceService) getInvoiceDataForPDFGen(
	ctx context.Context,
	inv *dto.InvoiceResponse,
	customer *customer.Customer,
	tenant *tenant.Tenant,
) (*pdf.InvoiceData, error) {
	invoiceNum := ""
	if inv.InvoiceNumber != nil {
		invoiceNum = *inv.InvoiceNumber
	}

	// Round to currency precision before converting to float64
	precision := types.GetCurrencyPrecision(inv.Currency)
	subtotal, _ := inv.Subtotal.Round(precision).Float64()
	totalDiscount, _ := inv.TotalDiscount.Round(precision).Float64()
	totalPrepaidCreditsApplied, _ := inv.TotalPrepaidCreditsApplied.Round(precision).Float64()
	totalTax, _ := inv.TotalTax.Round(precision).Float64()
	total, _ := inv.Total.Round(precision).Float64()
	amountPaid, _ := inv.AmountPaid.Round(precision).Float64()
	amountRemaining, _ := inv.AmountRemaining.Round(precision).Float64()

	// Convert to InvoiceData
	data := &pdf.InvoiceData{
		ID:                         inv.ID,
		InvoiceNumber:              invoiceNum,
		InvoiceStatus:              string(inv.InvoiceStatus),
		Currency:                   types.GetCurrencySymbol(inv.Currency),
		Precision:                  types.GetCurrencyPrecision(inv.Currency),
		AmountDue:                  total,
		Subtotal:                   subtotal,
		TotalDiscount:              totalDiscount,
		TotalPrepaidCreditsApplied: totalPrepaidCreditsApplied,
		TotalTax:                   totalTax,
		BillingReason:              inv.BillingReason,
		Notes:                      "",  // resolved from invoice metadata
		VAT:                        0.0, // resolved from invoice metadata
		Biller:                     s.getBillerInfo(tenant),
		PeriodStart:                pdf.CustomTime{Time: lo.FromPtr(inv.PeriodStart)},
		PeriodEnd:                  pdf.CustomTime{Time: lo.FromPtr(inv.PeriodEnd)},
		Recipient:                  s.getRecipientInfo(customer),
		BillingPeriod:              lo.FromPtrOr(inv.BillingPeriod, ""),
		Description:                inv.Description,
		AmountPaid:                 amountPaid,
		AmountRemaining:            amountRemaining,
		PaymentStatus:              string(inv.PaymentStatus),
		InvoiceType:                string(inv.InvoiceType),
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

	// Prepare line items
	var lineItems []pdf.LineItemData

	// Batch fetch prices and groups to avoid N+1 queries
	var priceMap map[string]*domainPrice.Price
	var groupMap map[string]string // groupID -> groupName

	// Collect all price IDs
	priceIDs := make([]string, 0)
	for _, item := range inv.LineItems {
		if item.PriceID != nil && *item.PriceID != "" {
			priceIDs = append(priceIDs, *item.PriceID)
		}
	}

	// Batch fetch all prices
	if len(priceIDs) > 0 {
		priceIDs = lo.Uniq(priceIDs)
		priceFilter := types.NewNoLimitPriceFilter().WithPriceIDs(priceIDs)
		prices, err := s.PriceRepo.List(ctx, priceFilter)
		if err == nil {
			priceMap = make(map[string]*domainPrice.Price, len(prices))
			for _, p := range prices {
				priceMap[p.ID] = p
			}

			// Collect unique group IDs from prices
			groupIDs := make([]string, 0)
			for _, p := range prices {
				if p.GroupID != "" {
					groupIDs = append(groupIDs, p.GroupID)
				}
			}

			// Batch fetch all groups
			if len(groupIDs) > 0 {
				groupIDs = lo.Uniq(groupIDs)
				groupService := NewGroupService(s.ServiceParams)
				groupFilter := &types.GroupFilter{
					QueryFilter: types.NewNoLimitQueryFilter(),
					GroupIDs:    groupIDs,
				}
				groupsResponse, err := groupService.ListGroups(ctx, groupFilter)
				if err == nil && groupsResponse != nil {
					groupMap = make(map[string]string, len(groupsResponse.Items))
					for _, g := range groupsResponse.Items {
						groupMap[g.ID] = g.Name
					}
				}
			}
		}
	}

	// Process line items - filter out zero-amount items for PDF
	for _, item := range inv.LineItems {
		// Skip line items with zero amount for PDF generation
		if item.Amount.IsZero() {
			s.Logger.Debugw("skipping zero-amount line item for PDF",
				"line_item_id", item.ID,
				"plan_display_name", lo.FromPtrOr(item.PlanDisplayName, ""),
				"amount", item.Amount.String())
			continue
		}

		planDisplayName := ""
		if item.PlanDisplayName != nil {
			planDisplayName = *item.PlanDisplayName
		}
		displayName := ""
		if item.DisplayName != nil {
			displayName = *item.DisplayName
		}

		// Round to currency precision before converting to float64
		precision := types.GetCurrencyPrecision(item.Currency)
		amount, _ := item.Amount.Round(precision).Float64()

		description := ""
		if item.Metadata != nil {
			if desc, ok := item.Metadata["description"]; ok {
				description = desc
			}
		}

		// Determine item type based on EntityType (source of truth)
		itemType := "subscription" // default fallback

		if item.EntityType != nil {
			switch *item.EntityType {
			case "addon":
				itemType = "addon"
			case "plan":
				itemType = "subscription"
			default:
				itemType = *item.EntityType
			}
		}

		// Get group name from batch-fetched maps
		groupName := "--"
		if item.PriceID != nil && *item.PriceID != "" {
			if price, ok := priceMap[*item.PriceID]; ok && price != nil && price.GroupID != "" {
				if name, ok := groupMap[price.GroupID]; ok && name != "" {
					groupName = name
				}
			}
		}

		lineItem := pdf.LineItemData{
			PlanDisplayName: planDisplayName,
			DisplayName:     displayName,
			Description:     description,
			Group:           groupName,
			Amount:          amount, // Keep original sign
			Quantity:        item.Quantity.InexactFloat64(),
			Currency:        types.GetCurrencySymbol(item.Currency),
			Type:            itemType,
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

		if item.UsageBreakdown != nil {
			lineItem.UsageBreakdown = item.UsageBreakdown
		}

		lineItems = append(lineItems, lineItem)
	}

	// Line items contain only actual billable items (subscriptions, addons)
	// Taxes and discounts are shown in the summary section, not as line items

	data.LineItems = lineItems

	// Get applied taxes for detailed breakdown
	appliedTaxes, err := s.getAppliedTaxesForPDF(ctx, inv.ID)
	if err != nil {
		s.Logger.Warnw("failed to get applied taxes for PDF", "error", err, "invoice_id", inv.ID)
		// Don't fail PDF generation, just skip applied taxes section
		appliedTaxes = []pdf.AppliedTaxData{}
	}
	data.AppliedTaxes = appliedTaxes

	// No need to process usage breakdown here as it's already handled in LineItemData

	appliedDiscounts, err := s.getAppliedDiscountsForPDF(ctx, inv)
	if err != nil {
		s.Logger.Warnw("failed to get applied discounts for PDF", "error", err, "invoice_id", inv.ID)
		// Don't fail PDF generation, just skip applied discounts section
		appliedDiscounts = []pdf.AppliedDiscountData{}
	}
	data.AppliedDiscounts = appliedDiscounts

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
		Name:    name,
		Address: pdf.AddressInfo{},
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
		Name:    t.Name,
		Address: pdf.AddressInfo{},
	}

	if t.BillingDetails != (tenant.TenantBillingDetails{}) {
		billingDetails := t.BillingDetails
		billerInfo.Email = billingDetails.Email
		// billerInfo.Website = billingDetails.Website //TODO: Add this
		billerInfo.HelpEmail = billingDetails.HelpEmail
		// billerInfo.PaymentInstructions = billingDetails.PaymentInstructions //TODO: Add this

		billerInfo.Address = pdf.AddressInfo{
			Street:     billingDetails.Address.FormatAddressLines(),
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
		s.Logger.InfowCtx(ctx, "invoice is not finalized, skipping recalculation", "invoice_id", invoiceID)
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

	// Calculate total adjustment credits (with currency-aware rounding)
	inv.AdjustmentAmount = types.RoundToCurrencyPrecision(totalAdjustmentAmount, inv.Currency)
	inv.RefundedAmount = types.RoundToCurrencyPrecision(totalRefundAmount, inv.Currency)
	inv.AmountDue = types.RoundToCurrencyPrecision(inv.Total.Sub(inv.AdjustmentAmount), inv.Currency)

	remaining := inv.AmountDue.Sub(inv.AmountPaid)
	if remaining.IsPositive() {
		inv.AmountRemaining = types.RoundToCurrencyPrecision(remaining, inv.Currency)
	} else {
		inv.AmountRemaining = decimal.Zero
	}

	// Update the payment status if the invoice is fully paid
	if inv.AmountRemaining.Equal(decimal.Zero) {
		s.Logger.InfowCtx(ctx, "invoice is fully paid, updating payment status to succeeded", "invoice_id", inv.ID)
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

func (s *invoiceService) publishSystemEvent(ctx context.Context, eventName types.WebhookEventName, invoiceID string) {
	webhookPayload, err := json.Marshal(struct {
		InvoiceID string `json:"invoice_id"`
		TenantID  string `json:"tenant_id"`
	}{
		InvoiceID: invoiceID,
		TenantID:  types.GetTenantID(ctx),
	})

	if err != nil {
		s.Logger.ErrorwCtx(ctx, "failed to marshal webhook payload", "error", err)
		return
	}

	webhookEvent := &types.WebhookEvent{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SYSTEM_EVENT),
		EventName:     eventName,
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
		UserID:        types.GetUserID(ctx),
		Timestamp:     time.Now().UTC(),
		Payload:       json.RawMessage(webhookPayload),
		EntityType:    types.SystemEntityTypeInvoice,
		EntityID:      invoiceID,
	}
	s.Logger.InfowCtx(ctx, "attempting to publish webhook event",
		"webhook_id", webhookEvent.ID,
		"event_name", eventName,
		"invoice_id", invoiceID,
		"tenant_id", webhookEvent.TenantID,
		"environment_id", webhookEvent.EnvironmentID,
	)

	if err := s.WebhookPublisher.PublishWebhook(ctx, webhookEvent); err != nil {
		s.Logger.ErrorwCtx(ctx, "failed to publish webhook event",
			"error", err,
			"webhook_id", webhookEvent.ID,
			"event_name", eventName,
			"invoice_id", invoiceID,
		)
		return
	}

	s.Logger.InfowCtx(ctx, "webhook event published successfully",
		"webhook_id", webhookEvent.ID,
		"event_name", eventName,
		"invoice_id", invoiceID,
	)
}

// RecalculateInvoiceV2 recalculates a draft subscription invoice in-place (replaces line items, reapplies credits/coupons/taxes).
func (s *invoiceService) RecalculateInvoiceV2(ctx context.Context, id string, finalize bool) (*dto.InvoiceResponse, error) {
	s.Logger.InfowCtx(ctx, "recalculating invoice v2 (draft)", "invoice_id", id)

	// Get the invoice with its line items
	inv, err := s.getInvoiceWithLineItems(ctx, id)
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

	// Get sub with line items
	sub, _, err := s.SubRepo.GetWithLineItems(ctx, *inv.SubscriptionID)
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
			s.Logger.InfowCtx(ctx, "archived existing line items for fresh recalculation",
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
			sub,
			*inv.PeriodStart,
			*inv.PeriodEnd,
			referencePoint,
			"",
		)
		if err != nil {
			return err
		}

		// STEP 3: Update invoice totals, metadata, and customer ID
		// Use invoicing customer ID from the new invoice request (which uses sub.GetInvoicingCustomerID())
		// This ensures backward compatibility - if subscription has invoicing customer ID, use it; otherwise use subscription customer ID
		inv.Total = newInvoiceReq.Total
		inv.Subtotal = newInvoiceReq.Subtotal
		inv.CustomerID = newInvoiceReq.CustomerID
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
				ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_LINE_ITEM),
				InvoiceID:        inv.ID,
				CustomerID:       inv.CustomerID,
				SubscriptionID:   inv.SubscriptionID,
				EntityID:         lineItemReq.EntityID,
				EntityType:       lineItemReq.EntityType,
				PlanDisplayName:  lineItemReq.PlanDisplayName,
				PriceID:          lineItemReq.PriceID,
				PriceType:        lineItemReq.PriceType,
				DisplayName:      lineItemReq.DisplayName,
				MeterID:          lineItemReq.MeterID,
				MeterDisplayName: lineItemReq.MeterDisplayName,
				PriceUnit:        lineItemReq.PriceUnit,
				PriceUnitAmount:  lineItemReq.PriceUnitAmount,
				Amount:           lineItemReq.Amount,
				Quantity:         lineItemReq.Quantity,
				Currency:         inv.Currency,
				PeriodStart:      lineItemReq.PeriodStart,
				PeriodEnd:        lineItemReq.PeriodEnd,
				Metadata:         lineItemReq.Metadata,
				EnvironmentID:    inv.EnvironmentID,
				CommitmentInfo:   lineItemReq.CommitmentInfo,
				BaseModel:        types.GetDefaultBaseModel(txCtx),
			}
			newLineItems[i] = lineItem
		}

		// STEP 5: Add the newly calculated line items
		if len(newLineItems) > 0 {
			if err := s.InvoiceRepo.AddLineItems(txCtx, inv.ID, newLineItems); err != nil {
				return err
			}
		}

		// Attach new line items to inv so credits/coupons apply to them
		inv.LineItems = newLineItems

		// STEP 6: Update the invoice with subtotal/totals from billing
		if err := s.InvoiceRepo.Update(txCtx, inv); err != nil {
			return err
		}

		// STEP 6b: Apply credits and coupons (same order as CreateInvoice: coupons first, then credit adjustment)
		computeReq := newInvoiceReq.ToComputeRequest()
		if err := s.applyCreditsAndCouponsToInvoice(txCtx, inv, computeReq); err != nil {
			return err
		}
		if err := s.InvoiceRepo.Update(txCtx, inv); err != nil {
			return err
		}

		// STEP 7: Apply taxes after recalculation
		if err := s.RecalculateTaxesOnInvoice(txCtx, inv); err != nil {
			return err
		}

		s.Logger.InfowCtx(ctx, "successfully recalculated invoice with fresh calculation",
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
		s.Logger.ErrorwCtx(ctx, "failed to recalculate invoice",
			"error", err,
			"invoice_id", inv.ID,
			"subscription_id", *inv.SubscriptionID)
		return nil, err
	}

	// Publish webhook event for invoice recalculation
	s.publishSystemEvent(ctx, types.WebhookEventInvoiceUpdate, inv.ID)

	// Finalize the invoice if requested
	if finalize {
		if err := s.FinalizeInvoice(ctx, id); err != nil {
			s.Logger.ErrorwCtx(ctx, "failed to finalize invoice after recalculation",
				"error", err,
				"invoice_id", id)
			return nil, err
		}
		s.Logger.InfowCtx(ctx, "successfully finalized invoice after recalculation", "invoice_id", id)
	}

	// Return updated invoice
	return s.GetInvoice(ctx, id)
}

// RecalculateVoidedInvoice creates a fresh replacement invoice for a voided subscription invoice
// covering the same billing period. It validates that:
//   - The invoice is of type SUBSCRIPTION
//   - The invoice status is VOIDED
//   - The invoice has never been recalculated (RecalculatedInvoiceID == nil)
//
// On success it links the original voided invoice to the new invoice via RecalculatedInvoiceID.
func (s *invoiceService) RecalculateInvoice(ctx context.Context, id string) (*dto.InvoiceResponse, error) {
	s.Logger.InfowCtx(ctx, "recalculating voided invoice", "invoice_id", id)

	inv, err := s.InvoiceRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	if inv.InvoiceType != types.InvoiceTypeSubscription {
		return nil, ierr.NewError("invoice type is not supported").
			WithHintf("only SUBSCRIPTION invoices can be recalculated, got %s", inv.InvoiceType).
			Mark(ierr.ErrValidation)
	}

	if inv.InvoiceStatus != types.InvoiceStatusVoided {
		return nil, ierr.NewError("invoice is not voided").
			WithHint("only VOIDED invoices can be recalculated").
			WithReportableDetails(map[string]any{"current_status": inv.InvoiceStatus}).
			Mark(ierr.ErrValidation)
	}

	if inv.RecalculatedInvoiceID != nil {
		return nil, ierr.NewError("invoice has already been recalculated").
			WithHintf("invoice %s was already recalculated as %s", id, *inv.RecalculatedInvoiceID).
			WithReportableDetails(map[string]any{"recalculated_invoice_id": *inv.RecalculatedInvoiceID}).
			Mark(ierr.ErrValidation)
	}

	if inv.SubscriptionID == nil {
		return nil, ierr.NewError("invoice has no associated subscription").
			WithHint("subscription_id is required for recalculation").
			Mark(ierr.ErrValidation)
	}

	if inv.PeriodStart == nil || inv.PeriodEnd == nil {
		return nil, ierr.NewError("invoice has no billing period").
			WithHint("period_start and period_end are required for recalculation").
			Mark(ierr.ErrValidation)
	}

	// Fetch subscription with current line items (same as subscription billing flow).
	sub, _, err := s.SubRepo.GetWithLineItems(ctx, *inv.SubscriptionID)
	if err != nil {
		return nil, err
	}

	// Use the same method as subscription billing (processSubscriptionPeriod): CreateSubscriptionInvoice
	// with normalized payment params. The previous (voided) invoice does not conflict with new creation:
	// - GetByIdempotencyKey excludes VOIDED (invoice.InvoiceStatusNEQ(VOIDED)), so same idempotency key won't hit the old invoice.
	// - ExistsForPeriod excludes VOIDED, so period-uniqueness check allows the new invoice.
	// - DB partial unique index on (subscription_id, period_start, period_end) has WHERE invoice_status != 'VOIDED', so the new row is valid.
	// - Voiding calls InvoiceRepo.Update which DeleteCache(inv.ID) and clears idempotency-key cache, so cache won't return the voided invoice.
	paymentParams := dto.NewPaymentParametersFromSubscription(
		sub.CollectionMethod,
		sub.PaymentBehavior,
		sub.GatewayPaymentMethodID,
	).NormalizePaymentParameters()

	newInv, _, err := s.CreateSubscriptionInvoice(ctx, &dto.CreateSubscriptionInvoiceRequest{
		SubscriptionID: *inv.SubscriptionID,
		PeriodStart:    *inv.PeriodStart,
		PeriodEnd:      *inv.PeriodEnd,
		ReferencePoint: types.ReferencePointPeriodEnd,
	}, paymentParams, types.InvoiceFlowManual, false)
	if err != nil {
		return nil, err
	}
	if newInv == nil {
		return nil, ierr.NewError("recalculation produced no invoice").
			WithHint("recalculation resulted in zero subtotal for this period").
			Mark(ierr.ErrValidation)
	}

	// Link the original voided invoice to the new replacement invoice.
	inv.RecalculatedInvoiceID = &newInv.ID
	if err := s.InvoiceRepo.Update(ctx, inv); err != nil {
		return nil, err
	}

	s.Logger.InfowCtx(ctx, "successfully recalculated voided invoice",
		"original_invoice_id", id,
		"new_invoice_id", newInv.ID,
	)

	return s.GetInvoice(ctx, newInv.ID)
}

// RecalculateTaxesOnInvoice recalculates taxes on an invoice if it's a subscription invoice
func (s *invoiceService) RecalculateTaxesOnInvoice(ctx context.Context, inv *invoice.Invoice) error {
	// Only apply taxes to subscription invoices
	if inv.InvoiceType != types.InvoiceTypeSubscription || inv.SubscriptionID == nil {
		return nil
	}

	// Create a minimal request for tax preparation
	// applyTaxesToInvoice gets subscription ID and customer ID from the invoice itself
	req := dto.InvoiceComputeRequest{}

	// Apply taxes to invoice
	if err := s.applyTaxesToInvoice(ctx, inv, req); err != nil {
		return err
	}

	// Update the invoice in the database
	if err := s.InvoiceRepo.Update(ctx, inv); err != nil {
		s.Logger.ErrorwCtx(ctx, "failed to update invoice with tax amounts",
			"error", err,
			"invoice_id", inv.ID,
			"total_tax", inv.TotalTax,
			"new_total", inv.Total)
		return err
	}

	return nil
}

// applyTaxesToInvoice applies taxes to an invoice.
// For one-off invoices, uses prepared tax rates from req.PreparedTaxRates.
// For subscription invoices, prepares tax rates from subscription associations.
func (s *invoiceService) applyTaxesToInvoice(ctx context.Context, inv *invoice.Invoice, req dto.InvoiceComputeRequest) error {
	taxService := NewTaxService(s.ServiceParams)
	var taxRates []*dto.TaxRateResponse

	if len(req.PreparedTaxRates) > 0 {
		// Use prepared tax rates (from one-off invoices or billing service)
		taxRates = req.PreparedTaxRates
	} else if inv.SubscriptionID != nil {
		// Prepare tax rates for subscription invoices
		taxPrepareReq := dto.CreateInvoiceRequest{
			SubscriptionID: inv.SubscriptionID,
			CustomerID:     inv.CustomerID,
		}
		preparedTaxRates, err := taxService.PrepareTaxRatesForInvoice(ctx, taxPrepareReq)
		if err != nil {
			s.Logger.ErrorwCtx(ctx, "failed to prepare tax rates for invoice",
				"error", err,
				"invoice_id", inv.ID,
				"subscription_id", *inv.SubscriptionID)
			return err
		}
		taxRates = preparedTaxRates
	}

	// Apply taxes if we have any tax rates
	if len(taxRates) == 0 {
		return nil
	}

	taxResult, err := taxService.ApplyTaxesOnInvoice(ctx, inv, taxRates)
	if err != nil {
		return err
	}

	// Update the invoice with calculated tax amounts
	inv.TotalTax = taxResult.TotalTaxAmount
	// Discount-first-then-tax: total = subtotal - prepaid credits - discount + tax
	inv.Total = inv.Subtotal.Sub(inv.TotalPrepaidCreditsApplied).Sub(inv.TotalDiscount).Add(taxResult.TotalTaxAmount)
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
	}

	// Update metadata if provided
	if req.Metadata != nil {
		inv.Metadata = *req.Metadata
	}

	// Update the invoice in the repository
	if err := s.InvoiceRepo.Update(ctx, inv); err != nil {
		return nil, err
	}

	// Publish webhook event for invoice update
	s.publishSystemEvent(ctx, types.WebhookEventInvoiceUpdate, id)

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
	s.publishSystemEvent(ctx, types.WebhookEventInvoiceCommunicationTriggered, inv.ID)
	return nil
}

// TriggerWebhook manually triggers a webhook event for an invoice
// This is useful for debugging or replaying missed webhook events
func (s *invoiceService) TriggerWebhook(ctx context.Context, invoiceID string, eventName types.WebhookEventName) error {
	// Validate event name
	validEvents := []types.WebhookEventName{
		types.WebhookEventInvoiceUpdateFinalized,
		types.WebhookEventInvoiceUpdatePayment,
		types.WebhookEventInvoiceUpdateVoided,
		types.WebhookEventInvoiceCommunicationTriggered,
	}

	isValid := false
	for _, validEvent := range validEvents {
		if eventName == validEvent {
			isValid = true
			break
		}
	}

	if !isValid {
		return ierr.NewError("invalid event name").
			WithHint("Event must be one of: invoice.update.finalized, invoice.update.payment, invoice.update.voided, invoice.communication.triggered").
			WithReportableDetails(map[string]interface{}{
				"event_name":   eventName,
				"valid_events": validEvents,
			}).
			Mark(ierr.ErrValidation)
	}

	// Get invoice to verify it exists
	inv, err := s.InvoiceRepo.Get(ctx, invoiceID)
	if err != nil {
		return err
	}

	s.Logger.InfowCtx(ctx, "manually triggering webhook event",
		"invoice_id", inv.ID,
		"event_name", eventName,
	)

	// Publish webhook event
	s.publishSystemEvent(ctx, eventName, inv.ID)
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

	s.Logger.InfowCtx(ctx, "processing first invoice payment for subscription activation",
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

	s.Logger.InfowCtx(ctx, "successfully activated subscription after first invoice payment",
		"invoice_id", invoice.ID,
		"subscription_id", *invoice.SubscriptionID)

	return nil
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
func (s *invoiceService) CalculateUsageBreakdown(ctx context.Context, inv *dto.InvoiceResponse, groupBy []string, forceRuntimeRecalculation bool) (map[string][]dto.UsageBreakdownItem, error) {
	s.Logger.InfowCtx(ctx, "calculating usage breakdown for invoice",
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

	s.Logger.InfowCtx(ctx, "found usage-based line items",
		"total_line_items", len(inv.LineItems),
		"usage_based_line_items", len(usageBasedLineItems))

	if len(usageBasedLineItems) == 0 {
		// No usage-based line items, return empty analytics
		return make(map[string][]dto.UsageBreakdownItem), nil
	}

	// Use flexible grouping analytics call
	return s.getFlexibleUsageBreakdownForInvoice(ctx, usageBasedLineItems, inv, groupBy, forceRuntimeRecalculation)
}

// getFlexibleUsageBreakdownForInvoice gets usage breakdown with flexible grouping for invoice line items
// Groups line items by their billing periods for efficient analytics queries
func (s *invoiceService) getFlexibleUsageBreakdownForInvoice(ctx context.Context, usageBasedLineItems []*dto.InvoiceLineItemResponse, inv *dto.InvoiceResponse, groupBy []string, forceRuntimeRecalculation bool) (map[string][]dto.UsageBreakdownItem, error) {
	// Step 1: Get customer external ID first
	customer, err := s.CustomerRepo.Get(ctx, inv.CustomerID)
	if err != nil {
		s.Logger.ErrorwCtx(ctx, "failed to get customer for flexible usage breakdown",
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
			s.Logger.WarnwCtx(ctx, "skipping line item with missing price_id or meter_id",
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
		s.Logger.WarnwCtx(ctx, "no valid meter IDs found")
		return make(map[string][]dto.UsageBreakdownItem), nil
	}

	// Batch feature retrieval for all meters at once
	featureFilter := types.NewNoLimitFeatureFilter()
	featureFilter.MeterIDs = meterIDs
	features, err := s.FeatureRepo.List(ctx, featureFilter)
	if err != nil {
		s.Logger.ErrorwCtx(ctx, "failed to get features for meters",
			"meter_ids_count", len(meterIDs),
			"error", err)
		return nil, err
	}

	// Build meterID -> featureID mapping
	meterToFeatureMap := make(map[string]string) // meterID -> featureID
	for _, feature := range features {
		meterToFeatureMap[feature.MeterID] = feature.ID
	}

	s.Logger.InfowCtx(ctx, "batched feature retrieval",
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
			s.Logger.WarnwCtx(ctx, "no feature found for meter",
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
			s.Logger.WarnwCtx(ctx, "missing period information for line item",
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
		s.Logger.WarnwCtx(ctx, "no valid line items found with periods")
		return make(map[string][]dto.UsageBreakdownItem), nil
	}

	s.Logger.InfowCtx(ctx, "grouped line items by periods",
		"invoice_id", inv.ID,
		"period_groups_count", len(periodGroups),
		"total_line_items", len(usageBasedLineItems))

	// Step 3: Make analytics requests for each period group
	allAnalyticsItems := make([]dto.UsageAnalyticItem, 0)
	featureUsageTrackingService := NewFeatureUsageTrackingService(s.ServiceParams, s.EventRepo, s.FeatureUsageRepo)

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

		s.Logger.InfowCtx(ctx, "making period-specific analytics request",
			"invoice_id", inv.ID,
			"period_start", periodKey.Start.Format(time.RFC3339),
			"period_end", periodKey.End.Format(time.RFC3339),
			"feature_ids_count", len(featureIDsForPeriod),
			"line_items_count", len(lineItemsInPeriod),
			"group_by", groupBy)

		analyticsResponse, err := featureUsageTrackingService.GetDetailedUsageAnalytics(ctx, analyticsReq)
		if err != nil {
			s.Logger.ErrorwCtx(ctx, "failed to get period-specific usage analytics",
				"invoice_id", inv.ID,
				"period_start", periodKey.Start.Format(time.RFC3339),
				"period_end", periodKey.End.Format(time.RFC3339),
				"error", err)
			return nil, err
		}

		// Collect all analytics items
		allAnalyticsItems = append(allAnalyticsItems, analyticsResponse.Items...)

		s.Logger.DebugwCtx(ctx, "retrieved period-specific analytics",
			"invoice_id", inv.ID,
			"period_start", periodKey.Start.Format(time.RFC3339),
			"period_end", periodKey.End.Format(time.RFC3339),
			"analytics_items_count", len(analyticsResponse.Items))
	}

	// Step 4: Create combined response and map to line items
	combinedResponse := &dto.GetUsageAnalyticsResponse{
		Items: allAnalyticsItems,
	}

	s.Logger.InfowCtx(ctx, "combined all period analytics",
		"invoice_id", inv.ID,
		"total_analytics_items", len(allAnalyticsItems))

	// Step 5: Map results back to line items with flexible grouping
	return s.mapFlexibleAnalyticsToLineItems(combinedResponse, lineItemToFeatureMap, lineItemMetadata, groupBy, forceRuntimeRecalculation)
}

// mapFlexibleAnalyticsToLineItems maps analytics response to line items with flexible grouping
func (s *invoiceService) mapFlexibleAnalyticsToLineItems(analyticsResponse *dto.GetUsageAnalyticsResponse, lineItemToFeatureMap map[string]string, lineItemMetadata map[string]*dto.InvoiceLineItemResponse, groupBy []string, forceRuntimeRecalculation bool) (map[string][]dto.UsageBreakdownItem, error) {
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
		totalUsageRevenue := decimal.Zero
		for _, analyticsItem := range analyticsItems {

			if forceRuntimeRecalculation {
				totalUsageRevenue = totalUsageRevenue.Add(analyticsItem.TotalCost)
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
				Cost:      analyticsItem.TotalCost.StringFixed(2),
				GroupedBy: groupedBy,
			}

			// Add optional fields
			if !analyticsItem.TotalUsage.IsZero() {
				usageStr := analyticsItem.TotalUsage.StringFixed(2)
				breakdownItem.Usage = &usageStr
			}

			if analyticsItem.EventCount > 0 {
				eventCount := int(analyticsItem.EventCount)
				breakdownItem.EventCount = &eventCount
			}

			lineItemUsageBreakdown = append(lineItemUsageBreakdown, breakdownItem)
		}

		// Update the line item quantity and amount with totals from breakdown
		lineItem.Quantity = totalUsageForLineItem
		if forceRuntimeRecalculation {
			lineItem.Amount = totalUsageRevenue
		}

		// Assign to response
		usageBreakdownResponse[lineItemID] = lineItemUsageBreakdown

		s.Logger.Debugw("mapped flexible usage breakdown for line item",
			"line_item_id", lineItemID,
			"feature_id", featureID,
			"groups_count", len(lineItemUsageBreakdown),
			"total_usage", totalUsageForLineItem.StringFixed(2))
	}

	return usageBreakdownResponse, nil
}

// GetInvoiceWithBreakdown retrieves an invoice with optional usage breakdown
func (s *invoiceService) GetInvoiceWithBreakdown(ctx context.Context, req dto.GetInvoiceWithBreakdownRequest) (*dto.InvoiceResponse, error) {

	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Get the invoice first
	invoice, err := s.GetInvoice(ctx, req.ID)
	if err != nil {
		return nil, err
	}

	// Handle usage breakdown - prioritize group_by over expand_by_source for flexibility
	if len(req.GroupBy) > 0 {
		// Use flexible grouping
		usageBreakdown, err := s.CalculateUsageBreakdown(ctx, invoice, req.GroupBy, req.ForceRuntimeRecalculation)
		if err != nil {
			return nil, err
		}
		invoice.WithUsageBreakdown(usageBreakdown)

		// Recalculate invoice totals based on updated line item amounts

		if req.ForceRuntimeRecalculation {
			s.recalculateInvoiceTotals(invoice)
		}
	}

	return invoice, nil
}

// recalculateInvoiceTotals recalculates invoice subtotal, total, amount_due and amount_remaining
// based on updated line item amounts after usage breakdown calculation
func (s *invoiceService) recalculateInvoiceTotals(inv *dto.InvoiceResponse) {
	// Calculate new subtotal from line item amounts
	newSubtotal := decimal.Zero
	for _, lineItem := range inv.LineItems {
		newSubtotal = newSubtotal.Add(lineItem.Amount)
	}

	// Update subtotal
	inv.Subtotal = newSubtotal

	// Calculate new total: subtotal - discount + tax
	newTotal := newSubtotal.Sub(inv.TotalDiscount).Add(inv.TotalTax)
	if newTotal.IsNegative() {
		newTotal = decimal.Zero
	}

	// Update total and amount_due
	inv.Total = newTotal
	inv.AmountDue = newTotal

	// Calculate amount_remaining: total - amount_paid
	inv.AmountRemaining = newTotal.Sub(inv.AmountPaid)
	if inv.AmountRemaining.IsNegative() {
		inv.AmountRemaining = decimal.Zero
	}

	s.Logger.Debugw("recalculated invoice totals after usage breakdown",
		"invoice_id", inv.ID,
		"new_subtotal", newSubtotal.StringFixed(2),
		"new_total", newTotal.StringFixed(2),
		"amount_due", inv.AmountDue.StringFixed(2),
		"amount_remaining", inv.AmountRemaining.StringFixed(2))
}

// getAppliedTaxesForPDF retrieves and formats applied tax data for PDF generation
func (s *invoiceService) getAppliedTaxesForPDF(ctx context.Context, invoiceID string) ([]pdf.AppliedTaxData, error) {
	// Get applied taxes for this invoice with tax rate details expanded - SINGLE DB CALL!
	taxService := NewTaxService(s.ServiceParams)
	filter := types.NewNoLimitTaxAppliedFilter()
	filter.EntityType = types.TaxRateEntityTypeInvoice
	filter.EntityID = invoiceID
	filter.QueryFilter.Expand = lo.ToPtr(types.NewExpand("tax_rate").String())

	appliedTaxesResponse, err := taxService.ListTaxApplied(ctx, filter)
	if err != nil {
		return nil, err
	}

	s.Logger.DebugwCtx(ctx, "Applied taxes response", "count", len(appliedTaxesResponse.Items))
	for i, item := range appliedTaxesResponse.Items {
		s.Logger.DebugwCtx(ctx, "Applied tax item", "index", i, "tax_rate_id", item.TaxRateID, "has_tax_rate", item.TaxRate != nil)
		if item.TaxRate != nil {
			s.Logger.DebugwCtx(ctx, "Tax rate details", "name", item.TaxRate.Name, "code", item.TaxRate.Code)
		}
	}

	if len(appliedTaxesResponse.Items) == 0 {
		return []pdf.AppliedTaxData{}, nil
	}

	// Convert to PDF format using expanded tax rate data
	appliedTaxes := make([]pdf.AppliedTaxData, 0, len(appliedTaxesResponse.Items))
	for _, appliedTax := range appliedTaxesResponse.Items {
		// Round to currency precision before converting to float64
		precision := types.GetCurrencyPrecision(appliedTax.Currency)
		taxableAmount, _ := appliedTax.TaxableAmount.Round(precision).Float64()
		taxAmount, _ := appliedTax.TaxAmount.Round(precision).Float64()

		// Use expanded tax rate data if available
		var taxName, taxCode, taxType string
		var taxRateValue float64

		if appliedTax.TaxRate != nil {
			// Use expanded tax rate data
			taxName = appliedTax.TaxRate.Name
			taxCode = appliedTax.TaxRate.Code
			taxType = string(appliedTax.TaxRate.TaxRateType)
			if appliedTax.TaxRate.TaxRateType == types.TaxRateTypePercentage && appliedTax.TaxRate.PercentageValue != nil {
				taxRateValue, _ = appliedTax.TaxRate.PercentageValue.Round(precision).Float64()
			} else if appliedTax.TaxRate.TaxRateType == types.TaxRateTypeFixed && appliedTax.TaxRate.FixedValue != nil {
				taxRateValue, _ = appliedTax.TaxRate.FixedValue.Round(precision).Float64()
			}
		} else {
			// Fallback if tax rate not expanded - this should not happen if expand works
			s.Logger.ErrorwCtx(ctx, "Tax rate expand failed - falling back to basic info", "tax_rate_id", appliedTax.TaxRateID)
			taxName = "Tax Rate " + appliedTax.TaxRateID[len(appliedTax.TaxRateID)-6:] // Show last 6 chars
			taxCode = appliedTax.TaxRateID
			taxType = "Unknown"
			taxRateValue = 0
		}

		appliedTaxData := pdf.AppliedTaxData{
			TaxName:       taxName,
			TaxCode:       taxCode,
			TaxType:       taxType,
			TaxRate:       taxRateValue,
			TaxableAmount: taxableAmount,
			TaxAmount:     taxAmount,
			// AppliedAt:     appliedTax.AppliedAt.Format("Jan 02, 2006"),
		}

		appliedTaxes = append(appliedTaxes, appliedTaxData)
	}

	return appliedTaxes, nil
}

// getAppliedDiscountsForPDF retrieves and formats applied discount data for PDF generation
func (s *invoiceService) getAppliedDiscountsForPDF(ctx context.Context, inv *dto.InvoiceResponse) ([]pdf.AppliedDiscountData, error) {
	// Get coupon applications for this invoice
	filter := types.NewNoLimitCouponApplicationFilter()
	filter.InvoiceIDs = []string{inv.ID}
	applications, err := s.CouponApplicationRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Convert to DTOs
	couponApplications := make([]*dto.CouponApplicationResponse, len(applications))
	for i, app := range applications {
		couponApplications[i] = &dto.CouponApplicationResponse{
			CouponApplication: app,
		}
	}

	if len(couponApplications) == 0 {
		return []pdf.AppliedDiscountData{}, nil
	}

	// Get coupon service to fetch coupon details
	couponService := NewCouponService(s.ServiceParams)

	// Convert to PDF format using coupon application data
	appliedDiscounts := make([]pdf.AppliedDiscountData, 0, len(couponApplications))
	for _, couponApp := range couponApplications {
		// Round to currency precision before converting to float64
		precision := types.GetCurrencyPrecision(couponApp.Currency)
		discountAmount, _ := couponApp.DiscountedAmount.Round(precision).Float64()

		discountName := "Discount"
		// Get coupon name from coupon service
		if coupon, err := couponService.GetCoupon(ctx, couponApp.CouponID); err == nil && coupon != nil {
			discountName = coupon.Name
		}

		// Determine discount type and value
		var discountValue float64
		discountType := string(couponApp.DiscountType)
		if couponApp.DiscountType == types.CouponTypePercentage && couponApp.DiscountPercentage != nil {
			discountValue, _ = couponApp.DiscountPercentage.Round(precision).Float64()
		} else if couponApp.DiscountType == types.CouponTypeFixed {
			// For fixed discounts, use the actual discount amount as the value
			discountValue = discountAmount
		} else {
			// Fallback
			discountValue = discountAmount
		}

		// Determine line item reference
		lineItemRef := "--"
		if couponApp.InvoiceLineItemID != nil {
			// Find the line item display name for this line item ID
			for _, lineItem := range inv.LineItems {
				if lineItem.ID == *couponApp.InvoiceLineItemID {
					if lineItem.DisplayName != nil && *lineItem.DisplayName != "" {
						lineItemRef = *lineItem.DisplayName
					} else if lineItem.PlanDisplayName != nil && *lineItem.PlanDisplayName != "" {
						lineItemRef = *lineItem.PlanDisplayName
					}
					break
				}
			}
		}

		appliedDiscount := pdf.AppliedDiscountData{
			DiscountName:   discountName,
			Type:           discountType,
			Value:          discountValue,
			DiscountAmount: discountAmount,
			LineItemRef:    lineItemRef,
		}

		appliedDiscounts = append(appliedDiscounts, appliedDiscount)
	}

	return appliedDiscounts, nil
}

// isSafeUpdateForPaidInvoice checks if the update request contains only safe fields for paid invoices
func isSafeUpdateForPaidInvoice(_ dto.UpdateInvoiceRequest) bool {
	// Currently, UpdateInvoiceRequest only contains InvoicePDFURL and DueDate
	// Both of these are considered safe for paid invoices
	// In the future, if more fields are added, they should be categorized here

	// For now, all fields in UpdateInvoiceRequest are safe
	// This function is here for future extensibility
	return true
}

// DeleteInvoice deletes an invoice (stub implementation)
func (s *invoiceService) DeleteInvoice(ctx context.Context, id string) error {
	// TODO: Implement invoice deletion if needed
	return ierr.NewError("invoice deletion not implemented").
		WithHint("Invoice deletion is not currently supported").
		Mark(ierr.ErrNotFound)
}

// SyncInvoiceToExternalVendors syncs an invoice to external vendors
func (s *invoiceService) SyncInvoiceToExternalVendors(ctx context.Context, invoiceID string) error {
	inv, err := s.InvoiceRepo.Get(ctx, invoiceID)
	if err != nil {
		return err
	}
	if inv.InvoiceStatus == types.InvoiceStatusSkipped {
		return nil // No-op for zero-dollar skipped invoices
	}
	if inv.SubscriptionID == nil {
		return nil // No subscription to sync for non-subscription invoices
	}

	payload, err := json.Marshal(map[string]string{"invoice_id": invoiceID})
	if err != nil {
		return err
	}
	event := &types.WebhookEvent{
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
		UserID:        types.GetUserID(ctx),
		Payload:       payload,
	}
	return integrationevents.DispatchInvoiceVendorSync(
		ctx, s.Config, s.ConnectionRepo, s.EntityIntegrationMappingRepo, s.Logger, event, "",
	)
}

// applyCreditsAndCouponsToInvoice applies wallet credits and coupons to invoice, updating totals once
func (s *invoiceService) applyCreditsAndCouponsToInvoice(ctx context.Context, inv *invoice.Invoice, req dto.InvoiceComputeRequest) error {
	s.Logger.DebugwCtx(ctx, "applying credit adjustments and coupons to invoice",
		"invoice_id", inv.ID,
		"customer_id", inv.CustomerID,
		"currency", inv.Currency,
	)

	// Step 1: Apply coupons first (handles computation, persistence, and mutations internally)
	couponApplicationService := NewCouponApplicationService(s.ServiceParams)
	couponResult, err := couponApplicationService.ApplyCouponsToInvoice(ctx, dto.ApplyCouponsToInvoiceRequest{
		Invoice:         inv,
		InvoiceCoupons:  req.InvoiceCoupons,
		LineItemCoupons: req.LineItemCoupons,
	})
	if err != nil {
		return err
	}

	// Update invoice totals
	inv.TotalDiscount = couponResult.TotalDiscountAmount

	// Step 2: Apply credit adjustments after discounts
	// Credits are applied to: amount - line_item_discount - invoice_level_discount
	creditAdjustmentService := NewCreditAdjustmentService(s.ServiceParams)
	creditResult, err := creditAdjustmentService.ApplyCreditsToInvoice(ctx, inv)
	if err != nil {
		return err
	}
	inv.TotalPrepaidCreditsApplied = creditResult.TotalPrepaidCreditsApplied

	newTotal := inv.Subtotal.Sub(inv.TotalDiscount).Sub(inv.TotalPrepaidCreditsApplied)
	if newTotal.IsNegative() {
		newTotal = decimal.Zero
	}

	inv.Total = newTotal

	inv.AmountDue = inv.Total
	inv.AmountRemaining = inv.Total.Sub(inv.AmountPaid)

	s.Logger.InfowCtx(ctx, "successfully applied credit adjustments and coupons to invoice",
		"invoice_id", inv.ID,
		"total_prepaid_applied", inv.TotalPrepaidCreditsApplied,
		"total_discount", inv.TotalDiscount,
		"invoice_level_coupons", len(req.InvoiceCoupons),
		"line_item_level_coupons", len(req.LineItemCoupons),
		"new_total", inv.Total,
		"subtotal", inv.Subtotal,
	)

	return nil
}

// applyCouponsToInvoice applies only coupons/discounts to an invoice (no credit deduction or taxes).
// Credits and taxes are deferred to the finalization step.
func (s *invoiceService) applyCouponsToInvoice(ctx context.Context, inv *invoice.Invoice, req dto.InvoiceComputeRequest) error {
	s.Logger.DebugwCtx(ctx, "applying coupons to invoice",
		"invoice_id", inv.ID,
		"customer_id", inv.CustomerID,
		"currency", inv.Currency,
	)

	// Apply coupons (handles computation, persistence, and mutations internally)
	couponApplicationService := NewCouponApplicationService(s.ServiceParams)
	couponResult, err := couponApplicationService.ApplyCouponsToInvoice(ctx, dto.ApplyCouponsToInvoiceRequest{
		Invoice:         inv,
		InvoiceCoupons:  req.InvoiceCoupons,
		LineItemCoupons: req.LineItemCoupons,
	})
	if err != nil {
		return err
	}

	inv.TotalDiscount = couponResult.TotalDiscountAmount

	newTotal := inv.Subtotal.Sub(inv.TotalDiscount)
	if newTotal.IsNegative() {
		newTotal = decimal.Zero
	}

	inv.Total = newTotal
	inv.AmountDue = inv.Total
	inv.AmountRemaining = inv.Total.Sub(inv.AmountPaid)

	s.Logger.InfowCtx(ctx, "successfully applied coupons to invoice",
		"invoice_id", inv.ID,
		"total_discount", inv.TotalDiscount,
		"invoice_level_coupons", len(req.InvoiceCoupons),
		"line_item_level_coupons", len(req.LineItemCoupons),
		"new_total", inv.Total,
		"subtotal", inv.Subtotal,
	)

	return nil
}

// DistributeInvoiceLevelDiscount proportionally distributes an invoice-level discount across all line items
// using a precision-preserving algorithm with currency-aware rounding that ensures exact distribution.
//
// PURPOSE:
// When an invoice-level discount (e.g., from an invoice-level coupon) is applied, it must be
// distributed proportionally across all line items based on their remaining billable amounts
// (after line-item discounts). This allows tracking how much of the invoice-level discount is
// attributed to each line item, which is essential for accurate financial reporting, tax calculations,
// and audit trails.
//
// USE CASES:
// - Invoice-level coupon applications (e.g., "10% off entire invoice")
// - Invoice-level promotional discounts
// - Any discount applied at the invoice level that needs to be allocated to line items
//
// ALGORITHM: Proportional Distribution with Capping and Consistent Rounding
//
// 1. Filter eligible line items: amountAfterLineItemDiscount > 0
// 2. Calculate total eligible amount (sum of all eligible amounts)
// 3. Sort items by amount desc, then ID (for consistent rounding behavior)
// 4. Distribute proportionally to all except last item, capping at line item amount
// 5. Assign remainder to last item, capping at line item amount
//
// This ensures consistent distribution behavior and prevents over-allocation to any line item.
func (s *invoiceService) DistributeInvoiceLevelDiscount(ctx context.Context, lineItems []*invoice.InvoiceLineItem, invoiceDiscountAmount decimal.Decimal) error {
	// Early return if no discount to distribute
	if invoiceDiscountAmount.IsZero() {
		return nil
	}

	// Initialize InvoiceLevelDiscount to zero for all line items
	for _, lineItem := range lineItems {
		lineItem.InvoiceLevelDiscount = decimal.Zero
	}

	// Find eligible line items (non-zero amounts after line-item discount)
	eligibleItems := make([]*invoice.InvoiceLineItem, 0, len(lineItems))
	for _, lineItem := range lineItems {
		amountAfterLineItemDiscount := lineItem.Amount.Sub(lineItem.LineItemDiscount)
		if amountAfterLineItemDiscount.IsPositive() {
			eligibleItems = append(eligibleItems, lineItem)
		}
	}

	// If no eligible items or discount is zero, return early
	if len(eligibleItems) == 0 {
		return nil
	}

	// Calculate total eligible amount
	totalEligibleAmount := decimal.Zero
	for _, lineItem := range eligibleItems {
		amountAfterLineItemDiscount := lineItem.Amount.Sub(lineItem.LineItemDiscount)
		totalEligibleAmount = totalEligibleAmount.Add(amountAfterLineItemDiscount)
	}

	// Edge case: Cannot distribute discount if all line items are fully discounted
	if totalEligibleAmount.IsZero() {
		s.Logger.InfowCtx(ctx, "cannot distribute invoice-level discount: all line items already fully discounted",
			"total_discount", invoiceDiscountAmount,
			"line_items_count", len(eligibleItems))
		return nil
	}

	// Sort items for consistent rounding behavior (by amount descending)
	// If amounts are equal, order doesn't matter
	sort.Slice(eligibleItems, func(i, j int) bool {
		amountI := eligibleItems[i].Amount.Sub(eligibleItems[i].LineItemDiscount)
		amountJ := eligibleItems[j].Amount.Sub(eligibleItems[j].LineItemDiscount)

		// Sort by amount descending (greater or equal)
		return amountI.GreaterThanOrEqual(amountJ)
	})

	// Track remaining discount amount
	remainingDiscount := invoiceDiscountAmount

	// First pass - apply to all except last item
	for i, lineItem := range eligibleItems {
		if i == len(eligibleItems)-1 {
			break // Skip last item, handled separately
		}

		// Line item's share of invoice discount = (Line item amount after line item discount / Total invoice amount after line item discounts) × Total invoice-level discount amount
		amountAfterLineItemDiscount := lineItem.Amount.Sub(lineItem.LineItemDiscount)

		proportion := amountAfterLineItemDiscount.Div(totalEligibleAmount)
		lineItemShare := proportion.Mul(invoiceDiscountAmount)

		// Round using currency precision
		lineItemShare = types.RoundToCurrencyPrecision(lineItemShare, lineItem.Currency)

		// Cap at line item amount (cannot exceed the eligible amount)
		if lineItemShare.GreaterThan(amountAfterLineItemDiscount) {
			lineItemShare = amountAfterLineItemDiscount
		}

		lineItem.InvoiceLevelDiscount = lineItemShare
		remainingDiscount = remainingDiscount.Sub(lineItemShare)
	}

	// Handle last item (with any rounding adjustment)
	if len(eligibleItems) > 0 {
		lastItem := eligibleItems[len(eligibleItems)-1]
		amountAfterLineItemDiscount := lastItem.Amount.Sub(lastItem.LineItemDiscount)

		// Cap at remaining discount and line item amount
		lastItemShare := remainingDiscount
		if lastItemShare.GreaterThan(amountAfterLineItemDiscount) {
			lastItemShare = amountAfterLineItemDiscount
		}

		// Ensure non-negative
		if lastItemShare.IsNegative() {
			lastItemShare = decimal.Zero
		}

		lastItem.InvoiceLevelDiscount = lastItemShare
	}

	return nil
}
