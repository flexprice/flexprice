package types

import (
	"encoding/json"
	"net/url"
	"strings"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// ConnectionMetadataType represents the type of connection metadata
type ConnectionMetadataType string

const (
	ConnectionMetadataTypeStripe         ConnectionMetadataType = "stripe"
	ConnectionMetadataTypeGeneric        ConnectionMetadataType = "generic"
	ConnectionMetadataTypeS3             ConnectionMetadataType = "s3"
	ConnectionMetadataTypeHubSpot        ConnectionMetadataType = "hubspot"
	ConnectionMetadataTypeRazorpay       ConnectionMetadataType = "razorpay"
	ConnectionMetadataTypeChargebee      ConnectionMetadataType = "chargebee"
	ConnectionMetadataTypeNomod          ConnectionMetadataType = "nomod"
	ConnectionMetadataTypeMoyasar        ConnectionMetadataType = "moyasar"
	ConnectionMetadataTypePaddle         ConnectionMetadataType = "paddle"
	ConnectionMetadataTypeZohoBooks      ConnectionMetadataType = "zoho_books"
	ConnectionMetadataTypeWhop           ConnectionMetadataType = "whop"
	ConnectionMetadataTypeAWSMarketplace ConnectionMetadataType = "aws_marketplace"
	ConnectionMetadataTypeGCPMarketplace ConnectionMetadataType = "gcp_marketplace"
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
		ConnectionMetadataTypeMoyasar,
		ConnectionMetadataTypePaddle,
		ConnectionMetadataTypeZohoBooks,
		ConnectionMetadataTypeWhop,
		ConnectionMetadataTypeAWSMarketplace,
		ConnectionMetadataTypeGCPMarketplace,
	}
	if !lo.Contains(allowedTypes, t) {
		return ierr.NewError("invalid connection metadata type").
			WithHint("Connection metadata type must be one of: stripe, generic, s3, hubspot, razorpay, chargebee, nomod, moyasar, paddle, zoho_books, whop, aws_marketplace, gcp_marketplace").
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
	Site   string `json:"site"`    // Chargebee site name (not encrypted)
	APIKey string `json:"api_key"` // Chargebee API key (encrypted)
	WebhookSecret   string `json:"webhook_secret,omitempty"`   // Chargebee Webhook Secret (encrypted, optional, NOT USED in v2)
	WebhookUsername string `json:"webhook_username,omitempty"` // Basic Auth username for webhooks (encrypted)
	WebhookPassword string `json:"webhook_password,omitempty"` // Basic Auth password for webhooks (encrypted)
	// GatewayAccountID optionally pins which gateway account NEW cards are vaulted
	// against. Charging an existing card never needs it — the payment source carries
	// its own gateway. Empty defers to the site's own routing, which is correct for a
	// deliberately configured multi-gateway site; set it only to override that, e.g.
	// when flow-specific defaults would otherwise split one customer's cards across
	// gateways. Not a secret; not encrypted.
	GatewayAccountID string `json:"gateway_account_id,omitempty"`
}

// isValidChargebeeSite reports whether s is a bare Chargebee site name.
// Chargebee sites are DNS labels: letters, digits and hyphens, not starting or
// ending with a hyphen.
func isValidChargebeeSite(s string) bool {
	if s == "" || len(s) > 63 {
		return false
	}
	if strings.HasPrefix(s, "-") || strings.HasSuffix(s, "-") {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-':
		default:
			return false
		}
	}
	return true
}

