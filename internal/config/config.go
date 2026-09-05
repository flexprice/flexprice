package config

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/Shopify/sarama"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

type Configuration struct {
	Deployment DeploymentConfig `validate:"required"`
	Server     ServerConfig     `validate:"required"`
	Auth       AuthConfig       `validate:"required"`
	Kafka      KafkaConfig      `validate:"required"`
	// KafkaSecondary is the optional second Kafka cluster the source event publisher also
	// writes to during the AWS→GCP migration (the "other" cloud's cluster). When set
	// (non-nil) every event is published to it in addition to the local `kafka` cluster;
	// when nil, publishing is single-cluster. The `kafka` block is this deployment's own
	// local cluster — consumed AND always written. See infrastructure/docs/GCP-CUTOVER-STEPWISE.md.
	KafkaSecondary         *KafkaConfig                 `mapstructure:"kafka_secondary" validate:"omitempty"`
	ClickHouse             ClickHouseConfig             `validate:"required"`
	Logging                LoggingConfig                `validate:"required"`
	Postgres               PostgresConfig               `validate:"required"`
	Otel                   OtelConfig                   `validate:"omitempty"`
	Pyroscope              PyroscopeConfig              `validate:"required"`
	Event                  EventConfig                  `validate:"required"`
	DynamoDB               DynamoDBConfig               `validate:"required"`
	Temporal               TemporalConfig               `validate:"required"`
	Webhook                Webhook                      `validate:"omitempty"`
	Secrets                SecretsConfig                `validate:"required"`
	Billing                BillingConfig                `validate:"omitempty"`
	S3                     S3Config                     `validate:"required"`
	Storage                StorageConfig                `mapstructure:"storage" validate:"omitempty"`
	GCS                    GCSConfig                    `mapstructure:"gcs" validate:"omitempty"`
	FlexpriceS3Exports     FlexpriceS3ExportsConfig     `mapstructure:"flexprice_s3_exports" validate:"omitempty"`
	FlexpriceGCSExports    FlexpriceGCSExportsConfig    `mapstructure:"flexprice_gcs_exports" validate:"omitempty"`
	FlexpriceS3Imports     FlexpriceS3ImportsConfig     `mapstructure:"flexprice_s3_imports" validate:"omitempty"`
	Marketplace            MarketplaceConfig            `mapstructure:"marketplace" validate:"omitempty"`
	Cache                  CacheConfig                  `validate:"required"`
	EventProcessing        EventProcessingConfig        `mapstructure:"event_processing" validate:"required"`
	EventProcessingLazy    EventProcessingLazyConfig    `mapstructure:"event_processing_lazy" validate:"required"`
	EventProcessingReplay  EventProcessingReplayConfig  `mapstructure:"event_processing_replay" validate:"required"`
	MeterUsageTracking     MeterUsageTrackingConfig     `mapstructure:"meter_usage_tracking" validate:"required"`
	MeterUsageTrackingLazy MeterUsageTrackingLazyConfig `mapstructure:"meter_usage_tracking_lazy" validate:"required"`
	BulkEventConsumption   BulkEventConsumptionConfig   `mapstructure:"bulk_event_consumption" validate:"required"`
	BulkMeterUsageTracking BulkMeterUsageTrackingConfig `mapstructure:"bulk_meter_usage_tracking" validate:"required"`
	UsageAlerts            UsageAlertsConfig            `mapstructure:"usage_alerts" validate:"omitempty"`
	EnvAccess              EnvAccessConfig              `mapstructure:"env_access" json:"env_access" validate:"omitempty"`
	Email                  EmailConfig                  `mapstructure:"email" validate:"required"`
	RBAC                   RBACConfig                   `mapstructure:"rbac" validate:"omitempty"`
	OAuth                  OAuthConfig                  `mapstructure:"oauth" validate:"required"`
	WalletBalanceAlert     WalletBalanceAlertConfig     `mapstructure:"wallet_balance_alert" validate:"required"`
	CustomerPortal         CustomerPortalConfig         `mapstructure:"customer_portal" validate:"required"`
	Checkout               CheckoutConfig               `mapstructure:"checkout" validate:"omitempty"`
	Redis                  RedisConfig                  `mapstructure:"redis" validate:"required"`
	RawEventsReprocessing  RawEventsReprocessingConfig  `mapstructure:"raw_events_reprocessing" validate:"required"`
	RawEventConsumption    RawEventConsumptionConfig    `mapstructure:"raw_event_consumption" validate:"required"`
	IntegrationEvents      IntegrationEventsConfig      `mapstructure:"integration_events" validate:"omitempty"`
	OnboardingEvents       OnboardingEventsConfig       `mapstructure:"onboarding_events" validate:"omitempty"`
	WebhookRetryJob        WebhookRetryJobConfig        `mapstructure:"webhook_retry_job" validate:"omitempty"`
	Gemini                 GeminiConfig                 `mapstructure:"gemini" validate:"omitempty"`
	Whop                   WhopConfig                   `mapstructure:"whop" validate:"omitempty"`
	Onboarding             OnboardingConfig             `mapstructure:"onboarding" validate:"omitempty"`
	ChatSupport            ChatSupportConfig            `mapstructure:"chat_support" validate:"omitempty"`
	Analytics              AnalyticsConfig              `mapstructure:"analytics" validate:"omitempty"`
}

// AnalyticsConfig gates the fire-and-forget analytics meter_usage feed.
type AnalyticsConfig struct {
	Enabled             bool   `mapstructure:"enabled" default:"false"`
	MeterUsageSinkTopic string `mapstructure:"meter_usage_sink_topic"`
}

type ChatSupportConfig struct {
	AppID          string `mapstructure:"app_id"`
	IdentitySecret string `mapstructure:"identity_secret"`
}

type OnboardingConfig struct {
	DefaultTenantName string `mapstructure:"default_tenant_name" validate:"omitempty" default:"Flexprice"`
}

// WhopConfig holds Whop integration settings (non-secret, static config)
type WhopConfig struct {
	// BaseURL overrides the default Whop API URL. Leave empty for production.
	// Set to "https://sandbox-api.whop.com" to use the Whop sandbox environment.
	BaseURL string `mapstructure:"base_url" validate:"omitempty"`
}

// GeminiConfig holds Google Gemini API settings for server-side AI pricing parse (portal).
type GeminiConfig struct {
	APIKey string `mapstructure:"api_key" validate:"omitempty"`
	Model  string `mapstructure:"model" validate:"omitempty"`
}

type CacheConfig struct {
	Enabled  bool                `mapstructure:"enabled" validate:"required"`
	InMemory InMemoryCacheConfig `mapstructure:"inmemory" validate:"required"`
	Redis    RedisCacheConfig    `mapstructure:"redis" validate:"required"`
}

type InMemoryCacheConfig struct {
	Enabled bool `mapstructure:"enabled" default:"false"`
}

type RedisCacheConfig struct {
	Enabled bool `mapstructure:"enabled" default:"false"`
}

type S3Config struct {
	Enabled             bool         `mapstructure:"enabled" validate:"required"`
	Region              string       `mapstructure:"region" validate:"required"`
	InvoiceBucketConfig BucketConfig `mapstructure:"invoice" validate:"required"`
}

type BucketConfig struct {
	Bucket                string `mapstructure:"bucket" validate:"required"`
	PresignExpiryDuration string `mapstructure:"presign_expiry_duration" validate:"required"`
	KeyPrefix             string `mapstructure:"key_prefix" validate:"omitempty"`
}

// Credential sources for FlexpriceS3ExportsConfig. Exactly one is active per
// deployment. "ambient" covers every AWS-default-chain shape (IRSA, Pod Identity,
// ECS task role, EC2 instance profile) with no per-mechanism value.
const (
	CredentialSourceStatic     = "static"
	CredentialSourceAmbient    = "ambient"
	CredentialSourceFederation = "federation" // GCP-to-AWS OIDC
)

type FlexpriceS3ExportsConfig struct {
	Bucket             string `mapstructure:"bucket" validate:"required"`
	Region             string `mapstructure:"region" validate:"required"`
	AWSAccessKeyID     string `mapstructure:"aws_access_key_id" validate:"omitempty"`
	AWSSecretAccessKey string `mapstructure:"aws_secret_access_key" validate:"omitempty"`
	AWSSessionToken    string `mapstructure:"aws_session_token,omitempty"`
	FederationRoleARN  string `mapstructure:"federation_role_arn,omitempty"`
	FederationEnabled  bool   `mapstructure:"federation_enabled" default:"false"`
	// CredentialSource selects how AWS credentials are obtained; empty is derived
	// by ResolvedCredentialSource for backward compatibility.
	CredentialSource string `mapstructure:"credential_source" validate:"omitempty,oneof=static ambient federation"`
}

// ResolvedCredentialSource derives the effective source so existing deployments
// need no config change: explicit CredentialSource wins; else federation (flag or
// legacy FederationRoleARN); else static (both keys present); else ambient.
func (c *FlexpriceS3ExportsConfig) ResolvedCredentialSource() string {
	if c.CredentialSource != "" {
		return c.CredentialSource
	}
	if c.FederationEnabled || c.FederationRoleARN != "" {
		return CredentialSourceFederation
	}
	if c.AWSAccessKeyID != "" && c.AWSSecretAccessKey != "" {
		return CredentialSourceStatic
	}
	return CredentialSourceAmbient
}

