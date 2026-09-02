package storage_test

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/storage"
	"github.com/stretchr/testify/require"
)

func TestNewPlatformStorage_S3Provider_ExplicitOverride(t *testing.T) {
	cfg := &config.Configuration{
		Storage: config.StorageConfig{Provider: "s3"},
		S3: config.S3Config{
			Enabled: true,
			Region:  "ap-south-1",
			InvoiceBucketConfig: config.BucketConfig{
				Bucket:                "flexprice-invoices",
				PresignExpiryDuration: "1h",
			},
		},
		FlexpriceS3Exports: config.FlexpriceS3ExportsConfig{
			Bucket:             "flexprice-exports",
			Region:             "ap-south-1",
			AWSAccessKeyID:     "AKIAEXAMPLE",
			AWSSecretAccessKey: "secret",
		},
	}

	s, err := storage.NewPlatformStorage(context.Background(), cfg, storage.ProviderS3, storage.PurposeInvoice, "flexprice-invoices", "ap-south-1", "", logger.NewNoopLogger())
	require.NoError(t, err)
	require.NotNil(t, s)
	require.Equal(t, storage.ProviderS3, s.Provider())
}

// TestNewPlatformStorage_GCSProvider proves the GCS branch constructs
// successfully without explicit credentials, matching a real GKE pod that
// relies on ambient Workload Identity / Application Default Credentials.
// NewPlatformStorage's GCS branch has no config knob to inject a fake
// EndpointURL (unlike gcsbackend.Config directly), so gcsbackend.New's
// underlying cloud.google.com/go storage.NewClient would otherwise try to
// resolve REAL Application Default Credentials at construction time — which
// only "works" on a machine that happens to have `gcloud auth
// application-default login` already run, and fails on CI / a real GKE pod
// without ADC configured. Setting STORAGE_EMULATOR_HOST makes the GCS client
// library skip ADC resolution entirely (it switches to
// option.WithoutAuthentication() internally — see cloud.google.com/go/storage
// NewClient), so this test's result never depends on ambient real-world GCP
// credentials.
func TestNewPlatformStorage_GCSProvider(t *testing.T) {
	t.Setenv("STORAGE_EMULATOR_HOST", "127.0.0.1:1")

	cfg := &config.Configuration{
		Storage: config.StorageConfig{Provider: "gcs"},
	}

	s, err := storage.NewPlatformStorage(context.Background(), cfg, storage.ProviderGCS, storage.PurposeInvoice, "flexprice-invoices", "", "", logger.NewNoopLogger())
	require.NoError(t, err)
	require.NotNil(t, s)
	require.Equal(t, storage.ProviderGCS, s.Provider())
}

// TestNewPlatformStorage_GCSProvider_WithSigner proves signerEmail is forwarded
// into the gcsbackend config for the GCS branch (the resolver validates a
// non-empty signer for invoice storage before ever calling here, but
// NewPlatformStorage itself must still thread whatever it is given through).
func TestNewPlatformStorage_GCSProvider_WithSigner(t *testing.T) {
	t.Setenv("STORAGE_EMULATOR_HOST", "127.0.0.1:1")

	cfg := &config.Configuration{
		Storage: config.StorageConfig{Provider: "gcs"},
		GCS: config.GCSConfig{
			SignerServiceAccountEmail: "signer@flexprice-project.iam.gserviceaccount.com",
		},
	}

	s, err := storage.NewPlatformStorage(context.Background(), cfg, storage.ProviderGCS, storage.PurposeInvoice, "flexprice-invoices", "", cfg.GCS.SignerServiceAccountEmail, logger.NewNoopLogger())
	require.NoError(t, err)
	require.NotNil(t, s)
	require.Equal(t, storage.ProviderGCS, s.Provider())
}

