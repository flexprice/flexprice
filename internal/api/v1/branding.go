package v1

import (
	"io"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

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
