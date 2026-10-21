package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
)

func (s *subscriptionModificationService) previewGroupedInvoicingMembership(
	ctx context.Context,
	params *dto.SubModifyGroupedInvoicingParams,
) (*dto.SubscriptionModifyResponse, error) {
	subSvc := NewSubscriptionService(s.serviceParams).(*subscriptionService)

	var parentSub *subscription.Subscription
	if params.Action == dto.GroupedInvoicingActionAdd {
		var err error
		parentSub, err = s.serviceParams.SubRepo.Get(ctx, params.ParentSubscriptionID)
		if err != nil {
			return nil, err
		}
	}

	changed := make([]dto.ChangedSubscription, 0, len(params.ChildSubscriptionIDs))
	for _, childID := range params.ChildSubscriptionIDs {
		var validateErr error
		if params.Action == dto.GroupedInvoicingActionAdd {
			validateErr = subSvc.validateAddToGroupedInvoicingDryRun(ctx, parentSub, childID)
		} else {
			validateErr = subSvc.validateRemoveFromGroupedInvoicingDryRun(ctx, childID)
		}
		if validateErr != nil {
			return nil, validateErr
		}
		changed = append(changed, dto.ChangedSubscription{
			ID:     childID,
			Action: dto.ChangedSubscriptionActionUpdated,
			Status: types.SubscriptionStatusActive,
		})
	}

	return &dto.SubscriptionModifyResponse{
		ChangedResources: dto.ChangedResources{
			Subscriptions: changed,
		},
	}, nil
}

func (s *subscriptionModificationService) executeGroupedInvoicingMembership(
	ctx context.Context,
	params *dto.SubModifyGroupedInvoicingParams,
) (*dto.SubscriptionModifyResponse, error) {
	subSvc := NewSubscriptionService(s.serviceParams).(*subscriptionService)

	var parentSub *subscription.Subscription
	if params.Action == dto.GroupedInvoicingActionAdd {
		var err error
		parentSub, err = s.serviceParams.SubRepo.Get(ctx, params.ParentSubscriptionID)
		if err != nil {
			return nil, err
		}
	}

	changed := make([]dto.ChangedSubscription, 0, len(params.ChildSubscriptionIDs))
	parentPromoted := false
	parentWasStandalone := parentSub != nil && parentSub.SubscriptionType == types.SubscriptionTypeStandalone

	err := s.serviceParams.DB.WithTx(ctx, func(txCtx context.Context) error {
		for _, childID := range params.ChildSubscriptionIDs {
			var opErr error
			if params.Action == dto.GroupedInvoicingActionAdd {
				opErr = subSvc.addToGroupedInvoicing(txCtx, parentSub, childID)
				if opErr == nil && parentWasStandalone && parentSub.SubscriptionType == types.SubscriptionTypeParent {
					parentPromoted = true
				}
			} else {
				opErr = subSvc.removeFromGroupedInvoicing(txCtx, childID)
			}
			if opErr != nil {
				return opErr
			}
			changed = append(changed, dto.ChangedSubscription{
				ID:     childID,
				Action: dto.ChangedSubscriptionActionUpdated,
				Status: types.SubscriptionStatusActive,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	for _, c := range changed {
		s.publishSystemEvent(ctx, types.WebhookEventSubscriptionUpdated, c.ID)
	}
	if parentPromoted && params.ParentSubscriptionID != "" {
		s.publishSystemEvent(ctx, types.WebhookEventSubscriptionUpdated, params.ParentSubscriptionID)
	}

	return &dto.SubscriptionModifyResponse{
		ChangedResources: dto.ChangedResources{
			Subscriptions: changed,
		},
	}, nil
}

func (s *subscriptionModificationService) previewGroupedAddonChange(
	ctx context.Context,
	parentID string,
	params *dto.SubModifyGroupedInvoicingParams,
) (*dto.SubscriptionModifyResponse, error) {
	parent, err := s.serviceParams.SubRepo.Get(ctx, parentID)
	if err != nil {
		return nil, err
	}

	reqs, err := s.groupedAddonChangeRequests(ctx, params)
	if err != nil {
		return nil, err
	}

	configs, settled, err := NewAddonChangeService(s.serviceParams).PreviewGrouped(ctx, parent, reqs)
	if err != nil {
		return nil, err
	}

	lineItems, associations := groupedAddonChanges(configs, true)
	return s.addonModifyResponse(ctx, parentID, lineItems, associations, settled.GetChanged(), nil)
}

func (s *subscriptionModificationService) executeGroupedAddonChange(
	ctx context.Context,
	parentID string,
	params *dto.SubModifyGroupedInvoicingParams,
	checkout *dto.CheckoutParams,
) (*dto.SubscriptionModifyResponse, error) {
	reqs, err := s.groupedAddonChangeRequests(ctx, params)
	if err != nil {
		return nil, err
	}

	changeSvc := NewAddonChangeService(s.serviceParams)
	if checkout != nil {
		gated, err := changeSvc.ExecutePayFirstGrouped(ctx, parentID, reqs, checkout)
		if err != nil {
			return nil, err
		}
		if gated != nil {
			lineItems, associations := groupedAddonChanges(gated.getConfigs(), false)
			return s.addonModifyResponse(ctx, parentID, lineItems, associations, draftChangedInvoices(gated.getSettled()), gated.getSession())
		}
	}

	configs, settled, err := changeSvc.ExecuteGrouped(ctx, parentID, reqs)
	if err != nil {
		return nil, err
	}

	for _, config := range configs {
		s.publishSystemEvent(ctx, types.WebhookEventSubscriptionUpdated, config.getSubscription().ID)
		triggerHubSpotDealSync(ctx, s.serviceParams, config.getSubscription().ID)
	}

	lineItems, associations := groupedAddonChanges(configs, false)
	return s.addonModifyResponse(ctx, parentID, lineItems, associations, settled.GetChanged(), nil)
}

func (s *subscriptionModificationService) groupedAddonChangeRequests(
	ctx context.Context,
	params *dto.SubModifyGroupedInvoicingParams,
) ([]AddonChangeRequest, error) {
	reqs := make([]AddonChangeRequest, 0, len(params.Changes))
	for _, change := range params.Changes {
		sub, err := s.loadSubscriptionWithLineItems(ctx, change.SubscriptionID)
		if err != nil {
			return nil, err
		}
		reqs = append(reqs, NewAddonChangeRequest(sub, change.AddonBulkParams))
	}

	return reqs, nil
}

func groupedAddonChanges(
	configs []*addonChangeConfig,
	isPreview bool,
) ([]dto.ChangedLineItem, []dto.ChangedAddonAssociation) {
	var lineItems []dto.ChangedLineItem
	var associations []dto.ChangedAddonAssociation
	for _, config := range configs {
		lineItems = append(lineItems, bulkAddonChangedLineItems(config, isPreview)...)
		associations = append(associations, bulkAddonChangedAssociations(config, isPreview)...)
	}

	return lineItems, associations
}
