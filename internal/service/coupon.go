package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/coupon"
	"github.com/flexprice/flexprice/internal/domain/coupon_application"
	"github.com/flexprice/flexprice/internal/domain/coupon_association"
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

// CouponWithAssociation represents a coupon with its association details
type CouponWithAssociation struct {
	Coupon      *dto.CouponResponse
	Association *dto.CouponAssociationResponse
}

// CouponService defines the interface for coupon operations
type CouponService interface {
	// Coupon operations
	CreateCoupon(ctx context.Context, req dto.CreateCouponRequest) (*dto.CouponResponse, error)
	GetCoupon(ctx context.Context, id string) (*dto.CouponResponse, error)
	UpdateCoupon(ctx context.Context, id string, req dto.UpdateCouponRequest) (*dto.CouponResponse, error)
	DeleteCoupon(ctx context.Context, id string) error
	ListCoupons(ctx context.Context, filter *types.CouponFilter) (*dto.ListCouponsResponse, error)

	// Coupon association operations
	CreateCouponAssociation(ctx context.Context, req dto.CreateCouponAssociationRequest) (*dto.CouponAssociationResponse, error)
	GetCouponAssociation(ctx context.Context, id string) (*dto.CouponAssociationResponse, error)
	DeleteCouponAssociation(ctx context.Context, id string) error
	GetCouponAssociationsBySubscription(ctx context.Context, subscriptionID string) ([]*dto.CouponAssociationResponse, error)

	// Coupon application operations
	CreateCouponApplication(ctx context.Context, req dto.CreateCouponApplicationRequest) (*dto.CouponApplicationResponse, error)
	GetCouponApplication(ctx context.Context, id string) (*dto.CouponApplicationResponse, error)
	GetCouponApplicationsByInvoice(ctx context.Context, invoiceID string) ([]*dto.CouponApplicationResponse, error)
	GetCouponApplicationsBySubscription(ctx context.Context, subscriptionID string) ([]*dto.CouponApplicationResponse, error)

	// Business logic operations
	ApplyCouponToSubscription(ctx context.Context, couponIDs []string, subscriptionID string) (*dto.CouponAssociationResponse, error)
	ApplyCouponToInvoice(ctx context.Context, couponID string, invoiceID string, originalPrice decimal.Decimal) (*dto.CouponApplicationResponse, error)
	ValidateCouponForSubscription(ctx context.Context, couponID string, subscriptionID string) error
	CalculateDiscount(ctx context.Context, couponID string, originalPrice decimal.Decimal) (decimal.Decimal, error)

	// Invoice integration operations (following tax pattern)
	PrepareCouponsForInvoice(ctx context.Context, req dto.CreateInvoiceRequest) ([]*CouponWithAssociation, error)
	ApplyCouponsOnInvoice(ctx context.Context, inv *invoice.Invoice, couponsWithAssociations []*CouponWithAssociation) (*CouponCalculationResult, error)
}

type couponService struct {
	ServiceParams
}

// NewCouponService creates a new coupon service
func NewCouponService(
	params ServiceParams,
) CouponService {
	return &couponService{
		ServiceParams: params,
	}
}

// CreateCoupon creates a new coupon
func (s *couponService) CreateCoupon(ctx context.Context, req dto.CreateCouponRequest) (*dto.CouponResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	baseModel := types.GetDefaultBaseModel(ctx)
	c := &coupon.Coupon{
		ID:                types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON),
		Name:              req.Name,
		RedeemAfter:       req.RedeemAfter,
		RedeemBefore:      req.RedeemBefore,
		MaxRedemptions:    req.MaxRedemptions,
		TotalRedemptions:  0,
		Rules:             req.Rules,
		AmountOff:         req.AmountOff,
		PercentageOff:     req.PercentageOff,
		Type:              req.Type,
		Cadence:           req.Cadence,
		DurationInPeriods: req.DurationInPeriods,
		Metadata:          req.Metadata,
		Currency:          *req.Currency,
		BaseModel:         baseModel,
		EnvironmentID:     types.GetEnvironmentID(ctx),
	}

	if err := s.CouponRepo.Create(ctx, c); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to create coupon").
			Mark(ierr.ErrInternal)
	}

	return s.toCouponResponse(c), nil
}

