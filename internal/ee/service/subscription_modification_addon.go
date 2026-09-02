package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

const previewCreatedLineItemID = "(preview-created)"

func (s *subscriptionModificationService) executeAddonModification(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyAddonParams,
	checkout *dto.CheckoutParams,
) (*dto.SubscriptionModifyResponse, error) {
	subSvc := NewSubscriptionService(s.serviceParams)

	var (
		result *dto.AddonChangeResult
		err    error
	)

	switch params.Action {
	case dto.SubscriptionModificationActionAdd:
		var sub *subscription.Subscription
		if sub, err = s.loadSubscriptionWithLineItems(ctx, subscriptionID); err == nil {
			result, err = subSvc.AttachAddon(ctx, sub, params.Add, checkout)
		}
	case dto.SubscriptionModificationActionRemove:
		result, err = subSvc.DetachAddon(ctx, params.Remove, subscriptionID)
	default:
		return nil, ierr.NewError("invalid action, action must be add or remove").Mark(ierr.ErrValidation)
	}
	if err != nil {
		return nil, err
	}

	// A pay-first attach has changed nothing yet — the association is pending and the line
	// items appear only once payment lands, so there is no subscription update to announce.
	if !result.PaymentPending() {
		s.publishSystemEvent(ctx, types.WebhookEventSubscriptionUpdated, subscriptionID)
		triggerHubSpotDealSync(ctx, s.serviceParams, subscriptionID)
	}

	return s.addonModifyResponse(ctx, subscriptionID, result, false)
}

func (s *subscriptionModificationService) previewAddonModification(
	ctx context.Context,
	subscriptionID string,
	params *dto.SubModifyAddonParams,
) (*dto.SubscriptionModifyResponse, error) {
	subSvc := NewSubscriptionService(s.serviceParams)

	var (
		result *dto.AddonChangeResult
		err    error
	)

	switch params.Action {
	case dto.SubscriptionModificationActionAdd:
		var sub *subscription.Subscription
		if sub, err = s.loadSubscriptionWithLineItems(ctx, subscriptionID); err == nil {
			params.Add.PreviewOnly = true
			result, err = subSvc.AttachAddon(ctx, sub, params.Add, nil)
		}
	case dto.SubscriptionModificationActionRemove:
		params.Remove.PreviewOnly = true
		result, err = subSvc.DetachAddon(ctx, params.Remove, subscriptionID)
	default:
		return nil, ierr.NewError("invalid action, action must be add or remove").Mark(ierr.ErrValidation)
	}
	if err != nil {
		return nil, err
	}

	return s.addonModifyResponse(ctx, subscriptionID, result, true)
}

func (s *subscriptionModificationService) loadSubscriptionWithLineItems(
	ctx context.Context,
	subscriptionID string,
) (*subscription.Subscription, error) {
	sub, lineItems, err := s.serviceParams.SubRepo.GetWithLineItems(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	sub.LineItems = lineItems
	return sub, nil
}

func (s *subscriptionModificationService) addonModifyResponse(
	ctx context.Context,
	subscriptionID string,
	result *dto.AddonChangeResult,
	isPreview bool,
) (*dto.SubscriptionModifyResponse, error) {
	subResp, err := NewSubscriptionService(s.serviceParams).GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	return &dto.SubscriptionModifyResponse{
		Subscription: subResp,
		ChangedResources: dto.ChangedResources{
			LineItems: addonChangedLineItems(result, isPreview),
			Invoices:  result.GetChangedInvoices(),
		},
		CheckoutSession: result.GetCheckoutSession(),
	}, nil
}

func addonChangedLineItems(result *dto.AddonChangeResult, isPreview bool) []dto.ChangedLineItem {
	created := result.GetCreatedLineItems()
	ended := result.GetEndedLineItems()
	if len(created)+len(ended) == 0 {
		return nil
	}

	items := make([]dto.ChangedLineItem, 0, len(created)+len(ended))

	for _, li := range created {
		id := li.ID
		if isPreview {
			id = previewCreatedLineItemID
		}

		startDate := li.StartDate
		changed := dto.ChangedLineItem{
			ID:           id,
			PriceID:      li.PriceID,
			Quantity:     li.Quantity,
			StartDate:    &startDate,
			ChangeAction: dto.ChangedLineItemActionCreated,
		}
		if !li.EndDate.IsZero() {
			endDate := li.EndDate
			changed.EndDate = &endDate
		}
		items = append(items, changed)
	}

	endDate := result.GetEffectiveDate()
	for _, li := range ended {
		startDate := li.StartDate
		items = append(items, dto.ChangedLineItem{
			ID:           li.ID,
			PriceID:      li.PriceID,
			Quantity:     li.Quantity,
			StartDate:    &startDate,
			EndDate:      &endDate,
			ChangeAction: dto.ChangedLineItemActionEnded,
		})
	}

	return items
}
