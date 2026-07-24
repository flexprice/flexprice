package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStorageExportConfig_ValidateForProvider_RegionRequirement covers Finding B:
// Region must be required for S3 but must NOT be required for GCS, since GCS
// buckets in this codebase's usage don't carry a region requirement (see
// gcsbackend.Config, which has no Region field at all).
func TestStorageExportConfig_ValidateForProvider_RegionRequirement(t *testing.T) {
	t.Run("GCS with empty region passes", func(t *testing.T) {
		cfg := &StorageExportConfig{
			Bucket: "my-gcs-bucket",
			Region: "",
		}
		err := cfg.ValidateForProvider(SecretProviderGCS)
		assert.NoError(t, err)
	})

	t.Run("S3 with empty region fails", func(t *testing.T) {
		cfg := &StorageExportConfig{
			Bucket: "my-s3-bucket",
			Region: "",
		}
		err := cfg.ValidateForProvider(SecretProviderS3)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "region")
	})

	t.Run("S3 with region passes", func(t *testing.T) {
		cfg := &StorageExportConfig{
			Bucket: "my-s3-bucket",
			Region: "us-west-2",
		}
		err := cfg.ValidateForProvider(SecretProviderS3)
		assert.NoError(t, err)
	})

	t.Run("flexprice-managed skips region check for any provider", func(t *testing.T) {
		cfg := &StorageExportConfig{
			IsFlexpriceManaged: true,
		}
		assert.NoError(t, cfg.ValidateForProvider(SecretProviderS3))
		assert.NoError(t, cfg.ValidateForProvider(SecretProviderGCS))
	})

	t.Run("nil config is valid", func(t *testing.T) {
		var cfg *StorageExportConfig
		assert.NoError(t, cfg.ValidateForProvider(SecretProviderS3))
	})

	t.Run("missing bucket still fails regardless of provider", func(t *testing.T) {
		cfg := &StorageExportConfig{Region: "us-west-2"}
		err := cfg.ValidateForProvider(SecretProviderS3)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "bucket")
	})

	t.Run("plain Validate (no-arg, used implicitly by Ent) does not require region", func(t *testing.T) {
		// This is the method Ent's generated code calls with no provider-type
		// context; it must never hard-require Region for any provider, since it
		// cannot tell S3 apart from GCS.
		cfg := &StorageExportConfig{
			Bucket: "my-gcs-bucket",
			Region: "",
		}
		assert.NoError(t, cfg.Validate())
	})
}
