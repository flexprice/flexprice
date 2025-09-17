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
	"github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/svix"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
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

	// Parse webhook event without signature verification (TEMPORARY)
	// event := &stripe.Event{}
	// err = json.Unmarshal(body, event)
	// if err != nil {
	// 	h.logger.Errorw("failed to parse webhook event JSON",
	// 		"error", err,
	// 		"body_length", len(body),
	// 		"environment_id", environmentID)
	// 	c.JSON(http.StatusBadRequest, gin.H{
	// 		"error":   "Failed to parse webhook event JSON",
	// 		"details": err.Error(),
	// 	})
	// 	return
	// }

	// h.logger.Infow("successfully parsed webhook event (without signature verification)",
	// 	"event_type", event.Type,
	// 	"event_id", event.ID,
	// 	"environment_id", environmentID)

	// Log the event type for debugging
	// h.logger.Infow("processing webhook event",
	// 	"event_type", event.Type,
	// 	"event_id", event.ID,
	// 	"environment_id", environmentID,
	// 	"tenant_id", tenantID,
	// 	"event_created", event.Created,
	// 	"event_livemode", event.Livemode)

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
	case string(types.WebhookEventTypeInvoicePaymentPaid):
		h.handleInvoicePaymentPaid(c, event, environmentID)
	case string(types.WebhookEventTypeSetupIntentSucceeded):
		h.handleSetupIntentSucceeded(c, event, environmentID)

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

	iter := stripeClient.V1CheckoutSessions.List(ctx, params)
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

// attachPaymentToStripeInvoice attaches a payment from a checkout session to the corresponding Stripe invoice
func (h *WebhookHandler) attachPaymentToStripeInvoice(ctx context.Context, session stripe.CheckoutSession, payment *dto.PaymentResponse) error {
	if session.PaymentIntent == nil {
		h.logger.Debugw("no payment intent in checkout session, skipping Stripe invoice attachment",
			"session_id", session.ID,
			"payment_id", payment.ID)
		return nil
	}

	paymentIntentID := session.PaymentIntent.ID

	// Find Stripe invoice ID using entity integration mapping (same pattern as StripeService)
	mappingService := service.NewEntityIntegrationMappingService(service.ServiceParams{
		Logger:                       h.logger,
		EntityIntegrationMappingRepo: h.stripeService.EntityIntegrationMappingRepo,
	})

	filter := &types.EntityIntegrationMappingFilter{
		EntityIDs:     []string{payment.DestinationID},
		EntityType:    "invoice",
		ProviderTypes: []string{"stripe"},
	}

	mappings, err := mappingService.GetEntityIntegrationMappings(ctx, filter)
	if err != nil || len(mappings.Items) == 0 {
		h.logger.Debugw("no Stripe invoice found for FlexPrice invoice, skipping attachment",
			"flexprice_invoice_id", payment.DestinationID,
			"payment_id", payment.ID,
			"error", err)
		return nil
	}

	stripeInvoiceID := mappings.Items[0].ProviderEntityID

	// Create Stripe client (same pattern as other StripeService methods)
	conn, err := h.stripeService.ConnectionRepo.GetByProvider(ctx, types.SecretProviderStripe)
	if err != nil {
		return err
	}

	stripeConfig, err := h.stripeService.GetDecryptedStripeConfig(conn)
	if err != nil {
		return err
	}

	stripeClient := stripe.NewClient(stripeConfig.SecretKey, nil)

	// Attach the payment intent to the Stripe invoice
	if err := h.stripeService.AttachPaymentToStripeInvoice(ctx, stripeClient, paymentIntentID, stripeInvoiceID); err != nil {
		return err
	}

	h.logger.Infow("successfully attached payment to Stripe invoice",
		"payment_id", payment.ID,
		"flexprice_invoice_id", payment.DestinationID,
		"stripe_invoice_id", stripeInvoiceID,
		"payment_intent_id", paymentIntentID,
		"session_id", session.ID)

	return nil
}

