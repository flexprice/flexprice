package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// ConnectionMetadataType represents the type of connection metadata
type ConnectionMetadataType string

const (
	ConnectionMetadataTypeStripe    ConnectionMetadataType = "stripe"
	ConnectionMetadataTypeGeneric   ConnectionMetadataType = "generic"
	ConnectionMetadataTypeS3        ConnectionMetadataType = "s3"
	ConnectionMetadataTypeHubSpot   ConnectionMetadataType = "hubspot"
	ConnectionMetadataTypeRazorpay  ConnectionMetadataType = "razorpay"
	ConnectionMetadataTypeChargebee ConnectionMetadataType = "chargebee"
	ConnectionMetadataTypeNomod     ConnectionMetadataType = "nomod"
)

func (t ConnectionMetadataType) Validate() error {
	allowedTypes := []ConnectionMetadataType{
		ConnectionMetadataTypeStripe,
		ConnectionMetadataTypeGeneric,
		ConnectionMetadataTypeS3,
		ConnectionMetadataTypeHubSpot,
		ConnectionMetadataTypeRazorpay,
		ConnectionMetadataTypeChargebee,
		ConnectionMetadataTypeNomod,
	}
	if !lo.Contains(allowedTypes, t) {
		return ierr.NewError("invalid connection metadata type").
			WithHint("Connection metadata type must be one of: stripe, generic, s3, hubspot, razorpay, chargebee, nomod").
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

// S3ConnectionMetadata represents S3-specific connection metadata (encrypted secrets only)
// This goes in the encrypted_secret_data column
type S3ConnectionMetadata struct {
	AWSAccessKeyID     string `json:"aws_access_key_id"`           // AWS access key (encrypted)
	AWSSecretAccessKey string `json:"aws_secret_access_key"`       // AWS secret access key (encrypted)
	AWSSessionToken    string `json:"aws_session_token,omitempty"` // AWS session token for temporary credentials (encrypted)
}

// Validate validates the S3 connection metadata
func (s *S3ConnectionMetadata) Validate() error {
	if s.AWSAccessKeyID == "" {
		return ierr.NewError("aws_access_key_id is required").
			WithHint("AWS access key ID is required").
			Mark(ierr.ErrValidation)
	}
	if s.AWSSecretAccessKey == "" {
		return ierr.NewError("aws_secret_access_key is required").
			WithHint("AWS secret access key is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// HubSpotConnectionMetadata represents HubSpot-specific connection metadata
type HubSpotConnectionMetadata struct {
	AccessToken  string `json:"access_token"`     // Private App Access Token (encrypted)
	ClientSecret string `json:"client_secret"`    // Private App Client Secret for webhook verification (encrypted)
	AppID        string `json:"app_id,omitempty"` // HubSpot App ID (optional, not encrypted)
}

// Validate validates the HubSpot connection metadata
func (h *HubSpotConnectionMetadata) Validate() error {
	if h.AccessToken == "" {
		return ierr.NewError("access_token is required").
			WithHint("HubSpot access token is required").
			Mark(ierr.ErrValidation)
	}
	if h.ClientSecret == "" {
		return ierr.NewError("client_secret is required").
			WithHint("HubSpot client secret is required for webhook verification").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// RazorpayConnectionMetadata represents Razorpay-specific connection metadata
type RazorpayConnectionMetadata struct {
	KeyID         string `json:"key_id"`         // Razorpay Key ID (encrypted)
	SecretKey     string `json:"secret_key"`     // Razorpay Secret Key (encrypted)
	WebhookSecret string `json:"webhook_secret"` // Razorpay Webhook Secret (encrypted, optional)
}

// Validate validates the Razorpay connection metadata
func (r *RazorpayConnectionMetadata) Validate() error {
	if r.KeyID == "" {
		return ierr.NewError("key_id is required").
			WithHint("Razorpay key ID is required").
			Mark(ierr.ErrValidation)
	}
	if r.SecretKey == "" {
		return ierr.NewError("secret_key is required").
			WithHint("Razorpay secret key is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// ChargebeeConnectionMetadata represents Chargebee-specific connection metadata
type ChargebeeConnectionMetadata struct {
	Site            string `json:"site"`                       // Chargebee site name (not encrypted)
	APIKey          string `json:"api_key"`                    // Chargebee API key (encrypted)
	WebhookSecret   string `json:"webhook_secret,omitempty"`   // Chargebee Webhook Secret (encrypted, optional, NOT USED in v2)
	WebhookUsername string `json:"webhook_username,omitempty"` // Basic Auth username for webhooks (encrypted)
	WebhookPassword string `json:"webhook_password,omitempty"` // Basic Auth password for webhooks (encrypted)
}

// Validate validates the Chargebee connection metadata
func (c *ChargebeeConnectionMetadata) Validate() error {
	if c.Site == "" {
		return ierr.NewError("site is required").
			WithHint("Chargebee site name is required").
			Mark(ierr.ErrValidation)
	}
	if c.APIKey == "" {
		return ierr.NewError("api_key is required").
			WithHint("Chargebee API key is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// QuickBooksConnectionMetadata represents QuickBooks-specific connection metadata
type QuickBooksConnectionMetadata struct {
	// Required for initial connection setup
	ClientID     string `json:"client_id"`     // OAuth Client ID (encrypted)
	ClientSecret string `json:"client_secret"` // OAuth Client Secret (encrypted)
	RealmID      string `json:"realm_id"`      // QuickBooks Company ID (not encrypted)
	Environment  string `json:"environment"`   // "sandbox" or "production"

	// Optional - for initial setup via auth code (will be cleared after token exchange)
	AuthCode    string `json:"auth_code,omitempty"`    // OAuth Authorization Code (temporary, encrypted)
	RedirectURI string `json:"redirect_uri,omitempty"` // OAuth Redirect URI (temporary)

	// Managed internally - set after auth code exchange or token refresh
	AccessToken  string `json:"access_token,omitempty"`  // OAuth Access Token (encrypted)
	RefreshToken string `json:"refresh_token,omitempty"` // OAuth Refresh Token (encrypted)

	// Webhook security
	WebhookVerifierToken string `json:"webhook_verifier_token,omitempty"` // QuickBooks webhook verifier token (encrypted)

	// Optional configuration
	IncomeAccountID string `json:"income_account_id,omitempty"` // QuickBooks Income Account ID (optional, defaults to "79")

	// Temporary OAuth session data (only used during OAuth flow, cleared after completion)
	OAuthSessionData string `json:"oauth_session_data,omitempty"` // Encrypted JSON containing session_id, csrf_state, credentials, etc.
}

// Validate validates the QuickBooks connection metadata
func (q *QuickBooksConnectionMetadata) Validate() error {
	if q.ClientID == "" {
		return ierr.NewError("client_id is required").
			WithHint("QuickBooks OAuth client ID is required").
			Mark(ierr.ErrValidation)
	}
	if q.ClientSecret == "" {
		return ierr.NewError("client_secret is required").
			WithHint("QuickBooks OAuth client secret is required").
			Mark(ierr.ErrValidation)
	}
	if q.RealmID == "" {
		return ierr.NewError("realm_id is required").
			WithHint("QuickBooks Company ID (realm ID) is required").
			Mark(ierr.ErrValidation)
	}
	if q.Environment != "sandbox" && q.Environment != "production" {
		return ierr.NewError("environment must be 'sandbox' or 'production'").
			WithHint("QuickBooks environment must be either 'sandbox' or 'production'").
			Mark(ierr.ErrValidation)
	}
	// Note: AccessToken and RefreshToken are not required during validation
	// They will be generated internally via auth code exchange or token refresh
	return nil
}

// NomodConnectionMetadata represents Nomod-specific connection metadata
type NomodConnectionMetadata struct {
	APIKey        string `json:"api_key"`        // Nomod API Key (encrypted)
	WebhookSecret string `json:"webhook_secret"` // Basic Auth secret for webhooks (encrypted, optional)
}

// Validate validates the Nomod connection metadata
func (n *NomodConnectionMetadata) Validate() error {
	if n.APIKey == "" {
		return ierr.NewError("api_key is required").
			WithHint("Nomod API key is required").
			Mark(ierr.ErrValidation)
	}
	// WebhookSecret is optional
	return nil
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
	Stripe     *StripeConnectionMetadata     `json:"stripe,omitempty"`
	S3         *S3ConnectionMetadata         `json:"s3,omitempty"`
	HubSpot    *HubSpotConnectionMetadata    `json:"hubspot,omitempty"`
	Razorpay   *RazorpayConnectionMetadata   `json:"razorpay,omitempty"`
	Chargebee  *ChargebeeConnectionMetadata  `json:"chargebee,omitempty"`
	QuickBooks *QuickBooksConnectionMetadata `json:"quickbooks,omitempty"`
	Nomod      *NomodConnectionMetadata      `json:"nomod,omitempty"`
	Generic    *GenericConnectionMetadata    `json:"generic,omitempty"`
	Settings   *ConnectionSettings           `json:"settings,omitempty"`
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
	case SecretProviderS3:
		if c.S3 == nil {
			return ierr.NewError("s3 metadata is required").
				WithHint("S3 metadata is required for s3 provider").
				Mark(ierr.ErrValidation)
		}
		return c.S3.Validate()
	case SecretProviderHubSpot:
		if c.HubSpot == nil {
			return ierr.NewError("hubspot metadata is required").
				WithHint("HubSpot metadata is required for hubspot provider").
				Mark(ierr.ErrValidation)
		}
		return c.HubSpot.Validate()
	case SecretProviderRazorpay:
		if c.Razorpay == nil {
			return ierr.NewError("razorpay metadata is required").
				WithHint("Razorpay metadata is required for razorpay provider").
				Mark(ierr.ErrValidation)
		}
		return c.Razorpay.Validate()
	case SecretProviderChargebee:
		if c.Chargebee == nil {
			return ierr.NewError("chargebee metadata is required").
				WithHint("Chargebee metadata is required for chargebee provider").
				Mark(ierr.ErrValidation)
		}
		return c.Chargebee.Validate()
	case SecretProviderQuickBooks:
		if c.QuickBooks == nil {
			return ierr.NewError("quickbooks metadata is required").
				WithHint("QuickBooks metadata is required for quickbooks provider").
				Mark(ierr.ErrValidation)
		}
		return c.QuickBooks.Validate()
	case SecretProviderNomod:
		if c.Nomod == nil {
			return ierr.NewError("nomod metadata is required").
				WithHint("Nomod metadata is required for nomod provider").
				Mark(ierr.ErrValidation)
		}
		return c.Nomod.Validate()
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
