package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"

	"github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration/quickbooks"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// Handler handles QuickBooks webhook events
type Handler struct {
	client         quickbooks.QuickBooksClient
	paymentSvc     quickbooks.QuickBooksPaymentService
	connectionRepo connection.Repository
	logger         *logger.Logger
}

// NewHandler creates a new QuickBooks webhook handler
func NewHandler(
	client quickbooks.QuickBooksClient,
	paymentSvc quickbooks.QuickBooksPaymentService,
	connectionRepo connection.Repository,
	logger *logger.Logger,
) *Handler {
	return &Handler{
		client:         client,
		paymentSvc:     paymentSvc,
		connectionRepo: connectionRepo,
		logger:         logger,
	}
}

// VerifyWebhookSignature verifies the QuickBooks webhook signature
// QuickBooks uses HMAC-SHA256 with the verifier token as the key
// Reference: https://developer.intuit.com/app/developer/qbo/docs/develop/webhooks/manage-webhooks-notifications
func (h *Handler) VerifyWebhookSignature(ctx context.Context, payload []byte, signature string) error {
	// Get QuickBooks connection to retrieve verifier token
	conn, err := h.connectionRepo.GetByProvider(ctx, types.SecretProviderQuickBooks)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to get QuickBooks connection for webhook verification").
			Mark(ierr.ErrNotFound)
	}

	// Get the decrypted QuickBooks config (which includes webhook verifier token)
	qbConfig, err := h.client.GetDecryptedQuickBooksConfig(conn)
	if err != nil {
		h.logger.Errorw("failed to decrypt QuickBooks config",
			"error", err,
			"connection_id", conn.ID)
		return ierr.NewError("failed to get QuickBooks configuration").
			WithHint("Unable to decrypt QuickBooks connection credentials").
			Mark(ierr.ErrInternal)
	}

	// Check if webhook verifier token is configured
	if qbConfig.WebhookVerifierToken == "" {
		h.logger.Warnw("webhook verifier token not configured - SECURITY RISK, skipping signature verification",
			"connection_id", conn.ID,
			"note", "Configure webhook_verifier_token in QuickBooks connection for production security")
		return nil // Allow webhook without verification (for development)
	}

	// Compute HMAC-SHA256
	mac := hmac.New(sha256.New, []byte(qbConfig.WebhookVerifierToken))
	mac.Write(payload)
	expectedSignature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	// Compare signatures
	if !hmac.Equal([]byte(signature), []byte(expectedSignature)) {
		return ierr.NewError("invalid webhook signature").
			WithHint("The webhook signature does not match the expected value").
			Mark(ierr.ErrPermissionDenied)
	}

	h.logger.Infow("webhook signature verified successfully", "connection_id", conn.ID)

	return nil
}

// HandleWebhook processes QuickBooks webhook events
func (h *Handler) HandleWebhook(ctx context.Context, payload []byte, services *ServiceDependencies) error {
	// Parse webhook payload
	var webhookPayload QuickBooksWebhookPayload
	if err := json.Unmarshal(payload, &webhookPayload); err != nil {
		h.logger.Errorw("failed to parse webhook payload",
			"error", err)
		return ierr.WithError(err).
			WithHint("Invalid webhook payload format").
			Mark(ierr.ErrValidation)
	}

	// Process each event notification
	for _, notification := range webhookPayload.EventNotifications {
		realmID := notification.RealmID

		h.logger.Debugw("processing webhook notification",
			"realm_id", realmID,
			"entities_count", len(notification.DataChangeEvent.Entities))

		// Verify realm ID matches our connection
		conn, err := h.connectionRepo.GetByProvider(ctx, types.SecretProviderQuickBooks)
		if err != nil {
			h.logger.Errorw("failed to get QuickBooks connection",
				"error", err,
				"realm_id", realmID)
			continue
		}

		// Check if realm ID matches
		if conn.EncryptedSecretData.QuickBooks != nil {
			if conn.EncryptedSecretData.QuickBooks.RealmID != realmID {
				h.logger.Warnw("realm ID mismatch, skipping notification",
					"expected_realm_id", conn.EncryptedSecretData.QuickBooks.RealmID,
					"received_realm_id", realmID)
				continue
			}
		}

		// Check if payment sync is enabled (inbound)
		if !conn.IsPaymentInboundEnabled() {
			h.logger.Debugw("payment inbound sync disabled, skipping webhook",
				"connection_id", conn.ID)
			continue
		}

		// Process each entity change
		for _, entity := range notification.DataChangeEvent.Entities {
			if err := h.processEntityChange(ctx, &entity, services); err != nil {
				h.logger.Errorw("failed to process entity change",
					"error", err,
					"entity_name", entity.Name,
					"entity_id", entity.ID,
					"operation", entity.Operation)
				// Continue processing other entities
			}
		}
	}

	return nil
}

// processEntityChange processes a single entity change event
func (h *Handler) processEntityChange(ctx context.Context, entity *EntityChange, services *ServiceDependencies) error {
	h.logger.Debugw("processing entity change",
		"entity_name", entity.Name,
		"entity_id", entity.ID,
		"operation", entity.Operation)

	// Only process Payment events for now
	if !entity.IsPaymentEvent() {
		h.logger.Debugw("skipping non-payment entity",
			"entity_name", entity.Name)
		return nil
	}

	// Only process Create and Update operations
	if !entity.IsCreateOperation() && !entity.IsUpdateOperation() {
		h.logger.Debugw("skipping non-create/update operation",
			"operation", entity.Operation)
		return nil
	}

	// Cast services to proper types
	paymentService, ok := services.PaymentService.(interfaces.PaymentService)
	if !ok {
		return ierr.NewError("invalid payment service type").Mark(ierr.ErrInternal)
	}

	invoiceService, ok := services.InvoiceService.(interfaces.InvoiceService)
	if !ok {
		return ierr.NewError("invalid invoice service type").Mark(ierr.ErrInternal)
	}

	// Handle payment event
	return h.handlePaymentEvent(ctx, entity.ID, paymentService, invoiceService)
}

// handlePaymentEvent processes a Payment entity change
func (h *Handler) handlePaymentEvent(ctx context.Context, paymentID string, paymentService interfaces.PaymentService, invoiceService interfaces.InvoiceService) error {
	h.logger.Infow("handling QuickBooks payment event",
		"quickbooks_payment_id", paymentID)

	// Delegate to payment service
	return h.paymentSvc.HandleExternalPaymentFromWebhook(ctx, paymentID, paymentService, invoiceService)
}