// isPaymentFromCheckoutSession checks if a payment intent is from a checkout session
func (h *WebhookHandler) isPaymentFromCheckoutSession(ctx context.Context, paymentIntentID, environmentID string) bool {
	// Direct query by gateway_payment_id and payment_method_type - much more efficient!
	paymentService := service.NewPaymentService(h.stripeService.ServiceParams)
	paymentMethodType := string(types.PaymentMethodTypePaymentLink)

	// Query directly by gateway_payment_id and payment_method_type
	payments, err := paymentService.ListPayments(ctx, &types.PaymentFilter{
		GatewayPaymentID:  &paymentIntentID,
		PaymentMethodType: &paymentMethodType,
		QueryFilter: &types.QueryFilter{
			Limit: lo.ToPtr(1), // Only need to check if one exists
		},
	})
	if err != nil {
		h.logger.Debugw("failed to check for existing payment records",
			"payment_intent_id", paymentIntentID,
			"error", err)
		return false
	}

	// If we found any PAYMENT_LINK payment with this gateway_payment_id, it's from checkout session
	if len(payments.Items) > 0 {
		h.logger.Debugw("found existing PAYMENT_LINK payment for this payment intent",
			"payment_id", payments.Items[0].ID,
			"payment_intent_id", paymentIntentID,
			"payment_method_type", payments.Items[0].PaymentMethodType)
		return true
	}

	return false
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

		// Attach payment to Stripe invoice if payment succeeded and invoice is synced to Stripe
		if paymentStatus == string(types.PaymentStatusSucceeded) {
			if err := h.attachPaymentToStripeInvoice(c.Request.Context(), session, payment); err != nil {
				h.logger.Errorw("failed to attach payment to Stripe invoice",
					"error", err,
					"payment_id", payment.ID,
					"invoice_id", payment.DestinationID,
					"session_id", session.ID,
				)
				// Don't fail the webhook if attachment fails, but log the error
				// The payment reconciliation was successful
			}
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

// handleInvoicePaymentPaid handles the invoice_payment.paid webhook event
// This is triggered when a card payment is successfully made to a Stripe invoice
// We use this to sync any FlexPrice credit payments as "paid out of band"
func (h *WebhookHandler) handleInvoicePaymentPaid(c *gin.Context, event *stripe.Event, environmentID string) {
	h.logger.Infow("handling invoice_payment.paid webhook event",
		"event_id", event.ID,
		"environment_id", environmentID)

	// Parse the invoice payment object
	var invoicePayment stripe.InvoicePayment
	if err := json.Unmarshal(event.Data.Raw, &invoicePayment); err != nil {
		h.logger.Errorw("failed to parse invoice payment from webhook",
			"error", err,
			"event_id", event.ID)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Failed to parse invoice payment data",
		})
		return
	}

	// From the webhook payload, invoice is a string ID
	// Parse it manually from the raw JSON since the struct might not match exactly
	var rawData map[string]interface{}
	if err := json.Unmarshal(event.Data.Raw, &rawData); err != nil {
		h.logger.Errorw("failed to parse raw webhook data",
			"error", err,
			"event_id", event.ID)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Failed to parse webhook data",
		})
		return
	}

	stripeInvoiceID := ""
	if invoiceID, exists := rawData["invoice"]; exists {
		if invoiceStr, ok := invoiceID.(string); ok {
			stripeInvoiceID = invoiceStr
		}
	}
	if stripeInvoiceID == "" {
		h.logger.Errorw("no invoice ID in invoice payment webhook",
			"event_id", event.ID)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "No invoice ID in webhook data",
		})
		return
	}

	h.logger.Infow("processing invoice payment webhook",
		"stripe_invoice_id", stripeInvoiceID,
		"amount_paid", invoicePayment.AmountPaid,
		"currency", invoicePayment.Currency,
		"event_id", event.ID)

	// Set context with environment ID
	ctx := types.SetEnvironmentID(c.Request.Context(), environmentID)

	// Get payment intent details to check payment source
	paymentIntentID := ""
	if invoicePayment.Payment != nil && invoicePayment.Payment.PaymentIntent != nil {
		paymentIntentID = invoicePayment.Payment.PaymentIntent.ID
	}

	if paymentIntentID == "" {
		h.logger.Warnw("no payment intent found in invoice payment",
			"stripe_invoice_id", stripeInvoiceID,
			"event_id", event.ID)
		c.JSON(http.StatusOK, gin.H{
			"message": "No payment intent found",
		})
		return
	}

	// Get payment intent details from Stripe
	paymentIntent, err := h.stripeService.GetPaymentIntent(ctx, paymentIntentID, environmentID)
	if err != nil {
		h.logger.Errorw("failed to get payment intent from Stripe",
			"error", err,
			"payment_intent_id", paymentIntentID,
			"stripe_invoice_id", stripeInvoiceID)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to get payment intent details",
		})
		return
	}

	// Check if this payment is from a checkout session (already processed by checkout.session.completed)
	// Skip processing to avoid double reconciliation
	if h.isPaymentFromCheckoutSession(ctx, paymentIntentID, environmentID) {
		h.logger.Infow("payment is from checkout session, skipping invoice_payment.paid processing to avoid double reconciliation",
			"payment_intent_id", paymentIntentID,
			"stripe_invoice_id", stripeInvoiceID,
			"event_id", event.ID)
		c.JSON(http.StatusOK, gin.H{
			"message": "Payment from checkout session already processed",
		})
		return
	}

	// Check payment source to determine processing approach
	paymentSource := paymentIntent.Metadata["payment_source"]
	if paymentSource == "flexprice" {
		// This is a FlexPrice-initiated payment - sync credit payments
		h.logger.Infow("processing FlexPrice-initiated payment - syncing credit payments",
			"payment_intent_id", paymentIntentID,
			"stripe_invoice_id", stripeInvoiceID)

		if err := h.syncFlexPriceCreditPayments(ctx, stripeInvoiceID); err != nil {
			h.logger.Errorw("failed to sync FlexPrice credit payments",
				"error", err,
				"stripe_invoice_id", stripeInvoiceID,
				"event_id", event.ID)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to sync credit payments",
			})
			return
		}
	} else {
		// This is an external Stripe payment - process reconciliation
		h.logger.Infow("processing external Stripe payment - reconciling invoice",
			"payment_intent_id", paymentIntentID,
			"stripe_invoice_id", stripeInvoiceID)

		if err := h.stripeService.ProcessExternalStripePayment(ctx, paymentIntent, stripeInvoiceID); err != nil {
			h.logger.Errorw("failed to process external Stripe payment",
				"error", err,
				"payment_intent_id", paymentIntentID,
				"stripe_invoice_id", stripeInvoiceID)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to process external payment",
			})
			return
		}
	}

	h.logger.Infow("successfully processed invoice payment webhook",
		"stripe_invoice_id", stripeInvoiceID,
		"event_id", event.ID)
}

