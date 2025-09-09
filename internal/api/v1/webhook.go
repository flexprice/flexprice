package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/svix"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/stripe/stripe-go/v82"
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

	// Get Stripe connection to retrieve webhook secret
	conn, err := h.stripeService.ConnectionRepo.GetByProvider(ctx, types.SecretProviderStripe)
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
	switch string(event.Type) {
	case string(types.WebhookEventTypeCustomerCreated):
		h.handleCustomerCreated(c, event, environmentID)
	case string(types.WebhookEventTypeCheckoutSessionCompleted):
		h.handleCheckoutSessionCompleted(c, event, environmentID)
	case string(types.WebhookEventTypeCheckoutSessionAsyncPaymentSucceeded):
		h.handleCheckoutSessionAsyncPaymentSucceeded(c, event, environmentID)
	case string(types.WebhookEventTypeCheckoutSessionAsyncPaymentFailed):
		h.handleCheckoutSessionAsyncPaymentFailed(c, event, environmentID)
	case string(types.WebhookEventTypeCheckoutSessionExpired):
		h.handleCheckoutSessionExpired(c, event, environmentID)
	case string(types.WebhookEventTypePaymentIntentPaymentFailed):
		h.handlePaymentIntentPaymentFailed(c, event, environmentID)

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

	// Convert Stripe customer to generic customer data
	customerData := map[string]interface{}{
		"name":  customer.Name,
		"email": customer.Email,
	}

	// Add address if available
	if customer.Address != nil {
		customerData["address"] = map[string]interface{}{
			"line1":       customer.Address.Line1,
			"line2":       customer.Address.Line2,
			"city":        customer.Address.City,
			"state":       customer.Address.State,
			"postal_code": customer.Address.PostalCode,
			"country":     customer.Address.Country,
		}
	}

	// Add metadata if available (with nil checks)
	if customer.Metadata != nil {
		for k, v := range customer.Metadata {
			// Add all metadata values since they are strings and can't be nil
			customerData[k] = v
		}
	}

	// Use integration service to sync customer from Stripe
	integrationService := service.NewIntegrationService(h.stripeService.ServiceParams)
	if err := integrationService.SyncCustomerFromProvider(c.Request.Context(), "stripe", customer.ID, customerData); err != nil {
		h.logger.Errorw("failed to sync customer from Stripe",
			"error", err,
			"stripe_customer_id", customer.ID,
			"environment_id", environmentID,
		)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to sync customer from Stripe",
		})
		return
	}

	h.logger.Infow("successfully synced customer from Stripe",
		"stripe_customer_id", customer.ID,
		"environment_id", environmentID,
	)
}

// findPaymentBySessionID finds a payment record by Stripe session ID
func (h *WebhookHandler) findPaymentBySessionID(ctx context.Context, sessionID string) (*dto.PaymentResponse, error) {
	paymentService := service.NewPaymentService(h.stripeService.ServiceParams)
	payments, err := paymentService.ListPayments(ctx, &types.PaymentFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
	})
	if err != nil {
		return nil, err
	}

	// Filter by gateway_tracking_id (which contains session ID)
	for _, payment := range payments.Items {
		if payment.GatewayTrackingID != nil && *payment.GatewayTrackingID == sessionID {
			return payment, nil
		}
	}

	return nil, nil
}

