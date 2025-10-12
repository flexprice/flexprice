package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// ConnectionMetadataType represents the type of connection metadata
type ConnectionMetadataType string

const (
	ConnectionMetadataTypeStripe  ConnectionMetadataType = "stripe"
	ConnectionMetadataTypeGeneric ConnectionMetadataType = "generic"
)

func (t ConnectionMetadataType) Validate() error {
	allowedTypes := []ConnectionMetadataType{
		ConnectionMetadataTypeStripe,
		ConnectionMetadataTypeGeneric,
	}
	if !lo.Contains(allowedTypes, t) {
		return ierr.NewError("invalid connection metadata type").
			WithHint("Connection metadata type must be one of: stripe, generic").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// StripeConnectionMetadata represents Stripe-specific connection metadata
type StripeConnectionMetadata struct {
	PublishableKey string `json:"publishable_key"`
	SecretKey      string `json:"secret_key"`
	WebhookSecret  string `json:"webhook_secret"`
	AccountID      string `json:"account_id,omitempty"`
}

// ConnectionSettings represents general connection settings
type ConnectionSettings struct {
	InvoiceSyncEnable *bool `json:"invoice_sync_enable,omitempty"`
}

// Validate validates the Stripe connection metadata
func (s *StripeConnectionMetadata) Validate() error {
	if s.PublishableKey == "" {
		return ierr.NewError("publishable_key is required").
			WithHint("Stripe publishable key is required").
			Mark(ierr.ErrValidation)
	}
	if s.SecretKey == "" {
		return ierr.NewError("secret_key is required").
			WithHint("Stripe secret key is required").
			Mark(ierr.ErrValidation)
	}
	if s.WebhookSecret == "" {
		return ierr.NewError("webhook_secret is required").
			WithHint("Stripe webhook secret is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// GenericConnectionMetadata represents generic connection metadata
type GenericConnectionMetadata struct {
	Data map[string]interface{} `json:"data"`
}

// Validate validates the generic connection metadata
func (g *GenericConnectionMetadata) Validate() error {
	if g.Data == nil {
		return ierr.NewError("data is required").
			WithHint("Generic connection metadata data is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// ConnectionMetadata represents structured connection metadata
type ConnectionMetadata struct {
	Stripe   *StripeConnectionMetadata  `json:"stripe,omitempty"`
	Generic  *GenericConnectionMetadata `json:"generic,omitempty"`
	Settings *ConnectionSettings        `json:"settings,omitempty"`
}

// Validate validates the connection metadata based on provider type
func (c *ConnectionMetadata) Validate(providerType SecretProvider) error {
	switch providerType {
	case SecretProviderStripe:
		if c.Stripe == nil {
			return ierr.NewError("stripe metadata is required").
				WithHint("Stripe metadata is required for stripe provider").
				Mark(ierr.ErrValidation)
		}
		return c.Stripe.Validate()
	default:
		// For other providers or unknown types, use generic format
		if c.Generic == nil {
			return ierr.NewError("generic metadata is required").
				WithHint("Generic metadata is required for this provider type").
				Mark(ierr.ErrValidation)
		}
		return c.Generic.Validate()
	}
}

// ConnectionFilter represents filters for connection queries
type ConnectionFilter struct {
	*QueryFilter
	*TimeRangeFilter
	// filters allows complex filtering based on multiple fields

	Filters       []*FilterCondition `json:"filters,omitempty" form:"filters" validate:"omitempty"`
	Sort          []*SortCondition   `json:"sort,omitempty" form:"sort" validate:"omitempty"`
	ConnectionIDs []string           `json:"connection_ids,omitempty" form:"connection_ids" validate:"omitempty"`
	ProviderType  SecretProvider     `json:"provider_type,omitempty" form:"provider_type" validate:"omitempty"`
}

// NewConnectionFilter creates a new ConnectionFilter with default values
func NewConnectionFilter() *ConnectionFilter {
	return &ConnectionFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

// NewNoLimitConnectionFilter creates a new ConnectionFilter with no pagination limits
func NewNoLimitConnectionFilter() *ConnectionFilter {
	return &ConnectionFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

// Validate validates the connection filter
func (f ConnectionFilter) Validate() error {
	if f.QueryFilter != nil {
		if err := f.QueryFilter.Validate(); err != nil {
			return err
		}
	}

	if f.TimeRangeFilter != nil {
		if err := f.TimeRangeFilter.Validate(); err != nil {
			return err
		}
	}

	if f.ProviderType != "" {
		if err := f.ProviderType.Validate(); err != nil {
			return err
		}
	}

	return nil
}

// GetLimit implements BaseFilter interface
func (f *ConnectionFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *ConnectionFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface
func (f *ConnectionFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface
func (f *ConnectionFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus implements BaseFilter interface
func (f *ConnectionFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// IsUnlimited implements BaseFilter interface
func (f *ConnectionFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return false
	}
	return f.QueryFilter.IsUnlimited()
}
