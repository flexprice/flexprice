package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	coupon_association "github.com/flexprice/flexprice/internal/domain/coupon_association"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

func (s *subscriptionModificationService) executeCouponModification(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyCouponParams,
) (*dto.SubscriptionModifyResponse, error) {
	effectiveDate := time.Now().UTC()
	if params.EffectiveDate != nil {
		effectiveDate = params.EffectiveDate.UTC()
	}
	switch params.Action {
	case dto.SubModifyCouponActionAdd:
		return s.executeAddCoupon(ctx, subscriptionID, params, effectiveDate)
	case dto.SubModifyCouponActionRemove:
		return s.executeRemoveCoupon(ctx, subscriptionID, *params.AssociationID, effectiveDate)
	default:
		return nil, ierr.NewError("unknown coupon action: " + string(params.Action)).
			Mark(ierr.ErrValidation)
	}
}

func (s *subscriptionModificationService) executeAddCoupon(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyCouponParams,
	effectiveDate time.Time,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams

	// Validate subscription exists before any mutation.
	sub, err := sp.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	// Resolve coupon by coupon_code only.
	c, err := sp.CouponRepo.GetByCode(ctx, *params.CouponCode)
	if err != nil {
		return nil, ierr.NewError("coupon not found").
			WithHintf("No published coupon with code '%s'", *params.CouponCode).
			Mark(ierr.ErrValidation)
	}
	if c.Status != types.StatusPublished {
		return nil, ierr.NewError("coupon not found or inactive").
			WithHint("Ensure the coupon is in 'published' status").
			Mark(ierr.ErrValidation)
	}
	couponID := c.ID

	// Resolve target: line-item level or subscription level.
	var lineItemID *string
	if params.SubscriptionLineItemID != nil {
		lineItem, err := sp.SubscriptionLineItemRepo.Get(ctx, *params.SubscriptionLineItemID)
		if err != nil {
			return nil, err
		}
		if lineItem.SubscriptionID != subscriptionID {
			return nil, ierr.NewError("subscription_line_item_id does not belong to this subscription").
				WithReportableDetails(map[string]interface{}{
					"subscription_line_item_id": *params.SubscriptionLineItemID,
					"subscription_id":           subscriptionID,
				}).
				Mark(ierr.ErrValidation)
		}
		lineItemID = params.SubscriptionLineItemID
	} else if params.SubscriptionID != nil && *params.SubscriptionID != subscriptionID {
		return nil, ierr.NewError("subscription_id does not match the subscription being modified").
			WithReportableDetails(map[string]interface{}{
				"provided_subscription_id": *params.SubscriptionID,
				"subscription_id":          subscriptionID,
			}).
			Mark(ierr.ErrValidation)
	}

	startDate := effectiveDate
	if params.StartDate != nil {
		startDate = params.StartDate.UTC()
	}

	filter := &types.CouponAssociationFilter{
		QueryFilter:     types.NewNoLimitQueryFilter(),
		SubscriptionIDs: []string{subscriptionID},
		CouponIDs:       []string{couponID},
		ActiveOnly:      true,
		PeriodStart:     &effectiveDate,
		PeriodEnd:       &effectiveDate,
	}
	existing, err := sp.CouponAssociationRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		return nil, ierr.NewError("coupon already active on this subscription for the given date range").
			WithHint("Remove the existing coupon association before adding it again, or use a different effective_date").
			WithReportableDetails(map[string]interface{}{
				"coupon_id":       couponID,
				"subscription_id": subscriptionID,
				"effective_date":  effectiveDate,
			}).
			Mark(ierr.ErrValidation)
	}

	assoc := &coupon_association.CouponAssociation{
		ID:                     types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON_ASSOCIATION),
		CouponID:               couponID,
		SubscriptionID:         subscriptionID,
		SubscriptionLineItemID: lineItemID,
		StartDate:              startDate,
		EndDate:                params.EndDate,
		EnvironmentID:          types.GetEnvironmentID(ctx),
		BaseModel:              types.GetDefaultBaseModel(ctx),
	}
	if err := sp.DB.WithTx(ctx, func(txCtx context.Context) error {
		return sp.CouponAssociationRepo.Create(txCtx, assoc)
	}); err != nil {
		return nil, err
	}

	s.publishSystemEvent(ctx, types.WebhookEventSubscriptionUpdated, subscriptionID)

	return &dto.SubscriptionModifyResponse{
		Subscription:     &dto.SubscriptionResponse{Subscription: sub},
		ChangedResources: dto.ChangedResources{},
	}, nil
}

