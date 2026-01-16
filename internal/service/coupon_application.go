package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/coupon"
	"github.com/flexprice/flexprice/internal/domain/coupon_application"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// CouponCalculationResult holds the result of applying coupons to an invoice
type CouponCalculationResult struct {
	TotalDiscountAmount          decimal.Decimal
	TotalInvoiceLineItemDiscount decimal.Decimal
	TotalInvoiceLevelDiscount    decimal.Decimal
}

type CouponApplicationService interface {
	CreateCouponApplication(ctx context.Context, req dto.CreateCouponApplicationRequest) (*dto.CouponApplicationResponse, error)
	GetCouponApplication(ctx context.Context, id string) (*dto.CouponApplicationResponse, error)
	ApplyCouponsToInvoice(ctx context.Context, req dto.ApplyCouponsToInvoiceRequest) (*CouponCalculationResult, error)
}

type couponApplicationService struct {
	ServiceParams
}

func NewCouponApplicationService(
	params ServiceParams,
) CouponApplicationService {
	return &couponApplicationService{
		ServiceParams: params,
	}
}

func (s *couponApplicationService) CreateCouponApplication(ctx context.Context, req dto.CreateCouponApplicationRequest) (*dto.CouponApplicationResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	var response *dto.CouponApplicationResponse

	// Use transaction for atomic operations
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		baseModel := types.GetDefaultBaseModel(txCtx)
		ca := &coupon_application.CouponApplication{
			ID:                  types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON_APPLICATION),
			CouponID:            req.CouponID,
			CouponAssociationID: req.CouponAssociationID,
			InvoiceID:           req.InvoiceID,
			InvoiceLineItemID:   req.InvoiceLineItemID,
			SubscriptionID:      req.SubscriptionID,
			AppliedAt:           time.Now(),
			OriginalPrice:       req.OriginalPrice,
			FinalPrice:          req.FinalPrice,
			DiscountedAmount:    req.DiscountedAmount,
			DiscountType:        req.DiscountType,
			DiscountPercentage:  req.DiscountPercentage,
			Currency:            req.Currency,
			CouponSnapshot:      req.CouponSnapshot,
			Metadata:            req.Metadata,
			BaseModel:           baseModel,
			EnvironmentID:       types.GetEnvironmentID(txCtx),
		}

		if err := s.CouponApplicationRepo.Create(txCtx, ca); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to create coupon application").
				Mark(ierr.ErrInternal)
		}

		response = &dto.CouponApplicationResponse{
			CouponApplication: ca,
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return response, nil
}

// GetCouponApplication retrieves a coupon application by ID
func (s *couponApplicationService) GetCouponApplication(ctx context.Context, id string) (*dto.CouponApplicationResponse, error) {
	ca, err := s.CouponApplicationRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	return &dto.CouponApplicationResponse{
		CouponApplication: ca,
	}, nil
}

