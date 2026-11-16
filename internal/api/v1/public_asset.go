package v1

import (
	"fmt"
	"io"
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
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
		TenantID:    c.Param("tenant_id"),
		File:        c.Param("file"),
		IfNoneMatch: c.GetHeader("If-None-Match"),
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

// readUploadedFile reads a multipart file field from a request whose body is capped at maxBodyBytes.
func readUploadedFile(c *gin.Context, field string, maxBodyBytes int64) ([]byte, error) {
	header, err := c.FormFile(field)
	if err != nil {
		if isRequestBodyTooLarge(err) {
			msg := fmt.Sprintf("Request body must be %d MiB or smaller", maxBodyBytes>>20)
			return nil, ierr.NewError(msg).WithHint(msg).Mark(ierr.ErrValidation)
		}
		return nil, ierr.WithError(err).
			WithHintf("Upload the image in a form field named %s", field).
			Mark(ierr.ErrValidation)
	}

	file, err := header.Open()
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Could not read the uploaded file").Mark(ierr.ErrValidation)
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Could not read the uploaded file").Mark(ierr.ErrValidation)
	}
	return data, nil
}
