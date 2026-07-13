package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// AlertService owns alert_settings CRUD: the configuration for subscription, subscription
// line item, and group spend alerts. See ERD subscription-spend-notifications.md.
//
// The pre-existing AlertLogsService (wallet / feature-wallet-balance alert logging) is
// intentionally untouched and separate for now.
type AlertService interface {
	CreateAlertSettings(ctx context.Context, req dto.CreateAlertSettingsRequest) (*dto.AlertSettingsResponse, error)
	UpdateAlertSettings(ctx context.Context, id string, req dto.UpdateAlertSettingsRequest) (*dto.AlertSettingsResponse, error)
	DeleteAlertSettings(ctx context.Context, id string) error
	GetAlertSettings(ctx context.Context, id string) (*dto.AlertSettingsResponse, error)
	ListAlertSettings(ctx context.Context, filter *types.AlertSettingsFilter) (*dto.ListAlertSettingsResponse, error)
}

type alertService struct {
	ServiceParams
}

func NewAlertService(params ServiceParams) AlertService {
	return &alertService{
		ServiceParams: params,
	}
}

func (s *alertService) CreateAlertSettings(ctx context.Context, req dto.CreateAlertSettingsRequest) (*dto.AlertSettingsResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Confirm the referenced entities exist (req.Validate already covered field shape).
	switch req.EntityType {
	case types.AlertEntityTypeSubscription:
		if _, err := s.SubRepo.Get(ctx, req.EntityID); err != nil {
			return nil, err
		}

	case types.AlertEntityTypeSubscriptionLineItem:
		// Fetch the line item; its SubscriptionID field proves the subscription exists too.
		lineItem, err := s.SubscriptionLineItemRepo.Get(ctx, req.EntityID)
		if err != nil {
			return nil, err
		}
		if lineItem.SubscriptionID != req.ParentEntityID {
			return nil, ierr.NewError("line item does not belong to the given subscription").
				WithHint("entity_id must be a line item on the subscription identified by parent_entity_id").
				WithReportableDetails(map[string]any{
					"entity_id":        req.EntityID,
					"parent_entity_id": req.ParentEntityID,
				}).
				Mark(ierr.ErrValidation)
		}

	case types.AlertEntityTypeGroup:
		if _, err := s.SubRepo.Get(ctx, req.ParentEntityID); err != nil {
			return nil, err
		}
		if _, err := s.GroupRepo.Get(ctx, req.EntityID); err != nil {
			return nil, err
		}
	}

	existing, err := s.AlertRepo.List(ctx, &types.AlertSettingsFilter{
		QueryFilter:      types.NewNoLimitQueryFilter(),
		EntityType:       req.EntityType,
		EntityID:         req.EntityID,
		ParentEntityType: req.ParentEntityType,
		ParentEntityID:   req.ParentEntityID,
	})
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		return nil, ierr.NewError("an alert configuration already exists for this entity").
			WithHint("Update or delete the existing alert configuration instead of creating a new one").
			WithReportableDetails(map[string]any{
				"entity_type":        req.EntityType,
				"entity_id":          req.EntityID,
				"parent_entity_type": req.ParentEntityType,
				"parent_entity_id":   req.ParentEntityID,
			}).
			Mark(ierr.ErrAlreadyExists)
	}

	alertSettings := req.ToAlertSettings(ctx)

	if err := s.AlertRepo.Create(ctx, alertSettings); err != nil {
		return nil, err
	}

	return dto.ToAlertSettingsResponse(alertSettings), nil
}

func (s *alertService) UpdateAlertSettings(ctx context.Context, id string, req dto.UpdateAlertSettingsRequest) (*dto.AlertSettingsResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	alertSettings, err := s.AlertRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// The caller sends the complete desired config
	// a threshold left out is cleared, not retained
	newConfig := req.Config
	if err := newConfig.Validate(); err != nil {
		return nil, err
	}

	// Gate the "above only" rule on the stored row's scope (the update payload carries no entity_type).
	parentEntityType := types.AlertEntityType(lo.FromPtr(alertSettings.ParentEntityType))
	if types.IsSubscriptionRootedAlert(alertSettings.EntityType, parentEntityType) {
		if err := dto.ValidateSpendThreshold(newConfig); err != nil {
			return nil, err
		}
	}

	alertSettings.Config = newConfig
	alertSettings.Enabled = newConfig.IsAlertEnabled()
	alertSettings.UpdatedAt = time.Now().UTC()
	alertSettings.UpdatedBy = types.GetUserID(ctx)

	if err := s.AlertRepo.Update(ctx, alertSettings); err != nil {
		return nil, err
	}

	return dto.ToAlertSettingsResponse(alertSettings), nil
}

func (s *alertService) DeleteAlertSettings(ctx context.Context, id string) error {
	if _, err := s.AlertRepo.Get(ctx, id); err != nil {
		return err
	}

	return s.AlertRepo.Delete(ctx, id)
}

func (s *alertService) GetAlertSettings(ctx context.Context, id string) (*dto.AlertSettingsResponse, error) {
	alertSettings, err := s.AlertRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	return dto.ToAlertSettingsResponse(alertSettings), nil
}

func (s *alertService) ListAlertSettings(ctx context.Context, filter *types.AlertSettingsFilter) (*dto.ListAlertSettingsResponse, error) {
	if filter == nil {
		filter = types.NewDefaultAlertSettingsFilter()
	}

	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewDefaultQueryFilter()
	}

	if err := filter.Validate(); err != nil {
		return nil, err
	}

	alertSettingsList, err := s.AlertRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	total, err := s.AlertRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	responses := make([]*dto.AlertSettingsResponse, len(alertSettingsList))
	for i, alertSettings := range alertSettingsList {
		responses[i] = dto.ToAlertSettingsResponse(alertSettings)
	}

	return &dto.ListAlertSettingsResponse{
		Items:      responses,
		Pagination: types.NewPaginationResponse(total, filter.GetLimit(), filter.GetOffset()),
	}, nil
}
