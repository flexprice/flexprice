package connection

import (
	"github.com/flexprice/flexprice/ent"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// Connection represents an integration connection in the system
type Connection struct {
	ID                  string                   `db:"id" json:"id"`
	Name                string                   `db:"name" json:"name"`
	ProviderType        types.SecretProvider     `db:"provider_type" json:"provider_type"`
	EncryptedSecretData types.ConnectionMetadata `db:"encrypted_secret_data" json:"encrypted_secret_data"`
	Metadata            map[string]interface{}   `db:"metadata" json:"metadata"`
	SyncConfig          *types.SyncConfig        `db:"sync_config" json:"sync_config,omitempty"`
	EnvironmentID       string                   `db:"environment_id" json:"environment_id"`
	types.BaseModel
}

// StripeConnection represents Stripe-specific connection metadata
type StripeConnection struct {
	PublishableKey string `json:"publishable_key"`
	SecretKey      string `json:"secret_key"`
	WebhookSecret  string `json:"webhook_secret"`
	AccountID      string `json:"account_id,omitempty"`
}

// GetStripeConfig extracts Stripe configuration from connection metadata
func (c *Connection) GetStripeConfig() (*StripeConnection, error) {
	if c.ProviderType != types.SecretProviderStripe {
		return nil, ierr.NewError("connection is not a Stripe connection").
			WithHint("Connection provider type must be Stripe").
			Mark(ierr.ErrValidation)
	}

	if c.EncryptedSecretData.Stripe == nil {
		return nil, ierr.NewError("stripe metadata is not configured").
			WithHint("Stripe metadata is required for Stripe connections").
			Mark(ierr.ErrValidation)
	}

	config := &StripeConnection{
		PublishableKey: c.EncryptedSecretData.Stripe.PublishableKey,
		SecretKey:      c.EncryptedSecretData.Stripe.SecretKey,
		WebhookSecret:  c.EncryptedSecretData.Stripe.WebhookSecret,
		AccountID:      c.EncryptedSecretData.Stripe.AccountID,
	}

	return config, nil
}

// IsInvoiceSyncEnabled checks if invoice sync is enabled for this connection
func (c *Connection) IsInvoiceSyncEnabled() bool {
	if c.Metadata == nil {
		return false // Default to false if metadata is not set
	}

	// Check if invoice_sync_enable is set to true
	if enable, ok := c.Metadata["invoice_sync_enable"].(bool); ok {
		return enable
	}

	return false // Default to false if not set or not a boolean
}

// convertMapToConnectionMetadata converts old map format to new structured format
func convertMapToConnectionMetadata(metadata map[string]interface{}, providerType types.SecretProvider) types.ConnectionMetadata {
	switch providerType {
	case types.SecretProviderStripe:
		stripeMetadata := &types.StripeConnectionMetadata{}
		if pk, ok := metadata["publishable_key"].(string); ok {
			stripeMetadata.PublishableKey = pk
		}
		if sk, ok := metadata["secret_key"].(string); ok {
			stripeMetadata.SecretKey = sk
		}
		if ws, ok := metadata["webhook_secret"].(string); ok {
			stripeMetadata.WebhookSecret = ws
		}
		if aid, ok := metadata["account_id"].(string); ok {
			stripeMetadata.AccountID = aid
		}
		return types.ConnectionMetadata{
			Stripe: stripeMetadata,
		}
	case types.SecretProviderS3:
		s3Metadata := &types.S3ConnectionMetadata{}
		if accessKey, ok := metadata["aws_access_key_id"].(string); ok {
			s3Metadata.AWSAccessKeyID = accessKey
		}
		if secretKey, ok := metadata["aws_secret_access_key"].(string); ok {
			s3Metadata.AWSSecretAccessKey = secretKey
		}
		if sessionToken, ok := metadata["aws_session_token"].(string); ok {
			s3Metadata.AWSSessionToken = sessionToken
		}
		return types.ConnectionMetadata{
			S3: s3Metadata,
		}
	case types.SecretProviderHubSpot:
		hubspotMetadata := &types.HubSpotConnectionMetadata{}
		if accessToken, ok := metadata["access_token"].(string); ok {
			hubspotMetadata.AccessToken = accessToken
		}
		if clientSecret, ok := metadata["client_secret"].(string); ok {
			hubspotMetadata.ClientSecret = clientSecret
		}
		if appID, ok := metadata["app_id"].(string); ok {
			hubspotMetadata.AppID = appID
		}
		return types.ConnectionMetadata{
			HubSpot: hubspotMetadata,
		}
	case types.SecretProviderRazorpay:
		razorpayMetadata := &types.RazorpayConnectionMetadata{}
		if keyID, ok := metadata["key_id"].(string); ok {
			razorpayMetadata.KeyID = keyID
		}
		if secretKey, ok := metadata["secret_key"].(string); ok {
			razorpayMetadata.SecretKey = secretKey
		}
		if webhookSecret, ok := metadata["webhook_secret"].(string); ok {
			razorpayMetadata.WebhookSecret = webhookSecret
		}
		return types.ConnectionMetadata{
			Razorpay: razorpayMetadata,
		}
	case types.SecretProviderChargebee:
		chargebeeMetadata := &types.ChargebeeConnectionMetadata{}
		if site, ok := metadata["site"].(string); ok {
			chargebeeMetadata.Site = site
		}
		if apiKey, ok := metadata["api_key"].(string); ok {
			chargebeeMetadata.APIKey = apiKey
		}
		if webhookSecret, ok := metadata["webhook_secret"].(string); ok {
			chargebeeMetadata.WebhookSecret = webhookSecret
		}
		if webhookUsername, ok := metadata["webhook_username"].(string); ok {
			chargebeeMetadata.WebhookUsername = webhookUsername
		}
		if webhookPassword, ok := metadata["webhook_password"].(string); ok {
			chargebeeMetadata.WebhookPassword = webhookPassword
		}
		return types.ConnectionMetadata{
			Chargebee: chargebeeMetadata,
		}
	default:
		// For other providers or unknown types, use generic format
		return types.ConnectionMetadata{
			Generic: &types.GenericConnectionMetadata{
				Data: metadata,
			},
		}
	}
}

// FromEnt converts an ent.Connection to domain Connection
func FromEnt(entConn *ent.Connection) *Connection {
	if entConn == nil {
		return nil
	}

	// Convert old map format to new structured format
	var metadata types.ConnectionMetadata
	if entConn.EncryptedSecretData != nil {
		metadata = convertMapToConnectionMetadata(entConn.EncryptedSecretData, types.SecretProvider(entConn.ProviderType))
	}

	return &Connection{
		ID:                  entConn.ID,
		Name:                entConn.Name,
		ProviderType:        types.SecretProvider(entConn.ProviderType),
		EncryptedSecretData: metadata,
		Metadata:            entConn.Metadata,
		SyncConfig:          entConn.SyncConfig,
		EnvironmentID:       entConn.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  entConn.TenantID,
			Status:    types.Status(entConn.Status),
			CreatedAt: entConn.CreatedAt,
			UpdatedAt: entConn.UpdatedAt,
			CreatedBy: entConn.CreatedBy,
			UpdatedBy: entConn.UpdatedBy,
		},
	}
}

