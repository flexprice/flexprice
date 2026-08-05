package storage

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveProvider(t *testing.T) {
	t.Run("explicit provider wins over detection", func(t *testing.T) {
		cfg := &config.Configuration{
			Storage: config.StorageConfig{Provider: "gcs"},
		}
		got := ResolveProvider(context.Background(), cfg)
		assert.Equal(t, ProviderGCS, got)
	})

	t.Run("falls back to S3 when provider empty and detection inconclusive", func(t *testing.T) {
		// No cloud metadata server is reachable in the test environment, so
		// CloudDetector.Detect returns "" and ResolveProvider falls back to S3.
		cfg := &config.Configuration{
			Storage: config.StorageConfig{Provider: ""},
		}
		got := ResolveProvider(context.Background(), cfg)
		assert.Equal(t, ProviderS3, got)
	})
}

func testConfig() *config.Configuration {
	return &config.Configuration{
		S3: config.S3Config{
			Enabled: true,
			Region:  "us-east-1",
			InvoiceBucketConfig: config.BucketConfig{
				Bucket:                "s3-invoice-bucket",
				PresignExpiryDuration: "15m",
				KeyPrefix:             "invoices/",
			},
		},
		GCS: config.GCSConfig{
			Enabled: true,
			InvoiceBucketConfig: config.BucketConfig{
				Bucket:                "gcs-invoice-bucket",
				PresignExpiryDuration: "20m",
				KeyPrefix:             "invoices/",
			},
		},
		FlexpriceS3Exports: config.FlexpriceS3ExportsConfig{
			Bucket:             "s3-exports-bucket",
			Region:             "us-west-2",
			AWSAccessKeyID:     "id",
			AWSSecretAccessKey: "secret",
		},
		FlexpriceGCSExports: config.FlexpriceGCSExportsConfig{
			Bucket: "gcs-exports-bucket",
		},
	}
}

func newTestResolver(t *testing.T, provider Provider, cfg *config.Configuration) *resolver {
	t.Helper()
	return &resolver{
		cfg:      cfg,
		provider: provider,
		logger:   logger.NewNoopLogger(),
		platform: make(map[Purpose]Storage),
	}
}

func TestResolver_BucketConfigFor(t *testing.T) {
	tests := []struct {
		name       string
		provider   Provider
		purpose    Purpose
		wantBucket string
	}{
		{"s3 invoice", ProviderS3, PurposeInvoice, "s3-invoice-bucket"},
		{"s3 export", ProviderS3, PurposeExport, "s3-exports-bucket"},
		{"gcs invoice", ProviderGCS, PurposeInvoice, "gcs-invoice-bucket"},
		{"gcs export", ProviderGCS, PurposeExport, "gcs-exports-bucket"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestResolver(t, tt.provider, testConfig())
			bc, err := r.BucketConfigFor(tt.purpose)
			require.NoError(t, err)
			assert.Equal(t, tt.wantBucket, bc.Bucket)

			if tt.purpose == PurposeExport {
				assert.Equal(t, "30m", bc.PresignExpiryDuration)
				assert.Empty(t, bc.KeyPrefix)
			}
		})
	}
}

func TestResolver_BucketConfigFor_EmptyBucket(t *testing.T) {
	tests := []struct {
		name        string
		provider    Provider
		purpose     Purpose
		wantHintSub string
	}{
		{"s3 invoice missing", ProviderS3, PurposeInvoice, "FLEXPRICE_S3_INVOICE_BUCKET"},
		{"s3 export missing", ProviderS3, PurposeExport, "FLEXPRICE_FLEXPRICE_S3_EXPORTS_BUCKET"},
		{"gcs invoice missing", ProviderGCS, PurposeInvoice, "FLEXPRICE_GCS_INVOICE_BUCKET"},
		{"gcs export missing", ProviderGCS, PurposeExport, "FLEXPRICE_FLEXPRICE_GCS_EXPORTS_BUCKET"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Configuration{} // all buckets empty
			r := newTestResolver(t, tt.provider, cfg)
			_, err := r.BucketConfigFor(tt.purpose)
			require.Error(t, err)
			assert.Contains(t, missingBucketHint(tt.provider, tt.purpose), tt.wantHintSub)
		})
	}
}

