package types

import (
	"encoding/json"
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

func TestS3JobConfig_ResolvedAccessMode(t *testing.T) {
	tests := []struct {
		name string
		cfg  *S3JobConfig
		want StorageAccessMode
	}{
		{"empty resolves to static_key", &S3JobConfig{}, StorageAccessModeStaticKey},
		{"explicit static_key passes through", &S3JobConfig{AccessMode: StorageAccessModeStaticKey}, StorageAccessModeStaticKey},
		{"explicit assume_role passes through", &S3JobConfig{AccessMode: StorageAccessModeAssumeRole}, StorageAccessModeAssumeRole},
		{"nil resolves to static_key", nil, StorageAccessModeStaticKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.cfg.ResolvedAccessMode())
		})
	}
}

// S3JobConfig (scheduled_tasks.job_config) and StorageExportConfig
// (connections.sync_config) describe the same storage destination in two
// places. When assume-role support was added to StorageExportConfig only,
// S3JobConfig silently dropped access_mode/role_arn/external_id: the API
// accepted them on a scheduled task and discarded them, with no error. This
// asserts the round-trip so the two shapes cannot drift apart again unnoticed.
func TestS3JobConfig_AccessModeFieldsRoundTrip(t *testing.T) {
	in := &S3JobConfig{
		Bucket:     "customer-bucket",
		Region:     "ap-south-1",
		AccessMode: StorageAccessModeAssumeRole,
		RoleARN:    "arn:aws:iam::123456789012:role/customer-role",
		ExternalID: "ext-id-abc123",
	}

	raw, err := json.Marshal(in)
	assert.NoError(t, err)

	var out S3JobConfig
	assert.NoError(t, json.Unmarshal(raw, &out))

	assert.Equal(t, StorageAccessModeAssumeRole, out.AccessMode, "access_mode must survive the round-trip")
	assert.Equal(t, in.RoleARN, out.RoleARN, "role_arn must survive the round-trip")
	assert.Equal(t, in.ExternalID, out.ExternalID, "external_id must survive the round-trip")
}

func TestStorageExportConfig_ValidateForProvider_AssumeRole(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *StorageExportConfig
		provider    SecretProvider
		wantErr     bool
		errContains string
	}{
		// assume_role is implemented but DISABLED pending a dedicated
		// per-environment Flexprice IAM principal — see the branch comment in
		// Factory.buildS3Storage. It must be rejected at creation regardless of
		// how well-formed the request is, so no customer is handed a
		// trust-policy contract we intend to change.
		{
			name: "assume_role rejected even when fully specified for S3",
			cfg: &StorageExportConfig{
				Bucket:     "my-bucket",
				Region:     "us-west-2",
				AccessMode: StorageAccessModeAssumeRole,
				RoleARN:    "arn:aws:iam::123456789012:role/flexprice-export",
				ExternalID: "ext-tenant-abc",
			},
			provider:    SecretProviderS3,
			wantErr:     true,
			errContains: "not enabled",
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
			errContains: "not enabled",
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