// Validate validates the Chargebee connection metadata
func (c *ChargebeeConnectionMetadata) Validate() error {
	if c.Site == "" {
		return ierr.NewError("site is required").
			WithHint("Chargebee site name is required").
			Mark(ierr.ErrValidation)
	}
	// The SDK builds request URLs by appending .chargebee.com to this value, so
	// it must be a bare site name. Anything containing URL syntax — a fragment,
	// path, port, or dot — would change which host the request reaches rather
	// than which Chargebee site it addresses.
	if !isValidChargebeeSite(c.Site) {
		return ierr.NewError("site must be a valid Chargebee site name").
			WithHint("Chargebee site must be the bare site name (letters, digits and hyphens only), not a URL").
			Mark(ierr.ErrValidation)
	}
	if c.APIKey == "" {
		return ierr.NewError("api_key is required").
			WithHint("Chargebee API key is required").
			Mark(ierr.ErrValidation)
	}
	// Chargebee v2 has no webhook signature scheme, so Basic Auth is the only
	// supported way to authenticate incoming webhooks. Without it, anyone who
	// knows a Chargebee invoice id can forge payment_succeeded events.
	if c.WebhookUsername == "" {
		return ierr.NewError("webhook_username is required").
			WithHint("Enable Basic Auth on the Chargebee webhook and set webhook_username to match").
			Mark(ierr.ErrValidation)
	}
	if c.WebhookPassword == "" {
		return ierr.NewError("webhook_password is required").
			WithHint("Enable Basic Auth on the Chargebee webhook and set webhook_password to match").
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

// MoyasarConnectionMetadata represents Moyasar-specific connection metadata
type MoyasarConnectionMetadata struct {
	PublishableKey string `json:"publishable_key"` // Moyasar Publishable Key (encrypted, for frontend use)
	SecretKey      string `json:"secret_key"`      // Moyasar Secret Key (encrypted)
	WebhookSecret  string `json:"webhook_secret"`  // Moyasar Webhook Secret (encrypted, optional)
}

// Validate validates the Moyasar connection metadata
func (m *MoyasarConnectionMetadata) Validate() error {
	if m.SecretKey == "" {
		return ierr.NewError("secret_key is required").
			WithHint("Moyasar secret key is required").
			Mark(ierr.ErrValidation)
	}
	// PublishableKey and WebhookSecret are optional
	return nil
}

// PaddleConnectionMetadata represents Paddle-specific connection metadata
type PaddleConnectionMetadata struct {
	APIKey          string `json:"api_key"`           // Paddle API Key (encrypted)
	WebhookSecret   string `json:"webhook_secret"`    // Paddle webhook secret (encrypted)
	ClientSideToken string `json:"client_side_token"` // Paddle.js client-side token (optional, encrypted)
}

// ZohoBooksConnectionMetadata represents Zoho Books OAuth connection metadata
type ZohoBooksConnectionMetadata struct {
	ClientID             string `json:"client_id"`                         // OAuth Client ID (encrypted)
	ClientSecret         string `json:"client_secret"`                     // OAuth Client Secret (encrypted)
	RefreshToken         string `json:"refresh_token,omitempty"`           // OAuth Refresh Token (encrypted)
	AccessToken          string `json:"access_token,omitempty"`            // OAuth Access Token (encrypted cache)
	AuthCode             string `json:"auth_code,omitempty"`               // OAuth Authorization Code (temporary, encrypted)
	RedirectURI          string `json:"redirect_uri,omitempty"`            // OAuth Redirect URI
	APIDomain            string `json:"api_domain,omitempty"`              // Zoho API domain from token exchange
	AccountsURL          string `json:"accounts_server,omitempty"`         // Zoho Accounts base URL / DC
	Location             string `json:"location,omitempty"`                // Zoho account location/DC hint
	OrganizationID       string `json:"organization_id,omitempty"`         // Selected Zoho Books organization
	OrganizationName     string `json:"organization_name,omitempty"`       // Selected organization name
	Scopes               string `json:"scopes,omitempty"`                  // Granted scopes, comma-separated
	AccessTokenExpiresAt string `json:"access_token_expires_at,omitempty"` // RFC3339 expiry timestamp
	OAuthSessionData     string `json:"oauth_session_data,omitempty"`      // Temporary encrypted session data
	// WebhookSecret is the Zoho Books webhook signing secret (encrypted at rest). Optional until inbound webhooks are configured.
	WebhookSecret string `json:"webhook_secret,omitempty"`
}

// Validate validates the Zoho Books connection metadata
func (z *ZohoBooksConnectionMetadata) Validate() error {
	if z.ClientID == "" {
		return ierr.NewError("client_id is required").
			WithHint("Zoho Books OAuth client ID is required").
			Mark(ierr.ErrValidation)
	}
	if z.ClientSecret == "" {
		return ierr.NewError("client_secret is required").
			WithHint("Zoho Books OAuth client secret is required").
			Mark(ierr.ErrValidation)
	}
	if z.RefreshToken == "" {
		return ierr.NewError("refresh_token is required").
			WithHint("Zoho Books refresh token is required after OAuth completion").
			Mark(ierr.ErrValidation)
	}
	if z.OrganizationID == "" {
		return ierr.NewError("organization_id is required").
			WithHint("Zoho Books organization_id is required for API calls").
			Mark(ierr.ErrValidation)
	}

	// accounts_server and api_domain are stored and then re-read as the host for
	// every subsequent token refresh and API call, which send this connection's
	// client_id and client_secret. They are only ever Zoho datacenter endpoints,
	// so restrict them to Zoho domains rather than accepting any host supplied
	// when the connection is created.
	if err := validateZohoEndpoint(z.AccountsURL, "accounts_server"); err != nil {
		return err
	}
	if err := validateZohoEndpoint(z.APIDomain, "api_domain"); err != nil {
		return err
	}

	return nil
}

// zohoEndpointSuffixes are the domains Zoho serves its accounts and API
// endpoints from across all datacenters. accounts_server uses the zoho.*
// domains; api_domain uses the zohoapis.* ones.
var zohoEndpointSuffixes = []string{
	".zoho.com",
	".zoho.eu",
	".zoho.in",
	".zoho.com.au",
	".zoho.jp",
	".zoho.com.cn",
	".zoho.sa",
	".zoho.uk",
	".zohocloud.ca",
	".zohoapis.com",
	".zohoapis.eu",
	".zohoapis.in",
	".zohoapis.com.au",
	".zohoapis.jp",
	".zohoapis.com.cn",
	".zohoapis.sa",
	".zohoapis.uk",
	".zohoapiscloud.ca",
}

// ValidateZohoEndpoint restricts a client-supplied Zoho URL to an https Zoho
// domain. Callers performing the OAuth token exchange must apply this to the
// accounts_server before sending client_id/client_secret to it, so those
// credentials cannot be exfiltrated to an attacker-chosen host.
//
// Unlike the internal check, this enforces a bare-origin invariant: the value is
// concatenated with a fixed OAuth path (e.g. accounts_server + "/oauth/v2/token"),
// so any path, query, fragment, or userinfo component would change which URL is
// actually reached — a query would swallow the OAuth path, a fragment would strip
// the OAuth query, and userinfo would send credentials to a different authority.
// It also rejects an empty value.
func ValidateZohoEndpoint(raw, field string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ierr.NewError(field+" is required").
			WithHintf("Zoho Books %s must be provided", field).
			Mark(ierr.ErrValidation)
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return ierr.NewError(field+" must be a valid https URL").
			WithHintf("Zoho Books %s must be an https URL", field).
			Mark(ierr.ErrValidation)
	}
	if u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return ierr.NewError(field+" must be a bare origin").
			WithHintf("Zoho Books %s must be a scheme and host only, with no path, query, fragment, or credentials", field).
			Mark(ierr.ErrValidation)
	}

	return validateZohoEndpoint(trimmed, field)
}

