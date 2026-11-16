package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"path"
	"regexp"
	"strings"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/billingentity"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/settings"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/storage"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
)

// BrandingService stores branding images and resolves an issuer's logo and icon.
type BrandingService interface {
	// StoreImage checks and re-encodes an upload, stores it under the tenant in ctx and the given environment, and returns its image key.
	StoreImage(ctx context.Context, environmentID string, asset types.BrandingAsset, data []byte) (string, error)
	// GetImage returns a stored image for the public endpoint; a malformed path is not found without a storage call.
	GetImage(ctx context.Context, req dto.BrandingImageRequest) (*dto.BrandingImage, error)
	// ReadImage downloads a stored image by its image key.
	ReadImage(ctx context.Context, key string) ([]byte, error)
	// ForInvoice returns the invoice issuer's name and image keys, read live.
	ForInvoice(ctx context.Context, inv *invoice.Invoice) (*IssuerBranding, error)
	// ForCustomer returns the name and image keys of the issuer that bills the customer.
	ForCustomer(ctx context.Context, cust *customer.Customer) (*IssuerBranding, error)
}

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

type brandingService struct {
	ServiceParams
}

func NewBrandingService(params ServiceParams) BrandingService {
	return &brandingService{ServiceParams: params}
}

func (s *brandingService) StoreImage(ctx context.Context, environmentID string, asset types.BrandingAsset, data []byte) (string, error) {
	img, err := processBrandingImage(data, asset)
	if err != nil {
		return "", err
	}

	store, err := s.StorageResolver.ForPlatform(ctx, storage.PurposeBranding)
	if err != nil {
		return "", err
	}
	bc, err := s.StorageResolver.BucketConfigFor(storage.PurposeBranding)
	if err != nil {
		return "", err
	}

	key := brandingImageKey(types.GetTenantID(ctx), environmentID, img)
	objectKey := brandingObjectKey(bc.KeyPrefix, key)
	exists, err := store.Exists(ctx, objectKey)
	if err != nil {
		s.Logger.Error(ctx, "branding image existence check failed", "error", err, "environment_id", environmentID, "key", key)
		return "", err
	}
	if exists {
		return key, nil
	}

	format := storage.UploadFormatPNG
	if img.Ext() == "jpg" {
		format = storage.UploadFormatJPEG
	}
	if _, err := store.Upload(ctx, &storage.UploadRequest{Key: objectKey, Data: img.Data(), Format: format}); err != nil {
		s.Logger.Error(ctx, "branding image upload failed", "error", err, "environment_id", environmentID, "key", key)
		return "", err
	}
	return key, nil
}

func (s *brandingService) GetImage(ctx context.Context, req dto.BrandingImageRequest) (*dto.BrandingImage, error) {
	key, ext, hash, ok := parseBrandingImageKey(req.TenantID, req.EnvironmentID, req.File)
	if !ok {
		return nil, ierr.NewError("image not found").WithHint("Image not found").Mark(ierr.ErrNotFound)
	}

	img := &dto.BrandingImage{ContentType: "image/png", ETag: `"` + hash + `"`}
	if ext == "jpg" {
		img.ContentType = "image/jpeg"
	}
	if req.IfNoneMatch == img.ETag {
		img.NotModified = true
		return img, nil
	}

	var err error
	img.Data, err = s.ReadImage(ctx, key)
	if err != nil {
		if !ierr.IsNotFound(err) {
			s.Logger.Error(ctx, "branding image read failed", "error", err, "tenant_id", req.TenantID, "environment_id", req.EnvironmentID, "key", key)
		}
		return nil, err
	}
	return img, nil
}

func (s *brandingService) ReadImage(ctx context.Context, key string) ([]byte, error) {
	store, err := s.StorageResolver.ForPlatform(ctx, storage.PurposeBranding)
	if err != nil {
		return nil, err
	}
	bc, err := s.StorageResolver.BucketConfigFor(storage.PurposeBranding)
	if err != nil {
		return nil, err
	}
	return store.Download(ctx, brandingObjectKey(bc.KeyPrefix, key))
}

func (s *brandingService) ForInvoice(ctx context.Context, inv *invoice.Invoice) (*IssuerBranding, error) {
	ctx = types.SetEnvironmentID(ctx, inv.EnvironmentID)

	issuer, err := NewInvoiceService(s.ServiceParams).GetInvoiceBillingEntity(ctx, inv)
	if err != nil {
		return nil, err
	}
	if issuer == nil {
		return s.tenantBranding(ctx, inv.EnvironmentID)
	}

	entity, err := s.publishedEntity(ctx, issuer.BillingEntityID)
	if err != nil {
		return nil, err
	}
	return s.issuerBranding(ctx, issuer.Name, entity, inv.EnvironmentID)
}

func (s *brandingService) ForCustomer(ctx context.Context, cust *customer.Customer) (*IssuerBranding, error) {
	ctx = types.SetEnvironmentID(ctx, cust.EnvironmentID)

	entity, err := NewBillingEntityService(s.ServiceParams).EffectiveEntity(ctx, cust)
	if err != nil && !ierr.IsNotFound(err) {
		return nil, err
	}
	if entity == nil {
		return s.tenantBranding(ctx, cust.EnvironmentID)
	}
	return s.issuerBranding(ctx, entity.Name, entity, cust.EnvironmentID)
}

