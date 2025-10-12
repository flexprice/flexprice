package dto

import (
	"encoding/json"

	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/types"
)

// CreateConnectionRequest represents the request to create a connection
type CreateConnectionRequest struct {
	Name                string                   `json:"name" validate:"required,max=255"`
	ProviderType        types.SecretProvider     `json:"provider_type" validate:"required"`
	EncryptedSecretData types.ConnectionMetadata `json:"encrypted_secret_data,omitempty"`
	Metadata            map[string]interface{}   `json:"metadata,omitempty"`
	SyncConfig          *types.SyncConfig        `json:"sync_config,omitempty" validate:"omitempty,dive"`
}

// UnmarshalJSON custom unmarshaling to handle flat metadata structure
func (req *CreateConnectionRequest) UnmarshalJSON(data []byte) error {
	// First, unmarshal to a temporary struct to get the raw data
	var temp struct {
		Name                string                 `json:"name"`
		ProviderType        types.SecretProvider   `json:"provider_type"`
		EncryptedSecretData map[string]interface{} `json:"encrypted_secret_data,omitempty"`
		Metadata            map[string]interface{} `json:"metadata,omitempty"`
		SyncConfig          *types.SyncConfig      `json:"sync_config,omitempty"`
	}

	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}

	// Set the basic fields
	req.Name = temp.Name
	req.ProviderType = temp.ProviderType
	req.Metadata = temp.Metadata
	req.SyncConfig = temp.SyncConfig

	// Convert flat encrypted secret data to structured format based on provider_type
	if temp.EncryptedSecretData != nil {
		req.EncryptedSecretData = convertFlatMetadataToStructured(temp.EncryptedSecretData, temp.ProviderType)
	}

	return nil
}

// convertFlatMetadataToStructured converts flat metadata to structured format
func convertFlatMetadataToStructured(flatMetadata map[string]interface{}, providerType types.SecretProvider) types.ConnectionMetadata {
	switch providerType {
	case types.SecretProviderStripe:
		stripeMetadata := &types.StripeConnectionMetadata{}

		if pk, ok := flatMetadata["publishable_key"].(string); ok {
			stripeMetadata.PublishableKey = pk
		}
		if sk, ok := flatMetadata["secret_key"].(string); ok {
			stripeMetadata.SecretKey = sk
		}
		if ws, ok := flatMetadata["webhook_secret"].(string); ok {
			stripeMetadata.WebhookSecret = ws
		}
		if aid, ok := flatMetadata["account_id"].(string); ok {
			stripeMetadata.AccountID = aid
		}

		return types.ConnectionMetadata{
			Stripe: stripeMetadata,
		}

	default:
		// For other providers or unknown types, use generic format
		return types.ConnectionMetadata{
			Generic: &types.GenericConnectionMetadata{
				Data: flatMetadata,
			},
		}
	}
}

// UpdateConnectionRequest represents the request to update a connection
type UpdateConnectionRequest struct {
	Name       string                 `json:"name,omitempty" validate:"omitempty,max=255"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
	SyncConfig *types.SyncConfig      `json:"sync_config,omitempty" validate:"omitempty,dive"`
}

// UnmarshalJSON custom unmarshaling to handle flat metadata structure
func (req *UpdateConnectionRequest) UnmarshalJSON(data []byte) error {
	// First, unmarshal to a temporary struct to get the raw data
	var temp struct {
		Name       string                 `json:"name"`
		Metadata   map[string]interface{} `json:"metadata,omitempty"`
		SyncConfig *types.SyncConfig      `json:"sync_config,omitempty"`
	}

	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}

	// Set the basic fields
	req.Name = temp.Name
	req.Metadata = temp.Metadata
	req.SyncConfig = temp.SyncConfig

	return nil
}

// ConnectionResponse represents the response for connection operations
type ConnectionResponse struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	ProviderType  types.SecretProvider   `json:"provider_type"`
	EnvironmentID string                 `json:"environment_id"`
	TenantID      string                 `json:"tenant_id"`
	Status        types.Status           `json:"status"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	SyncConfig    *types.SyncConfig      `json:"sync_config,omitempty"`
	CreatedAt     string                 `json:"created_at"`
	UpdatedAt     string                 `json:"updated_at"`
	CreatedBy     string                 `json:"created_by"`
	UpdatedBy     string                 `json:"updated_by"`
}

// ListConnectionsResponse represents the response for listing connections
type ListConnectionsResponse struct {
	Connections []ConnectionResponse `json:"connections"`
	Total       int                  `json:"total"`
	Limit       int                  `json:"limit"`
	Offset      int                  `json:"offset"`
}

// ToConnection converts CreateConnectionRequest to domain Connection
func (req *CreateConnectionRequest) ToConnection() *connection.Connection {
	return &connection.Connection{
		Name:                req.Name,
		ProviderType:        req.ProviderType,
		EncryptedSecretData: req.EncryptedSecretData,
		Metadata:            req.Metadata,
		SyncConfig:          req.SyncConfig,
	}
}

// ToConnectionResponse converts domain Connection to ConnectionResponse
func ToConnectionResponse(conn *connection.Connection) *ConnectionResponse {
	if conn == nil {
		return nil
	}

	return &ConnectionResponse{
		ID:            conn.ID,
		Name:          conn.Name,
		ProviderType:  conn.ProviderType,
		EnvironmentID: conn.EnvironmentID,
		TenantID:      conn.TenantID,
		Status:        conn.Status,
		Metadata:      conn.Metadata,
		SyncConfig:    conn.GetSyncConfig(),
		CreatedAt:     conn.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:     conn.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		CreatedBy:     conn.CreatedBy,
		UpdatedBy:     conn.UpdatedBy,
	}
}

// ToConnectionResponses converts multiple domain Connections to ConnectionResponses
func ToConnectionResponses(connections []*connection.Connection) []ConnectionResponse {
	var responses []ConnectionResponse
	for _, conn := range connections {
		if resp := ToConnectionResponse(conn); resp != nil {
			responses = append(responses, *resp)
		}
	}
	return responses
}
