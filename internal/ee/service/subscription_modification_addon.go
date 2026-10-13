package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addonassociation"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// addonBulkParams maps the single-addon payload onto the batch one, so type "addon" has one
// implementation whichever shape the caller sends.
func addonBulkParams(req dto.ExecuteSubscriptionModifyRequest) (*dto.SubModifyBulkAddonParams, error) {
	if req.BulkAddonParams != nil {
		return req.BulkAddonParams, nil
	}

	switch req.AddonParams.Action {
	case dto.SubscriptionModificationActionAdd:
		return &dto.SubModifyBulkAddonParams{
			Adds: []*dto.AddAddonToSubscriptionRequest{req.AddonParams.Add},
		}, nil
	case dto.SubscriptionModificationActionRemove:
		return &dto.SubModifyBulkAddonParams{
			Removes: []*dto.RemoveAddonRequest{req.AddonParams.Remove},
		}, nil
	default:
		return nil, ierr.NewError("invalid action, action must be add or remove").
			Mark(ierr.ErrValidation)
	}
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
	lineItems []dto.ChangedLineItem,
	associations []dto.ChangedAddonAssociation,
	invoices []dto.ChangedInvoice,
	checkoutSession *dto.CheckoutSessionResponse,
) (*dto.SubscriptionModifyResponse, error) {
	subResp, err := NewSubscriptionService(s.serviceParams).GetSubscription(ctx, subscriptionID)
	if err != nil {
		return nil, err
	}

	return &dto.SubscriptionModifyResponse{
		Subscription: subResp,
		ChangedResources: dto.ChangedResources{
			LineItems:         lineItems,
			AddonAssociations: associations,
			Invoices:          invoices,
		},
		CheckoutSession: checkoutSession,
	}, nil
}

func changedCreatedAssociation(
	association *addonassociation.AddonAssociation,
	isPreview bool,
) dto.ChangedAddonAssociation {
	id := association.ID
	if isPreview {
		id = previewCreatedID
	}

	changed := dto.ChangedAddonAssociation{
		ID:           id,
		AddonID:      association.AddonID,
		AddonStatus:  association.AddonStatus,
		StartDate:    association.StartDate,
		ChangeAction: dto.ChangedAddonAssociationActionCreated,
	}
	if association.EndDate != nil {
		changed.EndDate = association.EndDate
	}

	return changed
}

func changedEndedAssociation(
	association *addonassociation.AddonAssociation,
	endDate time.Time,
) dto.ChangedAddonAssociation {
	return dto.ChangedAddonAssociation{
		ID:           association.ID,
		AddonID:      association.AddonID,
		AddonStatus:  types.AddonStatusCancelled,
		StartDate:    association.StartDate,
		EndDate:      &endDate,
		ChangeAction: dto.ChangedAddonAssociationActionEnded,
	}
}

func changedCreatedLineItem(li *subscription.SubscriptionLineItem, isPreview bool) dto.ChangedLineItem {
	id := li.ID
	if isPreview {
		id = previewCreatedID
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

	return changed
}

func changedEndedLineItem(li *subscription.SubscriptionLineItem, endDate time.Time) dto.ChangedLineItem {
	startDate := li.StartDate
	return dto.ChangedLineItem{
		ID:           li.ID,
		PriceID:      li.PriceID,
		Quantity:     li.Quantity,
		StartDate:    &startDate,
		EndDate:      &endDate,
		ChangeAction: dto.ChangedLineItemActionEnded,
	}
}
