package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

type SecretHandler struct {
	service service.SecretService
	logger  *logger.Logger
}

func NewSecretHandler(service service.SecretService, logger *logger.Logger) *SecretHandler {
	return &SecretHandler{
		service: service,
		logger:  logger,
	}
}

// ListAPIKeys godoc
// @Summary List API keys
// @Description Get a paginated list of API keys
// @Tags secrets
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param limit query int false "Limit"
// @Param offset query int false "Offset"
// @Param status query string false "Status (published/archived)"
// @Success 200 {object} dto.ListSecretsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /secrets/api/keys [get]
func (h *SecretHandler) ListAPIKeys(c *gin.Context) {
	filter := &types.SecretFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
		Type:        lo.ToPtr(types.SecretTypePrivateKey),
		Provider:    lo.ToPtr(types.SecretProviderFlexPrice),
	}

	if err := c.ShouldBindQuery(filter); err != nil {
		h.logger.Errorw("failed to bind query", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Please check the query parameters").
			Mark(ierr.ErrValidation))
		return
	}

	secrets, err := h.service.ListAPIKeys(c.Request.Context(), filter)
	if err != nil {
		h.logger.Errorw("failed to list secrets", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, secrets)
}

// CreateAPIKey godoc
// @Summary Create a new API key
// @Description Create a new API key. Provide 'service_account_id' in body to create API key for a service account, otherwise creates for authenticated user.
// @Tags secrets
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.CreateAPIKeyRequest true "API key creation request\"
// @Success 201 {object} dto.CreateAPIKeyResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /secrets/api/keys [post]
func (h *SecretHandler) CreateAPIKey(c *gin.Context) {
	var req dto.CreateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Errorw("failed to bind request", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	secret, apiKey, err := h.service.CreateAPIKey(c.Request.Context(), &req)
	if err != nil {
		h.logger.Errorw("failed to create api key", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, dto.CreateAPIKeyResponse{
		Secret: *dto.ToSecretResponse(secret),
		APIKey: apiKey,
	})
}

// DeleteAPIKey godoc
// @Summary Delete an API key
// @Description Delete an API key by ID
// @Tags secrets
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "API key ID"
// @Success 204 "No Content"
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /secrets/api/keys/{id} [delete]
func (h *SecretHandler) DeleteAPIKey(c *gin.Context) {
	id := c.Param("id")
	if err := h.service.Delete(c.Request.Context(), id); err != nil {
		h.logger.Errorw("failed to delete api key", "error", err)
		c.Error(err)
		return
	}

	c.Status(http.StatusNoContent)
}

// CreateIntegration godoc
// @Summary Create or update an integration
// @Description Create or update integration credentials
// @Tags Integrations
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param provider path string true "Integration provider"
// @Param request body dto.CreateIntegrationRequest true "Integration creation request"
// @Success 201 {object} dto.SecretResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /secrets/integrations/create/{provider} [post]
func (h *SecretHandler) CreateIntegration(c *gin.Context) {
	provider := types.SecretProvider(c.Param("provider"))
	var req dto.CreateIntegrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Errorw("failed to bind request", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	req.Provider = provider
	secret, err := h.service.CreateIntegration(c.Request.Context(), &req)
	if err != nil {
		h.logger.Errorw("failed to create integration", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, dto.ToSecretResponse(secret))
}

// GetIntegration godoc
// @Summary Get integration details
// @Description Get details of a specific integration
// @Tags Integrations
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param provider path string true "Integration provider"
// @Success 200 {object} dto.SecretResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /secrets/integrations/by-provider/{provider} [get]
func (h *SecretHandler) GetIntegration(c *gin.Context) {
	provider := types.SecretProvider(c.Param("provider"))
	filter := &types.SecretFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
		Type:        lo.ToPtr(types.SecretTypeIntegration),
		Provider:    lo.ToPtr(provider),
	}

	secrets, err := h.service.ListIntegrations(c.Request.Context(), filter)
	if err != nil {
		h.logger.Errorw("failed to list secrets", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, secrets)
}

// DeleteIntegration godoc
// @Summary Delete an integration
// @Description Delete integration credentials
// @Tags Integrations
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Integration ID"
// @Success 204 "No Content"
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /secrets/integrations/{id} [delete]
func (h *SecretHandler) DeleteIntegration(c *gin.Context) {
	id := c.Param("id")

	if err := h.service.Delete(c.Request.Context(), id); err != nil {
		h.logger.Errorw("failed to delete integration", "error", err)
		c.Error(err)
		return
	}

	c.Status(http.StatusNoContent)
}

// ListLinkedIntegrations godoc
// @Summary List linked integrations
// @Description Get a list of unique providers which have a valid linked integration secret
// @Tags Integrations
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} dto.LinkedIntegrationsResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /secrets/integrations/linked [get]
func (h *SecretHandler) ListLinkedIntegrations(c *gin.Context) {
	providers, err := h.service.ListLinkedIntegrations(c.Request.Context())
	if err != nil {
		h.logger.Errorw("failed to list linked integrations", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, dto.LinkedIntegrationsResponse{
		Providers: providers,
	})
}
