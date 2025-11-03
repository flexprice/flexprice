package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/coupon_association"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

type CouponAssociationService interface {
	CreateCouponAssociation(ctx context.Context, req dto.CreateCouponAssociationRequest) (*dto.CouponAssociationResponse, error)
	GetCouponAssociation(ctx context.Context, id string) (*dto.CouponAssociationResponse, error)
	DeleteCouponAssociation(ctx context.Context, id string) error
	GetCouponAssociationsBySubscriptionFilter(ctx context.Context, filter *coupon_association.Filter) ([]*dto.CouponAssociationResponse, error)
	ApplyCouponToSubscription(ctx context.Context, couponRequests []dto.SubscriptionCouponRequest, subscriptionID string) error

	// Line item coupon association methods
	ApplyCouponToSubscriptionLineItem(ctx context.Context, couponRequests []dto.SubscriptionCouponRequest, subscriptionID string, lineItemID string) error
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
		startDate := time.Now()
		if req.StartDate != nil {
			startDate = *req.StartDate
		}

		ca := &coupon_association.CouponAssociation{
			ID:                     types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON_ASSOCIATION),
			CouponID:               req.CouponID,
			SubscriptionID:         req.SubscriptionID,
			SubscriptionLineItemID: req.SubscriptionLineItemID,
			SubscriptionPhaseID:    req.SubscriptionPhaseID,
			StartDate:              startDate,
			EndDate:                req.EndDate,
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

// GetCouponAssociationsBySubscriptionFilter retrieves coupon associations using the domain Filter
func (s *couponAssociationService) GetCouponAssociationsBySubscriptionFilter(ctx context.Context, filter *coupon_association.Filter) ([]*dto.CouponAssociationResponse, error) {
	associations, err := s.CouponAssociationRepo.GetBySubscriptionFilter(ctx, filter)
	if err != nil {
		return nil, err
	}

	responses := make([]*dto.CouponAssociationResponse, len(associations))
	for i, ca := range associations {
		responses[i] = s.toCouponAssociationResponse(ca)
	}

	return responses, nil
}

func (s *couponAssociationService) ApplyCouponToSubscription(ctx context.Context, couponRequests []dto.SubscriptionCouponRequest, subscriptionID string) error {
	// Validate input parameters
	if len(couponRequests) == 0 {
		return nil
	}

	if subscriptionID == "" {
		return ierr.NewError("subscription_id is required").
			WithHint("Please provide a valid subscription ID").
			Mark(ierr.ErrValidation)
	}

	validationService := NewCouponValidationService(s.ServiceParams)

	// Validate each coupon request
	for i, couponReq := range couponRequests {
		if err := couponReq.Validate(); err != nil {
			return ierr.WithError(err).
				WithHint("Coupon request validation failed").
				WithReportableDetails(map[string]interface{}{
					"index": i,
				}).
				Mark(ierr.ErrValidation)
		}

		// Validate coupon applicability
		if err := validationService.ValidateCoupon(ctx, couponReq.CouponID, &subscriptionID); err != nil {
			return ierr.WithError(err).
				WithHint("Coupon validation failed").
				WithReportableDetails(map[string]interface{}{
					"coupon_id":       couponReq.CouponID,
					"subscription_id": subscriptionID,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	// Apply each coupon with its dates
	for _, couponReq := range couponRequests {
		req := dto.CreateCouponAssociationRequest{
			CouponID:       couponReq.CouponID,
			SubscriptionID: subscriptionID,
			StartDate:      couponReq.StartDate,
			EndDate:        couponReq.EndDate,
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

func (s *couponAssociationService) ApplyCouponToSubscriptionLineItem(ctx context.Context, couponRequests []dto.SubscriptionCouponRequest, subscriptionID string, lineItemID string) error {
	// Validate input parameters
	if len(couponRequests) == 0 {
		return nil
	}

	if subscriptionID == "" {
		return ierr.NewError("subscription_id is required").
			WithHint("Please provide a valid subscription ID").
			Mark(ierr.ErrValidation)
	}

	if lineItemID == "" {
		return ierr.NewError("subscription_line_item_id is required").
			WithHint("Please provide a valid subscription line item ID").
			Mark(ierr.ErrValidation)
	}

	validationService := NewCouponValidationService(s.ServiceParams)

	// Validate each coupon request
	for i, couponReq := range couponRequests {
		if err := couponReq.Validate(); err != nil {
			return ierr.WithError(err).
				WithHint("Coupon request validation failed").
				WithReportableDetails(map[string]interface{}{
					"index": i,
				}).
				Mark(ierr.ErrValidation)
		}

		// Validate coupon applicability
		if err := validationService.ValidateCoupon(ctx, couponReq.CouponID, &subscriptionID); err != nil {
			return ierr.WithError(err).
				WithHint("Coupon validation failed").
				WithReportableDetails(map[string]interface{}{
					"coupon_id":       couponReq.CouponID,
					"subscription_id": subscriptionID,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	// Apply each coupon with its dates
	for _, couponReq := range couponRequests {
		req := dto.CreateCouponAssociationRequest{
			CouponID:               couponReq.CouponID,
			SubscriptionID:         subscriptionID,
			SubscriptionLineItemID: &lineItemID,
			StartDate:              couponReq.StartDate,
			EndDate:                couponReq.EndDate,
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
