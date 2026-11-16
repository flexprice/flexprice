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
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/storage"
	"github.com/flexprice/flexprice/internal/types"
)

// PublicAssetService stores images under a tenant by content hash and serves them publicly.
type PublicAssetService interface {
	// StoreImage stores a processed image under the tenant in ctx and returns its asset key.
	StoreImage(ctx context.Context, img *processedImage) (string, error)
	// Get returns a stored asset for the public endpoint; a malformed path is not found without a storage call.
	Get(ctx context.Context, req dto.PublicAssetRequest) (*dto.PublicAsset, error)
	// Read downloads a stored asset by its key.
	Read(ctx context.Context, key string) ([]byte, error)
}

type publicAssetService struct {
	ServiceParams
}

func NewPublicAssetService(params ServiceParams) PublicAssetService {
	return &publicAssetService{ServiceParams: params}
}

func (s *publicAssetService) StoreImage(ctx context.Context, img *processedImage) (string, error) {
	store, err := s.StorageResolver.ForPlatform(ctx, storage.PurposePublicAsset)
	if err != nil {
		return "", err
	}
	bc, err := s.StorageResolver.BucketConfigFor(storage.PurposePublicAsset)
	if err != nil {
		return "", err
	}

	key := publicAssetKey(types.GetTenantID(ctx), img)
	objectKey := publicAssetObjectKey(bc.KeyPrefix, key)
	exists, err := store.Exists(ctx, objectKey)
	if err != nil {
		s.Logger.Error(ctx, "public asset existence check failed", "error", err, "key", key)
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
		s.Logger.Error(ctx, "public asset upload failed", "error", err, "key", key)
		return "", err
	}
	return key, nil
}

func (s *publicAssetService) Get(ctx context.Context, req dto.PublicAssetRequest) (*dto.PublicAsset, error) {
	key, ext, hash, ok := parsePublicAssetPath(req.TenantID, req.File)
	if !ok {
		return nil, ierr.NewError("asset not found").WithHint("Asset not found").Mark(ierr.ErrNotFound)
	}

	asset := &dto.PublicAsset{ContentType: "image/png", ETag: `"` + hash + `"`}
	if ext == "jpg" {
		asset.ContentType = "image/jpeg"
	}
	if req.IfNoneMatch == asset.ETag {
		asset.NotModified = true
		return asset, nil
	}

	var err error
	asset.Data, err = s.Read(ctx, key)
	if err != nil {
		if !ierr.IsNotFound(err) {
			s.Logger.Error(ctx, "public asset read failed", "error", err, "tenant_id", req.TenantID, "key", key)
		}
		return nil, err
	}
	return asset, nil
}

func (s *publicAssetService) Read(ctx context.Context, key string) ([]byte, error) {
	store, err := s.StorageResolver.ForPlatform(ctx, storage.PurposePublicAsset)
	if err != nil {
		return nil, err
	}
	bc, err := s.StorageResolver.BucketConfigFor(storage.PurposePublicAsset)
	if err != nil {
		return nil, err
	}
	return store.Download(ctx, publicAssetObjectKey(bc.KeyPrefix, key))
}

// publicAssetObjectKey is the storage object for an asset key <tenant>/<file>: <prefix>/<tenant>/assets/<file>.
func publicAssetObjectKey(prefix, key string) string {
	tenantDir, file := path.Split(key)
	ext := path.Ext(file)
	return storage.ObjectKey(prefix, path.Join(tenantDir, "assets"), strings.TrimSuffix(file, ext), strings.TrimPrefix(ext, "."), false)
}

// publicAssetURL is the public URL of an asset key, or "" when unset or no public origin is configured.
func publicAssetURL(cfg *config.Configuration, key *string) string {
	if key == nil || cfg == nil || cfg.Assets.BaseURL == "" {
		return ""
	}
	return strings.TrimRight(cfg.Assets.BaseURL, "/") + "/v1/assets/" + *key
}

const (
	assetMaxPixels   = 4_000_000
	assetJPEGQuality = 90
)

var assetFilePattern = regexp.MustCompile(`^([0-9a-f]{64})\.(png|jpg)$`)
var assetIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,50}$`)

// processedImage is an uploaded image after checks and re-encoding.
type processedImage struct {
	data []byte
	ext  string
	hash string
}

func (i *processedImage) Data() []byte {
	if i == nil {
		return nil
	}
	return i.data
}

func (i *processedImage) Ext() string {
	if i == nil {
		return ""
	}
	return i.ext
}

func (i *processedImage) Hash() string {
	if i == nil {
		return ""
	}
	return i.hash
}

func imageRejection(msg string) error {
	return ierr.NewError(msg).WithHint(msg).Mark(ierr.ErrValidation)
}

// processImage checks an upload against the caller's size and dimension rules and re-encodes it in the same format, dropping metadata.
func processImage(data []byte, maxBytes int, checkDimensions func(width, height int) error) (*processedImage, error) {
	if len(data) > maxBytes {
		return nil, imageRejection(fmt.Sprintf("Image must be %d KB or smaller", maxBytes/1024))
	}

	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg") {
		return nil, imageRejection("Only PNG and JPEG images are allowed")
	}
	if cfg.Width*cfg.Height > assetMaxPixels {
		return nil, imageRejection("Image must be 4 megapixels or smaller")
	}
	if err := checkDimensions(cfg.Width, cfg.Height); err != nil {
		return nil, err
	}

	var out bytes.Buffer
	ext := "png"
	if format == "png" {
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, imageRejection("Only PNG and JPEG images are allowed")
		}
		err = png.Encode(&out, img)
		if err != nil {
			return nil, ierr.WithError(err).WithHint("Could not process the image").Mark(ierr.ErrSystem)
		}
	} else {
		ext = "jpg"
		img, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, imageRejection("Only PNG and JPEG images are allowed")
		}
		err = jpeg.Encode(&out, img, &jpeg.Options{Quality: assetJPEGQuality})
		if err != nil {
			return nil, ierr.WithError(err).WithHint("Could not process the image").Mark(ierr.ErrSystem)
		}
	}

	sum := sha256.Sum256(out.Bytes())
	return &processedImage{data: out.Bytes(), ext: ext, hash: hex.EncodeToString(sum[:])}, nil
}

// publicAssetKey is the asset key: <tenant>/<sha256>.<ext>.
func publicAssetKey(tenantID string, img *processedImage) string {
	return fmt.Sprintf("%s/%s.%s", tenantID, img.Hash(), img.Ext())
}

// parsePublicAssetPath validates public asset path segments and returns the asset key.
func parsePublicAssetPath(tenantID, file string) (key, ext, hash string, ok bool) {
	if !assetIDPattern.MatchString(tenantID) {
		return "", "", "", false
	}
	m := assetFilePattern.FindStringSubmatch(file)
	if m == nil {
		return "", "", "", false
	}
	return tenantID + "/" + file, m[2], m[1], true
}