// getSessionIDFromPaymentIntent gets the session ID by looking up checkout sessions with the payment intent ID
func (h *WebhookHandler) getSessionIDFromPaymentIntent(ctx context.Context, paymentIntentID string, environmentID string) (string, error) {
	// Get Stripe connection using the stripe service connection repo
	conn, err := h.stripeService.ConnectionRepo.GetByProvider(ctx, types.SecretProviderStripe)
	if err != nil {
		return "", err
	}

	// Get Stripe config
	stripeConfig, err := h.stripeService.GetDecryptedStripeConfig(conn)
	if err != nil {
		return "", err
	}

	// Initialize Stripe client
	stripeClient := stripe.NewClient(stripeConfig.SecretKey, nil)

	// List checkout sessions with the payment intent ID
	params := &stripe.CheckoutSessionListParams{
		PaymentIntent: stripe.String(paymentIntentID),
	}
	params.Limit = stripe.Int64(1)

	iter := stripeClient.V1CheckoutSessions.List(context.Background(), params)
	var sessionID string

	iter(func(session *stripe.CheckoutSession, err error) bool {
		if err != nil {
			return false // Stop iteration on error
		}
		sessionID = session.ID
		return false // Stop after first result
	})

	if sessionID == "" {
		return "", fmt.Errorf("no checkout session found for payment intent %s", paymentIntentID)
	}

	return sessionID, nil
}

