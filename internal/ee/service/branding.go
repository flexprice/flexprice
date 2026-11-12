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

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/billingentity"
	"github.com/flexprice/flexprice/internal/domain/environment"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/storage"
	"github.com/flexprice/flexprice/internal/types"
)

// BrandingService stores branding images and resolves an issuer's logo and icon.
type BrandingService interface {
	// StoreImage checks and re-encodes an upload, stores it under the tenant in ctx and the given environment, and returns its image key.
	StoreImage(ctx context.Context, environmentID string, asset types.BrandingAsset, data []byte) (string, error)
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
func selectBrandingKeys(entity *billingentity.BillingEntity, env *environment.Environment) (logoKey, iconKey *string) {
	if entity != nil && (entity.LogoKey != nil || entity.IconKey != nil) {
		return entity.LogoKey, entity.IconKey
	}
	if env == nil {
		return nil, nil
	}
	return env.LogoKey, env.IconKey
}
