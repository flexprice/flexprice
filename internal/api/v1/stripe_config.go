package v1

import (
	"net/http"
	"strconv"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

// StripeConfigHandler handles HTTP requests for Stripe configuration management
type StripeConfigHandler struct {
	stripeIntegrationService service.StripeIntegrationService
	logger                   *logger.Logger
}

// NewStripeConfigHandler creates a new Stripe configuration HTTP handler
func NewStripeConfigHandler(
	stripeIntegrationService service.StripeIntegrationService,
	logger *logger.Logger,
) *StripeConfigHandler {
	return &StripeConfigHandler{
		stripeIntegrationService: stripeIntegrationService,
		logger:                   logger,
	}
}

// GetStripeConfig retrieves the Stripe configuration for the current tenant and environment
// @Summary Get Stripe configuration
// @Description Get the Stripe integration configuration for the current tenant and environment
// @Tags stripe-config
// @Accept application/json
// @Produce application/json
// @Security ApiKeyAuth
// @Success 200 {object} dto.StripeConfigResponse "Stripe configuration retrieved successfully"
// @Failure 400 {object} ierr.ErrorResponse "Bad request"
// @Failure 401 {object} ierr.ErrorResponse "Unauthorized"
// @Failure 404 {object} ierr.ErrorResponse "Configuration not found"
// @Failure 500 {object} ierr.ErrorResponse "Internal server error"
// @Router /stripe/config [get]
func (h *StripeConfigHandler) GetStripeConfig(c *gin.Context) {
	ctx := c.Request.Context()

	// Extract tenant and environment from context
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	h.logger.Infow("retrieving Stripe configuration",
		"tenant_id", tenantID,
		"environment_id", environmentID,
	)

	// Get configuration from service
	config, err := h.stripeIntegrationService.GetTenantConfig(ctx, tenantID, environmentID)
	if err != nil {
		h.logger.Errorw("failed to retrieve Stripe configuration",
			"error", err,
			"tenant_id", tenantID,
			"environment_id", environmentID,
		)
		c.Error(err)
		return
	}

	// Convert to response DTO
	response := h.newStripeConfigResponse(config)

	h.logger.Infow("Stripe configuration retrieved successfully",
		"tenant_id", tenantID,
		"environment_id", environmentID,
	)

	c.JSON(http.StatusOK, response)
}

// CreateOrUpdateStripeConfig creates or updates the Stripe configuration for the current tenant and environment
// @Summary Create or update Stripe configuration
// @Description Create or update the Stripe integration configuration for the current tenant and environment
// @Tags stripe-config
// @Accept application/json
// @Produce application/json
// @Security ApiKeyAuth
// @Param request body dto.CreateOrUpdateStripeConfigRequest true "Stripe configuration request"
// @Success 200 {object} dto.StripeConfigResponse "Configuration updated successfully"
// @Success 201 {object} dto.StripeConfigResponse "Configuration created successfully"
// @Failure 400 {object} ierr.ErrorResponse "Bad request - invalid payload"
// @Failure 401 {object} ierr.ErrorResponse "Unauthorized"
// @Failure 500 {object} ierr.ErrorResponse "Internal server error"
// @Router /stripe/config [put]
func (h *StripeConfigHandler) CreateOrUpdateStripeConfig(c *gin.Context) {
	ctx := c.Request.Context()

	// Extract tenant and environment from context
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	// Parse request body
	var req dto.CreateOrUpdateStripeConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Errorw("invalid request payload",
			"error", err,
			"tenant_id", tenantID,
			"environment_id", environmentID,
		)
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	// Validate request
	if err := req.Validate(); err != nil {
		h.logger.Errorw("request validation failed",
			"error", err,
			"tenant_id", tenantID,
			"environment_id", environmentID,
		)
		c.Error(ierr.WithError(err).
			WithHint("Please check the configuration values").
			Mark(ierr.ErrValidation))
		return
	}

	h.logger.Infow("creating or updating Stripe configuration",
		"tenant_id", tenantID,
		"environment_id", environmentID,
		"sync_enabled", req.SyncEnabled,
	)

	// Check if configuration exists
	existingConfig, err := h.stripeIntegrationService.GetTenantConfig(ctx, tenantID, environmentID)
	isUpdate := err == nil && existingConfig != nil

	var response *service.StripeTenantConfigResponse

	if isUpdate {
		// Update existing configuration
		updateReq := h.toUpdateRequest(req)
		response, err = h.stripeIntegrationService.UpdateTenantConfig(ctx, tenantID, environmentID, updateReq)
		if err != nil {
			h.logger.Errorw("failed to update Stripe configuration",
				"error", err,
				"tenant_id", tenantID,
				"environment_id", environmentID,
			)
			c.Error(err)
			return
		}

		h.logger.Infow("Stripe configuration updated successfully",
			"tenant_id", tenantID,
			"environment_id", environmentID,
		)

		c.JSON(http.StatusOK, h.newStripeConfigResponse(response))
	} else {
		// Create new configuration
		createReq := h.toCreateRequest(req)
		response, err = h.stripeIntegrationService.CreateTenantConfig(ctx, createReq)
		if err != nil {
			h.logger.Errorw("failed to create Stripe configuration",
				"error", err,
				"tenant_id", tenantID,
				"environment_id", environmentID,
			)
			c.Error(err)
			return
		}

		h.logger.Infow("Stripe configuration created successfully",
			"tenant_id", tenantID,
			"environment_id", environmentID,
		)

		c.JSON(http.StatusCreated, h.newStripeConfigResponse(response))
	}
}

// DeleteStripeConfig deletes the Stripe configuration for the current tenant and environment
// @Summary Delete Stripe configuration
// @Description Delete the Stripe integration configuration for the current tenant and environment
// @Tags stripe-config
// @Accept application/json
// @Produce application/json
// @Security ApiKeyAuth
// @Success 204 "Configuration deleted successfully"
// @Failure 400 {object} ierr.ErrorResponse "Bad request"
// @Failure 401 {object} ierr.ErrorResponse "Unauthorized"
// @Failure 404 {object} ierr.ErrorResponse "Configuration not found"
// @Failure 500 {object} ierr.ErrorResponse "Internal server error"
// @Router /stripe/config [delete]
func (h *StripeConfigHandler) DeleteStripeConfig(c *gin.Context) {
	ctx := c.Request.Context()

	// Extract tenant and environment from context
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	h.logger.Infow("deleting Stripe configuration",
		"tenant_id", tenantID,
		"environment_id", environmentID,
	)

	// Delete configuration
	err := h.stripeIntegrationService.DeleteTenantConfig(ctx, tenantID, environmentID)
	if err != nil {
		h.logger.Errorw("failed to delete Stripe configuration",
			"error", err,
			"tenant_id", tenantID,
			"environment_id", environmentID,
		)
		c.Error(err)
		return
	}

	h.logger.Infow("Stripe configuration deleted successfully",
		"tenant_id", tenantID,
		"environment_id", environmentID,
	)

	c.Status(http.StatusNoContent)
}

// TestStripeConnection tests the Stripe API connection using the current configuration
// @Summary Test Stripe connection
// @Description Test the Stripe API connection for the current tenant and environment configuration
// @Tags stripe-config
// @Accept application/json
// @Produce application/json
// @Security ApiKeyAuth
// @Success 200 {object} dto.StripeConnectionTestResponse "Connection test completed"
// @Failure 400 {object} ierr.ErrorResponse "Bad request"
// @Failure 401 {object} ierr.ErrorResponse "Unauthorized"
// @Failure 404 {object} ierr.ErrorResponse "Configuration not found"
// @Failure 500 {object} ierr.ErrorResponse "Internal server error"
// @Router /stripe/config/test [post]
func (h *StripeConfigHandler) TestStripeConnection(c *gin.Context) {
	ctx := c.Request.Context()

	// Extract tenant and environment from context
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	h.logger.Infow("testing Stripe connection",
		"tenant_id", tenantID,
		"environment_id", environmentID,
	)

	// Test connection
	result, err := h.stripeIntegrationService.TestStripeConnection(ctx, tenantID, environmentID)
	if err != nil {
		h.logger.Errorw("failed to test Stripe connection",
			"error", err,
			"tenant_id", tenantID,
			"environment_id", environmentID,
		)
		c.Error(err)
		return
	}

	// Convert to response DTO
	response := h.newStripeConnectionTestResponse(result)

	h.logger.Infow("Stripe connection test completed",
		"tenant_id", tenantID,
		"environment_id", environmentID,
		"success", result.Success,
		"latency_ms", result.LatencyMs,
	)

	c.JSON(http.StatusOK, response)
}

// GetStripeConfigStatus returns a summary of the Stripe configuration status
// @Summary Get Stripe configuration status
// @Description Get a summary of the Stripe integration configuration status including validation and health
// @Tags stripe-config
// @Accept application/json
// @Produce application/json
// @Security ApiKeyAuth
// @Success 200 {object} dto.StripeConfigStatusResponse "Configuration status retrieved"
// @Failure 400 {object} ierr.ErrorResponse "Bad request"
// @Failure 401 {object} ierr.ErrorResponse "Unauthorized"
// @Failure 500 {object} ierr.ErrorResponse "Internal server error"
// @Router /stripe/config/status [get]
func (h *StripeConfigHandler) GetStripeConfigStatus(c *gin.Context) {
	ctx := c.Request.Context()

	// Extract tenant and environment from context
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	h.logger.Infow("retrieving Stripe configuration status",
		"tenant_id", tenantID,
		"environment_id", environmentID,
	)

	// Get configuration
	config, err := h.stripeIntegrationService.GetTenantConfig(ctx, tenantID, environmentID)
	configExists := err == nil && config != nil

	// Test connection if configuration exists
	var connectionTest *service.StripeConnectionTestResponse
	if configExists {
		connectionTest, _ = h.stripeIntegrationService.TestStripeConnection(ctx, tenantID, environmentID)
	}

	// Build status response
	response := h.newStripeConfigStatusResponse(config, connectionTest, configExists)

	h.logger.Infow("Stripe configuration status retrieved",
		"tenant_id", tenantID,
		"environment_id", environmentID,
		"configured", configExists,
		"sync_enabled", configExists && config.SyncEnabled,
	)

	c.JSON(http.StatusOK, response)
}

// ListStripeConfigHistory returns the configuration change history
// @Summary List Stripe configuration history
// @Description Get the change history for Stripe configuration (for audit purposes)
// @Tags stripe-config
// @Accept application/json
// @Produce application/json
// @Security ApiKeyAuth
// @Param limit query int false "Number of records to return" default(50)
// @Param offset query int false "Number of records to skip" default(0)
// @Success 200 {object} dto.StripeConfigHistoryResponse "Configuration history retrieved"
// @Failure 400 {object} ierr.ErrorResponse "Bad request"
// @Failure 401 {object} ierr.ErrorResponse "Unauthorized"
// @Failure 500 {object} ierr.ErrorResponse "Internal server error"
// @Router /stripe/config/history [get]
func (h *StripeConfigHandler) ListStripeConfigHistory(c *gin.Context) {
	// Extract tenant and environment from context
	tenantID := types.GetTenantID(c.Request.Context())
	environmentID := types.GetEnvironmentID(c.Request.Context())

	// Parse query parameters
	limitStr := c.DefaultQuery("limit", "50")
	offsetStr := c.DefaultQuery("offset", "0")

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 || limit > 1000 {
		limit = 50
	}

	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		offset = 0
	}

	h.logger.Infow("retrieving Stripe configuration history",
		"tenant_id", tenantID,
		"environment_id", environmentID,
		"limit", limit,
		"offset", offset,
	)

	// For now, return empty history since audit logging isn't implemented yet
	// This endpoint is prepared for future audit functionality
	response := dto.NewStripeConfigHistoryResponse([]dto.StripeConfigHistoryEntry{}, 0, limit, offset)

	h.logger.Infow("Stripe configuration history retrieved",
		"tenant_id", tenantID,
		"environment_id", environmentID,
		"total_records", 0,
	)

	c.JSON(http.StatusOK, response)
}