func (s *brandingService) tenantBranding(ctx context.Context, environmentID string) (*IssuerBranding, error) {
	t, err := s.TenantRepo.GetByID(ctx, types.GetTenantID(ctx))
	if err != nil {
		return nil, err
	}
	return s.issuerBranding(ctx, t.Name, nil, environmentID)
}

func (s *brandingService) issuerBranding(ctx context.Context, name string, entity *billingentity.BillingEntity, environmentID string) (*IssuerBranding, error) {
	cfg, err := environmentBranding(ctx, s.SettingsRepo, environmentID)
	if err != nil {
		return nil, err
	}
	logoKey, iconKey := selectBrandingKeys(entity, cfg)
	return &IssuerBranding{name: name, logoKey: logoKey, iconKey: iconKey}, nil
}

// publishedEntity loads a billing entity; one no longer published reads as none.
func (s *brandingService) publishedEntity(ctx context.Context, id string) (*billingentity.BillingEntity, error) {
	entity, err := s.BillingEntityRepo.Get(ctx, id)
	if ierr.IsNotFound(err) {
		return nil, nil
	}
	return entity, err
}

// brandingObjectKey is the storage object for an image key: <prefix>/branding/<key>.
func brandingObjectKey(prefix, key string) string {
	ext := path.Ext(key)
	return storage.ObjectKey(prefix, "branding", strings.TrimSuffix(key, ext), strings.TrimPrefix(ext, "."), false)
}

// brandingImageURL is the public URL of an image key, or "" when unset or no public origin is configured.
func brandingImageURL(cfg *config.Configuration, key *string) string {
	if key == nil || cfg == nil || cfg.Branding.BaseURL == "" {
		return ""
	}
	return strings.TrimRight(cfg.Branding.BaseURL, "/") + "/v1/branding/images/" + *key
}

const (
	brandingMaxUploadBytes = 512 * 1024
	brandingMaxPixels      = 4_000_000
	brandingMinSide        = 128
	brandingJPEGQuality    = 90
)

var brandingFilePattern = regexp.MustCompile(`^([0-9a-f]{64})\.(png|jpg)$`)
var brandingIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,50}$`)

// brandingImage is an uploaded image after checks and re-encoding.
type brandingImage struct {
	data []byte
	ext  string
	hash string
}

func (i *brandingImage) Data() []byte {
	if i == nil {
		return nil
	}
	return i.data
}

func (i *brandingImage) Ext() string {
	if i == nil {
		return ""
	}
	return i.ext
}

func (i *brandingImage) Hash() string {
	if i == nil {
		return ""
	}
	return i.hash
}

func brandingRejection(msg string) error {
	return ierr.NewError(msg).WithHint(msg).Mark(ierr.ErrValidation)
}

// processBrandingImage checks an upload and re-encodes it in the same format, dropping metadata.
func processBrandingImage(data []byte, asset types.BrandingAsset) (*brandingImage, error) {
	if len(data) > brandingMaxUploadBytes {
		return nil, brandingRejection("Image must be 512 KB or smaller")
	}

	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg") {
		return nil, brandingRejection("Only PNG and JPEG images are allowed")
	}
	if cfg.Width*cfg.Height > brandingMaxPixels {
		return nil, brandingRejection("Image must be 4 megapixels or smaller")
	}
	if err := checkBrandingDimensions(asset, cfg.Width, cfg.Height); err != nil {
		return nil, err
	}

	var out bytes.Buffer
	ext := "png"
	if format == "png" {
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, brandingRejection("Only PNG and JPEG images are allowed")
		}
		err = png.Encode(&out, img)
		if err != nil {
			return nil, ierr.WithError(err).WithHint("Could not process the image").Mark(ierr.ErrSystem)
		}
	} else {
		ext = "jpg"
		img, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, brandingRejection("Only PNG and JPEG images are allowed")
		}
		err = jpeg.Encode(&out, img, &jpeg.Options{Quality: brandingJPEGQuality})
		if err != nil {
			return nil, ierr.WithError(err).WithHint("Could not process the image").Mark(ierr.ErrSystem)
		}
	}

	sum := sha256.Sum256(out.Bytes())
	return &brandingImage{data: out.Bytes(), ext: ext, hash: hex.EncodeToString(sum[:])}, nil
}

func checkBrandingDimensions(asset types.BrandingAsset, width, height int) error {
	if asset == types.BrandingAssetIcon {
		if width != height {
			return brandingRejection("Icon must be square")
		}
		if width < brandingMinSide {
			return brandingRejection("Icon must be at least 128 x 128 px")
		}
		return nil
	}
	if width < brandingMinSide || height < brandingMinSide {
		return brandingRejection("Logo must be at least 128 px on each side")
	}
	return nil
}

// brandingImageKey is the image key: <tenant>/<environment>/<sha256>.<ext>.
func brandingImageKey(tenantID, environmentID string, img *brandingImage) string {
	return fmt.Sprintf("%s/%s/%s.%s", tenantID, environmentID, img.Hash(), img.Ext())
}

// parseBrandingImageKey validates public image path segments and returns the image key.
func parseBrandingImageKey(tenantID, environmentID, file string) (key, ext, hash string, ok bool) {
	if !brandingIDPattern.MatchString(tenantID) || !brandingIDPattern.MatchString(environmentID) {
		return "", "", "", false
	}
	m := brandingFilePattern.FindStringSubmatch(file)
	if m == nil {
		return "", "", "", false
	}
	return fmt.Sprintf("%s/%s/%s", tenantID, environmentID, file), m[2], m[1], true
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
