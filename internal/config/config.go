package config

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
	Enabled bool   `mapstructure:"enabled" validate:"required"`
	Type    string `mapstructure:"type" validate:"required"`
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
	// TopicDLQ is the legacy shared dead-letter topic. It is now the fallback DLQ
	// for consumers without a per-consumer-group topic_dlq (see the TopicDLQ fields
	// on the event_processing* / *_usage_tracking configs). Empty falls back to an
	// in-memory queue (non-durable).
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
	// TopicDLQ is the per-consumer-group dead-letter Kafka topic. Empty disables the
	// per-consumer DLQ and falls back to the legacy shared DLQ (kafka.topic_dlq).
	TopicDLQ string `mapstructure:"topic_dlq" default:""`
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
	// TopicDLQ is the per-consumer-group dead-letter Kafka topic. Empty disables the
	// per-consumer DLQ and falls back to the legacy shared DLQ (kafka.topic_dlq).
	TopicDLQ string `mapstructure:"topic_dlq" default:""`
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
	// TopicDLQ is the per-consumer-group dead-letter Kafka topic. Empty disables the
	// per-consumer DLQ and falls back to the legacy shared DLQ (kafka.topic_dlq).
	TopicDLQ string `mapstructure:"topic_dlq" default:""`
}

type FeatureUsageTrackingLazyConfig struct {
	Enabled               bool   `mapstructure:"enabled" default:"true"`
	Topic                 string `mapstructure:"topic" default:"events_lazy"`
	RateLimit             int64  `mapstructure:"rate_limit" default:"1"`
	ConsumerGroup         string `mapstructure:"consumer_group" default:"v1_feature_tracking_service_realtime"`
	TopicBackfill         string `mapstructure:"topic_backfill" default:"v1_feature_tracking_service_lazy_backfill"`
	RateLimitBackfill     int64  `mapstructure:"rate_limit_backfill" default:"1"`
	ConsumerGroupBackfill string `mapstructure:"consumer_group_backfill" default:"v1_feature_tracking_service_lazy_backfill"`
	// TopicDLQ is the per-consumer-group dead-letter Kafka topic. Empty disables the
	// per-consumer DLQ and falls back to the legacy shared DLQ (kafka.topic_dlq).
	TopicDLQ string `mapstructure:"topic_dlq" default:""`
}

type FeatureUsageTrackingReplayConfig struct {
	Enabled       bool   `mapstructure:"enabled" default:"true"`
	Topic         string `mapstructure:"topic" default:"v1_feature_tracking_service_replay"`
	RateLimit     int64  `mapstructure:"rate_limit" default:"1"`
	ConsumerGroup string `mapstructure:"consumer_group" default:"v1_feature_tracking_service_replay"`
}

// MeterUsageTrackingConfig configures the meter_usage pipeline consumer
type MeterUsageTrackingConfig struct {
	Enabled       bool   `mapstructure:"enabled" default:"true"`
	Topic         string `mapstructure:"topic" default:"events"`
	RateLimit     int64  `mapstructure:"rate_limit" default:"1"`
	ConsumerGroup string `mapstructure:"consumer_group" default:"v1_meter_usage_tracking_service"`
	// TopicDLQ is the per-consumer-group dead-letter Kafka topic. Empty disables the
	// per-consumer DLQ and falls back to the legacy shared DLQ (kafka.topic_dlq).
	TopicDLQ string `mapstructure:"topic_dlq" default:""`
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
	// TopicDLQ is the per-consumer-group dead-letter Kafka topic. Empty disables the
	// per-consumer DLQ and falls back to the legacy shared DLQ (kafka.topic_dlq).
	TopicDLQ string `mapstructure:"topic_dlq" default:""`
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
	EnableUsageBenchmark              bool   `mapstructure:"enable_usage_benchmark" validate:"omitempty"`

	// Per-tenant overrides for the meter-usage rollout. Resolution order:
	//   1. disabled_tenants — tenant force-disabled (highest priority)
	//   2. enabled_tenants  — tenant force-enabled
	//   3. global flag above — applies to everyone else
	MeterUsageForPreviewInvoiceEnabledTenants  []string `mapstructure:"meter_usage_for_preview_invoice_enabled_tenants" validate:"omitempty"`
	MeterUsageForPreviewInvoiceDisabledTenants []string `mapstructure:"meter_usage_for_preview_invoice_disabled_tenants" validate:"omitempty"`
	MeterUsageForAnalyticsEnabledTenants       []string `mapstructure:"meter_usage_for_analytics_enabled_tenants" validate:"omitempty"`
	MeterUsageForAnalyticsDisabledTenants      []string `mapstructure:"meter_usage_for_analytics_disabled_tenants" validate:"omitempty"`
	UsageBenchmarkEnabledTenants               []string `mapstructure:"usage_benchmark_enabled_tenants" validate:"omitempty"`
	UsageBenchmarkDisabledTenants              []string `mapstructure:"usage_benchmark_disabled_tenants" validate:"omitempty"`
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
	// TopicDLQ is the per-consumer-group dead-letter Kafka topic. Empty disables the
	// per-consumer DLQ and falls back to the legacy shared DLQ (kafka.topic_dlq).
	TopicDLQ string `mapstructure:"topic_dlq" default:""`
}

