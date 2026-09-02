package config_test

import (
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlexpriceS3ExportsConfig_AllowsEmptyStaticCredsWhenFederationConfigured(t *testing.T) {
	cfg := config.FlexpriceS3ExportsConfig{
		Bucket:            "flexprice-exports",
		Region:            "ap-south-1",
		FederationRoleARN: "arn:aws:iam::123456789012:role/flexprice-gke-federation",
		FederationEnabled: true,
		// AWSAccessKeyID / AWSSecretAccessKey intentionally empty
	}

	v := validator.New()
	err := v.Struct(cfg)
	require.NoError(t, err, "struct-tag validation must not require static keys once they're omitempty")

	assert.NoError(t, cfg.Validate(), "custom Validate() must accept federation-only config")
}

func TestFlexpriceS3ExportsConfig_RejectsNoCredentialSourceAtAll(t *testing.T) {
	cfg := config.FlexpriceS3ExportsConfig{
		Bucket: "flexprice-exports",
		Region: "ap-south-1",
		// no static keys, no federation role ARN, no explicit credential_source —
		// must fail custom Validate() to preserve historic behavior.
	}

	assert.Error(t, cfg.Validate(), "must reject config with zero credential sources when ambient is not explicitly requested")
}

func TestResolvedCredentialSource(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.FlexpriceS3ExportsConfig
		want string
	}{
		{
			name: "explicit credential_source wins",
			cfg: config.FlexpriceS3ExportsConfig{
				CredentialSource:   config.CredentialSourceStatic,
				FederationEnabled:  true,
				FederationRoleARN:  "arn:aws:iam::123456789012:role/x",
				AWSAccessKeyID:     "",
				AWSSecretAccessKey: "",
			},
			want: config.CredentialSourceStatic,
		},
		{
			name: "federation enabled derives federation",
			cfg: config.FlexpriceS3ExportsConfig{
				FederationEnabled: true,
				FederationRoleARN: "arn:aws:iam::123456789012:role/x",
			},
			want: config.CredentialSourceFederation,
		},
		{
			name: "both static keys present derives static",
			cfg: config.FlexpriceS3ExportsConfig{
				AWSAccessKeyID:     "AKIAEXAMPLE",
				AWSSecretAccessKey: "secret",
			},
			want: config.CredentialSourceStatic,
		},
		{
			name: "nothing configured derives ambient",
			cfg:  config.FlexpriceS3ExportsConfig{},
			want: config.CredentialSourceAmbient,
		},
		{
			name: "explicit ambient overrides what would otherwise derive static",
			cfg: config.FlexpriceS3ExportsConfig{
				CredentialSource:   config.CredentialSourceAmbient,
				AWSAccessKeyID:     "AKIAEXAMPLE",
				AWSSecretAccessKey: "secret",
			},
			want: config.CredentialSourceAmbient,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.cfg.ResolvedCredentialSource())
		})
	}
}

func TestFlexpriceS3ExportsConfig_Validate_TableDriven(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.FlexpriceS3ExportsConfig
		wantErr bool
	}{
		{
			name: "ambient explicit with no credentials passes",
			cfg: config.FlexpriceS3ExportsConfig{
				Bucket:           "flexprice-exports",
				Region:           "ap-south-1",
				CredentialSource: config.CredentialSourceAmbient,
			},
			wantErr: false,
		},
		{
			name: "static with missing secret key fails",
			cfg: config.FlexpriceS3ExportsConfig{
				Bucket:         "flexprice-exports",
				Region:         "ap-south-1",
				AWSAccessKeyID: "AKIAEXAMPLE",
				// AWSSecretAccessKey intentionally empty
			},
			wantErr: true,
		},
		{
			name: "federation with no role ARN fails",
			cfg: config.FlexpriceS3ExportsConfig{
				Bucket:            "flexprice-exports",
				Region:            "ap-south-1",
				FederationEnabled: true,
			},
			wantErr: true,
		},
		{
			name: "static with both keys passes",
			cfg: config.FlexpriceS3ExportsConfig{
				Bucket:             "flexprice-exports",
				Region:             "ap-south-1",
				AWSAccessKeyID:     "AKIAEXAMPLE",
				AWSSecretAccessKey: "secret",
			},
			wantErr: false,
		},
		{
			name: "nothing configured and ambient not explicit fails",
			cfg: config.FlexpriceS3ExportsConfig{
				Bucket: "flexprice-exports",
				Region: "ap-south-1",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
