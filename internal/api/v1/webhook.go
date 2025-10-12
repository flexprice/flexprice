package v1

import (
	"io"
	"net/http"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/integration/stripe/webhook"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/svix"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

// WebhookHandler handles webhook-related endpoints
type WebhookHandler struct {
	config                          *config.Configuration
	svixClient                      *svix.Client
	logger                          *logger.Logger
	integrationFactory              *integration.Factory
	customerService                 interfaces.CustomerService
	paymentService                  interfaces.PaymentService
	invoiceService                  interfaces.InvoiceService
	planService                     interfaces.PlanService
	subscriptionService             interfaces.SubscriptionService
	entityIntegrationMappingService interfaces.EntityIntegrationMappingService
	db                              postgres.IClient
}

// NewWebhookHandler creates a new webhook handler
func NewWebhookHandler(
	cfg *config.Configuration,
	svixClient *svix.Client,
	logger *logger.Logger,
	integrationFactory *integration.Factory,
	customerService interfaces.CustomerService,
	paymentService interfaces.PaymentService,
	invoiceService interfaces.InvoiceService,
	planService interfaces.PlanService,
	subscriptionService interfaces.SubscriptionService,
	entityIntegrationMappingService interfaces.EntityIntegrationMappingService,
	db postgres.IClient,
) *WebhookHandler {
	return &WebhookHandler{
		config:                          cfg,
		svixClient:                      svixClient,
		logger:                          logger,
		integrationFactory:              integrationFactory,
		customerService:                 customerService,
		paymentService:                  paymentService,
		invoiceService:                  invoiceService,
		planService:                     planService,
		subscriptionService:             subscriptionService,
		entityIntegrationMappingService: entityIntegrationMappingService,
		db:                              db,
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
		c.Error(err)
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
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"url":          url,
		"svix_enabled": true,
	})
}

// @Summary Handle Stripe webhook events
// @Description Process incoming Stripe webhook events for payment status updates and customer creation
// @Tags Webhooks
// @Accept json
// @Produce json
// @Param tenant_id path string true "Tenant ID"
// @Param environment_id path string true "Environment ID"
// @Param Stripe-Signature header string true "Stripe webhook signature"
// @Success 200 {object} map[string]interface{} "Webhook processed successfully"
// @Failure 400 {object} map[string]interface{} "Bad request - missing parameters or invalid signature"
// @Failure 500 {object} map[string]interface{} "Internal server error"
// @Router /webhooks/stripe/{tenant_id}/{environment_id} [post]
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

	// Get Stripe integration
	stripeIntegration, err := h.integrationFactory.GetStripeIntegration(ctx)
	if err != nil {
		h.logger.Errorw("failed to get Stripe integration", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Stripe integration not available",
		})
		return
	}

	// Get Stripe client and configuration
	_, stripeConfig, err := stripeIntegration.Client.GetStripeClient(ctx)
	if err != nil {
		h.logger.Errorw("failed to get Stripe client and configuration",
			"error", err,
			"environment_id", environmentID,
		)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Stripe connection not configured for this environment",
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

	// Parse and verify the webhook event using new integration
	event, err := stripeIntegration.PaymentSvc.ParseWebhookEvent(body, signature, stripeConfig.WebhookSecret)
	if err != nil {
		h.logger.Errorw("failed to parse/verify Stripe webhook event", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Failed to verify webhook signature or parse event",
		})
		return
	}

	// Create service dependencies for webhook handler
	serviceDeps := &webhook.ServiceDependencies{
		CustomerService:                 h.customerService,
		PaymentService:                  h.paymentService,
		InvoiceService:                  h.invoiceService,
		PlanService:                     h.planService,
		SubscriptionService:             h.subscriptionService,
		EntityIntegrationMappingService: h.entityIntegrationMappingService,
		DB:                              h.db,
	}

	// Handle the webhook event using new integration
	err = stripeIntegration.WebhookHandler.HandleWebhookEvent(ctx, event, environmentID, serviceDeps)
	if err != nil {
		h.logger.Errorw("failed to handle webhook event", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to process webhook event",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Webhook processed successfully",
	})
}
