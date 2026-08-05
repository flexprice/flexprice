package webhookDto

import "github.com/flexprice/flexprice/internal/types"

type InternalAlertEvent struct {
	FeatureID   string           `json:"feature_id,omitempty"`
	WalletID    string           `json:"wallet_id,omitempty"`
	CustomerID  string           `json:"customer_id,omitempty"`
	AlertType   types.AlertType  `json:"alert_type"`
	AlertStatus types.AlertState `json:"alert_status"`

	EntityType       types.AlertEntityType `json:"entity_type,omitempty"`
	EntityID         string                `json:"entity_id,omitempty"`
	ParentEntityID   string                `json:"parent_entity_id,omitempty"`
	ParentEntityType types.AlertEntityType `json:"parent_entity_type,omitempty"`
	AlertInfo        types.AlertInfo       `json:"alert_info,omitempty"`
}