// GetCoupon retrieves a coupon by ID
func (s *couponService) GetCoupon(ctx context.Context, id string) (*dto.CouponResponse, error) {
	c, err := s.CouponRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	return s.toCouponResponse(c), nil
}

// UpdateCoupon updates an existing coupon
func (s *couponService) UpdateCoupon(ctx context.Context, id string, req dto.UpdateCouponRequest) (*dto.CouponResponse, error) {
	c, err := s.CouponRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Update fields if provided
	if req.Name != nil {
		c.Name = *req.Name
	}

	if req.Metadata != nil {
		c.Metadata = req.Metadata
	}

	c.UpdatedAt = time.Now()
	c.UpdatedBy = types.GetUserID(ctx)

	if err := s.CouponRepo.Update(ctx, c); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to update coupon").
			Mark(ierr.ErrInternal)
	}

	return s.toCouponResponse(c), nil
}

// DeleteCoupon deletes a coupon
func (s *couponService) DeleteCoupon(ctx context.Context, id string) error {
	return s.CouponRepo.Delete(ctx, id)
}

// ListCoupons lists coupons with filtering
func (s *couponService) ListCoupons(ctx context.Context, filter *types.CouponFilter) (*dto.ListCouponsResponse, error) {
	if filter == nil {
		filter = types.NewCouponFilter()
	}

	if err := filter.Validate(); err != nil {
		return nil, err
	}

	coupons, err := s.CouponRepo.List(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list coupons").
			Mark(ierr.ErrInternal)
	}

	total, err := s.CouponRepo.Count(ctx, filter)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to count coupons").
			Mark(ierr.ErrInternal)
	}

	responses := make([]*dto.CouponResponse, len(coupons))
	for i, c := range coupons {
		responses[i] = s.toCouponResponse(c)
	}

	listResponse := types.NewListResponse(responses, total, filter.GetLimit(), filter.GetOffset())
	return &listResponse, nil
}

// CreateCouponAssociation creates a new coupon association
func (s *couponService) CreateCouponAssociation(ctx context.Context, req dto.CreateCouponAssociationRequest) (*dto.CouponAssociationResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Validate that subscription_id is provided (mandatory)
	if req.SubscriptionID == "" {
		return nil, ierr.NewError("subscription_id is required").
			WithHint("Please provide a subscription ID").
			Mark(ierr.ErrValidation)
	}

	// subscription_line_item_id is optional - can be nil for subscription-level associations

	var response *dto.CouponAssociationResponse

	// Use transaction for atomic operations
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		// Use validation service for coupon-subscription validation
		validationService := NewCouponValidationService(s.ServiceParams)
		if err := validationService.ValidateCouponForSubscription(txCtx, req.CouponID, req.SubscriptionID); err != nil {
			return ierr.WithError(err).
				WithHint("Coupon validation failed for subscription association").
				WithReportableDetails(map[string]interface{}{
					"coupon_id":       req.CouponID,
					"subscription_id": req.SubscriptionID,
				}).
				Mark(ierr.ErrValidation)
		}

		// Check for existing association to prevent duplicates
		existingAssociations, err := s.CouponAssociationRepo.GetBySubscription(txCtx, req.SubscriptionID)
		if err != nil {
			s.Logger.Warnw("failed to check existing coupon associations",
				"subscription_id", req.SubscriptionID,
				"error", err)
			// Don't fail the operation for this check
		} else {
			for _, existing := range existingAssociations {
				if existing.CouponID == req.CouponID {
					return ierr.NewError("coupon is already associated with this subscription").
						WithHint("This coupon is already applied to the subscription").
						WithReportableDetails(map[string]interface{}{
							"coupon_id":       req.CouponID,
							"subscription_id": req.SubscriptionID,
						}).
						Mark(ierr.ErrAlreadyExists)
				}
			}
		}

		// Create the coupon association object properly
		baseModel := types.GetDefaultBaseModel(txCtx)
		ca := &coupon_association.CouponAssociation{
			ID:                     types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON_ASSOCIATION),
			CouponID:               req.CouponID,
			SubscriptionID:         req.SubscriptionID,
			SubscriptionLineItemID: req.SubscriptionLineItemID,
			Metadata:               req.Metadata,
			BaseModel:              baseModel,
			EnvironmentID:          types.GetEnvironmentID(txCtx),
		}

		if err := s.CouponAssociationRepo.Create(txCtx, ca); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to create coupon association").
				WithReportableDetails(map[string]interface{}{
					"coupon_id": req.CouponID,
				}).
				Mark(ierr.ErrInternal)
		}

		if err := s.CouponRepo.IncrementRedemptions(txCtx, req.CouponID); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to increment coupon redemptions").
				Mark(ierr.ErrInternal)
		}

		s.Logger.Infow("created coupon association",
			"association_id", ca.ID,
			"coupon_id", req.CouponID,
			"subscription_id", req.SubscriptionID,
			"subscription_line_item_id", req.SubscriptionLineItemID,
			"created_by", types.GetUserID(txCtx))

		response = s.toCouponAssociationResponse(ca)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return response, nil
}

