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
	GetCouponApplicationsByInvoice(ctx context.Context, invoiceID string) ([]*dto.CouponApplicationResponse, error)
	GetCouponApplicationsBySubscription(ctx context.Context, subscriptionID string) ([]*dto.CouponApplicationResponse, error)
	ApplyCouponToInvoice(ctx context.Context, couponID string, invoiceID string, originalPrice decimal.Decimal) (*dto.CouponApplicationResponse, error)
	ApplyCouponsOnInvoice(ctx context.Context, inv *invoice.Invoice, invoiceCoupons []dto.InvoiceCoupon) (*CouponCalculationResult, error)
	ApplyCouponsOnInvoiceWithLineItems(ctx context.Context, inv *invoice.Invoice, invoiceCoupons []dto.InvoiceCoupon, lineItemCoupons []dto.InvoiceLineItemCoupon) (*CouponCalculationResult, error)
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

		response = s.toCouponApplicationResponse(ca)
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

	return s.toCouponApplicationResponse(ca), nil
}

// GetCouponApplicationsByInvoice retrieves coupon applications for an invoice
func (s *couponApplicationService) GetCouponApplicationsByInvoice(ctx context.Context, invoiceID string) ([]*dto.CouponApplicationResponse, error) {
	applications, err := s.CouponApplicationRepo.GetByInvoice(ctx, invoiceID)
	if err != nil {
		return nil, err
	}

	responses := make([]*dto.CouponApplicationResponse, len(applications))
	for i, ca := range applications {
		responses[i] = s.toCouponApplicationResponse(ca)
	}

	return responses, nil
}

// GetCouponApplicationsBySubscription retrieves coupon applications for a subscription
func (s *couponApplicationService) GetCouponApplicationsBySubscription(ctx context.Context, subscriptionID string) ([]*dto.CouponApplicationResponse, error) {
	applications, err := s.CouponApplicationRepo.GetBySubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	responses := make([]*dto.CouponApplicationResponse, len(applications))
	for i, ca := range applications {
		responses[i] = s.toCouponApplicationResponse(ca)
	}

	return responses, nil
}

// ApplyCouponToInvoice applies a coupon to an invoice and creates a coupon application
func (s *couponApplicationService) ApplyCouponToInvoice(ctx context.Context, couponID string, invoiceID string, originalPrice decimal.Decimal) (*dto.CouponApplicationResponse, error) {
	// Validate input parameters
	if couponID == "" {
		return nil, ierr.NewError("coupon_id is required").
			WithHint("Please provide a valid coupon ID").
			Mark(ierr.ErrValidation)
	}

	if invoiceID == "" {
		return nil, ierr.NewError("invoice_id is required").
			WithHint("Please provide a valid invoice ID").
			Mark(ierr.ErrValidation)
	}

	if originalPrice.LessThan(decimal.Zero) {
		return nil, ierr.NewError("original_price must be greater than zero").
			WithHint("Please provide a valid original price").
			WithReportableDetails(map[string]interface{}{
				"original_price": originalPrice,
			}).
			Mark(ierr.ErrValidation)
	}

	s.Logger.Infow("applying coupon to invoice",
		"coupon_id", couponID,
		"invoice_id", invoiceID,
		"original_price", originalPrice)

	var response *dto.CouponApplicationResponse

	// Use transaction for atomic operations
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		// Get the coupon
		c, err := s.CouponRepo.Get(txCtx, couponID)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to get coupon for invoice application").
				WithReportableDetails(map[string]interface{}{
					"coupon_id":  couponID,
					"invoice_id": invoiceID,
				}).
				Mark(ierr.ErrNotFound)
		}

		// Validate coupon is active
		if c.Status != types.StatusPublished {
			return ierr.NewError("only active coupons can be applied").
				WithHint("Please select an active coupon").
				WithReportableDetails(map[string]interface{}{
					"coupon_id":  couponID,
					"invoice_id": invoiceID,
					"status":     c.Status,
				}).
				Mark(ierr.ErrValidation)
		}

		// Validate coupon is valid for redemption
		if !c.IsValid() {
			return ierr.NewError("coupon is not valid for redemption").
				WithHint("Coupon may be expired, have reached maximum redemptions, or not yet available for redemption").
				WithReportableDetails(map[string]interface{}{
					"coupon_id":         couponID,
					"invoice_id":        invoiceID,
					"redeem_after":      c.RedeemAfter,
					"redeem_before":     c.RedeemBefore,
					"total_redemptions": c.TotalRedemptions,
					"max_redemptions":   c.MaxRedemptions,
				}).
				Mark(ierr.ErrValidation)
		}

		// Calculate discount
		discount := c.CalculateDiscount(originalPrice)
		finalPrice := c.ApplyDiscount(originalPrice)

		// Validate that the final price is not negative
		if finalPrice.LessThan(decimal.Zero) {
			return ierr.NewError("discount amount exceeds original price").
				WithHint("The discount amount cannot be greater than the original price").
				WithReportableDetails(map[string]interface{}{
					"coupon_id":      couponID,
					"invoice_id":     invoiceID,
					"original_price": originalPrice,
					"discount":       discount,
					"final_price":    finalPrice,
				}).
				Mark(ierr.ErrValidation)
		}

		// Create coupon application
		req := dto.CreateCouponApplicationRequest{
			CouponID:         couponID,
			InvoiceID:        invoiceID,
			OriginalPrice:    originalPrice,
			FinalPrice:       finalPrice,
			DiscountedAmount: discount,
			DiscountType:     c.Type,
			Currency:         c.Currency,
			CouponSnapshot: map[string]interface{}{
				"name":           c.Name,
				"type":           c.Type,
				"cadence":        c.Cadence,
				"amount_off":     c.AmountOff,
				"percentage_off": c.PercentageOff,
			},
		}

		if c.Type == types.CouponTypePercentage {
			req.DiscountPercentage = c.PercentageOff
		}

		application, err := s.CreateCouponApplication(txCtx, req)
		if err != nil {
			s.Logger.Errorw("failed to create coupon application for invoice",
				"coupon_id", couponID,
				"invoice_id", invoiceID,
				"error", err)
			return err
		}

		s.Logger.Infow("successfully applied coupon to invoice",
			"coupon_id", couponID,
			"invoice_id", invoiceID,
			"application_id", application.ID,
			"original_price", originalPrice,
			"final_price", finalPrice,
			"discount", discount)

		response = application
		return nil
	})

	if err != nil {
		return nil, err
	}

	return response, nil
}

