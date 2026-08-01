package storage

import (
	"context"
	"sync"

	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
)

// Purpose identifies which Flexprice-owned bucket a platform storage request
// targets. Invoice PDFs and exports live in different buckets even though both
// are platform-owned, and each has its own key prefix and presign expiry.
type Purpose string

const (
	PurposeInvoice Purpose = "invoice"
	PurposeExport  Purpose = "export"
)

// Resolver answers "which Storage for this operation?" for both storage classes.
//
// Platform storage (invoice PDFs, Flexprice-managed exports) is same-cloud by
// definition: the bucket follows the deployment's own cloud, and credentials
// come from that cloud's ambient identity or configured keys. It is resolved
// once and cached.
//
// Connection storage (customer bring-your-own-bucket) is resolved per call
// against the database, because credentials live on the connection row and may
// change between calls.
type Resolver interface {
	ForPlatform(ctx context.Context, purpose Purpose) (Storage, error)
	ForConnection(ctx context.Context, connectionID string) (Storage, error)
	// Provider reports the resolved platform storage provider.
	Provider() Provider
	// BucketConfigFor returns the bucket settings (key prefix, presign expiry)
	// for a platform purpose under the resolved provider. Callers need this
	// because key layout and presign expiry are provider-specific config, and
	// reading cfg.S3.* directly is what hardcoded the invoice path to S3.
	BucketConfigFor(purpose Purpose) (config.BucketConfig, error)
}

// ConnectionStorageProvider is the narrow slice of internal/integration.Factory
// that the resolver needs. Declared here rather than imported because
// internal/integration already imports this package; depending on it directly
// would create a cycle. cmd/server wires the concrete factory in.
type ConnectionStorageProvider interface {
	GetStorageProvider(ctx context.Context, connectionID string) (Storage, error)
}

type resolver struct {
	cfg      *config.Configuration
	provider Provider
	connSvc  ConnectionStorageProvider
	logger   *logger.Logger

	mu       sync.Mutex
	platform map[Purpose]Storage
}

// NewResolver constructs the Resolver. The platform storage provider is resolved
// once here, at application bootstrap: CloudDetector performs blocking metadata
// probes (500ms timeout each), so resolving per call would add that latency to
// every request and could yield inconsistent answers if a probe flaked.
func NewResolver(ctx context.Context, cfg *config.Configuration, connSvc ConnectionStorageProvider, log *logger.Logger) Resolver {
	provider := ResolveProvider(ctx, cfg)
	log.Info(ctx, "resolved platform storage provider", "provider", string(provider))

	return &resolver{
		cfg:      cfg,
		provider: provider,
		connSvc:  connSvc,
		logger:   log,
		platform: make(map[Purpose]Storage),
	}
}

// Provider returns the resolved platform storage provider. Callers that need to
// select provider-specific configuration (bucket, key prefix, presign expiry)
// use this rather than re-running detection.
func (r *resolver) Provider() Provider { return r.provider }

func (r *resolver) ForPlatform(ctx context.Context, purpose Purpose) (Storage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if s, ok := r.platform[purpose]; ok {
		return s, nil
	}

	bucket, region, err := r.platformBucket(purpose)
	if err != nil {
		return nil, err
	}

	s, err := NewPlatformStorage(ctx, r.cfg, r.provider, bucket, region, r.logger)
	if err != nil {
		return nil, err
	}

	r.platform[purpose] = s
	return s, nil
}

func (r *resolver) ForConnection(ctx context.Context, connectionID string) (Storage, error) {
	if r.connSvc == nil {
		return nil, ierr.NewError("connection storage is not configured").
			WithHint("No connection storage provider was wired into the storage resolver").
			Mark(ierr.ErrSystem)
	}
	return r.connSvc.GetStorageProvider(ctx, connectionID)
}

