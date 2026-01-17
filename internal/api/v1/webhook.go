package v1

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/integration"
	chargebeewebhook "github.com/flexprice/flexprice/internal/integration/chargebee/webhook"
	moyasarwebhook "github.com/flexprice/flexprice/internal/integration/moyasar/webhook"
	nomodwebhook "github.com/flexprice/flexprice/internal/integration/nomod/webhook"
	quickbookswebhook "github.com/flexprice/flexprice/internal/integration/quickbooks/webhook"
	razorpaywebhook "github.com/flexprice/flexprice/internal/integration/razorpay/webhook"
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

// @Summary Handle HubSpot webhook events
// @Description Process incoming HubSpot webhook events for deal closed won and customer creation
// @Tags Webhooks
// @Accept json
// @Produce json
// @Param tenant_id path string true "Tenant ID"
// @Param environment_id path string true "Environment ID"
// @Param X-HubSpot-Signature-v3 header string true "HubSpot webhook signature"
// @Success 200 {object} map[string]interface{} "Webhook received (always returns 200)"
// @Router /webhooks/hubspot/{tenant_id}/{environment_id} [post]
func (h *WebhookHandler) HandleHubSpotWebhook(c *gin.Context) {
	// Always return 200 OK to HubSpot to prevent retries
	// We log errors internally but don't expose them to HubSpot
	defer func() {
		c.JSON(http.StatusOK, gin.H{
			"message": "Webhook received",
		})
	}()

	tenantID := c.Param("tenant_id")
	environmentID := c.Param("environment_id")

	if tenantID == "" || environmentID == "" {
		h.logger.Errorw("missing tenant_id or environment_id in webhook URL",
			"tenant_id", tenantID,
			"environment_id", environmentID)
		return
	}

	// Read the raw request body
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		h.logger.Errorw("failed to read request body", "error", err)
		return
	}

	// Get HubSpot v3 signature and timestamp headers
	signature := c.GetHeader("X-HubSpot-Signature-v3")
	timestamp := c.GetHeader("X-HubSpot-Request-Timestamp")

	if signature == "" {
		h.logger.Errorw("missing X-HubSpot-Signature-v3 header")
		return
	}

	if timestamp == "" {
		h.logger.Errorw("missing X-HubSpot-Request-Timestamp header")
		return
	}

	h.logger.Infow("received HubSpot webhook",
		"signature_length", len(signature),
		"timestamp", timestamp,
		"tenant_id", tenantID,
		"environment_id", environmentID)

	// Set context with tenant and environment IDs
	ctx := types.SetTenantID(c.Request.Context(), tenantID)
	ctx = types.SetEnvironmentID(ctx, environmentID)
	c.Request = c.Request.WithContext(ctx)

	// Get HubSpot integration
	hubspotIntegration, err := h.integrationFactory.GetHubSpotIntegration(ctx)
	if err != nil {
		h.logger.Errorw("failed to get HubSpot integration", "error", err)
		return
	}

	// Get HubSpot configuration
	hubspotConfig, err := hubspotIntegration.Client.GetHubSpotConfig(ctx)
	if err != nil {
		h.logger.Errorw("failed to get HubSpot configuration",
			"error", err,
			"environment_id", environmentID)
		return
	}

	// Verify webhook secret is configured
	if hubspotConfig.ClientSecret == "" {
		h.logger.Errorw("client secret not configured for HubSpot connection",
			"environment_id", environmentID)
		return
	}

	// Validate timestamp (reject if older than 5 minutes)
	timestampInt, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		h.logger.Errorw("invalid timestamp format", "timestamp", timestamp, "error", err)
		return
	}

	currentTime := time.Now().UnixMilli()
	maxAllowedTimestamp := int64(300000) // 5 minutes in milliseconds
	if currentTime-timestampInt > maxAllowedTimestamp {
		h.logger.Warnw("timestamp too old, rejecting webhook",
			"timestamp", timestampInt,
			"current_time", currentTime,
			"age_ms", currentTime-timestampInt)
		return
	}

	// Construct the full URL that HubSpot called
	// When behind a proxy (like ngrok), check X-Forwarded-Proto
	var scheme string
	if proto := c.GetHeader("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	} else if c.Request.TLS != nil {
		scheme = "https"
	} else {
		scheme = "http"
	}
	fullURL := scheme + "://" + c.Request.Host + c.Request.URL.String()

	h.logger.Debugw("verifying v3 signature",
		"method", c.Request.Method,
		"full_url", fullURL,
		"timestamp", timestamp)

	// Verify webhook signature (v3)
	signatureValid := hubspotIntegration.Client.VerifyWebhookSignatureV3(
		c.Request.Method,
		fullURL,
		body,
		timestamp,
		signature,
		hubspotConfig.ClientSecret,
	)

	if !signatureValid {
		h.logger.Errorw("invalid webhook signature - rejecting")
		return
	}

	// Log webhook processing (without sensitive data)
	h.logger.Infow("processing HubSpot webhook",
		"environment_id", environmentID,
		"tenant_id", tenantID,
		"payload_length", len(body))

	// Parse webhook payload
	events, err := hubspotIntegration.WebhookHandler.ParseWebhookPayload(body)
	if err != nil {
		h.logger.Errorw("failed to parse HubSpot webhook payload", "error", err)
		return
	}

	// Create service dependencies for webhook handler
	serviceDeps := &interfaces.ServiceDependencies{
		CustomerService:                 h.customerService,
		PaymentService:                  h.paymentService,
		InvoiceService:                  h.invoiceService,
		PlanService:                     h.planService,
		SubscriptionService:             h.subscriptionService,
		EntityIntegrationMappingService: h.entityIntegrationMappingService,
		DB:                              h.db,
	}

	// Handle the webhook events
	err = hubspotIntegration.WebhookHandler.HandleWebhookEvent(ctx, events, environmentID, serviceDeps)
	if err != nil {
		h.logger.Errorw("failed to handle HubSpot webhook event", "error", err)
		return
	}

	h.logger.Infow("successfully processed HubSpot webhook",
		"environment_id", environmentID,
		"event_count", len(events))
}