// syncFlexPriceCreditPayments finds and syncs all FlexPrice credit payments for a Stripe invoice
func (h *WebhookHandler) syncFlexPriceCreditPayments(ctx context.Context, stripeInvoiceID string) error {
	// Step 1: Find the FlexPrice invoice ID from the Stripe invoice mapping
	flexInvoiceID, err := h.getFlexPriceInvoiceID(ctx, stripeInvoiceID)
	if err != nil {
		return err
	}

	if flexInvoiceID == "" {
		h.logger.Infow("no FlexPrice invoice found for Stripe invoice",
			"stripe_invoice_id", stripeInvoiceID)
		return nil // Not an error, just no FlexPrice invoice to sync
	}

	// Step 2: Get all successful credit payments for this FlexPrice invoice
	creditPayments, err := h.getSuccessfulCreditPayments(ctx, flexInvoiceID)
	if err != nil {
		return err
	}

	if len(creditPayments) == 0 {
		h.logger.Infow("no credit payments found for FlexPrice invoice",
			"flexprice_invoice_id", flexInvoiceID,
			"stripe_invoice_id", stripeInvoiceID)
		return nil
	}

	h.logger.Infow("syncing credit payments to Stripe invoice",
		"flexprice_invoice_id", flexInvoiceID,
		"stripe_invoice_id", stripeInvoiceID,
		"credit_payments_count", len(creditPayments))

	// Step 3: Initialize StripeInvoiceSyncService to sync payments
	stripeInvoiceSyncService := service.NewStripeInvoiceSyncService(service.ServiceParams{
		Logger:                       h.logger,
		Config:                       h.config,
		ConnectionRepo:               h.stripeService.ConnectionRepo,
		EntityIntegrationMappingRepo: h.stripeService.EntityIntegrationMappingRepo,
		InvoiceRepo:                  h.stripeService.InvoiceRepo,
		CustomerRepo:                 h.stripeService.CustomerRepo,
		PaymentRepo:                  h.stripeService.PaymentRepo,
	})

	// Step 4: Calculate total credit amount from all payments
	var totalCreditAmount decimal.Decimal
	var creditPaymentIDs []string

	for _, creditPayment := range creditPayments {
		// Skip if payment is already from Stripe (avoid double sync)
		if creditPayment.PaymentGateway != nil && *creditPayment.PaymentGateway == string(types.PaymentGatewayTypeStripe) {
			h.logger.Debugw("credit payment already from Stripe, skipping sync",
				"payment_id", creditPayment.ID)
			continue
		}

		totalCreditAmount = totalCreditAmount.Add(creditPayment.Amount)
		creditPaymentIDs = append(creditPaymentIDs, creditPayment.ID)
	}

	// Only proceed if we have credits to sync
	if totalCreditAmount.GreaterThan(decimal.Zero) {
		h.logger.Infow("syncing total credit amount to Stripe as paid out of band",
			"flexprice_invoice_id", flexInvoiceID,
			"stripe_invoice_id", stripeInvoiceID,
			"total_credit_amount", totalCreditAmount,
			"credit_payments_count", len(creditPaymentIDs))

		// Sync the total credit amount to Stripe as paid out of band (single operation)
		err = stripeInvoiceSyncService.SyncPaymentToStripe(ctx, flexInvoiceID, totalCreditAmount, "flexprice_credits", nil)
		if err != nil {
			h.logger.Errorw("failed to sync total credit amount to Stripe",
				"error", err,
				"flexprice_invoice_id", flexInvoiceID,
				"stripe_invoice_id", stripeInvoiceID,
				"total_credit_amount", totalCreditAmount)
			return err
		}
	}

	h.logger.Infow("successfully synced all credit payments to Stripe",
		"flexprice_invoice_id", flexInvoiceID,
		"stripe_invoice_id", stripeInvoiceID,
		"total_credit_amount", totalCreditAmount,
		"credit_payments_synced", len(creditPayments))

	return nil
}

