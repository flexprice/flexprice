package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// ScheduledTaskInterval represents the interval for scheduled tasks
type ScheduledTaskInterval string

const (
	ScheduledTaskIntervalCustom ScheduledTaskInterval = "custom" // 10 minutes for testing
	ScheduledTaskIntervalHourly ScheduledTaskInterval = "hourly"
	ScheduledTaskIntervalDaily  ScheduledTaskInterval = "daily"
)

// Validate validates the scheduled task interval
func (s ScheduledTaskInterval) Validate() error {
	allowedIntervals := []ScheduledTaskInterval{
		ScheduledTaskIntervalCustom,
		ScheduledTaskIntervalHourly,
		ScheduledTaskIntervalDaily,
	}
	if s == "" {
		return ierr.NewError("interval is required").
			WithHint("Scheduled task interval must be specified").
			Mark(ierr.ErrValidation)
	}
	for _, interval := range allowedIntervals {
		if s == interval {
			return nil
		}
	}
	return ierr.NewError("invalid scheduled task interval").
		WithHint("Interval must be one of: custom, hourly, daily").
		Mark(ierr.ErrValidation)
}

// ScheduledTaskEntityType represents the entity type for scheduled tasks
type ScheduledTaskEntityType string

const (
	ScheduledTaskEntityTypeEvents       ScheduledTaskEntityType = "events"
	ScheduledTaskEntityTypeInvoice      ScheduledTaskEntityType = "invoice"
	ScheduledTaskEntityTypeCreditTopups ScheduledTaskEntityType = "credit_topups"
	ScheduledTaskEntityTypeCreditUsage  ScheduledTaskEntityType = "credit_usage"
)

// Validate validates the entity type
func (e ScheduledTaskEntityType) Validate() error {
	allowedTypes := []ScheduledTaskEntityType{
		ScheduledTaskEntityTypeEvents,
		ScheduledTaskEntityTypeInvoice,
		ScheduledTaskEntityTypeCreditTopups,
		ScheduledTaskEntityTypeCreditUsage,
	}
	if e == "" {
		return ierr.NewError("entity type is required").
			WithHint("Scheduled task entity type must be specified").
			Mark(ierr.ErrValidation)
	}
	for _, entityType := range allowedTypes {
		if e == entityType {
			return nil
		}
	}
	return ierr.NewError("invalid entity type").
		WithHint("Entity type must be one of: events, invoices, credit_topups").
		Mark(ierr.ErrValidation)
}

// S3CompressionType represents the compression type for S3 uploads
type S3CompressionType string

const (
	S3CompressionTypeNone S3CompressionType = "none"
	S3CompressionTypeGzip S3CompressionType = "gzip"
)

// Validate validates the S3 compression type
func (c S3CompressionType) Validate() error {
	allowedTypes := []S3CompressionType{
		S3CompressionTypeNone,
		S3CompressionTypeGzip,
	}
	if c == "" {
		return nil // Optional field
	}
	for _, compressionType := range allowedTypes {
		if c == compressionType {
			return nil
		}
	}
	return ierr.NewError("invalid S3 compression type").
		WithHint("Compression type must be one of: none, gzip").
		Mark(ierr.ErrValidation)
}

// S3EncryptionType represents the encryption type for S3 uploads
type S3EncryptionType string

const (
	S3EncryptionTypeAES256     S3EncryptionType = "AES256"
	S3EncryptionTypeAwsKms     S3EncryptionType = "aws:kms"
	S3EncryptionTypeAwsKmsDsse S3EncryptionType = "aws:kms:dsse"
)

// Validate validates the S3 encryption type
func (e S3EncryptionType) Validate() error {
	allowedTypes := []S3EncryptionType{
		S3EncryptionTypeAES256,
		S3EncryptionTypeAwsKms,
		S3EncryptionTypeAwsKmsDsse,
	}
	if e == "" {
		return nil // Optional field
	}
	for _, encryptionType := range allowedTypes {
		if e == encryptionType {
			return nil
		}
	}
	return ierr.NewError("invalid S3 encryption type").
		WithHint("Encryption type must be one of: AES256, aws:kms, aws:kms:dsse").
		Mark(ierr.ErrValidation)
}

