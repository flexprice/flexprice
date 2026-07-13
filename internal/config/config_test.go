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
		// no static keys, no federation role ARN — must fail custom Validate()
	}

	assert.Error(t, cfg.Validate(), "must reject config with zero credential sources")
}