// getFlexPriceInvoiceID finds the FlexPrice invoice ID from a Stripe invoice ID using entity mapping
func (h *WebhookHandler) getFlexPriceInvoiceID(ctx context.Context, stripeInvoiceID string) (string, error) {
	// Use the existing StripeService to access repositories
	mappingService := service.NewEntityIntegrationMappingService(service.ServiceParams{
		Logger:                       h.logger,
		EntityIntegrationMappingRepo: h.stripeService.EntityIntegrationMappingRepo,
	})

	// Find the mapping from Stripe invoice ID to FlexPrice invoice ID
	filter := &types.EntityIntegrationMappingFilter{
		ProviderEntityIDs: []string{stripeInvoiceID},
		EntityType:        "invoice",
		ProviderTypes:     []string{"stripe"},
	}

	mappings, err := mappingService.GetEntityIntegrationMappings(ctx, filter)
	if err != nil {
		return "", err
	}

	if len(mappings.Items) == 0 {
		return "", nil // No mapping found, not an error
	}

	return mappings.Items[0].EntityID, nil
}

// getSuccessfulCreditPayments gets all successful credit payments for a FlexPrice invoice
func (h *WebhookHandler) getSuccessfulCreditPayments(ctx context.Context, flexInvoiceID string) ([]*payment.Payment, error) {
	// Use the payment repository directly to get domain objects
	destinationType := string(types.PaymentDestinationTypeInvoice)
	paymentStatus := string(types.PaymentStatusSucceeded)
	creditPaymentType := string(types.PaymentMethodTypeCredits)

	filter := &types.PaymentFilter{
		DestinationType:   &destinationType,
		DestinationID:     &flexInvoiceID,
		PaymentStatus:     &paymentStatus,
		PaymentMethodType: &creditPaymentType,
	}

	// Use the repository directly to get payment domain objects
	payments, err := h.stripeService.PaymentRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	return payments, nil
}