// handleCheckoutSessionCompleted handles checkout.session.completed webhook
func (h *WebhookHandler) handleCheckoutSessionCompleted(c *gin.Context, event *stripe.Event, environmentID string) {
	var session stripe.CheckoutSession
	err := json.Unmarshal(event.Data.Raw, &session)
	if err != nil {
		h.logger.Errorw("failed to parse checkout session from webhook", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Failed to parse checkout session data",
		})
		return
	}

	// Log the webhook data correctly
	h.logger.Infow("received checkout.session.completed webhook",
		"session_id", session.ID,
		"status", session.Status,
		"environment_id", environmentID,
		"event_id", event.ID,
		"event_type", event.Type,
		"has_payment_intent", session.PaymentIntent != nil,
		"payment_intent_id", func() string {
			if session.PaymentIntent != nil {
				return session.PaymentIntent.ID
			}
			return ""
		}(),
	)

	// Find payment record by session ID
	payment, err := h.findPaymentBySessionID(c.Request.Context(), session.ID)
	if err != nil {
		h.logger.Errorw("failed to find payment record", "error", err, "session_id", session.ID)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to find payment record",
		})
		return
	}

	if payment == nil {
		h.logger.Warnw("no payment record found for session", "session_id", session.ID)
		c.JSON(http.StatusOK, gin.H{
			"message": "No payment record found for session",
		})
		return
	}

	// Check if payment is already successful - but allow processing for overpayment reconciliation
	if payment.PaymentStatus == types.PaymentStatusSucceeded {
		h.logger.Infow("payment is already successful, but will proceed to check for overpayment",
			"payment_id", payment.ID,
			"session_id", session.ID,
			"current_status", payment.PaymentStatus)
		// Continue processing to handle potential overpayment instead of returning
	}

	// Make a separate Stripe API call to get current status
	paymentStatusResp, err := h.stripeService.GetPaymentStatus(c.Request.Context(), session.ID, environmentID)
	if err != nil {
		h.logger.Errorw("failed to get payment status from Stripe API",
			"error", err,
			"session_id", session.ID,
			"payment_id", payment.ID)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to get payment status from Stripe",
		})
		return
	}

	h.logger.Infow("retrieved payment status from Stripe API",
		"session_id", session.ID,
		"payment_intent_id", paymentStatusResp.PaymentIntentID,
		"payment_method_id", paymentStatusResp.PaymentMethodID,
		"status", paymentStatusResp.Status,
		"amount", paymentStatusResp.Amount.String(),
		"currency", paymentStatusResp.Currency,
		"payment_id", payment.ID,
	)

	// Determine payment status based on Stripe API response
	var paymentStatus string
	var succeededAt *time.Time
	var failedAt *time.Time
	var errorMessage *string

	switch paymentStatusResp.Status {
	case "complete", "succeeded":
		paymentStatus = string(types.PaymentStatusSucceeded)
		now := time.Now()
		succeededAt = &now
	case "failed", "canceled":
		paymentStatus = string(types.PaymentStatusFailed)
		now := time.Now()
		failedAt = &now
		errMsg := "Payment failed or canceled"
		errorMessage = &errMsg
	case "pending", "processing":
		paymentStatus = string(types.PaymentStatusPending)
	default:
		paymentStatus = string(types.PaymentStatusPending)
	}

	// Update payment with data from Stripe API
	paymentService := service.NewPaymentService(h.stripeService.ServiceParams)
	updateReq := dto.UpdatePaymentRequest{
		PaymentStatus: &paymentStatus,
	}

	// Update payment method ID with payment intent ID (pi_ prefixed)
	if paymentStatusResp.PaymentIntentID != "" {
		updateReq.GatewayPaymentID = &paymentStatusResp.PaymentIntentID
	}

	// Update payment method ID with pm_ prefixed ID from Stripe
	if paymentStatusResp.PaymentMethodID != "" {
		updateReq.PaymentMethodID = &paymentStatusResp.PaymentMethodID
	}

	// Set succeeded_at or failed_at based on status
	if succeededAt != nil {
		updateReq.SucceededAt = succeededAt
	}
	if failedAt != nil {
		updateReq.FailedAt = failedAt
	}
	if errorMessage != nil {
		updateReq.ErrorMessage = errorMessage
	}

	_, err = paymentService.UpdatePayment(c.Request.Context(), payment.ID, updateReq)
	if err != nil {
		h.logger.Errorw("failed to update payment status", "error", err, "payment_id", payment.ID)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to update payment status",
		})
		return
	}

	// If payment succeeded and we have a payment method, set it as default in Stripe if customer doesn't have one
	if paymentStatus == string(types.PaymentStatusSucceeded) && paymentStatusResp.PaymentMethodID != "" {
		// Get customer ID from payment metadata or destination
		var customerID string
		if payment.DestinationType == types.PaymentDestinationTypeInvoice {
			// Try to get customer from invoice
			invoiceService := service.NewInvoiceService(h.stripeService.ServiceParams)
			invoiceResp, err := invoiceService.GetInvoice(c.Request.Context(), payment.DestinationID)
			if err == nil {
				customerID = invoiceResp.CustomerID
			}
		}

		if customerID != "" {
			// Check if SaveCardAndMakeDefault was set in gateway metadata
			saveCardAndMakeDefault := false
			if payment.GatewayMetadata != nil {
				if saveCardStr, exists := payment.GatewayMetadata["save_card_and_make_default"]; exists {
					saveCardAndMakeDefault = saveCardStr == "true"
				}
			}

			if saveCardAndMakeDefault {
				// Set this payment method as default in Stripe
				if setErr := h.stripeService.SetDefaultPaymentMethod(c.Request.Context(), customerID, paymentStatusResp.PaymentMethodID); setErr != nil {
					h.logger.Errorw("failed to set default payment method in Stripe",
						"error", setErr,
						"customer_id", customerID,
						"payment_method_id", paymentStatusResp.PaymentMethodID,
					)
				}
			} else {
				h.logger.Infow("payment was not configured to save card, skipping default assignment",
					"customer_id", customerID,
					"payment_method_id", paymentStatusResp.PaymentMethodID,
					"save_card_and_make_default", saveCardAndMakeDefault,
				)
			}
		}
	}
	// Reconcile payment with invoice if payment succeeded or if payment was already succeeded
	// This handles both new payments and duplicate payments that should result in overpayment
	if paymentStatus == string(types.PaymentStatusSucceeded) || payment.PaymentStatus == types.PaymentStatusSucceeded {
		if err := h.stripeService.ReconcilePaymentWithInvoice(c.Request.Context(), payment.ID, payment.Amount); err != nil {
			h.logger.Errorw("failed to reconcile payment with invoice",
				"error", err,
				"payment_id", payment.ID,
				"invoice_id", payment.DestinationID,
			)
			// Don't fail the webhook if reconciliation fails, but log the error
			// The payment is still marked as succeeded
		}
	}

	h.logger.Infow("successfully updated payment status from webhook",
		"payment_id", payment.ID,
		"session_id", session.ID,
		"payment_intent_id", paymentStatusResp.PaymentIntentID,
		"payment_method_id", paymentStatusResp.PaymentMethodID,
		"status", paymentStatus,
		"environment_id", environmentID,
	)
}

