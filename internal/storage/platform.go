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
			s3Cfg.FederationRoleARN = cfg.FlexpriceS3Exports.FederationRoleARN
			// FederationTokenSource is wired in Plan 2 once the GCP identity
			// token minting implementation exists; nil here means New() falls
			// through to the ambient chain if federation role ARN alone is set
			// without a token source — acceptable for this plan since federation
			// isn't actually exercised until Plan 2 lands.
		}
		return s3backend.New(s3Cfg, log)
	default:
		return nil, ierr.NewErrorf("unsupported storage provider: %s", provider).
			WithHint("storage.provider must be 's3' or 'gcs'").
			Mark(ierr.ErrValidation)
	}
}