// Validate enforces the resolved credential source's requirements. Explicit
// "ambient" requires no credentials (that's the point of the default chain); an
// empty source with nothing configured still errors, preserving historic behavior
// rather than silently resolving to ambient. Consumers call this explicitly —
// it is not reached from Configuration.Validate() (dead on the boot path).
func (c *FlexpriceS3ExportsConfig) Validate() error {
	if c.Bucket == "" {
		return ierr.NewError("flexprice S3 exports bucket is not configured").
			WithHint("Set flexprice_s3_exports.bucket (FLEXPRICE_FLEXPRICE_S3_EXPORTS_BUCKET)").
			Mark(ierr.ErrValidation)
	}
	if c.Region == "" {
		return ierr.NewError("flexprice S3 exports region is not configured").
			WithHint("Set flexprice_s3_exports.region (FLEXPRICE_FLEXPRICE_S3_EXPORTS_REGION)").
			Mark(ierr.ErrValidation)
	}

	hasStaticKeys := c.AWSAccessKeyID != "" && c.AWSSecretAccessKey != ""
	hasFederation := c.FederationRoleARN != ""

	switch c.ResolvedCredentialSource() {
	case CredentialSourceFederation:
		if !hasFederation {
			return ierr.NewError("federation credentials are selected but federation_role_arn is not set").
				WithHint("Set flexprice_s3_exports.federation_role_arn, or change credential_source/federation_enabled to another source").
				Mark(ierr.ErrValidation)
		}
	case CredentialSourceAmbient:
		// Derived ambient (nothing configured) still errors; only explicit ambient
		// skips the credential requirement.
		if c.CredentialSource != CredentialSourceAmbient {
			return ierr.NewError("no credential source configured for flexprice_s3_exports").
				WithHint("Set either aws_access_key_id/aws_secret_access_key, federation_role_arn, or credential_source: \"ambient\" to explicitly use the AWS default credential chain").
				Mark(ierr.ErrValidation)
		}
	case CredentialSourceStatic:
		if !hasStaticKeys {
			return ierr.NewError("no credential source configured for flexprice_s3_exports").
				WithHint("Set both aws_access_key_id and aws_secret_access_key (FLEXPRICE_FLEXPRICE_S3_EXPORTS_AWS_ACCESS_KEY_ID / FLEXPRICE_FLEXPRICE_S3_EXPORTS_AWS_SECRET_ACCESS_KEY), or set credential_source to \"ambient\" or \"federation\"").
				Mark(ierr.ErrValidation)
		}
	default:
		// A typo'd credential_source reaches here (the boot path skips the oneof
		// tag); reject rather than fall through to ambient.
		return ierr.NewError("invalid flexprice_s3_exports.credential_source").
			WithHint("credential_source must be one of \"static\", \"ambient\", or \"federation\"").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// StorageConfig lets deployments explicitly pin the platform storage backend,
// overriding CloudDetector's auto-detection. Empty Provider means "auto-detect".
type StorageConfig struct {
	Provider string `mapstructure:"provider" validate:"omitempty,oneof=s3 gcs"`
}

type GCSConfig struct {
	Enabled             bool         `mapstructure:"enabled" default:"false"`
	InvoiceBucketConfig BucketConfig `mapstructure:"invoice" validate:"omitempty"`
	// SignerServiceAccountEmail signs presigned GET URLs (Workload Identity can't
	// self-sign; needs roles/iam.serviceAccountTokenCreator). The resolver rejects
	// an empty value under GCS.
	SignerServiceAccountEmail string `mapstructure:"signer_service_account_email" validate:"omitempty"`
}

// FlexpriceGCSExportsConfig is the GCS counterpart to FlexpriceS3ExportsConfig:
// the Flexprice-owned bucket that Flexprice-managed export connections write to
// when the deployment runs on GCP.
//
// No SA-key field by design: on GCP Flexprice uses Workload Identity, and
// iam.disableServiceAccountKeyCreation commonly blocks exported keys. Customer
// BYO GCS connections still carry a key on the connection row.
type FlexpriceGCSExportsConfig struct {
	Bucket string `mapstructure:"bucket" validate:"omitempty"`
	// See GCSConfig.SignerServiceAccountEmail; same signer requirement for exports.
	SignerServiceAccountEmail string `mapstructure:"signer_service_account_email" validate:"omitempty"`
}

// Validate ensures the section is usable when a Flexprice-managed GCS export
// connection actually consumes it.
func (c *FlexpriceGCSExportsConfig) Validate() error {
	if c.Bucket == "" {
		return ierr.NewError("flexprice GCS exports bucket is not configured").
			WithHint("Set flexprice_gcs_exports.bucket (FLEXPRICE_FLEXPRICE_GCS_EXPORTS_BUCKET) to the Flexprice-owned GCS bucket").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// FlexpriceS3ImportsConfig points at the Flexprice-managed bucket that CSV Box
// (and any other trusted upstream uploader) writes into. The import API resolves
// an upload id to s3://<Bucket>/<KeyPrefix><upload_id>.csv and presigns a GET
// against those credentials — the caller never supplies a URL.
type FlexpriceS3ImportsConfig struct {
	Enabled            bool   `mapstructure:"enabled"`
	Bucket             string `mapstructure:"bucket" validate:"required_if=Enabled true"`
	Region             string `mapstructure:"region" validate:"required_if=Enabled true"`
	KeyPrefix          string `mapstructure:"key_prefix"`
	AWSAccessKeyID     string `mapstructure:"aws_access_key_id" validate:"required_if=Enabled true"`
	AWSSecretAccessKey string `mapstructure:"aws_secret_access_key" validate:"required_if=Enabled true"`
	AWSSessionToken    string `mapstructure:"aws_session_token,omitempty"`
}

// MarketplaceConfig groups Flexprice's own credentials/identity for each marketplace it reports
// usage to. Azure would be added as a further sibling field.
type MarketplaceConfig struct {
	AWS AWSMarketplaceConfig `mapstructure:"aws" validate:"omitempty"`
	GCP GCPMarketplaceConfig `mapstructure:"gcp" validate:"omitempty"`
}

// AWSMarketplaceConfig holds Flexprice's OWN AWS identity — the caller that assumes each tenant's
// role. sts:AssumeRole is an authenticated API: the tenant's trust policy names this principal, so
// these credentials are what signs the AssumeRole request. They are unrelated to the tenant's
// role_arn/external_id, which are the assume *target*, stored per-connection.
//
// These are set explicitly rather than resolved from the ambient AWS credential chain: the chain
// ends at the EC2 instance-metadata endpoint, which is unreachable off EC2 and stalls for seconds
// before failing — turning connection creation into a hang on any non-EC2 host.
//
// SessionToken is only set when the credentials are temporary (an ASIA... key from STS/SSO). A
// long-lived AKIA... IAM user key has no session token, and sending a non-empty one with it makes
// AWS reject the request.
type AWSMarketplaceConfig struct {
	Region          string `mapstructure:"region" validate:"omitempty"`
	AccessKeyID     string `mapstructure:"access_key_id" validate:"omitempty"`
	SecretAccessKey string `mapstructure:"secret_access_key" validate:"omitempty"`
	SessionToken    string `mapstructure:"session_token" validate:"omitempty"`
}

// GCPMarketplaceConfig holds the two values Flexprice renders into the tenant-facing Workload
// Identity Federation setup script (design doc FLE-981 §5.3, step 2's --account-id and
// --attribute-condition). Unlike AWSMarketplaceConfig, these are not credentials: authenticating to
// GCP happens ambiently, via the AWS identity attached to the worker process's own runtime
// environment (the credentials JSON a tenant generates hard-codes a real EC2/ECS instance-metadata
// endpoint for Google's client library to fetch that identity from — see the GCP client package
// doc comment for the full explanation). This is a different identity from AWSMarketplaceConfig's
// static caller credentials above (which sign AssumeRole calls for AWS Marketplace) — it names the
// ambient instance role the worker actually runs as, purely so the WIF setup script we hand tenants
// trusts the right principal. FlexpriceAWSAccountID/RoleName only need to be *correct*, i.e. matching
// that role.
type GCPMarketplaceConfig struct {
	FlexpriceAWSAccountID string `mapstructure:"flexprice_aws_account_id" validate:"omitempty"`
	FlexpriceAWSRoleName  string `mapstructure:"flexprice_aws_role_name" validate:"omitempty"`
}

type DeploymentConfig struct {
	Mode types.RunMode `mapstructure:"mode" validate:"required"`
}

type ServerConfig struct {
	Address string `mapstructure:"address" validate:"required"`
}

type AuthConfig struct {
	Provider types.AuthProvider `mapstructure:"provider" validate:"required"`
	Secret   string             `mapstructure:"secret" validate:"required"`
	SAML     SAMLConfig         `mapstructure:"saml"`
	Supabase SupabaseConfig     `mapstructure:"supabase"`
	APIKey   APIKeyConfig       `mapstructure:"api_key"`
}

// SAMLConfig holds deployment-level SAML settings. Per-tenant identity provider
// details live in the tenant's saml_config setting, not here.
type SAMLConfig struct {
	// Enabled is the deployment-wide switch for the whole SAML feature. When it
	// is off the SAML routes are not mounted at all and no tenant may store a
	// configuration, so a deployment that does not offer SSO exposes none of it
	// and cannot accumulate configurations that silently do nothing.
	//
	// Defaults to off: SSO is opt-in per deployment.
	Enabled bool `mapstructure:"enabled"`

	// BaseURL is the externally reachable origin of this deployment — scheme and
	// host only; the SAML paths are built onto it. It is deployment-level rather
	// than per-tenant because a deployment has exactly one API origin, and
	// because it must not come from the inbound request: it is signed into the
	// AuthnRequest as the ACS URL and checked again when the assertion arrives,
	// so a request-derived origin would let a caller controlling the Host header
	// have assertions delivered to a host of their choosing.
	//
	// Nothing tenant-specific lives here. The tenant appears in the path, which
	// the SP builds, so one origin serves every tenant.
	BaseURL string `mapstructure:"base_url"`

	// DashboardURL receives the browser redirect after a successful assertion,
	// carrying the minted token. Deployment-level for the same reason: it names
	// this deployment's own frontend, and taking it from the request would make
	// the callback an open redirect.
	DashboardURL string `mapstructure:"dashboard_url"`
}

// validate refuses a SAML deployment that cannot serve a working login.
//
// Only enforced when the feature is on, so a deployment that does not offer SSO
// is unaffected by any of it.
//
// Both URLs must be absolute: BaseURL builds the entity ID and ACS URL published
// in our metadata, and a relative value produces endpoints an identity provider
// cannot call back. Both must be https away from loopback: the assertion and the
// minted token both travel through the browser, so plaintext exposes them in
// transit. Loopback is exempt because it never leaves the machine and is how
// this is developed against a local identity provider.
func (c SAMLConfig) validate() error {
	if !c.Enabled {
		return nil
	}

	for _, field := range []struct{ name, raw string }{
		{"auth.saml.base_url", c.BaseURL},
		{"auth.saml.dashboard_url", c.DashboardURL},
	} {
		value := strings.TrimSpace(field.raw)
		if value == "" {
			return fmt.Errorf("%s is required when auth.saml.enabled is true", field.name)
		}

		u, err := url.Parse(value)
		if err != nil || u.Host == "" {
			return fmt.Errorf("%s must be an absolute URL (got %q)", field.name, field.raw)
		}
		if u.Scheme != "https" && !isLoopbackHost(u.Hostname()) {
			return fmt.Errorf("%s must use https (plain http is allowed only for localhost) (got %q)", field.name, field.raw)
		}
	}
	return nil
}

// validateSAMLDependencies refuses to start a SAML deployment without Redis.
//
// The AuthnRequest IDs that make an assertion single-use are held in Redis. The
// redirect that starts a login and the callback that finishes it are separate
// requests, and a load balancer may route them to different replicas, so
// process-local state fails roughly (N-1)/N of logins on an N-replica
// deployment — at random, and looking like an identity provider fault rather
// than a Flexprice one.
//
// Failing at boot is much kinder than that: a deployment either has the state
// store SAML needs, or it does not offer SAML.
func (c Configuration) validateSAMLDependencies() error {
	if !c.Auth.SAML.Enabled {
		return nil
	}
	if !c.Cache.Enabled || !c.Cache.Redis.Enabled {
		return fmt.Errorf("auth.saml.enabled requires cache.enabled and cache.redis.enabled: " +
			"SAML keeps outstanding login requests in Redis so a login started on one replica " +
			"can be completed on another")
	}

	// auth.secret is the HMAC key the SSO token is signed with. An empty key
	// still produces a verifiable signature, so a deployment that boots without
	// one accepts a token anybody can mint — the forger names any user in any
	// tenant, and the middleware then loads that user and grants their roles.
	//
	// The warn-only validateSecrets does not cover this: it checks auth.secret
	// only under the Flexprice provider, so a Supabase deployment offering SSO
	// with no secret set started silently. Hard-failing is safe here for the
	// same reason as the checks above — it applies only when SSO is switched
	// on, so it cannot take down a deployment that does not offer it.
	if strings.TrimSpace(c.Auth.Secret) == "" {
		return fmt.Errorf("auth.saml.enabled requires a non-empty auth.secret (FLEXPRICE_AUTH_SECRET): " +
			"it signs the SSO token, and an empty key lets anyone mint one naming any user")
	}
	return nil
}

// isLoopbackHost reports whether a host never leaves this machine.
func isLoopbackHost(host string) bool {
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

type SupabaseConfig struct {
	BaseURL    string `mapstructure:"base_url"`
	ServiceKey string `mapstructure:"service_key"`
}

type KafkaConfig struct {
	Brokers       []string `mapstructure:"brokers" validate:"required"`
	ConsumerGroup string   `mapstructure:"consumer_group" validate:"required"`
	Topic         string   `mapstructure:"topic" validate:"required"`
	TopicLazy     string   `mapstructure:"topic_lazy" validate:"required"`
	// TopicBulk is this cluster's batched-ingest topic. Per-cluster because a shared prod
	// cluster renames topics (FLEXPRICE_KAFKA_TOPICS).
	TopicBulk string `mapstructure:"topic_bulk"`
	// Batching bounds for PublishBatch; a batch closes at whichever is hit first.
	// BulkMaxBatchBytes must stay under the topic's max.message.bytes (1 MB default on MSK).
	// Read from the LOCAL cluster only: both clusters must receive byte-identical payloads to
	// stay dedup-identical, so these must not diverge per cluster.
	BulkMaxBatchSize  int `mapstructure:"bulk_max_batch_size" default:"200"`
	BulkMaxBatchBytes int `mapstructure:"bulk_max_batch_bytes" default:"524288"`
	// TopicDLQ is the global fallback dead-letter Kafka topic used by handlers that
	// do not define their own per-consumer-group topic_dlq. Empty disables DLQ for
	// those handlers.
	TopicDLQ string `mapstructure:"topic_dlq" default:""`
	// OffsetRetention overrides the broker's offsets.retention.minutes for this client.
	// Zero leaves retention to the broker.
	OffsetRetention time.Duration `mapstructure:"offset_retention"`
	TLS             bool          `mapstructure:"tls"` // set to true if using 9094 port else can set to false
	// TLSCACertFile is the path to a PEM-encoded CA bundle used to verify the
	// broker certificate. Empty (the default) means the OS trust store is used,
	// which is correct for brokers with publicly-trusted certs (MSK, Confluent
	// Cloud). Set it only when the broker presents a private/self-signed CA.
	// The CA is applied to the Kafka client alone, so it does not affect other
	// outbound TLS in the process (Stripe, GCP, webhooks).
	//
	// Must be PEM. JKS/PKCS12 truststores are not supported — sarama and Go's
	// crypto/x509 read PEM only. Convert with:
	//
	//	keytool -exportcert -alias <alias> -keystore truststore.jks -rfc -file ca.pem
	TLSCACertFile string `mapstructure:"tls_ca_cert_file"`
	// TLSServerName overrides the hostname the broker certificate is verified
	// against. Empty (the default) verifies against the dial address, i.e. the
	// broker's advertised listener, which is correct almost always. Set it only
	// when a private CA issues a certificate whose SAN does not match that name
	// — a broker behind an SNI-mismatched load balancer, typically.
	TLSServerName string               `mapstructure:"tls_server_name"`
	UseSASL       bool                 `mapstructure:"use_sasl"`
	SASLMechanism sarama.SASLMechanism `mapstructure:"sasl_mechanism"`
	SASLUser      string               `mapstructure:"sasl_user"`
	SASLPassword  string               `mapstructure:"sasl_password"`
	// SASLOAuthScopes is consulted only when SASLMechanism is OAUTHBEARER.
	// Empty defaults to ["https://www.googleapis.com/auth/cloud-platform"],
	// which is what GCP Managed Kafka requires.
	SASLOAuthScopes        []string `mapstructure:"sasl_oauth_scopes"`
	ClientID               string   `mapstructure:"client_id" validate:"required"`
	RouteTenantsOnLazyMode []string `mapstructure:"route_tenants_on_lazy_mode" validate:"omitempty"`
	// TopicsDefaults/Topics describe the full desired topic set for `migrate kafka`
	// (partition counts, replication factor, retention). Consumed only by
	// cmd/migrate (kafka subcommand), not by the server/consumer/worker processes. A deploy's
	// FLEXPRICE_KAFKA_TOPICS env var (JSON), when set, FULLY REPLACES this block
	// (no merge) — see internal/kafka/topicspec.
	TopicsDefaults KafkaTopicsDefaults       `mapstructure:"topics_defaults"`
	Topics         map[string]KafkaTopicSpec `mapstructure:"topics"`
}

type KafkaTopicsDefaults struct {
	ReplicationFactor int16 `mapstructure:"replication_factor"`
	RetentionMs       int64 `mapstructure:"retention_ms"`
}

type KafkaTopicSpec struct {
	Partitions        int    `mapstructure:"partitions"`
	ReplicationFactor *int16 `mapstructure:"replication_factor"`
	RetentionMs       *int64 `mapstructure:"retention_ms"`
}

type ClickHouseConfig struct {
	// MaxOpenConns caps concurrent ClickHouse queries per PROCESS, so insert throughput is
	// bounded by (MaxOpenConns / insert latency) per pod/task no matter how many run. Left
	// at 0 the driver applies MaxIdleConns+5 = 10, which silently caps a consumer fleet:
	// 40 tasks x 10 conns / 685ms inserts = ~580 events/s. Exhaustion surfaces as
	// clickhouse-go ErrAcquireConnTimeout ("acquire conn timeout") raised client-side —
	// the query never reaches the server, so ClickHouse logs nothing. Size it against the
	// server's spare admission (system.metrics Query vs max_concurrent_queries): raising it
	// helps only when ClickHouse has headroom, and hurts when it is already saturated.
	MaxOpenConns int `mapstructure:"max_open_conns"`
	MaxIdleConns int `mapstructure:"max_idle_conns"`
	// DialTimeout doubles as the pool-acquire deadline inside clickhouse-go
	// (clickhouse.go acquire() waits on a semaphore of MaxOpenConns slots for DialTimeout),
	// so it cannot be tuned for pool pressure without also changing dial failover — see
	// the ConnOpenInOrder note in GetClientOptions. Prefer raising MaxOpenConns instead.
	DialTimeout time.Duration `mapstructure:"dial_timeout"`
	ReadTimeout time.Duration `mapstructure:"read_timeout"`
	Address     string        `mapstructure:"address" validate:"required"`
	TLS         bool          `mapstructure:"tls"`
	// TLSSkipVerify disables server certificate and hostname verification on the
	// TLS connection. It only takes effect when TLS is true. Intended for dev
	// environments whose ClickHouse serves a self-signed certificate (equivalent to
	// SSL=true with SSL_MODE=NONE); it removes MITM protection, so leave it false
	// everywhere else and trust the CA instead.
	TLSSkipVerify bool `mapstructure:"tls_skip_verify"`
	// Protocol selects the ClickHouse wire protocol: "native" (default) or "http".
	// The two protocols listen on different ports and are not interchangeable — a
	// native client pointed at an HTTP port fails with
	// "[handshake] unexpected packet [72] from server" (72 is 'H' of "HTTP/1.1").
	// Ports: native 9000 / 9440 (TLS), http 8123 / 8443 (TLS). Set this to "http"
	// when ClickHouse is only reachable through an HTTP(S) endpoint, e.g. behind a
	// TLS-terminating load balancer that exposes 8443. Empty means native so
	// existing deployments keep their current behaviour.
	Protocol       string `mapstructure:"protocol" validate:"omitempty,oneof=native http"`
	Username       string `mapstructure:"username" validate:"required"`
	Password       string `mapstructure:"password" validate:"required"`
	Database       string `mapstructure:"database" validate:"required"`
	MaxMemoryUsage int64  `mapstructure:"max_memory_usage" validate:"required"`
}

type LoggingConfig struct {
	Level   types.LogLevel `mapstructure:"level" validate:"required"`
	DBLevel types.LogLevel `mapstructure:"db_level" validate:"required"`

	// Service identity fields added to every log line
	ServiceName string `mapstructure:"service_name" validate:"omitempty"`
	Environment string `mapstructure:"environment" validate:"omitempty"`
	Region      string `mapstructure:"region" validate:"omitempty"`

	// OpenTelemetry log export configuration (works with SigNoz, Grafana, Datadog, etc.)
	OtelEnabled    bool   `mapstructure:"otel_enabled" default:"false"`
	OtelEndpoint   string `mapstructure:"otel_endpoint" validate:"omitempty"`    // e.g. <host>:<port>
	OtelInsecure   bool   `mapstructure:"otel_insecure" default:"false"`         // set true for local collector without TLS
	OtelProtocol   string `mapstructure:"otel_protocol" default:"grpc"`          // grpc (default) or http
	OtelAuthHeader string `mapstructure:"otel_auth_header" validate:"omitempty"` // header name
	OtelAuthValue  string `mapstructure:"otel_auth_value" validate:"omitempty"`  // header value / token
	OtelDebug      bool   `mapstructure:"otel_debug" default:"false"`            // use synchronous SimpleProcessor and verbose stderr output
}

type PostgresConfig struct {
	Host                   string `mapstructure:"host" validate:"required"`
	Port                   int    `mapstructure:"port" validate:"required"`
	User                   string `mapstructure:"user" validate:"required"`
	Password               string `mapstructure:"password" validate:"required"`
	DBName                 string `mapstructure:"dbname" validate:"required"`
	SSLMode                string `mapstructure:"sslmode" validate:"required"`
	MaxOpenConns           int    `mapstructure:"max_open_conns" default:"10"`
	MaxIdleConns           int    `mapstructure:"max_idle_conns" default:"5"`
	ConnMaxLifetimeMinutes int    `mapstructure:"conn_max_lifetime_minutes" default:"60"`

	// Reader endpoint configuration for read replicas
	ReaderHost string `mapstructure:"reader_host"`
	ReaderPort int    `mapstructure:"reader_port"`
}

type APIKeyConfig struct {
	Header string                   `mapstructure:"header" validate:"required" default:"x-api-key"`
	Keys   map[string]APIKeyDetails `mapstructure:"keys"` // map of hashed API key to its details
}

type APIKeyDetails struct {
	TenantID string `mapstructure:"tenant_id" json:"tenant_id" validate:"required"`
	UserID   string `mapstructure:"user_id" json:"user_id" validate:"required"`
	Name     string `mapstructure:"name" json:"name" validate:"required"`      // description of what this key is for
	IsActive bool   `mapstructure:"is_active" json:"is_active" default:"true"` // whether this key is active
}

// OtelConfig is the unified OTLP exporter configuration. Each signal (traces,
// logs) can target a different backend with its own headers — useful when you
// want, for example, logs to SigNoz and traces to Sentry. Top-level fields act
// as defaults; per-signal fields override when non-empty.
type OtelConfig struct {
	Enabled     bool              `mapstructure:"enabled" default:"false"`
	ServiceName string            `mapstructure:"service_name" validate:"omitempty"` // falls back to logging.service_name, then deployment.mode
	Protocol    string            `mapstructure:"protocol" default:"grpc"`           // grpc (default) or http
	Insecure    bool              `mapstructure:"insecure" default:"false"`          // true for local collector without TLS
	Headers     map[string]string `mapstructure:"headers" validate:"omitempty"`      // applied to every signal unless that signal supplies its own non-empty map

	Traces  OtelTracesConfig  `mapstructure:"traces"`
	Logs    OtelLogsConfig    `mapstructure:"logs"`
	Metrics OtelMetricsConfig `mapstructure:"metrics"`
}

// OtelTracesConfig configures OTLP span export.
//
// For backends that need a single auth header (Sentry's OTLP gateway, SigNoz
// Cloud, Grafana Cloud, etc.) prefer the AuthHeader/AuthValue pair — these are
// env-var friendly. Use Headers when you need to send more than one header.
// AuthHeader/AuthValue are merged into the resolved header set at startup.
type OtelTracesConfig struct {
	Enabled             bool              `mapstructure:"enabled" default:"false"`
	Endpoint            string            `mapstructure:"endpoint" validate:"omitempty"` // host:port (grpc) or full URL (http)
	Protocol            string            `mapstructure:"protocol" validate:"omitempty"` // overrides otel.protocol when non-empty
	AuthHeader          string            `mapstructure:"auth_header" validate:"omitempty"`
	AuthValue           string            `mapstructure:"auth_value" validate:"omitempty"`
	Headers             map[string]string `mapstructure:"headers" validate:"omitempty"`          // overrides otel.headers when non-empty
	SampleRate          float64           `mapstructure:"sample_rate" default:"1.0"`             // 0.0 - 1.0
	StorageSpansEnabled bool              `mapstructure:"storage_spans_enabled" default:"false"` // master switch for ALL DB/ClickHouse/cache child spans (can be noisy)
	// Per-trace throttle on storage spans (0.0-1.0), applied when StorageSpansEnabled
	// is true. Independent of SampleRate (which thins whole traces incl. server spans);
	// this thins only the DB/cache/ClickHouse fan-out. Default 0.2; set 1.0 to debug.
	StorageSpansSampleRate float64 `mapstructure:"storage_spans_sample_rate" default:"0.2"`
	// Cache spans are the noisiest fan-out (fire on every get/set/delete on hot
	// paths), so they get a per-type opt-in on top of StorageSpansEnabled.
	// Both default false, and both require StorageSpansEnabled=true to emit —
	// StorageSpansEnabled is the master kill switch for all storage spans.
	RedisCacheSpansEnabled    bool `mapstructure:"redis_cache_spans_enabled" default:"false"`     // db.system=redis cache spans (also requires storage_spans_enabled)
	InMemoryCacheSpansEnabled bool `mapstructure:"in_memory_cache_spans_enabled" default:"false"` // db.system=in_memory cache spans (also requires storage_spans_enabled)
	// CaptureExceptions records errors (CaptureException calls, error-level logs,
	// recovered panics) as OTel "exception" span events for SigNoz's Exceptions
	// tab. Keep sample_rate at 1.0 so error-bearing traces are not sampled away.
	CaptureExceptions bool `mapstructure:"capture_exceptions" default:"true"`
}

// OtelLogsConfig configures OTLP log export. See OtelTracesConfig for the
// AuthHeader/AuthValue convenience pair.
type OtelLogsConfig struct {
	Enabled    bool              `mapstructure:"enabled" default:"false"`
	Endpoint   string            `mapstructure:"endpoint" validate:"omitempty"`
	Protocol   string            `mapstructure:"protocol" validate:"omitempty"`
	AuthHeader string            `mapstructure:"auth_header" validate:"omitempty"`
	AuthValue  string            `mapstructure:"auth_value" validate:"omitempty"`
	Headers    map[string]string `mapstructure:"headers" validate:"omitempty"`
}

// MergedHeaders returns the effective header set, merging the AuthHeader/
// AuthValue convenience pair into the explicit Headers map. The pair wins on
// conflict so single-header env-var configs take precedence over YAML defaults.
func (c OtelTracesConfig) MergedHeaders() map[string]string {
	return mergeAuthHeader(c.Headers, c.AuthHeader, c.AuthValue)
}

// MergedHeaders — see OtelTracesConfig.MergedHeaders.
func (c OtelLogsConfig) MergedHeaders() map[string]string {
	return mergeAuthHeader(c.Headers, c.AuthHeader, c.AuthValue)
}

// OtelMetricsConfig configures OTLP metric export (app-level DB/cache metrics).
// Independent of Traces: metrics are always-on aggregate signal, cheap and
// unsampled, so they carry steady-state monitoring while spans stay for debug.
type OtelMetricsConfig struct {
	Enabled    bool              `mapstructure:"enabled" default:"false"`
	Endpoint   string            `mapstructure:"endpoint" validate:"omitempty"`
	Protocol   string            `mapstructure:"protocol" validate:"omitempty"`
	AuthHeader string            `mapstructure:"auth_header" validate:"omitempty"`
	AuthValue  string            `mapstructure:"auth_value" validate:"omitempty"`
	Headers    map[string]string `mapstructure:"headers" validate:"omitempty"`
	// Export interval in seconds (PeriodicReader). Longer = cheaper (fewer samples).
	IntervalSeconds int `mapstructure:"interval_seconds" default:"60"`
	// TemporalEnabled attaches the Temporal Go SDK MetricsHandler to the shared
	// MeterProvider when the metrics pipeline is on. Off by default — Temporal
	// SDK series are higher volume than app DB/cache metrics.
	TemporalEnabled bool `mapstructure:"temporal_enabled" default:"false"`
	// HTTPServerEnabled keeps otelgin's http.server.request.duration instead of
	// dropping it, so request rate / latency / error rate per route come from
	// metrics rather than from spans. Off by default (~31% of our own ingestion,
	// and SigNoz already derives it); turn it on where the backend cannot store
	// traces and this is the only source of API latency.
	HTTPServerEnabled bool `mapstructure:"http_server_enabled" default:"false"`
}

// MergedHeaders — see OtelTracesConfig.MergedHeaders.
func (c OtelMetricsConfig) MergedHeaders() map[string]string {
	return mergeAuthHeader(c.Headers, c.AuthHeader, c.AuthValue)
}

func mergeAuthHeader(headers map[string]string, authHeader, authValue string) map[string]string {
	if authHeader == "" || authValue == "" {
		return headers
	}
	out := make(map[string]string, len(headers)+1)
	for k, v := range headers {
		out[k] = v
	}
	out[authHeader] = authValue
	return out
}

// ResolveServiceName returns the service name for the OTel resource.
// Precedence: otel.service_name → logging.service_name → deployment.mode.
func (c OtelConfig) ResolveServiceName(cfg *Configuration) string {
	if c.ServiceName != "" {
		return c.ServiceName
	}
	if cfg.Logging.ServiceName != "" {
		return cfg.Logging.ServiceName
	}
	return string(cfg.Deployment.Mode)
}

// ResolveProtocol picks a per-signal protocol, falling back to otel.protocol,
// then to "grpc". The result is normalized to a canonical transport value:
// "http" for any HTTP variant (the OTel-standard "http/protobuf", "http/json",
// or a bare "http") and "grpc" otherwise. Normalizing here prevents the
// exporter-selection bug where a config value of "http/protobuf" failed an
// exact `protocol == "http"` check and silently fell back to the gRPC exporter.
func (c OtelConfig) ResolveProtocol(signalProtocol string) string {
	raw := signalProtocol
	if raw == "" {
		raw = c.Protocol
	}
	if raw == "" {
		return "grpc"
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), "http") {
		return "http"
	}
	return "grpc"
}

// ResolveHeaders picks per-signal headers, falling back to otel.headers when
// the signal hasn't supplied its own.
func (c OtelConfig) ResolveHeaders(signalHeaders map[string]string) map[string]string {
	if len(signalHeaders) > 0 {
		return signalHeaders
	}
	return c.Headers
}

type PyroscopeConfig struct {
	Enabled         bool     `mapstructure:"enabled"`
	ServerAddress   string   `mapstructure:"server_address"`
	ApplicationName string   `mapstructure:"application_name"`
	BasicAuthUser   string   `mapstructure:"basic_auth_user"`
	BasicAuthPass   string   `mapstructure:"basic_auth_password"`
	ProfileTypes    []string `mapstructure:"profile_types"`
	SampleRate      uint32   `mapstructure:"sample_rate" default:"100"`
	DisableGCRuns   bool     `mapstructure:"disable_gc_runs" default:"false"`
}

type TemporalConfig struct {
	Address                string               `mapstructure:"address" validate:"required"`
	TaskQueue              string               `mapstructure:"task_queue" validate:"required"`
	Namespace              string               `mapstructure:"namespace" validate:"required"`
	APIKey                 string               `mapstructure:"api_key"`
	APIKeyName             string               `mapstructure:"api_key_name"`
	TLS                    bool                 `mapstructure:"tls"`
	MaxWorkflowsPerCronRun int                  `mapstructure:"max_workflows_per_cron_run"`
	Worker                 TemporalWorkerConfig `mapstructure:"worker"`
}

type TemporalWorkerConfig struct {
	// MaxConcurrentActivityExecutionSize is the max number of activities executed concurrently per worker.
	// Default: 10
	MaxConcurrentActivityExecutionSize int `mapstructure:"max_concurrent_activity_execution_size"`
	// MaxConcurrentWorkflowTaskExecutionSize is the max number of workflow tasks executed concurrently per worker.
	// Default: 10
	MaxConcurrentWorkflowTaskExecutionSize int `mapstructure:"max_concurrent_workflow_task_execution_size"`
	// WorkerActivitiesPerSecond is the rate limit for activities per second per worker. 0 means unlimited.
	// Default: 5
	WorkerActivitiesPerSecond float64 `mapstructure:"worker_activities_per_second"`
	// TaskQueueActivitiesPerSecond is the rate limit for activities per second across all workers for the task queue. 0 means unlimited.
	// Default: 0 (unlimited)
	TaskQueueActivitiesPerSecond float64 `mapstructure:"task_queue_activities_per_second"`
}

type SecretsConfig struct {
	EncryptionKey string `mapstructure:"encryption_key" validate:"required"`
}

type BillingConfig struct {
	TenantID      string `mapstructure:"tenant_id" validate:"omitempty"`
	EnvironmentID string `mapstructure:"environment_id" validate:"omitempty"`
}

type EventProcessingConfig struct {
	// Rate limit in messages consumed per second
	Enabled               bool   `mapstructure:"enabled" default:"true"`
	Topic                 string `mapstructure:"topic" default:"events"`
	RateLimit             int64  `mapstructure:"rate_limit" default:"1"`
	ConsumerGroup         string `mapstructure:"consumer_group" default:"v1_event_processing"`
	TopicBackfill         string `mapstructure:"topic_backfill" default:"event_processing_backfill"`
	RateLimitBackfill     int64  `mapstructure:"rate_limit_backfill" default:"1"`
	ConsumerGroupBackfill string `mapstructure:"consumer_group_backfill" default:"v1_event_processing_backfill"`
	TopicDLQ              string `mapstructure:"topic_dlq" default:""`
}

type EventProcessingLazyConfig struct {
	Enabled               bool   `mapstructure:"enabled" default:"true"`
	Topic                 string `mapstructure:"topic" default:"events_lazy"`
	RateLimit             int64  `mapstructure:"rate_limit" default:"1"`
	ConsumerGroup         string `mapstructure:"consumer_group" default:"v1_event_processing_lazy"`
	TopicBackfill         string `mapstructure:"topic_backfill" default:"event_processing_lazy_backfill"`
	RateLimitBackfill     int64  `mapstructure:"rate_limit_backfill" default:"1"`
	ConsumerGroupBackfill string `mapstructure:"consumer_group_backfill" default:"v1_event_processing_lazy_backfill"`
	TopicDLQ              string `mapstructure:"topic_dlq" default:""`
}

type EventProcessingReplayConfig struct {
	Enabled       bool   `mapstructure:"enabled" default:"true"`
	Topic         string `mapstructure:"topic" default:"v1_event_processing_replay"`
	RateLimit     int64  `mapstructure:"rate_limit" default:"1"`
	ConsumerGroup string `mapstructure:"consumer_group" default:"v1_event_processing_replay"`
}

// MeterUsageTrackingConfig configures the meter_usage pipeline consumer
type MeterUsageTrackingConfig struct {
	Enabled                   bool   `mapstructure:"enabled" default:"true"`
	Topic                     string `mapstructure:"topic" default:"events"`
	RateLimit                 int64  `mapstructure:"rate_limit" default:"1"`
	ConsumerGroup             string `mapstructure:"consumer_group" default:"v1_meter_usage_tracking_service"`
	TopicDLQ                  string `mapstructure:"topic_dlq" default:""`
	RedisDeduplicationEnabled bool   `mapstructure:"redis_deduplication_enabled" default:"false"`

	// event.rejected webhook (fired when an event produces no meter usage); opt-in.
	RejectedEventWebhookEnabled bool `mapstructure:"rejected_event_webhook_enabled" default:"false"`
	// throttle: at most once per window per (tenant, env, event_name); needs Redis.
	RejectedEventWebhookWindow time.Duration `mapstructure:"rejected_event_webhook_window" default:"10m"`
}

// UsageAlertsConfig controls the usage-driven alert pipeline end to end:
// meter-usage post-insert schedules a debounced per-customer Temporal workflow
// which evaluates spend, entitlement-grant, and wallet alerts.
type UsageAlertsConfig struct {
	// Enabled routes post-insert alerting through the debounced Temporal
	// workflow instead of the Kafka wallet-alert path and inline spend-breach check.
	Enabled bool `mapstructure:"enabled" default:"false"`
	// ScheduleDelay is the debounce window: the workflow's StartDelay AND the
	// TTL of the Redis lock that throttles schedule attempts to one per customer per window.
	ScheduleDelay time.Duration `mapstructure:"schedule_delay" default:"5m30s"`
	// StaleAfter bounds staleness on both queues: a workflow run firing more
	// than this past its intended time yields once (ContinueAsNew) to the back
	// of the queue so fresher customers evaluate first, and each activity's
	// ScheduleToStartTimeout is set to the same value.
	StaleAfter               time.Duration `mapstructure:"stale_after" default:"1h"`
	WalletAlertsEnabled      bool          `mapstructure:"wallet_alerts_enabled" default:"true"`
	SpendAlertsEnabled       bool          `mapstructure:"spend_alerts_enabled" default:"true"`
	EntitlementAlertsEnabled bool          `mapstructure:"entitlement_alerts_enabled" default:"true"`
}

// MeterUsageTrackingLazyConfig configures the lazy consumer for tenants that
// the central publisher routes to the events_lazy topic (see Kafka.TopicLazy
// and Kafka.RouteTenantsOnLazyMode). Mirrors MeterUsageTrackingConfig but
// reads from a separate topic with its own consumer group so lazy traffic is
// isolated from the normal stream.
type MeterUsageTrackingLazyConfig struct {
	Enabled       bool   `mapstructure:"enabled" default:"true"`
	Topic         string `mapstructure:"topic" default:"events_lazy"`
	RateLimit     int64  `mapstructure:"rate_limit" default:"1"`
	ConsumerGroup string `mapstructure:"consumer_group" default:"v1_meter_usage_tracking_service_lazy"`
	TopicDLQ      string `mapstructure:"topic_dlq" default:""`
}

type WalletBalanceAlertConfig struct {
	// Rate limit in messages consumed per second
	Enabled       bool   `mapstructure:"enabled" default:"true"`
	Topic         string `mapstructure:"topic" default:"wallet_alert"`
	RateLimit     int64  `mapstructure:"rate_limit" default:"1"`
	ConsumerGroup string `mapstructure:"consumer_group" default:"v1_wallet_alert_service"`
}

type RawEventsReprocessingConfig struct {
	Enabled     bool   `mapstructure:"enabled" default:"true"`
	OutputTopic string `mapstructure:"output_topic" default:"prod_events_v4"`
}

type RawEventConsumptionConfig struct {
	Enabled       bool   `mapstructure:"enabled" default:"true"`
	Topic         string `mapstructure:"topic" default:"raw_events"`
	OutputTopic   string `mapstructure:"output_topic" default:"events"`
	RateLimit     int64  `mapstructure:"rate_limit" default:"10"`
	ConsumerGroup string `mapstructure:"consumer_group" default:"v1_raw_event_processing"`
}

// BulkEventConsumptionConfig configures the batch-mode consumer that reads
// RawEventBatch messages published by POST /events/bulk (batch_source=api_bulk
// metadata) and bulk-inserts each event into the ClickHouse events table.
// Shares the raw_events topic with RawEventConsumption (Bento) but a separate
// consumer group; a metadata filter keeps the two paths from cross-processing.
type BulkEventConsumptionConfig struct {
	Enabled       bool   `mapstructure:"enabled" default:"true"`
	Topic         string `mapstructure:"topic" default:"raw_events"`
	RateLimit     int64  `mapstructure:"rate_limit" default:"10"`
	ConsumerGroup string `mapstructure:"consumer_group" default:"v1_bulk_event_consumption"`
	TopicDLQ      string `mapstructure:"topic_dlq" default:""`
}

// BulkMeterUsageTrackingConfig is the batch-mode sibling of MeterUsageTracking:
// it reads the same api_bulk batches from raw_events, extracts per-meter
// quantity/hash for every event, and bulk-inserts into meter_usage. Distinct
// consumer group from BulkEventConsumption so the two run in parallel.
type BulkMeterUsageTrackingConfig struct {
	Enabled                      bool   `mapstructure:"enabled" default:"true"`
	Topic                        string `mapstructure:"topic" default:"raw_events"`
	RateLimit                    int64  `mapstructure:"rate_limit" default:"10"`
	ConsumerGroup                string `mapstructure:"consumer_group" default:"v1_bulk_meter_usage_tracking"`
	TopicDLQ                     string `mapstructure:"topic_dlq" default:""`
	RedisDeduplicationEnabled    bool   `mapstructure:"redis_deduplication_enabled" default:"true"`
	PostInsertSideEffectsEnabled bool   `mapstructure:"post_insert_side_effects_enabled" default:"true"`
}

type OnboardingEventsConfig struct {
	Enabled       bool   `mapstructure:"enabled" default:"true"`
	Topic         string `mapstructure:"topic" default:"staging_onboarding_events"`
	RateLimit     int64  `mapstructure:"rate_limit" default:"100"`
	ConsumerGroup string `mapstructure:"consumer_group" default:"onboarding_events_consumer"`
	MaxRetries    int    `mapstructure:"max_retries" default:"3"`
}

// WebhookRetryJobConfig configures the Temporal stale-webhook retry cron job.
// All filtering is applied by the activity after the DB query.
type WebhookRetryJobConfig struct {
	// Enabled is a kill switch — false exits the activity immediately with zero counts.
	Enabled bool `mapstructure:"enabled" default:"true"`
	// MaxAttempts is the maximum number of delivery failures before a system_event is
	// abandoned by the retry job. Replaces the hardcoded FailureCountLT(4) in the query.
	MaxAttempts int `mapstructure:"max_attempts" default:"5"`
	// RateLimit is the maximum number of webhook deliveries per second within a single
	// cron job run (token-bucket, golang.org/x/time/rate).
	RateLimit int `mapstructure:"rate_limit" default:"5"`
	// ExcludedTenants is a flat list of tenant IDs to skip entirely. Empty = process all.
	ExcludedTenants []string `mapstructure:"excluded_tenants"`
	// AllowedEventTypes is a whitelist of event_name values to retry. Empty = retry all.
	AllowedEventTypes []string `mapstructure:"allowed_event_types"`
}

type EnvAccessConfig struct {
	UserEnvMapping map[string]map[string][]string `mapstructure:"user_env_mapping" json:"user_env_mapping" validate:"omitempty"`
}

func resolveTenantRollout(tenantID string, globalEnabled bool, enabledTenants, disabledTenants []string) bool {
	if tenantID != "" {
		if slices.Contains(disabledTenants, tenantID) {
			return false
		}
		if slices.Contains(enabledTenants, tenantID) {
			return true
		}
	}
	return globalEnabled
}

type Email struct {
	Enabled      bool   `mapstructure:"enabled" validate:"required"`
	ResendAPIKey string `mapstructure:"resend_api_key" validate:"omitempty"`
	FromAddress  string `mapstructure:"from_address" validate:"omitempty"`
	ReplyTo      string `mapstructure:"reply_to" validate:"omitempty"`
	CalendarURL  string `mapstructure:"calendar_url" validate:"omitempty"`
}

type EmailConfig struct {
	Enabled          bool   `mapstructure:"enabled" validate:"required"`
	ResendAPIKey     string `mapstructure:"resend_api_key" validate:"omitempty"`
	FromAddress      string `mapstructure:"from_address" validate:"omitempty"`
	ReplyTo          string `mapstructure:"reply_to" validate:"omitempty"`
	CalendarURL      string `mapstructure:"calendar_url" validate:"omitempty"`
	ZapierWebhookURL string `mapstructure:"zapier_webhook_url" validate:"omitempty"`
}

type CheckoutConfig struct {
	BaseURL string `mapstructure:"base_url" validate:"required,url"`
}

type CustomerPortalConfig struct {
	URL               string `mapstructure:"url" validate:"required"`
	TokenTimeoutHours int    `mapstructure:"token_timeout_hours" validate:"required"`
}

// RedisConfig holds configuration for Redis
type RedisConfig struct {
	Host string `mapstructure:"host" default:"localhost"`
	Port int    `mapstructure:"port" default:"6379"`
	// Username is the data-node ACL user; leave empty for requirepass-style auth.
	Username  string        `mapstructure:"username" default:""`
	Password  string        `mapstructure:"password" default:""`
	DB        int           `mapstructure:"db" default:"0"`
	UseTLS bool `mapstructure:"use_tls" default:"false"`
	// Set to the cert SAN to verify ElastiCache wildcard certs.
	TLSServerName string `mapstructure:"tls_server_name" default:""`
	// Defaults true for ElastiCache compatibility; set false to verify.
	TLSSkipVerify bool          `mapstructure:"tls_skip_verify" default:"true"`
	PoolSize      int           `mapstructure:"pool_size" default:"10"`
	Timeout       time.Duration `mapstructure:"timeout" default:"5s"`
	KeyPrefix     string        `mapstructure:"key_prefix" default:"flexprice"`
	// ClusterMode: true → *redis.ClusterClient (Redis Cluster, ElastiCache
	// cluster-mode enabled). false → standalone *redis.Client. Default is
	// true to preserve the pre-1.1 hardcoded behaviour; flip to false for
	// single-node Redis. Baked default lives in config.yaml; env override:
	// FLEXPRICE_REDIS_CLUSTER_MODE. Ignored when SentinelMasterName is set.
	ClusterMode bool `mapstructure:"cluster_mode"`

	// Sentinel HA: a non-empty SentinelMasterName switches to Sentinel mode
	// (ignores Host/Port/ClusterMode) and resolves the master via the quorum.
	// SentinelAddrs are the sentinel endpoints, NOT the master. Username/Password
	// (above) auth the data nodes; SentinelUsername/Password auth the sentinels.
	SentinelMasterName string   `mapstructure:"sentinel_master_name" default:""`
	SentinelAddrs      []string `mapstructure:"sentinel_addrs"`
	SentinelUsername   string   `mapstructure:"sentinel_username" default:""`
	SentinelPassword   string   `mapstructure:"sentinel_password" default:""`

	// RouteReadsToReplicas (Sentinel only) routes reads to the lowest-latency node
	// among master+replicas; writes stay on master. Read scaling, not sharding.
	RouteReadsToReplicas bool `mapstructure:"route_reads_to_replicas" default:"false"`
}

func NewConfig() (*Configuration, error) {
	v := viper.New()

	// Step 1: Load `.env` then `.env.local` if they exist.
	_ = godotenv.Load()

	// Step 2: Initialize Viper
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath("../../../internal/config")
	v.AddConfigPath("../../internal/config")
	v.AddConfigPath("./internal/config")
	v.AddConfigPath("./config")

	// Step 3: Set up environment variables support
	v.SetEnvPrefix("FLEXPRICE")
	v.AutomaticEnv()

	// Step 4: Environment variable key mapping (e.g., FLEXPRICE_KAFKA_CONSUMER_GROUP)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Bind bare env vars (no FLEXPRICE_ prefix) for service identity fields
	_ = v.BindEnv("logging.service_name", "SERVICE_NAME")
	_ = v.BindEnv("logging.environment", "ENVIRONMENT")
	_ = v.BindEnv("logging.region", "REGION")

	// Auto-bind every scalar/slice key in the Configuration struct to its FLEXPRICE_* env
	// var. This replaces ~50 hand-written v.BindEnv calls (a graveyard grown one prod
	// incident at a time). Viper's AutomaticEnv is NOT consulted by Unmarshal for keys
	// absent from the loaded config.yaml or nested under underscores, so such keys silently
	// fall to their Go zero value unless the key is registered. Walking the struct registers
	// every leaf key, so a FLEXPRICE_* env var always lands regardless of which config.yaml a
	// deployment mounts (baked file on ECS, ConfigMap on GKE) — and no new key can be
	// forgotten. Runs once at startup; reflection cost is irrelevant. See bindEnvs below.
	// clickhouse.protocol (FLEXPRICE_CLICKHOUSE_PROTOCOL) is bound automatically here.
	bindEnvs(v, reflect.TypeOf(Configuration{}))

	// Exception capture is on by default. Struct `default:` tags aren't applied at runtime
	// here (defaults live in config.yaml), so guarantee default-on for deploys whose
	// config.yaml predates this key. Env/yaml still override.
	v.SetDefault("otel.traces.capture_exceptions", true)

	// Step 5: Read the YAML file
	if err := v.ReadInConfig(); err != nil {
		fmt.Printf("Error reading config file: %v\n", err)
		if !errors.As(err, &viper.ConfigFileNotFoundError{}) {
			return nil, err
		}
	} else {
		fmt.Fprintf(os.Stderr, "Using config file: %s\n", v.ConfigFileUsed())
	}

	var cfg Configuration
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unable to decode into config struct, %v", err)
	}

	// Step 6: Parse API keys from env var (JSON string override).
	// We read the OS env var directly instead of via Viper because the value is a JSON
	// string — Viper/mapstructure would try to decode it as a map and panic during
	// Unmarshal. Reading it here (after Unmarshal) avoids that conflict.
	apiKeysEnv := os.Getenv("FLEXPRICE_AUTH_API_KEY_KEYS")
	if apiKeysEnv != "" {
		var apiKeys map[string]APIKeyDetails
		if err := json.Unmarshal([]byte(apiKeysEnv), &apiKeys); err != nil {
			return nil, fmt.Errorf("failed to parse FLEXPRICE_AUTH_API_KEY_KEYS JSON: %v", err)
		}
		cfg.Auth.APIKey.Keys = apiKeys
	}

	// tenant webhook config
	tenantWebhookConfig := make(map[string]TenantWebhookConfig)
	if err := v.UnmarshalKey("webhook.tenants", &tenantWebhookConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal webhook tenants config: %v", err)
	}
	cfg.Webhook.Tenants = tenantWebhookConfig
	cfg.Webhook.normalizeTenantKeys()

	// Alternative: try to parse user_env_mapping directly
	userEnvMappingJSON := v.GetString("user_env_mapping")
	if userEnvMappingJSON != "" {
		var userEnvMapping map[string]map[string][]string
		if err := json.Unmarshal([]byte(userEnvMappingJSON), &userEnvMapping); err != nil {
			return nil, fmt.Errorf("failed to parse FLEXPRICE_USER_ENV_MAPPING JSON: %v", err)
		}
		cfg.EnvAccess.UserEnvMapping = userEnvMapping
	}

	return &cfg, nil
}

// bindEnvs walks a (possibly nested) struct type and registers a Viper env binding for
// every scalar/slice leaf field. The dotted key path is built from each field's
// `mapstructure` tag (falling back to the lowercased field name); Viper derives the env var
// name from that path via SetEnvPrefix("FLEXPRICE") + SetEnvKeyReplacer(".", "_"), i.e.
// FLEXPRICE_<UPPER_SNAKE_PATH> — exactly the names the Helm chart and ECS task definitions
// already set.
//
// Why this is needed: Viper's AutomaticEnv is not consulted by Unmarshal for keys that are
// absent from the loaded config.yaml or nested under underscores, so such keys silently
// resolve to their Go zero value. Registering the key here makes Unmarshal honor the env var
// regardless of which config.yaml a deployment mounts. Replaces ~50 hand-maintained
// v.BindEnv calls and guarantees no future key can be forgotten.
//
// Maps are intentionally NOT bound: the env-driven ones hold JSON strings
// (auth.api_key.keys, env_access.user_env_mapping) that mapstructure can't decode into a
// map; they are parsed by hand after Unmarshal. Slices ARE bound — Viper's default
// StringToSlice decode hook splits a comma-separated env var into []string (e.g.
// kafka_secondary.brokers). Unexported fields are skipped.
func bindEnvs(v *viper.Viper, t reflect.Type, parts ...string) {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" { // unexported — Unmarshal can't set it anyway
			continue
		}
		tag := strings.Split(f.Tag.Get("mapstructure"), ",")[0]
		if tag == "-" {
			continue
		}
		if tag == "" {
			tag = strings.ToLower(f.Name)
		}
		path := append(append([]string{}, parts...), tag)

		ft := f.Type
		for ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}
		switch ft.Kind() {
		case reflect.Struct:
			bindEnvs(v, ft, path...)
		case reflect.Map:
			// JSON-string env vars can't decode into a map; parsed by hand after Unmarshal.
		default:
			// scalars and slices (Viper splits comma-separated env into []string)
			_ = v.BindEnv(strings.Join(path, "."))
		}
	}
}

