package types

// BrandingAsset names one of the two branding images an environment or billing entity can carry.
type BrandingAsset string

const (
	BrandingAssetLogo BrandingAsset = "logo"
	BrandingAssetIcon BrandingAsset = "icon"
)

func (a BrandingAsset) Validate() error {
	return validateEnum("asset", a, []BrandingAsset{
		BrandingAssetLogo,
		BrandingAssetIcon,
	})
}