// Helper conversion methods

// toCreateRequest converts DTO to service create request
func (h *StripeConfigHandler) toCreateRequest(req dto.CreateOrUpdateStripeConfigRequest) service.CreateStripeTenantConfigRequest {
	serviceReq := service.CreateStripeTenantConfigRequest{
		APIKey:                   req.APIKey,
		SyncEnabled:              req.SyncEnabled,
		AggregationWindowMinutes: req.AggregationWindowMinutes,
		Metadata:                 req.Metadata,
	}

	if req.WebhookConfig != nil {
		serviceReq.WebhookConfig = &service.StripeWebhookConfig{
			EndpointURL: req.WebhookConfig.EndpointURL,
			Secret:      req.WebhookConfig.Secret,
			Enabled:     req.WebhookConfig.Enabled,
		}
	}

	return serviceReq
}

// toUpdateRequest converts DTO to service update request
func (h *StripeConfigHandler) toUpdateRequest(req dto.CreateOrUpdateStripeConfigRequest) service.UpdateStripeTenantConfigRequest {
	serviceReq := service.UpdateStripeTenantConfigRequest{
		APIKey:                   &req.APIKey,
		SyncEnabled:              &req.SyncEnabled,
		AggregationWindowMinutes: &req.AggregationWindowMinutes,
		Metadata:                 req.Metadata,
	}

	if req.WebhookConfig != nil {
		serviceReq.WebhookConfig = &service.StripeWebhookConfig{
			EndpointURL: req.WebhookConfig.EndpointURL,
			Secret:      req.WebhookConfig.Secret,
			Enabled:     req.WebhookConfig.Enabled,
		}
	}

	return serviceReq
}

