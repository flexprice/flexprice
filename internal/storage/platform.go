package storage

import (
	"context"

	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/storage/gcsbackend"
	"github.com/flexprice/flexprice/internal/storage/s3backend"
)

// ResolveProvider determines which backend platform storage uses: an explicit
// cfg.Storage.Provider wins, otherwise CloudDetector probes the cloud metadata
// endpoints, falling back to S3 when detection is inconclusive (local dev, bare
// metal).
//
// Detection performs blocking HTTP probes (500ms timeout each), so callers must
// resolve once at bootstrap and pass the result down rather than calling this
// per request. Resolver does exactly that.
func ResolveProvider(ctx context.Context, cfg *config.Configuration) Provider {
	if provider := Provider(cfg.Storage.Provider); provider != "" {
		return provider
	}
	if provider := NewDefaultCloudDetector().Detect(ctx); provider != "" {
		return provider
	}
	return ProviderS3
}

// NewPlatformStorage constructs the Storage instance used for Flexprice-owned
// buckets (invoice PDFs, Flexprice-managed exports). provider/bucket/region and
// signerEmail are passed explicitly because invoice storage and export storage
// may use different buckets and different signing identities even though both
// are platform-owned, and because provider detection must happen once at
// bootstrap (see ResolveProvider) rather than on each construction.
//
// signerEmail applies to GCS only and is ignored for S3, where presigned URLs
// are signed with the request credentials themselves. Mapping a purpose to its
// signer is the Resolver's job, which is why signerEmail is a plain string.
//
// purpose is taken separately because credential validation is purpose-scoped:
// only PurposeExport reads the flexprice_s3_exports section, so only it may
// enforce that section's requirements (see the S3 branch).
func NewPlatformStorage(ctx context.Context, cfg *config.Configuration, provider Provider, purpose Purpose, bucket, region, signerEmail string, log *logger.Logger) (Storage, error) {
	switch provider {
	case ProviderGCS:
		return gcsbackend.New(ctx, &gcsbackend.Config{
			Bucket:                    bucket,
			SignerServiceAccountEmail: signerEmail,
		}, log)
	case ProviderS3:
		// FlexpriceS3Exports.Validate() is not invoked anywhere on the boot path
		// (Configuration.Validate() is dead code — see task-6-report.md), so this
		// is the only place credential wiring for the platform S3 backend is
		// actually validated. Must run before constructing s3Cfg so a
		// misconfigured credential source fails loudly here rather than lazily
		// inside the AWS SDK's ambient credential chain resolution.
		//
		// Scoped to PurposeExport deliberately. Invoice storage draws its bucket
		// and region from cfg.S3 and never reads flexprice_s3_exports, so
		// validating that section for PurposeInvoice would break every existing
		// deployment that serves invoice PDFs without having enabled exports —
		// the section is legitimately empty there.
		if purpose == PurposeExport {
			if err := cfg.FlexpriceS3Exports.Validate(); err != nil {
				return nil, err
			}
		}

		s3Cfg := &s3backend.Config{
			Bucket: bucket,
			Region: region,
		}
		// Static keys from flexprice_s3_exports are export-scoped (see the
		// purpose comment above): PurposeInvoice must not pick them up, or an
		// export-bucket-only credential would break invoice PDF upload/presign.
		if purpose == PurposeExport && cfg.FlexpriceS3Exports.AWSAccessKeyID != "" {
			s3Cfg.AWSAccessKeyID = cfg.FlexpriceS3Exports.AWSAccessKeyID
			s3Cfg.AWSSecretAccessKey = cfg.FlexpriceS3Exports.AWSSecretAccessKey
			s3Cfg.AWSSessionToken = cfg.FlexpriceS3Exports.AWSSessionToken
		}
		if purpose == PurposeExport && cfg.FlexpriceS3Exports.FederationEnabled {
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
		return s3backend.New(ctx, s3Cfg, log)
	default:
		return nil, ierr.NewErrorf("unsupported storage provider: %s", provider).
			WithHint("storage.provider must be 's3' or 'gcs'").
			Mark(ierr.ErrValidation)
	}
}
