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
		return s.executeAddCoupon(ctx, subscriptionID, *params.CouponID, effectiveDate)
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
	couponID string,
	effectiveDate time.Time,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams

	// Validate subscription exists before any mutation.
	subSvc := NewSubscriptionService(sp)
	subResp, err := subSvc.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	c, err := sp.CouponRepo.Get(ctx, couponID)
	if err != nil {
		return nil, ierr.NewError("coupon not found or inactive").
			WithHint("Provide a valid, active coupon_id").
			WithReportableDetails(map[string]interface{}{"coupon_id": couponID}).
			Mark(ierr.ErrValidation)
	}
	if c.Status != types.StatusPublished {
		return nil, ierr.NewError("coupon not found or inactive").
			WithHint("The specified coupon is not currently active").
			WithReportableDetails(map[string]interface{}{"coupon_id": couponID, "status": c.Status}).
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
			WithHint("Remove the existing coupon association before adding it again, or use a different effective_date").
			WithReportableDetails(map[string]interface{}{
				"coupon_id":       couponID,
				"subscription_id": subscriptionID,
				"effective_date":  effectiveDate,
			}).
			Mark(ierr.ErrValidation)
	}

	assoc := &coupon_association.CouponAssociation{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON_ASSOCIATION),
		CouponID:       couponID,
		SubscriptionID: subscriptionID,
		StartDate:      effectiveDate,
		EnvironmentID:  types.GetEnvironmentID(ctx),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	if err := sp.DB.WithTx(ctx, func(txCtx context.Context) error {
		return sp.CouponAssociationRepo.Create(txCtx, assoc)
	}); err != nil {
		return nil, err
	}

	s.publishSystemEvent(ctx, types.WebhookEventSubscriptionUpdated, subscriptionID)

	return &dto.SubscriptionModifyResponse{
		Subscription:     subResp,
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
	subSvc := NewSubscriptionService(sp)
	subResp, err := subSvc.GetSubscription(ctx, subscriptionID)
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
		Subscription:     subResp,
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
		return s.previewAddCoupon(ctx, subscriptionID, *params.CouponID, effectiveDate)
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
	couponID string,
	effectiveDate time.Time,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams

	c, err := sp.CouponRepo.Get(ctx, couponID)
	if err != nil {
		return nil, ierr.NewError("coupon not found or inactive").
			WithHint("Provide a valid, active coupon_id").
			WithReportableDetails(map[string]interface{}{"coupon_id": couponID}).
			Mark(ierr.ErrValidation)
	}
	if c.Status != types.StatusPublished {
		return nil, ierr.NewError("coupon not found or inactive").
			WithReportableDetails(map[string]interface{}{"coupon_id": couponID, "status": c.Status}).
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
			}).
			Mark(ierr.ErrValidation)
	}

	subSvc := NewSubscriptionService(sp)
	subResp, err := subSvc.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	return &dto.SubscriptionModifyResponse{
		Subscription:     subResp,
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

	subSvc := NewSubscriptionService(sp)
	subResp, err := subSvc.GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}
	return &dto.SubscriptionModifyResponse{
		Subscription:     subResp,
		ChangedResources: dto.ChangedResources{},
	}, nil
}
