package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	domainIntegration "github.com/flexprice/flexprice/internal/domain/integration"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

// StripeWebhookHandler handles incoming Stripe webhooks
type StripeWebhookHandler interface {
	ProcessWebhook(ctx context.Context, rawPayload []byte, signature string) (*webhookDto.StripeWebhookResponse, error)
	ValidateSignature(payload []byte, signature string, secret string) error
}

// stripeWebhookHandler implements StripeWebhookHandler
type stripeWebhookHandler struct {
	customerService          service.CustomerService
	customerMappingRepo      domainIntegration.EntityIntegrationMappingRepository
	stripeTenantConfigRepo   domainIntegration.StripeTenantConfigRepository
	meterProviderMappingRepo domainIntegration.MeterProviderMappingRepository
	config                   *config.Configuration
	logger                   *logger.Logger
}

// NewStripeWebhookHandler creates a new Stripe webhook handler
func NewStripeWebhookHandler(
	customerService service.CustomerService,
	customerMappingRepo domainIntegration.EntityIntegrationMappingRepository,
	stripeTenantConfigRepo domainIntegration.StripeTenantConfigRepository,
	meterProviderMappingRepo domainIntegration.MeterProviderMappingRepository,
	config *config.Configuration,
	logger *logger.Logger,
) StripeWebhookHandler {
	return &stripeWebhookHandler{
		customerService:          customerService,
		customerMappingRepo:      customerMappingRepo,
		stripeTenantConfigRepo:   stripeTenantConfigRepo,
		meterProviderMappingRepo: meterProviderMappingRepo,
		config:                   config,
		logger:                   logger,
	}
}

// ProcessWebhook processes incoming Stripe webhook events
func (h *stripeWebhookHandler) ProcessWebhook(ctx context.Context, rawPayload []byte, signature string) (*webhookDto.StripeWebhookResponse, error) {
	// Parse the webhook event
	var event webhookDto.StripeWebhookEvent
	if err := json.Unmarshal(rawPayload, &event); err != nil {
		h.logger.Errorw("failed to parse Stripe webhook payload",
			"error", err,
			"payload_size", len(rawPayload),
		)
		return webhookDto.NewStripeWebhookErrorResponse("Invalid JSON payload"),
			ierr.NewError("failed to parse webhook payload").
				WithHint("Webhook payload must be valid JSON").
				Mark(ierr.ErrValidation)
	}

	// Validate the event structure
	if err := event.Validate(); err != nil {
		h.logger.Errorw("invalid Stripe webhook event structure",
			"error", err,
			"event_id", event.ID,
			"event_type", event.Type,
		)
		return webhookDto.NewStripeWebhookErrorResponse("Invalid event structure"), err
	}

	h.logger.Infow("received Stripe webhook",
		"event_id", event.ID,
		"event_type", event.Type,
		"livemode", event.Livemode,
	)

	// Handle different event types
	switch {
	case event.IsCustomerCreated():
		return h.handleCustomerCreated(ctx, &event, rawPayload, signature)
	case event.IsCustomerUpdated():
		return h.handleCustomerUpdated(ctx, &event)
	case event.IsCustomerDeleted():
		return h.handleCustomerDeleted(ctx, &event)
	default:
		h.logger.Debugw("unsupported Stripe webhook event type",
			"event_type", event.Type,
			"event_id", event.ID,
		)
		// Return success for unsupported events to avoid retries
		return webhookDto.NewStripeWebhookResponse(), nil
	}
}

