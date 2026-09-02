package storage

import (
	"context"
	"sync"

	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
)

// Purpose selects which Flexprice-owned bucket a request targets. Invoice PDFs
// and exports live in different buckets with their own prefix and presign expiry.
type Purpose string

const (
	PurposeInvoice Purpose = "invoice"
	PurposeExport  Purpose = "export"
	// PurposeImport is the Flexprice-owned bucket that trusted upstream
	// uploaders (e.g. CSV Box) write into. The /tasks import API accepts an
	// upload_id and resolves it to a key under this bucket — the caller never
	// supplies a URL.
	PurposeImport Purpose = "import"
)

// Resolver selects the Storage for an operation. Platform storage (invoices,
// managed exports) follows the deployment's own cloud and is resolved once and
// cached; connection storage (customer BYOB) is resolved per call from the DB,
// since credentials live on the connection row.
type Resolver interface {
	ForPlatform(ctx context.Context, purpose Purpose) (Storage, error)
	ForConnection(ctx context.Context, connectionID string) (Storage, error)
	Provider() Provider
	// BucketConfigFor returns provider-specific bucket settings (prefix, presign
	// expiry) for a purpose. Reading cfg.S3.* directly is what hardcoded the
	// invoice path to S3.
	BucketConfigFor(purpose Purpose) (config.BucketConfig, error)
}

// ConnectionStorageProvider is the narrow slice of internal/integration.Factory
// the resolver needs. Declared here, not imported, to avoid an import cycle
// (internal/integration already imports this package). cmd/server wires it in.
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

// NewResolver resolves the platform provider once at bootstrap (CloudDetector
// blocks on metadata probes, so it must not run per call).
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

func (r *resolver) Provider() Provider { return r.provider }

// providerFor picks the effective backend for a purpose. It is normally the
// deployment-wide provider (r.provider, chosen at boot by CloudDetector), but
// imports are a deliberate exception: CSV Box only writes to S3, so an import
// running on a GCP-hosted worker still reads its source object from S3 using
// static credentials from FlexpriceS3Imports. Without this override, GCP
// deployments hit the GCS branch and can never resolve an S3-backed CSV.
func (r *resolver) providerFor(purpose Purpose) Provider {
	if purpose == PurposeImport {
		return ProviderS3
	}
	return r.provider
}