func TestNewPlatformStorage_UnsupportedProvider_ReturnsError(t *testing.T) {
	cfg := &config.Configuration{
		Storage: config.StorageConfig{Provider: "azure"},
	}

	s, err := storage.NewPlatformStorage(context.Background(), cfg, storage.Provider("azure"), storage.PurposeInvoice, "bucket", "region", "", logger.NewNoopLogger())
	require.Error(t, err)
	require.Nil(t, s)
}

// TestNewPlatformStorage_S3Provider_InvalidFlexpriceS3ExportsCreds verifies the
// handoff obligation from Task 6's review: FlexpriceS3ExportsConfig.Validate()
// must actually be invoked at the point platform S3 storage is constructed from
// FlexpriceS3Exports config, since Configuration.Validate() is dead code on the
// real boot path. Neither static keys nor federation are configured here, so
// Validate() must return an error and NewPlatformStorage must propagate it.
func TestNewPlatformStorage_S3Provider_InvalidFlexpriceS3ExportsCreds(t *testing.T) {
	cfg := &config.Configuration{
		Storage: config.StorageConfig{Provider: "s3"},
		S3: config.S3Config{
			Enabled: true,
			Region:  "ap-south-1",
			InvoiceBucketConfig: config.BucketConfig{
				Bucket:                "flexprice-invoices",
				PresignExpiryDuration: "1h",
			},
		},
		FlexpriceS3Exports: config.FlexpriceS3ExportsConfig{
			Bucket: "flexprice-exports",
			Region: "ap-south-1",
			// No static keys, no federation role ARN configured.
		},
	}

	s, err := storage.NewPlatformStorage(context.Background(), cfg, storage.ProviderS3, storage.PurposeExport, "flexprice-exports", "ap-south-1", "", logger.NewNoopLogger())
	require.Error(t, err)
	require.Nil(t, s)
	require.Contains(t, err.Error(), "no credential source configured")
}

// TestNewPlatformStorage_S3Provider_FederationEnabledWithoutRoleARN verifies the
// FederationEnabled-but-no-role-ARN branch of FlexpriceS3ExportsConfig.Validate()
// is also enforced through NewPlatformStorage.
func TestNewPlatformStorage_S3Provider_FederationEnabledWithoutRoleARN(t *testing.T) {
	cfg := &config.Configuration{
		Storage: config.StorageConfig{Provider: "s3"},
		S3: config.S3Config{
			Enabled: true,
			Region:  "ap-south-1",
			InvoiceBucketConfig: config.BucketConfig{
				Bucket:                "flexprice-invoices",
				PresignExpiryDuration: "1h",
			},
		},
		FlexpriceS3Exports: config.FlexpriceS3ExportsConfig{
			Bucket:            "flexprice-exports",
			Region:            "ap-south-1",
			FederationEnabled: true,
			// FederationRoleARN intentionally left empty.
		},
	}

	s, err := storage.NewPlatformStorage(context.Background(), cfg, storage.ProviderS3, storage.PurposeExport, "flexprice-exports", "ap-south-1", "", logger.NewNoopLogger())
	require.Error(t, err)
	require.Nil(t, s)
	require.Contains(t, err.Error(), "federation credentials are selected but federation_role_arn is not set")
}