func (s *subscriptionModificationService) executeRemoveCoupon(
	ctx context.Context,
	subscriptionID string,
	associationID string,
	effectiveDate time.Time,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams

	// Validate subscription exists before any mutation.
	sub, err := sp.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	assoc, err := sp.CouponAssociationRepo.Get(ctx, associationID)
	if err != nil {
		return nil, ierr.NewError("association not found").
			WithHint("Provide a valid association_id belonging to this subscription").
			WithReportableDetails(map[string]interface{}{"association_id": associationID}).
			Mark(ierr.ErrNotFound)
	}
	if assoc.SubscriptionID != subscriptionID {
		return nil, ierr.NewError("association does not belong to this subscription").
			WithReportableDetails(map[string]interface{}{
				"association_id":  associationID,
				"subscription_id": subscriptionID,
			}).
			Mark(ierr.ErrValidation)
	}

	if assoc.EndDate != nil && !assoc.EndDate.After(effectiveDate) {
		return nil, ierr.NewError("association already inactive").
			WithHint("This coupon association has already ended").
			WithReportableDetails(map[string]interface{}{
				"association_id": associationID,
				"end_date":       assoc.EndDate,
			}).
			Mark(ierr.ErrValidation)
	}

	if effectiveDate.Before(assoc.StartDate) {
		return nil, ierr.NewError("effective_date cannot be before association start_date").
			WithHint("Use an effective_date on or after the association start date").
			WithReportableDetails(map[string]interface{}{
				"association_id": associationID,
				"start_date":     assoc.StartDate,
				"effective_date": effectiveDate,
			}).
			Mark(ierr.ErrValidation)
	}

	assoc.EndDate = &effectiveDate
	if err := sp.DB.WithTx(ctx, func(txCtx context.Context) error {
		return sp.CouponAssociationRepo.Update(txCtx, assoc)
	}); err != nil {
		return nil, err
	}

	s.publishSystemEvent(ctx, types.WebhookEventSubscriptionUpdated, subscriptionID)

	return &dto.SubscriptionModifyResponse{
		Subscription:     &dto.SubscriptionResponse{Subscription: sub},
		ChangedResources: dto.ChangedResources{},
	}, nil
}

func (s *subscriptionModificationService) previewCouponModification(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyCouponParams,
) (*dto.SubscriptionModifyResponse, error) {
	effectiveDate := time.Now().UTC()
	if params.EffectiveDate != nil {
		effectiveDate = params.EffectiveDate.UTC()
	}
	switch params.Action {
	case dto.SubModifyCouponActionAdd:
		return s.previewAddCoupon(ctx, subscriptionID, params, effectiveDate)
	case dto.SubModifyCouponActionRemove:
		return s.previewRemoveCoupon(ctx, subscriptionID, *params.AssociationID, effectiveDate)
	default:
		return nil, ierr.NewError("unknown coupon action: " + string(params.Action)).
			Mark(ierr.ErrValidation)
	}
}