// handleCustomerCreated handles customer.created webhook events
func (h *stripeWebhookHandler) handleCustomerCreated(ctx context.Context, event *webhookDto.StripeWebhookEvent, rawPayload []byte, signature string) (*webhookDto.StripeWebhookResponse, error) {
	// Extract customer data from the event
	customerData, err := event.GetCustomerData()
	if err != nil {
		h.logger.Errorw("failed to extract customer data from webhook",
			"error", err,
			"event_id", event.ID,
		)
		return webhookDto.NewStripeWebhookErrorResponse("Failed to extract customer data"), err
	}

	// Convert to processed customer
	processedCustomer := customerData.ToProcessedCustomer()

	// Validate processed customer data
	if err := processedCustomer.Validate(); err != nil {
		h.logger.Errorw("invalid customer data from Stripe webhook",
			"error", err,
			"stripe_customer_id", customerData.ID,
			"event_id", event.ID,
		)
		return webhookDto.NewStripeWebhookErrorResponse("Invalid customer data"), err
	}

	// Validate that required tenant and environment metadata is present
	if processedCustomer.TenantID == "" || processedCustomer.EnvironmentID == "" {
		h.logger.Errorw("missing required metadata in Stripe customer",
			"stripe_customer_id", customerData.ID,
			"tenant_id", processedCustomer.TenantID,
			"environment_id", processedCustomer.EnvironmentID,
			"event_id", event.ID,
		)
		return webhookDto.NewStripeWebhookErrorResponse("Missing required tenant_id or environment_id in customer metadata"),
			ierr.NewError("missing required metadata").
				WithHint("Stripe customer must have tenant_id and environment_id in metadata").
				Mark(ierr.ErrValidation)
	}

	// Set tenant and environment context
	ctx = context.WithValue(ctx, types.CtxTenantID, processedCustomer.TenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, processedCustomer.EnvironmentID)

	// Get tenant Stripe configuration for signature validation
	tenantConfig, err := h.stripeTenantConfigRepo.GetByTenantAndEnvironment(ctx, processedCustomer.TenantID, processedCustomer.EnvironmentID)
	if err != nil {
		h.logger.Errorw("failed to get tenant Stripe configuration",
			"error", err,
			"tenant_id", processedCustomer.TenantID,
			"environment_id", processedCustomer.EnvironmentID,
			"event_id", event.ID,
		)
		return webhookDto.NewStripeWebhookErrorResponse("Stripe integration not configured"), err
	}

	// Extract webhook secret from config
	webhookSecret := ""
	if tenantConfig.WebhookConfig != nil {
		if secret, ok := tenantConfig.WebhookConfig["webhook_secret"].(string); ok {
			webhookSecret = secret
		}
	}

	if webhookSecret == "" {
		h.logger.Errorw("webhook secret not configured",
			"tenant_id", processedCustomer.TenantID,
			"environment_id", processedCustomer.EnvironmentID,
			"event_id", event.ID,
		)
		return webhookDto.NewStripeWebhookErrorResponse("Webhook secret not configured"),
			ierr.NewError("webhook secret not configured").
				WithHint("Stripe webhook secret must be configured for this tenant").
				Mark(ierr.ErrValidation)
	}

	// Validate webhook signature
	if err := h.ValidateSignature(rawPayload, signature, webhookSecret); err != nil {
		h.logger.Errorw("invalid Stripe webhook signature",
			"error", err,
			"event_id", event.ID,
			"tenant_id", processedCustomer.TenantID,
		)
		return webhookDto.NewStripeWebhookErrorResponse("Invalid webhook signature"), err
	}

	h.logger.Infow("processing customer.created webhook",
		"stripe_customer_id", customerData.ID,
		"external_id", processedCustomer.ExternalID,
		"tenant_id", processedCustomer.TenantID,
		"environment_id", processedCustomer.EnvironmentID,
		"event_id", event.ID,
	)

	// Check if customer mapping already exists
	existingMapping, err := h.customerMappingRepo.GetByProviderEntityID(ctx, domainIntegration.ProviderTypeStripe, customerData.ID)
	if err != nil && !ierr.IsNotFound(err) {
		h.logger.Errorw("failed to check existing customer mapping",
			"error", err,
			"stripe_customer_id", customerData.ID,
			"event_id", event.ID,
		)
		return webhookDto.NewStripeWebhookErrorResponse("Database error"), err
	}

	if existingMapping != nil {
		h.logger.Infow("customer mapping already exists, updating metadata",
			"stripe_customer_id", customerData.ID,
			"flexprice_customer_id", existingMapping.EntityID,
			"event_id", event.ID,
		)

		// Update existing mapping metadata
		for k, v := range processedCustomer.Metadata {
			existingMapping.Metadata[k] = v
		}
		if err := h.customerMappingRepo.Update(ctx, existingMapping); err != nil {
			h.logger.Errorw("failed to update existing customer mapping",
				"error", err,
				"stripe_customer_id", customerData.ID,
				"event_id", event.ID,
			)
			return webhookDto.NewStripeWebhookErrorResponse("Failed to update customer mapping"), err
		}

		return webhookDto.NewStripeWebhookResponse(), nil
	}

	// Create or lookup FlexPrice customer
	flexpriceCustomerID, err := h.findOrCreateCustomer(ctx, processedCustomer)
	if err != nil {
		h.logger.Errorw("failed to find or create FlexPrice customer",
			"error", err,
			"stripe_customer_id", customerData.ID,
			"external_id", processedCustomer.ExternalID,
			"event_id", event.ID,
		)
		return webhookDto.NewStripeWebhookErrorResponse("Failed to create customer"), err
	}

	// Create customer integration mapping
	mapping := domainIntegration.CreateStripeCustomerMapping(
		flexpriceCustomerID,
		customerData.ID,
		processedCustomer.ExternalID,
		processedCustomer.EnvironmentID,
	)

	// Add additional metadata
	mapping.Metadata["stripe_created_at"] = strconv.FormatInt(customerData.Created, 10)
	mapping.Metadata["webhook_event_id"] = event.ID
	if customerData.Email != "" {
		mapping.Metadata["stripe_email"] = customerData.Email
	}
	if customerData.Name != "" {
		mapping.Metadata["stripe_name"] = customerData.Name
	}

	// Save the mapping
	if err := h.customerMappingRepo.Create(ctx, mapping); err != nil {
		h.logger.Errorw("failed to create customer integration mapping",
			"error", err,
			"stripe_customer_id", customerData.ID,
			"flexprice_customer_id", flexpriceCustomerID,
			"event_id", event.ID,
		)
		return webhookDto.NewStripeWebhookErrorResponse("Failed to create customer mapping"), err
	}

	h.logger.Infow("successfully processed customer.created webhook",
		"stripe_customer_id", customerData.ID,
		"flexprice_customer_id", flexpriceCustomerID,
		"external_id", processedCustomer.ExternalID,
		"event_id", event.ID,
	)

	return webhookDto.NewStripeWebhookResponse(), nil
}