func (r *resolver) ForPlatform(ctx context.Context, purpose Purpose) (Storage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if s, ok := r.platform[purpose]; ok {
		return s, nil
	}

	if err := r.checkProviderEnabled(purpose); err != nil {
		return nil, err
	}

	bucket, region, err := r.platformBucket(purpose)
	if err != nil {
		return nil, err
	}

	signerEmail, err := r.signerFor(purpose)
	if err != nil {
		return nil, err
	}

	s, err := NewPlatformStorage(ctx, r.cfg, r.providerFor(purpose), purpose, bucket, region, signerEmail, r.logger)
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

// signerFor returns the GCS signing identity for a purpose (S3 signs with the
// request credentials, so it returns empty). A missing signer is rejected here,
// not at first presign: uploads would still succeed, but every presigned GET
// would fail — surfacing only when a customer clicks a download link.
//
// Imports always go through the S3 branch (providerFor pins them), so this
// method never has to produce a GCS import signer — the S3 short-circuit at
// the top handles them.
func (r *resolver) signerFor(purpose Purpose) (string, error) {
	if r.providerFor(purpose) != ProviderGCS {
		return "", nil
	}

	switch purpose {
	case PurposeInvoice:
		signer := r.cfg.GCS.SignerServiceAccountEmail
		if signer == "" {
			// Env var name goes in the message: ierr's Error() renders only
			// "Code: Message", so a hint-only name is invisible in logs.
			return "", ierr.NewError("no GCS signer service account configured for invoice storage; set FLEXPRICE_GCS_SIGNER_SERVICE_ACCOUNT_EMAIL").
				WithHint("Set gcs.signer_service_account_email (FLEXPRICE_GCS_SIGNER_SERVICE_ACCOUNT_EMAIL) to a service account with roles/iam.serviceAccountTokenCreator; uploads would succeed but every presigned invoice PDF download link would fail without it").
				Mark(ierr.ErrValidation)
		}
		return signer, nil
	case PurposeExport:
		signer := r.cfg.FlexpriceGCSExports.SignerServiceAccountEmail
		if signer == "" {
			return "", ierr.NewError("no GCS signer service account configured for export storage; set FLEXPRICE_FLEXPRICE_GCS_EXPORTS_SIGNER_SERVICE_ACCOUNT_EMAIL").
				WithHint("Set flexprice_gcs_exports.signer_service_account_email (FLEXPRICE_FLEXPRICE_GCS_EXPORTS_SIGNER_SERVICE_ACCOUNT_EMAIL) to a service account with roles/iam.serviceAccountTokenCreator; uploads would succeed but every presigned export download link would fail without it").
				Mark(ierr.ErrValidation)
		}
		return signer, nil
	default:
		return "", unsupportedPurpose(purpose)
	}
}

// checkProviderEnabled enforces the s3.enabled / gcs.enabled kill switch, which
// has only ever gated invoice PDFs (exports have their own config). Preserves the
// pre-refactor behavior where storage-off deployments failed fast instead of
// issuing live bucket calls. Import has its own enabled flag on
// FlexpriceS3Imports, checked in platformBucket.
func (r *resolver) checkProviderEnabled(purpose Purpose) error {
	if purpose != PurposeInvoice {
		return nil
	}

	switch r.provider {
	case ProviderS3:
		if !r.cfg.S3.Enabled {
			return ierr.NewError("s3 is not enabled").
				WithHint("s3 is not enabled but is required to generate invoice pdf url.").
				Mark(ierr.ErrSystem)
		}
	case ProviderGCS:
		if !r.cfg.GCS.Enabled {
			return ierr.NewError("gcs is not enabled").
				WithHint("gcs is not enabled but is required to generate invoice pdf url.").
				Mark(ierr.ErrSystem)
		}
	}

	return nil
}

// platformBucket returns the bucket and region for a purpose. Region is empty
// for GCS (bucket location is fixed at creation).
func (r *resolver) platformBucket(purpose Purpose) (bucket, region string, err error) {
	bc, err := r.BucketConfigFor(purpose)
	if err != nil {
		return "", "", err
	}

	switch r.providerFor(purpose) {
	case ProviderS3:
		switch purpose {
		case PurposeInvoice:
			region = r.cfg.S3.Region
		case PurposeImport:
			region = r.cfg.FlexpriceS3Imports.Region
		default:
			region = r.cfg.FlexpriceS3Exports.Region
		}
	}

	return bc.Bucket, region, nil
}

// BucketConfigFor returns bucket settings for a purpose. Invoice buckets carry a
// full config.BucketConfig; export config is flat (no prefix/expiry fields), so
// export synthesizes one with expiry "30m" to match defaultPresignExpiry.
//
// Imports are pinned to S3 (see providerFor): CSV Box only writes to S3, so
// even a GCP-hosted deployment reads its imports through the S3 branch below,
// using FlexpriceS3Imports credentials.
func (r *resolver) BucketConfigFor(purpose Purpose) (config.BucketConfig, error) {
	var bc config.BucketConfig

	switch r.providerFor(purpose) {
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
		case PurposeImport:
			// Imports are opt-in per deployment; refuse if disabled so the
			// downstream download failure is caught here with a clear hint
			// instead of surfacing as an S3 auth error later.
			if !r.cfg.FlexpriceS3Imports.Enabled {
				return config.BucketConfig{}, ierr.NewError("csv imports are not enabled on this deployment").
					WithHint("Set flexprice_s3_imports.enabled=true (FLEXPRICE_FLEXPRICE_S3_IMPORTS_ENABLED) and configure bucket/credentials").
					Mark(ierr.ErrInvalidOperation)
			}
			bc = config.BucketConfig{
				Bucket:                r.cfg.FlexpriceS3Imports.Bucket,
				KeyPrefix:             r.cfg.FlexpriceS3Imports.KeyPrefix,
				PresignExpiryDuration: "5m",
			}
		default:
			return config.BucketConfig{}, unsupportedPurpose(purpose)
		}
	default:
		return config.BucketConfig{}, ierr.NewErrorf("unsupported storage provider: %s", r.providerFor(purpose)).
			WithHint("storage.provider must be 's3' or 'gcs'").
			Mark(ierr.ErrValidation)
	}

	if bc.Bucket == "" {
		return config.BucketConfig{}, ierr.NewErrorf("no %s bucket configured for storage provider %s", purpose, r.providerFor(purpose)).
			WithHint(missingBucketHint(r.providerFor(purpose), purpose)).
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
	case provider == ProviderS3 && purpose == PurposeImport:
		return "Set flexprice_s3_imports.bucket (FLEXPRICE_FLEXPRICE_S3_IMPORTS_BUCKET)"
	default:
		return "Set flexprice_s3_exports.bucket (FLEXPRICE_FLEXPRICE_S3_EXPORTS_BUCKET)"
	}
}
