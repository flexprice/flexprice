package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/validator"
)

// ScheduledTaskInterval represents the interval for scheduled tasks
type ScheduledTaskInterval string

const (
	ScheduledTaskIntervalEvery15Minutes ScheduledTaskInterval = "15MIN"
	ScheduledTaskIntervalEvery30Minutes ScheduledTaskInterval = "30MIN"
	ScheduledTaskIntervalCustom         ScheduledTaskInterval = "custom" // 10 minutes for testing
	ScheduledTaskIntervalHourly         ScheduledTaskInterval = "hourly"
	ScheduledTaskIntervalDaily          ScheduledTaskInterval = "daily"
)

// Validate validates the scheduled task interval
func (s ScheduledTaskInterval) Validate() error {
	allowedIntervals := []ScheduledTaskInterval{
		ScheduledTaskIntervalEvery15Minutes,
		ScheduledTaskIntervalEvery30Minutes,
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
		WithHint("Interval must be one of: 15MIN, 30MIN, custom, hourly, daily").
		Mark(ierr.ErrValidation)
}

// ScheduledTaskEntityType represents the entity type for scheduled tasks
type ScheduledTaskEntityType string

const (
	ScheduledTaskEntityTypeEvents         ScheduledTaskEntityType = "events"
	ScheduledTaskEntityTypeInvoice        ScheduledTaskEntityType = "invoice"
	ScheduledTaskEntityTypeCreditTopups   ScheduledTaskEntityType = "credit_topups"
	ScheduledTaskEntityTypeCreditUsage    ScheduledTaskEntityType = "credit_usage"
	ScheduledTaskEntityTypeUsageAnalytics ScheduledTaskEntityType = "usage_analytics"
)

// Validate validates the entity type
func (e ScheduledTaskEntityType) Validate() error {
	allowedTypes := []ScheduledTaskEntityType{
		ScheduledTaskEntityTypeEvents,
		ScheduledTaskEntityTypeInvoice,
		ScheduledTaskEntityTypeCreditTopups,
		ScheduledTaskEntityTypeCreditUsage,
		ScheduledTaskEntityTypeUsageAnalytics,
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
		WithHint("Entity type must be one of: events, invoice, credit_topups, credit_usage, usage_analytics").
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

// StorageAccessMode selects how Flexprice authenticates to a customer BYOB bucket.
// Empty == StorageAccessModeStaticKey (back-compat for pre-existing rows); always
// read via ResolvedAccessMode().
type StorageAccessMode string

const (
	// StaticKey: tenant supplies static credentials, stored encrypted.
	StorageAccessModeStaticKey StorageAccessMode = "static_key"
	// AssumeRole (S3-only): Flexprice calls sts:AssumeRole with RoleARN+ExternalID.
	StorageAccessModeAssumeRole StorageAccessMode = "assume_role"
	// Reserved for future GCP keyless modes; NOT implemented (ValidateForProvider rejects).
	StorageAccessModeImpersonation StorageAccessMode = "impersonation"
	StorageAccessModeDirectGrant   StorageAccessMode = "direct_grant"
	StorageAccessModeWIF           StorageAccessMode = "wif"
)

// StorageExportConfig is non-sensitive cloud storage export config (sync_config
// column), cloud-agnostic across S3 and GCS.
type StorageExportConfig struct {
	Bucket             string            `json:"bucket"`                         // Storage bucket name
	Region             string            `json:"region"`                         // Cloud region (e.g., "us-west-2"); unused for GCS
	KeyPrefix          string            `json:"key_prefix,omitempty"`           // Optional prefix for object keys (e.g., "flexprice-exports/")
	Compression        S3CompressionType `json:"compression,omitempty"`          // Compression type: "gzip", "none" (default: "none")
	Encryption         S3EncryptionType  `json:"encryption,omitempty"`           // Encryption type: "AES256", "aws:kms", "aws:kms:dsse" (default: "AES256")
	IsFlexpriceManaged bool              `json:"is_flexprice_managed,omitempty"` // If true, use Flexprice-managed storage credentials instead of user-provided
	// Non-secret identifiers, so plaintext in sync_config. Read AccessMode via
	// ResolvedAccessMode(). RoleARN/ExternalID apply to assume_role; ExternalID is
	// tenant-supplied (derived from tenant_id), never generated by Flexprice.
	AccessMode StorageAccessMode `json:"access_mode,omitempty"`
	RoleARN    string            `json:"role_arn,omitempty"`
	ExternalID string            `json:"external_id,omitempty"`
}

// ResolvedAccessMode treats empty (pre-existing rows) as static_key.
func (s *StorageExportConfig) ResolvedAccessMode() StorageAccessMode {
	if s == nil || s.AccessMode == "" {
		return StorageAccessModeStaticKey
	}
	return s.AccessMode
}

// Validate checks provider-agnostic invariants only. Ent's codegen calls this
// no-arg Validate() on sync_config at Create/Update with no provider context, so
// it can't enforce the S3-only Region rule — callers with provider context use
// ValidateForProvider.
func (s *StorageExportConfig) Validate() error {
	if s == nil {
		return nil
	}

	if s.IsFlexpriceManaged {
		return nil // No validation needed for Flexprice-managed connections
	}

	if s.Bucket == "" {
		return ierr.NewError("bucket is required").
			WithHint("S3 bucket name is required").
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

// ValidateForProvider adds the provider-specific rule that plain Validate() can't
// (Region required for S3, not GCS). Use this wherever provider context exists.
func (s *StorageExportConfig) ValidateForProvider(providerType SecretProvider) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if s == nil || s.IsFlexpriceManaged {
		return nil
	}

	if providerType != SecretProviderGCS && s.Region == "" {
		return ierr.NewError("region is required").
			WithHint("AWS region is required").
			Mark(ierr.ErrValidation)
	}

	switch s.ResolvedAccessMode() {
	case StorageAccessModeStaticKey:
		// Credentials live in EncryptedSecretData, not here.
	case StorageAccessModeAssumeRole:
		// Rejected at creation: disabled pending a per-environment IAM principal
		// (see Factory.buildS3Storage).
		return ierr.NewError("assume_role access mode is not enabled").
			WithHint("Cross-account AssumeRole for customer buckets is implemented but disabled pending a dedicated per-environment Flexprice IAM principal. Use access_mode 'static_key' with customer-supplied credentials for now.").
			Mark(ierr.ErrValidation)
	case StorageAccessModeImpersonation, StorageAccessModeDirectGrant, StorageAccessModeWIF:
		return ierr.NewError("access mode is not yet supported").
			WithHintf("access_mode %q is reserved for future GCP BYOB support and is not implemented yet", s.AccessMode).
			Mark(ierr.ErrValidation)
	default:
		return ierr.NewError("invalid access_mode").
			WithHintf("access_mode %q is not recognized", s.AccessMode).
			Mark(ierr.ErrValidation)
	}

	return nil
}

// S3JobConfig represents the configuration for an S3 export job
// This is stored in the job_config JSON field of scheduled_tasks table
type S3JobConfig struct {
	Bucket               string               `json:"bucket"`                           // S3 bucket name
	Region               string               `json:"region"`                           // AWS region (e.g., "us-west-2")
	KeyPrefix            string               `json:"key_prefix,omitempty"`             // Optional prefix for S3 keys (e.g., "flexprice-exports/")
	Compression          S3CompressionType    `json:"compression,omitempty"`            // Compression type: "gzip", "none" (default: "none")
	Encryption           S3EncryptionType     `json:"encryption,omitempty"`             // Encryption type: "AES256", "aws:kms", "aws:kms:dsse" (default: "AES256")
	EndpointURL          string               `json:"endpoint_url,omitempty"`           // Custom S3-compatible endpoint URL; must be https on a publicly routable host
	UsePathStyle         bool                 `json:"use_path_style,omitempty"`         // Use path-style addressing (required for MinIO)
	ExportMetadataFields ExportMetadataFields `json:"export_metadata_fields,omitempty"` // Optional user-selected metadata columns
	// Provider records which object store this job targets. Empty means S3, so
	// existing rows written before GCS support keep their meaning. It exists
	// because Validate() is called by Ent without provider context and must not
	// enforce S3-only rules (notably Region) on a GCS job.
	Provider SecretProvider `json:"provider,omitempty"`
	// AccessMode/RoleARN/ExternalID mirror the same fields on
	// StorageExportConfig. Credentials are always resolved from the CONNECTION
	// (Factory.buildS3Storage reads conn.GetSyncConfig().Storage), never from
	// this struct — these exist so a job config round-trips what the caller sent
	// instead of silently discarding it, and so the values are visible when
	// inspecting a scheduled task. Empty AccessMode means static_key, keeping
	// rows written before assume-role support unchanged.
	AccessMode StorageAccessMode `json:"access_mode,omitempty"`
	RoleARN    string            `json:"role_arn,omitempty"`
	ExternalID string            `json:"external_id,omitempty"`
}

// ResolvedAccessMode returns the effective access mode, treating empty as
// static_key so pre-existing rows keep their behavior.
func (s *S3JobConfig) ResolvedAccessMode() StorageAccessMode {
	if s == nil || s.AccessMode == "" {
		return StorageAccessModeStaticKey
	}
	return s.AccessMode
}

// isGCS reports whether this job targets Google Cloud Storage.
func (s *S3JobConfig) isGCS() bool {
	return s.Provider == SecretProviderGCS
}

// Validate validates the S3 job configuration
// This should only be called AFTER determining if the connection is Flexprice-managed
// For Flexprice-managed: bucket/region/key_prefix should already be populated by service layer
// For custom S3: user must provide all required fields
func (s *S3JobConfig) Validate() error {
	if s == nil {
		return ierr.NewError("S3 job config is required").
			WithHint("S3 job configuration must be provided").
			Mark(ierr.ErrValidation)
	}

	// Bucket is required for every provider (should be populated by now).
	if s.Bucket == "" {
		return ierr.NewError("bucket is required").
			WithHint("Storage bucket name is required").
			Mark(ierr.ErrValidation)
	}

	// Region is an S3-only concept — a GCS bucket's location is fixed at creation
	// and is never carried in the job config. This method is also invoked
	// implicitly by Ent's generated code (ent/schema/scheduledtask.go declares
	// job_config as field.JSON(&types.S3JobConfig{}), and Ent auto-calls a no-arg
	// Validate() on JSON field types at Create()/Update() time), which has no
	// provider context — so requiring a region unconditionally here would reject
	// every Flexprice-managed GCS scheduled task before it could be persisted,
	// regardless of what the service layer decided. Callers that do have provider
	// context should use ValidateForProvider.
	if s.Region == "" && !s.isGCS() {
		return ierr.NewError("region is required").
			WithHint("AWS region is required").
			Mark(ierr.ErrValidation)
	}

	if err := s.validateEndpointURL(); err != nil {
		return err
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

// ValidateForFlexpriceManaged validates only compression and encryption
// Used before populating bucket/region from config
// ValidateForFlexpriceManaged validates only compression and encryption for Flexprice-managed connections
func (s *S3JobConfig) ValidateForFlexpriceManaged() error {
	if s == nil {
		return ierr.NewError("S3 job config is required").
			WithHint("S3 job configuration must be provided").
			Mark(ierr.ErrValidation)
	}

	if err := s.validateEndpointURL(); err != nil {
		return err
	}

	// Only validate compression and encryption (bucket/region will be populated from config)
	if err := s.Compression.Validate(); err != nil {
		return err
	}

	if err := s.Encryption.Validate(); err != nil {
		return err
	}

	return nil
}

// validateEndpointURL rejects a custom S3 endpoint that is not a public https
// host. The endpoint receives SigV4-signed requests carrying our credentials,
// so a tenant-supplied value naming an internal address (the cloud metadata
// service, or any host inside the VPC) must not be accepted. Applied on both
// validation paths because a Flexprice-managed connection can still carry a
// tenant-supplied endpoint.
//
// This intentionally precludes S3-compatible endpoints on private addresses,
// such as a self-hosted MinIO reachable only inside the network. There is no
// way to distinguish a tenant's own MinIO from an internal address chosen to
// make us sign a request against it, because both are simply private hosts
// supplied through the API. Supporting that case needs an operator-controlled
// allowlist in configuration rather than a per-tenant field.
func (s *S3JobConfig) validateEndpointURL() error {
	if s.EndpointURL == "" {
		return nil
	}

	if err := validator.ValidateOutboundURL(s.EndpointURL); err != nil {
		return ierr.WithError(err).
			WithHint("S3 endpoint_url must be an https URL pointing to a publicly routable host").
			WithReportableDetails(map[string]any{
				"endpoint_url": s.EndpointURL,
			}).
			Mark(ierr.ErrValidation)
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

// ExportMetadataEntityType identifies which entity's metadata a field is read from.
type ExportMetadataEntityType string

const (
	ExportMetadataEntityTypeCustomer ExportMetadataEntityType = "customer"
	ExportMetadataEntityTypeWallet   ExportMetadataEntityType = "wallet"
)

// allowedMetadataEntityTypes maps each export entity type to the metadata entity types it supports.
var allowedMetadataEntityTypes = map[ScheduledTaskEntityType][]ExportMetadataEntityType{
	ScheduledTaskEntityTypeCreditUsage:    {ExportMetadataEntityTypeCustomer, ExportMetadataEntityTypeWallet},
	ScheduledTaskEntityTypeUsageAnalytics: {ExportMetadataEntityTypeCustomer},
}

func (e ExportMetadataEntityType) Validate() error {
	allowedTypes := []ExportMetadataEntityType{
		ExportMetadataEntityTypeCustomer,
		ExportMetadataEntityTypeWallet,
	}
	if e == "" {
		return nil // Optional field
	}
	for _, entityType := range allowedTypes {
		if e == entityType {
			return nil
		}
	}
	return ierr.NewError("invalid export metadata field entity_type").
		WithHint("invalid export metadata field entity_type").
		WithReportableDetails(map[string]interface{}{
			"entity_type":   e,
			"allowed_types": allowedTypes,
		}).
		Mark(ierr.ErrValidation)
}

// ExportMetadataField describes one user-selected metadata field to append to an export CSV.
type ExportMetadataField struct {
	EntityType ExportMetadataEntityType `json:"entity_type" validate:"required"` // which entity's metadata to read from
	FieldKey   string                   `json:"field_key" validate:"required"`   // metadata key to look up
	ColumnName string                   `json:"column_name,omitempty"`           // CSV column header to be shown in the exported file
}

// ExportMetadataFields is a named slice type so validation methods can be attached.
type ExportMetadataFields []ExportMetadataField

// ValidateAndDefault validates each field against the allowed metadata entity types for the given
// export entity type, then normalises defaults. The entity-type check runs first so the error
// message is meaningful before any defaulting happens.
func (fields ExportMetadataFields) ValidateAndDefault(exportEntity ScheduledTaskEntityType) error {
	if len(fields) == 0 {
		return nil
	}

	allowed, ok := allowedMetadataEntityTypes[exportEntity]
	if !ok {
		return ierr.NewError("export_metadata_fields are not supported for this export type").
			Mark(ierr.ErrValidation)
	}

	for i := range fields {
		f := &fields[i]
		if f.EntityType == "" {
			return ierr.NewError("export metadata field entity_type is required").
				WithHintf("export_metadata_fields[%d] is missing entity_type", i).
				Mark(ierr.ErrValidation)
		}
		supported := false
		for _, a := range allowed {
			if f.EntityType == a {
				supported = true
				break
			}
		}
		if !supported {
			return ierr.NewError("unsupported metadata entity_type for this export").
				WithHintf("unsupported metadata entity_type for this export").
				WithReportableDetails(map[string]interface{}{
					"index":         i,
					"entity_type":   f.EntityType,
					"allowed_types": allowed,
					"export_entity": exportEntity,
				}).
				Mark(ierr.ErrValidation)
		}
		if f.FieldKey == "" {
			return ierr.NewError("export metadata field field_key is required").
				WithHintf("export_metadata_fields[%d] is missing field_key", i).
				Mark(ierr.ErrValidation)
		}
		if f.ColumnName == "" {
			f.ColumnName = f.FieldKey
		}
	}
	return nil
}

// GetExportMetadataFields returns the export metadata fields slice, safe to call on nil config.
func (s *S3JobConfig) GetExportMetadataFields() ExportMetadataFields {
	if s == nil {
		return nil
	}
	return s.ExportMetadataFields
}
