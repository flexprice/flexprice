package types

import (
	"fmt"
	"strings"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// SyncConfig defines which entities should be synced between FlexPrice and external providers
type SyncConfig struct {
	// Integration sync (Stripe, Razorpay, QuickBooks, etc.)
	Plan         *EntitySyncConfig `json:"plan,omitempty"`
	Subscription *EntitySyncConfig `json:"subscription,omitempty"`
	Invoice      *EntitySyncConfig `json:"invoice,omitempty"`
	Customer     *EntitySyncConfig `json:"customer,omitempty"`
	Payment      *EntitySyncConfig `json:"payment,omitempty"` // Payment sync (QuickBooks bidirectional)
	Price        *EntitySyncConfig `json:"price,omitempty"`   // Price sync (Stripe only) — outbound only, see Validate()
	// CRM sync (HubSpot, Salesforce, etc.)
	Deal  *EntitySyncConfig `json:"deal,omitempty"`
	Quote *EntitySyncConfig `json:"quote,omitempty"`
	// Tag stays "s3" for back-compat.
	Storage *StorageExportConfig `json:"s3,omitempty"`
	// AWSMarketplace connection metadata
	AWSMarketplace *AWSMarketplaceSyncConfig `json:"aws_marketplace,omitempty"`
	// InvoiceSyncSettings controls line-item transformation during outbound invoice sync
	InvoiceSyncSettings *InvoiceSyncSettings `json:"invoice_sync_settings,omitempty"`
}

// AWSMarketplaceSyncConfig holds AWS Marketplace connection config that isn't a secret. Region
// selects the AWS Marketplace Metering Service regional endpoint BatchMeterUsage targets at report
// time (internal/temporal/activities/marketplace/report_activities.go) — it must match the region
// AWS enabled SaaS metering for this product in (almost always us-east-1, the default AWS assigns
// unless the seller specifically requested another region), so it's tenant-specific and can't be
// hardcoded. Lives here, not in EncryptedSecretData, for the same reason S3's bucket/region does:
// it's destination/endpoint config for an outbound call, not authentication material.
type AWSMarketplaceSyncConfig struct {
	Region string `json:"region"`
}

// Validate validates the AWS Marketplace sync config
func (a *AWSMarketplaceSyncConfig) Validate() error {
	if a == nil {
		return nil
	}
	if a.Region == "" {
		return ierr.NewError("region is required").
			WithHint("AWS Marketplace region is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// EntitySyncConfig defines sync direction for an entity
type EntitySyncConfig struct {
	Inbound  bool `json:"inbound"`  // Inbound from external provider to FlexPrice
	Outbound bool `json:"outbound"` // Outbound from FlexPrice to external provider
}

// InvoiceSyncSettings controls how invoice line items are transformed during outbound sync.
type InvoiceSyncSettings struct {
	// NormalizeFixedTo re-expresses fixed-charge line items in a smaller billing period.
	// For example, a quarterly fixed charge of $300 with NormalizeFixedTo=MONTHLY becomes
	// qty=3, rate=$100. Empty string means no normalization (keep original).
	NormalizeFixedTo BillingPeriod `json:"normalize_fixed_to,omitempty"`

	// ServicePeriodCustomFields names the Zoho custom fields that receive the
	// invoice's service start and end dates.
	ServicePeriodCustomFields *ServicePeriodCustomFields `json:"service_period_custom_fields,omitempty"`

	// MetadataCustomFields copies metadata values onto Zoho invoice custom fields
	// verbatim.
	MetadataCustomFields []MetadataCustomField `json:"metadata_custom_fields,omitempty"`

	// SubmitForApproval submits the synced invoice into the merchant's Zoho Books approval
	// flow before recording payment. Zoho rejects payments on draft invoices, and merchants
	// configure a Zoho auto-approval rule for FlexPrice-sent invoices, so we submit, wait for
	// that rule to fire, then pay.
	SubmitForApproval bool `json:"submit_for_approval,omitempty"`
}

// IsSubmitForApprovalEnabled reports whether synced invoices should be submitted into the
// merchant's Zoho Books approval flow before payment is recorded against them.
func (s *InvoiceSyncSettings) IsSubmitForApprovalEnabled() bool {
	return s != nil && s.SubmitForApproval
}

type MetadataCustomFieldSource string

const (
	// MaxMetadataCustomFields bounds the mapping list; a Zoho org supports far fewer
	// custom fields per module than this.
	MaxMetadataCustomFields = 50
	maxCustomFieldRefLen    = 255
)

const (
	MetadataCustomFieldSourceCustomer MetadataCustomFieldSource = "customer"
	MetadataCustomFieldSourceInvoice  MetadataCustomFieldSource = "invoice"
)

type MetadataCustomField struct {
	Source      MetadataCustomFieldSource `json:"source"`
	MetadataKey string                    `json:"metadata_key"`
	Field       string                    `json:"field"`
}

func (m MetadataCustomField) Validate() error {
	switch m.Source {
	case MetadataCustomFieldSourceCustomer, MetadataCustomFieldSourceInvoice:
	default:
		return ierr.NewError("invalid metadata custom field source").
			WithHint(fmt.Sprintf("source must be %q or %q",
				MetadataCustomFieldSourceCustomer, MetadataCustomFieldSourceInvoice)).
			Mark(ierr.ErrValidation)
	}

	key := strings.TrimSpace(m.MetadataKey)
	if key == "" {
		return ierr.NewError("metadata custom field key is required").
			WithHint("Provide the metadata key to copy from").
			Mark(ierr.ErrValidation)
	}
	if len(key) > maxCustomFieldRefLen {
		return ierr.NewError("metadata custom field key is too long").
			WithHint(fmt.Sprintf("metadata_key must be at most %d characters", maxCustomFieldRefLen)).
			Mark(ierr.ErrValidation)
	}

	field := strings.TrimSpace(m.Field)
	if field == "" {
		return ierr.NewError("metadata custom field target is required").
			WithHint("Provide the Zoho custom field API name or ID to write to").
			Mark(ierr.ErrValidation)
	}
	if len(field) > maxCustomFieldRefLen {
		return ierr.NewError("metadata custom field target is too long").
			WithHint(fmt.Sprintf("field must be at most %d characters", maxCustomFieldRefLen)).
			Mark(ierr.ErrValidation)
	}

	return nil
}

// Two mappings may not write to the same Zoho field, and neither may claim a field the
// service period already uses.
func (s *InvoiceSyncSettings) ValidateMetadataCustomFields() error {
	if s == nil || len(s.MetadataCustomFields) == 0 {
		return nil
	}

	if len(s.MetadataCustomFields) > MaxMetadataCustomFields {
		return ierr.NewError("too many metadata custom field mappings").
			WithHint(fmt.Sprintf("At most %d metadata custom fields may be mapped", MaxMetadataCustomFields)).
			Mark(ierr.ErrValidation)
	}

	seen := make(map[string]struct{}, len(s.MetadataCustomFields)+2)
	if s.ServicePeriodCustomFields.IsConfigured() {
		seen[s.ServicePeriodCustomFields.StartFieldID] = struct{}{}
		seen[s.ServicePeriodCustomFields.EndFieldID] = struct{}{}
	}

	for _, m := range s.MetadataCustomFields {
		if err := m.Validate(); err != nil {
			return err
		}

		field := strings.TrimSpace(m.Field)
		if _, dup := seen[field]; dup {
			return ierr.NewError("duplicate zoho custom field mapping").
				WithHint(fmt.Sprintf("Zoho custom field %q is mapped more than once", field)).
				Mark(ierr.ErrValidation)
		}
		seen[field] = struct{}{}
	}

	return nil
}

// ServicePeriodCustomFields holds the Zoho custom field IDs for the service period.
type ServicePeriodCustomFields struct {
	StartFieldID string `json:"start_field_id,omitempty"`
	EndFieldID   string `json:"end_field_id,omitempty"`
}

// IsConfigured reports whether both field IDs are present. A half-configured pair
// would render a start date with no end date, so both are required together.
func (s *ServicePeriodCustomFields) IsConfigured() bool {
	if s == nil {
		return false
	}
	return s.StartFieldID != "" && s.EndFieldID != ""
}

func (s *ServicePeriodCustomFields) Validate() error {
	if s == nil {
		return nil
	}
	if (s.StartFieldID == "") != (s.EndFieldID == "") {
		return ierr.NewError("service period custom fields must be set together").
			WithHint("Provide both start_field_id and end_field_id, or neither").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// NormalizedFixedQuantity returns how many units of NormalizeFixedTo fit between start and end.
// Returns 0 if either date is nil, settings are nil, or NormalizeFixedTo is empty.
func (s *InvoiceSyncSettings) NormalizedFixedQuantity(billingPeriod *string) int {
	if s == nil || s.NormalizeFixedTo == "" || billingPeriod == nil || *billingPeriod == "" {
		return 0
	}
	return periodQuantity(*billingPeriod, s.NormalizeFixedTo)
}

// periodQuantity computes how many whole units of the target billing period fit in [start, end).
func periodQuantity(billingPeriod string, target BillingPeriod) int {
	monthsFor := map[BillingPeriod]int{
		BILLING_PERIOD_MONTHLY:   1,
		BILLING_PERIOD_QUARTER:   3,
		BILLING_PERIOD_HALF_YEAR: 6,
		BILLING_PERIOD_ANNUAL:    12,
	}

	bp, bpOk := monthsFor[BillingPeriod(billingPeriod)]
	t, tOk := monthsFor[target]

	if !bpOk || !tOk || t == 0 || t > bp {
		return 0
	}

	return bp / t
}

// DefaultSyncConfig returns a sync config with all entities disabled
func DefaultSyncConfig() *SyncConfig {
	return &SyncConfig{
		// Integration sync
		Plan:         &EntitySyncConfig{Inbound: false, Outbound: false},
		Subscription: &EntitySyncConfig{Inbound: false, Outbound: false},
		Invoice:      &EntitySyncConfig{Inbound: false, Outbound: false},
		Customer:     &EntitySyncConfig{Inbound: false, Outbound: false},
		Payment:      &EntitySyncConfig{Inbound: false, Outbound: false},
		Price:        &EntitySyncConfig{Inbound: false, Outbound: false},
		// CRM sync
		Deal:  &EntitySyncConfig{Inbound: false, Outbound: false},
		Quote: &EntitySyncConfig{Inbound: false, Outbound: false},
	}
}

// Ent calls this without provider context; skips Region.
func (s *SyncConfig) Validate() error {
	if s == nil {
		return nil
	}

	if s.Plan != nil && s.Plan.Outbound {
		return ierr.NewError("plan outbound sync is not allowed").Mark(ierr.ErrValidation)
	}

	if s.Subscription != nil && s.Subscription.Outbound {
		return ierr.NewError("subscription outbound sync is not allowed").Mark(ierr.ErrValidation)
	}

	if s.Invoice != nil && s.Invoice.Inbound {
		return ierr.NewError("invoice inbound sync is not allowed").Mark(ierr.ErrValidation)
	}

	if s.Deal != nil && s.Deal.Inbound {
		return ierr.NewError("deal inbound sync is not allowed").Mark(ierr.ErrValidation)
	}

	if s.Quote != nil && s.Quote.Inbound {
		return ierr.NewError("quote inbound sync is not allowed").Mark(ierr.ErrValidation)
	}

	if s.Price != nil && s.Price.Inbound {
		return ierr.NewError("price inbound sync is not allowed").Mark(ierr.ErrValidation)
	}

	if s.Storage != nil {
		if err := s.Storage.Validate(); err != nil {
			return err
		}
	}

	// Validate AWS Marketplace config if present
	if s.AWSMarketplace != nil {
		if err := s.AWSMarketplace.Validate(); err != nil {
			return err
		}
	}

	if s.InvoiceSyncSettings != nil && s.InvoiceSyncSettings.NormalizeFixedTo != "" {
		if err := s.InvoiceSyncSettings.NormalizeFixedTo.Validate(); err != nil {
			return ierr.NewError("invalid normalize_fixed_to billing period").
				WithHint(err.Error()).
				Mark(ierr.ErrValidation)
		}
	}

	if s.InvoiceSyncSettings != nil {
		if err := s.InvoiceSyncSettings.ServicePeriodCustomFields.Validate(); err != nil {
			return err
		}
		if err := s.InvoiceSyncSettings.ValidateMetadataCustomFields(); err != nil {
			return err
		}
	}

	return nil
}

// Enforces S3-only Region rule.
func (s *SyncConfig) ValidateForProvider(providerType SecretProvider) error {
	if s == nil {
		return nil
	}

	if err := s.Validate(); err != nil {
		return err
	}

	if s.Storage != nil {
		if err := s.Storage.ValidateForProvider(providerType); err != nil {
			return err
		}
	}

	return nil
}

func ProviderBaseSyncConfig(provider SecretProvider) *SyncConfig {
	off := &EntitySyncConfig{Inbound: false, Outbound: false}

	switch provider {
	case SecretProviderStripe:
		return &SyncConfig{
			Customer:     &EntitySyncConfig{Inbound: true, Outbound: true},
			Invoice:      &EntitySyncConfig{Inbound: false, Outbound: true},
			Payment:      off,
			Plan:         off,
			Subscription: off,
			Price:        off,
			Deal:         off,
			Quote:        off,
		}
	case SecretProviderHubSpot:
		return &SyncConfig{
			Customer:     &EntitySyncConfig{Inbound: true, Outbound: false},
			Invoice:      &EntitySyncConfig{Inbound: false, Outbound: true},
			Payment:      off,
			Plan:         off,
			Subscription: off,
			Price:        &EntitySyncConfig{Inbound: false, Outbound: false},
			Deal:         off,
			Quote:        off,
		}
	case SecretProviderRazorpay:
		return &SyncConfig{
			Customer:     &EntitySyncConfig{Inbound: false, Outbound: true},
			Invoice:      &EntitySyncConfig{Inbound: false, Outbound: true},
			Payment:      off,
			Plan:         off,
			Subscription: off,
			Price:        &EntitySyncConfig{Inbound: false, Outbound: false},
			Deal:         off,
			Quote:        off,
		}
	case SecretProviderChargebee:
		return &SyncConfig{
			Customer:     &EntitySyncConfig{Inbound: false, Outbound: true},
			Invoice:      &EntitySyncConfig{Inbound: false, Outbound: true},
			Payment:      off,
			Plan:         off,
			Subscription: off,
			Price:        &EntitySyncConfig{Inbound: false, Outbound: false},
			Deal:         off,
			Quote:        off,
		}
	case SecretProviderQuickBooks:
		return &SyncConfig{
			Customer:     &EntitySyncConfig{Inbound: false, Outbound: true},
			Invoice:      &EntitySyncConfig{Inbound: false, Outbound: true},
			Payment:      off,
			Plan:         off,
			Subscription: off,
			Price:        &EntitySyncConfig{Inbound: false, Outbound: false},
			Deal:         off,
			Quote:        off,
		}
	case SecretProviderZohoBooks:
		return &SyncConfig{
			Customer:     &EntitySyncConfig{Inbound: false, Outbound: false},
			Invoice:      &EntitySyncConfig{Inbound: false, Outbound: true},
			Payment:      off,
			Plan:         off,
			Subscription: off,
			Price:        &EntitySyncConfig{Inbound: false, Outbound: false},
			Deal:         off,
			Quote:        off,
		}
	case SecretProviderPaddle:
		return &SyncConfig{
			Customer:     &EntitySyncConfig{Inbound: true, Outbound: true},
			Invoice:      &EntitySyncConfig{Inbound: true, Outbound: true},
			Payment:      off,
			Plan:         off,
			Subscription: off,
			Price:        &EntitySyncConfig{Inbound: false, Outbound: false},
			Deal:         off,
			Quote:        off,
		}
	case SecretProviderNomod:
		return &SyncConfig{
			Customer:     &EntitySyncConfig{Inbound: false, Outbound: true},
			Invoice:      &EntitySyncConfig{Inbound: false, Outbound: true},
			Payment:      off,
			Plan:         off,
			Subscription: off,
			Price:        &EntitySyncConfig{Inbound: false, Outbound: false},
			Deal:         off,
			Quote:        off,
		}
	case SecretProviderMoyasar:
		return &SyncConfig{
			Customer:     off,
			Invoice:      &EntitySyncConfig{Inbound: false, Outbound: true},
			Payment:      off,
			Plan:         off,
			Subscription: off,
			Price:        &EntitySyncConfig{Inbound: false, Outbound: false},
			Deal:         off,
			Quote:        off,
		}
	case SecretProviderWhop:
		return &SyncConfig{
			Customer:     &EntitySyncConfig{Inbound: false, Outbound: true},
			Invoice:      &EntitySyncConfig{Inbound: false, Outbound: true},
			Payment:      off,
			Plan:         off,
			Subscription: off,
			Price:        &EntitySyncConfig{Inbound: false, Outbound: false},
			Deal:         off,
			Quote:        off,
		}
	case SecretProviderTabs:
		return &SyncConfig{
			Customer:     &EntitySyncConfig{Inbound: false, Outbound: true},
			Invoice:      &EntitySyncConfig{Inbound: false, Outbound: true},
			Payment:      off,
			Plan:         off,
			Subscription: off,
			Price:        &EntitySyncConfig{Inbound: false, Outbound: false},
			Deal:         off,
			Quote:        off,
		}
	default:
		return nil
	}
}