// validateZohoEndpoint checks a Zoho-supplied URL is https and served from a
// Zoho domain. An empty value is allowed: these fields are populated by the
// token exchange and are absent before OAuth completes.
func validateZohoEndpoint(raw, field string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !u.IsAbs() || u.Scheme != "https" {
		return ierr.NewError(field+" must be a valid https URL").
			WithHintf("Zoho Books %s must be an https URL", field).
			Mark(ierr.ErrValidation)
	}

	host := strings.ToLower(u.Hostname())
	for _, suffix := range zohoEndpointSuffixes {
		// Match the bare domain as well as any subdomain of it.
		if strings.HasSuffix(host, suffix) || host == strings.TrimPrefix(suffix, ".") {
			return nil
		}
	}

	return ierr.NewError(field+" must be a Zoho endpoint").
		WithHintf("Zoho Books %s must be a Zoho domain", field).
		WithReportableDetails(map[string]any{
			"allowed_domains": zohoEndpointSuffixes,
		}).
		Mark(ierr.ErrValidation)
}

// Validate validates the Paddle connection metadata
func (p *PaddleConnectionMetadata) Validate() error {
	if p.APIKey == "" {
		return ierr.NewError("api_key is required").
			WithHint("Paddle API key is required").
			Mark(ierr.ErrValidation)
	}
	if p.WebhookSecret == "" {
		return ierr.NewError("webhook_secret is required").
			WithHint("Paddle webhook secret is required for webhook verification").
			Mark(ierr.ErrValidation)
	}
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

// WhopConnectionMetadata represents Whop-specific connection metadata
type WhopConnectionMetadata struct {
	APIKey        string `json:"api_key"`              // Whop API key / Bearer token (encrypted)
	CompanyID     string `json:"company_id"`           // Whop company ID (biz_...)
	ProductID     string `json:"product_id,omitempty"` // Whop product ID (prod_...) - created on first sync if empty
	WebhookSecret string `json:"webhook_secret"`       // Whop webhook signing secret (encrypted) - required to verify incoming webhooks
}

// Validate validates the Whop connection metadata
func (w *WhopConnectionMetadata) Validate() error {
	if w.APIKey == "" {
		return ierr.NewError("api_key is required").
			WithHint("Whop API key is required").
			Mark(ierr.ErrValidation)
	}
	if w.CompanyID == "" {
		return ierr.NewError("company_id is required").
			WithHint("Whop company ID is required").
			Mark(ierr.ErrValidation)
	}
	if w.WebhookSecret == "" {
		return ierr.NewError("webhook_secret is required").
			WithHint("Whop webhook secret is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// TabsConnectionMetadata represents Tabs-specific connection metadata
type TabsConnectionMetadata struct {
	APIKey string `json:"api_key"` // Tabs API Key (encrypted)
}

// Validate validates the Tabs connection metadata
func (t *TabsConnectionMetadata) Validate() error {
	if t.APIKey == "" {
		return ierr.NewError("api_key is required").
			WithHint("Tabs API key is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// AWSMarketplaceConnectionSecrets represents AWS Marketplace connection secrets. Both fields are
// tenant-provided (the frontend derives ExternalID deterministically from tenant_id and displays it
// inline with the static IAM/trust policy templates the tenant pastes into their own AWS account) and
// stored encrypted, so a single decrypt at cron time yields everything AssumeRole needs (design doc
// FLE-981 §7.1).
type AWSMarketplaceConnectionSecrets struct {
	RoleArn    string `json:"role_arn"`
	ExternalID string `json:"external_id"`
}

// Validate validates the AWS Marketplace connection secrets
func (a *AWSMarketplaceConnectionSecrets) Validate() error {
	if a.RoleArn == "" {
		return ierr.NewError("role_arn is required").
			WithHint("AWS Marketplace role_arn is required").
			Mark(ierr.ErrValidation)
	}
	if a.ExternalID == "" {
		return ierr.NewError("external_id is required").
			WithHint("AWS Marketplace external_id is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// GCPMarketplaceConnectionSecrets represents GCP Marketplace connection secrets. CredentialsJSON is
// the tenant's Workload Identity Federation credentials file, generated by
// `gcloud iam workload-identity-pools create-cred-config --aws` and pasted verbatim. It is stored as
// one opaque encrypted blob — there is no private key inside it, only config telling Google's client
// library how to exchange Flexprice's own AWS credentials for a short-lived GCP token at report time
// (design doc FLE-981 §5.4).
type GCPMarketplaceConnectionSecrets struct {
	CredentialsJSON string `json:"credentials_json"`
}

// Validate validates the GCP Marketplace connection secrets. It checks the JSON is well-formed and
// has every top-level field the external_account flow needs — audience, token_url, credential_source,
// and service_account_impersonation_url, which the setup script's `create-cred-config --aws` call
// always produces — so a malformed or incomplete paste fails here with a clear hint, rather than only
// surfacing during the live token exchange. It does not attempt the exchange itself (that happens
// once, synchronously, in the connection-creation verification step).
func (g *GCPMarketplaceConnectionSecrets) Validate() error {
	if g.CredentialsJSON == "" {
		return ierr.NewError("credentials_json is required").
			WithHint("GCP Marketplace credentials_json is required").
			Mark(ierr.ErrValidation)
	}
	var parsed struct {
		Type                           string          `json:"type"`
		Audience                       string          `json:"audience"`
		TokenURL                       string          `json:"token_url"`
		CredentialSource               json.RawMessage `json:"credential_source"`
		ServiceAccountImpersonationURL string          `json:"service_account_impersonation_url"`
	}
	if err := json.Unmarshal([]byte(g.CredentialsJSON), &parsed); err != nil {
		return ierr.NewError("credentials_json is not valid JSON").
			WithHint("GCP Marketplace credentials_json must be the JSON file generated by `gcloud iam workload-identity-pools create-cred-config`").
			Mark(ierr.ErrValidation)
	}
	if parsed.Type != "external_account" {
		return ierr.NewError("credentials_json is not a workload identity federation config").
			WithHint("GCP Marketplace credentials_json must have \"type\": \"external_account\" — paste the file generated by `gcloud iam workload-identity-pools create-cred-config`, not a service account key").
			Mark(ierr.ErrValidation)
	}
	if parsed.Audience == "" || parsed.TokenURL == "" || len(parsed.CredentialSource) == 0 || parsed.ServiceAccountImpersonationURL == "" {
		return ierr.NewError("credentials_json is missing required fields").
			WithHint("GCP Marketplace credentials_json must include audience, token_url, credential_source, and service_account_impersonation_url — paste the file generated by `gcloud iam workload-identity-pools create-cred-config --aws` unmodified").
			Mark(ierr.ErrValidation)
	}

	// Both URLs are contacted synchronously during connection verification and
	// on every later token exchange. The generated config always points at
	// Google's STS and IAM endpoints, so anything else means the file was edited
	// after generation and must not be used as an outbound request target.
	if err := validateGoogleEndpoint(parsed.TokenURL, "token_url"); err != nil {
		return err
	}
	if err := validateGoogleEndpoint(parsed.ServiceAccountImpersonationURL, "service_account_impersonation_url"); err != nil {
		return err
	}

	return nil
}

// googleEndpointSuffixes are the domains Google serves its STS and IAM
// credential endpoints from.
// googleEndpointHosts are the exact hosts the workload identity federation flow
// contacts. Matched exactly rather than by domain suffix: a suffix match on
// googleapis.com would also accept hosts serving tenant-controlled content,
// such as storage.googleapis.com.
var googleEndpointHosts = []string{
	"sts.googleapis.com",
	"iamcredentials.googleapis.com",
}

// validateGoogleEndpoint checks a URL from a workload identity federation
// config is https and addresses one of Google's credential endpoints.
func validateGoogleEndpoint(raw, field string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !u.IsAbs() || u.Scheme != "https" {
		return ierr.NewError(field+" must be a valid https URL").
			WithHintf("GCP Marketplace credentials_json %s must be an https URL", field).
			Mark(ierr.ErrValidation)
	}

	host := strings.ToLower(u.Hostname())
	if lo.Contains(googleEndpointHosts, host) {
		return nil
	}

	return ierr.NewError(field+" must be a Google credentials endpoint").
		WithHintf("GCP Marketplace credentials_json %s must be a Google credentials endpoint — paste the file generated by `gcloud iam workload-identity-pools create-cred-config` unmodified", field).
		WithReportableDetails(map[string]any{
			"allowed_hosts": googleEndpointHosts,
		}).
		Mark(ierr.ErrValidation)
}

// AzureMarketplaceConnectionSecrets represents Azure Marketplace connection secrets. All three
// fields are generated by the tenant in their own Entra ID tenant and pasted here; Flexprice never
// creates or registers anything on Azure. Stored encrypted so a single decrypt at report time yields
// everything the client_credentials token request needs.
type AzureMarketplaceConnectionSecrets struct {
	TenantID     string `json:"tenant_id"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// Validate validates the Azure Marketplace connection secrets
func (a *AzureMarketplaceConnectionSecrets) Validate() error {
	if a.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("Azure Marketplace tenant_id is required").
			Mark(ierr.ErrValidation)
	}
	if a.ClientID == "" {
		return ierr.NewError("client_id is required").
			WithHint("Azure Marketplace client_id is required").
			Mark(ierr.ErrValidation)
	}
	if a.ClientSecret == "" {
		return ierr.NewError("client_secret is required").
			WithHint("Azure Marketplace client_secret is required").
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
	Stripe           *StripeConnectionMetadata          `json:"stripe,omitempty"`
	S3               *S3ConnectionMetadata              `json:"s3,omitempty"`
	HubSpot          *HubSpotConnectionMetadata         `json:"hubspot,omitempty"`
	Razorpay         *RazorpayConnectionMetadata        `json:"razorpay,omitempty"`
	Chargebee        *ChargebeeConnectionMetadata       `json:"chargebee,omitempty"`
	QuickBooks       *QuickBooksConnectionMetadata      `json:"quickbooks,omitempty"`
	Nomod            *NomodConnectionMetadata           `json:"nomod,omitempty"`
	Moyasar          *MoyasarConnectionMetadata         `json:"moyasar,omitempty"`
	Paddle           *PaddleConnectionMetadata          `json:"paddle,omitempty"`
	ZohoBooks        *ZohoBooksConnectionMetadata       `json:"zoho_books,omitempty"`
	Whop             *WhopConnectionMetadata            `json:"whop,omitempty"`
	Tabs             *TabsConnectionMetadata            `json:"tabs,omitempty"`
	AWSMarketplace   *AWSMarketplaceConnectionSecrets   `json:"aws_marketplace,omitempty"`
	GCPMarketplace   *GCPMarketplaceConnectionSecrets   `json:"gcp_marketplace,omitempty"`
	AzureMarketplace *AzureMarketplaceConnectionSecrets `json:"azure_marketplace,omitempty"`
	Generic          *GenericConnectionMetadata         `json:"generic,omitempty"`
	Settings         *ConnectionSettings                `json:"settings,omitempty"`
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
	case SecretProviderMoyasar:
		if c.Moyasar == nil {
			return ierr.NewError("moyasar metadata is required").
				WithHint("Moyasar metadata is required for moyasar provider").
				Mark(ierr.ErrValidation)
		}
		return c.Moyasar.Validate()
	case SecretProviderPaddle:
		if c.Paddle == nil {
			return ierr.NewError("paddle metadata is required").
				WithHint("Paddle metadata is required for paddle provider").
				Mark(ierr.ErrValidation)
		}
		return c.Paddle.Validate()
	case SecretProviderZohoBooks:
		if c.ZohoBooks == nil {
			return ierr.NewError("zoho_books metadata is required").
				WithHint("Zoho Books metadata is required for zoho_books provider").
				Mark(ierr.ErrValidation)
		}
		return c.ZohoBooks.Validate()
	case SecretProviderWhop:
		if c.Whop == nil {
			return ierr.NewError("whop metadata is required").
				WithHint("Whop metadata is required for whop provider").
				Mark(ierr.ErrValidation)
		}
		return c.Whop.Validate()
	case SecretProviderTabs:
		if c.Tabs == nil {
			return ierr.NewError("tabs metadata is required").
				WithHint("Tabs metadata is required for tabs provider").
				Mark(ierr.ErrValidation)
		}
		return c.Tabs.Validate()
	case SecretProviderAWSMarketplace:
		if c.AWSMarketplace == nil {
			return ierr.NewError("aws_marketplace metadata is required").
				WithHint("AWS Marketplace metadata is required for aws_marketplace provider").
				Mark(ierr.ErrValidation)
		}
		return c.AWSMarketplace.Validate()
	case SecretProviderGCPMarketplace:
		if c.GCPMarketplace == nil {
			return ierr.NewError("gcp_marketplace metadata is required").
				WithHint("GCP Marketplace metadata is required for gcp_marketplace provider").
				Mark(ierr.ErrValidation)
		}
		return c.GCPMarketplace.Validate()
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
