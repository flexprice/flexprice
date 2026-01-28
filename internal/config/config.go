package config

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
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
	Deployment                 DeploymentConfig                 `validate:"required"`
	Server                     ServerConfig                     `validate:"required"`
	Auth                       AuthConfig                       `validate:"required"`
	Kafka                      KafkaConfig                      `validate:"required"`
	ClickHouse                 ClickHouseConfig                 `validate:"required"`
	Logging                    LoggingConfig                    `validate:"required"`
	Postgres                   PostgresConfig                   `validate:"required"`
	Sentry                     SentryConfig                     `validate:"required"`
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
	CostSheetUsageTracking     CostSheetUsageTrackingConfig     `mapstructure:"costsheet_usage_tracking" validate:"required"`
	CostSheetUsageTrackingLazy CostSheetUsageTrackingLazyConfig `mapstructure:"costsheet_usage_tracking_lazy" validate:"required"`
	EventPostProcessing        EventPostProcessingConfig        `mapstructure:"event_post_processing" validate:"required"`
	FeatureUsageTracking       FeatureUsageTrackingConfig       `mapstructure:"feature_usage_tracking" validate:"required"`
	FeatureUsageTrackingLazy   FeatureUsageTrackingLazyConfig   `mapstructure:"feature_usage_tracking_lazy" validate:"required"`
	FeatureUsageTrackingReplay FeatureUsageTrackingReplayConfig `mapstructure:"feature_usage_tracking_replay" validate:"required"`
	EnvAccess                  EnvAccessConfig                  `mapstructure:"env_access" json:"env_access" validate:"omitempty"`
	FeatureFlag                FeatureFlagConfig                `mapstructure:"feature_flag" validate:"required"`
	Email                      EmailConfig                      `mapstructure:"email" validate:"required"`
	RBAC                       RBACConfig                       `mapstructure:"rbac" validate:"omitempty"`
	OAuth                      OAuthConfig                      `mapstructure:"oauth" validate:"required"`
	WalletBalanceAlert         WalletBalanceAlertConfig         `mapstructure:"wallet_balance_alert" validate:"required"`
	CustomerPortal             CustomerPortalConfig             `mapstructure:"customer_portal" validate:"required"`
	Redis                      RedisConfig                      `mapstructure:"redis" validate:"required"`
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
	Brokers                []string             `mapstructure:"brokers" validate:"required"`
	ConsumerGroup          string               `mapstructure:"consumer_group" validate:"required"`
	Topic                  string               `mapstructure:"topic" validate:"required"`
	TopicLazy              string               `mapstructure:"topic_lazy" validate:"required"`
	TLS                    bool                 `mapstructure:"tls"` // set to true if using 9094 port else can set to false
	UseSASL                bool                 `mapstructure:"use_sasl"`
	SASLMechanism          sarama.SASLMechanism `mapstructure:"sasl_mechanism"`
	SASLUser               string               `mapstructure:"sasl_user"`
	SASLPassword           string               `mapstructure:"sasl_password"`
	ClientID               string               `mapstructure:"client_id" validate:"required"`
	RouteTenantsOnLazyMode []string             `mapstructure:"route_tenants_on_lazy_mode" validate:"omitempty"`
}

type ClickHouseConfig struct {
	Address  string `mapstructure:"address" validate:"required"`
	TLS      bool   `mapstructure:"tls"`
	Username string `mapstructure:"username" validate:"required"`
	Password string `mapstructure:"password" validate:"required"`
	Database string `mapstructure:"database" validate:"required"`
}

