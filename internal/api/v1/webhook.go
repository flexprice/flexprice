package v1

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/svix"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stripe/stripe-go/v79"
)

// WebhookHandler handles webhook-related endpoints
type WebhookHandler struct {
	config        *config.Configuration
	svixClient    *svix.Client
	logger        *logger.Logger
	stripeService *service.StripeService
}

// NewWebhookHandler creates a new webhook handler
func NewWebhookHandler(
	cfg *config.Configuration,
	svixClient *svix.Client,
	logger *logger.Logger,
	stripeService *service.StripeService,
) *WebhookHandler {
	return &WebhookHandler{
		config:        cfg,
		svixClient:    svixClient,
		logger:        logger,
		stripeService: stripeService,
	}
}

// GetDashboardURL handles the GET /webhooks/dashboard endpoint
func (h *WebhookHandler) GetDashboardURL(c *gin.Context) {
	if !h.config.Webhook.Svix.Enabled {
		c.JSON(http.StatusOK, gin.H{
			"url":          "",
			"svix_enabled": false,
		})
		return
	}

	tenantID := types.GetTenantID(c.Request.Context())
	environmentID := types.GetEnvironmentID(c.Request.Context())

	// Get or create Svix application
	appID, err := h.svixClient.GetOrCreateApplication(c.Request.Context(), tenantID, environmentID)
	if err != nil {
		h.logger.Errorw("failed to get/create Svix application",
			"error", err,
			"tenant_id", tenantID,
			"environment_id", environmentID,
		)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to get webhook dashboard",
		})
		return
	}

	// Get dashboard URL
	url, err := h.svixClient.GetDashboardURL(c.Request.Context(), appID)
	if err != nil {
		h.logger.Errorw("failed to get Svix dashboard URL",
			"error", err,
			"tenant_id", tenantID,
			"environment_id", environmentID,
			"app_id", appID,
		)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to get webhook dashboard",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"url":          url,
		"svix_enabled": true,
	})
}

// HandleStripeWebhook handles Stripe webhook events
// POST /webhooks/stripe/{tenant_id}/{environment_id}
func (h *WebhookHandler) HandleStripeWebhook(c *gin.Context) {
	tenantID := c.Param("tenant_id")
	environmentID := c.Param("environment_id")

	if tenantID == "" || environmentID == "" {
		h.logger.Errorw("missing tenant_id or environment_id in webhook URL")
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "tenant_id and environment_id are required",
		})
		return
	}

	// Read the raw request body
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		h.logger.Errorw("failed to read request body", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Failed to read request body",
		})
		return
	}

	// Get Stripe signature from headers
	signature := c.GetHeader("Stripe-Signature")
	if signature == "" {
		h.logger.Errorw("missing Stripe-Signature header")
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Missing Stripe-Signature header",
		})
		return
	}

	// Set context with tenant and environment IDs
	ctx := types.SetTenantID(c.Request.Context(), tenantID)
	ctx = types.SetEnvironmentID(ctx, environmentID)
	c.Request = c.Request.WithContext(ctx)

	// Get Stripe connection to retrieve webhook secret
	conn, err := h.stripeService.ConnectionRepo.GetByEnvironmentAndProvider(ctx, environmentID, types.SecretProviderStripe)
	if err != nil {
		h.logger.Errorw("failed to get Stripe connection for webhook verification",
			"error", err,
			"environment_id", environmentID,
		)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Stripe connection not configured for this environment",
		})
		return
	}

	// Get Stripe configuration including webhook secret
	stripeConfig, err := h.stripeService.GetDecryptedStripeConfig(conn)
	if err != nil {
		h.logger.Errorw("failed to get Stripe configuration",
			"error", err,
			"environment_id", environmentID,
		)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid Stripe configuration",
		})
		return
	}

	// Verify webhook secret is configured
	if stripeConfig.WebhookSecret == "" {
		h.logger.Errorw("webhook secret not configured for Stripe connection",
			"environment_id", environmentID,
		)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Webhook secret not configured",
		})
		return
	}

	// Log webhook processing (without sensitive data)
	h.logger.Debugw("processing webhook",
		"environment_id", environmentID,
		"tenant_id", tenantID,
		"payload_length", len(body),
	)

	// Parse and verify the webhook event
	event, err := h.stripeService.ParseWebhookEvent(body, signature, stripeConfig.WebhookSecret)
	if err != nil {
		h.logger.Errorw("failed to parse/verify Stripe webhook event", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Failed to verify webhook signature",
		})
		return
	}

	// Handle different event types
	switch event.Type {
	case "customer.created":
		h.handleCustomerCreated(c, event, environmentID)
	default:
		h.logger.Infow("unhandled Stripe webhook event type", "type", event.Type)
		c.JSON(http.StatusOK, gin.H{
			"message": "Event type not handled",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Webhook processed successfully",
	})
}

func (h *WebhookHandler) handleCustomerCreated(c *gin.Context, event *stripe.Event, environmentID string) {
	var customer stripe.Customer
	err := json.Unmarshal(event.Data.Raw, &customer)
	if err != nil {
		h.logger.Errorw("failed to parse customer from webhook", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Failed to parse customer data",
		})
		return
	}

	if err := h.stripeService.CreateCustomerFromStripe(c.Request.Context(), &customer, environmentID); err != nil {
		h.logger.Errorw("failed to create customer from Stripe",
			"error", err,
			"stripe_customer_id", customer.ID,
			"environment_id", environmentID,
		)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to create customer",
		})
		return
	}

	h.logger.Infow("successfully created customer from Stripe",
		"stripe_customer_id", customer.ID,
		"environment_id", environmentID,
	)
}
