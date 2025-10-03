package stripe

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stripe/stripe-go/v82"
)

// Client handles Stripe API client setup and configuration
type Client struct {
	connectionRepo    connection.Repository
	encryptionService security.EncryptionService
	logger            *logger.Logger
}

// NewClient creates a new Stripe client
func NewClient(
	connectionRepo connection.Repository,
	encryptionService security.EncryptionService,
	logger *logger.Logger,
) *Client {
	return &Client{
		connectionRepo:    connectionRepo,
		encryptionService: encryptionService,
		logger:            logger,
	}
}

// StripeConfig holds decrypted Stripe configuration
type StripeConfig struct {
	SecretKey      string
	PublishableKey string
	WebhookSecret  string
}

// GetStripeClient returns a configured Stripe client for the current environment
func (c *Client) GetStripeClient(ctx context.Context) (*stripe.Client, *StripeConfig, error) {
	// Get Stripe connection for this environment
	conn, err := c.connectionRepo.GetByProvider(ctx, types.SecretProviderStripe)
	if err != nil {
		return nil, nil, ierr.NewError("failed to get Stripe connection").
			WithHint("Stripe connection not configured for this environment").
			Mark(ierr.ErrNotFound)
	}

	stripeConfig, err := c.GetDecryptedStripeConfig(conn)
	if err != nil {
		return nil, nil, ierr.NewError("failed to get Stripe configuration").
			WithHint("Invalid Stripe configuration").
			Mark(ierr.ErrValidation)
	}

	// Initialize Stripe client
	stripeClient := stripe.NewClient(stripeConfig.SecretKey, nil)

	return stripeClient, stripeConfig, nil
}

// GetDecryptedStripeConfig decrypts and returns Stripe configuration
func (c *Client) GetDecryptedStripeConfig(conn *connection.Connection) (*StripeConfig, error) {
	// Decrypt the connection metadata if it's encrypted
	decryptedMetadata, err := c.decryptConnectionMetadata(conn)
	if err != nil {
		return nil, err
	}

	// Extract Stripe configuration from decrypted metadata
	stripeConfig := &StripeConfig{}

	if secretKey, exists := decryptedMetadata["secret_key"]; exists {
		stripeConfig.SecretKey = secretKey
	}

	if publishableKey, exists := decryptedMetadata["publishable_key"]; exists {
		stripeConfig.PublishableKey = publishableKey
	}

	if webhookSecret, exists := decryptedMetadata["webhook_secret"]; exists {
		stripeConfig.WebhookSecret = webhookSecret
		c.logger.Infow("webhook secret found in decrypted metadata",
			"has_webhook_secret", webhookSecret != "",
			"webhook_secret_length", len(webhookSecret))
	} else {
		c.logger.Warnw("webhook_secret not found in decrypted metadata",
			"available_keys", lo.Keys(decryptedMetadata))
	}

	c.logger.Infow("final stripe config",
		"has_secret_key", stripeConfig.SecretKey != "",
		"has_publishable_key", stripeConfig.PublishableKey != "",
		"has_webhook_secret", stripeConfig.WebhookSecret != "")

	return stripeConfig, nil
}

// decryptConnectionMetadata decrypts the connection encrypted secret data if it's encrypted
func (c *Client) decryptConnectionMetadata(conn *connection.Connection) (types.Metadata, error) {
	c.logger.Infow("decrypting connection metadata",
		"connection_id", conn.ID,
		"has_encrypted_secret_data", conn.EncryptedSecretData.Stripe != nil || conn.EncryptedSecretData.Generic != nil,
		"metadata_keys", lo.Keys(conn.Metadata))

	// Check if the connection has encrypted secret data
	if conn.EncryptedSecretData.Stripe == nil && conn.EncryptedSecretData.Generic == nil {
		c.logger.Warnw("no encrypted secret data found", "connection_id", conn.ID)
		return types.Metadata{}, nil
	}

	// For Stripe connections, decrypt the structured metadata
	if conn.ProviderType == types.SecretProviderStripe {
		if conn.EncryptedSecretData.Stripe == nil {
			c.logger.Warnw("no stripe metadata found in encrypted secret data", "connection_id", conn.ID)
			return types.Metadata{}, nil
		}

		// Decrypt each field
		secretKey, err := c.encryptionService.Decrypt(conn.EncryptedSecretData.Stripe.SecretKey)
		if err != nil {
			c.logger.Errorw("failed to decrypt secret key", "connection_id", conn.ID, "error", err)
			return nil, ierr.NewError("failed to decrypt secret key").Mark(ierr.ErrInternal)
		}

		publishableKey, err := c.encryptionService.Decrypt(conn.EncryptedSecretData.Stripe.PublishableKey)
		if err != nil {
			c.logger.Errorw("failed to decrypt publishable key", "connection_id", conn.ID, "error", err)
			return nil, ierr.NewError("failed to decrypt publishable key").Mark(ierr.ErrInternal)
		}

		webhookSecret, err := c.encryptionService.Decrypt(conn.EncryptedSecretData.Stripe.WebhookSecret)
		if err != nil {
			c.logger.Errorw("failed to decrypt webhook secret", "connection_id", conn.ID, "error", err)
			return nil, ierr.NewError("failed to decrypt webhook secret").Mark(ierr.ErrInternal)
		}

		decryptedMetadata := types.Metadata{
			"secret_key":      secretKey,
			"publishable_key": publishableKey,
			"webhook_secret":  webhookSecret,
			"account_id":      conn.EncryptedSecretData.Stripe.AccountID,
		}

		c.logger.Infow("successfully decrypted connection metadata",
			"connection_id", conn.ID,
			"decrypted_keys", lo.Keys(decryptedMetadata),
			"has_webhook_secret", webhookSecret != "")

		// Merge with existing non-encrypted metadata
		merged := make(types.Metadata)
		if conn.Metadata != nil {
			for k, v := range conn.Metadata {
				if vStr, ok := v.(string); ok {
					merged[k] = vStr
				} else {
					merged[k] = fmt.Sprintf("%v", v)
				}
			}
		}
		for k, v := range decryptedMetadata {
			merged[k] = v
		}

		return merged, nil
	}
	// If no encrypted data, return the metadata as-is
	if conn.Metadata != nil {
		metadata := make(types.Metadata)
		for k, v := range conn.Metadata {
			if vStr, ok := v.(string); ok {
				metadata[k] = vStr
			} else {
				metadata[k] = fmt.Sprintf("%v", v)
			}
		}
		return metadata, nil
	}
	return types.Metadata{}, nil
}

// HasStripeConnection checks if the tenant has a Stripe connection available
func (c *Client) HasStripeConnection(ctx context.Context) bool {
	conn, err := c.connectionRepo.GetByProvider(ctx, types.SecretProviderStripe)
	return err == nil && conn != nil && conn.Status == types.StatusPublished
}
