package v1

import (
	"io"
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

type BrandingHandler struct {
	service service.BrandingService
	log     *logger.Logger
}

func NewBrandingHandler(service service.BrandingService, log *logger.Logger) *BrandingHandler {
	return &BrandingHandler{service: service, log: log}
}

// GetImage serves a branding image publicly with immutable caching.
func (h *BrandingHandler) GetImage(c *gin.Context) {
	image, err := h.service.GetImage(c.Request.Context(), dto.BrandingImageRequest{
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
	c.Header("ETag", image.ETag)
	if image.NotModified {
		c.Status(http.StatusNotModified)
		return
	}
	c.Data(http.StatusOK, image.ContentType, image.Data)
}

// readBrandingUpload reads the asset path param and the multipart "file" part.
func readBrandingUpload(c *gin.Context) (dto.UploadBrandingAssetRequest, error) {
	req := dto.UploadBrandingAssetRequest{Asset: types.BrandingAsset(c.Param("asset"))}

	header, err := c.FormFile("file")
	if err != nil {
		if isRequestBodyTooLarge(err) {
			return req, ierr.NewError("Request body must be 1 MiB or smaller").
				WithHint("Request body must be 1 MiB or smaller").
				Mark(ierr.ErrValidation)
		}
		return req, ierr.WithError(err).
			WithHint("Upload the image in a form field named file").
			Mark(ierr.ErrValidation)
	}

	file, err := header.Open()
	if err != nil {
		return req, ierr.WithError(err).WithHint("Could not read the uploaded file").Mark(ierr.ErrValidation)
	}
	defer file.Close()

	req.Data, err = io.ReadAll(file)
	if err != nil {
		return req, ierr.WithError(err).WithHint("Could not read the uploaded file").Mark(ierr.ErrValidation)
	}
	return req, nil
}