// platformBucket selects the bucket and region for a purpose under the resolved
// provider. Region is meaningless for GCS — a bucket's location is fixed at
// creation — so it is returned empty there.
func (r *resolver) platformBucket(purpose Purpose) (bucket, region string, err error) {
	bc, err := r.BucketConfigFor(purpose)
	if err != nil {
		return "", "", err
	}

	switch r.provider {
	case ProviderS3:
		if purpose == PurposeInvoice {
			region = r.cfg.S3.Region
		} else {
			region = r.cfg.FlexpriceS3Exports.Region
		}
	}

	return bc.Bucket, region, nil
}

// BucketConfigFor returns the bucket settings for a platform purpose under the
// resolved provider.
//
// Invoice buckets already carry a full config.BucketConfig (bucket, key prefix,
// presign expiry) in the S3/GCS config sections. Export buckets do not: the
// Flexprice-managed exports config (FlexpriceS3ExportsConfig /
// FlexpriceGCSExportsConfig) is a flat struct describing the Flexprice-owned
// bucket and its credentials/identity, with no KeyPrefix or
// PresignExpiryDuration fields — export key prefixes are per-connection
// (SyncConfig), not global. So for PurposeExport this synthesizes a
// BucketConfig: bucket from the exports config, empty KeyPrefix, and
// PresignExpiryDuration hardcoded to "30m" to match defaultPresignExpiry in
// both s3backend and gcsbackend.
func (r *resolver) BucketConfigFor(purpose Purpose) (config.BucketConfig, error) {
	var bc config.BucketConfig

	switch r.provider {
	case ProviderGCS:
		switch purpose {
		case PurposeInvoice:
			bc = r.cfg.GCS.InvoiceBucketConfig
		case PurposeExport:
			bc = config.BucketConfig{
				Bucket:                r.cfg.FlexpriceGCSExports.Bucket,
				PresignExpiryDuration: "30m",
			}
		default:
			return config.BucketConfig{}, unsupportedPurpose(purpose)
		}
	case ProviderS3:
		switch purpose {
		case PurposeInvoice:
			bc = r.cfg.S3.InvoiceBucketConfig
		case PurposeExport:
			bc = config.BucketConfig{
				Bucket:                r.cfg.FlexpriceS3Exports.Bucket,
				PresignExpiryDuration: "30m",
			}
		default:
			return config.BucketConfig{}, unsupportedPurpose(purpose)
		}
	default:
		return config.BucketConfig{}, ierr.NewErrorf("unsupported storage provider: %s", r.provider).
			WithHint("storage.provider must be 's3' or 'gcs'").
			Mark(ierr.ErrValidation)
	}

	if bc.Bucket == "" {
		return config.BucketConfig{}, ierr.NewErrorf("no %s bucket configured for storage provider %s", purpose, r.provider).
			WithHint(missingBucketHint(r.provider, purpose)).
			Mark(ierr.ErrValidation)
	}

	return bc, nil
}

func unsupportedPurpose(purpose Purpose) error {
	return ierr.NewErrorf("unsupported storage purpose: %s", purpose).
		WithHint("purpose must be 'invoice' or 'export'").
		Mark(ierr.ErrValidation)
}

// missingBucketHint names the exact env var an operator must set, since the
// config key differs per provider and purpose.
func missingBucketHint(provider Provider, purpose Purpose) string {
	switch {
	case provider == ProviderGCS && purpose == PurposeInvoice:
		return "Set gcs.invoice.bucket (FLEXPRICE_GCS_INVOICE_BUCKET)"
	case provider == ProviderGCS && purpose == PurposeExport:
		return "Set flexprice_gcs_exports.bucket (FLEXPRICE_FLEXPRICE_GCS_EXPORTS_BUCKET)"
	case provider == ProviderS3 && purpose == PurposeInvoice:
		return "Set s3.invoice.bucket (FLEXPRICE_S3_INVOICE_BUCKET)"
	default:
		return "Set flexprice_s3_exports.bucket (FLEXPRICE_FLEXPRICE_S3_EXPORTS_BUCKET)"
	}
}
