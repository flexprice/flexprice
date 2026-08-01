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

func TestStorageExportConfig_ResolvedAccessMode(t *testing.T) {
	tests := []struct {
		name string
		cfg  *StorageExportConfig
		want StorageAccessMode
	}{
		{
			name: "empty access mode resolves to static_key",
			cfg:  &StorageExportConfig{},
			want: StorageAccessModeStaticKey,
		},
		{
			name: "explicit static_key passes through",
			cfg:  &StorageExportConfig{AccessMode: StorageAccessModeStaticKey},
			want: StorageAccessModeStaticKey,
		},
		{
			name: "explicit assume_role passes through",
			cfg:  &StorageExportConfig{AccessMode: StorageAccessModeAssumeRole},
			want: StorageAccessModeAssumeRole,
		},
		{
			name: "nil config resolves to static_key",
			cfg:  nil,
			want: StorageAccessModeStaticKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.cfg.ResolvedAccessMode())
		})
	}
}

func TestStorageExportConfig_ValidateForProvider_AssumeRole(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *StorageExportConfig
		provider    SecretProvider
		wantErr     bool
		errContains string
	}{
		{
			name: "assume_role with role_arn and external_id passes for S3",
			cfg: &StorageExportConfig{
				Bucket:     "my-bucket",
				Region:     "us-west-2",
				AccessMode: StorageAccessModeAssumeRole,
				RoleARN:    "arn:aws:iam::123456789012:role/flexprice-export",
				ExternalID: "ext-tenant-abc",
			},
			provider: SecretProviderS3,
			wantErr:  false,
		},
		{
			name: "assume_role without role_arn fails",
			cfg: &StorageExportConfig{
				Bucket:     "my-bucket",
				Region:     "us-west-2",
				AccessMode: StorageAccessModeAssumeRole,
				ExternalID: "ext-tenant-abc",
			},
			provider:    SecretProviderS3,
			wantErr:     true,
			errContains: "role_arn",
		},
		{
			name: "assume_role without external_id fails",
			cfg: &StorageExportConfig{
				Bucket:     "my-bucket",
				Region:     "us-west-2",
				AccessMode: StorageAccessModeAssumeRole,
				RoleARN:    "arn:aws:iam::123456789012:role/flexprice-export",
			},
			provider:    SecretProviderS3,
			wantErr:     true,
			errContains: "external_id",
		},
		{
			name: "assume_role rejected for GCS",
			cfg: &StorageExportConfig{
				Bucket:     "my-bucket",
				AccessMode: StorageAccessModeAssumeRole,
				RoleARN:    "arn:aws:iam::123456789012:role/flexprice-export",
				ExternalID: "ext-tenant-abc",
			},
			provider:    SecretProviderGCS,
			wantErr:     true,
			errContains: "S3",
		},
		{
			name: "static_key unchanged (no role/external id needed)",
			cfg: &StorageExportConfig{
				Bucket:     "my-bucket",
				Region:     "us-west-2",
				AccessMode: StorageAccessModeStaticKey,
			},
			provider: SecretProviderS3,
			wantErr:  false,
		},
		{
			name: "reserved impersonation mode rejected",
			cfg: &StorageExportConfig{
				Bucket:     "my-bucket",
				AccessMode: StorageAccessModeImpersonation,
			},
			provider:    SecretProviderGCS,
			wantErr:     true,
			errContains: "not yet supported",
		},
		{
			name: "reserved direct_grant mode rejected",
			cfg: &StorageExportConfig{
				Bucket:     "my-bucket",
				AccessMode: StorageAccessModeDirectGrant,
			},
			provider:    SecretProviderGCS,
			wantErr:     true,
			errContains: "not yet supported",
		},
		{
			name: "reserved wif mode rejected",
			cfg: &StorageExportConfig{
				Bucket:     "my-bucket",
				AccessMode: StorageAccessModeWIF,
			},
			provider:    SecretProviderGCS,
			wantErr:     true,
			errContains: "not yet supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.ValidateForProvider(tt.provider)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