type LoggingConfig struct {
	Level   types.LogLevel `mapstructure:"level" validate:"required"`
	DBLevel types.LogLevel `mapstructure:"db_level" validate:"required"`

	// Fluentd configuration
	FluentdEnabled bool   `mapstructure:"fluentd_enabled" default:"false"`
	FluentdHost    string `mapstructure:"fluentd_host" validate:"omitempty"`
	FluentdPort    int    `mapstructure:"fluentd_port" validate:"omitempty"`
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

type SentryConfig struct {
	Enabled     bool    `mapstructure:"enabled"`
	DSN         string  `mapstructure:"dsn"`
	Environment string  `mapstructure:"environment"`
	SampleRate  float64 `mapstructure:"sample_rate" default:"1.0"`
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
	Address    string `mapstructure:"address" validate:"required"`
	TaskQueue  string `mapstructure:"task_queue" validate:"required"`
	Namespace  string `mapstructure:"namespace" validate:"required"`
	APIKey     string `mapstructure:"api_key"`
	APIKeyName string `mapstructure:"api_key_name"`
	TLS        bool   `mapstructure:"tls"`
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
}

type FeatureUsageTrackingLazyConfig struct {
	Enabled               bool   `mapstructure:"enabled" default:"true"`
	Topic                 string `mapstructure:"topic" default:"events_lazy"`
	RateLimit             int64  `mapstructure:"rate_limit" default:"1"`
	ConsumerGroup         string `mapstructure:"consumer_group" default:"v1_feature_tracking_service_realtime"`
	TopicBackfill         string `mapstructure:"topic_backfill" default:"v1_feature_tracking_service_lazy_backfill"`
	RateLimitBackfill     int64  `mapstructure:"rate_limit_backfill" default:"1"`
	ConsumerGroupBackfill string `mapstructure:"consumer_group_backfill" default:"v1_feature_tracking_service_lazy_backfill"`
}

type FeatureUsageTrackingReplayConfig struct {
	Enabled       bool   `mapstructure:"enabled" default:"true"`
	Topic         string `mapstructure:"topic" default:"v1_feature_tracking_service_replay"`
	RateLimit     int64  `mapstructure:"rate_limit" default:"1"`
	ConsumerGroup string `mapstructure:"consumer_group" default:"v1_feature_tracking_service_replay"`
}

type WalletBalanceAlertConfig struct {
	// Rate limit in messages consumed per second
	Enabled       bool   `mapstructure:"enabled" default:"true"`
	Topic         string `mapstructure:"topic" default:"wallet_alert"`
	RateLimit     int64  `mapstructure:"rate_limit" default:"1"`
	ConsumerGroup string `mapstructure:"consumer_group" default:"v1_wallet_alert_service"`
}

type EnvAccessConfig struct {
	UserEnvMapping map[string]map[string][]string `mapstructure:"user_env_mapping" json:"user_env_mapping" validate:"omitempty"`
}

type FeatureFlagConfig struct {
	EnableFeatureUsageForAnalytics bool   `mapstructure:"enable_feature_usage_for_analytics" validate:"required"`
	ForceV1ForTenant               string `mapstructure:"force_v1_for_tenant" validate:"omitempty"`
}

type Email struct {
	Enabled      bool   `mapstructure:"enabled" validate:"required"`
	ResendAPIKey string `mapstructure:"resend_api_key" validate:"omitempty"`
	FromAddress  string `mapstructure:"from_address" validate:"omitempty"`
	ReplyTo      string `mapstructure:"reply_to" validate:"omitempty"`
	CalendarURL  string `mapstructure:"calendar_url" validate:"omitempty"`
}

type EmailConfig struct {
	Enabled      bool   `mapstructure:"enabled" validate:"required"`
	ResendAPIKey string `mapstructure:"resend_api_key" validate:"omitempty"`
	FromAddress  string `mapstructure:"from_address" validate:"omitempty"`
	ReplyTo      string `mapstructure:"reply_to" validate:"omitempty"`
	CalendarURL  string `mapstructure:"calendar_url" validate:"omitempty"`
}
type CostSheetUsageTrackingConfig struct {
	Enabled       bool   `mapstructure:"enabled" default:"true"`
	Topic         string `mapstructure:"topic" default:"events"`
	RateLimit     int64  `mapstructure:"rate_limit" default:"1"`
	ConsumerGroup string `mapstructure:"consumer_group" default:"v1_costsheet_usage_tracking_service"`
}

type CostSheetUsageTrackingLazyConfig struct {
	Enabled       bool   `mapstructure:"enabled" default:"true"`
	Topic         string `mapstructure:"topic" default:"events_lazy"`
	RateLimit     int64  `mapstructure:"rate_limit" default:"1"`
	ConsumerGroup string `mapstructure:"consumer_group" default:"v1_costsheet_usage_tracking_service_lazy"`
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
}

func NewConfig() (*Configuration, error) {
	v := viper.New()

	// Step 1: Load `.env` if it exists
	_ = godotenv.Load()

	// Step 2: Initialize Viper
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath("./internal/config")
	v.AddConfigPath("./config")

	// Step 3: Set up environment variables support
	v.SetEnvPrefix("FLEXPRICE")
	v.AutomaticEnv()

	// Step 4: Environment variable key mapping (e.g., FLEXPRICE_KAFKA_CONSUMER_GROUP)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

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

	// Step 6: Parse API keys
	apiKeysStr := v.GetString("auth.api_key.keys")
	// Parse API keys JSON if present
	if apiKeysStr != "" {
		var apiKeys map[string]APIKeyDetails
		if err := json.Unmarshal([]byte(apiKeysStr), &apiKeys); err != nil {
			return nil, fmt.Errorf("failed to parse API keys JSON: %v", err)
		}
		cfg.Auth.APIKey.Keys = apiKeys
	}

	// tenant webhook config
	tenantWebhookConfig := make(map[string]TenantWebhookConfig)
	if err := v.UnmarshalKey("webhook.tenants", &tenantWebhookConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal webhook tenants config: %v", err)
	}
	cfg.Webhook.Tenants = tenantWebhookConfig

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
