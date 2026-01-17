package moyasar

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/types"
)

// MoyasarClient defines the interface for Moyasar API operations
type MoyasarClient interface {
	GetMoyasarConfig(ctx context.Context) (*MoyasarConfig, error)
	GetDecryptedMoyasarConfig(conn *connection.Connection) (*MoyasarConfig, error)
	HasMoyasarConnection(ctx context.Context) bool
	GetConnection(ctx context.Context) (*connection.Connection, error)
	CreatePayment(ctx context.Context, req *CreatePaymentRequest) (*CreatePaymentResponse, error)
	GetPayment(ctx context.Context, paymentID string) (*MoyasarPayment, error)
	RefundPayment(ctx context.Context, paymentID string, amount int) (*RefundPaymentResponse, error)
	VoidPayment(ctx context.Context, paymentID string) (*MoyasarPayment, error)
	VerifyWebhookSignature(ctx context.Context, payload []byte, signature string) error
	// Token methods
	CreateToken(ctx context.Context, req *CreateTokenRequest) (*CreateTokenResponse, error)
	GetToken(ctx context.Context, tokenID string) (*MoyasarToken, error)
	ChargeWithToken(ctx context.Context, tokenID string, amount int, currency, description string, metadata map[string]string, givenID string) (*CreatePaymentResponse, error)
}

// Client handles Moyasar API client setup and configuration
type Client struct {
	connectionRepo    connection.Repository
	encryptionService security.EncryptionService
	logger            *logger.Logger
	httpClient        *http.Client
}

