package storage_test

import (
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

	s, err := storage.NewPlatformStorage(cfg, "flexprice-invoices", "ap-south-1", logger.NewNoopLogger())
	require.NoError(t, err)
	require.NotNil(t, s)
	require.Equal(t, storage.ProviderS3, s.Provider())
}

func TestNewPlatformStorage_GCSProvider(t *testing.T) {
	cfg := &config.Configuration{
		Storage: config.StorageConfig{Provider: "gcs"},
	}

	s, err := storage.NewPlatformStorage(cfg, "flexprice-invoices", "", logger.NewNoopLogger())
	require.NoError(t, err)
	require.NotNil(t, s)
	require.Equal(t, storage.ProviderGCS, s.Provider())
}

func TestNewPlatformStorage_UnsupportedProvider_ReturnsError(t *testing.T) {
	cfg := &config.Configuration{
		Storage: config.StorageConfig{Provider: "azure"},
	}

	s, err := storage.NewPlatformStorage(cfg, "bucket", "region", logger.NewNoopLogger())
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

	s, err := storage.NewPlatformStorage(cfg, "flexprice-invoices", "ap-south-1", logger.NewNoopLogger())
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

	s, err := storage.NewPlatformStorage(cfg, "flexprice-invoices", "ap-south-1", logger.NewNoopLogger())
	require.Error(t, err)
	require.Nil(t, s)
	require.Contains(t, err.Error(), "federation_enabled is true but federation_role_arn is not set")
}
