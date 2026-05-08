package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
)

func (s *subscriptionModificationService) previewGroupedInvoicingMembership(
	ctx context.Context,
	modifyType dto.SubscriptionModifyType,
	params *dto.SubModifyGroupedInvoicingParams,
) (*dto.SubscriptionModifyResponse, error) {
	subSvc := NewSubscriptionService(s.serviceParams).(*subscriptionService)

	var parentSub *subscription.Subscription
	if modifyType == dto.SubscriptionModifyTypeGroupedInvoicingAdd {
		var err error
		parentSub, err = s.serviceParams.SubRepo.Get(ctx, params.ParentSubscriptionID)
		if err != nil {
			return nil, err
		}
	}

	changed := make([]dto.ChangedSubscription, 0, len(params.ChildSubscriptionIDs))
	for _, childID := range params.ChildSubscriptionIDs {
		var validateErr error
		if modifyType == dto.SubscriptionModifyTypeGroupedInvoicingAdd {
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
	modifyType dto.SubscriptionModifyType,
	params *dto.SubModifyGroupedInvoicingParams,
) (*dto.SubscriptionModifyResponse, error) {
	subSvc := NewSubscriptionService(s.serviceParams).(*subscriptionService)

	var parentSub *subscription.Subscription
	if modifyType == dto.SubscriptionModifyTypeGroupedInvoicingAdd {
		var err error
		parentSub, err = s.serviceParams.SubRepo.Get(ctx, params.ParentSubscriptionID)
		if err != nil {
			return nil, err
		}
	}

	changed := make([]dto.ChangedSubscription, 0, len(params.ChildSubscriptionIDs))
	err := s.serviceParams.DB.WithTx(ctx, func(txCtx context.Context) error {
		for _, childID := range params.ChildSubscriptionIDs {
			var opErr error
			if modifyType == dto.SubscriptionModifyTypeGroupedInvoicingAdd {
				opErr = subSvc.addToGroupedInvoicing(txCtx, parentSub, childID)
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

	return &dto.SubscriptionModifyResponse{
		ChangedResources: dto.ChangedResources{
			Subscriptions: changed,
		},
	}, nil
}