// @Summary Handle Razorpay webhook events
// @Description Process incoming Razorpay webhook events for payment capture and failure
// @Tags Webhooks
// @Accept json
// @Produce json
// @Param tenant_id path string true "Tenant ID"
// @Param environment_id path string true "Environment ID"
// @Param X-Razorpay-Signature header string true "Razorpay webhook signature"
// @Success 200 {object} map[string]interface{} "Webhook received (always returns 200)"
// @Router /webhooks/razorpay/{tenant_id}/{environment_id} [post]
func (h *WebhookHandler) HandleRazorpayWebhook(c *gin.Context) {
	// Always return 200 OK to Razorpay to prevent retries
	// We log errors internally but don't expose them to Razorpay
	defer func() {
		c.JSON(http.StatusOK, gin.H{
			"message": "Webhook received",
		})
	}()

	tenantID := c.Param("tenant_id")
	environmentID := c.Param("environment_id")

	if tenantID == "" || environmentID == "" {
		h.logger.Errorw("missing tenant_id or environment_id in webhook URL",
			"tenant_id", tenantID,
			"environment_id", environmentID)
		return
	}

	// Read the raw request body
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		h.logger.Errorw("failed to read request body", "error", err)
		return
	}

	// Get Razorpay signature from headers
	signature := c.GetHeader("X-Razorpay-Signature")

	// Log all headers for debugging (only in case of missing signature)
	if signature == "" {
		h.logger.Warnw("missing X-Razorpay-Signature header - webhook test ping or signature not configured",
			"tenant_id", tenantID,
			"environment_id", environmentID,
			"has_body", len(body) > 0,
			"content_type", c.GetHeader("Content-Type"))
		return
	}

	// Get Razorpay event ID for idempotency
	// As per Razorpay docs: "The value for this header is unique per event"
	eventID := c.GetHeader("x-razorpay-event-id")

	// Set context with tenant and environment IDs
	ctx := types.SetTenantID(c.Request.Context(), tenantID)
	ctx = types.SetEnvironmentID(ctx, environmentID)
	c.Request = c.Request.WithContext(ctx)

	// Get Razorpay integration
	razorpayIntegration, err := h.integrationFactory.GetRazorpayIntegration(ctx)
	if err != nil {
		h.logger.Errorw("failed to get Razorpay integration", "error", err)
		return
	}

	// Verify webhook signature
	err = razorpayIntegration.Client.VerifyWebhookSignature(ctx, body, signature)
	if err != nil {
		h.logger.Errorw("failed to verify Razorpay webhook signature", "error", err)
		return
	}

	// Log webhook processing (without sensitive data)
	h.logger.Infow("processing Razorpay webhook",
		"environment_id", environmentID,
		"tenant_id", tenantID,
		"event_id", eventID,
		"payload_length", len(body))

	// Parse webhook payload
	var event razorpaywebhook.RazorpayWebhookEvent
	err = json.Unmarshal(body, &event)
	if err != nil {
		h.logger.Errorw("failed to parse Razorpay webhook payload", "error", err)
		return
	}

	// Create service dependencies for webhook handler
	serviceDeps := &razorpaywebhook.ServiceDependencies{
		CustomerService:                 h.customerService,
		PaymentService:                  h.paymentService,
		InvoiceService:                  h.invoiceService,
		PlanService:                     h.planService,
		SubscriptionService:             h.subscriptionService,
		EntityIntegrationMappingService: h.entityIntegrationMappingService,
		DB:                              h.db,
	}

	// Handle the webhook event
	err = razorpayIntegration.WebhookHandler.HandleWebhookEvent(ctx, &event, environmentID, serviceDeps)
	if err != nil {
		h.logger.Errorw("failed to handle Razorpay webhook event", "error", err)
		return
	}

	h.logger.Infow("successfully processed Razorpay webhook",
		"environment_id", environmentID,
		"event_id", eventID,
		"event_type", event.Event)
}

