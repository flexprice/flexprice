package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/coupon"
	"github.com/flexprice/flexprice/internal/domain/coupon_association"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type CouponAssociationService interface {
	CreateCouponAssociation(ctx context.Context, req dto.CreateCouponAssociationRequest) (*dto.CouponAssociationResponse, error)
	GetCouponAssociation(ctx context.Context, id string) (*dto.CouponAssociationResponse, error)
	DeleteCouponAssociation(ctx context.Context, id string) error
	ListCouponAssociations(ctx context.Context, filter *types.CouponAssociationFilter) (*dto.ListCouponAssociationsResponse, error)
	ApplyCouponsToSubscription(ctx context.Context, subscription *subscription.Subscription, coupons []dto.SubscriptionCouponRequest) error
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

		ca := &coupon_association.CouponAssociation{
			ID:                     types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON_ASSOCIATION),
			CouponID:               req.CouponID,
			SubscriptionID:         req.SubscriptionID,
			SubscriptionLineItemID: req.SubscriptionLineItemID,
			SubscriptionPhaseID:    req.SubscriptionPhaseID,
			StartDate:              req.StartDate.UTC(),
			EndDate:                req.EndDate,
			Metadata:               req.Metadata,
			BaseModel:              types.GetDefaultBaseModel(txCtx),
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
	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewDefaultQueryFilter()
	}

	expand := filter.GetExpand()
	if !expand.IsEmpty() {
		if err := expand.Validate(types.CouponAssociationExpandConfig); err != nil {
			return nil, err
		}
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

	var couponsByID map[string]*coupon.Coupon
	if expand.Has(types.ExpandCoupon) {
		couponIDs := lo.Uniq(lo.Map(associations, func(ca *coupon_association.CouponAssociation, _ int) string {
			return ca.CouponID
		}))
		if len(couponIDs) > 0 {
			couponFilter := types.NewNoLimitCouponFilter()
			couponFilter.CouponIDs = couponIDs
			couponFilter.Status = lo.ToPtr(types.StatusPublished)
			coupons, err := s.CouponRepo.List(ctx, couponFilter)
			if err != nil {
				return nil, err
			}
			couponsByID = make(map[string]*coupon.Coupon, len(coupons))
			for _, c := range coupons {
				couponsByID[c.ID] = c
			}
			s.Logger.Debug(ctx, "fetched coupons for coupon associations", "count", len(coupons))
		}
	}

	var lineItemsByID map[string]*dto.SubscriptionLineItemResponse
	if expand.Has(types.ExpandSubscriptionLineItems) {
		lineItemIDs := lo.Uniq(lo.FilterMap(associations, func(ca *coupon_association.CouponAssociation, _ int) (string, bool) {
			if ca.SubscriptionLineItemID != nil && *ca.SubscriptionLineItemID != "" {
				return *ca.SubscriptionLineItemID, true
			}
			return "", false
		}))
		if len(lineItemIDs) > 0 {
			subService := NewSubscriptionService(s.ServiceParams)
			liFilter := types.NewNoLimitSubscriptionLineItemFilter()
			liFilter.SubscriptionLineItemIDs = lineItemIDs
			liFilter.Status = lo.ToPtr(types.StatusPublished)
			nested := expand.GetNested(types.ExpandSubscriptionLineItems)
			if !nested.IsEmpty() {
				liFilter.Expand = lo.ToPtr(nested.String())
			}
			lineItemsResp, err := subService.ListSubscriptionLineItems(ctx, liFilter)
			if err != nil {
				return nil, err
			}
			lineItemsByID = make(map[string]*dto.SubscriptionLineItemResponse, len(lineItemsResp.Items))
			for _, li := range lineItemsResp.Items {
				if li != nil && li.SubscriptionLineItem != nil {
					lineItemsByID[li.SubscriptionLineItem.ID] = li
				}
			}
			s.Logger.Debug(ctx, "fetched subscription line items for coupon associations", "count", len(lineItemsResp.Items))
		}
	}

	for i, ca := range associations {
		item := s.toCouponAssociationResponse(ca)
		if expand.Has(types.ExpandCoupon) {
			if c, ok := couponsByID[ca.CouponID]; ok {
				item.Coupon = dto.NewCouponResponse(c)
			}
		}
		if expand.Has(types.ExpandSubscriptionLineItems) && ca.SubscriptionLineItemID != nil {
			if li, ok := lineItemsByID[*ca.SubscriptionLineItemID]; ok {
				item.SubscriptionLineItem = li
			}
		}
		items[i] = item
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
// Handles both subscription-level and line item-level coupons based on LineItemID field
// Uses the subscription object for validation (avoids DB fetch in transactions)
func (s *couponAssociationService) ApplyCouponsToSubscription(ctx context.Context, subscription *subscription.Subscription, coupons []dto.SubscriptionCouponRequest) error {
	if subscription == nil {
		return ierr.NewError("subscription is required").
			WithHint("Please provide a valid subscription object").
			Mark(ierr.ErrValidation)
	}

	if subscription.ID == "" {
		return ierr.NewError("subscription_id is required").
			WithHint("Subscription must have a valid ID").
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

		// Get coupon details for validation
		coupon, err := s.CouponRepo.Get(ctx, couponReq.CouponID)
		if err != nil {
			return err
		}

		// Validate coupon applicability using subscription object (avoids DB fetch)
		if err := validationService.ValidateCoupon(ctx, *coupon, subscription); err != nil {
			return ierr.WithError(err).
				WithHint("Coupon validation failed").
				WithReportableDetails(map[string]interface{}{
					"coupon_id":       couponReq.CouponID,
					"subscription_id": subscription.ID,
				}).
				Mark(ierr.ErrValidation)
		}

		// For repeated cadence with no explicit end_date, derive end_date from
		// duration_in_periods so the ActiveOnly date filter handles expiry without
		// counting CouponApplication rows on every invoice.
		if coupon.Cadence == types.CouponCadenceRepeated &&
			coupon.DurationInPeriods != nil &&
			couponReq.EndDate == nil {
			endDate, err := computeCouponEndDate(
				couponReq.StartDate,
				subscription.BillingAnchor,
				subscription.BillingPeriod,
				subscription.BillingPeriodCount,
				*coupon.DurationInPeriods,
			)
			if err != nil {
				return err
			}
			couponReq.EndDate = &endDate
		}

		// Create coupon association request
		// LineItemID is used directly if provided (coupon applies to specific line item)
		// If omitted, coupon applies at subscription level
		createReq := dto.CreateCouponAssociationRequest{
			CouponID:               couponReq.CouponID,
			SubscriptionID:         subscription.ID,
			SubscriptionLineItemID: couponReq.LineItemID,
			StartDate:              couponReq.StartDate.UTC(),
			EndDate:                couponReq.EndDate,
			SubscriptionPhaseID:    couponReq.SubscriptionPhaseID,
			Metadata:               map[string]string{},
		}

		// Create the coupon association
		_, err = s.CreateCouponAssociation(ctx, createReq)
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

// computeCouponEndDate advances startDate through n billing periods using the
// subscription's billing anchor and period configuration (same logic as invoice
// scheduling), then subtracts 1s so the result is strictly before the (n+1)th
// period start — ensuring the ActiveOnly filter (end_date >= period_start) does
// not match that next period.
func computeCouponEndDate(startDate, billingAnchor time.Time, period types.BillingPeriod, periodCount, n int) (time.Time, error) {
	current := startDate
	for i := 0; i < n; i++ {
		next, err := types.NextBillingDate(current, billingAnchor, periodCount, period, nil)
		if err != nil {
			return startDate, err
		}
		current = next
	}
	return current.Add(-time.Second), nil
}
