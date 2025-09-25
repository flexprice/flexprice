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