// ApplyCouponsToInvoice applies both invoice-level and line item-level coupons to an invoice.
// This is the unified method that handles all coupon application logic.
// CouponService.ApplyDiscount() handles all validation and calculation.
func (s *couponApplicationService) ApplyCouponsToInvoice(ctx context.Context, req dto.ApplyCouponsToInvoiceRequest) (*CouponCalculationResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	inv := req.Invoice
	invoiceCoupons := req.InvoiceCoupons
	lineItemCoupons := req.LineItemCoupons

	result := &CouponCalculationResult{
		TotalDiscountAmount:          decimal.Zero,
		TotalInvoiceLineItemDiscount: decimal.Zero,
		TotalInvoiceLevelDiscount:    decimal.Zero,
	}
	if len(invoiceCoupons) == 0 && len(lineItemCoupons) == 0 {
		return result, nil
	}

	s.Logger.Infow("applying coupons to invoice",
		"invoice_id", inv.ID,
		"invoice_coupon_count", len(invoiceCoupons),
		"line_item_coupon_count", len(lineItemCoupons),
		"original_total", inv.Total)

	// Step 1: Fetch all coupons upfront before transaction (fail fast if any missing)
	couponIDs := make([]string, 0, len(invoiceCoupons)+len(lineItemCoupons))
	for _, ic := range invoiceCoupons {
		couponIDs = append(couponIDs, ic.CouponID)
	}
	for _, lic := range lineItemCoupons {
		couponIDs = append(couponIDs, lic.CouponID)
	}

	couponsMap := make(map[string]*coupon.Coupon)
	if len(couponIDs) == 0 {
		return result, nil
	}
	couponFilter := types.NewNoLimitCouponFilter()
	couponFilter.CouponIDs = couponIDs
	coupons, err := s.CouponRepo.List(ctx, couponFilter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to fetch coupons").
			Mark(ierr.ErrDatabase)
	}

	for _, c := range coupons {
		couponsMap[c.ID] = c
	}

	// Validate all coupons exist - fail fast if any missing
	for _, couponID := range couponIDs {
		if _, exists := couponsMap[couponID]; !exists {
			return nil, ierr.NewError("one or more coupons not found").
				WithHint("Coupons must exist before applying to invoice").
				WithReportableDetails(map[string]interface{}{
					"missing_coupon_id": couponID,
				}).
				Mark(ierr.ErrNotFound)
		}
	}

	// Step 2: Prepare all data outside transaction (calculations, validations, entity building)
	// This is a pure function - we do NOT mutate inv or inv.LineItems
	couponService := NewCouponService(s.ServiceParams)
	totalLineItemDiscount := decimal.Zero
	totalInvoiceLevelDiscount := decimal.Zero
	appliedCoupons := make([]*dto.CouponApplicationResponse, 0)
	lineItemCouponApplications := make([]*coupon_application.CouponApplication, 0)
	invoiceLevelCouponApplications := make([]*coupon_application.CouponApplication, 0)

	// Build a map of line items by PriceID for O(1) lookup
	lineItemsByPriceID := make(map[string]*invoice.InvoiceLineItem)
	for _, lineItem := range inv.LineItems {
		if lineItem.PriceID != nil {
			lineItemsByPriceID[lo.FromPtr(lineItem.PriceID)] = lineItem
		}
	}

	// Process line item coupons (mutate line items directly since we'll persist them in DB)
	for _, lineItemCoupon := range lineItemCoupons {
		// Find the line item this coupon applies to by matching price_id
		targetLineItem, exists := lineItemsByPriceID[lineItemCoupon.LineItemID]
		if !exists {
			s.Logger.Warnw("line item not found for coupon, skipping",
				"price_id_used_as_line_item_id", lineItemCoupon.LineItemID,
				"coupon_id", lineItemCoupon.CouponID)
			continue
		}

		// Coupon already validated to exist in map
		coupon := couponsMap[lineItemCoupon.CouponID]

		// Use correct ApplyDiscount signature
		discountResult, err := couponService.ApplyDiscount(ctx, dto.ApplyDiscountRequest{
			CouponID:      lineItemCoupon.CouponID,
			OriginalPrice: targetLineItem.Amount,
			Currency:      inv.Currency,
		})
		if err != nil {
			s.Logger.Warnw("failed to apply line item coupon, skipping",
				"coupon_id", lineItemCoupon.CouponID,
				"error", err)
			continue
		}

		// Accumulate discount for this line item (multiple coupons can apply to same item)
		// Mutate line item directly since we'll persist it in DB anyway
		targetLineItem.LineItemDiscount = targetLineItem.LineItemDiscount.Add(discountResult.Discount)
		totalLineItemDiscount = totalLineItemDiscount.Add(discountResult.Discount)

		// Build coupon application entity (for persistence by caller)
		couponAssociationID := ""
		if lineItemCoupon.CouponAssociationID != nil {
			couponAssociationID = *lineItemCoupon.CouponAssociationID
		}
		ca := &coupon_application.CouponApplication{
			ID:                  types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON_APPLICATION),
			CouponID:            lineItemCoupon.CouponID,
			CouponAssociationID: couponAssociationID,
			InvoiceID:           inv.ID,
			InvoiceLineItemID:   &targetLineItem.ID,
			SubscriptionID:      inv.SubscriptionID,
			AppliedAt:           time.Now(),
			OriginalPrice:       targetLineItem.Amount,
			FinalPrice:          discountResult.FinalPrice,
			DiscountedAmount:    discountResult.Discount,
			DiscountType:        coupon.Type,
			DiscountPercentage:  coupon.PercentageOff,
			Currency:            inv.Currency,
			CouponSnapshot: map[string]interface{}{
				"type":           coupon.Type,
				"amount_off":     coupon.AmountOff,
				"percentage_off": coupon.PercentageOff,
				"applied_to":     "line_item",
				"line_item_id":   targetLineItem.ID,
				"price_id":       lineItemCoupon.LineItemID,
			},
			BaseModel:     types.GetDefaultBaseModel(ctx),
			EnvironmentID: types.GetEnvironmentID(ctx),
		}

		lineItemCouponApplications = append(lineItemCouponApplications, ca)
		appliedCoupons = append(appliedCoupons, &dto.CouponApplicationResponse{
			CouponApplication: ca,
		})

		s.Logger.Debugw("prepared line item coupon application",
			"line_item_id", targetLineItem.ID,
			"price_id", lineItemCoupon.LineItemID,
			"coupon_id", lineItemCoupon.CouponID,
			"original_amount", targetLineItem.Amount,
			"discount", discountResult.Discount,
			"accumulated_discount", targetLineItem.LineItemDiscount,
			"final_price", discountResult.FinalPrice)
	}

	// Calculate subtotal after line item discounts
	// This is the base for invoice-level discounts
	subtotalAfterLineItemDiscounts := decimal.Zero
	for _, lineItem := range inv.LineItems {
		amountAfterLineItemDiscount := lineItem.Amount.Sub(lineItem.LineItemDiscount)
		subtotalAfterLineItemDiscounts = subtotalAfterLineItemDiscounts.Add(amountAfterLineItemDiscount)
	}

	// Process invoice-level coupons (pure computation - no mutations)
	// Apply sequentially to subtotal after line item discounts
	runningSubTotal := subtotalAfterLineItemDiscounts
	for _, invoiceCoupon := range invoiceCoupons {
		// Skip if running subtotal is zero or negative (nothing to discount)
		if runningSubTotal.LessThanOrEqual(decimal.Zero) {
			s.Logger.Warnw("running subtotal is zero or negative, skipping remaining invoice coupons",
				"running_subtotal", runningSubTotal,
				"coupon_id", invoiceCoupon.CouponID)
			break
		}

		// Coupon already validated to exist in map
		coupon := couponsMap[invoiceCoupon.CouponID]

		// Use correct ApplyDiscount signature
		discountResult, err := couponService.ApplyDiscount(ctx, dto.ApplyDiscountRequest{
			CouponID:      invoiceCoupon.CouponID,
			OriginalPrice: runningSubTotal,
			Currency:      inv.Currency,
		})
		if err != nil {
			s.Logger.Warnw("failed to apply invoice coupon, skipping",
				"coupon_id", invoiceCoupon.CouponID,
				"error", err)
			continue
		}

		totalInvoiceLevelDiscount = totalInvoiceLevelDiscount.Add(discountResult.Discount)
		runningSubTotal = discountResult.FinalPrice

		// Build coupon application entity (for persistence by caller)
		couponAssociationID := ""
		if invoiceCoupon.CouponAssociationID != nil {
			couponAssociationID = *invoiceCoupon.CouponAssociationID
		}
		ca := &coupon_application.CouponApplication{
			ID:                  types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON_APPLICATION),
			CouponID:            invoiceCoupon.CouponID,
			CouponAssociationID: couponAssociationID,
			InvoiceID:           inv.ID,
			SubscriptionID:      inv.SubscriptionID,
			AppliedAt:           time.Now(),
			OriginalPrice:       runningSubTotal.Add(discountResult.Discount), // Original before this discount
			FinalPrice:          discountResult.FinalPrice,
			DiscountedAmount:    discountResult.Discount,
			DiscountType:        coupon.Type,
			DiscountPercentage:  coupon.PercentageOff,
			Currency:            inv.Currency,
			CouponSnapshot: map[string]interface{}{
				"type":           coupon.Type,
				"amount_off":     coupon.AmountOff,
				"percentage_off": coupon.PercentageOff,
				"applied_to":     "invoice",
			},
			BaseModel:     types.GetDefaultBaseModel(ctx),
			EnvironmentID: types.GetEnvironmentID(ctx),
		}

		invoiceLevelCouponApplications = append(invoiceLevelCouponApplications, ca)
		appliedCoupons = append(appliedCoupons, &dto.CouponApplicationResponse{
			CouponApplication: ca,
		})

		s.Logger.Debugw("prepared invoice coupon application",
			"coupon_id", invoiceCoupon.CouponID,
			"original_subtotal", runningSubTotal.Add(discountResult.Discount),
			"discount", discountResult.Discount,
			"final_subtotal", discountResult.FinalPrice)
	}

	// Step 3: Apply mutations in transaction (mutations at boundaries)
	// Computation was pure, now we apply the results
	totalDiscountAmount := totalLineItemDiscount.Add(totalInvoiceLevelDiscount)

	err = s.DB.WithTx(ctx, func(txCtx context.Context) error {
		// Persist coupon applications
		for _, ca := range appliedCoupons {
			if err := s.CouponApplicationRepo.Create(txCtx, ca.CouponApplication); err != nil {
				s.Logger.Errorw("failed to create coupon application",
					"coupon_application_id", ca.CouponApplication.ID,
					"error", err)
				return err
			}
		}

		// Explicitly distribute invoice-level discount (mutating method - name makes it clear)
		if !totalInvoiceLevelDiscount.IsZero() && len(inv.LineItems) > 0 {
			invoiceService := NewInvoiceService(s.ServiceParams)
			if err := invoiceService.DistributeInvoiceLevelDiscount(txCtx, inv.LineItems, totalInvoiceLevelDiscount); err != nil {
				s.Logger.Errorw("failed to distribute invoice-level discount",
					"invoice_id", inv.ID,
					"total_invoice_level_discount", totalInvoiceLevelDiscount,
					"error", err)
				return err
			}
		}

		// Update line items with all discounts
		for _, lineItem := range inv.LineItems {
			if !lineItem.LineItemDiscount.IsZero() || !lineItem.InvoiceLevelDiscount.IsZero() {
				if err := s.InvoiceRepo.UpdateLineItem(txCtx, lineItem); err != nil {
					s.Logger.Errorw("failed to update line item with discount",
						"line_item_id", lineItem.ID,
						"line_item_discount", lineItem.LineItemDiscount,
						"invoice_level_discount", lineItem.InvoiceLevelDiscount,
						"error", err)
					return err
				}
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Build result after mutations are applied
	result = &CouponCalculationResult{
		TotalDiscountAmount:          totalDiscountAmount,
		TotalInvoiceLineItemDiscount: totalLineItemDiscount,
		TotalInvoiceLevelDiscount:    totalInvoiceLevelDiscount,
	}

	s.Logger.Infow("completed coupon application to invoice",
		"invoice_id", inv.ID,
		"total_discount", result.TotalDiscountAmount,
		"applied_coupon_count", len(appliedCoupons))

	return result, nil
}
