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
// @ID listApiKeys
// @Description Use when listing API keys (e.g. admin view or rotating keys). Returns a paginated list.
// @Tags Secrets
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param limit query int false "Limit"
// @Param offset query int false "Offset"
// @Param status query string false "Status (published/archived)"
// @Success 200 {object} dto.ListSecretsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
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
// @ID createApiKey
// @Description Use when issuing a new API key (e.g. for a service account or for the current user). Provide service_account_id to create for a service account.
// @Tags Secrets
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.CreateAPIKeyRequest true "API key creation request"
// @Success 201 {object} dto.CreateAPIKeyResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
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
// @ID deleteApiKey
// @Description Use when revoking an API key (e.g. rotation or compromise). Permanently invalidates the key.
// @Tags Secrets
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "API key ID"
// @Success 204 "No Content"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
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

