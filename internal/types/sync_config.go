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
	Payment      *EntitySyncConfig `json:"payment,omitempty"` // Payment sync (QuickBooks bidirectional)
	// CRM sync (HubSpot, Salesforce, etc.)
	Deal  *EntitySyncConfig `json:"deal,omitempty"`
	Quote *EntitySyncConfig `json:"quote,omitempty"`
}

// EntitySyncConfig defines sync direction for an entity
type EntitySyncConfig struct {
	Inbound  bool `json:"inbound"`  // Inbound from external provider to FlexPrice
	Outbound bool `json:"outbound"` // Outbound from FlexPrice to external provider
}

// DefaultSyncConfig returns a sync config with all entities disabled
func DefaultSyncConfig() *SyncConfig {
	return &SyncConfig{
		// Integration sync
		Plan:         &EntitySyncConfig{Inbound: false, Outbound: false},
		Subscription: &EntitySyncConfig{Inbound: false, Outbound: false},
		Invoice:      &EntitySyncConfig{Inbound: false, Outbound: false},
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

	return nil
}