// handleCheckoutSessionAsyncPaymentSucceeded handles checkout.session.async_payment_succeeded webhook
func (h *WebhookHandler) handleCheckoutSessionAsyncPaymentSucceeded(c *gin.Context, event *stripe.Event, environmentID string) {
	var session stripe.CheckoutSession
	err := json.Unmarshal(event.Data.Raw, &session)
	if err != nil {
		h.logger.Errorw("failed to parse checkout session from webhook", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Failed to parse checkout session data",
		})
		return
	}

	// Log the webhook data correctly
	h.logger.Infow("received checkout.session.async_payment_succeeded webhook",
		"session_id", session.ID,
		"status", session.Status,
		"environment_id", environmentID,
		"event_id", event.ID,
		"event_type", event.Type,
		"has_payment_intent", session.PaymentIntent != nil,
		"payment_intent_id", func() string {
			if session.PaymentIntent != nil {
				return session.PaymentIntent.ID
			}
			return ""
		}(),
	)

	// Find payment record by session ID
	payment, err := h.findPaymentBySessionID(c.Request.Context(), session.ID)
	if err != nil {
		h.logger.Errorw("failed to find payment record", "error", err, "session_id", session.ID)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to find payment record",
		})
		return
	}

	if payment == nil {
		h.logger.Warnw("no payment record found for session", "session_id", session.ID)
		c.JSON(http.StatusOK, gin.H{
			"message": "No payment record found for session",
		})
		return
	}

	// Check if payment is already successful - but allow processing for overpayment reconciliation
	if payment.PaymentStatus == types.PaymentStatusSucceeded {
		h.logger.Infow("payment is already successful, but will proceed to check for overpayment",
			"payment_id", payment.ID,
			"session_id", session.ID,
			"current_status", payment.PaymentStatus)
		// Continue processing to handle potential overpayment instead of returning
	}

	// Make a separate Stripe API call to get current status
	paymentStatusResp, err := h.stripeService.GetPaymentStatus(c.Request.Context(), session.ID, environmentID)
	if err != nil {
		h.logger.Errorw("failed to get payment status from Stripe API",
			"error", err,
			"session_id", session.ID,
			"payment_id", payment.ID)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to get payment status from Stripe",
		})
		return
	}

	h.logger.Infow("retrieved payment status from Stripe API",
		"session_id", session.ID,
		"payment_intent_id", paymentStatusResp.PaymentIntentID,
		"payment_method_id", paymentStatusResp.PaymentMethodID,
		"status", paymentStatusResp.Status,
		"amount", paymentStatusResp.Amount.String(),
		"currency", paymentStatusResp.Currency,
		"payment_id", payment.ID,
	)

	// Determine payment status based on Stripe API response
	var paymentStatus string
	var succeededAt *time.Time
	var failedAt *time.Time
	var errorMessage *string

	switch paymentStatusResp.Status {
	case "complete", "succeeded":
		paymentStatus = string(types.PaymentStatusSucceeded)
		now := time.Now()
		succeededAt = &now
	case "failed", "canceled":
		paymentStatus = string(types.PaymentStatusFailed)
		now := time.Now()
		failedAt = &now
		errMsg := "Payment failed or canceled"
		errorMessage = &errMsg
	case "pending", "processing":
		paymentStatus = string(types.PaymentStatusPending)
	default:
		paymentStatus = string(types.PaymentStatusPending)
	}

	// Update payment with data from Stripe API
	paymentService := service.NewPaymentService(h.stripeService.ServiceParams)
	updateReq := dto.UpdatePaymentRequest{
		PaymentStatus: &paymentStatus,
	}

	// Update payment method ID with payment intent ID (pi_ prefixed)
	if paymentStatusResp.PaymentIntentID != "" {
		updateReq.GatewayPaymentID = &paymentStatusResp.PaymentIntentID
	}

	// Update payment method ID with pm_ prefixed ID from Stripe
	if paymentStatusResp.PaymentMethodID != "" {
		updateReq.PaymentMethodID = &paymentStatusResp.PaymentMethodID
	}

	// Set succeeded_at or failed_at based on status
	if succeededAt != nil {
		updateReq.SucceededAt = succeededAt
	}
	if failedAt != nil {
		updateReq.FailedAt = failedAt
	}
	if errorMessage != nil {
		updateReq.ErrorMessage = errorMessage
	}

	_, err = paymentService.UpdatePayment(c.Request.Context(), payment.ID, updateReq)
	if err != nil {
		h.logger.Errorw("failed to update payment status", "error", err, "payment_id", payment.ID)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to update payment status",
		})
		return
	}

	// Reconcile payment with invoice if payment succeeded or if payment was already succeeded
	// This handles both new payments and duplicate payments that should result in overpayment
	if paymentStatus == string(types.PaymentStatusSucceeded) || payment.PaymentStatus == types.PaymentStatusSucceeded {
		if err := h.stripeService.ReconcilePaymentWithInvoice(c.Request.Context(), payment.ID, payment.Amount); err != nil {
			h.logger.Errorw("failed to reconcile payment with invoice",
				"error", err,
				"payment_id", payment.ID,
				"invoice_id", payment.DestinationID,
			)
			// Don't fail the webhook if reconciliation fails, but log the error
			// The payment is still marked as succeeded
		}
	}

	h.logger.Infow("successfully updated payment status from async webhook",
		"payment_id", payment.ID,
		"session_id", session.ID,
		"payment_intent_id", paymentStatusResp.PaymentIntentID,
		"payment_method_id", paymentStatusResp.PaymentMethodID,
		"status", paymentStatus,
		"environment_id", environmentID,
	)
}

