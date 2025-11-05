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
	ApplyCouponsToInvoice(ctx context.Context, inv *invoice.Invoice, invoiceCoupons []dto.InvoiceCoupon, lineItemCoupons []dto.InvoiceLineItemCoupon) (*CouponCalculationResult, error)
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
func (s *couponApplicationService) ApplyCouponsToInvoice(ctx context.Context, inv *invoice.Invoice, invoiceCoupons []dto.InvoiceCoupon, lineItemCoupons []dto.InvoiceLineItemCoupon) (*CouponCalculationResult, error) {
	if len(invoiceCoupons) == 0 && len(lineItemCoupons) == 0 {
		return &CouponCalculationResult{
			TotalDiscountAmount: decimal.Zero,
			AppliedCoupons:      make([]*dto.CouponApplicationResponse, 0),
			Currency:            inv.Currency,
			Metadata:            make(map[string]interface{}),
		}, nil
	}

	s.Logger.Infow("applying coupons to invoice",
		"invoice_id", inv.ID,
		"invoice_coupon_count", len(invoiceCoupons),
		"line_item_coupon_count", len(lineItemCoupons),
		"original_total", inv.Total)

	var result *CouponCalculationResult
	couponService := NewCouponService(s.ServiceParams)

	// Use transaction for atomic operations
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		// Batch fetch all coupons upfront to avoid duplicate fetches
		couponIDs := make([]string, 0, len(invoiceCoupons)+len(lineItemCoupons))
		for _, ic := range invoiceCoupons {
			couponIDs = append(couponIDs, ic.CouponID)
		}
		for _, lic := range lineItemCoupons {
			couponIDs = append(couponIDs, lic.CouponID)
		}

		couponsMap := make(map[string]*coupon.Coupon)
		if len(couponIDs) > 0 {
			coupons, err := s.CouponRepo.GetBatch(txCtx, couponIDs)
			if err != nil {
				s.Logger.Warnw("failed to batch fetch coupons, will fetch individually", "error", err)
			} else {
				for _, c := range coupons {
					couponsMap[c.ID] = c
				}
			}
		}

		totalDiscount := decimal.Zero
		appliedCoupons := make([]*dto.CouponApplicationResponse, 0)
		lineItemDiscounts := make(map[string]decimal.Decimal) // lineItemID -> total discount for that line item

		// Step 1: Apply line item level coupons first
		for _, lineItemCoupon := range lineItemCoupons {
			// Find the line item this coupon applies to by matching price_id
			var targetLineItem *invoice.InvoiceLineItem
			for _, lineItem := range inv.LineItems {
				if lineItem.PriceID != nil && *lineItem.PriceID == lineItemCoupon.LineItemID {
					targetLineItem = lineItem
					break
				}
			}

			if targetLineItem == nil {
				s.Logger.Warnw("line item not found for coupon, skipping",
					"price_id_used_as_line_item_id", lineItemCoupon.LineItemID,
					"coupon_id", lineItemCoupon.CouponID)
				continue
			}

			// Calculate discount using CouponService which handles validation and calculation
			originalLineItemAmount := targetLineItem.Amount

			// Get coupon from batch-fetched map or fetch individually
			coupon := couponsMap[lineItemCoupon.CouponID]
			if coupon == nil {
				var err error
				coupon, err = s.CouponRepo.Get(txCtx, lineItemCoupon.CouponID)
				if err != nil {
					s.Logger.Warnw("failed to get coupon, skipping",
						"coupon_id", lineItemCoupon.CouponID,
						"error", err)
					continue
				}
			}

			discountResult, err := couponService.ApplyDiscount(txCtx, *coupon, originalLineItemAmount)
			if err != nil {
				s.Logger.Warnw("failed to apply line item coupon, skipping",
					"coupon_id", lineItemCoupon.CouponID,
					"error", err)
				continue
			}

			// Create coupon application
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
				OriginalPrice:       originalLineItemAmount,
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
				BaseModel:     types.GetDefaultBaseModel(txCtx),
				EnvironmentID: types.GetEnvironmentID(txCtx),
			}

			if err := s.CouponApplicationRepo.Create(txCtx, ca); err != nil {
				s.Logger.Warnw("failed to create line item coupon application, skipping",
					"coupon_id", lineItemCoupon.CouponID,
					"error", err)
				continue
			}

			appliedCoupons = append(appliedCoupons, &dto.CouponApplicationResponse{
				CouponApplication: ca,
			})
			totalDiscount = totalDiscount.Add(discountResult.Discount)
			lineItemDiscounts[targetLineItem.ID] = lineItemDiscounts[targetLineItem.ID].Add(discountResult.Discount)

			s.Logger.Debugw("applied line item coupon",
				"line_item_id", targetLineItem.ID,
				"price_id", lineItemCoupon.LineItemID,
				"coupon_id", lineItemCoupon.CouponID,
				"original_amount", originalLineItemAmount,
				"discount", discountResult.Discount,
				"final_price", discountResult.FinalPrice)
		}

		// Step 2: Apply invoice-level coupons to the remaining invoice total
		runningSubTotal := inv.Subtotal.Sub(totalDiscount)

		for _, invoiceCoupon := range invoiceCoupons {
			// Get coupon from batch-fetched map or fetch individually
			coupon := couponsMap[invoiceCoupon.CouponID]
			if coupon == nil {
				var err error
				coupon, err = s.CouponRepo.Get(txCtx, invoiceCoupon.CouponID)
				if err != nil {
					s.Logger.Warnw("failed to get coupon, skipping",
						"coupon_id", invoiceCoupon.CouponID,
						"error", err)
					continue
				}
			}

			// Calculate discount using CouponService which handles validation and calculation
			discountResult, err := couponService.ApplyDiscount(txCtx, *coupon, runningSubTotal)
			if err != nil {
				s.Logger.Warnw("failed to apply invoice coupon, skipping",
					"coupon_id", invoiceCoupon.CouponID,
					"error", err)
				continue
			}

			// Create coupon application
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
				BaseModel:     types.GetDefaultBaseModel(txCtx),
				EnvironmentID: types.GetEnvironmentID(txCtx),
			}

			if err := s.CouponApplicationRepo.Create(txCtx, ca); err != nil {
				s.Logger.Warnw("failed to create invoice coupon application, skipping",
					"coupon_id", invoiceCoupon.CouponID,
					"error", err)
				continue
			}

			appliedCoupons = append(appliedCoupons, &dto.CouponApplicationResponse{
				CouponApplication: ca,
			})
			totalDiscount = totalDiscount.Add(discountResult.Discount)
			runningSubTotal = discountResult.FinalPrice

			s.Logger.Debugw("applied invoice coupon",
				"coupon_id", invoiceCoupon.CouponID,
				"original_subtotal", runningSubTotal.Add(discountResult.Discount),
				"discount", discountResult.Discount,
				"final_subtotal", discountResult.FinalPrice)
		}

		result = &CouponCalculationResult{
			TotalDiscountAmount: totalDiscount,
			AppliedCoupons:      appliedCoupons,
			Currency:            inv.Currency,
			Metadata: map[string]interface{}{
				"total_coupons_processed":    len(invoiceCoupons) + len(lineItemCoupons),
				"successful_applications":    len(appliedCoupons),
				"validation_failures":        (len(invoiceCoupons) + len(lineItemCoupons)) - len(appliedCoupons),
				"invoice_level_coupons":      len(invoiceCoupons),
				"line_item_level_coupons":    len(lineItemCoupons),
				"line_item_discount_details": lineItemDiscounts,
			},
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	s.Logger.Infow("completed coupon application to invoice",
		"invoice_id", inv.ID,
		"total_discount", result.TotalDiscountAmount,
		"applied_coupon_count", len(result.AppliedCoupons))

	return result, nil
}
