package storage

import (
	"context"

	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/storage/gcsbackend"
	"github.com/flexprice/flexprice/internal/storage/s3backend"
)

// NewPlatformStorage constructs the Storage instance used for Flexprice-owned
// buckets (invoice PDFs, Flexprice-managed exports). bucket/region are passed
// explicitly per call because invoice storage and export storage may use
// different buckets even though both are platform-owned.
//
// Backend selection: explicit cfg.Storage.Provider wins; otherwise CloudDetector
// picks a default. Platform storage stays on S3 in this rollout — GCS backend
// is available here but not yet the default for any platform bucket.
func NewPlatformStorage(cfg *config.Configuration, bucket, region string, log *logger.Logger) (Storage, error) {
	provider := Provider(cfg.Storage.Provider)
	if provider == "" {
		provider = NewDefaultCloudDetector().Detect(context.Background())
		if provider == "" {
			provider = ProviderS3 // default when detection is inconclusive (local dev, bare metal)
		}
	}

	switch provider {
	case ProviderGCS:
		return gcsbackend.New(&gcsbackend.Config{
			Bucket: bucket,
		}, log)
	case ProviderS3:
		// FlexpriceS3Exports.Validate() is not invoked anywhere on the boot path
		// (Configuration.Validate() is dead code — see task-6-report.md), so this
		// is the only place credential wiring for the platform S3 backend is
		// actually validated. Must run before constructing s3Cfg so a
		// misconfigured credential source fails loudly here rather than lazily
		// inside the AWS SDK's ambient credential chain resolution.
		if err := cfg.FlexpriceS3Exports.Validate(); err != nil {
			return nil, err
		}

		s3Cfg := &s3backend.Config{
			Bucket: bucket,
			Region: region,
		}
		if cfg.FlexpriceS3Exports.AWSAccessKeyID != "" {
			s3Cfg.AWSAccessKeyID = cfg.FlexpriceS3Exports.AWSAccessKeyID
			s3Cfg.AWSSecretAccessKey = cfg.FlexpriceS3Exports.AWSSecretAccessKey
			s3Cfg.AWSSessionToken = cfg.FlexpriceS3Exports.AWSSessionToken
		}
		if cfg.FlexpriceS3Exports.FederationEnabled {
			// FederationTokenSource is wired in Plan 2 once the companion
			// Terraform+Go GCP-identity-token-minting implementation exists.
			// Until then there is no way to actually federate, and letting
			// s3backend.New() warn-and-fall-through to the ambient AWS
			// credential chain is a worse failure mode here than failing
			// loud: on non-AWS compute (e.g. GKE, which is exactly why
			// federation is being built) the ambient chain resolves nothing,
			// so the operator would see no actionable error until every S3
			// call starts failing deep inside the SDK. Fail bootstrap now
			// with a clear, actionable message instead.
			return nil, ierr.NewError("OIDC federation is enabled but not yet fully wired").
				WithHint("FederationEnabled requires a companion Terraform+Go token-source implementation that has not landed yet; either set static AWS credentials, or wait for federation support to complete").
				Mark(ierr.ErrValidation)
		}
		return s3backend.New(s3Cfg, log)
	default:
		return nil, ierr.NewErrorf("unsupported storage provider: %s", provider).
			WithHint("storage.provider must be 's3' or 'gcs'").
			Mark(ierr.ErrValidation)
	}
}