// handleCheckoutSessionAsyncPaymentFailed handles checkout.session.async_payment_failed webhook
func (h *WebhookHandler) handleCheckoutSessionAsyncPaymentFailed(c *gin.Context, event *stripe.Event, environmentID string) {
	var session stripe.CheckoutSession
	err := json.Unmarshal(event.Data.Raw, &session)
	if err != nil {
		h.logger.Errorw("failed to parse checkout session from webhook", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Failed to parse checkout session data",
		})
		return
	}

	// Log the webhook data correctly
	h.logger.Infow("received checkout.session.async_payment_failed webhook",
		"session_id", session.ID,
		"status", session.Status,
		"environment_id", environmentID,
		"event_id", event.ID,
		"event_type", event.Type,
		"has_payment_intent", session.PaymentIntent != nil,
		"payment_intent_id", func() string {
			if session.PaymentIntent != nil {
				return session.PaymentIntent.ID
			}
			return ""
		}(),
	)

	// Find payment record by session ID
	payment, err := h.findPaymentBySessionID(c.Request.Context(), session.ID)
	if err != nil {
		h.logger.Errorw("failed to find payment record", "error", err, "session_id", session.ID)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to find payment record",
		})
		return
	}

	if payment == nil {
		h.logger.Warnw("no payment record found for session", "session_id", session.ID)
		c.JSON(http.StatusOK, gin.H{
			"message": "No payment record found for session",
		})
		return
	}

	// Check if payment is already successful - prevent any updates to successful payments
	if payment.PaymentStatus == types.PaymentStatusSucceeded {
		h.logger.Warnw("payment is already successful, skipping update",
			"payment_id", payment.ID,
			"session_id", session.ID,
			"current_status", payment.PaymentStatus)
		c.JSON(http.StatusOK, gin.H{
			"message": "Payment is already successful, no update needed",
		})
		return
	}

	// Make a separate Stripe API call to get current status
	paymentStatusResp, err := h.stripeService.GetPaymentStatus(c.Request.Context(), session.ID, environmentID)
	if err != nil {
		h.logger.Errorw("failed to get payment status from Stripe API",
			"error", err,
			"session_id", session.ID,
			"payment_id", payment.ID)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to get payment status from Stripe",
		})
		return
	}

	h.logger.Infow("retrieved payment status from Stripe API",
		"session_id", session.ID,
		"payment_intent_id", paymentStatusResp.PaymentIntentID,
		"payment_method_id", paymentStatusResp.PaymentMethodID,
		"status", paymentStatusResp.Status,
		"amount", paymentStatusResp.Amount.String(),
		"currency", paymentStatusResp.Currency,
		"payment_id", payment.ID,
	)

	// Determine payment status based on Stripe API response
	var paymentStatus string
	var succeededAt *time.Time
	var failedAt *time.Time
	var errorMessage *string

	switch paymentStatusResp.Status {
	case "complete", "succeeded":
		paymentStatus = string(types.PaymentStatusSucceeded)
		now := time.Now()
		succeededAt = &now
	case "failed", "canceled":
		paymentStatus = string(types.PaymentStatusFailed)
		now := time.Now()
		failedAt = &now
		errMsg := "Payment failed or canceled"
		errorMessage = &errMsg
	case "pending", "processing":
		paymentStatus = string(types.PaymentStatusPending)
	default:
		paymentStatus = string(types.PaymentStatusPending)
	}

	// Update payment with data from Stripe API
	paymentService := service.NewPaymentService(h.stripeService.ServiceParams)
	updateReq := dto.UpdatePaymentRequest{
		PaymentStatus: &paymentStatus,
	}

	// Update payment method ID with payment intent ID (pi_ prefixed)
	if paymentStatusResp.PaymentIntentID != "" {
		updateReq.GatewayPaymentID = &paymentStatusResp.PaymentIntentID
	}

	// Update payment method ID with pm_ prefixed ID from Stripe
	if paymentStatusResp.PaymentMethodID != "" {
		updateReq.PaymentMethodID = &paymentStatusResp.PaymentMethodID
	}

	// Set succeeded_at or failed_at based on status
	if succeededAt != nil {
		updateReq.SucceededAt = succeededAt
	}
	if failedAt != nil {
		updateReq.FailedAt = failedAt
	}
	if errorMessage != nil {
		updateReq.ErrorMessage = errorMessage
	}

	_, err = paymentService.UpdatePayment(c.Request.Context(), payment.ID, updateReq)
	if err != nil {
		h.logger.Errorw("failed to update payment status", "error", err, "payment_id", payment.ID)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to update payment status",
		})
		return
	}

	// Reconcile payment with invoice if payment succeeded
	if paymentStatus == string(types.PaymentStatusSucceeded) {
		if err := h.stripeService.ReconcilePaymentWithInvoice(c.Request.Context(), payment.ID, payment.Amount); err != nil {
			h.logger.Errorw("failed to reconcile payment with invoice",
				"error", err,
				"payment_id", payment.ID,
				"invoice_id", payment.DestinationID,
			)
			// Don't fail the webhook if reconciliation fails, but log the error
			// The payment is still marked as succeeded
		}
	}

	h.logger.Infow("successfully updated payment status from failed webhook",
		"payment_id", payment.ID,
		"session_id", session.ID,
		"payment_intent_id", paymentStatusResp.PaymentIntentID,
		"payment_method_id", paymentStatusResp.PaymentMethodID,
		"status", paymentStatus,
		"environment_id", environmentID,
	)
}