// handleSetupIntentSucceeded handles setup_intent.succeeded webhook
func (h *WebhookHandler) handleSetupIntentSucceeded(c *gin.Context, event *stripe.Event, environmentID string) {
	var setupIntent stripe.SetupIntent
	err := json.Unmarshal(event.Data.Raw, &setupIntent)
	if err != nil {
		h.logger.Errorw("failed to parse setup intent from webhook", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Failed to parse setup intent data",
		})
		return
	}

	h.logger.Infow("received setup_intent.succeeded webhook",
		"setup_intent_id", setupIntent.ID,
		"status", setupIntent.Status,
		"environment_id", environmentID,
		"event_id", event.ID,
		"event_type", event.Type,
		"has_payment_method", setupIntent.PaymentMethod != nil,
		"payment_method_id", func() string {
			if setupIntent.PaymentMethod != nil {
				return setupIntent.PaymentMethod.ID
			}
			return ""
		}(),
		"metadata", setupIntent.Metadata,
	)

	// Check if setup intent succeeded and has a payment method
	if setupIntent.Status != stripe.SetupIntentStatusSucceeded || setupIntent.PaymentMethod == nil {
		h.logger.Infow("setup intent not in succeeded status or no payment method attached",
			"setup_intent_id", setupIntent.ID,
			"status", setupIntent.Status,
			"has_payment_method", setupIntent.PaymentMethod != nil)
		c.JSON(http.StatusOK, gin.H{
			"message": "Setup intent not succeeded or no payment method",
		})
		return
	}

	// Check if set_default was requested
	setAsDefault, exists := setupIntent.Metadata["set_default"]
	h.logger.Infow("checking set_default metadata",
		"setup_intent_id", setupIntent.ID,
		"set_default_exists", exists,
		"set_default_value", setAsDefault,
		"all_metadata", setupIntent.Metadata)

	if !exists || setAsDefault != "true" {
		h.logger.Infow("set_as_default not requested for this setup intent",
			"setup_intent_id", setupIntent.ID,
			"set_default_exists", exists,
			"set_default_value", setAsDefault)
		c.JSON(http.StatusOK, gin.H{
			"message": "Set as default not requested",
		})
		return
	}

	// Get customer ID from metadata
	customerID, exists := setupIntent.Metadata["customer_id"]
	if !exists {
		h.logger.Errorw("customer_id not found in setup intent metadata",
			"setup_intent_id", setupIntent.ID)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Customer ID not found in setup intent metadata",
		})
		return
	}

	// Set the payment method as default
	paymentMethodID := setupIntent.PaymentMethod.ID
	h.logger.Infow("attempting to set payment method as default",
		"setup_intent_id", setupIntent.ID,
		"customer_id", customerID,
		"payment_method_id", paymentMethodID)

	err = h.stripeService.SetDefaultPaymentMethod(c.Request.Context(), customerID, paymentMethodID)
	if err != nil {
		h.logger.Errorw("failed to set payment method as default",
			"error", err,
			"error_type", fmt.Sprintf("%T", err),
			"setup_intent_id", setupIntent.ID,
			"customer_id", customerID,
			"payment_method_id", paymentMethodID)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to set payment method as default",
			"details": err.Error(),
		})
		return
	}

	h.logger.Infow("successfully processed setup intent and set payment method as default",
		"setup_intent_id", setupIntent.ID,
		"customer_id", customerID,
		"payment_method_id", paymentMethodID)

	c.JSON(http.StatusOK, gin.H{

		"message": "Setup intent processed and payment method set as default",
	})
}
