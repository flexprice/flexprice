package connection

import (
	"github.com/flexprice/flexprice/ent"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// Connection represents an integration connection in the system
type Connection struct {
	ID            string                 `db:"id" json:"id"`
	Name          string                 `db:"name" json:"name"`
	ProviderType  types.SecretProvider   `db:"provider_type" json:"provider_type"`
	Metadata      map[string]interface{} `db:"metadata" json:"metadata"`
	EnvironmentID string                 `db:"environment_id" json:"environment_id"`
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

	// Use the metadata directly since it should already be decrypted by the service layer
	metadata := c.Metadata

	config := &StripeConnection{}
	if pk, ok := metadata["publishable_key"].(string); ok {
		config.PublishableKey = pk
	}
	if sk, ok := metadata["secret_key"].(string); ok {
		config.SecretKey = sk
	}
	if ws, ok := metadata["webhook_secret"].(string); ok {
		config.WebhookSecret = ws
	}
	if aid, ok := metadata["account_id"].(string); ok {
		config.AccountID = aid
	}

	return config, nil
}

// FromEnt converts an ent.Connection to domain Connection
func FromEnt(entConn *ent.Connection) *Connection {
	if entConn == nil {
		return nil
	}

	return &Connection{
		ID:            entConn.ID,
		Name:          entConn.Name,
		ProviderType:  types.SecretProvider(entConn.ProviderType),
		Metadata:      entConn.Metadata,
		EnvironmentID: entConn.EnvironmentID,
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