// @Summary Handle Chargebee webhook events
// @Description Process incoming Chargebee webhook events for payment status updates
// @Tags Webhooks
// @Accept json
// @Produce json
// @Param tenant_id path string true "Tenant ID"
// @Param environment_id path string true "Environment ID"
// @Param Authorization header string false "Basic Auth credentials"
// @Success 200 {object} map[string]interface{} "Webhook processed successfully"
// @Failure 401 {object} map[string]interface{} "Unauthorized"
// @Failure 400 {object} map[string]interface{} "Bad request"
// @Failure 500 {object} map[string]interface{} "Internal server error"
// @Router /webhooks/chargebee/{tenant_id}/{environment_id} [post]
func (h *WebhookHandler) HandleChargebeeWebhook(c *gin.Context) {
	// Always return 200 OK to Chargebee to prevent retries
	// We log errors internally but don't expose them to Chargebee
	defer func() {
		c.JSON(http.StatusOK, gin.H{
			"message": "Webhook received",
		})
	}()

	tenantID := c.Param("tenant_id")
	environmentID := c.Param("environment_id")

	if tenantID == "" || environmentID == "" {
		h.logger.Errorw("missing tenant_id or environment_id in webhook URL",
			"tenant_id", tenantID,
			"environment_id", environmentID)
		return
	}

	// Read the raw request body
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		h.logger.Errorw("failed to read request body", "error", err)
		return
	}

	// Set context with tenant and environment IDs
	ctx := types.SetTenantID(c.Request.Context(), tenantID)
	ctx = types.SetEnvironmentID(ctx, environmentID)
	c.Request = c.Request.WithContext(ctx)

	// Get Chargebee integration
	chargebeeIntegration, err := h.integrationFactory.GetChargebeeIntegration(ctx)
	if err != nil {
		h.logger.Errorw("failed to get Chargebee integration", "error", err)
		return
	}

	// Verify Basic Authentication (Chargebee v2 security - OPTIONAL)
	// Only verify if credentials are configured in connection settings
	username, password, hasAuth := c.Request.BasicAuth()

	// Get connection to check if webhook auth is configured
	conn, err := chargebeeIntegration.Client.GetConnection(ctx)
	if err != nil {
		h.logger.Errorw("failed to get Chargebee connection", "error", err)
		return
	}

	// Check if webhook auth is configured in FlexPrice
	hasWebhookAuthConfigured := conn.EncryptedSecretData.Chargebee != nil &&
		conn.EncryptedSecretData.Chargebee.WebhookUsername != "" &&
		conn.EncryptedSecretData.Chargebee.WebhookPassword != ""

	// Case 1: Auth configured in FlexPrice but webhook request has no auth
	if hasWebhookAuthConfigured && !hasAuth {
		h.logger.Errorw("webhook auth is configured but request has no Basic Auth credentials",
			"remote_addr", c.ClientIP(),
			"tenant_id", tenantID,
			"environment_id", environmentID)
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	// Case 2: Auth NOT configured in FlexPrice but webhook request has auth
	if !hasWebhookAuthConfigured && hasAuth {
		h.logger.Errorw("webhook request has Basic Auth but no credentials configured in FlexPrice",
			"remote_addr", c.ClientIP(),
			"tenant_id", tenantID,
			"environment_id", environmentID,
			"note", "Configure webhook_username and webhook_password in connection settings")
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	} else if hasWebhookAuthConfigured && hasAuth {
		// Case 3: Both sides have auth - verify it
		err = chargebeeIntegration.Client.VerifyWebhookBasicAuth(ctx, username, password)
		if err != nil {
			h.logger.Errorw("Chargebee webhook basic auth verification failed",
				"error", err,
				"remote_addr", c.ClientIP())
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		h.logger.Debugw("Chargebee webhook basic auth verified",
			"remote_addr", c.ClientIP())
	} else {
		// Case 4: Neither side has auth - allow but warn
		h.logger.Infow("Chargebee webhook processing without authentication",
			"remote_addr", c.ClientIP(),
			"tenant_id", tenantID,
			"environment_id", environmentID,
			"note", "Consider configuring Basic Auth for security")
	}

	// Parse webhook event
	var event chargebeewebhook.ChargebeeWebhookEvent
	if err := json.Unmarshal(body, &event); err != nil {
		h.logger.Errorw("failed to parse Chargebee webhook event",
			"error", err,
			"environment_id", environmentID)
		return
	}

	// Log webhook processing (without sensitive data)
	h.logger.Infow("processing Chargebee webhook",
		"environment_id", environmentID,
		"event_id", event.ID,
		"event_type", event.EventType,
		"occurred_at", event.OccurredAt)

	// Handle the event
	err = chargebeeIntegration.WebhookHandler.HandleWebhookEvent(ctx, &event, environmentID)
	if err != nil {
		h.logger.Errorw("error processing Chargebee webhook event",
			"error", err,
			"event_id", event.ID,
			"event_type", event.EventType)
		return
	}

	h.logger.Infow("successfully processed Chargebee webhook",
		"environment_id", environmentID,
		"event_id", event.ID,
		"event_type", event.EventType)
}

// @Summary Handle QuickBooks webhook events
// @Description Process incoming QuickBooks webhook events for payment sync
// @Tags Webhooks
// @Accept json
// @Produce json
// @Param tenant_id path string true "Tenant ID"
// @Param environment_id path string true "Environment ID"
// @Param intuit-signature header string false "QuickBooks webhook signature"
// @Success 200 {object} map[string]interface{} "Webhook processed successfully"
// @Failure 401 {object} map[string]interface{} "Unauthorized - invalid signature"
// @Failure 400 {object} map[string]interface{} "Bad request"
// @Failure 500 {object} map[string]interface{} "Internal server error"
// @Router /webhooks/quickbooks/{tenant_id}/{environment_id} [post]
func (h *WebhookHandler) HandleQuickBooksWebhook(c *gin.Context) {
	// Always return 200 OK to QuickBooks to prevent retries
	// We log errors internally but don't expose them to QuickBooks
	defer func() {
		c.JSON(http.StatusOK, gin.H{
			"message": "Webhook received",
		})
	}()

	tenantID := c.Param("tenant_id")
	environmentID := c.Param("environment_id")

	if tenantID == "" || environmentID == "" {
		h.logger.Errorw("missing tenant_id or environment_id in webhook URL",
			"tenant_id", tenantID,
			"environment_id", environmentID)
		return
	}

	// Read the raw request body
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		h.logger.Errorw("failed to read request body", "error", err)
		return
	}

	// Get QuickBooks signature from headers
	signature := c.GetHeader("intuit-signature")

	// Log webhook receipt (without sensitive data)
	h.logger.Debugw("received QuickBooks webhook",
		"tenant_id", tenantID,
		"environment_id", environmentID,
		"has_signature", signature != "",
		"body_length", len(body))

	// Set context with tenant and environment IDs
	ctx := types.SetTenantID(c.Request.Context(), tenantID)
	ctx = types.SetEnvironmentID(ctx, environmentID)
	c.Request = c.Request.WithContext(ctx)

	// Get QuickBooks integration
	qbIntegration, err := h.integrationFactory.GetQuickBooksIntegration(ctx)
	if err != nil {
		h.logger.Errorw("failed to get QuickBooks integration", "error", err)
		return
	}

	// Verify webhook signature (if signature provided)
	if signature != "" {
		err = qbIntegration.WebhookHandler.VerifyWebhookSignature(ctx, body, signature)
		if err != nil {
			h.logger.Errorw("failed to verify QuickBooks webhook signature",
				"error", err,
				"tenant_id", tenantID,
				"environment_id", environmentID)
			// Don't return 401 - QuickBooks expects 200
			return
		}
		h.logger.Debugw("QuickBooks webhook signature verified",
			"tenant_id", tenantID,
			"environment_id", environmentID)
	} else {
		h.logger.Warnw("QuickBooks webhook received without signature",
			"tenant_id", tenantID,
			"environment_id", environmentID,
			"note", "Consider configuring webhook verifier token for security")
	}

	// Create service dependencies for webhook handler
	serviceDeps := &quickbookswebhook.ServiceDependencies{
		PaymentService: h.paymentService,
		InvoiceService: h.invoiceService,
	}

	// Handle the webhook event
	err = qbIntegration.WebhookHandler.HandleWebhook(ctx, body, serviceDeps)
	if err != nil {
		h.logger.Errorw("failed to handle QuickBooks webhook event",
			"error", err,
			"tenant_id", tenantID,
			"environment_id", environmentID)
		return
	}

	h.logger.Infow("successfully processed QuickBooks webhook",
		"tenant_id", tenantID,
		"environment_id", environmentID)
}