// handleCustomerUpdated handles customer.updated webhook events (placeholder)
func (h *stripeWebhookHandler) handleCustomerUpdated(ctx context.Context, event *webhookDto.StripeWebhookEvent) (*webhookDto.StripeWebhookResponse, error) {
	h.logger.Debugw("customer.updated webhook received (not implemented)",
		"event_id", event.ID,
	)
	return webhookDto.NewStripeWebhookResponse(), nil
}

// handleCustomerDeleted handles customer.deleted webhook events (placeholder)
func (h *stripeWebhookHandler) handleCustomerDeleted(ctx context.Context, event *webhookDto.StripeWebhookEvent) (*webhookDto.StripeWebhookResponse, error) {
	h.logger.Debugw("customer.deleted webhook received (not implemented)",
		"event_id", event.ID,
	)
	return webhookDto.NewStripeWebhookResponse(), nil
}

// findOrCreateCustomer finds an existing customer by external_id or creates a new one
func (h *stripeWebhookHandler) findOrCreateCustomer(ctx context.Context, processedCustomer *webhookDto.ProcessedStripeCustomer) (string, error) {
	// If external_id is provided, try to find existing customer
	if processedCustomer.ExternalID != "" {
		customer, err := h.customerService.GetCustomerByLookupKey(ctx, processedCustomer.ExternalID)
		if err == nil && customer != nil {
			h.logger.Infow("found existing customer by external_id",
				"external_id", processedCustomer.ExternalID,
				"customer_id", customer.ID,
			)
			return customer.ID, nil
		}

		if !ierr.IsNotFound(err) {
			return "", err
		}
	}

	// Create new customer if not found
	createRequest := dto.CreateCustomerRequest{
		ExternalID: processedCustomer.ExternalID,
		Name:       processedCustomer.Name,
		Email:      processedCustomer.Email,
		Metadata: map[string]string{
			"stripe_customer_id": processedCustomer.StripeCustomerID,
			"created_via":        "stripe_webhook",
		},
	}

	// Add Stripe metadata to customer metadata
	for k, v := range processedCustomer.Metadata {
		if k != "tenant_id" && k != "environment_id" && k != "external_id" {
			createRequest.Metadata["stripe_"+k] = v
		}
	}

	customer, err := h.customerService.CreateCustomer(ctx, createRequest)
	if err != nil {
		return "", err
	}

	h.logger.Infow("created new customer from Stripe webhook",
		"customer_id", customer.ID,
		"external_id", processedCustomer.ExternalID,
		"stripe_customer_id", processedCustomer.StripeCustomerID,
	)

	return customer.ID, nil
}

// ValidateSignature validates the Stripe webhook signature
func (h *stripeWebhookHandler) ValidateSignature(payload []byte, signature string, secret string) error {
	// Parse the signature header
	sigParts := strings.Split(signature, ",")
	var timestamp string
	var v1Signature string

	for _, part := range sigParts {
		if strings.HasPrefix(part, "t=") {
			timestamp = part[2:]
		} else if strings.HasPrefix(part, "v1=") {
			v1Signature = part[3:]
		}
	}

	if timestamp == "" || v1Signature == "" {
		return ierr.NewError("invalid signature format").
			WithHint("Stripe signature header format is invalid").
			Mark(ierr.ErrValidation)
	}

	// Check timestamp (reject if older than 5 minutes)
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return ierr.NewError("invalid timestamp in signature").
			WithHint("Timestamp in signature header must be valid Unix timestamp").
			Mark(ierr.ErrValidation)
	}

	eventTime := time.Unix(ts, 0)
	if time.Since(eventTime) > 5*time.Minute {
		return ierr.NewError("webhook timestamp too old").
			WithHint("Webhook events must be processed within 5 minutes").
			Mark(ierr.ErrValidation)
	}

	// Compute the expected signature
	signedPayload := timestamp + "." + string(payload)
	expectedSignature := h.computeSignature(signedPayload, secret)

	// Compare signatures
	if !hmac.Equal([]byte(v1Signature), []byte(expectedSignature)) {
		return ierr.NewError("signature verification failed").
			WithHint("Webhook signature does not match expected value").
			Mark(ierr.ErrPermissionDenied)
	}

	return nil
}

// computeSignature computes the HMAC signature for webhook validation
func (h *stripeWebhookHandler) computeSignature(payload string, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}