func TestResolver_BucketConfigFor_UnknownPurpose(t *testing.T) {
	r := newTestResolver(t, ProviderS3, testConfig())
	_, err := r.BucketConfigFor(Purpose("bogus"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported storage purpose")
}

func TestResolver_SignerFor(t *testing.T) {
	cfg := testConfig()
	cfg.GCS.SignerServiceAccountEmail = "invoice-signer@flexprice-project.iam.gserviceaccount.com"
	cfg.FlexpriceGCSExports.SignerServiceAccountEmail = "export-signer@flexprice-project.iam.gserviceaccount.com"

	tests := []struct {
		name       string
		provider   Provider
		purpose    Purpose
		wantSigner string
	}{
		{"gcs invoice uses GCS.SignerServiceAccountEmail", ProviderGCS, PurposeInvoice, "invoice-signer@flexprice-project.iam.gserviceaccount.com"},
		{"gcs export uses FlexpriceGCSExports.SignerServiceAccountEmail", ProviderGCS, PurposeExport, "export-signer@flexprice-project.iam.gserviceaccount.com"},
		{"s3 invoice has no signer", ProviderS3, PurposeInvoice, ""},
		{"s3 export has no signer", ProviderS3, PurposeExport, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestResolver(t, tt.provider, cfg)
			signer, err := r.signerFor(tt.purpose)
			require.NoError(t, err)
			assert.Equal(t, tt.wantSigner, signer)
		})
	}
}

// TestResolver_SignerFor_GCSInvoiceEmptySigner proves an unset GCS invoice
// signer fails loud and names the env var, rather than silently resolving
// to a resolver that can upload but never presign a download link.
func TestResolver_SignerFor_GCSInvoiceEmptySigner(t *testing.T) {
	cfg := testConfig() // GCS.SignerServiceAccountEmail left empty
	r := newTestResolver(t, ProviderGCS, cfg)

	signer, err := r.signerFor(PurposeInvoice)
	require.Error(t, err)
	assert.Empty(t, signer)
	assert.Contains(t, err.Error(), "FLEXPRICE_GCS_SIGNER_SERVICE_ACCOUNT_EMAIL")
}

// TestResolver_ForPlatform_GCSInvoiceEmptySigner proves ForPlatform surfaces
// the signer error end to end (not just the unexported signerFor helper).
func TestResolver_ForPlatform_GCSInvoiceEmptySigner(t *testing.T) {
	cfg := testConfig() // GCS.SignerServiceAccountEmail left empty
	r := newTestResolver(t, ProviderGCS, cfg)

	s, err := r.ForPlatform(context.Background(), PurposeInvoice)
	require.Error(t, err)
	assert.Nil(t, s)
	assert.Contains(t, err.Error(), "FLEXPRICE_GCS_SIGNER_SERVICE_ACCOUNT_EMAIL")
}

// TestResolver_ForPlatform_InvoiceWorksWithoutExportsConfig exercises the real
// call path that regressed: an existing AWS deployment with invoice S3 fully
// configured and flexprice_s3_exports never set. ForPlatform(PurposeInvoice)
// must construct successfully, because invoice storage does not read that
// section. Previously it failed with "flexprice S3 exports bucket is not
// configured" on the first invoice-PDF request.
func TestResolver_ForPlatform_InvoiceWorksWithoutExportsConfig(t *testing.T) {
	cfg := &config.Configuration{
		S3: config.S3Config{
			Enabled: true,
			Region:  "us-east-1",
			InvoiceBucketConfig: config.BucketConfig{
				Bucket:                "s3-invoice-bucket",
				PresignExpiryDuration: "15m",
			},
		},
		// FlexpriceS3Exports deliberately zero-valued.
	}
	r := newTestResolver(t, ProviderS3, cfg)

	s, err := r.ForPlatform(context.Background(), PurposeInvoice)
	require.NoError(t, err)
	require.NotNil(t, s)
	assert.Equal(t, ProviderS3, s.Provider())

	// The export purpose still enforces the exports config, so a deployment that
	// actually uses exports keeps failing loud instead of silently misresolving.
	_, err = r.ForPlatform(context.Background(), PurposeExport)
	require.Error(t, err)
}

func TestResolver_ForPlatform_Caches(t *testing.T) {
	cfg := testConfig()
	r := newTestResolver(t, ProviderS3, cfg)

	s1, err := r.ForPlatform(context.Background(), PurposeExport)
	require.NoError(t, err)
	require.NotNil(t, s1)

	s2, err := r.ForPlatform(context.Background(), PurposeExport)
	require.NoError(t, err)

	assert.Same(t, s1, s2)
}
