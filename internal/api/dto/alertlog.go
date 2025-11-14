package dto

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/alertlogs"
	"github.com/flexprice/flexprice/internal/types"
)

// AlertLogResponse represents the alert log response
type AlertLogResponse struct {
	ID               string                `json:"id"`
	EntityType       types.AlertEntityType `json:"entity_type"`
	EntityID         string                `json:"entity_id"`
	ParentEntityType *string               `json:"parent_entity_type,omitempty"`
	ParentEntityID   *string               `json:"parent_entity_id,omitempty"`
	CustomerID       *string               `json:"customer_id,omitempty"`
	AlertType        types.AlertType       `json:"alert_type"`
	AlertStatus      types.AlertState      `json:"alert_status"`
	AlertInfo        types.AlertInfo       `json:"alert_info"`
	EnvironmentID    string                `json:"environment_id"`
	TenantID         string                `json:"tenant_id"`
	Status           string                `json:"status"`
	CreatedAt        time.Time             `json:"created_at"`
	UpdatedAt        time.Time             `json:"updated_at"`
	CreatedBy        string                `json:"created_by"`
	UpdatedBy        string                `json:"updated_by"`

	// Expanded fields
	Customer *CustomerResponse `json:"customer,omitempty"`
	Wallet   *WalletResponse   `json:"wallet,omitempty"`
	Feature  *FeatureResponse  `json:"feature,omitempty"`
}

// ToAlertLogResponse converts domain alert log to DTO
func ToAlertLogResponse(alertLog *alertlogs.AlertLog) *AlertLogResponse {
	if alertLog == nil {
		return nil
	}

	return &AlertLogResponse{
		ID:               alertLog.ID,
		EntityType:       alertLog.EntityType,
		EntityID:         alertLog.EntityID,
		ParentEntityType: alertLog.ParentEntityType,
		ParentEntityID:   alertLog.ParentEntityID,
		CustomerID:       alertLog.CustomerID,
		AlertType:        alertLog.AlertType,
		AlertStatus:      alertLog.AlertStatus,
		AlertInfo:        alertLog.AlertInfo,
		EnvironmentID:    alertLog.EnvironmentID,
		TenantID:         alertLog.TenantID,
		Status:           string(alertLog.Status),
		CreatedAt:        alertLog.CreatedAt,
		UpdatedAt:        alertLog.UpdatedAt,
		CreatedBy:        alertLog.CreatedBy,
		UpdatedBy:        alertLog.UpdatedBy,
	}
}

// ListAlertLogsResponse represents the response for listing alert logs
type ListAlertLogsResponse struct {
	Items      []*AlertLogResponse       `json:"items"`
	Pagination *types.PaginationResponse `json:"pagination"`
}
