package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

func (s *subscriptionModificationService) executeBulkAddonModification(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyBulkAddonParams,
	checkout *dto.CheckoutParams,
) (*dto.SubscriptionModifyResponse, error) {
	sub, err := s.loadSubscriptionWithLineItems(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	changeSvc := NewAddonChangeService(s.serviceParams)
	req := NewAddonChangeRequest(sub, params)

	if checkout != nil {
		gated, err := changeSvc.ExecutePayFirst(ctx, req, checkout)
		if err != nil {
			return nil, err
		}

		// Nothing is live yet: the attaches are pending and the removals untouched, so there
		// is no subscription update to announce.
		if gated != nil {
			return s.addonModifyResponse(
				ctx,
				subscriptionID,
				bulkAddonChangedLineItems(gated.getConfig(), false),
				nil,
				gated.getSession(),
			)
		}
		// Zero or negative net → nothing to collect, so fall through and apply immediately.
	}

	config, settled, err := changeSvc.Execute(ctx, req)
	if err != nil {
		return nil, err
	}

	s.publishSystemEvent(ctx, types.WebhookEventSubscriptionUpdated, subscriptionID)
	triggerHubSpotDealSync(ctx, s.serviceParams, subscriptionID)

	return s.addonModifyResponse(
		ctx,
		subscriptionID,
		bulkAddonChangedLineItems(config, false),
		settled.GetChanged(),
		nil,
	)
}

func (s *subscriptionModificationService) previewBulkAddonModification(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyBulkAddonParams,
) (*dto.SubscriptionModifyResponse, error) {
	sub, err := s.loadSubscriptionWithLineItems(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	config, settled, err := NewAddonChangeService(s.serviceParams).
		Preview(ctx, NewAddonChangeRequest(sub, params))
	if err != nil {
		return nil, err
	}

	return s.addonModifyResponse(ctx, subscriptionID,
		bulkAddonChangedLineItems(config, true), settled.GetChanged(), nil)
}

// Each ended item carries its own detach date: entries in a batch do not share one.
func bulkAddonChangedLineItems(config *addonChangeConfig, isPreview bool) []dto.ChangedLineItem {
	items := []dto.ChangedLineItem{}

	for _, attach := range config.getAttaches() {
		for _, li := range attach.getLineItems() {
			items = append(items, changedCreatedLineItem(li, isPreview))
		}
	}

	for _, detach := range config.getDetaches() {
		for _, li := range detach.getLineItems() {
			items = append(items, changedEndedLineItem(li, detach.getEffectiveDate()))
		}
	}

	if len(items) == 0 {
		return nil
	}
	return items
}