// NewClient creates a new Moyasar client
func NewClient(
	connectionRepo connection.Repository,
	encryptionService security.EncryptionService,
	logger *logger.Logger,
) MoyasarClient {
	return &Client{
		connectionRepo:    connectionRepo,
		encryptionService: encryptionService,
		logger:            logger,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// GetMoyasarConfig retrieves and decrypts Moyasar configuration for the current environment
func (c *Client) GetMoyasarConfig(ctx context.Context) (*MoyasarConfig, error) {
	// Get Moyasar connection for this environment
	conn, err := c.connectionRepo.GetByProvider(ctx, types.SecretProviderMoyasar)
	if err != nil {
		return nil, ierr.NewError("failed to get Moyasar connection").
			WithHint("Moyasar connection not configured for this environment").
			Mark(ierr.ErrNotFound)
	}

	moyasarConfig, err := c.GetDecryptedMoyasarConfig(conn)
	if err != nil {
		return nil, ierr.NewError("failed to get Moyasar configuration").
			WithHint("Invalid Moyasar configuration").
			Mark(ierr.ErrValidation)
	}

	// Validate required fields
	if moyasarConfig.SecretKey == "" {
		c.logger.Errorw("missing Moyasar secret key",
			"connection_id", conn.ID,
			"environment_id", conn.EnvironmentID)
		return nil, ierr.NewError("missing Moyasar secret key").
			WithHint("Configure Moyasar secret key in the connection settings").
			Mark(ierr.ErrValidation)
	}

	return moyasarConfig, nil
}

// GetDecryptedMoyasarConfig decrypts and returns Moyasar configuration
func (c *Client) GetDecryptedMoyasarConfig(conn *connection.Connection) (*MoyasarConfig, error) {
	// Check if the connection has encrypted secret data for Moyasar
	if conn.EncryptedSecretData.Moyasar == nil {
		c.logger.Warnw("no moyasar metadata found in encrypted secret data", "connection_id", conn.ID)
		return nil, ierr.NewError("no moyasar configuration found").
			WithHint("Moyasar credentials not configured").
			Mark(ierr.ErrNotFound)
	}

	// Decrypt each field
	secretKey, err := c.encryptionService.Decrypt(conn.EncryptedSecretData.Moyasar.SecretKey)
	if err != nil {
		c.logger.Errorw("failed to decrypt secret key", "connection_id", conn.ID, "error", err)
		return nil, ierr.NewError("failed to decrypt secret key").Mark(ierr.ErrInternal)
	}

	// Decrypt publishable key (optional)
	var publishableKey string
	if conn.EncryptedSecretData.Moyasar.PublishableKey != "" {
		publishableKey, err = c.encryptionService.Decrypt(conn.EncryptedSecretData.Moyasar.PublishableKey)
		if err != nil {
			c.logger.Warnw("failed to decrypt publishable key", "connection_id", conn.ID, "error", err)
			// Don't fail - publishable key is optional
			publishableKey = ""
		}
	}

	// Decrypt webhook secret (optional)
	var webhookSecret string
	if conn.EncryptedSecretData.Moyasar.WebhookSecret != "" {
		webhookSecret, err = c.encryptionService.Decrypt(conn.EncryptedSecretData.Moyasar.WebhookSecret)
		if err != nil {
			c.logger.Warnw("failed to decrypt webhook secret", "connection_id", conn.ID, "error", err)
			// Don't fail - webhook secret is optional
			webhookSecret = ""
		}
	}

	moyasarConfig := &MoyasarConfig{
		PublishableKey: publishableKey,
		SecretKey:      secretKey,
		WebhookSecret:  webhookSecret,
	}

	c.logger.Infow("successfully decrypted moyasar credentials",
		"connection_id", conn.ID,
		"has_publishable_key", publishableKey != "",
		"has_secret_key", secretKey != "",
		"has_webhook_secret", webhookSecret != "")

	return moyasarConfig, nil
}

// HasMoyasarConnection checks if the tenant has a Moyasar connection available
func (c *Client) HasMoyasarConnection(ctx context.Context) bool {
	conn, err := c.connectionRepo.GetByProvider(ctx, types.SecretProviderMoyasar)
	return err == nil && conn != nil && conn.Status == types.StatusPublished
}

// GetConnection retrieves the Moyasar connection for the current context
func (c *Client) GetConnection(ctx context.Context) (*connection.Connection, error) {
	conn, err := c.connectionRepo.GetByProvider(ctx, types.SecretProviderMoyasar)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get Moyasar connection").
			Mark(ierr.ErrDatabase)
	}
	if conn == nil {
		return nil, ierr.NewError("Moyasar connection not found").
			WithHint("Moyasar connection not configured for this environment").
			Mark(ierr.ErrNotFound)
	}
	return conn, nil
}

// CreatePayment creates a payment in Moyasar
func (c *Client) CreatePayment(ctx context.Context, req *CreatePaymentRequest) (*CreatePaymentResponse, error) {
	config, err := c.GetMoyasarConfig(ctx)
	if err != nil {
		return nil, err
	}

	// Marshal request body
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, ierr.NewError("failed to marshal payment request").
			WithHint("Invalid payment request data").
			Mark(ierr.ErrInternal)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, BaseURL+"/payments", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, ierr.NewError("failed to create HTTP request").Mark(ierr.ErrInternal)
	}

	// Set headers - Moyasar uses HTTP Basic Auth with secret key as username
	httpReq.SetBasicAuth(config.SecretKey, "")
	httpReq.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		c.logger.Errorw("failed to create payment in Moyasar", "error", err)
		return nil, ierr.NewError("failed to create payment in Moyasar").
			WithHint("Unable to connect to Moyasar API").
			Mark(ierr.ErrInternal)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, ierr.NewError("failed to read Moyasar response").Mark(ierr.ErrInternal)
	}

	// Handle non-2xx responses
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errResp ErrorResponse
		if err := json.Unmarshal(respBody, &errResp); err == nil {
			c.logger.Errorw("Moyasar API error", "status", resp.StatusCode, "message", errResp.Message, "type", errResp.Type)
			return nil, ierr.NewError(errResp.Message).
				WithHint("Moyasar payment creation failed").
				WithReportableDetails(map[string]interface{}{
					"type":   errResp.Type,
					"errors": errResp.Errors,
				}).
				Mark(ierr.ErrInternal)
		}
		return nil, ierr.NewError("Moyasar API error").
			WithHint(fmt.Sprintf("HTTP status %d", resp.StatusCode)).
			Mark(ierr.ErrInternal)
	}

	// Parse successful response
	var payment CreatePaymentResponse
	if err := json.Unmarshal(respBody, &payment); err != nil {
		return nil, ierr.NewError("failed to parse Moyasar response").Mark(ierr.ErrInternal)
	}

	c.logger.Infow("successfully created payment in Moyasar",
		"payment_id", payment.ID,
		"status", payment.Status,
		"amount", payment.Amount)

	return &payment, nil
}