// GetCouponAssociation retrieves a coupon association by ID
func (s *couponService) GetCouponAssociation(ctx context.Context, id string) (*dto.CouponAssociationResponse, error) {
	ca, err := s.CouponAssociationRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	return s.toCouponAssociationResponse(ca), nil
}

// DeleteCouponAssociation deletes a coupon association
func (s *couponService) DeleteCouponAssociation(ctx context.Context, id string) error {
	return s.CouponAssociationRepo.Delete(ctx, id)
}

// GetCouponAssociationsBySubscription retrieves coupon associations for a subscription
func (s *couponService) GetCouponAssociationsBySubscription(ctx context.Context, subscriptionID string) ([]*dto.CouponAssociationResponse, error) {
	associations, err := s.CouponAssociationRepo.GetBySubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	responses := make([]*dto.CouponAssociationResponse, len(associations))
	for i, ca := range associations {
		responses[i] = s.toCouponAssociationResponse(ca)
	}

	return responses, nil
}

// CreateCouponApplication creates a new coupon application
func (s *couponService) CreateCouponApplication(ctx context.Context, req dto.CreateCouponApplicationRequest) (*dto.CouponApplicationResponse, error) {
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
func (s *couponService) GetCouponApplication(ctx context.Context, id string) (*dto.CouponApplicationResponse, error) {
	ca, err := s.CouponApplicationRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	return s.toCouponApplicationResponse(ca), nil
}

// GetCouponApplicationsByInvoice retrieves coupon applications for an invoice
func (s *couponService) GetCouponApplicationsByInvoice(ctx context.Context, invoiceID string) ([]*dto.CouponApplicationResponse, error) {
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
func (s *couponService) GetCouponApplicationsBySubscription(ctx context.Context, subscriptionID string) ([]*dto.CouponApplicationResponse, error) {
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
func (s *couponService) ApplyCouponToInvoice(ctx context.Context, couponID string, invoiceID string, originalPrice decimal.Decimal) (*dto.CouponApplicationResponse, error) {
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

// ValidateCouponForSubscription validates if a coupon can be applied to a subscription
func (s *couponService) ValidateCouponForSubscription(ctx context.Context, couponID string, subscriptionID string) error {
	validationService := NewCouponValidationService(s.ServiceParams)
	return validationService.ValidateCouponForSubscription(ctx, couponID, subscriptionID)
}

// CalculateDiscount calculates the discount amount for a given coupon and price
func (s *couponService) CalculateDiscount(ctx context.Context, couponID string, originalPrice decimal.Decimal) (decimal.Decimal, error) {
	// Validate input parameters
	if couponID == "" {
		return decimal.Zero, ierr.NewError("coupon_id is required").
			WithHint("Please provide a valid coupon ID").
			Mark(ierr.ErrValidation)
	}

	if originalPrice.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero, ierr.NewError("original_price must be greater than zero").
			WithHint("Please provide a valid original price").
			WithReportableDetails(map[string]interface{}{
				"original_price": originalPrice,
			}).
			Mark(ierr.ErrValidation)
	}

	s.Logger.Debugw("calculating discount for coupon",
		"coupon_id", couponID,
		"original_price", originalPrice)

	// Get the coupon
	c, err := s.CouponRepo.Get(ctx, couponID)
	if err != nil {
		return decimal.Zero, ierr.WithError(err).
			WithHint("Failed to get coupon for discount calculation").
			WithReportableDetails(map[string]interface{}{
				"coupon_id": couponID,
			}).
			Mark(ierr.ErrNotFound)
	}

	// Validate coupon is active
	if c.Status != types.StatusPublished {
		return decimal.Zero, ierr.NewError("only active coupons can be used for discount calculation").
			WithHint("Please select an active coupon").
			WithReportableDetails(map[string]interface{}{
				"coupon_id": couponID,
				"status":    c.Status,
			}).
			Mark(ierr.ErrValidation)
	}

	// Validate coupon is valid for redemption
	if !c.IsValid() {
		return decimal.Zero, ierr.NewError("coupon is not valid for redemption").
			WithHint("Coupon may be expired, have reached maximum redemptions, or not yet available for redemption").
			WithReportableDetails(map[string]interface{}{
				"coupon_id":         couponID,
				"redeem_after":      c.RedeemAfter,
				"redeem_before":     c.RedeemBefore,
				"total_redemptions": c.TotalRedemptions,
				"max_redemptions":   c.MaxRedemptions,
			}).
			Mark(ierr.ErrValidation)
	}

	discount := c.CalculateDiscount(originalPrice)

	s.Logger.Debugw("calculated discount for coupon",
		"coupon_id", couponID,
		"original_price", originalPrice,
		"discount", discount,
		"coupon_type", c.Type)

	return discount, nil
}

func (s *couponService) ApplyCouponToSubscription(ctx context.Context, couponIDs []string, subscriptionID string) (*dto.CouponAssociationResponse, error) {
	// Validate input parameters
	if len(couponIDs) == 0 {
		return nil, ierr.NewError("at least one coupon_id is required").
			WithHint("Please provide at least one coupon ID to apply").
			Mark(ierr.ErrValidation)
	}

	if subscriptionID == "" {
		return nil, ierr.NewError("subscription_id is required").
			WithHint("Please provide a valid subscription ID").
			Mark(ierr.ErrValidation)
	}

	s.Logger.Infow("applying coupons to subscription",
		"subscription_id", subscriptionID,
		"coupon_count", len(couponIDs),
		"coupon_ids", couponIDs)

	// Validate that subscription exists (basic check)
	_, err := s.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get subscription for coupon application").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": subscriptionID,
			}).
			Mark(ierr.ErrNotFound)
	}

	// Validate each coupon
	for _, couponID := range couponIDs {
		if err := s.ValidateCouponForSubscription(ctx, couponID, subscriptionID); err != nil {
			return nil, ierr.WithError(err).
				WithHint("Coupon validation failed").
				WithReportableDetails(map[string]interface{}{
					"coupon_id":       couponID,
					"subscription_id": subscriptionID,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	// Apply all coupons to the subscription
	var lastAssociation *dto.CouponAssociationResponse
	appliedCoupons := make([]string, 0, len(couponIDs))
	failedCoupons := make([]string, 0)

	for _, couponID := range couponIDs {
		// Create coupon association request
		req := dto.CreateCouponAssociationRequest{
			CouponID:       couponID,
			SubscriptionID: subscriptionID,
			Metadata:       map[string]string{},
		}

		// Create the coupon association
		association, err := s.CreateCouponAssociation(ctx, req)
		if err != nil {
			s.Logger.Errorw("failed to create coupon association",
				"subscription_id", subscriptionID,
				"coupon_id", couponID,
				"error", err)
			failedCoupons = append(failedCoupons, couponID)
			continue // Continue with other coupons even if one fails
		}

		appliedCoupons = append(appliedCoupons, couponID)
		lastAssociation = association

		s.Logger.Infow("successfully applied coupon to subscription",
			"subscription_id", subscriptionID,
			"coupon_id", couponID,
			"association_id", association.ID)
	}

	// If no coupons were applied successfully, return an error
	if len(appliedCoupons) == 0 {
		return nil, ierr.NewError("failed to apply any coupons to subscription").
			WithHint("All requested coupons failed to be applied").
			WithReportableDetails(map[string]interface{}{
				"subscription_id":   subscriptionID,
				"requested_coupons": couponIDs,
				"failed_coupons":    failedCoupons,
			}).
			Mark(ierr.ErrValidation)
	}

	// If some coupons failed, log a warning but return the last successful association
	if len(failedCoupons) > 0 {
		s.Logger.Warnw("some coupons failed to be applied",
			"subscription_id", subscriptionID,
			"applied_coupons", appliedCoupons,
			"failed_coupons", failedCoupons,
			"total_requested", len(couponIDs),
			"total_applied", len(appliedCoupons))
	}

	s.Logger.Infow("completed coupon application to subscription",
		"subscription_id", subscriptionID,
		"total_requested", len(couponIDs),
		"total_applied", len(appliedCoupons),
		"total_failed", len(failedCoupons))

	return lastAssociation, nil
}

// PrepareCouponsForInvoice prepares coupons for an invoice with optimized batch fetching
func (s *couponService) PrepareCouponsForInvoice(ctx context.Context, req dto.CreateInvoiceRequest) ([]*CouponWithAssociation, error) {
	// Handle subscription invoices with existing associations
	if req.SubscriptionID != nil {
		s.Logger.Infow("preparing coupons for subscription invoice",
			"subscription_id", *req.SubscriptionID,
			"customer_id", req.CustomerID)

		// Get coupon associations for the subscription
		associations, err := s.GetCouponAssociationsBySubscription(ctx, *req.SubscriptionID)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to get coupon associations for subscription").
				WithReportableDetails(map[string]interface{}{
					"subscription_id": *req.SubscriptionID,
				}).
				Mark(ierr.ErrInternal)
		}

		if len(associations) == 0 {
			s.Logger.Debugw("no coupon associations found for subscription",
				"subscription_id", *req.SubscriptionID)
			return make([]*CouponWithAssociation, 0), nil
		}

		// Batch fetch all coupon details to minimize database calls
		couponsWithAssociations, err := s.batchFetchCouponsWithAssociations(ctx, associations)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to batch fetch coupons").
				Mark(ierr.ErrInternal)
		}

		s.Logger.Infow("prepared coupons for subscription invoice",
			"subscription_id", *req.SubscriptionID,
			"total_associations", len(associations),
			"valid_coupons", len(couponsWithAssociations))

		return couponsWithAssociations, nil
	}

	// Handle standalone invoices with direct coupon IDs
	if len(req.Coupons) > 0 {
		s.Logger.Infow("preparing coupons for standalone invoice",
			"customer_id", req.CustomerID,
			"coupon_ids", req.Coupons)

		return s.prepareCouponsForStandaloneInvoice(ctx, req.Coupons)
	}

	// No coupons to process
	return make([]*CouponWithAssociation, 0), nil
}

// prepareCouponsForStandaloneInvoice prepares coupons for standalone invoices (without subscription associations)
func (s *couponService) prepareCouponsForStandaloneInvoice(ctx context.Context, couponIDs []string) ([]*CouponWithAssociation, error) {
	if len(couponIDs) == 0 {
		return make([]*CouponWithAssociation, 0), nil
	}

	s.Logger.Debugw("preparing standalone coupons for invoice",
		"coupon_count", len(couponIDs),
		"coupon_ids", couponIDs)

	// Batch fetch coupons using optimized repository method
	domainCoupons, err := s.CouponRepo.GetBatch(ctx, couponIDs)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to batch fetch coupons for standalone invoice").
			WithReportableDetails(map[string]interface{}{
				"coupon_ids": couponIDs,
			}).
			Mark(ierr.ErrInternal)
	}

	// Filter out invalid coupon IDs and validate coupons
	couponsWithAssociations := make([]*CouponWithAssociation, 0, len(couponIDs))
	fetchedCoupons := make(map[string]*coupon.Coupon, len(domainCoupons))

	// Create lookup map for fetched coupons
	for _, c := range domainCoupons {
		fetchedCoupons[c.ID] = c
	}

	// Process each requested coupon ID
	for _, couponID := range couponIDs {
		domainCoupon, exists := fetchedCoupons[couponID]
		if !exists {
			s.Logger.Warnw("coupon not found for standalone invoice",
				"coupon_id", couponID)
			continue // Skip non-existent coupons
		}

		// Validate coupon is active and redeemable
		if domainCoupon.Status != types.StatusPublished {
			s.Logger.Warnw("coupon is not published, skipping",
				"coupon_id", couponID,
				"status", domainCoupon.Status)
			continue
		}

		if !domainCoupon.IsValid() {
			s.Logger.Warnw("coupon is not valid for redemption, skipping",
				"coupon_id", couponID,
				"redeem_after", domainCoupon.RedeemAfter,
				"redeem_before", domainCoupon.RedeemBefore,
				"total_redemptions", domainCoupon.TotalRedemptions,
				"max_redemptions", domainCoupon.MaxRedemptions)
			continue
		}

		// Create CouponWithAssociation with null association for standalone coupons
		couponsWithAssociations = append(couponsWithAssociations, &CouponWithAssociation{
			Coupon:      s.toCouponResponse(domainCoupon),
			Association: nil, // No association for standalone coupons
		})
	}

	s.Logger.Infow("prepared standalone coupons for invoice",
		"requested_count", len(couponIDs),
		"valid_coupons", len(couponsWithAssociations))

	return couponsWithAssociations, nil
}

// batchFetchCouponsWithAssociations fetches all coupons for associations in batch with validation
func (s *couponService) batchFetchCouponsWithAssociations(ctx context.Context, associations []*dto.CouponAssociationResponse) ([]*CouponWithAssociation, error) {
	if len(associations) == 0 {
		return make([]*CouponWithAssociation, 0), nil
	}

	s.Logger.Debugw("batch fetching coupons for associations",
		"association_count", len(associations))

	// Extract coupon IDs for batch fetching
	couponIDs := make([]string, len(associations))
	associationMap := make(map[string]*dto.CouponAssociationResponse)

	for i, association := range associations {
		couponIDs[i] = association.CouponID
		associationMap[association.CouponID] = association
	}

	// Batch fetch coupons using optimized repository method
	domainCoupons, err := s.CouponRepo.GetBatch(ctx, couponIDs)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to batch fetch coupons from repository").
			Mark(ierr.ErrInternal)
	}

	// Convert domain coupons to DTO and create lookup map
	coupons := make(map[string]*dto.CouponResponse, len(domainCoupons))
	for _, domainCoupon := range domainCoupons {
		coupons[domainCoupon.ID] = s.toCouponResponse(domainCoupon)
	}

	// Filter and combine valid coupons with their associations
	couponsWithAssociations := make([]*CouponWithAssociation, 0, len(associations))

	for _, association := range associations {
		coupon, exists := coupons[association.CouponID]
		if !exists {
			continue // Coupon fetch failed, skip
		}

		// Note: Comprehensive validation will be performed later by the validation service
		// No pre-filtering needed here to avoid duplicating validation logic

		couponsWithAssociations = append(couponsWithAssociations, &CouponWithAssociation{
			Coupon:      coupon,
			Association: association,
		})
	}

	s.Logger.Debugw("completed batch coupon fetch and validation",
		"total_associations", len(associations),
		"fetched_coupons", len(coupons),
		"valid_coupons", len(couponsWithAssociations))

	return couponsWithAssociations, nil
}