// TestNewPlatformStorage_S3Provider_FederationEnabledWithRoleARN_FailsLoud verifies
// the fix for the "silent fallback to ambient AWS credentials" finding: when
// FederationEnabled is true AND FederationRoleARN is set (passing
// FlexpriceS3ExportsConfig.Validate()), NewPlatformStorage must NOT proceed to
// construct an s3backend client with FederationTokenSource left nil — doing so
// would let s3backend.New() warn-and-fall-through to the ambient AWS credential
// chain, which resolves nothing on non-AWS compute (e.g. GKE) and looks
// indistinguishable from "just broken". Federation isn't fully wired until the
// companion Terraform+Go token-source implementation lands, so this must fail
// bootstrap loudly instead.
func TestNewPlatformStorage_S3Provider_FederationEnabledWithRoleARN_FailsLoud(t *testing.T) {
	cfg := &config.Configuration{
		Storage: config.StorageConfig{Provider: "s3"},
		S3: config.S3Config{
			Enabled: true,
			Region:  "ap-south-1",
			InvoiceBucketConfig: config.BucketConfig{
				Bucket:                "flexprice-invoices",
				PresignExpiryDuration: "1h",
			},
		},
		FlexpriceS3Exports: config.FlexpriceS3ExportsConfig{
			Bucket:            "flexprice-exports",
			Region:            "ap-south-1",
			FederationEnabled: true,
			FederationRoleARN: "arn:aws:iam::123456789012:role/flexprice-gke-federation",
		},
	}

	s, err := storage.NewPlatformStorage(context.Background(), cfg, storage.ProviderS3, storage.PurposeExport, "flexprice-exports", "ap-south-1", "", logger.NewNoopLogger())
	require.Error(t, err)
	require.Nil(t, s)
	require.Contains(t, err.Error(), "OIDC federation is enabled but not yet fully wired")
}

// TestNewPlatformStorage_S3Provider_InvoiceIgnoresEmptyExportsConfig is the
// regression guard for a backward-compatibility break: invoice storage draws its
// bucket from cfg.S3 and its region from cfg.S3.Region, and never reads
// flexprice_s3_exports at all. Validating that section for PurposeInvoice broke
// every pre-existing AWS deployment that serves invoice PDFs but never enabled
// exports — a legitimately empty section made GetInvoicePDFUrl fail with
// "flexprice S3 exports bucket is not configured".
//
// The failure was lazy rather than at boot (Resolver caches per purpose), so it
// surfaced only on the first invoice-PDF request.
func TestNewPlatformStorage_S3Provider_InvoiceIgnoresEmptyExportsConfig(t *testing.T) {
	cfg := &config.Configuration{
		Storage: config.StorageConfig{Provider: "s3"},
		S3: config.S3Config{
			Enabled: true,
			Region:  "ap-south-1",
			InvoiceBucketConfig: config.BucketConfig{
				Bucket:                "flexprice-invoices",
				PresignExpiryDuration: "1h",
			},
		},
		// FlexpriceS3Exports deliberately zero-valued: exports never enabled.
	}

	s, err := storage.NewPlatformStorage(context.Background(), cfg, storage.ProviderS3, storage.PurposeInvoice, "flexprice-invoices", "ap-south-1", "", logger.NewNoopLogger())
	require.NoError(t, err)
	require.NotNil(t, s)
	require.Equal(t, storage.ProviderS3, s.Provider())
}

// TestNewPlatformStorage_S3Provider_InvoiceIgnoresFederationEnabled pins the
// other half of the same scoping rule: federation is an exports-credential
// concern, so a federation setting left over in config must not make the invoice
// path fail loud. Without the purpose scope this returned "OIDC federation is
// enabled but not yet fully wired" for invoice PDFs.
func TestNewPlatformStorage_S3Provider_InvoiceIgnoresFederationEnabled(t *testing.T) {
	cfg := &config.Configuration{
		Storage: config.StorageConfig{Provider: "s3"},
		S3: config.S3Config{
			Enabled: true,
			Region:  "ap-south-1",
			InvoiceBucketConfig: config.BucketConfig{
				Bucket:                "flexprice-invoices",
				PresignExpiryDuration: "1h",
			},
		},
		FlexpriceS3Exports: config.FlexpriceS3ExportsConfig{
			Bucket:            "flexprice-exports",
			Region:            "ap-south-1",
			FederationEnabled: true,
			FederationRoleARN: "arn:aws:iam::123456789012:role/flexprice-gke-federation",
		},
	}

	s, err := storage.NewPlatformStorage(context.Background(), cfg, storage.ProviderS3, storage.PurposeInvoice, "flexprice-invoices", "ap-south-1", "", logger.NewNoopLogger())
	require.NoError(t, err)
	require.NotNil(t, s)
	require.Equal(t, storage.ProviderS3, s.Provider())
}