// GetPayment retrieves a payment from Moyasar by ID
func (c *Client) GetPayment(ctx context.Context, paymentID string) (*MoyasarPayment, error) {
	config, err := c.GetMoyasarConfig(ctx)
	if err != nil {
		return nil, err
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, BaseURL+"/payments/"+paymentID, nil)
	if err != nil {
		return nil, ierr.NewError("failed to create HTTP request").Mark(ierr.ErrInternal)
	}

	// Set headers
	httpReq.SetBasicAuth(config.SecretKey, "")
	httpReq.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		c.logger.Errorw("failed to get payment from Moyasar", "error", err, "payment_id", paymentID)
		return nil, ierr.NewError("failed to get payment from Moyasar").
			WithHint("Unable to connect to Moyasar API").
			Mark(ierr.ErrInternal)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, ierr.NewError("failed to read Moyasar response").Mark(ierr.ErrInternal)
	}

	// Handle non-2xx responses
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == 404 {
			return nil, ierr.NewError("payment not found").
				WithHint(fmt.Sprintf("Payment %s not found in Moyasar", paymentID)).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.NewError("Moyasar API error").
			WithHint(fmt.Sprintf("HTTP status %d", resp.StatusCode)).
			Mark(ierr.ErrInternal)
	}

	// Parse successful response
	var payment MoyasarPayment
	if err := json.Unmarshal(respBody, &payment); err != nil {
		return nil, ierr.NewError("failed to parse Moyasar response").Mark(ierr.ErrInternal)
	}

	c.logger.Infow("successfully fetched payment from Moyasar",
		"payment_id", payment.ID,
		"status", payment.Status)

	return &payment, nil
}

// RefundPayment refunds a payment in Moyasar
func (c *Client) RefundPayment(ctx context.Context, paymentID string, amount int) (*RefundPaymentResponse, error) {
	config, err := c.GetMoyasarConfig(ctx)
	if err != nil {
		return nil, err
	}

	// Build request body
	reqBody := map[string]interface{}{}
	if amount > 0 {
		reqBody["amount"] = amount
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, ierr.NewError("failed to marshal refund request").Mark(ierr.ErrInternal)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, BaseURL+"/payments/"+paymentID+"/refund", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, ierr.NewError("failed to create HTTP request").Mark(ierr.ErrInternal)
	}

	// Set headers
	httpReq.SetBasicAuth(config.SecretKey, "")
	httpReq.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		c.logger.Errorw("failed to refund payment in Moyasar", "error", err, "payment_id", paymentID)
		return nil, ierr.NewError("failed to refund payment in Moyasar").
			WithHint("Unable to connect to Moyasar API").
			Mark(ierr.ErrInternal)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, ierr.NewError("failed to read Moyasar response").Mark(ierr.ErrInternal)
	}

	// Handle non-2xx responses
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errResp ErrorResponse
		if err := json.Unmarshal(respBody, &errResp); err == nil {
			return nil, ierr.NewError(errResp.Message).
				WithHint("Moyasar refund failed").
				Mark(ierr.ErrInternal)
		}
		return nil, ierr.NewError("Moyasar API error").
			WithHint(fmt.Sprintf("HTTP status %d", resp.StatusCode)).
			Mark(ierr.ErrInternal)
	}

	// Parse successful response
	var refund RefundPaymentResponse
	if err := json.Unmarshal(respBody, &refund); err != nil {
		return nil, ierr.NewError("failed to parse Moyasar response").Mark(ierr.ErrInternal)
	}

	c.logger.Infow("successfully refunded payment in Moyasar",
		"payment_id", paymentID,
		"refund_id", refund.ID,
		"amount", refund.Amount)

	return &refund, nil
}