// GetSyncConfig returns the sync config or default (all disabled) if not set
func (c *Connection) GetSyncConfig() *types.SyncConfig {
	if c.SyncConfig == nil {
		return types.DefaultSyncConfig()
	}
	return c.SyncConfig
}

// IsPlanInboundEnabled checks if plan inbound sync is enabled
func (c *Connection) IsPlanInboundEnabled() bool {
	config := c.GetSyncConfig()
	return config.Plan != nil && config.Plan.Inbound
}

// IsPlanOutboundEnabled checks if plan outbound sync is enabled
func (c *Connection) IsPlanOutboundEnabled() bool {
	return false
}

// IsSubscriptionInboundEnabled checks if subscription inbound sync is enabled
func (c *Connection) IsSubscriptionInboundEnabled() bool {
	config := c.GetSyncConfig()
	return config.Subscription != nil && config.Subscription.Inbound
}

// IsSubscriptionOutboundEnabled checks if subscription outbound sync is enabled
func (c *Connection) IsSubscriptionOutboundEnabled() bool {
	return false
}

// IsInvoiceInboundEnabled checks if invoice inbound sync is enabled
func (c *Connection) IsInvoiceInboundEnabled() bool {
	return false
}

// IsInvoiceOutboundEnabled checks if invoice outbound sync is enabled
func (c *Connection) IsInvoiceOutboundEnabled() bool {
	config := c.GetSyncConfig()
	return config.Invoice != nil && config.Invoice.Outbound
}

// IsDealInboundEnabled checks if deal inbound sync is enabled
func (c *Connection) IsDealInboundEnabled() bool {
	return false
}

// IsDealOutboundEnabled checks if deal outbound sync is enabled
func (c *Connection) IsDealOutboundEnabled() bool {
	config := c.GetSyncConfig()
	return config.Deal != nil && config.Deal.Outbound
}
