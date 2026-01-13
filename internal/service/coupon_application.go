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
	TotalDiscountAmount decimal.Decimal
	AppliedCoupons      []*dto.CouponApplicationResponse
	Currency            string
	Metadata            map[string]interface{}
}

type CouponApplicationService interface {
	CreateCouponApplication(ctx context.Context, req dto.CreateCouponApplicationRequest) (*dto.CouponApplicationResponse, error)
	GetCouponApplication(ctx context.Context, id string) (*dto.CouponApplicationResponse, error)
	ApplyLineItemCouponsToInvoice(ctx context.Context, inv *invoice.Invoice, lineItemCoupons []dto.InvoiceLineItemCoupon) (*CouponCalculationResult, error)
	ApplyInvoiceLevelCouponsToInvoice(ctx context.Context, inv *invoice.Invoice, invoiceCoupons []dto.InvoiceCoupon) (*CouponCalculationResult, error)
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

// ApplyLineItemCouponsToInvoice applies line item-level coupons to an invoice.
func (s *couponApplicationService) ApplyLineItemCouponsToInvoice(ctx context.Context, inv *invoice.Invoice, lineItemCoupons []dto.InvoiceLineItemCoupon) (*CouponCalculationResult, error) {
	result := &CouponCalculationResult{
		TotalDiscountAmount: decimal.Zero,
		AppliedCoupons:      make([]*dto.CouponApplicationResponse, 0),
		Currency:            inv.Currency,
		Metadata:            make(map[string]interface{}),
	}
	if len(lineItemCoupons) == 0 {
		return result, nil
	}

	s.Logger.Infow("applying line item coupons to invoice",
		"invoice_id", inv.ID,
		"line_item_coupon_count", len(lineItemCoupons),
	)

	// Step 1: Fetch all coupons upfront before transaction (fail fast if any missing)
	couponIDs := make([]string, 0, len(lineItemCoupons))
	for _, lic := range lineItemCoupons {
		couponIDs = append(couponIDs, lic.CouponID)
	}

	couponFilter := types.NewNoLimitCouponFilter()
	couponFilter.CouponIDs = couponIDs
	coupons, err := s.CouponRepo.List(ctx, couponFilter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to fetch coupons").
			Mark(ierr.ErrDatabase)
	}

	couponsMap := make(map[string]*coupon.Coupon)

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
					"coupon_ids":        couponIDs,
				}).
				Mark(ierr.ErrNotFound)
		}
	}

	// Build a map of line items by PriceID for O(1) lookup
	lineItemsByPriceID := make(map[string]*invoice.InvoiceLineItem)
	for _, lineItem := range inv.LineItems {
		if lineItem.PriceID != nil {
			lineItemsByPriceID[lo.FromPtr(lineItem.PriceID)] = lineItem
		}
	}

	// Step 2: Prepare all data outside transaction (calculations, validations, entity building)
	couponService := NewCouponService(s.ServiceParams)
	totalDiscount := decimal.Zero
	appliedCoupons := make([]*dto.CouponApplicationResponse, 0)
	lineItemCouponApplications := make([]*coupon_application.CouponApplication, 0)

	// Process line item coupons (outside transaction)
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

		// Build coupon application entity (outside transaction)
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
		totalDiscount = totalDiscount.Add(discountResult.Discount)

		// Set LineItemDiscount on the line item (accumulate if multiple coupons apply to same item)
		targetLineItem.LineItemDiscount = targetLineItem.LineItemDiscount.Add(discountResult.Discount)

		s.Logger.Debugw("prepared line item coupon application",
			"line_item_id", targetLineItem.ID,
			"price_id", lineItemCoupon.LineItemID,
			"coupon_id", lineItemCoupon.CouponID,
			"original_amount", targetLineItem.Amount,
			"discount", discountResult.Discount,
			"line_item_discount", targetLineItem.LineItemDiscount,
			"final_price", discountResult.FinalPrice)
	}

	// Step 3: Minimal transaction - only database writes
	err = s.DB.WithTx(ctx, func(txCtx context.Context) error {
		// Create line item coupon applications
		for _, ca := range lineItemCouponApplications {
			if err := s.CouponApplicationRepo.Create(txCtx, ca); err != nil {
				s.Logger.Errorw("failed to create line item coupon application",
					"coupon_id", ca.CouponID,
					"error", err)
				return err
			}
		}

		// Update line items with discounts
		for _, lineItem := range inv.LineItems {
			if !lineItem.LineItemDiscount.IsZero() {
				if err := s.InvoiceRepo.UpdateLineItem(txCtx, lineItem); err != nil {
					s.Logger.Errorw("failed to update line item with discount",
						"line_item_id", lineItem.ID,
						"line_item_discount", lineItem.LineItemDiscount,
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

	result = &CouponCalculationResult{
		TotalDiscountAmount: totalDiscount,
		AppliedCoupons:      appliedCoupons,
		Currency:            inv.Currency,
		Metadata: map[string]interface{}{
			"line_item_level_coupons": len(lineItemCoupons),
			"successful_applications": len(appliedCoupons),
		},
	}

	s.Logger.Infow("completed line item coupon application to invoice",
		"invoice_id", inv.ID,
		"total_discount", result.TotalDiscountAmount,
		"applied_coupon_count", len(result.AppliedCoupons))

	return result, nil
}

// ApplyInvoiceLevelCouponsToInvoice applies invoice-level coupons to an invoice.
// It considers prepaid credits applied when calculating the running subtotal.
func (s *couponApplicationService) ApplyInvoiceLevelCouponsToInvoice(ctx context.Context, inv *invoice.Invoice, invoiceCoupons []dto.InvoiceCoupon) (*CouponCalculationResult, error) {
	result := &CouponCalculationResult{
		TotalDiscountAmount: decimal.Zero,
		AppliedCoupons:      make([]*dto.CouponApplicationResponse, 0),
		Currency:            inv.Currency,
		Metadata:            make(map[string]interface{}),
	}
	if len(invoiceCoupons) == 0 {
		return result, nil
	}

	s.Logger.Infow("applying invoice level coupons to invoice",
		"invoice_id", inv.ID,
		"invoice_coupon_count", len(invoiceCoupons),
		"total_prepaid_credits_applied", inv.TotalPrepaidCreditsApplied,
	)

	// Step 1: Fetch all coupons upfront before transaction (fail fast if any missing)
	couponIDs := make([]string, 0, len(invoiceCoupons))
	for _, ic := range invoiceCoupons {
		couponIDs = append(couponIDs, ic.CouponID)
	}

	couponFilter := types.NewNoLimitCouponFilter()
	couponFilter.CouponIDs = couponIDs
	coupons, err := s.CouponRepo.List(ctx, couponFilter)
	if err != nil {
		return nil, err
	}

	couponsMap := make(map[string]*coupon.Coupon)
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
					"coupon_ids":        couponIDs,
				}).
				Mark(ierr.ErrNotFound)
		}
	}

	// Step 2: Calculate total line item discounts (if any were applied)
	totalLineItemDiscount := decimal.Zero
	for _, lineItem := range inv.LineItems {
		totalLineItemDiscount = totalLineItemDiscount.Add(lineItem.LineItemDiscount)
	}

	// Step 3: Prepare all data outside transaction (calculations, validations, entity building)
	couponService := NewCouponService(s.ServiceParams)
	totalDiscount := decimal.Zero
	appliedCoupons := make([]*dto.CouponApplicationResponse, 0)
	invoiceLevelCouponApplications := make([]*coupon_application.CouponApplication, 0)

	// Process invoice-level coupons (outside transaction)
	// Key change: running subtotal now accounts for prepaid credits applied
	// Formula: subtotal - line item discounts - prepaid credits applied
	runningSubTotal := inv.Subtotal.Sub(totalLineItemDiscount).Sub(inv.TotalPrepaidCreditsApplied)
	for _, invoiceCoupon := range invoiceCoupons {
		// Coupon already validated to exist in map
		coupon := couponsMap[invoiceCoupon.CouponID]

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

		// Build coupon application entity (outside transaction)
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
			OriginalPrice:       runningSubTotal,
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
		totalDiscount = totalDiscount.Add(discountResult.Discount)
		runningSubTotal = discountResult.FinalPrice

		s.Logger.Debugw("prepared invoice coupon application",
			"coupon_id", invoiceCoupon.CouponID,
			"original_subtotal", runningSubTotal.Add(discountResult.Discount),
			"discount", discountResult.Discount,
			"final_subtotal", discountResult.FinalPrice)
	}

	// Step 4: Minimal transaction - only database writes
	err = s.DB.WithTx(ctx, func(txCtx context.Context) error {
		// Create invoice-level coupon applications
		for _, ca := range invoiceLevelCouponApplications {
			if err := s.CouponApplicationRepo.Create(txCtx, ca); err != nil {
				s.Logger.Errorw("failed to create invoice coupon application",
					"coupon_id", ca.CouponID,
					"error", err)
				return err
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	result = &CouponCalculationResult{
		TotalDiscountAmount: totalDiscount,
		AppliedCoupons:      appliedCoupons,
		Currency:            inv.Currency,
		Metadata: map[string]interface{}{
			"invoice_level_coupons":   len(invoiceCoupons),
			"successful_applications": len(appliedCoupons),
			"validation_failures":     len(invoiceCoupons) - len(appliedCoupons),
		},
	}

	s.Logger.Infow("completed invoice level coupon application to invoice",
		"invoice_id", inv.ID,
		"total_discount", result.TotalDiscountAmount,
		"applied_coupon_count", len(result.AppliedCoupons))

	return result, nil
}
