package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"regexp"

	"github.com/flexprice/flexprice/internal/domain/billingentity"
	"github.com/flexprice/flexprice/internal/domain/environment"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

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
