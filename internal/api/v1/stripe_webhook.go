package v1

import (
	"context"
	"io"
	"net/http"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/flexprice/flexprice/internal/webhook/handler"
	"github.com/gin-gonic/gin"
)

// StripeWebhookHandler handles HTTP requests for Stripe webhooks
type StripeWebhookHandler struct {
	stripeHandler handler.StripeWebhookHandler
	logger        *logger.Logger
}

// NewStripeWebhookHandler creates a new Stripe webhook HTTP handler
func NewStripeWebhookHandler(
	stripeHandler handler.StripeWebhookHandler,
	logger *logger.Logger,
) *StripeWebhookHandler {
	return &StripeWebhookHandler{
		stripeHandler: stripeHandler,
		logger:        logger,
	}
}

// ReceiveWebhook handles incoming Stripe webhook requests
// @Summary Receive Stripe webhook
// @Description Receives and processes Stripe webhook events, specifically customer.created events
// @Tags webhooks
// @Accept application/json
// @Produce application/json
// @Param Stripe-Signature header string true "Stripe webhook signature for verification"
// @Param payload body object true "Stripe webhook event payload"
// @Success 200 {object} webhookDto.StripeWebhookResponse "Webhook processed successfully"
// @Failure 400 {object} webhookDto.StripeWebhookResponse "Bad request - invalid payload or signature"
// @Failure 401 {object} webhookDto.StripeWebhookResponse "Unauthorized - invalid signature"
// @Failure 403 {object} webhookDto.StripeWebhookResponse "Forbidden - webhook not configured"
// @Failure 500 {object} webhookDto.StripeWebhookResponse "Internal server error"
// @Router /webhooks/stripe/{tenant_id}/{environment_id} [post]
func (h *StripeWebhookHandler) ReceiveWebhook(c *gin.Context) {
	// Extract tenant & environment from path params if provided
	tenantID := c.Param("tenant_id")
	environmentID := c.Param("environment_id")

	ctx := c.Request.Context()
	if tenantID != "" {
		ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
	}
	if environmentID != "" {
		ctx = context.WithValue(ctx, types.CtxEnvironmentID, environmentID)
	}

	// Get the signature from headers
	signature := c.GetHeader("Stripe-Signature")
	if signature == "" {
		h.logger.Errorw("missing Stripe-Signature header",
			"remote_addr", c.ClientIP(),
			"user_agent", c.GetHeader("User-Agent"),
		)
		response := webhookDto.NewStripeWebhookErrorResponse("Missing Stripe-Signature header")
		c.JSON(http.StatusBadRequest, response)
		return
	}

	// Read the raw body
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		h.logger.Errorw("failed to read webhook body",
			"error", err,
			"remote_addr", c.ClientIP(),
		)
		response := webhookDto.NewStripeWebhookErrorResponse("Failed to read request body")
		c.JSON(http.StatusBadRequest, response)
		return
	}

	// Log the webhook request (but not the full payload for security)
	h.logger.Infow("received Stripe webhook request",
		"content_length", len(rawBody),
		"content_type", c.GetHeader("Content-Type"),
		"remote_addr", c.ClientIP(),
		"user_agent", c.GetHeader("User-Agent"),
	)

	// Validate minimum payload size
	if len(rawBody) == 0 {
		h.logger.Errorw("empty webhook payload",
			"remote_addr", c.ClientIP(),
		)
		response := webhookDto.NewStripeWebhookErrorResponse("Empty webhook payload")
		c.JSON(http.StatusBadRequest, response)
		return
	}

	// Validate maximum payload size (Stripe typically sends < 64KB)
	const maxPayloadSize = 64 * 1024 // 64KB
	if len(rawBody) > maxPayloadSize {
		h.logger.Errorw("webhook payload too large",
			"payload_size", len(rawBody),
			"max_size", maxPayloadSize,
			"remote_addr", c.ClientIP(),
		)
		response := webhookDto.NewStripeWebhookErrorResponse("Webhook payload too large")
		c.JSON(http.StatusBadRequest, response)
		return
	}

	// Process the webhook using the business logic handler
	response, err := h.stripeHandler.ProcessWebhook(ctx, rawBody, signature)
	if err != nil {
		// Log the error but don't expose internal details
		h.logger.Errorw("webhook processing failed",
			"error", err,
			"payload_size", len(rawBody),
			"remote_addr", c.ClientIP(),
		)

		// Return appropriate HTTP status based on error type
		statusCode := h.getStatusCodeFromError(err)

		// If we don't have a response, create a generic error response
		if response == nil {
			response = webhookDto.NewStripeWebhookErrorResponse("Webhook processing failed")
		}

		c.JSON(statusCode, response)
		return
	}

	// Success response
	h.logger.Infow("webhook processed successfully",
		"payload_size", len(rawBody),
		"remote_addr", c.ClientIP(),
	)

	c.JSON(http.StatusOK, response)
}

// getStatusCodeFromError determines the appropriate HTTP status code based on the error type
func (h *StripeWebhookHandler) getStatusCodeFromError(err error) int {
	// This could be enhanced to check specific error types
	// For now, using a simple heuristic based on error message
	errorMsg := err.Error()

	switch {
	case contains(errorMsg, "signature"):
		return http.StatusUnauthorized
	case contains(errorMsg, "not configured"), contains(errorMsg, "permission"):
		return http.StatusForbidden
	case contains(errorMsg, "validation"), contains(errorMsg, "invalid"), contains(errorMsg, "parse"):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

// contains checks if a string contains a substring (case-insensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			(len(s) > len(substr) &&
				anySubstring(s, substr)))
}

// anySubstring checks if substr exists anywhere in s
func anySubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestWebhook provides a test endpoint for webhook validation during development
// @Summary Test Stripe webhook endpoint
// @Description Test endpoint to validate webhook configuration and connectivity
// @Tags webhooks
// @Accept application/json
// @Produce application/json
// @Success 200 {object} map[string]interface{} "Test successful"
// @Router /webhooks/stripe/test [get]
func (h *StripeWebhookHandler) TestWebhook(c *gin.Context) {
	response := map[string]interface{}{
		"status":    "ok",
		"message":   "Stripe webhook endpoint is ready",
		"timestamp": c.Request.Header.Get("X-Request-ID"),
	}

	h.logger.Infow("webhook test endpoint accessed",
		"remote_addr", c.ClientIP(),
		"user_agent", c.GetHeader("User-Agent"),
	)

	c.JSON(http.StatusOK, response)
}
