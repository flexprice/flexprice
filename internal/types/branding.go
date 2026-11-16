package types

import ierr "github.com/flexprice/flexprice/internal/errors"

// BrandingAsset names one of the two branding images an environment or billing entity can carry.
type BrandingAsset string

const (
	BrandingAssetLogo BrandingAsset = "logo"
	BrandingAssetIcon BrandingAsset = "icon"
)

// Validate requires the asset to be logo or icon.
func (a BrandingAsset) Validate() error {
	if a == "" {
		return ierr.NewError("asset is required").
			WithHint("Asset must be logo or icon").
			Mark(ierr.ErrValidation)
	}
	return validateEnum("asset", a, []BrandingAsset{
		BrandingAssetLogo,
		BrandingAssetIcon,
	})
}