// @Summary Handle Nomod webhook events
// @Description Process incoming Nomod webhook events for payment and invoice payments
// @Tags Webhooks
// @Accept json
// @Produce json
// @Param tenant_id path string true "Tenant ID"
// @Param environment_id path string true "Environment ID"
// @Param X-API-KEY header string false "Nomod webhook secret (if configured)"
// @Success 200 {object} map[string]interface{} "Webhook processed successfully"
// @Failure 401 {object} map[string]interface{} "Unauthorized - invalid or missing X-API-KEY"
// @Router /webhooks/nomod/{tenant_id}/{environment_id} [post]
func (h *WebhookHandler) HandleNomodWebhook(c *gin.Context) {
	tenantID := c.Param("tenant_id")
	environmentID := c.Param("environment_id")

	if tenantID == "" || environmentID == "" {
		h.logger.Errorw("missing tenant_id or environment_id in webhook URL",
			"tenant_id", tenantID,
			"environment_id", environmentID)
		c.JSON(http.StatusOK, gin.H{
			"message": "Webhook received",
		})
		return
	}

	// Read the raw request body
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		h.logger.Errorw("failed to read request body", "error", err)
		c.JSON(http.StatusOK, gin.H{
			"message": "Webhook received",
		})
		return
	}

	// Get X-API-KEY from headers for authentication
	providedAPIKey := c.GetHeader("X-API-KEY")

	// Set context with tenant and environment IDs
	ctx := types.SetTenantID(c.Request.Context(), tenantID)
	ctx = types.SetEnvironmentID(ctx, environmentID)
	c.Request = c.Request.WithContext(ctx)

	// Get Nomod integration
	nomodIntegration, err := h.integrationFactory.GetNomodIntegration(ctx)
	if err != nil {
		h.logger.Errorw("failed to get Nomod integration", "error", err)
		c.JSON(http.StatusOK, gin.H{
			"message": "Webhook received",
		})
		return
	}

	// Get connection to check if webhook secret is configured
	conn, err := nomodIntegration.Client.GetConnection(ctx)
	if err != nil {
		h.logger.Errorw("failed to get Nomod connection", "error", err)
		c.JSON(http.StatusOK, gin.H{
			"message": "Webhook received",
		})
		return
	}

	// Check if webhook secret is configured
	hasWebhookSecretConfigured := conn.EncryptedSecretData.Nomod != nil &&
		conn.EncryptedSecretData.Nomod.WebhookSecret != ""

	hasAPIKey := providedAPIKey != ""

	// Verify webhook authentication if webhook secret is configured
	if hasWebhookSecretConfigured {
		if !hasAPIKey {
			h.logger.Errorw("webhook secret configured but X-API-KEY header not provided",
				"remote_addr", c.ClientIP(),
				"tenant_id", tenantID,
				"environment_id", environmentID)
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "X-API-KEY header required for webhook authentication",
			})
			return
		}

		// Verify the API key
		err = nomodIntegration.Client.VerifyWebhookAuth(ctx, providedAPIKey)
		if err != nil {
			h.logger.Errorw("Nomod webhook authentication failed",
				"error", err,
				"remote_addr", c.ClientIP())
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Invalid webhook authentication",
			})
			return
		}
		h.logger.Debugw("Nomod webhook authentication successful",
			"remote_addr", c.ClientIP())
	} else {
		h.logger.Debugw("Nomod webhook processing without authentication",
			"tenant_id", tenantID,
			"environment_id", environmentID)
	}

	// Log webhook processing (without sensitive data)
	h.logger.Infow("processing Nomod webhook",
		"environment_id", environmentID,
		"tenant_id", tenantID)

	// Parse webhook payload
	var payload nomodwebhook.NomodWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		h.logger.Errorw("failed to parse Nomod webhook payload", "error", err)
		return
	}

	h.logger.Infow("parsed Nomod webhook payload",
		"charge_id", payload.ID,
		"has_invoice_id", payload.InvoiceID != nil,
		"has_payment_link_id", payload.PaymentLinkID != nil)

	// Create service dependencies for webhook handler
	serviceDeps := &nomodwebhook.ServiceDependencies{
		CustomerService: h.customerService,
		PaymentService:  h.paymentService,
		InvoiceService:  h.invoiceService,
		PlanService:     h.planService,
	}

	// Handle the event
	err = nomodIntegration.WebhookHandler.HandleWebhookEvent(ctx, &payload, serviceDeps)
	if err != nil {
		h.logger.Errorw("failed to handle Nomod webhook event",
			"error", err,
			"charge_id", payload.ID,
			"environment_id", environmentID)
		c.JSON(http.StatusOK, gin.H{
			"message": "Webhook received",
		})
		return
	}

	h.logger.Infow("successfully processed Nomod webhook",
		"charge_id", payload.ID,
		"environment_id", environmentID)

	c.JSON(http.StatusOK, gin.H{
		"message": "Webhook processed successfully",
	})
}