// ApplyCouponsOnInvoiceWithLineItems applies both invoice-level and line item-level coupons to an invoice
func (s *couponApplicationService) ApplyCouponsOnInvoiceWithLineItems(ctx context.Context, inv *invoice.Invoice, invoiceCoupons []dto.InvoiceCoupon, lineItemCoupons []dto.InvoiceLineItemCoupon) (*CouponCalculationResult, error) {
	if len(invoiceCoupons) == 0 && len(lineItemCoupons) == 0 {
		return &CouponCalculationResult{
			TotalDiscountAmount: decimal.Zero,
			AppliedCoupons:      make([]*dto.CouponApplicationResponse, 0),
			Currency:            inv.Currency,
			Metadata:            make(map[string]interface{}),
		}, nil
	}

	s.Logger.Infow("applying coupons to invoice with line item support",
		"invoice_id", inv.ID,
		"invoice_coupon_count", len(invoiceCoupons),
		"line_item_coupon_count", len(lineItemCoupons),
		"original_total", inv.Total)

	var result *CouponCalculationResult

	// Use transaction for atomic operations
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		totalDiscount := decimal.Zero
		applicationRequests := make([]dto.CreateCouponApplicationRequest, 0, len(invoiceCoupons)+len(lineItemCoupons))

		// Step 1: Apply line item level coupons first
		lineItemDiscounts := make(map[string]decimal.Decimal) // lineItemID -> total discount for that line item
		for _, lineItemCoupon := range lineItemCoupons {
			// Find the line item this coupon applies to by matching price_id
			// (lineItemCoupon.LineItemID contains the price_id from billing service)
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

			// Calculate discount for this line item
			originalLineItemAmount := targetLineItem.Amount
			discount := lineItemCoupon.CalculateDiscount(originalLineItemAmount)
			finalPrice := lineItemCoupon.ApplyDiscount(originalLineItemAmount)

			// Create application request for line item coupon
			req := dto.CreateCouponApplicationRequest{
				CouponID:          lineItemCoupon.CouponID,
				InvoiceID:         inv.ID,
				InvoiceLineItemID: &targetLineItem.ID,
				OriginalPrice:     originalLineItemAmount,
				FinalPrice:        finalPrice,
				DiscountedAmount:  discount,
				DiscountType:      lineItemCoupon.Type,
				Currency:          inv.Currency,
				CouponSnapshot: map[string]interface{}{
					"type":           lineItemCoupon.Type,
					"amount_off":     lineItemCoupon.AmountOff,
					"percentage_off": lineItemCoupon.PercentageOff,
					"applied_to":     "line_item",
					"line_item_id":   targetLineItem.ID,
					"price_id":       lineItemCoupon.LineItemID,
				},
			}

			// Set association ID if provided
			if lineItemCoupon.CouponAssociationID != nil {
				req.CouponAssociationID = *lineItemCoupon.CouponAssociationID
			}

			if lineItemCoupon.Type == types.CouponTypePercentage {
				req.DiscountPercentage = lineItemCoupon.PercentageOff
			}

			if inv.SubscriptionID != nil {
				req.SubscriptionID = inv.SubscriptionID
			}

			applicationRequests = append(applicationRequests, req)
			totalDiscount = totalDiscount.Add(discount)

			// Track line item discount for invoice total calculation
			lineItemDiscounts[targetLineItem.ID] = lineItemDiscounts[targetLineItem.ID].Add(discount)

			s.Logger.Debugw("applied line item coupon",
				"line_item_id", targetLineItem.ID,
				"price_id", lineItemCoupon.LineItemID,
				"coupon_id", lineItemCoupon.CouponID,
				"original_amount", originalLineItemAmount,
				"discount", discount,
				"final_price", finalPrice)
		}

		// Step 2: Apply invoice-level coupons to the remaining invoice total
		// Calculate the new invoice total after line item discounts
		adjustedInvoiceTotal := inv.Total.Sub(totalDiscount)
		runningTotal := adjustedInvoiceTotal

		for _, invoiceCoupon := range invoiceCoupons {
			// Calculate discount for this coupon based on the running total
			discount := invoiceCoupon.CalculateDiscount(runningTotal)
			finalPrice := invoiceCoupon.ApplyDiscount(runningTotal)

			// Create application request for invoice-level coupon
			req := dto.CreateCouponApplicationRequest{
				CouponID:         invoiceCoupon.CouponID,
				InvoiceID:        inv.ID,
				OriginalPrice:    runningTotal,
				FinalPrice:       finalPrice,
				DiscountedAmount: discount,
				DiscountType:     invoiceCoupon.Type,
				Currency:         inv.Currency,
				CouponSnapshot: map[string]interface{}{
					"type":           invoiceCoupon.Type,
					"amount_off":     invoiceCoupon.AmountOff,
					"percentage_off": invoiceCoupon.PercentageOff,
					"applied_to":     "invoice",
				},
			}

			// Set association ID if provided
			if invoiceCoupon.CouponAssociationID != nil {
				req.CouponAssociationID = *invoiceCoupon.CouponAssociationID
			}

			if invoiceCoupon.Type == types.CouponTypePercentage {
				req.DiscountPercentage = invoiceCoupon.PercentageOff
			}

			if inv.SubscriptionID != nil {
				req.SubscriptionID = inv.SubscriptionID
			}

			applicationRequests = append(applicationRequests, req)
			totalDiscount = totalDiscount.Add(discount)
			runningTotal = finalPrice

			s.Logger.Debugw("applied invoice coupon",
				"coupon_id", invoiceCoupon.CouponID,
				"original_total", runningTotal.Add(discount),
				"discount", discount,
				"final_total", finalPrice)
		}

		// Batch create coupon applications
		appliedCoupons, err := s.batchCreateCouponApplications(txCtx, applicationRequests)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to batch create coupon applications").
				Mark(ierr.ErrInternal)
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

	s.Logger.Infow("completed coupon application to invoice with line items",
		"invoice_id", inv.ID,
		"total_discount", result.TotalDiscountAmount,
		"applied_coupon_count", len(result.AppliedCoupons))

	return result, nil
}