// VoidPayment voids an authorized but uncaptured payment in Moyasar
// This is used for cancelling SetupIntent or authorized payments
func (c *Client) VoidPayment(ctx context.Context, paymentID string) (*MoyasarPayment, error) {
	config, err := c.GetMoyasarConfig(ctx)
	if err != nil {
		return nil, err
	}

	// Create HTTP request to void the payment
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, BaseURL+"/payments/"+paymentID+"/void", nil)
	if err != nil {
		return nil, ierr.NewError("failed to create HTTP request").Mark(ierr.ErrInternal)
	}

	// Set headers
	httpReq.SetBasicAuth(config.SecretKey, "")
	httpReq.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		c.logger.Errorw("failed to void payment in Moyasar", "error", err, "payment_id", paymentID)
		return nil, ierr.NewError("failed to void payment in Moyasar").
			WithHint("Unable to connect to Moyasar API").
			Mark(ierr.ErrInternal)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, ierr.NewError("failed to read Moyasar response").Mark(ierr.ErrInternal)
	}

	// Handle non-2xx responses
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errResp ErrorResponse
		if err := json.Unmarshal(respBody, &errResp); err == nil {
			return nil, ierr.NewError(errResp.Message).
				WithHint("Moyasar void failed").
				Mark(ierr.ErrInternal)
		}
		return nil, ierr.NewError("Moyasar API error").
			WithHint(fmt.Sprintf("HTTP status %d", resp.StatusCode)).
			Mark(ierr.ErrInternal)
	}

	// Parse successful response
	var payment MoyasarPayment
	if err := json.Unmarshal(respBody, &payment); err != nil {
		return nil, ierr.NewError("failed to parse Moyasar response").Mark(ierr.ErrInternal)
	}

	c.logger.Infow("successfully voided payment in Moyasar",
		"payment_id", paymentID,
		"status", payment.Status)

	return &payment, nil
}

// VerifyWebhookSignature verifies the Moyasar webhook signature
func (c *Client) VerifyWebhookSignature(ctx context.Context, payload []byte, signature string) error {
	config, err := c.GetMoyasarConfig(ctx)
	if err != nil {
		c.logger.Errorw("failed to get Moyasar config for signature verification", "error", err)
		return ierr.NewError("failed to verify webhook signature").
			WithHint("Unable to verify Moyasar webhook signature").
			Mark(ierr.ErrInternal)
	}

	// Use webhook secret for verification
	secretForVerification := config.WebhookSecret
	if secretForVerification == "" {
		c.logger.Errorw("webhook secret not configured")
		return ierr.NewError("webhook secret not configured").
			WithHint("Configure Moyasar webhook secret").
			Mark(ierr.ErrValidation)
	}

	// Decode the received signature from hex
	decodedSignature, err := hex.DecodeString(signature)
	if err != nil {
		c.logger.Errorw("failed to decode webhook signature",
			"error", err,
			"signature", signature)
		return ierr.NewError("invalid webhook signature format").
			WithHint("Signature must be a valid hex string").
			Mark(ierr.ErrValidation)
	}

	// Calculate expected HMAC
	mac := hmac.New(sha256.New, []byte(secretForVerification))
	mac.Write(payload)
	expectedMAC := mac.Sum(nil)

	// Use constant-time comparison to prevent timing attacks
	if !hmac.Equal(expectedMAC, decodedSignature) {
		c.logger.Errorw("webhook signature mismatch",
			"expected_mac_length", len(expectedMAC),
			"received_signature_length", len(decodedSignature),
			"payload_length", len(payload),
			"using_webhook_secret", config.WebhookSecret != "")
		return ierr.NewError("webhook signature verification failed").
			WithHint("Invalid webhook signature").
			Mark(ierr.ErrValidation)
	}

	c.logger.Infow("webhook signature verified successfully",
		"using_webhook_secret", config.WebhookSecret != "")
	return nil
}

// ============================================================================
// Token Methods
// ============================================================================