// S3ExportConfig represents S3 export configuration (non-sensitive settings)
// This goes in the sync_config column
type S3ExportConfig struct {
	Bucket      string            `json:"bucket"`                // S3 bucket name
	Region      string            `json:"region"`                // AWS region (e.g., "us-west-2")
	KeyPrefix   string            `json:"key_prefix,omitempty"`  // Optional prefix for S3 keys (e.g., "flexprice-exports/")
	Compression S3CompressionType `json:"compression,omitempty"` // Compression type: "gzip", "none" (default: "none")
	Encryption  S3EncryptionType  `json:"encryption,omitempty"`  // Encryption type: "AES256", "aws:kms", "aws:kms:dsse" (default: "AES256")
}

// Validate validates the S3 export configuration
func (s *S3ExportConfig) Validate() error {
	if s == nil {
		return nil
	}

	if s.Bucket == "" {
		return ierr.NewError("bucket is required").
			WithHint("S3 bucket name is required").
			Mark(ierr.ErrValidation)
	}
	if s.Region == "" {
		return ierr.NewError("region is required").
			WithHint("AWS region is required").
			Mark(ierr.ErrValidation)
	}

	// Validate compression type if provided
	if err := s.Compression.Validate(); err != nil {
		return err
	}

	// Validate encryption type if provided
	if err := s.Encryption.Validate(); err != nil {
		return err
	}

	return nil
}

// S3JobConfig represents the configuration for an S3 export job
// This is stored in the job_config JSON field of scheduled_tasks table
type S3JobConfig struct {
	Bucket       string            `json:"bucket"`                   // S3 bucket name
	Region       string            `json:"region"`                   // AWS region (e.g., "us-west-2")
	KeyPrefix    string            `json:"key_prefix,omitempty"`     // Optional prefix for S3 keys (e.g., "flexprice-exports/")
	Compression  S3CompressionType `json:"compression,omitempty"`    // Compression type: "gzip", "none" (default: "none")
	Encryption   S3EncryptionType  `json:"encryption,omitempty"`     // Encryption type: "AES256", "aws:kms", "aws:kms:dsse" (default: "AES256")
	EndpointURL  string            `json:"endpoint_url,omitempty"`   // Custom S3 endpoint URL (e.g., "http://minio:9000" for MinIO)
	UsePathStyle bool              `json:"use_path_style,omitempty"` // Use path-style addressing instead of virtual-hosted-style (required for MinIO)
}

// Validate validates the S3 job configuration
func (s *S3JobConfig) Validate() error {
	if s == nil {
		return ierr.NewError("S3 job config is required").
			WithHint("S3 job configuration must be provided").
			Mark(ierr.ErrValidation)
	}

	if s.Bucket == "" {
		return ierr.NewError("bucket is required").
			WithHint("S3 bucket name is required").
			Mark(ierr.ErrValidation)
	}
	if s.Region == "" {
		return ierr.NewError("region is required").
			WithHint("AWS region is required").
			Mark(ierr.ErrValidation)
	}

	// Validate compression type if provided
	if err := s.Compression.Validate(); err != nil {
		return err
	}

	// Validate encryption type if provided
	if err := s.Encryption.Validate(); err != nil {
		return err
	}

	// Set defaults
	if s.Compression == "" {
		s.Compression = S3CompressionTypeNone
	}
	if s.Encryption == "" {
		s.Encryption = S3EncryptionTypeAES256
	}

	return nil
}

// SetDefaults sets default values for the S3 job configuration
func (s *S3JobConfig) SetDefaults() {
	if s.Compression == "" {
		s.Compression = S3CompressionTypeNone
	}
	if s.Encryption == "" {
		s.Encryption = S3EncryptionTypeAES256
	}
}

// CreateScheduledTaskInput represents the input for creating a scheduled task
type CreateScheduledTaskInput struct {
	ConnectionID string
	EntityType   ScheduledTaskEntityType
	Interval     ScheduledTaskInterval
	Enabled      bool
	JobConfig    *S3JobConfig
}

// UpdateScheduledTaskInput represents the input for updating a scheduled task
type UpdateScheduledTaskInput struct {
	Interval  *ScheduledTaskInterval
	Enabled   *bool
	JobConfig *S3JobConfig
}