// newStripeConfigResponse creates DTO response from service response
func (h *StripeConfigHandler) newStripeConfigResponse(config *service.StripeTenantConfigResponse) *dto.StripeConfigResponse {
	if config == nil {
		return nil
	}

	response := &dto.StripeConfigResponse{
		TenantID:                 config.TenantID,
		EnvironmentID:            config.EnvironmentID,
		SyncEnabled:              config.SyncEnabled,
		AggregationWindowMinutes: config.AggregationWindowMinutes,
		Metadata:                 config.Metadata,
		CreatedAt:                config.CreatedAt.Format(time.RFC3339),
		UpdatedAt:                config.UpdatedAt.Format(time.RFC3339),
	}

	if config.WebhookConfig != nil {
		response.WebhookConfig = &dto.StripeWebhookConfigDTO{
			EndpointURL: config.WebhookConfig.EndpointURL,
			Secret:      config.WebhookConfig.Secret,
			Enabled:     config.WebhookConfig.Enabled,
		}
	}

	return response
}

// newStripeConnectionTestResponse creates DTO response from service response
func (h *StripeConfigHandler) newStripeConnectionTestResponse(result *service.StripeConnectionTestResponse) *dto.StripeConnectionTestResponse {
	if result == nil {
		return nil
	}

	return &dto.StripeConnectionTestResponse{
		Success:   result.Success,
		Message:   result.Message,
		LatencyMs: result.LatencyMs,
		TestedAt:  result.TestedAt.Format(time.RFC3339),
	}
}

// newStripeConfigStatusResponse creates DTO status response
func (h *StripeConfigHandler) newStripeConfigStatusResponse(
	config *service.StripeTenantConfigResponse,
	connectionTest *service.StripeConnectionTestResponse,
	configured bool,
) *dto.StripeConfigStatusResponse {
	response := &dto.StripeConfigStatusResponse{
		Configured:      configured,
		SyncEnabled:     configured && config != nil && config.SyncEnabled,
		ConfigurationOK: configured && connectionTest != nil && connectionTest.Success,
		Issues:          []string{},
	}

	if connectionTest != nil {
		response.ConnectionStatus = h.newStripeConnectionTestResponse(connectionTest)
		testedAt := connectionTest.TestedAt.Format(time.RFC3339)
		response.LastTestedAt = &testedAt
	}

	// Add configuration issues
	if !configured {
		response.Issues = append(response.Issues, "Stripe integration not configured")
	} else if connectionTest != nil && !connectionTest.Success {
		response.Issues = append(response.Issues, "Stripe API connection failed: "+connectionTest.Message)
	}

	return response
}
