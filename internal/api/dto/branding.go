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

// BrandingImageRequest addresses a public branding image by its path segments.
type BrandingImageRequest struct {
	TenantID      string
	EnvironmentID string
	File          string
	IfNoneMatch   string
}

// BrandingImage is a stored branding image; NotModified means the caller's cached copy is current.
type BrandingImage struct {
	Data        []byte
	ContentType string
	ETag        string
	NotModified bool
}
