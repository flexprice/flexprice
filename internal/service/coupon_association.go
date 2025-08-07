package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/coupon_association"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

type CouponAssociationService interface {
	CreateCouponAssociation(ctx context.Context, req dto.CreateCouponAssociationRequest) (*dto.CouponAssociationResponse, error)
	GetCouponAssociation(ctx context.Context, id string) (*dto.CouponAssociationResponse, error)
	DeleteCouponAssociation(ctx context.Context, id string) error
	GetCouponAssociationsBySubscription(ctx context.Context, subscriptionID string) ([]*dto.CouponAssociationResponse, error)
	GetCouponAssociationsBySubscriptionLineItem(ctx context.Context, subscriptionLineItemID string) ([]*dto.CouponAssociationResponse, error)
	ApplyCouponToSubscription(ctx context.Context, couponIDs []string, subscriptionID string) error

	// Line item coupon association methods
	ApplyCouponToSubscriptionLineItem(ctx context.Context, couponIDs []string, subscriptionID string, priceID string) error
}

type couponAssociationService struct {
	ServiceParams
}

func NewCouponAssociationService(
	params ServiceParams,
) CouponAssociationService {
	return &couponAssociationService{
		ServiceParams: params,
	}
}

func (s *couponAssociationService) CreateCouponAssociation(ctx context.Context, req dto.CreateCouponAssociationRequest) (*dto.CouponAssociationResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	var response *dto.CouponAssociationResponse

	// Use transaction for atomic operations
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {

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
func (s *couponAssociationService) GetCouponAssociation(ctx context.Context, id string) (*dto.CouponAssociationResponse, error) {
	ca, err := s.CouponAssociationRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	return s.toCouponAssociationResponse(ca), nil
}

// DeleteCouponAssociation deletes a coupon association
func (s *couponAssociationService) DeleteCouponAssociation(ctx context.Context, id string) error {
	return s.CouponAssociationRepo.Delete(ctx, id)
}

// GetCouponAssociationsBySubscription retrieves coupon associations for a subscription
func (s *couponAssociationService) GetCouponAssociationsBySubscription(ctx context.Context, subscriptionID string) ([]*dto.CouponAssociationResponse, error) {
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

// GetCouponAssociationsBySubscriptionLineItem retrieves coupon associations for a subscription line item
func (s *couponAssociationService) GetCouponAssociationsBySubscriptionLineItem(ctx context.Context, subscriptionLineItemID string) ([]*dto.CouponAssociationResponse, error) {
	associations, err := s.CouponAssociationRepo.GetBySubscriptionLineItem(ctx, subscriptionLineItemID)
	if err != nil {
		return nil, err
	}

	responses := make([]*dto.CouponAssociationResponse, len(associations))
	for i, ca := range associations {
		responses[i] = s.toCouponAssociationResponse(ca)
	}

	return responses, nil
}

func (s *couponAssociationService) ApplyCouponToSubscription(ctx context.Context, couponIDs []string, subscriptionID string) error {
	// Validate input parameters
	if len(couponIDs) == 0 {
		return ierr.NewError("at least one coupon_id is required").
			WithHint("Please provide at least one coupon ID to apply").
			Mark(ierr.ErrValidation)
	}

	if subscriptionID == "" {
		return ierr.NewError("subscription_id is required").
			WithHint("Please provide a valid subscription ID").
			Mark(ierr.ErrValidation)
	}

	validationService := NewCouponValidationService(s.ServiceParams)

	// Validate each coupon
	for _, couponID := range couponIDs {
		if err := validationService.ValidateCoupon(ctx, couponID, &subscriptionID); err != nil {
			return ierr.WithError(err).
				WithHint("Coupon validation failed").
				WithReportableDetails(map[string]interface{}{
					"coupon_id":       couponID,
					"subscription_id": subscriptionID,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	for _, couponID := range couponIDs {
		// Create coupon association request
		req := dto.CreateCouponAssociationRequest{
			CouponID:       couponID,
			SubscriptionID: subscriptionID,
			Metadata:       map[string]string{},
		}

		// Create the coupon association
		_, err := s.CreateCouponAssociation(ctx, req)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *couponAssociationService) ApplyCouponToSubscriptionLineItem(ctx context.Context, couponIDs []string, subscriptionID string, lineItemID string) error {
	if len(couponIDs) == 0 {
		return ierr.NewError("at least one coupon_id is required").
			WithHint("Please provide at least one coupon ID to apply").
			Mark(ierr.ErrValidation)
	}

	if subscriptionID == "" {
		return ierr.NewError("subscription_id is required").
			WithHint("Please provide a valid subscription ID").
			Mark(ierr.ErrValidation)
	}

	if lineItemID == "" {
		return ierr.NewError("price_id is required").
			WithHint("Please provide a valid price ID").
			Mark(ierr.ErrValidation)
	}

	for _, couponID := range couponIDs {
		req := dto.CreateCouponAssociationRequest{
			CouponID:               couponID,
			SubscriptionID:         subscriptionID,
			SubscriptionLineItemID: &lineItemID,
			Metadata:               map[string]string{},
		}

		_, err := s.CreateCouponAssociation(ctx, req)
		if err != nil {
			return err
		}
	}

	return nil
}

// Helper method to convert domain models to DTOs
func (s *couponAssociationService) toCouponAssociationResponse(ca *coupon_association.CouponAssociation) *dto.CouponAssociationResponse {
	return &dto.CouponAssociationResponse{
		CouponAssociation: ca,
	}
}
