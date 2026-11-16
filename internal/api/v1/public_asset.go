package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/gin-gonic/gin"
)

type PublicAssetHandler struct {
	service service.PublicAssetService
	log     *logger.Logger
}

func NewPublicAssetHandler(service service.PublicAssetService, log *logger.Logger) *PublicAssetHandler {
	return &PublicAssetHandler{service: service, log: log}
}

// Get serves a public asset with immutable caching.
func (h *PublicAssetHandler) Get(c *gin.Context) {
	asset, err := h.service.Get(c.Request.Context(), dto.PublicAssetRequest{
		TenantID:      c.Param("tenant_id"),
		EnvironmentID: c.Param("environment_id"),
		File:          c.Param("file"),
		IfNoneMatch:   c.GetHeader("If-None-Match"),
	})
	if err != nil {
		c.Error(err)
		return
	}

	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Header("ETag", asset.ETag)
	if asset.NotModified {
		c.Status(http.StatusNotModified)
		return
	}
	c.Data(http.StatusOK, asset.ContentType, asset.Data)
}
