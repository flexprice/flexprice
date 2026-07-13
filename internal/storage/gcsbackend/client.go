package gcsbackend

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"time"

	"cloud.google.com/go/storage"
	gcsoption "google.golang.org/api/option"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	fpstorage "github.com/flexprice/flexprice/internal/storage/storagetypes"
)

const defaultPresignExpiry = 30 * time.Minute

// Config holds everything needed to construct a GCS-backed storage.Storage.
type Config struct {
	Bucket string
	// KeyPrefix is NOT applied by this backend. Prefixing is the caller's
	// responsibility: build the full key via storage.ObjectKey(prefix, ...)
	// before calling Upload/Download/Exists/PresignGet. This field exists so
	// callers that plumb a full job/connection config through can populate it
	// for their own bookkeeping, but the backend never reads it.
	KeyPrefix       string
	CompressionGzip bool
	// ServiceAccountJSON, if set, is used instead of ambient credentials
	// (Workload Identity / Application Default Credentials).
	ServiceAccountJSON []byte
	// SignerServiceAccountEmail is required for PresignGet when running under
	// Workload Identity (ADC can't sign URLs without an explicit signer identity
	// holding roles/iam.serviceAccountTokenCreator / iam.signBlob permission).
	SignerServiceAccountEmail string
	// EndpointURL, if set, overrides the default GCS API endpoint (used to
	// point the client at a local fake/emulator in tests).
	EndpointURL string
}

type client struct {
	gcs    *storage.Client
	cfg    *Config
	logger *logger.Logger
}

// New constructs a GCS-backed storage.Storage. Credential resolution order:
// 1. explicit service account JSON (ServiceAccountJSON), if set
// 2. ambient Application Default Credentials (Workload Identity, etc.)
func New(cfg *Config, log *logger.Logger) (fpstorage.Storage, error) {
	ctx := context.Background()
	var opts []gcsoption.ClientOption
	if len(cfg.ServiceAccountJSON) > 0 {
		opts = append(opts, gcsoption.WithCredentialsJSON(cfg.ServiceAccountJSON))
	}
	if cfg.EndpointURL != "" {
		opts = append(opts, gcsoption.WithEndpoint(cfg.EndpointURL), gcsoption.WithoutAuthentication())
	}

	gcsClient, err := storage.NewClient(ctx, opts...)
	if err != nil {
		return nil, ierr.WithError(err).WithHint("failed to create GCS client").Mark(ierr.ErrHTTPClient)
	}

	return &client{gcs: gcsClient, cfg: cfg, logger: log}, nil
}

func (c *client) Provider() fpstorage.Provider { return fpstorage.ProviderGCS }

func (c *client) FileURL(key string) string {
	return fpstorage.FileURL(fpstorage.ProviderGCS, c.cfg.Bucket, key)
}

func (c *client) Upload(ctx context.Context, req *fpstorage.UploadRequest) (*fpstorage.UploadResponse, error) {
	data := req.Data
	originalSize := int64(len(data))
	compressedSize := originalSize

	if req.Compress && c.cfg.CompressionGzip {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		if _, err := gz.Write(data); err != nil {
			return nil, ierr.WithError(err).WithHint("failed to compress data").Mark(ierr.ErrSystem)
		}
		if err := gz.Close(); err != nil {
			return nil, ierr.WithError(err).WithHint("failed to close gzip writer").Mark(ierr.ErrSystem)
		}
		data = buf.Bytes()
		compressedSize = int64(len(data))
	}

	obj := c.gcs.Bucket(c.cfg.Bucket).Object(req.Key)
	w := obj.NewWriter(ctx)
	if req.ContentType != "" {
		w.ContentType = req.ContentType
	} else {
		w.ContentType = contentTypeFor(req.Format, req.Compress && c.cfg.CompressionGzip)
	}

	if _, err := io.Copy(w, bytes.NewReader(data)); err != nil {
		_ = w.Close()
		return nil, ierr.WithError(err).
			WithHint("failed to upload object to GCS").
			WithMessagef("bucket:%s, key:%s", c.cfg.Bucket, req.Key).
			Mark(ierr.ErrHTTPClient)
	}
	if err := w.Close(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("failed to finalize GCS upload").
			WithMessagef("bucket:%s, key:%s", c.cfg.Bucket, req.Key).
			Mark(ierr.ErrHTTPClient)
	}

	return &fpstorage.UploadResponse{
		FileURL:        c.FileURL(req.Key),
		Bucket:         c.cfg.Bucket,
		Key:            req.Key,
		FileSizeBytes:  originalSize,
		CompressedSize: compressedSize,
		UploadedAt:     time.Now(),
	}, nil
}

func (c *client) Download(ctx context.Context, key string) ([]byte, error) {
	r, err := c.gcs.Bucket(c.cfg.Bucket).Object(key).NewReader(ctx)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("failed to download object from GCS").
			WithMessagef("bucket:%s, key:%s", c.cfg.Bucket, key).
			Mark(ierr.ErrHTTPClient)
	}
	defer r.Close()
	return io.ReadAll(r)
}

func (c *client) Exists(ctx context.Context, key string) (bool, error) {
	_, err := c.gcs.Bucket(c.cfg.Bucket).Object(key).Attrs(ctx)
	if errors.Is(err, storage.ErrObjectNotExist) {
		return false, nil
	}
	if err != nil {
		return false, ierr.WithError(err).WithHint("failed to check object existence").Mark(ierr.ErrHTTPClient)
	}
	return true, nil
}

func (c *client) PresignGet(ctx context.Context, key string, duration time.Duration) (string, error) {
	if duration == 0 {
		duration = defaultPresignExpiry
	}
	opts := &storage.SignedURLOptions{
		Method:  "GET",
		Expires: time.Now().Add(duration),
	}
	if c.cfg.SignerServiceAccountEmail != "" {
		opts.GoogleAccessID = c.cfg.SignerServiceAccountEmail
		opts.Scheme = storage.SigningSchemeV4
	}

	url, err := c.gcs.Bucket(c.cfg.Bucket).SignedURL(key, opts)
	if err != nil {
		return "", ierr.WithError(err).
			WithHint("failed to generate signed url — GCS signing requires the service account to hold iam.signBlob permission").
			WithMessagef("bucket:%s, key:%s", c.cfg.Bucket, key).
			Mark(ierr.ErrHTTPClient)
	}
	return url, nil
}

func (c *client) ValidateConnection(ctx context.Context) error {
	if _, err := c.gcs.Bucket(c.cfg.Bucket).Attrs(ctx); err != nil {
		return ierr.WithError(err).
			WithHint("failed to validate GCS connection - check credentials and bucket name").
			WithMessagef("bucket:%s", c.cfg.Bucket).
			Mark(ierr.ErrHTTPClient)
	}
	return nil
}

func contentTypeFor(format fpstorage.UploadFormat, compressed bool) string {
	if compressed {
		return "application/gzip"
	}
	switch format {
	case fpstorage.UploadFormatCSV:
		return "text/csv"
	case fpstorage.UploadFormatJSON:
		return "application/json"
	case fpstorage.UploadFormatPDF:
		return "application/pdf"
	default:
		return "application/octet-stream"
	}
}
