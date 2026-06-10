package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	taxassociation "github.com/flexprice/flexprice/internal/domain/taxassociation"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

func (s *subscriptionModificationService) executeTaxModification(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyTaxParams,
) (*dto.SubscriptionModifyResponse, error) {
	effectiveDate := time.Now().UTC()
	if params.EffectiveDate != nil {
		effectiveDate = params.EffectiveDate.UTC()
	}
	switch params.Action {
	case dto.SubModifyActionAdd:
		return s.executeAddTax(ctx, subscriptionID, *params.TaxRateID, effectiveDate)
	case dto.SubModifyActionRemove:
		return s.executeRemoveTax(ctx, subscriptionID, *params.AssociationID, effectiveDate)
	default:
		return nil, ierr.NewError("unknown tax action: " + string(params.Action)).
			Mark(ierr.ErrValidation)
	}
}

func (s *subscriptionModificationService) executeAddTax(
	ctx context.Context,
	subscriptionID string,
	taxRateID string,
	effectiveDate time.Time,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams

	taxRate, err := sp.TaxRateRepo.Get(ctx, taxRateID)
	if err != nil {
		return nil, ierr.NewError("tax rate not found or inactive").
			WithHint("Provide a valid, active tax_rate_id").
			WithReportableDetails(map[string]interface{}{"tax_rate_id": taxRateID}).
			Mark(ierr.ErrValidation)
	}
	if taxRate.TaxRateStatus != types.TaxRateStatusActive {
		return nil, ierr.NewError("tax rate not found or inactive").
			WithHint("The specified tax rate is not currently active").
			WithReportableDetails(map[string]interface{}{
				"tax_rate_id": taxRateID,
				"status":      taxRate.TaxRateStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	filter := &types.TaxAssociationFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		EntityType:  types.TaxRateEntityTypeSubscription,
		EntityID:    subscriptionID,
		TaxRateIDs:  []string{taxRateID},
	}
	existing, err := sp.TaxAssociationRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	for _, ta := range existing {
		if !ta.StartDate.After(effectiveDate) && (ta.EndDate == nil || ta.EndDate.After(effectiveDate)) {
			return nil, ierr.NewError("tax rate already active on this subscription for the given date range").
				WithHint("Remove the existing tax association before adding it again, or use a different effective_date").
				WithReportableDetails(map[string]interface{}{
					"tax_rate_id":     taxRateID,
					"subscription_id": subscriptionID,
					"effective_date":  effectiveDate,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	assoc := &taxassociation.TaxAssociation{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_ASSOCIATION),
		TaxRateID:     taxRateID,
		EntityType:    types.TaxRateEntityTypeSubscription,
		EntityID:      subscriptionID,
		StartDate:     effectiveDate,
		Priority:      100,
		AutoApply:     true,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	if err := sp.TaxAssociationRepo.Create(ctx, assoc); err != nil {
		return nil, err
	}

	s.publishSystemEvent(ctx, types.WebhookEventSubscriptionUpdated, subscriptionID)

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

func (s *subscriptionModificationService) executeRemoveTax(
	ctx context.Context,
	subscriptionID string,
	associationID string,
	effectiveDate time.Time,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams

	assoc, err := sp.TaxAssociationRepo.Get(ctx, associationID)
	if err != nil {
		return nil, ierr.NewError("association not found").
			WithHint("Provide a valid association_id belonging to this subscription").
			WithReportableDetails(map[string]interface{}{"association_id": associationID}).
			Mark(ierr.ErrNotFound)
	}
	if assoc.EntityType != types.TaxRateEntityTypeSubscription || assoc.EntityID != subscriptionID {
		return nil, ierr.NewError("association does not belong to this subscription").
			WithReportableDetails(map[string]interface{}{
				"association_id":  associationID,
				"subscription_id": subscriptionID,
			}).
			Mark(ierr.ErrValidation)
	}

	if assoc.EndDate != nil && !assoc.EndDate.After(effectiveDate) {
		return nil, ierr.NewError("association already inactive").
			WithHint("This tax association has already ended").
			WithReportableDetails(map[string]interface{}{
				"association_id": associationID,
				"end_date":       assoc.EndDate,
			}).
			Mark(ierr.ErrValidation)
	}

	assoc.EndDate = &effectiveDate
	if err := sp.TaxAssociationRepo.Update(ctx, assoc); err != nil {
		return nil, err
	}

	s.publishSystemEvent(ctx, types.WebhookEventSubscriptionUpdated, subscriptionID)

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

func (s *subscriptionModificationService) previewTaxModification(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyTaxParams,
) (*dto.SubscriptionModifyResponse, error) {
	effectiveDate := time.Now().UTC()
	if params.EffectiveDate != nil {
		effectiveDate = params.EffectiveDate.UTC()
	}
	switch params.Action {
	case dto.SubModifyActionAdd:
		return s.previewAddTax(ctx, subscriptionID, *params.TaxRateID, effectiveDate)
	case dto.SubModifyActionRemove:
		return s.previewRemoveTax(ctx, subscriptionID, *params.AssociationID, effectiveDate)
	default:
		return nil, ierr.NewError("unknown tax action: " + string(params.Action)).
			Mark(ierr.ErrValidation)
	}
}

func (s *subscriptionModificationService) previewAddTax(
	ctx context.Context,
	subscriptionID string,
	taxRateID string,
	effectiveDate time.Time,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams

	taxRate, err := sp.TaxRateRepo.Get(ctx, taxRateID)
	if err != nil {
		return nil, ierr.NewError("tax rate not found or inactive").
			WithHint("Provide a valid, active tax_rate_id").
			WithReportableDetails(map[string]interface{}{"tax_rate_id": taxRateID}).
			Mark(ierr.ErrValidation)
	}
	if taxRate.TaxRateStatus != types.TaxRateStatusActive {
		return nil, ierr.NewError("tax rate not found or inactive").
			WithReportableDetails(map[string]interface{}{
				"tax_rate_id": taxRateID,
				"status":      taxRate.TaxRateStatus,
			}).
			Mark(ierr.ErrValidation)
	}

	filter := &types.TaxAssociationFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		EntityType:  types.TaxRateEntityTypeSubscription,
		EntityID:    subscriptionID,
		TaxRateIDs:  []string{taxRateID},
	}
	existing, err := sp.TaxAssociationRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	for _, ta := range existing {
		if !ta.StartDate.After(effectiveDate) && (ta.EndDate == nil || ta.EndDate.After(effectiveDate)) {
			return nil, ierr.NewError("tax rate already active on this subscription for the given date range").
				WithReportableDetails(map[string]interface{}{
					"tax_rate_id":     taxRateID,
					"subscription_id": subscriptionID,
				}).
				Mark(ierr.ErrValidation)
		}
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

func (s *subscriptionModificationService) previewRemoveTax(
	ctx context.Context,
	subscriptionID string,
	associationID string,
	effectiveDate time.Time,
) (*dto.SubscriptionModifyResponse, error) {
	sp := s.serviceParams

	assoc, err := sp.TaxAssociationRepo.Get(ctx, associationID)
	if err != nil {
		return nil, ierr.NewError("association not found").
			WithHint("Provide a valid association_id").
			WithReportableDetails(map[string]interface{}{"association_id": associationID}).
			Mark(ierr.ErrNotFound)
	}
	if assoc.EntityType != types.TaxRateEntityTypeSubscription || assoc.EntityID != subscriptionID {
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
