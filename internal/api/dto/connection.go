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

	case types.SecretProviderS3:
		s3Metadata := &types.S3ConnectionMetadata{}

		if accessKey, ok := flatMetadata["aws_access_key_id"].(string); ok {
			s3Metadata.AWSAccessKeyID = accessKey
		}
		if secretKey, ok := flatMetadata["aws_secret_access_key"].(string); ok {
			s3Metadata.AWSSecretAccessKey = secretKey
		}
		if sessionToken, ok := flatMetadata["aws_session_token"].(string); ok {
			s3Metadata.AWSSessionToken = sessionToken
		}

		return types.ConnectionMetadata{
			S3: s3Metadata,
		}

	case types.SecretProviderHubSpot:
		hubspotMetadata := &types.HubSpotConnectionMetadata{}

		if accessToken, ok := flatMetadata["access_token"].(string); ok {
			hubspotMetadata.AccessToken = accessToken
		}
		if clientSecret, ok := flatMetadata["client_secret"].(string); ok {
			hubspotMetadata.ClientSecret = clientSecret
		}
		if appID, ok := flatMetadata["app_id"].(string); ok {
			hubspotMetadata.AppID = appID
		}

		return types.ConnectionMetadata{
			HubSpot: hubspotMetadata,
		}

	case types.SecretProviderRazorpay:
		razorpayMetadata := &types.RazorpayConnectionMetadata{}

		if keyID, ok := flatMetadata["key_id"].(string); ok {
			razorpayMetadata.KeyID = keyID
		}
		if secretKey, ok := flatMetadata["secret_key"].(string); ok {
			razorpayMetadata.SecretKey = secretKey
		}
		if webhookSecret, ok := flatMetadata["webhook_secret"].(string); ok {
			razorpayMetadata.WebhookSecret = webhookSecret
		}

		return types.ConnectionMetadata{
			Razorpay: razorpayMetadata,
		}

	case types.SecretProviderChargebee:
		chargebeeMetadata := &types.ChargebeeConnectionMetadata{}

		if site, ok := flatMetadata["site"].(string); ok {
			chargebeeMetadata.Site = site
		}
		if apiKey, ok := flatMetadata["api_key"].(string); ok {
			chargebeeMetadata.APIKey = apiKey
		}
		if webhookSecret, ok := flatMetadata["webhook_secret"].(string); ok {
			chargebeeMetadata.WebhookSecret = webhookSecret
		}
		if webhookUsername, ok := flatMetadata["webhook_username"].(string); ok {
			chargebeeMetadata.WebhookUsername = webhookUsername
		}
		if webhookPassword, ok := flatMetadata["webhook_password"].(string); ok {
			chargebeeMetadata.WebhookPassword = webhookPassword
		}

		return types.ConnectionMetadata{
			Chargebee: chargebeeMetadata,
		}

	case types.SecretProviderQuickBooks:
		qbMetadata := &types.QuickBooksConnectionMetadata{}

		// Required fields
		if clientID, ok := flatMetadata["client_id"].(string); ok {
			qbMetadata.ClientID = clientID
		}
		if clientSecret, ok := flatMetadata["client_secret"].(string); ok {
			qbMetadata.ClientSecret = clientSecret
		}
		if realmID, ok := flatMetadata["realm_id"].(string); ok {
			qbMetadata.RealmID = realmID
		}
		if environment, ok := flatMetadata["environment"].(string); ok {
			qbMetadata.Environment = environment
		}

		// Required for initial token exchange (captured from OAuth redirect)
		if authCode, ok := flatMetadata["auth_code"].(string); ok {
			qbMetadata.AuthCode = authCode
		}
		if redirectURI, ok := flatMetadata["redirect_uri"].(string); ok {
			qbMetadata.RedirectURI = redirectURI
		}

		if accessToken, ok := flatMetadata["access_token"].(string); ok {
			qbMetadata.AccessToken = accessToken
		}
		if refreshToken, ok := flatMetadata["refresh_token"].(string); ok {
			qbMetadata.RefreshToken = refreshToken
		}

		// Optional config
		if incomeAccountID, ok := flatMetadata["income_account_id"].(string); ok {
			qbMetadata.IncomeAccountID = incomeAccountID
		}

		return types.ConnectionMetadata{
			QuickBooks: qbMetadata,
		}

	case types.SecretProviderNomod:
		nomodMetadata := &types.NomodConnectionMetadata{}

		if apiKey, ok := flatMetadata["api_key"].(string); ok {
			nomodMetadata.APIKey = apiKey
		}
		if webhookSecret, ok := flatMetadata["webhook_secret"].(string); ok {
			nomodMetadata.WebhookSecret = webhookSecret
		}

		return types.ConnectionMetadata{
			Nomod: nomodMetadata,
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
	Name                string                    `json:"name,omitempty" validate:"omitempty,max=255"`
	Metadata            map[string]interface{}    `json:"metadata,omitempty"`
	SyncConfig          *types.SyncConfig         `json:"sync_config,omitempty" validate:"omitempty,dive"`
	EncryptedSecretData *types.ConnectionMetadata `json:"encrypted_secret_data,omitempty"` // For updating webhook tokens, etc.
}

// UnmarshalJSON custom unmarshaling to handle flat metadata structure
func (req *UpdateConnectionRequest) UnmarshalJSON(data []byte) error {
	// First, unmarshal to a temporary struct to get the raw data
	var temp struct {
		Name                string                    `json:"name"`
		Metadata            map[string]interface{}    `json:"metadata,omitempty"`
		SyncConfig          *types.SyncConfig         `json:"sync_config,omitempty"`
		EncryptedSecretData *types.ConnectionMetadata `json:"encrypted_secret_data,omitempty"`
	}

	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}

	// Set the basic fields
	req.Name = temp.Name
	req.Metadata = temp.Metadata
	req.SyncConfig = temp.SyncConfig
	req.EncryptedSecretData = temp.EncryptedSecretData

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
