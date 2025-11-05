package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/coupon_association"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

type CouponAssociationService interface {
	CreateCouponAssociation(ctx context.Context, req dto.CreateCouponAssociationRequest) (*dto.CouponAssociationResponse, error)
	GetCouponAssociation(ctx context.Context, id string) (*dto.CouponAssociationResponse, error)
	DeleteCouponAssociation(ctx context.Context, id string) error
	ListCouponAssociations(ctx context.Context, filter *types.CouponAssociationFilter) (*dto.ListCouponAssociationsResponse, error)
	ApplyCouponsToSubscription(ctx context.Context, subscriptionID string, coupons []dto.SubscriptionCouponRequest) error
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
		startDate := time.Now().UTC()
		if req.StartDate != nil {
			startDate = req.StartDate.UTC()
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

// ListCouponAssociations retrieves coupon associations with filtering and pagination
func (s *couponAssociationService) ListCouponAssociations(ctx context.Context, filter *types.CouponAssociationFilter) (*dto.ListCouponAssociationsResponse, error) {
	if filter == nil {
		filter = types.NewCouponAssociationFilter()
	}

	if err := filter.Validate(); err != nil {
		return nil, err
	}

	associations, err := s.CouponAssociationRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	count, err := s.CouponAssociationRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	items := make([]*dto.CouponAssociationResponse, len(associations))
	for i, ca := range associations {
		items[i] = s.toCouponAssociationResponse(ca)
	}

	return &dto.ListCouponAssociationsResponse{
		Items: items,
		Pagination: types.NewPaginationResponse(
			count,
			filter.GetLimit(),
			filter.GetOffset(),
		),
	}, nil
}

// ApplyCouponsToSubscription applies coupons to a subscription
// Handles both subscription-level and line item-level coupons based on PriceID field
func (s *couponAssociationService) ApplyCouponsToSubscription(ctx context.Context, subscriptionID string, coupons []dto.SubscriptionCouponRequest) error {
	if subscriptionID == "" {
		return ierr.NewError("subscription_id is required").
			WithHint("Please provide a valid subscription ID").
			Mark(ierr.ErrValidation)
	}

	if len(coupons) == 0 {
		return nil
	}

	validationService := NewCouponValidationService(s.ServiceParams)

	// Validate each coupon request
	for i, couponReq := range coupons {
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

		// Determine subscription line item ID based on PriceID
		var subscriptionLineItemID *string
		if couponReq.PriceID != nil {
			// Find line item by price_id
			lineItem, err := s.findLineItemByPriceID(ctx, subscriptionID, *couponReq.PriceID)
			if err != nil {
				return ierr.WithError(err).
					WithHint("Failed to find line item for price ID").
					WithReportableDetails(map[string]interface{}{
						"price_id":        *couponReq.PriceID,
						"subscription_id": subscriptionID,
					}).
					Mark(ierr.ErrValidation)
			}
			subscriptionLineItemID = &lineItem.ID
		}

		// Create coupon association request
		createReq := dto.CreateCouponAssociationRequest{
			CouponID:               couponReq.CouponID,
			SubscriptionID:         subscriptionID,
			SubscriptionLineItemID: subscriptionLineItemID,
			StartDate:              couponReq.StartDate,
			EndDate:                couponReq.EndDate,
			SubscriptionPhaseID:    couponReq.SubscriptionPhaseID,
			Metadata:               map[string]string{},
		}

		// Create the coupon association
		_, err := s.CreateCouponAssociation(ctx, createReq)
		if err != nil {
			return err
		}
	}

	return nil
}

// findLineItemByPriceID finds a subscription line item by price ID
func (s *couponAssociationService) findLineItemByPriceID(ctx context.Context, subscriptionID, priceID string) (*subscription.SubscriptionLineItem, error) {
	filter := types.NewSubscriptionLineItemFilter()
	filter.SubscriptionIDs = []string{subscriptionID}
	filter.PriceIDs = []string{priceID}

	lineItems, err := s.SubscriptionLineItemRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	if len(lineItems) == 0 {
		return nil, ierr.NewError("line item not found").
			WithHint("No line item found with the specified price ID").
			Mark(ierr.ErrNotFound)
	}

	return lineItems[0], nil
}

// Helper method to convert domain models to DTOs
func (s *couponAssociationService) toCouponAssociationResponse(ca *coupon_association.CouponAssociation) *dto.CouponAssociationResponse {
	return &dto.CouponAssociationResponse{
		CouponAssociation: ca,
	}
}
