package types

import (
	"time"

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
	ScheduledTaskIntervalWeekly         ScheduledTaskInterval = "weekly"
	ScheduledTaskIntervalMonthly        ScheduledTaskInterval = "monthly"
	ScheduledTaskIntervalManual         ScheduledTaskInterval = "manual"
)

// Validate validates the scheduled task interval
func (s ScheduledTaskInterval) Validate() error {
	allowedIntervals := []ScheduledTaskInterval{
		ScheduledTaskIntervalEvery15Minutes,
		ScheduledTaskIntervalEvery30Minutes,
		ScheduledTaskIntervalCustom,
		ScheduledTaskIntervalHourly,
		ScheduledTaskIntervalDaily,
		ScheduledTaskIntervalWeekly,
		ScheduledTaskIntervalMonthly,
		ScheduledTaskIntervalManual,
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
		WithHint("Interval must be one of: 15MIN, 30MIN, custom, hourly, daily, weekly, monthly, manual").
		Mark(ierr.ErrValidation)
}

// ExportSchedule is an export's run time.
type ExportSchedule struct {
	Hour       int    `json:"hour"`
	Minute     int    `json:"minute"`
	DayOfWeek  *int   `json:"day_of_week,omitempty"`
	DayOfMonth *int   `json:"day_of_month,omitempty"`
	Timezone   string `json:"timezone"`
}

// Validate checks fields for interval.
func (s *ExportSchedule) Validate(interval ScheduledTaskInterval) error {
	if s.Hour < 0 || s.Hour > 23 {
		return ierr.NewError("invalid schedule hour").
			WithHint("Hour must be between 0 and 23").
			Mark(ierr.ErrValidation)
	}
	if s.Minute < 0 || s.Minute > 59 {
		return ierr.NewError("invalid schedule minute").
			WithHint("Minute must be between 0 and 59").
			Mark(ierr.ErrValidation)
	}
	if _, err := s.Location(); err != nil || s.Timezone == "Local" {
		return ierr.NewError("invalid schedule timezone").
			WithHint("Timezone must be a valid IANA name such as Asia/Kolkata").
			Mark(ierr.ErrValidation)
	}
	if interval == ScheduledTaskIntervalWeekly {
		if s.DayOfWeek == nil || *s.DayOfWeek < 0 || *s.DayOfWeek > 6 {
			return ierr.NewError("invalid schedule day_of_week").
				WithHint("Weekly schedules require day_of_week between 0 (Sunday) and 6").
				Mark(ierr.ErrValidation)
		}
	} else if s.DayOfWeek != nil {
		return ierr.NewError("unexpected schedule day_of_week").
			WithHint("day_of_week is only allowed for weekly schedules").
			Mark(ierr.ErrValidation)
	}
	if interval == ScheduledTaskIntervalMonthly {
		if s.DayOfMonth == nil || *s.DayOfMonth < 1 || *s.DayOfMonth > 28 {
			return ierr.NewError("invalid schedule day_of_month").
				WithHint("Monthly schedules require day_of_month between 1 and 28").
				Mark(ierr.ErrValidation)
		}
	} else if s.DayOfMonth != nil {
		return ierr.NewError("unexpected schedule day_of_month").
			WithHint("day_of_month is only allowed for monthly schedules").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// Location resolves the timezone.
func (s *ExportSchedule) Location() (*time.Location, error) {
	if s.Timezone == "" {
		return nil, ierr.NewError("timezone is required").
			WithHint("Schedule timezone must be specified").
			Mark(ierr.ErrValidation)
	}
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Timezone must be a valid IANA name such as Asia/Kolkata").
			Mark(ierr.ErrValidation)
	}
	return loc, nil
}

// ValidateExportSchedule checks schedule presence.
func ValidateExportSchedule(interval ScheduledTaskInterval, s *ExportSchedule) error {
	switch interval {
	case ScheduledTaskIntervalWeekly, ScheduledTaskIntervalMonthly:
		if s == nil {
			return ierr.NewError("schedule is required").
				WithHint("Weekly and monthly exports require a schedule").
				Mark(ierr.ErrValidation)
		}
	case ScheduledTaskIntervalManual:
		if s != nil {
			return ierr.NewError("schedule not allowed").
				WithHint("Manual exports do not take a schedule").
				Mark(ierr.ErrValidation)
		}
	case ScheduledTaskIntervalEvery15Minutes, ScheduledTaskIntervalEvery30Minutes, ScheduledTaskIntervalCustom:
		if s != nil {
			return ierr.NewError("schedule not allowed").
				WithHint("Only hourly, daily, weekly and monthly exports take a schedule").
				Mark(ierr.ErrValidation)
		}
	}
	if s == nil {
		return nil
	}
	return s.Validate(interval)
}

// ScheduledTaskEntityType represents the entity type for scheduled tasks
type ScheduledTaskEntityType string

const (
	ScheduledTaskEntityTypeEvents         ScheduledTaskEntityType = "events"
	ScheduledTaskEntityTypeInvoice        ScheduledTaskEntityType = "invoice"
	ScheduledTaskEntityTypeCreditTopups   ScheduledTaskEntityType = "credit_topups"
	ScheduledTaskEntityTypeWalletDebits   ScheduledTaskEntityType = "wallet_debits"
	ScheduledTaskEntityTypeWalletLedger   ScheduledTaskEntityType = "wallet_ledger"
	ScheduledTaskEntityTypeCreditUsage    ScheduledTaskEntityType = "credit_usage"
	ScheduledTaskEntityTypeUsageAnalytics ScheduledTaskEntityType = "usage_analytics"
	ScheduledTaskEntityTypeRevenueFacts   ScheduledTaskEntityType = "revenue_facts"
	ScheduledTaskEntityTypeReport         ScheduledTaskEntityType = "report"
	ScheduledTaskEntityTypeAnalyticsView  ScheduledTaskEntityType = "analytics_view"
	ScheduledTaskEntityTypeCustomers      ScheduledTaskEntityType = "customers"
)

// True for report-backed exports.
func (e ScheduledTaskEntityType) IsReportSource() bool {
	return e == ScheduledTaskEntityTypeReport || e == ScheduledTaskEntityTypeAnalyticsView
}

// True when a customer_filter may limit the export.
func (e ScheduledTaskEntityType) AllowsCustomerFilter() bool {
	switch e {
	case ScheduledTaskEntityTypeCustomers,
		ScheduledTaskEntityTypeWalletLedger,
		ScheduledTaskEntityTypeInvoice,
		ScheduledTaskEntityTypeCreditTopups,
		ScheduledTaskEntityTypeCreditUsage,
		ScheduledTaskEntityTypeUsageAnalytics,
		ScheduledTaskEntityTypeReport:
		return true
	}
	return false
}

// ExportOrigin is where an export was created.
type ExportOrigin string

const (
	ExportOriginConsole       ExportOrigin = "console"
	ExportOriginCustomersPage ExportOrigin = "customers_page"
)

// Validate checks the origin; empty is allowed.
func (o ExportOrigin) Validate() error {
	switch o {
	case "", ExportOriginConsole, ExportOriginCustomersPage:
		return nil
	}
	return ierr.NewError("invalid origin").
		WithHint("Origin must be one of: console, customers_page").
		Mark(ierr.ErrValidation)
}

// SanitizeExportCustomerFilter keeps only the list filters.
func SanitizeExportCustomerFilter(f *CustomerFilter) *CustomerFilter {
	if f == nil {
		return nil
	}
	out := &CustomerFilter{
		Filters:         f.Filters,
		MetadataFilter:  f.MetadataFilter,
		Email:           f.Email,
		BillingEntityID: f.BillingEntityID,
	}
	if len(out.Filters) == 0 {
		out.Filters = nil
	}
	if f.QueryFilter != nil && f.QueryFilter.Status != nil {
		out.QueryFilter = &QueryFilter{Status: f.QueryFilter.Status}
	}
	return out
}

// One report filter.
type ReportParamFilter struct {
	Field string   `json:"field"`
	Op    string   `json:"op" enums:"eq,in"`
	Value []string `json:"value,omitempty"`
}

// Report export parameters.
type ReportParams struct {
	Shape      string              `json:"shape" validate:"required" enums:"timeseries,breakdown,scalar"`
	Dimensions []string            `json:"dimensions,omitempty"`
	Grain      string              `json:"grain,omitempty" enums:"day,week,month"`
	Currency   string              `json:"currency,omitempty"`
	Filters    []ReportParamFilter `json:"filters,omitempty"`
}

// Report or view source.
type ExportSourceConfig struct {
	ReportKey    string        `json:"report_key,omitempty"`
	ReportParams *ReportParams `json:"report_params,omitempty"`
	ViewID       string        `json:"view_id,omitempty"`
	// Limits the export to matching customers.
	CustomerFilter *CustomerFilter `json:"customer_filter,omitempty"`
}

// GetCustomerFilter is nil-safe.
func (c *ExportSourceConfig) GetCustomerFilter() *CustomerFilter {
	if c == nil {
		return nil
	}
	return c.CustomerFilter
}

// Checks config fits entity.
func (c *ExportSourceConfig) Validate(entity ScheduledTaskEntityType) error {
	if entity == ScheduledTaskEntityTypeCustomers {
		if c == nil || c.CustomerFilter == nil || c.ReportKey != "" || c.ReportParams != nil || c.ViewID != "" {
			return ierr.NewError("invalid source_config").
				WithHint("customers exports require only customer_filter").
				Mark(ierr.ErrValidation)
		}
		return nil
	}
	if c != nil && c.CustomerFilter != nil && !entity.AllowsCustomerFilter() {
		return ierr.NewError("customer_filter not supported for this export").
			WithHint("This data can't be limited to customers yet.").
			Mark(ierr.ErrValidation)
	}
	if !entity.IsReportSource() {
		if c != nil && (c.CustomerFilter == nil || c.ReportKey != "" || c.ReportParams != nil || c.ViewID != "") {
			return ierr.NewError("source_config not allowed").
				WithHint("source_config is only for report and analytics_view exports").
				Mark(ierr.ErrValidation)
		}
		return nil
	}
	if c == nil {
		return ierr.NewError("source_config is required").
			WithHint("Provide source_config for report and analytics_view exports").
			Mark(ierr.ErrValidation)
	}
	if entity == ScheduledTaskEntityTypeAnalyticsView {
		if c.ViewID == "" || c.ReportKey != "" || c.ReportParams != nil {
			return ierr.NewError("invalid source_config").
				WithHint("analytics_view exports require only view_id").
				Mark(ierr.ErrValidation)
		}
		return nil
	}
	if c.ReportKey == "" || c.ViewID != "" {
		return ierr.NewError("invalid source_config").
			WithHint("report exports require report_key and no view_id").
			Mark(ierr.ErrValidation)
	}
	if c.ReportParams == nil || c.ReportParams.Shape == "" {
		return ierr.NewError("report_params.shape is required").
			WithHint("Provide report_params with a shape").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// Validate validates the entity type
func (e ScheduledTaskEntityType) Validate() error {
	allowedTypes := []ScheduledTaskEntityType{
		ScheduledTaskEntityTypeEvents,
		ScheduledTaskEntityTypeInvoice,
		ScheduledTaskEntityTypeCreditTopups,
		ScheduledTaskEntityTypeWalletDebits,
		ScheduledTaskEntityTypeWalletLedger,
		ScheduledTaskEntityTypeCreditUsage,
		ScheduledTaskEntityTypeUsageAnalytics,
		ScheduledTaskEntityTypeRevenueFacts,
		ScheduledTaskEntityTypeReport,
		ScheduledTaskEntityTypeAnalyticsView,
		ScheduledTaskEntityTypeCustomers,
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
		WithHint("Entity type must be one of: events, invoice, credit_topups, wallet_debits, wallet_ledger, credit_usage, usage_analytics, revenue_facts, report, analytics_view, customers").
		Mark(ierr.ErrValidation)
}

// MaxExportEmailRecipients caps recipients per export.
const MaxExportEmailRecipients = 10

// ExportEmailConfig lists export email recipients.
type ExportEmailConfig struct {
	Recipients []string `json:"recipients"`
}

// NormalizeRecipients trims, lowercases and dedupes, keeping order.
func NormalizeRecipients(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		addr := NormalizeEmail(raw)
		if addr == "" {
			continue
		}
		if _, dup := seen[addr]; dup {
			continue
		}
		seen[addr] = struct{}{}
		out = append(out, addr)
	}
	return out
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

type StorageExportConfig struct {
	Bucket             string            `json:"bucket"`                         // Storage bucket name
	Region             string            `json:"region"`                         // Cloud region (e.g., "us-west-2"); unused for GCS
	KeyPrefix          string            `json:"key_prefix,omitempty"`           // Optional prefix for object keys (e.g., "flexprice-exports/")
	Compression        S3CompressionType `json:"compression,omitempty"`          // Compression type: "gzip", "none" (default: "none")
	Encryption         S3EncryptionType  `json:"encryption,omitempty"`           // Encryption type: "AES256", "aws:kms", "aws:kms:dsse" (default: "AES256")
	IsFlexpriceManaged bool              `json:"is_flexprice_managed,omitempty"` // If true, use Flexprice-managed storage credentials instead of user-provided
}

// Ent calls this without provider context; skips Region.
func (s *StorageExportConfig) Validate() error {
	if s == nil {
		return nil
	}

	if s.IsFlexpriceManaged {
		return nil
	}

	if s.Bucket == "" {
		return ierr.NewError("bucket is required").
			WithHint("S3 bucket name is required").
			Mark(ierr.ErrValidation)
	}

	if err := s.Compression.Validate(); err != nil {
		return err
	}

	if err := s.Encryption.Validate(); err != nil {
		return err
	}

	return nil
}

// Enforces S3-only Region rule.
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
	// Empty means S3 for back-compat.
	Provider SecretProvider `json:"provider,omitempty"`
}

// Ent calls this without provider context; skips Region.
func (s *S3JobConfig) Validate() error {
	if s == nil {
		return ierr.NewError("S3 job config is required").
			WithHint("S3 job configuration must be provided").
			Mark(ierr.ErrValidation)
	}

	if s.Bucket == "" {
		return ierr.NewError("bucket is required").
			WithHint("Storage bucket name is required").
			Mark(ierr.ErrValidation)
	}

	if s.Region == "" && s.Provider != SecretProviderGCS {
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

// Export delivery types and statuses.
const (
	ExportDeliveryTypeFlexprice = "flexprice"
	ExportDeliveryTypeS3        = "s3"
	ExportDeliveryTypeGCS       = "gcs"
	ExportDeliveryTypeEmail     = "email"

	ExportDeliveryStatusDelivered   = "delivered"
	ExportDeliveryStatusFailed      = "failed"
	ExportDeliveryStatusSkipped     = "skipped"
	ExportDeliveryStatusRateLimited = "rate_limited"
)

// ExportDelivery is one destination result.
type ExportDelivery struct {
	Type   string    `json:"type"`
	Target string    `json:"target"`
	Status string    `json:"status"`
	Detail string    `json:"detail,omitempty"`
	At     time.Time `json:"at"`
}
