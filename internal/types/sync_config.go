package types

import (
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
	// CRM sync (HubSpot, Salesforce, etc.)
	Deal  *EntitySyncConfig `json:"deal,omitempty"`
	Quote *EntitySyncConfig `json:"quote,omitempty"`
	// Storage connection metadata (for Flexprice-managed or customer BYO storage connections; S3 or GCS)
	// JSON tag stays "s3" for backward compatibility with existing DB rows; the Go field name is cloud-agnostic since GCS connections also use this config.
	Storage *StorageExportConfig `json:"s3,omitempty"`
	// InvoiceSyncSettings controls line-item transformation during outbound invoice sync
	InvoiceSyncSettings *InvoiceSyncSettings `json:"invoice_sync_settings,omitempty"`
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
		// CRM sync
		Deal:  &EntitySyncConfig{Inbound: false, Outbound: false},
		Quote: &EntitySyncConfig{Inbound: false, Outbound: false},
	}
}

// Validate validates the SyncConfig without provider context.
//
// NOTE: this method is also invoked implicitly by Ent's generated code, because
// ent/schema/connection.go declares sync_config as field.JSON(&types.SyncConfig{}),
// and Ent's codegen auto-detects and calls a no-arg Validate() error method on any
// JSON field type at Create()/Update() time. That call site has no provider-type
// context available, so the embedded StorageExportConfig is checked with its
// provider-agnostic Validate() (no S3-only Region requirement enforced here).
// Callers that DO have provider context should call ValidateForProvider instead.
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

	// Validate storage export config if present
	if s.Storage != nil {
		if err := s.Storage.Validate(); err != nil {
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

	return nil
}

// ValidateForProvider validates the SyncConfig for the given connection provider
// type, additionally enforcing the S3-only Region requirement on the embedded
// StorageExportConfig. Prefer this over the plain Validate() wherever the caller
// has provider-type context (e.g. connection create/update).
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
			Deal:         off,
			Quote:        off,
		}
	default:
		return nil
	}
}
