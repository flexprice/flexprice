package storage

import (
	"context"

	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/storage/gcsbackend"
	"github.com/flexprice/flexprice/internal/storage/s3backend"
)

// ResolveProvider picks the backend: explicit cfg.Storage.Provider wins, else
// CloudDetector probes metadata endpoints, falling back to S3. Detection blocks
// on HTTP probes, so callers resolve once at bootstrap (Resolver does this).
func ResolveProvider(ctx context.Context, cfg *config.Configuration) Provider {
	return resolveProviderWith(ctx, cfg, NewDefaultCloudDetector())
}

func resolveProviderWith(ctx context.Context, cfg *config.Configuration, detector *CloudDetector) Provider {
	if provider := Provider(cfg.Storage.Provider); provider != "" {
		return provider
	}
	if provider := detector.Detect(ctx); provider != "" {
		return provider
	}
	return ProviderS3
}

// NewPlatformStorage constructs Storage for a Flexprice-owned bucket. Invoice and
// export storage may differ in bucket, region and signer, so those are explicit
// args. signerEmail applies to GCS only (S3 signs with the request credentials).
func NewPlatformStorage(ctx context.Context, cfg *config.Configuration, provider Provider, purpose Purpose, bucket, region, signerEmail string, log *logger.Logger) (Storage, error) {
	switch provider {
	case ProviderGCS:
		return gcsbackend.New(ctx, &gcsbackend.Config{
			Bucket:                    bucket,
			SignerServiceAccountEmail: signerEmail,
		}, log)
	case ProviderS3:
		// Only the export path reads flexprice_s3_exports, so only it validates
		// that section; validating it for invoices would break deployments that
		// serve invoice PDFs without exports enabled. This is the sole validation
		// point (Configuration.Validate() is not on the boot path).
		if purpose == PurposeExport {
			if err := cfg.FlexpriceS3Exports.Validate(); err != nil {
				return nil, err
			}
		}

		s3Cfg := &s3backend.Config{
			Bucket: bucket,
			Region: region,
		}
		// Export-scoped static keys: invoices must not pick them up.
		if purpose == PurposeExport && cfg.FlexpriceS3Exports.AWSAccessKeyID != "" {
			s3Cfg.AWSAccessKeyID = cfg.FlexpriceS3Exports.AWSAccessKeyID
			s3Cfg.AWSSecretAccessKey = cfg.FlexpriceS3Exports.AWSSecretAccessKey
			s3Cfg.AWSSessionToken = cfg.FlexpriceS3Exports.AWSSessionToken
		}
		// Import-scoped static keys: same scoping discipline as exports so a
		// misconfigured section can't leak credentials into the invoice path.
		if purpose == PurposeImport && cfg.FlexpriceS3Imports.AWSAccessKeyID != "" {
			s3Cfg.AWSAccessKeyID = cfg.FlexpriceS3Imports.AWSAccessKeyID
			s3Cfg.AWSSecretAccessKey = cfg.FlexpriceS3Imports.AWSSecretAccessKey
			s3Cfg.AWSSessionToken = cfg.FlexpriceS3Imports.AWSSessionToken
		}
		if purpose == PurposeExport && cfg.FlexpriceS3Exports.ResolvedCredentialSource() == config.CredentialSourceFederation {
			// Federation has no token source yet (Plan 2). Fail loudly at boot
			// rather than let s3backend fall through to the ambient chain, which
			// resolves nothing on non-AWS compute and fails deep in the SDK.
			return nil, ierr.NewError("OIDC federation is enabled but not yet fully wired").
				WithHint("FederationEnabled requires a companion Terraform+Go token-source implementation that has not landed yet; either set static AWS credentials, or wait for federation support to complete").
				Mark(ierr.ErrValidation)
		}
		return s3backend.New(ctx, s3Cfg, log)
	default:
		return nil, ierr.NewErrorf("unsupported storage provider: %s", provider).
			WithHint("storage.provider must be 's3' or 'gcs'").
			Mark(ierr.ErrValidation)
	}
}
