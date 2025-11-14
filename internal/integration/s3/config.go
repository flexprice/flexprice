package s3

import (
	"github.com/flexprice/flexprice/internal/types"
)

// Config represents the configuration for S3 integration
type Config struct {
	// From sync_config.s3
	Bucket      string
	Region      string
	KeyPrefix   string
	Compression types.S3CompressionType
	Encryption  types.S3EncryptionType

	// From encrypted_secret_data
	AWSAccessKeyID     string
	AWSSecretAccessKey string
}

// NewConfigFromConnection creates a Config from S3ConnectionMetadata (secrets) and S3ExportConfig (settings)
func NewConfigFromConnection(secretData *types.S3ConnectionMetadata, exportConfig *types.S3ExportConfig) *Config {
	if secretData == nil || exportConfig == nil {
		return nil
	}

	config := &Config{
		// From sync_config
		Bucket:      exportConfig.Bucket,
		Region:      exportConfig.Region,
		KeyPrefix:   exportConfig.KeyPrefix,
		Compression: exportConfig.Compression,
		Encryption:  exportConfig.Encryption,

		// From encrypted_secret_data
		AWSAccessKeyID:     secretData.AWSAccessKeyID,
		AWSSecretAccessKey: secretData.AWSSecretAccessKey,
	}

	// Set defaults
	if config.Compression == "" {
		config.Compression = types.S3CompressionTypeNone
	}
	if config.Encryption == "" {
		config.Encryption = types.S3EncryptionTypeAES256
	}

	return config
}