// ApplyCouponsOnInvoice applies coupons to an invoice with optimized batch processing
func (s *couponService) ApplyCouponsOnInvoice(ctx context.Context, inv *invoice.Invoice, couponsWithAssociations []*CouponWithAssociation) (*CouponCalculationResult, error) {
	if len(couponsWithAssociations) == 0 {
		return &CouponCalculationResult{
			TotalDiscountAmount: decimal.Zero,
			AppliedCoupons:      make([]*dto.CouponApplicationResponse, 0),
			Currency:            inv.Currency,
			Metadata:            make(map[string]interface{}),
		}, nil
	}

	s.Logger.Infow("applying coupons to invoice",
		"invoice_id", inv.ID,
		"coupon_count", len(couponsWithAssociations),
		"original_total", inv.Total)

	var result *CouponCalculationResult

	// Use transaction for atomic operations
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		// Validate all coupons using the same validation logic as subscription
		validationResults, err := s.validateCouponsForInvoice(txCtx, inv, couponsWithAssociations)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to batch validate coupons").
				Mark(ierr.ErrInternal)
		}

		// Filter out invalid coupons
		validCouponsWithAssociations := make([]*CouponWithAssociation, 0, len(couponsWithAssociations))
		for i, couponWithAssociation := range couponsWithAssociations {
			if validationResults[i] == nil {
				validCouponsWithAssociations = append(validCouponsWithAssociations, couponWithAssociation)
			} else {
				associationID := ""
				if couponWithAssociation.Association != nil {
					associationID = couponWithAssociation.Association.ID
				}
				s.Logger.Warnw("coupon validation failed, skipping coupon",
					"coupon_id", couponWithAssociation.Coupon.ID,
					"association_id", associationID,
					"invoice_id", inv.ID,
					"error", validationResults[i].Error())
			}
		}

		if len(validCouponsWithAssociations) == 0 {
			result = &CouponCalculationResult{
				TotalDiscountAmount: decimal.Zero,
				AppliedCoupons:      make([]*dto.CouponApplicationResponse, 0),
				Currency:            inv.Currency,
				Metadata: map[string]interface{}{
					"total_coupons_processed": len(couponsWithAssociations),
					"successful_applications": 0,
					"validation_failures":     len(couponsWithAssociations),
				},
			}
			return nil
		}

		// Process valid coupons and create applications in batch
		totalDiscount := decimal.Zero
		runningTotal := inv.Total
		applicationRequests := make([]dto.CreateCouponApplicationRequest, 0, len(validCouponsWithAssociations))

		// Calculate discounts for all valid coupons
		for _, couponWithAssociation := range validCouponsWithAssociations {
			coupon := couponWithAssociation.Coupon
			association := couponWithAssociation.Association

			// Calculate discount for this coupon based on the running total
			discount := coupon.CalculateDiscount(runningTotal)
			finalPrice := coupon.ApplyDiscount(runningTotal)

			// Validate that the final price is not negative
			if finalPrice.LessThan(decimal.Zero) {
				associationID := ""
				if association != nil {
					associationID = association.ID
				}
				s.Logger.Warnw("discount amount exceeds running total, skipping coupon",
					"coupon_id", coupon.ID,
					"association_id", associationID,
					"invoice_id", inv.ID,
					"running_total", runningTotal,
					"discount", discount,
					"final_price", finalPrice)
				continue
			}

			// Create application request
			req := dto.CreateCouponApplicationRequest{
				CouponID:         coupon.ID,
				InvoiceID:        inv.ID,
				OriginalPrice:    runningTotal,
				FinalPrice:       finalPrice,
				DiscountedAmount: discount,
				DiscountType:     coupon.Type,
				Currency:         inv.Currency,
				CouponSnapshot: map[string]interface{}{
					"name":           coupon.Name,
					"type":           coupon.Type,
					"cadence":        coupon.Cadence,
					"amount_off":     coupon.AmountOff,
					"percentage_off": coupon.PercentageOff,
				},
			}

			// Set association ID only if association exists
			if association != nil {
				req.CouponAssociationID = association.ID
			}

			if coupon.Type == types.CouponTypePercentage {
				req.DiscountPercentage = coupon.PercentageOff
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
				"total_coupons_processed": len(couponsWithAssociations),
				"successful_applications": len(appliedCoupons),
				"validation_failures":     len(couponsWithAssociations) - len(validCouponsWithAssociations),
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

// validateCouponsForInvoice validates all coupons for an invoice using the same validation logic as subscription
func (s *couponService) validateCouponsForInvoice(ctx context.Context, inv *invoice.Invoice, couponsWithAssociations []*CouponWithAssociation) ([]error, error) {
	if len(couponsWithAssociations) == 0 {
		return []error{}, nil
	}

	s.Logger.Debugw("validating coupons for invoice",
		"invoice_id", inv.ID,
		"coupon_count", len(couponsWithAssociations))

	// Use the same validation service that's used for subscription validation
	validationService := NewCouponValidationService(s.ServiceParams)

	validationResults := make([]error, len(couponsWithAssociations))

	// Validate coupons based on invoice type
	if inv.SubscriptionID != nil {
		// For subscription invoices: use full subscription-based validation
		for i, cwa := range couponsWithAssociations {
			// Use the exact same validation method as subscription
			err := validationService.ValidateCouponForInvoice(ctx, cwa.Coupon.ID, inv.ID, *inv.SubscriptionID)
			if err != nil {
				validationResults[i] = err
			}
		}
	} else {
		// For standalone invoices: use basic coupon validation only
		for i, cwa := range couponsWithAssociations {
			err := s.validateCouponForStandaloneInvoice(ctx, cwa.Coupon.ID, inv.ID)
			if err != nil {
				validationResults[i] = err
			}
		}
	}

	return validationResults, nil
}

// validateCouponForStandaloneInvoice performs basic validation for coupons applied to standalone invoices
func (s *couponService) validateCouponForStandaloneInvoice(ctx context.Context, couponID string, invoiceID string) error {
	s.Logger.Debugw("validating coupon for standalone invoice",
		"coupon_id", couponID,
		"invoice_id", invoiceID)

	// Get coupon details
	coupon, err := s.CouponRepo.Get(ctx, couponID)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to get coupon details").
			WithReportableDetails(map[string]interface{}{
				"coupon_id":  couponID,
				"invoice_id": invoiceID,
			}).
			Mark(ierr.ErrNotFound)
	}

	// Use the validation service for basic coupon validation
	validationService := NewCouponValidationService(s.ServiceParams)

	// Perform basic coupon validation (status, validity, etc.)
	if err := validationService.ValidateCouponBasic(coupon); err != nil {
		return err
	}

	// For standalone invoices, we only enforce "once" cadence validation
	// since there's no subscription context for "repeated" or "forever" cadence
	if coupon.Cadence == types.CouponCadenceRepeated || coupon.Cadence == types.CouponCadenceForever {
		s.Logger.Warnw("coupon with repeated/forever cadence not recommended for standalone invoices",
			"coupon_id", couponID,
			"invoice_id", invoiceID,
			"cadence", coupon.Cadence)
		// Allow but warn - these cadences are designed for subscriptions
	}

	s.Logger.Debugw("coupon validation successful for standalone invoice",
		"coupon_id", couponID,
		"invoice_id", invoiceID)

	return nil
}

// batchCreateCouponApplications creates multiple coupon applications in a batch operation
func (s *couponService) batchCreateCouponApplications(ctx context.Context, requests []dto.CreateCouponApplicationRequest) ([]*dto.CouponApplicationResponse, error) {
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

// Helper methods to convert domain models to DTOs
func (s *couponService) toCouponResponse(c *coupon.Coupon) *dto.CouponResponse {
	return &dto.CouponResponse{
		Coupon: c,
	}
}

func (s *couponService) toCouponAssociationResponse(ca *coupon_association.CouponAssociation) *dto.CouponAssociationResponse {
	return &dto.CouponAssociationResponse{
		CouponAssociation: ca,
	}
}

func (s *couponService) toCouponApplicationResponse(ca *coupon_application.CouponApplication) *dto.CouponApplicationResponse {
	return &dto.CouponApplicationResponse{
		CouponApplication: ca,
	}
}