// ApplyCouponsOnInvoice applies coupons to an invoice with optimized batch processing
func (s *couponApplicationService) ApplyCouponsOnInvoice(ctx context.Context, inv *invoice.Invoice, invoiceCoupons []dto.InvoiceCoupon) (*CouponCalculationResult, error) {
	if len(invoiceCoupons) == 0 {
		return &CouponCalculationResult{
			TotalDiscountAmount: decimal.Zero,
			AppliedCoupons:      make([]*dto.CouponApplicationResponse, 0),
			Currency:            inv.Currency,
			Metadata:            make(map[string]interface{}),
		}, nil
	}

	s.Logger.Infow("applying coupons to invoice",
		"invoice_id", inv.ID,
		"coupon_count", len(invoiceCoupons),
		"original_total", inv.Total)

	var result *CouponCalculationResult

	// Use transaction for atomic operations
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {

		// Process valid coupons and create applications in batch
		totalDiscount := decimal.Zero
		runningTotal := inv.Total
		applicationRequests := make([]dto.CreateCouponApplicationRequest, 0, len(invoiceCoupons))

		// Calculate discounts for all valid coupons
		for _, invoiceCoupon := range invoiceCoupons {
			// Calculate discount for this coupon based on the running total
			discount := invoiceCoupon.CalculateDiscount(runningTotal)
			finalPrice := invoiceCoupon.ApplyDiscount(runningTotal)

			// Create application request
			req := dto.CreateCouponApplicationRequest{
				CouponID:         invoiceCoupon.CouponID,
				InvoiceID:        inv.ID,
				OriginalPrice:    runningTotal,
				FinalPrice:       finalPrice,
				DiscountedAmount: discount,
				DiscountType:     invoiceCoupon.Type,
				Currency:         inv.Currency,
				CouponSnapshot: map[string]interface{}{
					"type":           invoiceCoupon.Type,
					"amount_off":     invoiceCoupon.AmountOff,
					"percentage_off": invoiceCoupon.PercentageOff,
				},
			}

			// Set association ID only if association exists
			if invoiceCoupon.CouponAssociationID != nil {
				req.CouponAssociationID = *invoiceCoupon.CouponAssociationID
			}

			if invoiceCoupon.Type == types.CouponTypePercentage {
				req.DiscountPercentage = invoiceCoupon.PercentageOff
			}

			if inv.SubscriptionID != nil {
				req.SubscriptionID = inv.SubscriptionID
			}

			applicationRequests = append(applicationRequests, req)
			totalDiscount = totalDiscount.Add(discount)
			runningTotal = finalPrice
		}

		// Batch create coupon applications
		appliedCoupons, err := s.batchCreateCouponApplications(txCtx, applicationRequests)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to batch create coupon applications").
				Mark(ierr.ErrInternal)
		}

		result = &CouponCalculationResult{
			TotalDiscountAmount: totalDiscount,
			AppliedCoupons:      appliedCoupons,
			Currency:            inv.Currency,
			Metadata: map[string]interface{}{
				"total_coupons_processed": len(invoiceCoupons),
				"successful_applications": len(appliedCoupons),
				"validation_failures":     len(invoiceCoupons) - len(appliedCoupons),
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
		"applied_coupons", len(result.AppliedCoupons))

	return result, nil
}

func (s *couponApplicationService) batchCreateCouponApplications(ctx context.Context, requests []dto.CreateCouponApplicationRequest) ([]*dto.CouponApplicationResponse, error) {
	if len(requests) == 0 {
		return []*dto.CouponApplicationResponse{}, nil
	}

	s.Logger.Debugw("batch creating coupon applications",
		"application_count", len(requests))

	responses := make([]*dto.CouponApplicationResponse, 0, len(requests))
	baseModel := types.GetDefaultBaseModel(ctx)

	// Create applications in batch using optimized approach
	for _, req := range requests {
		// Validate each request
		if err := req.Validate(); err != nil {
			s.Logger.Errorw("invalid coupon application request",
				"coupon_id", req.CouponID,
				"invoice_id", req.InvoiceID,
				"error", err)
			continue
		}

		// Create the application
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
			EnvironmentID:       types.GetEnvironmentID(ctx),
		}

		if err := s.CouponApplicationRepo.Create(ctx, ca); err != nil {
			s.Logger.Errorw("failed to create coupon application in batch",
				"coupon_id", req.CouponID,
				"invoice_id", req.InvoiceID,
				"error", err)
			continue
		}

		responses = append(responses, s.toCouponApplicationResponse(ca))

		s.Logger.Debugw("successfully created coupon application",
			"application_id", ca.ID,
			"coupon_id", req.CouponID,
			"invoice_id", req.InvoiceID,
			"discount", req.DiscountedAmount)
	}

	s.Logger.Infow("completed batch coupon application creation",
		"requested_count", len(requests),
		"successful_count", len(responses))

	return responses, nil
}

// Helper method to convert domain models to DTOs
func (s *couponApplicationService) toCouponResponse(c *coupon.Coupon) *dto.CouponResponse {
	return &dto.CouponResponse{
		Coupon: c,
	}
}

func (s *couponApplicationService) toCouponApplicationResponse(ca *coupon_application.CouponApplication) *dto.CouponApplicationResponse {
	return &dto.CouponApplicationResponse{
		CouponApplication: ca,
	}
}