func (c Configuration) Validate() error {
	return validator.ValidateRequest(c)
}

// devDBPassword is the shared local docker-compose DB password baked into config.yaml.
// Legitimate for local dev; a red flag in any real deployment.
const devDBPassword = "flexprice123"

const (
	defaultClickHouseDialTimeout = 10 * time.Second
	defaultClickHouseReadTimeout = 30 * time.Second
)

// placeholderSecrets are the exact dev/sample values baked into config.yaml (plus empty).
// A non-local deployment booting with any of these for an ENABLED feature is running on a
// public credential, so validateSecrets flags it (warn-only — see NewValidatedConfig).
var placeholderSecrets = map[string]bool{
	"": true, // unset
	"dev-only-insecure-secret-prod-sets-FLEXPRICE_AUTH_SECRET": true,
	"<supabase service key>":                                   true,
	"svix_auth_token":                                          true,
}

// NewValidatedConfig loads configuration and, for non-local deployments, enforces fail-fast
// validation: struct `validate` tags plus a placeholder-secret check. Binary entry points
// (cmd/server) use this so a misconfigured deployment fails to START — with a rolling deploy
// the old task keeps serving and the deploy fails loudly — instead of booting into a silent
// incident (empty auth.secret → forgeable JWTs, dev DB password, etc). Unit tests call
// NewConfig directly to stay lean.
func NewValidatedConfig() (*Configuration, error) {
	cfg, err := NewConfig()
	if err != nil {
		return nil, err
	}
	if cfg.Deployment.Mode == types.ModeLocal {
		return cfg, nil
	}
	// NOTE: we deliberately do NOT run the full-struct cfg.Validate() here.
	// Many fields carry dormant `validate:"required"` tags that were never enforced
	// (Validate was never called at boot historically) and are broken for boot-time
	// use: `required` on a bool fails whenever it's false (Cache.Enabled, S3.Enabled,
	// DynamoDB.InUse), and AWS-only creds (FlexpriceS3Exports.*) are legitimately unset
	// on GCP. Enforcing them wholesale crashlooped every non-local pod. Fixing those
	// tags is a separate cleanup; until then fail-fast is scoped to the targeted secret
	// check below.
	//
	// validateSecrets is WARN-ONLY: it logs placeholder/dev secrets for enabled features
	// but does NOT abort boot. A hard fail at boot risks a prod crashloop (cf. the
	// 2026-07-06 full-struct-validation incident), so we surface misconfig in logs
	// without taking prod down. Tighten to hard-fail later, once every env is confirmed
	// clean via a staging deploy.
	if err := cfg.validateSecrets(); err != nil {
		log.Printf("[config] WARNING: %v", err)
	}

	// Scoped hard fail, unlike the warn-only check above. It applies only when
	// auth.saml.enabled is on, so a deployment that does not offer SSO cannot be
	// taken down by it — the risk that made the rest of this function warn-only.
	//
	// Failing at boot is the right trade here because the alternative is worse
	// than a crash: with an empty or relative base URL the SP metadata a
	// customer uploads to their identity provider contains unusable endpoints,
	// and the failure surfaces much later as an audience mismatch on every
	// assertion, pointing at signatures rather than at configuration.
	if err := cfg.Auth.SAML.validate(); err != nil {
		return nil, err
	}
	if err := cfg.validateSAMLDependencies(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// validateSecrets flags placeholder/dev secret values for features that are actually
// enabled. It is intentionally conservative — it checks only KNOWN baked sentinels (not mere
// emptiness of optional secrets) so it never false-positives on a legitimate deploy.
// The caller (NewValidatedConfig) gates this on deployment.mode != local and treats a
// returned error as a WARNING, not a boot failure.
func (c Configuration) validateSecrets() error {
	isPlaceholder := func(v string) bool { return placeholderSecrets[strings.TrimSpace(v)] }

	var bad []string
	// auth.secret signs/verifies JWTs for the flexprice provider — always required there.
	if c.Auth.Provider == types.AuthProviderFlexprice && isPlaceholder(c.Auth.Secret) {
		bad = append(bad, "auth.secret (FLEXPRICE_AUTH_SECRET)")
	}
	// supabase service key — required only when supabase is the auth provider.
	if c.Auth.Provider == types.AuthProviderSupabase && isPlaceholder(c.Auth.Supabase.ServiceKey) {
		bad = append(bad, "auth.supabase.service_key (FLEXPRICE_AUTH_SUPABASE_SERVICE_KEY)")
	}
	// svix token — required only when the Svix webhook backend is on.
	if c.Webhook.Svix.Enabled && isPlaceholder(c.Webhook.Svix.AuthToken) {
		bad = append(bad, "webhook.svix_config.auth_token (FLEXPRICE_SVIX_API_KEY)")
	}
	// db creds — reject only the shared dev password. Don't require non-empty: managed IAM
	// auth can legitimately use an empty DB password.
	if strings.TrimSpace(c.Postgres.Password) == devDBPassword {
		bad = append(bad, "postgres.password (FLEXPRICE_POSTGRES_PASSWORD)")
	}
	if strings.TrimSpace(c.ClickHouse.Password) == devDBPassword {
		bad = append(bad, "clickhouse.password (FLEXPRICE_CLICKHOUSE_PASSWORD)")
	}
	// NOTE: secrets.encryption_key is intentionally NOT checked. ECS prod currently decrypts
	// with the baked value (canonical env FLEXPRICE_SECRETS_ENCRYPTION_KEY is unset there; ECS
	// sets a different, unread name). Rejecting it would block prod boot and switching keys
	// would corrupt stored ciphertext. Reconcile separately before adding it. See config.yaml.

	if len(bad) > 0 {
		return fmt.Errorf("placeholder/dev secrets detected for enabled features (mode %q): %s — inject real values via FLEXPRICE_* env",
			c.Deployment.Mode, strings.Join(bad, ", "))
	}
	return nil
}

// GetDefaultConfig returns a default configuration for local development
// This is useful for running scripts or other non-web applications
func GetDefaultConfig() *Configuration {
	return &Configuration{
		Deployment: DeploymentConfig{Mode: types.ModeLocal},
		Logging:    LoggingConfig{Level: types.LogLevelDebug},
	}
}

// protocol maps the configured protocol name onto the driver's enum. An unset or
// unrecognised value yields clickhouse.Native, which is both the driver's zero
// value and the behaviour every deployment had before this field existed.
func (c ClickHouseConfig) protocol() clickhouse.Protocol {
	if strings.EqualFold(c.Protocol, "http") {
		return clickhouse.HTTP
	}
	return clickhouse.Native
}

func (c ClickHouseConfig) GetClientOptions() *clickhouse.Options {
	options := &clickhouse.Options{
		Addr: []string{c.Address},
		Auth: clickhouse.Auth{
			Database: c.Database,
			Username: c.Username,
			Password: c.Password,
		},
		ConnOpenStrategy: clickhouse.ConnOpenInOrder,
		// Bounded dial/read deadlines. Without DialTimeout the native driver can
		// block forever on connect: ClickHouse Cloud behind AWS PrivateLink is
		// fronted by multiple AZ ENIs, and an in-order dial to an ENI that never
		// completes the TCP/native handshake hangs indefinitely with no default
		// deadline. A finite DialTimeout makes it fail over to the next address.
		DialTimeout: defaultClickHouseDialTimeout,
		ReadTimeout: defaultClickHouseReadTimeout,
		// Pool sizing. Zero values leave the driver defaults (MaxIdleConns 5,
		// MaxOpenConns MaxIdleConns+5), which cap per-process query concurrency at 10.
		MaxOpenConns: c.MaxOpenConns,
		MaxIdleConns: c.MaxIdleConns,
	}
	if c.DialTimeout > 0 {
		options.DialTimeout = c.DialTimeout
	}
	if c.ReadTimeout > 0 {
		options.ReadTimeout = c.ReadTimeout
	}
	if c.TLS {
		options.TLS = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: c.TLSSkipVerify} // #nosec G402 -- opt-in, dev-only self-signed certs
	}
	options.Protocol = c.protocol()

	maxMemoryUsageBytes := c.MaxMemoryUsage * int64(1024) * int64(1024) * int64(1024)
	options.Settings = clickhouse.Settings{
		"max_memory_usage": maxMemoryUsageBytes,
	}
	return options
}

func (c PostgresConfig) GetDSN() string {
	return fmt.Sprintf(
		"user=%s password=%s dbname=%s host=%s port=%d sslmode=%s",
		c.User,
		c.Password,
		c.DBName,
		c.Host,
		c.Port,
		c.SSLMode,
	)
}

func (c PostgresConfig) GetReaderDSN() string {
	// If reader host is not configured, fall back to writer host
	host := c.ReaderHost
	port := c.ReaderPort

	if host == "" {
		host = c.Host
	}
	if port == 0 {
		port = c.Port
	}

	return fmt.Sprintf(
		"user=%s password=%s dbname=%s host=%s port=%d sslmode=%s",
		c.User,
		c.Password,
		c.DBName,
		host,
		port,
		c.SSLMode,
	)
}

func (c PostgresConfig) HasSeparateReader() bool {
	return c.ReaderHost != "" && c.ReaderHost != c.Host
}

type RBACConfig struct {
	RolesConfigPath string `mapstructure:"roles_config_path" json:"roles_config_path"`
}

// OAuthConfig holds generic OAuth configuration for multiple providers
type OAuthConfig struct {
	// Base redirect URI - provider-specific paths may be appended
	// Example: "https://admin-dev.flexprice.io/tools/integrations/oauth/callback"
	RedirectURI string `mapstructure:"redirect_uri" validate:"required,url"`
}
