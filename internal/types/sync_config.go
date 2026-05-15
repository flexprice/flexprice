package types

import (
	"math"
	"time"

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
	// S3 connection metadata (for Flexprice-managed S3 connections)
	S3 *S3ExportConfig `json:"s3,omitempty"`
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

	// CollapseUsage when true collapses usage line items to qty=1 with rate=total amount.
	CollapseUsage bool `json:"collapse_usage,omitempty"`
}

// NormalizedFixedQuantity returns how many units of NormalizeFixedTo fit between start and end.
// Returns 0 if either date is nil, settings are nil, or NormalizeFixedTo is empty.
func (s *InvoiceSyncSettings) NormalizedFixedQuantity(start, end *time.Time) int {
	if s == nil || s.NormalizeFixedTo == "" || start == nil || end == nil {
		return 0
	}
	return periodQuantity(*start, *end, s.NormalizeFixedTo)
}

// periodQuantity computes how many whole units of the target billing period fit in [start, end).
func periodQuantity(start, end time.Time, target BillingPeriod) int {
	if !end.After(start) {
		return 0
	}
	switch target {
	case BILLING_PERIOD_DAILY:
		return int(math.Round(end.Sub(start).Hours() / 24))
	case BILLING_PERIOD_WEEKLY:
		return int(math.Round(end.Sub(start).Hours() / (24 * 7)))
	case BILLING_PERIOD_MONTHLY:
		return monthsBetween(start, end)
	case BILLING_PERIOD_QUARTER:
		return monthsBetween(start, end) / 3
	case BILLING_PERIOD_HALF_YEAR:
		return monthsBetween(start, end) / 6
	case BILLING_PERIOD_ANNUAL:
		return monthsBetween(start, end) / 12
	default:
		return 0
	}
}

func monthsBetween(start, end time.Time) int {
	years := end.Year() - start.Year()
	months := int(end.Month()) - int(start.Month())
	total := years*12 + months
	if end.Day() < start.Day() && !isLastDayOfMonth(end) {
		total--
	}
	if total < 0 {
		return 0
	}
	return total
}

func isLastDayOfMonth(t time.Time) bool {
	return t.Day() == time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, t.Location()).Day()
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

// Validate validates the SyncConfig
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

	// Validate S3 export config if present
	if s.S3 != nil {
		if err := s.S3.Validate(); err != nil {
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