// @Summary Handle Moyasar webhook events
// @Description Process incoming Moyasar webhook events for payment status updates
// @Tags Webhooks
// @Accept json
// @Produce json
// @Param tenant_id path string true "Tenant ID"
// @Param environment_id path string true "Environment ID"
// @Param X-Moyasar-Signature header string false "Moyasar webhook signature"
// @Success 200 {object} map[string]interface{} "Webhook received (always returns 200)"
// @Router /webhooks/moyasar/{tenant_id}/{environment_id} [post]
func (h *WebhookHandler) HandleMoyasarWebhook(c *gin.Context) {
	// Always return 200 OK to Moyasar to prevent retries
	// We log errors internally but don't expose them to Moyasar
	defer func() {
		c.JSON(http.StatusOK, gin.H{
			"message": "Webhook received",
		})
	}()

	tenantID := c.Param("tenant_id")
	environmentID := c.Param("environment_id")

	if tenantID == "" || environmentID == "" {
		h.logger.Errorw("missing tenant_id or environment_id in webhook URL",
			"tenant_id", tenantID,
			"environment_id", environmentID)
		return
	}

	// Read the raw request body
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		h.logger.Errorw("failed to read request body", "error", err)
		return
	}

	// Get Moyasar signature from headers (if present)
	signature := c.GetHeader("X-Moyasar-Signature")

	// Set context with tenant and environment IDs
	ctx := types.SetTenantID(c.Request.Context(), tenantID)
	ctx = types.SetEnvironmentID(ctx, environmentID)
	c.Request = c.Request.WithContext(ctx)

	// Get Moyasar integration
	moyasarIntegration, err := h.integrationFactory.GetMoyasarIntegration(ctx)
	if err != nil {
		h.logger.Errorw("failed to get Moyasar integration", "error", err)
		return
	}

	// Verify webhook signature (if present and webhook secret is configured)
	if signature != "" {
		err = moyasarIntegration.Client.VerifyWebhookSignature(ctx, body, signature)
		if err != nil {
			h.logger.Errorw("failed to verify Moyasar webhook signature",
				"error", err,
				"tenant_id", tenantID,
				"environment_id", environmentID)
			return
		}
		h.logger.Debugw("Moyasar webhook signature verified",
			"tenant_id", tenantID,
			"environment_id", environmentID)
	} else {
		h.logger.Warnw("Moyasar webhook received without signature",
			"tenant_id", tenantID,
			"environment_id", environmentID,
			"note", "Consider configuring webhook secret for security")
	}

	// Log webhook processing (without sensitive data)
	h.logger.Infow("processing Moyasar webhook",
		"environment_id", environmentID,
		"tenant_id", tenantID,
		"payload_length", len(body))

	// Parse webhook payload
	var event moyasarwebhook.MoyasarWebhookEvent
	err = json.Unmarshal(body, &event)
	if err != nil {
		h.logger.Errorw("failed to parse Moyasar webhook payload", "error", err)
		return
	}

	// Create service dependencies for webhook handler
	serviceDeps := &moyasarwebhook.ServiceDependencies{
		CustomerService:                 h.customerService,
		PaymentService:                  h.paymentService,
		InvoiceService:                  h.invoiceService,
		PlanService:                     h.planService,
		SubscriptionService:             h.subscriptionService,
		EntityIntegrationMappingService: h.entityIntegrationMappingService,
		DB:                              h.db,
	}

	// Handle the webhook event
	err = moyasarIntegration.WebhookHandler.HandleWebhookEvent(ctx, &event, environmentID, serviceDeps)
	if err != nil {
		h.logger.Errorw("failed to handle Moyasar webhook event",
			"error", err,
			"event_type", event.Type,
			"environment_id", environmentID)
		return
	}

	h.logger.Infow("successfully processed Moyasar webhook",
		"environment_id", environmentID,
		"event_type", event.Type)
}
