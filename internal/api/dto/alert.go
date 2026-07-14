package dto

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/alert"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/samber/lo"
)

// allowedAlertSettingsEntityTypes are the entity types this CRUD accepts. AlertEntityType.Validate
// also allows wallet/feature, which belong to the older AlertLogsService, so we narrow it here.
var allowedAlertSettingsEntityTypes = []types.AlertEntityType{
	types.AlertEntityTypeSubscription,
	types.AlertEntityTypeSubscriptionLineItem,
	types.AlertEntityTypeGroup,
}

// CreateAlertSettingsRequest is the body for creating a subscription-, line-item-, or
// group-level spend alert configuration.
type CreateAlertSettingsRequest struct {
	// entity_type is the entity being monitored: "subscription", "subscription_line_item", or "group".
	EntityType types.AlertEntityType `json:"entity_type" validate:"required"`

	// entity_id is the id of the monitored entity: subscription id, subscription_line_item id, or
	// group id, matching entity_type.
	EntityID string `json:"entity_id" validate:"required"`

	// parent_entity_type must be "subscription" when entity_type is "subscription_line_item" or
	// "group"; omitted for subscription-level alerts.
	ParentEntityType types.AlertEntityType `json:"parent_entity_type,omitempty"`

	// parent_entity_id is the subscription id that owns a line-item or group alert. Required when
	// entity_type is "subscription_line_item" or "group"; omitted for subscription-level alerts.
	ParentEntityID string `json:"parent_entity_id,omitempty"`

	// config holds the threshold configuration (critical / warning / info + alert_enabled).
	Config *types.AlertSettings `json:"config" validate:"required"`
}

// Validate checks field shape and the parent requirements for each scope. AlertService confirms
// the referenced entities actually exist, since that needs repository lookups.
func (r *CreateAlertSettingsRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	if err := r.EntityType.Validate(); err != nil {
		return err
	}
	if !lo.Contains(allowedAlertSettingsEntityTypes, r.EntityType) {
		return ierr.NewError("invalid entity type for alert settings").
			WithHint("entity_type must be one of subscription, subscription_line_item, group").
			WithReportableDetails(map[string]any{
				"entity_type": r.EntityType,
			}).
			Mark(ierr.ErrValidation)
	}

	switch r.EntityType {
	case types.AlertEntityTypeSubscription:
		if r.ParentEntityType != "" || r.ParentEntityID != "" {
			return ierr.NewError("parent entity must not be set for subscription-level alerts").
				WithHint("Subscription-level alerts are not scoped to a parent entity").
				Mark(ierr.ErrValidation)
		}

	case types.AlertEntityTypeSubscriptionLineItem:
		if r.ParentEntityType != types.AlertEntityTypeSubscription || r.ParentEntityID == "" {
			return ierr.NewError("parent_entity_type and parent_entity_id are required for line item alerts").
				WithHint("Set parent_entity_type to subscription and parent_entity_id to the owning subscription id").
				Mark(ierr.ErrValidation)
		}

	case types.AlertEntityTypeGroup:
		if r.ParentEntityType != types.AlertEntityTypeSubscription || r.ParentEntityID == "" {
			return ierr.NewError("parent_entity_type and parent_entity_id are mandatory for group alerts").
				WithHint("A group alert always belongs to exactly one subscription").
				Mark(ierr.ErrValidation)
		}
	}

	if err := r.Config.Validate(); err != nil {
		return err
	}
	if types.IsSubscriptionRootedAlert(r.EntityType, r.ParentEntityType) {
		return ValidateSpendThreshold(r.Config)
	}
	return nil
}

// ToAlertSettings converts the request into a domain AlertSettings ready for persistence.
func (r *CreateAlertSettingsRequest) ToAlertSettings(ctx context.Context) *alert.AlertSettings {
	var parentEntityType, parentEntityID *string
	if r.ParentEntityType != "" {
		parentEntityType = lo.ToPtr(string(r.ParentEntityType))
	}
	if r.ParentEntityID != "" {
		parentEntityID = lo.ToPtr(r.ParentEntityID)
	}

	return &alert.AlertSettings{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ALERT_SETTINGS),
		EntityType:       r.EntityType,
		EntityID:         r.EntityID,
		ParentEntityType: parentEntityType,
		ParentEntityID:   parentEntityID,
		Enabled:          r.Config.IsAlertEnabled(),
		Config:           r.Config,
		EnvironmentID:    types.GetEnvironmentID(ctx),
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
}

// UpdateAlertSettingsRequest is the body for PUT /v1/alerts/setting/:id. Config replaces the
// stored config wholesale, not a per-field merge. The caller must send the complete desired config every time;
// a threshold left out of the request is cleared, not left alone.
type UpdateAlertSettingsRequest struct {
	Config *types.AlertSettings `json:"config" validate:"required"`
}

// Validate checks field shape.
func (r *UpdateAlertSettingsRequest) Validate() error {
	return validator.ValidateRequest(r)
}

// ValidateSpendThreshold requires every configured threshold to use the "above" condition. It only
// applies to subscription-rooted rows; wallet alerts elsewhere legitimately use "below".
func ValidateSpendThreshold(config *types.AlertSettings) error {
	thresholds := []*types.AlertThreshold{config.Critical, config.Warning, config.Info}
	for _, threshold := range thresholds {
		if threshold != nil && threshold.Condition != types.AlertConditionAbove {
			return ierr.NewError("alert threshold condition must be 'above' for subscription spend alerts").
				WithHint("Subscription, line item, and group spend alerts only support the 'above' condition").
				Mark(ierr.ErrValidation)
		}
	}
	return nil
}

// AlertSettingsResponse represents the alert settings response, shared by all three scopes.
type AlertSettingsResponse struct {
	*alert.AlertSettings
}

// ListAlertSettingsResponse represents the paginated list response for alert settings.
type ListAlertSettingsResponse = types.ListResponse[*AlertSettingsResponse] // @name ListAlertSettingsResponse

// ToAlertSettingsResponse converts a domain AlertSettings into its response DTO.
func ToAlertSettingsResponse(a *alert.AlertSettings) *AlertSettingsResponse {
	return &AlertSettingsResponse{AlertSettings: a}
}
