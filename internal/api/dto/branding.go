package dto

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// UploadBrandingAssetRequest is one logo or icon upload, read from the multipart field "file".
type UploadBrandingAssetRequest struct {
	Asset types.BrandingAsset
	Data  []byte
}

func (r *UploadBrandingAssetRequest) Validate() error {
	if err := r.Asset.Validate(); err != nil {
		return err
	}
	if len(r.Data) == 0 {
		return ierr.NewError("Upload the image in a form field named file").
			WithHint("Upload the image in a form field named file").
			Mark(ierr.ErrValidation)
	}
	return nil
}
