package dto

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/alert"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// CreateAlertRequest represents the request to create a new alert
type CreateAlertRequest struct {
	EntityType  string                 `json:"entity_type" validate:"required"`
	EntityID    string                 `json:"entity_id"`
	AlertMetric types.AlertMetric      `json:"alert_metric" validate:"required"`
	AlertInfo   map[string]interface{} `json:"alert_info,omitempty"`
}

// Validate validates the create alert request
func (r *CreateAlertRequest) Validate() error {
	if r.EntityType == "" {
		return ierr.NewError("entity_type is required").
			WithHint("Entity type is required").
			Mark(ierr.ErrValidation)
	}
	if string(r.AlertMetric) == "" {
		return ierr.NewError("alert_metric is required").
			WithHint("Alert metric is required").
			Mark(ierr.ErrValidation)
	}
	// Validate that alert metric is one of the allowed values
	switch r.AlertMetric {
	case types.AlertMetricCreditBalance,
		types.AlertMetricOngoingBalance:
		// Valid metric
	default:
		return ierr.NewError("invalid alert_metric").
			WithHint("Alert metric must be one of: credit_balance, ongoing_balance").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// ToAlert converts the request to an alert domain model
func (r *CreateAlertRequest) ToAlert(ctx context.Context) *alert.Alert {
	now := time.Now().UTC()
	return &alert.Alert{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ALERT),
		EntityType:    r.EntityType,
		EntityID:      r.EntityID,
		AlertMetric:   r.AlertMetric,
		AlertState:    types.AlertStateOk,
		AlertInfo:     r.AlertInfo,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel: types.BaseModel{
			TenantID:  types.GetTenantID(ctx),
			Status:    types.StatusPublished,
			CreatedAt: now,
			UpdatedAt: now,
			CreatedBy: types.GetUserID(ctx),
			UpdatedBy: types.GetUserID(ctx),
		},
	}
}

// AlertResponse represents the response for alert operations
type AlertResponse struct {
	ID            string                 `json:"id"`
	EntityType    string                 `json:"entity_type"`
	EntityID      string                 `json:"entity_id,omitempty"`
	AlertMetric   types.AlertMetric      `json:"alert_metric"`
	AlertState    string                 `json:"alert_state"`
	AlertInfo     map[string]interface{} `json:"alert_info,omitempty"`
	TenantID      string                 `json:"tenant_id"`
	EnvironmentID string                 `json:"environment_id"`
	Status        string                 `json:"status"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
	CreatedBy     string                 `json:"created_by"`
	UpdatedBy     string                 `json:"updated_by"`
}

// NewAlertResponse creates a new alert response from an alert domain model
func NewAlertResponse(a *alert.Alert) *AlertResponse {
	if a == nil {
		return nil
	}
	return &AlertResponse{
		ID:            a.ID,
		EntityType:    a.EntityType,
		EntityID:      a.EntityID,
		AlertMetric:   a.AlertMetric,
		AlertState:    string(a.AlertState),
		AlertInfo:     a.AlertInfo,
		TenantID:      a.TenantID,
		EnvironmentID: a.EnvironmentID,
		Status:        string(a.Status),
		CreatedAt:     a.CreatedAt,
		UpdatedAt:     a.UpdatedAt,
		CreatedBy:     a.CreatedBy,
		UpdatedBy:     a.UpdatedBy,
	}
}

// ListAlertsResponse represents the response for listing alerts
type ListAlertsResponse struct {
	Items []*AlertResponse `json:"items"`
}

// NewListAlertsResponse creates a new list alerts response from a slice of alert domain models
func NewListAlertsResponse(alerts []*alert.Alert) *ListAlertsResponse {
	items := make([]*AlertResponse, len(alerts))
	for i, a := range alerts {
		items[i] = NewAlertResponse(a)
	}
	return &ListAlertsResponse{
		Items: items,
	}
}

// CheckAlertsRequest represents the request to check alerts
type CheckAlertsRequest struct {
	TenantIDs   []string              `json:"tenant_ids" validate:"required"`
	EnvIDs      []string              `json:"env_ids" validate:"required"`
	EntityType  string                `json:"entity_type" validate:"required"`
	EntityIDs   []string              `json:"entity_ids,omitempty"`
	AlertMetric types.AlertMetric     `json:"alert_metric" validate:"required"`
	Threshold   *types.AlertThreshold `json:"threshold,omitempty"`
}

// Validate validates the check alerts request
func (r *CheckAlertsRequest) Validate() error {
	if len(r.TenantIDs) == 0 {
		return ierr.NewError("tenant_ids is required").
			WithHint("At least one tenant ID is required").
			Mark(ierr.ErrValidation)
	}
	if len(r.EnvIDs) == 0 {
		return ierr.NewError("env_ids is required").
			WithHint("At least one environment ID is required").
			Mark(ierr.ErrValidation)
	}
	if r.EntityType == "" {
		return ierr.NewError("entity_type is required").
			WithHint("Entity type is required").
			Mark(ierr.ErrValidation)
	}
	if string(r.AlertMetric) == "" {
		return ierr.NewError("alert_metric is required").
			WithHint("Alert metric is required").
			Mark(ierr.ErrValidation)
	}
	// Validate that alert metric is one of the allowed values
	switch r.AlertMetric {
	case types.AlertMetricCreditBalance,
		types.AlertMetricOngoingBalance:
		// Valid metric
	default:
		return ierr.NewError("invalid alert_metric").
			WithHint("Alert metric must be one of: credit_balance, ongoing_balance").
			Mark(ierr.ErrValidation)
	}
	// Threshold is optional - if not provided, entity's own threshold will be used
	return nil
}
