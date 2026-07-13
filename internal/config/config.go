package config

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/Shopify/sarama"
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
	KafkaSecondary             *KafkaConfig                     `mapstructure:"kafka_secondary" validate:"omitempty"`
	ClickHouse                 ClickHouseConfig                 `validate:"required"`
	Logging                    LoggingConfig                    `validate:"required"`
	Postgres                   PostgresConfig                   `validate:"required"`
	Sentry                     SentryConfig                     `validate:"required"`
	Otel                       OtelConfig                       `validate:"omitempty"`
	Pyroscope                  PyroscopeConfig                  `validate:"required"`
	Event                      EventConfig                      `validate:"required"`
	DynamoDB                   DynamoDBConfig                   `validate:"required"`
	Temporal                   TemporalConfig                   `validate:"required"`
	Webhook                    Webhook                          `validate:"omitempty"`
	Secrets                    SecretsConfig                    `validate:"required"`
	Billing                    BillingConfig                    `validate:"omitempty"`
	S3                         S3Config                         `validate:"required"`
	FlexpriceS3Exports         FlexpriceS3ExportsConfig         `mapstructure:"flexprice_s3_exports" validate:"omitempty"`
	Cache                      CacheConfig                      `validate:"required"`
	EventProcessing            EventProcessingConfig            `mapstructure:"event_processing" validate:"required"`
	EventProcessingLazy        EventProcessingLazyConfig        `mapstructure:"event_processing_lazy" validate:"required"`
	EventProcessingReplay      EventProcessingReplayConfig      `mapstructure:"event_processing_replay" validate:"required"`
	CostSheetUsageTracking     CostSheetUsageTrackingConfig     `mapstructure:"costsheet_usage_tracking" validate:"required"`
	CostSheetUsageTrackingLazy CostSheetUsageTrackingLazyConfig `mapstructure:"costsheet_usage_tracking_lazy" validate:"required"`
	EventPostProcessing        EventPostProcessingConfig        `mapstructure:"event_post_processing" validate:"required"`
	FeatureUsageTracking       FeatureUsageTrackingConfig       `mapstructure:"feature_usage_tracking" validate:"required"`
	FeatureUsageTrackingLazy   FeatureUsageTrackingLazyConfig   `mapstructure:"feature_usage_tracking_lazy" validate:"required"`
	FeatureUsageTrackingReplay FeatureUsageTrackingReplayConfig `mapstructure:"feature_usage_tracking_replay" validate:"required"`
	MeterUsageTracking         MeterUsageTrackingConfig         `mapstructure:"meter_usage_tracking" validate:"required"`
	MeterUsageTrackingLazy     MeterUsageTrackingLazyConfig     `mapstructure:"meter_usage_tracking_lazy" validate:"required"`
	UsageBenchmark             UsageBenchmarkConfig             `mapstructure:"usage_benchmark" validate:"omitempty"`
	EnvAccess                  EnvAccessConfig                  `mapstructure:"env_access" json:"env_access" validate:"omitempty"`
	FeatureFlag                FeatureFlagConfig                `mapstructure:"feature_flag" validate:"required"`
	Email                      EmailConfig                      `mapstructure:"email" validate:"required"`
	RBAC                       RBACConfig                       `mapstructure:"rbac" validate:"omitempty"`
	OAuth                      OAuthConfig                      `mapstructure:"oauth" validate:"required"`
	WalletBalanceAlert         WalletBalanceAlertConfig         `mapstructure:"wallet_balance_alert" validate:"required"`
	CustomerPortal             CustomerPortalConfig             `mapstructure:"customer_portal" validate:"required"`
	Checkout                   CheckoutConfig                   `mapstructure:"checkout" validate:"omitempty"`
	Redis                      RedisConfig                      `mapstructure:"redis" validate:"required"`
	RawEventsReprocessing      RawEventsReprocessingConfig      `mapstructure:"raw_events_reprocessing" validate:"required"`
	RawEventConsumption        RawEventConsumptionConfig        `mapstructure:"raw_event_consumption" validate:"required"`
	IntegrationEvents          IntegrationEventsConfig          `mapstructure:"integration_events" validate:"omitempty"`
	OnboardingEvents           OnboardingEventsConfig           `mapstructure:"onboarding_events" validate:"omitempty"`
	WebhookRetryJob            WebhookRetryJobConfig            `mapstructure:"webhook_retry_job" validate:"omitempty"`
	Gemini                     GeminiConfig                     `mapstructure:"gemini" validate:"omitempty"`
	Whop                       WhopConfig                       `mapstructure:"whop" validate:"omitempty"`
	WebhookLogging             WebhookLoggingConfig             `mapstructure:"webhook_logging" validate:"omitempty"`
}