// CreateToken creates a token for a card in Moyasar
func (c *Client) CreateToken(ctx context.Context, req *CreateTokenRequest) (*CreateTokenResponse, error) {
	config, err := c.GetMoyasarConfig(ctx)
	if err != nil {
		return nil, err
	}

	// Marshal request body
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, ierr.NewError("failed to marshal token request").
			WithHint("Invalid token request data").
			Mark(ierr.ErrInternal)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, BaseURL+"/tokens", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, ierr.NewError("failed to create HTTP request").Mark(ierr.ErrInternal)
	}

	// Set headers - always use secret key for server-side token creation
	httpReq.SetBasicAuth(config.SecretKey, "")
	httpReq.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		c.logger.Errorw("failed to create token in Moyasar", "error", err)
		return nil, ierr.NewError("failed to create token in Moyasar").
			WithHint("Unable to connect to Moyasar API").
			Mark(ierr.ErrInternal)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, ierr.NewError("failed to read Moyasar response").Mark(ierr.ErrInternal)
	}

	// Handle non-2xx responses
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errResp ErrorResponse
		if err := json.Unmarshal(respBody, &errResp); err == nil {
			c.logger.Errorw("Moyasar API error", "status", resp.StatusCode, "message", errResp.Message)
			return nil, ierr.NewError(errResp.Message).
				WithHint("Moyasar token creation failed").
				Mark(ierr.ErrInternal)
		}
		return nil, ierr.NewError("Moyasar API error").
			WithHint(fmt.Sprintf("HTTP status %d", resp.StatusCode)).
			Mark(ierr.ErrInternal)
	}

	// Parse successful response
	var token CreateTokenResponse
	if err := json.Unmarshal(respBody, &token); err != nil {
		return nil, ierr.NewError("failed to parse Moyasar response").Mark(ierr.ErrInternal)
	}

	c.logger.Infow("successfully created token in Moyasar",
		"token_id", token.ID,
		"status", token.Status,
		"brand", token.Brand)

	return &token, nil
}

// GetToken retrieves a token from Moyasar by ID
func (c *Client) GetToken(ctx context.Context, tokenID string) (*MoyasarToken, error) {
	config, err := c.GetMoyasarConfig(ctx)
	if err != nil {
		return nil, err
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, BaseURL+"/tokens/"+tokenID, nil)
	if err != nil {
		return nil, ierr.NewError("failed to create HTTP request").Mark(ierr.ErrInternal)
	}

	// Set headers
	httpReq.SetBasicAuth(config.SecretKey, "")
	httpReq.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		c.logger.Errorw("failed to get token from Moyasar", "error", err, "token_id", tokenID)
		return nil, ierr.NewError("failed to get token from Moyasar").
			WithHint("Unable to connect to Moyasar API").
			Mark(ierr.ErrInternal)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, ierr.NewError("failed to read Moyasar response").Mark(ierr.ErrInternal)
	}

	// Handle non-2xx responses
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == 404 {
			return nil, ierr.NewError("token not found").
				WithHint(fmt.Sprintf("Token %s not found in Moyasar", tokenID)).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.NewError("Moyasar API error").
			WithHint(fmt.Sprintf("HTTP status %d", resp.StatusCode)).
			Mark(ierr.ErrInternal)
	}

	// Parse successful response
	var token MoyasarToken
	if err := json.Unmarshal(respBody, &token); err != nil {
		return nil, ierr.NewError("failed to parse Moyasar response").Mark(ierr.ErrInternal)
	}

	c.logger.Infow("successfully fetched token from Moyasar",
		"token_id", token.ID,
		"status", token.Status)

	return &token, nil
}

// ChargeWithToken charges a payment using a saved token
func (c *Client) ChargeWithToken(ctx context.Context, tokenID string, amount int, currency, description string, metadata map[string]string, givenID string) (*CreatePaymentResponse, error) {
	// Build payment request with token source
	req := &CreatePaymentRequest{
		Amount:      amount,
		Currency:    currency,
		Description: description,
		Metadata:    metadata,
		GivenID:     givenID,
		Source: &PaymentSource{
			Type:  PaymentSourceTypeToken,
			Token: tokenID,
		},
	}

	c.logger.Infow("charging payment with token",
		"token_id", tokenID,
		"amount", amount,
		"currency", currency)

	return c.CreatePayment(ctx, req)
}