type CostSheetUsageTrackingLazyConfig struct {
	Enabled       bool   `mapstructure:"enabled" default:"true"`
	Topic         string `mapstructure:"topic" default:"events_lazy"`
	RateLimit     int64  `mapstructure:"rate_limit" default:"1"`
	ConsumerGroup string `mapstructure:"consumer_group" default:"v1_costsheet_usage_tracking_service_lazy"`
	// TopicDLQ is the per-consumer-group dead-letter Kafka topic. Empty disables the
	// per-consumer DLQ and falls back to the legacy shared DLQ (kafka.topic_dlq).
	TopicDLQ string `mapstructure:"topic_dlq" default:""`
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

	// Explicitly bind keys where AutomaticEnv is ambiguous due to underscores in key segments
	_ = v.BindEnv("clickhouse.password", "FLEXPRICE_CLICKHOUSE_PASSWORD")
	_ = v.BindEnv("clickhouse.username", "FLEXPRICE_CLICKHOUSE_USERNAME")
	_ = v.BindEnv("clickhouse.address", "FLEXPRICE_CLICKHOUSE_ADDRESS")
	_ = v.BindEnv("clickhouse.database", "FLEXPRICE_CLICKHOUSE_DATABASE")

	// Redis cluster_mode/key_prefix are supplied only as env vars by the Helm chart
	// (they are not rendered into config.yaml). viper's Unmarshal does not consult
	// AutomaticEnv for keys absent from the config file, so without these explicit
	// binds ClusterMode silently resolves to false and a single-node client is used
	// against a cluster-mode endpoint, causing "MOVED" errors on every off-slot key.
	_ = v.BindEnv("redis.cluster_mode", "FLEXPRICE_REDIS_CLUSTER_MODE")
	_ = v.BindEnv("redis.key_prefix", "FLEXPRICE_REDIS_KEY_PREFIX")

	// Explicitly bind unified OTel config vars — AutomaticEnv misses nested keys with underscores
	_ = v.BindEnv("otel.enabled", "FLEXPRICE_OTEL_ENABLED")
	_ = v.BindEnv("otel.service_name", "FLEXPRICE_OTEL_SERVICE_NAME")
	_ = v.BindEnv("otel.protocol", "FLEXPRICE_OTEL_PROTOCOL")
	_ = v.BindEnv("otel.insecure", "FLEXPRICE_OTEL_INSECURE")
	_ = v.BindEnv("otel.traces.enabled", "FLEXPRICE_OTEL_TRACES_ENABLED")
	_ = v.BindEnv("otel.traces.endpoint", "FLEXPRICE_OTEL_TRACES_ENDPOINT")
	_ = v.BindEnv("otel.traces.protocol", "FLEXPRICE_OTEL_TRACES_PROTOCOL")
	_ = v.BindEnv("otel.traces.auth_header", "FLEXPRICE_OTEL_TRACES_AUTH_HEADER")
	_ = v.BindEnv("otel.traces.auth_value", "FLEXPRICE_OTEL_TRACES_AUTH_VALUE")
	_ = v.BindEnv("otel.traces.sample_rate", "FLEXPRICE_OTEL_TRACES_SAMPLE_RATE")
	_ = v.BindEnv("otel.traces.storage_spans_enabled", "FLEXPRICE_OTEL_TRACES_STORAGE_SPANS_ENABLED")
	_ = v.BindEnv("otel.traces.capture_exceptions", "FLEXPRICE_OTEL_TRACES_CAPTURE_EXCEPTIONS")
	// Exception capture is on by default. Struct `default:` tags aren't applied at
	// runtime here (defaults live in config.yaml), so guarantee default-on via
	// SetDefault for deploys whose config.yaml predates this key. Env/yaml override.
	v.SetDefault("otel.traces.capture_exceptions", true)
	_ = v.BindEnv("otel.logs.enabled", "FLEXPRICE_OTEL_LOGS_ENABLED")
	_ = v.BindEnv("otel.logs.endpoint", "FLEXPRICE_OTEL_LOGS_ENDPOINT")
	_ = v.BindEnv("otel.logs.protocol", "FLEXPRICE_OTEL_LOGS_PROTOCOL")
	_ = v.BindEnv("otel.logs.insecure", "FLEXPRICE_OTEL_LOGS_INSECURE")
	_ = v.BindEnv("otel.logs.auth_header", "FLEXPRICE_OTEL_LOGS_AUTH_HEADER")
	_ = v.BindEnv("otel.logs.auth_value", "FLEXPRICE_OTEL_LOGS_AUTH_VALUE")

	// Explicitly bind OTel logging vars — AutomaticEnv can miss nested keys with underscores
	_ = v.BindEnv("logging.otel_enabled", "FLEXPRICE_LOGGING_OTEL_ENABLED")
	_ = v.BindEnv("logging.otel_endpoint", "FLEXPRICE_LOGGING_OTEL_ENDPOINT")
	_ = v.BindEnv("logging.otel_insecure", "FLEXPRICE_LOGGING_OTEL_INSECURE")
	_ = v.BindEnv("logging.otel_protocol", "FLEXPRICE_LOGGING_OTEL_PROTOCOL")
	_ = v.BindEnv("logging.otel_auth_header", "FLEXPRICE_LOGGING_OTEL_AUTH_HEADER")
	_ = v.BindEnv("logging.otel_auth_value", "FLEXPRICE_LOGGING_OTEL_AUTH_VALUE")
	_ = v.BindEnv("logging.otel_debug", "FLEXPRICE_LOGGING_OTEL_DEBUG")

	// Explicitly bind Temporal env vars — AutomaticEnv + Unmarshal misses these
	// because the yaml ships non-empty defaults (api_key: "strong api key"), so
	// Unmarshal returns the yaml value instead of consulting AutomaticEnv.
	_ = v.BindEnv("temporal.api_key", "FLEXPRICE_TEMPORAL_API_KEY")
	_ = v.BindEnv("temporal.api_key_name", "FLEXPRICE_TEMPORAL_API_KEY_NAME")

	// Explicitly bind auth.api_key.header — AutomaticEnv misses keys containing underscores
	_ = v.BindEnv("auth.api_key.header", "FLEXPRICE_AUTH_API_KEY_HEADER")
	// Explicitly bind auth.secret — the helm ConfigMap's rendered config.yaml omits this key,
	// so on GKE deployments it stays empty and supabase/JWT token validation fails (login
	// broken, and an empty key makes tokens forgeable). The FLEXPRICE_AUTH_SECRET env is
	// injected from the secret; this bind makes Unmarshal actually read it.
	_ = v.BindEnv("auth.secret", "FLEXPRICE_AUTH_SECRET")
	// NOTE: auth.api_key.keys is intentionally NOT bound here because the env var is a
	// JSON string but Viper/mapstructure expects a map. It is handled manually in Step 6.

	// Explicitly bind the Svix auth token. The helm ConfigMap renders webhook.svix_config
	// {enabled, base_url} but NOT auth_token (it's a secret), and the chart injects the token
	// as FLEXPRICE_SVIX_API_KEY. Without this bind, webhook.svix_config.auth_token stays empty
	// on GKE (AutomaticEnv+Unmarshal won't map FLEXPRICE_SVIX_API_KEY to it), so the Svix client
	// is created with an empty key and every call 401s. Same class as auth.secret above.
	_ = v.BindEnv("webhook.svix_config.auth_token", "FLEXPRICE_SVIX_API_KEY")

	// Explicitly bind the second-cluster keys — their segment (kafka_secondary) contains an
	// underscore, which AutomaticEnv cannot disambiguate, and kafka_secondary is absent from
	// the YAML defaults (nil unless configured). Without these binds, FLEXPRICE_KAFKA_SECONDARY_*
	// are silently ignored and dual-write never turns on. See infrastructure/docs/GCP-CUTOVER-STEPWISE.md.
	_ = v.BindEnv("kafka_secondary.brokers", "FLEXPRICE_KAFKA_SECONDARY_BROKERS")
	_ = v.BindEnv("kafka_secondary.consumer_group", "FLEXPRICE_KAFKA_SECONDARY_CONSUMER_GROUP")
	_ = v.BindEnv("kafka_secondary.topic", "FLEXPRICE_KAFKA_SECONDARY_TOPIC")
	_ = v.BindEnv("kafka_secondary.topic_lazy", "FLEXPRICE_KAFKA_SECONDARY_TOPIC_LAZY")
	_ = v.BindEnv("kafka_secondary.topic_dlq", "FLEXPRICE_KAFKA_SECONDARY_TOPIC_DLQ")
	_ = v.BindEnv("kafka_secondary.tls", "FLEXPRICE_KAFKA_SECONDARY_TLS")
	_ = v.BindEnv("kafka_secondary.use_sasl", "FLEXPRICE_KAFKA_SECONDARY_USE_SASL")
	_ = v.BindEnv("kafka_secondary.sasl_mechanism", "FLEXPRICE_KAFKA_SECONDARY_SASL_MECHANISM")
	_ = v.BindEnv("kafka_secondary.sasl_user", "FLEXPRICE_KAFKA_SECONDARY_SASL_USER")
	_ = v.BindEnv("kafka_secondary.sasl_password", "FLEXPRICE_KAFKA_SECONDARY_SASL_PASSWORD")
	_ = v.BindEnv("kafka_secondary.sasl_oauth_scopes", "FLEXPRICE_KAFKA_SECONDARY_SASL_OAUTH_SCOPES")
	_ = v.BindEnv("kafka_secondary.client_id", "FLEXPRICE_KAFKA_SECONDARY_CLIENT_ID")
	_ = v.BindEnv("kafka_secondary.route_tenants_on_lazy_mode", "FLEXPRICE_KAFKA_SECONDARY_ROUTE_TENANTS_ON_LAZY_MODE")

	// Explicitly bind the PRIMARY kafka SASL credentials. The helm ConfigMap renders the
	// kafka block without sasl_user/sasl_password, so on a GKE deployment whose primary
	// cluster uses a password mechanism (e.g. AWS MSK SCRAM-SHA-512) these stay empty and
	// SCRAM auth fails. The FLEXPRICE_KAFKA_SASL_{USER,PASSWORD} envs are injected by the
	// chart; these binds make Unmarshal read them. (OAUTHBEARER/GMK needs neither.)
	_ = v.BindEnv("kafka.sasl_user", "FLEXPRICE_KAFKA_SASL_USER")
	_ = v.BindEnv("kafka.sasl_password", "FLEXPRICE_KAFKA_SASL_PASSWORD")

	// Explicitly bind the per-consumer-group DLQ topics. Both key segments contain
	// underscores, which AutomaticEnv+Unmarshal cannot disambiguate, so without these
	// binds the FLEXPRICE_*_TOPIC_DLQ envs would be ignored.
	_ = v.BindEnv("event_processing.topic_dlq", "FLEXPRICE_EVENT_PROCESSING_TOPIC_DLQ")
	_ = v.BindEnv("event_processing_lazy.topic_dlq", "FLEXPRICE_EVENT_PROCESSING_LAZY_TOPIC_DLQ")
	_ = v.BindEnv("feature_usage_tracking.topic_dlq", "FLEXPRICE_FEATURE_USAGE_TRACKING_TOPIC_DLQ")
	_ = v.BindEnv("feature_usage_tracking_lazy.topic_dlq", "FLEXPRICE_FEATURE_USAGE_TRACKING_LAZY_TOPIC_DLQ")
	_ = v.BindEnv("meter_usage_tracking.topic_dlq", "FLEXPRICE_METER_USAGE_TRACKING_TOPIC_DLQ")
	_ = v.BindEnv("meter_usage_tracking_lazy.topic_dlq", "FLEXPRICE_METER_USAGE_TRACKING_LAZY_TOPIC_DLQ")
	_ = v.BindEnv("costsheet_usage_tracking.topic_dlq", "FLEXPRICE_COSTSHEET_USAGE_TRACKING_TOPIC_DLQ")
	_ = v.BindEnv("costsheet_usage_tracking_lazy.topic_dlq", "FLEXPRICE_COSTSHEET_USAGE_TRACKING_LAZY_TOPIC_DLQ")

	// Explicitly bind deployment.mode — the helm ConfigMap is one shared object across the
	// api/consumer/worker Deployments, so it cannot carry a per-component mode; the only
	// per-component signal is the FLEXPRICE_DEPLOYMENT_MODE env each Deployment sets. Since
	// deployment.mode is absent from the rendered config.yaml and was not bound, AutomaticEnv+
	// Unmarshal ignored the env and every pod fell back to ModeLocal (running API + Temporal +
	// Kafka consumers regardless of role — e.g. the api consuming prod topics). This bind makes
	// Unmarshal honor the per-Deployment env. See infrastructure/docs/gcp-mumbai-gmk-consumer-runbook.md.
	_ = v.BindEnv("deployment.mode", "FLEXPRICE_DEPLOYMENT_MODE")

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

func (c Configuration) Validate() error {
	return validator.ValidateRequest(c)
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