// WebhookLoggingConfig controls which inbound webhook requests are persisted to the DB.
type WebhookLoggingConfig struct {
	TenantIDs      []string `mapstructure:"tenant_ids"`
	EnvironmentIDs []string `mapstructure:"environment_ids"`
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

type FlexpriceS3ExportsConfig struct {
	Bucket             string `mapstructure:"bucket" validate:"required"`
	Region             string `mapstructure:"region" validate:"required"`
	AWSAccessKeyID     string `mapstructure:"aws_access_key_id" validate:"required"`
	AWSSecretAccessKey string `mapstructure:"aws_secret_access_key" validate:"required"`
	AWSSessionToken    string `mapstructure:"aws_session_token,omitempty"`
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
	Supabase SupabaseConfig     `mapstructure:"supabase"`
	APIKey   APIKeyConfig       `mapstructure:"api_key"`
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
	// TopicDLQ is the global fallback dead-letter Kafka topic used by handlers that
	// do not define their own per-consumer-group topic_dlq. Empty disables DLQ for
	// those handlers.
	TopicDLQ      string               `mapstructure:"topic_dlq" default:""`
	TLS           bool                 `mapstructure:"tls"` // set to true if using 9094 port else can set to false
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
}

type ClickHouseConfig struct {
	Address        string `mapstructure:"address" validate:"required"`
	TLS            bool   `mapstructure:"tls"`
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
	AutoMigrate            bool   `mapstructure:"auto_migrate" default:"false"`

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

// SentryConfig is retained only for transitional rollback. Error/exception
// capture is now OTel-native (see internal/tracing.CaptureException and
// internal/spanerr); Sentry is no longer the sink and defaults to disabled.
type SentryConfig struct {
	Enabled     bool    `mapstructure:"enabled" default:"false"`
	DSN         string  `mapstructure:"dsn"`
	Environment string  `mapstructure:"environment"`
	SampleRate  float64 `mapstructure:"sample_rate" default:"1.0"`
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

	Traces OtelTracesConfig `mapstructure:"traces"`
	Logs   OtelLogsConfig   `mapstructure:"logs"`
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
	StorageSpansEnabled bool              `mapstructure:"storage_spans_enabled" default:"false"` // enable per-query DB/cache/ClickHouse child spans (can be noisy)
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

type EventPostProcessingConfig struct {
	// Rate limit in messages consumed per second
	Enabled               bool   `mapstructure:"enabled" default:"true"`
	Topic                 string `mapstructure:"topic" default:"events_post_processing"`
	RateLimit             int64  `mapstructure:"rate_limit" default:"1"`
	ConsumerGroup         string `mapstructure:"consumer_group" default:"v1_events_post_processing"`
	TopicBackfill         string `mapstructure:"topic_backfill" default:"v1_events_post_processing_backfill"`
	RateLimitBackfill     int64  `mapstructure:"rate_limit_backfill" default:"1"`
	ConsumerGroupBackfill string `mapstructure:"consumer_group_backfill" default:"v1_events_post_processing_backfill"`
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
type FeatureUsageTrackingConfig struct {
	// Rate limit in messages consumed per second
	Enabled                bool   `mapstructure:"enabled" default:"true"`
	Topic                  string `mapstructure:"topic" default:"events"`
	RateLimit              int64  `mapstructure:"rate_limit" default:"1"`
	ConsumerGroup          string `mapstructure:"consumer_group" default:"v1_feature_tracking_service"`
	TopicBackfill          string `mapstructure:"topic_backfill" default:"v1_feature_tracking_service_backfill"`
	RateLimitBackfill      int64  `mapstructure:"rate_limit_backfill" default:"1"`
	ConsumerGroupBackfill  string `mapstructure:"consumer_group_backfill" default:"v1_feature_tracking_service_backfill"`
	BackfillEnabled        bool   `mapstructure:"backfill_enabled" default:"false"`
	WalletAlertPushEnabled bool   `mapstructure:"wallet_alert_push_enabled" default:"true"`
	TopicDLQ               string `mapstructure:"topic_dlq" default:""`
}

type FeatureUsageTrackingLazyConfig struct {
	Enabled               bool   `mapstructure:"enabled" default:"true"`
	Topic                 string `mapstructure:"topic" default:"events_lazy"`
	RateLimit             int64  `mapstructure:"rate_limit" default:"1"`
	ConsumerGroup         string `mapstructure:"consumer_group" default:"v1_feature_tracking_service_realtime"`
	TopicBackfill         string `mapstructure:"topic_backfill" default:"v1_feature_tracking_service_lazy_backfill"`
	RateLimitBackfill     int64  `mapstructure:"rate_limit_backfill" default:"1"`
	ConsumerGroupBackfill string `mapstructure:"consumer_group_backfill" default:"v1_feature_tracking_service_lazy_backfill"`
	TopicDLQ              string `mapstructure:"topic_dlq" default:""`
}

type FeatureUsageTrackingReplayConfig struct {
	Enabled       bool   `mapstructure:"enabled" default:"true"`
	Topic         string `mapstructure:"topic" default:"v1_feature_tracking_service_replay"`
	RateLimit     int64  `mapstructure:"rate_limit" default:"1"`
	ConsumerGroup string `mapstructure:"consumer_group" default:"v1_feature_tracking_service_replay"`
}

// MeterUsageTrackingConfig configures the meter_usage pipeline consumer
type MeterUsageTrackingConfig struct {
	Enabled                   bool   `mapstructure:"enabled" default:"true"`
	Topic                     string `mapstructure:"topic" default:"events"`
	RateLimit                 int64  `mapstructure:"rate_limit" default:"1"`
	ConsumerGroup             string `mapstructure:"consumer_group" default:"v1_meter_usage_tracking_service"`
	TopicDLQ                  string `mapstructure:"topic_dlq" default:""`
	RedisDeduplicationEnabled bool   `mapstructure:"redis_deduplication_enabled" default:"false"`

	// event.unmatched webhook (fired when an event produces no meter usage); opt-in.
	UnmatchedEventWebhookEnabled bool `mapstructure:"unmatched_event_webhook_enabled" default:"false"`
	// throttle: at most once per window per (tenant, env, event_name); needs Redis.
	UnmatchedEventWebhookWindow time.Duration `mapstructure:"unmatched_event_webhook_window" default:"10m"`
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

// UsageBenchmarkConfig configures the usage benchmarking consumer
type UsageBenchmarkConfig struct {
	Enabled       bool   `mapstructure:"enabled" default:"false"`
	Topic         string `mapstructure:"topic" default:"staging_benchmarking"`
	RateLimit     int64  `mapstructure:"rate_limit" default:"10"`
	ConsumerGroup string `mapstructure:"consumer_group" default:"v1_usage_benchmark_service"`
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

type FeatureFlagConfig struct {
	EnableFeatureUsageForAnalytics    bool   `mapstructure:"enable_feature_usage_for_analytics" validate:"required"`
	ForceV1ForTenant                  string `mapstructure:"force_v1_for_tenant" validate:"omitempty"`
	EnableMeterUsageForPreviewInvoice bool   `mapstructure:"enable_meter_usage_for_preview_invoice" validate:"omitempty"`
	EnableMeterUsageForAnalytics      bool   `mapstructure:"enable_meter_usage_for_analytics" validate:"omitempty"`
	EnableMeterUsageForBilling        bool   `mapstructure:"enable_meter_usage_for_billing" validate:"omitempty"`
	EnableUsageBenchmark              bool   `mapstructure:"enable_usage_benchmark" validate:"omitempty"`

	// Per-tenant overrides for the meter-usage rollout. Resolution order:
	//   1. disabled_tenants — tenant force-disabled (highest priority)
	//   2. enabled_tenants  — tenant force-enabled
	//   3. global flag above — applies to everyone else
	MeterUsageForPreviewInvoiceEnabledTenants  []string `mapstructure:"meter_usage_for_preview_invoice_enabled_tenants" validate:"omitempty"`
	MeterUsageForPreviewInvoiceDisabledTenants []string `mapstructure:"meter_usage_for_preview_invoice_disabled_tenants" validate:"omitempty"`
	MeterUsageForAnalyticsEnabledTenants       []string `mapstructure:"meter_usage_for_analytics_enabled_tenants" validate:"omitempty"`
	MeterUsageForAnalyticsDisabledTenants      []string `mapstructure:"meter_usage_for_analytics_disabled_tenants" validate:"omitempty"`
	MeterUsageForBillingEnabledTenants         []string `mapstructure:"meter_usage_for_billing_enabled_tenants" validate:"omitempty"`
	MeterUsageForBillingDisabledTenants        []string `mapstructure:"meter_usage_for_billing_disabled_tenants" validate:"omitempty"`
	UsageBenchmarkEnabledTenants               []string `mapstructure:"usage_benchmark_enabled_tenants" validate:"omitempty"`
	UsageBenchmarkDisabledTenants              []string `mapstructure:"usage_benchmark_disabled_tenants" validate:"omitempty"`
}

// IsMeterUsageEnabledForBilling resolves the meter-usage rollout for the
// billing service for a specific tenant.
func (c *FeatureFlagConfig) IsMeterUsageEnabledForBilling(tenantID string) bool {
	return resolveTenantRollout(
		tenantID,
		c.EnableMeterUsageForBilling,
		c.MeterUsageForBillingEnabledTenants,
		c.MeterUsageForBillingDisabledTenants,
	)
}

// IsMeterUsageEnabledForPreviewInvoice resolves the meter-usage rollout for the
// preview-invoice endpoint for a specific tenant. See FeatureFlagConfig for the
// resolution order.
func (c *FeatureFlagConfig) IsMeterUsageEnabledForPreviewInvoice(tenantID string) bool {
	return resolveTenantRollout(
		tenantID,
		c.EnableMeterUsageForPreviewInvoice,
		c.MeterUsageForPreviewInvoiceEnabledTenants,
		c.MeterUsageForPreviewInvoiceDisabledTenants,
	)
}

// IsMeterUsageEnabledForAnalytics resolves the meter-usage rollout for the
// analytics endpoint for a specific tenant. See FeatureFlagConfig for the
// resolution order.
func (c *FeatureFlagConfig) IsMeterUsageEnabledForAnalytics(tenantID string) bool {
	return resolveTenantRollout(
		tenantID,
		c.EnableMeterUsageForAnalytics,
		c.MeterUsageForAnalyticsEnabledTenants,
		c.MeterUsageForAnalyticsDisabledTenants,
	)
}

// IsUsageBenchmarkEnabled resolves the usage-benchmark publish gate for a
// specific tenant. Gates publishBenchmarkEvent in the wallet billing path so
// the feature-usage / meter-usage comparison runs only for selected tenants.
// See FeatureFlagConfig for the resolution order.
func (c *FeatureFlagConfig) IsUsageBenchmarkEnabled(tenantID string) bool {
	return resolveTenantRollout(
		tenantID,
		c.EnableUsageBenchmark,
		c.UsageBenchmarkEnabledTenants,
		c.UsageBenchmarkDisabledTenants,
	)
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
type CostSheetUsageTrackingConfig struct {
	Enabled       bool   `mapstructure:"enabled" default:"true"`
	Topic         string `mapstructure:"topic" default:"events"`
	RateLimit     int64  `mapstructure:"rate_limit" default:"1"`
	ConsumerGroup string `mapstructure:"consumer_group" default:"v1_costsheet_usage_tracking_service"`
	TopicDLQ      string `mapstructure:"topic_dlq" default:""`
}

type CostSheetUsageTrackingLazyConfig struct {
	Enabled       bool   `mapstructure:"enabled" default:"true"`
	Topic         string `mapstructure:"topic" default:"events_lazy"`
	RateLimit     int64  `mapstructure:"rate_limit" default:"1"`
	ConsumerGroup string `mapstructure:"consumer_group" default:"v1_costsheet_usage_tracking_service_lazy"`
	TopicDLQ      string `mapstructure:"topic_dlq" default:""`
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
	Host      string        `mapstructure:"host" default:"localhost"`
	Port      int           `mapstructure:"port" default:"6379"`
	Password  string        `mapstructure:"password" default:""`
	DB        int           `mapstructure:"db" default:"0"`
	UseTLS    bool          `mapstructure:"use_tls" default:"false"`
	PoolSize  int           `mapstructure:"pool_size" default:"10"`
	Timeout   time.Duration `mapstructure:"timeout" default:"5s"`
	KeyPrefix string        `mapstructure:"key_prefix" default:"flexprice"`
	// ClusterMode: true → *redis.ClusterClient (Redis Cluster, ElastiCache
	// cluster-mode enabled). false → standalone *redis.Client. Default is
	// true to preserve the pre-1.1 hardcoded behaviour; flip to false for
	// single-node Redis. Baked default lives in config.yaml; env override:
	// FLEXPRICE_REDIS_CLUSTER_MODE.
	ClusterMode bool `mapstructure:"cluster_mode"`
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
		fmt.Printf("Using config file: %s\n", v.ConfigFileUsed())
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

	// Parse webhook logging tenant/environment ID lists from env vars.
	// Viper cannot split comma-separated env vars into []string reliably,
	// so we read os.Getenv directly and split manually.
	if raw := os.Getenv("FLEXPRICE_WEBHOOK_LOGGING_TENANT_IDS"); raw != "" {
		cfg.WebhookLogging.TenantIDs = strings.Split(raw, ",")
	}
	if raw := os.Getenv("FLEXPRICE_WEBHOOK_LOGGING_ENVIRONMENT_IDS"); raw != "" {
		cfg.WebhookLogging.EnvironmentIDs = strings.Split(raw, ",")
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

func (c ClickHouseConfig) GetClientOptions() *clickhouse.Options {
	options := &clickhouse.Options{
		Addr: []string{c.Address},
		Auth: clickhouse.Auth{
			Database: c.Database,
			Username: c.Username,
			Password: c.Password,
		},
		ConnOpenStrategy: clickhouse.ConnOpenInOrder,
	}
	if c.TLS {
		options.TLS = &tls.Config{}
	}

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