func (s *subscriptionModificationService) previewAddCoupon(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyCouponParams,
	effectiveDate time.Time,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams

	// Resolve coupon by coupon_code only.
	c, err := sp.CouponRepo.GetByCode(ctx, *params.CouponCode)
	if err != nil {
		return nil, ierr.NewError("coupon not found").
			WithHintf("No published coupon with code '%s'", *params.CouponCode).
			Mark(ierr.ErrValidation)
	}
	if c.Status != types.StatusPublished {
		return nil, ierr.NewError("coupon not found or inactive").
			WithHint("Ensure the coupon is in 'published' status").
			Mark(ierr.ErrValidation)
	}
	couponID := c.ID

	// Resolve target: line-item level or subscription level.
	if params.SubscriptionLineItemID != nil {
		lineItem, err := sp.SubscriptionLineItemRepo.Get(ctx, *params.SubscriptionLineItemID)
		if err != nil {
			return nil, err
		}
		if lineItem.SubscriptionID != subscriptionID {
			return nil, ierr.NewError("subscription_line_item_id does not belong to this subscription").
				WithReportableDetails(map[string]interface{}{
					"subscription_line_item_id": *params.SubscriptionLineItemID,
					"subscription_id":           subscriptionID,
				}).
				Mark(ierr.ErrValidation)
		}
	} else if params.SubscriptionID != nil && *params.SubscriptionID != subscriptionID {
		return nil, ierr.NewError("subscription_id does not match the subscription being modified").
			WithReportableDetails(map[string]interface{}{
				"provided_subscription_id": *params.SubscriptionID,
				"subscription_id":          subscriptionID,
			}).
			Mark(ierr.ErrValidation)
	}

	filter := &types.CouponAssociationFilter{
		QueryFilter:     types.NewNoLimitQueryFilter(),
		SubscriptionIDs: []string{subscriptionID},
		CouponIDs:       []string{couponID},
		ActiveOnly:      true,
		PeriodStart:     &effectiveDate,
		PeriodEnd:       &effectiveDate,
	}
	existing, err := sp.CouponAssociationRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		return nil, ierr.NewError("coupon already active on this subscription for the given date range").
			WithReportableDetails(map[string]interface{}{
				"coupon_id":       couponID,
				"subscription_id": subscriptionID,
				"effective_date":  effectiveDate,
			}).
			Mark(ierr.ErrValidation)
	}

	sub, err := sp.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	return &dto.SubscriptionModifyResponse{
		Subscription:     &dto.SubscriptionResponse{Subscription: sub},
		ChangedResources: dto.ChangedResources{},
	}, nil
}

func (s *subscriptionModificationService) previewRemoveCoupon(
	ctx context.Context,
	subscriptionID string,
	associationID string,
	effectiveDate time.Time,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams

	assoc, err := sp.CouponAssociationRepo.Get(ctx, associationID)
	if err != nil {
		return nil, ierr.NewError("association not found").
			WithHint("Provide a valid association_id").
			WithReportableDetails(map[string]interface{}{"association_id": associationID}).
			Mark(ierr.ErrNotFound)
	}
	if assoc.SubscriptionID != subscriptionID {
		return nil, ierr.NewError("association does not belong to this subscription").
			WithReportableDetails(map[string]interface{}{
				"association_id":  associationID,
				"subscription_id": subscriptionID,
			}).
			Mark(ierr.ErrValidation)
	}
	if assoc.EndDate != nil && !assoc.EndDate.After(effectiveDate) {
		return nil, ierr.NewError("association already inactive").
			WithReportableDetails(map[string]interface{}{"association_id": associationID}).
			Mark(ierr.ErrValidation)
	}

	if effectiveDate.Before(assoc.StartDate) {
		return nil, ierr.NewError("effective_date cannot be before association start_date").
			WithHint("Use an effective_date on or after the association start date").
			WithReportableDetails(map[string]interface{}{
				"association_id": associationID,
				"start_date":     assoc.StartDate,
				"effective_date": effectiveDate,
			}).
			Mark(ierr.ErrValidation)
	}

	sub, err := sp.SubRepo.Get(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	return &dto.SubscriptionModifyResponse{
		Subscription:     &dto.SubscriptionResponse{Subscription: sub},
		ChangedResources: dto.ChangedResources{},
	}, nil
}
