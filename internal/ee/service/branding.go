package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/billingentity"
	"github.com/flexprice/flexprice/internal/domain/settings"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
)

// IssuerBranding is an issuer's name and branding image keys.
type IssuerBranding struct {
	name    string
	logoKey *string
	iconKey *string
}

func (b *IssuerBranding) Name() string {
	if b == nil {
		return ""
	}
	return b.name
}

func (b *IssuerBranding) LogoKey() *string {
	if b == nil {
		return nil
	}
	return b.logoKey
}

func (b *IssuerBranding) IconKey() *string {
	if b == nil {
		return nil
	}
	return b.iconKey
}

// tenantIssuerBranding is the tenant's name with its environment's pair.
func tenantIssuerBranding(ctx context.Context, params ServiceParams, environmentID string) (*IssuerBranding, error) {
	t, err := params.TenantRepo.GetByID(ctx, types.GetTenantID(ctx))
	if err != nil {
		return nil, err
	}
	return issuerBranding(ctx, params, t.Name, nil, environmentID)
}

// issuerBranding is the issuer's name with the entity's own pair when it has any asset, else its environment's.
func issuerBranding(ctx context.Context, params ServiceParams, name string, entity *billingentity.BillingEntity, environmentID string) (*IssuerBranding, error) {
	cfg, err := environmentBranding(ctx, params.SettingsRepo, environmentID)
	if err != nil {
		return nil, err
	}
	logoKey, iconKey := selectBrandingKeys(entity, cfg)
	return &IssuerBranding{name: name, logoKey: logoKey, iconKey: iconKey}, nil
}

const (
	brandingMaxUploadBytes = 512 * 1024
	brandingMinSide        = 128
)

// processBrandingImage checks a logo or icon upload and re-encodes it.
func processBrandingImage(data []byte, asset types.BrandingAsset) (*processedImage, error) {
	return processImage(data, brandingMaxUploadBytes, func(width, height int) error {
		return checkBrandingDimensions(asset, width, height)
	})
}

func checkBrandingDimensions(asset types.BrandingAsset, width, height int) error {
	if asset == types.BrandingAssetIcon {
		if width != height {
			return imageRejection("Icon must be square")
		}
		if width < brandingMinSide {
			return imageRejection("Icon must be at least 128 x 128 px")
		}
		return nil
	}
	if width < brandingMinSide || height < brandingMinSide {
		return imageRejection("Logo must be at least 128 px on each side")
	}
	return nil
}

// selectBrandingKeys uses the entity's own pair when it has any asset, else the environment's.
func selectBrandingKeys(entity *billingentity.BillingEntity, env *types.BrandingConfig) (logoKey, iconKey *string) {
	if entity != nil && (entity.LogoKey != nil || entity.IconKey != nil) {
		return entity.LogoKey, entity.IconKey
	}
	if env == nil {
		return nil, nil
	}
	return env.LogoKey, env.IconKey
}

// environmentBranding reads the environment's branding_config from the database; none set means no assets.
func environmentBranding(ctx context.Context, repo settings.Repository, environmentID string) (*types.BrandingConfig, error) {
	stored, err := repo.GetByKeyUncached(ctx, environmentID, types.SettingKeyBrandingConfig)
	if ierr.IsNotFound(err) {
		return &types.BrandingConfig{}, nil
	}
	if err != nil {
		return nil, err
	}
	cfg, err := utils.ToStruct[types.BrandingConfig](stored.Value)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}