// handleCheckoutSessionExpired handles checkout.session.expired webhook
func (h *WebhookHandler) handleCheckoutSessionExpired(c *gin.Context, event *stripe.Event, environmentID string) {
	var session stripe.CheckoutSession
	err := json.Unmarshal(event.Data.Raw, &session)
	if err != nil {
		h.logger.Errorw("failed to parse checkout session from webhook", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Failed to parse checkout session data",
		})
		return
	}

	h.logger.Infow("received checkout.session.expired webhook",
		"session_id", session.ID,
		"status", session.Status,
		"environment_id", environmentID,
		"event_id", event.ID,
	)

	// Find payment record by session ID
	payment, err := h.findPaymentBySessionID(c.Request.Context(), session.ID)
	if err != nil {
		h.logger.Errorw("failed to find payment record", "error", err, "session_id", session.ID)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to find payment record",
		})
		return
	}

	if payment == nil {
		h.logger.Warnw("no payment record found for session", "session_id", session.ID)
		c.JSON(http.StatusOK, gin.H{
			"message": "No payment record found for session",
		})
		return
	}

	// Skip if payment is already in a final state (succeeded, failed, refunded)
	if payment.PaymentStatus == types.PaymentStatusSucceeded ||
		payment.PaymentStatus == types.PaymentStatusFailed ||
		payment.PaymentStatus == types.PaymentStatusRefunded ||
		payment.PaymentStatus == types.PaymentStatusPartiallyRefunded {
		h.logger.Infow("payment is already in final state, skipping expiration update",
			"payment_id", payment.ID,
			"session_id", session.ID,
			"current_status", payment.PaymentStatus)
		c.JSON(http.StatusOK, gin.H{
			"message": "Payment is already in final state, no update needed",
		})
		return
	}

	// Only update pending/initiated payments to failed status (expired)
	paymentService := service.NewPaymentService(h.stripeService.ServiceParams)
	now := time.Now()
	failedStatus := string(types.PaymentStatusFailed)
	updateReq := dto.UpdatePaymentRequest{
		PaymentStatus: &failedStatus,
		FailedAt:      &now,
		ErrorMessage:  lo.ToPtr("Payment session expired"),
	}

	_, err = paymentService.UpdatePayment(c.Request.Context(), payment.ID, updateReq)
	if err != nil {
		h.logger.Errorw("failed to update payment to expired status", "error", err, "payment_id", payment.ID)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to update payment status",
		})
		return
	}

	h.logger.Infow("successfully marked payment as expired",
		"payment_id", payment.ID,
		"session_id", session.ID,
		"previous_status", payment.PaymentStatus,
		"new_status", failedStatus,
		"environment_id", environmentID,
	)
}

// handlePaymentIntentPaymentFailed handles payment_intent.payment_failed webhook
func (h *WebhookHandler) handlePaymentIntentPaymentFailed(c *gin.Context, event *stripe.Event, environmentID string) {
	var paymentIntent stripe.PaymentIntent
	err := json.Unmarshal(event.Data.Raw, &paymentIntent)
	if err != nil {
		h.logger.Errorw("failed to parse payment intent from webhook", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Failed to parse payment intent data",
		})
		return
	}

	// Step 1: Log the webhook data
	h.logger.Infow("received payment_intent.payment_failed webhook",
		"payment_intent_id", paymentIntent.ID,
		"status", paymentIntent.Status,
		"environment_id", environmentID,
		"event_id", event.ID,
		"event_type", event.Type,
		"last_payment_error", func() string {
			if paymentIntent.LastPaymentError != nil {
				return paymentIntent.LastPaymentError.Error()
			}
			return ""
		}(),
		"metadata", paymentIntent.Metadata,
	)

	// Step 2: Get session ID by calling Stripe API to lookup checkout session with payment intent ID
	sessionID, err := h.getSessionIDFromPaymentIntent(c.Request.Context(), paymentIntent.ID, environmentID)
	if err != nil {
		h.logger.Errorw("failed to get session ID from payment intent", "error", err, "payment_intent_id", paymentIntent.ID)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to get session ID from payment intent",
		})
		return
	}

	h.logger.Infow("found session ID for payment intent", "payment_intent_id", paymentIntent.ID, "session_id", sessionID)

	// Step 3: Find payment record by session ID in gateway_tracking_id
	payment, err := h.findPaymentBySessionID(c.Request.Context(), sessionID)
	if err != nil {
		h.logger.Errorw("failed to find payment record", "error", err, "session_id", sessionID)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to find payment record",
		})
		return
	}

	if payment == nil {
		h.logger.Warnw("no payment record found for session", "session_id", sessionID, "payment_intent_id", paymentIntent.ID)
		c.JSON(http.StatusOK, gin.H{
			"message": "No payment record found for session",
		})
		return
	}

	h.logger.Infow("found payment record", "payment_id", payment.ID, "session_id", sessionID)

	// Check if payment is already successful - prevent any updates to successful payments
	if payment.PaymentStatus == types.PaymentStatusSucceeded {
		h.logger.Warnw("payment is already successful, skipping update",
			"payment_id", payment.ID,
			"session_id", sessionID,
			"payment_intent_id", paymentIntent.ID,
			"current_status", payment.PaymentStatus)
		c.JSON(http.StatusOK, gin.H{
			"message": "Payment is already successful, no update needed",
		})
		return
	}

	// Step 4: Get payment_intent_id from webhook response and call Stripe API to get latest response
	paymentStatusResp, err := h.stripeService.GetPaymentStatusByPaymentIntent(c.Request.Context(), paymentIntent.ID, environmentID)
	if err != nil {
		h.logger.Errorw("failed to get payment status from Stripe API",
			"error", err,
			"payment_intent_id", paymentIntent.ID)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to get payment status from Stripe",
		})
		return
	}

	h.logger.Infow("retrieved payment status from Stripe API",
		"payment_intent_id", paymentIntent.ID,
		"payment_method_id", paymentStatusResp.PaymentMethodID,
		"status", paymentStatusResp.Status,
		"amount", paymentStatusResp.Amount.String(),
		"currency", paymentStatusResp.Currency,
	)

	// Step 5: Update our DB columns according to Stripe API response
	var paymentStatus string
	var failedAt *time.Time
	var errorMessage *string

	switch paymentStatusResp.Status {
	case "failed", "canceled", "requires_payment_method":
		paymentStatus = string(types.PaymentStatusFailed)
		now := time.Now()
		failedAt = &now
		errMsg := "Payment failed or canceled"
		errorMessage = &errMsg
	case "pending", "processing":
		paymentStatus = string(types.PaymentStatusPending)
	default:
		paymentStatus = string(types.PaymentStatusFailed)
		now := time.Now()
		failedAt = &now
		errMsg := "Payment failed"
		errorMessage = &errMsg
	}

	// Update payment with data from Stripe API
	paymentService := service.NewPaymentService(h.stripeService.ServiceParams)
	updateReq := dto.UpdatePaymentRequest{
		PaymentStatus: &paymentStatus,
	}

	// Update gateway_payment_id with pi_ prefixed payment intent ID
	updateReq.GatewayPaymentID = &paymentIntent.ID

	// Update payment_method_id with pm_ prefixed payment method ID from Stripe
	if paymentStatusResp.PaymentMethodID != "" {
		updateReq.PaymentMethodID = &paymentStatusResp.PaymentMethodID
	}

	// Set failed_at and error_message
	if failedAt != nil {
		updateReq.FailedAt = failedAt
	}
	if errorMessage != nil {
		updateReq.ErrorMessage = errorMessage
	}

	_, err = paymentService.UpdatePayment(c.Request.Context(), payment.ID, updateReq)
	if err != nil {
		h.logger.Errorw("failed to update payment status", "error", err, "payment_id", payment.ID)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to update payment status",
		})
		return
	}

	h.logger.Infow("successfully updated payment status from payment_intent.payment_failed webhook",
		"payment_id", payment.ID,
		"payment_intent_id", paymentIntent.ID,
		"payment_method_id", paymentStatusResp.PaymentMethodID,
		"status", paymentStatus,
		"environment_id", environmentID,
	)

	c.JSON(http.StatusOK, gin.H{
		"message": "Payment intent payment failed webhook processed successfully",
	})
}
