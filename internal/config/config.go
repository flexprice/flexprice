package config

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/Shopify/sarama"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/go-playground/validator/v10"
	"github.com/joho/godotenv"
	"github.com/spf13/viper"
	"go.temporal.io/sdk/client"
)

type Configuration struct {
	Deployment DeploymentConfig `validate:"required"`
	Server     ServerConfig     `validate:"required"`
	Auth       AuthConfig       `validate:"required"`
	Kafka      KafkaConfig      `validate:"required"`
	ClickHouse ClickHouseConfig `validate:"required"`
	Logging    LoggingConfig    `validate:"required"`
	Postgres   PostgresConfig   `validate:"required"`
	Sentry     SentryConfig     `validate:"required"`
	Event      EventConfig      `validate:"required"`
	DynamoDB   DynamoDBConfig   `validate:"required"`
	Temporal   TemporalConfig   `validate:"required"`
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
	Brokers       []string             `mapstructure:"brokers" validate:"required"`
	ConsumerGroup string               `mapstructure:"consumer_group" validate:"required"`
	Topic         string               `mapstructure:"topic" validate:"required"`
	UseSASL       bool                 `mapstructure:"use_sasl"`
	SASLMechanism sarama.SASLMechanism `mapstructure:"sasl_mechanism"`
	SASLUser      string               `mapstructure:"sasl_user"`
	SASLPassword  string               `mapstructure:"sasl_password"`
	ClientID      string               `mapstructure:"client_id" validate:"required"`
}

type ClickHouseConfig struct {
	Address  string `mapstructure:"address" validate:"required"`
	TLS      bool   `mapstructure:"tls"`
	Username string `mapstructure:"username" validate:"required"`
	Password string `mapstructure:"password" validate:"required"`
	Database string `mapstructure:"database" validate:"required"`
}

type LoggingConfig struct {
	Level types.LogLevel `mapstructure:"level" validate:"required"`
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

type TemporalConfig struct {
	Address   string `mapstructure:"address" validate:"required"`
	TaskQueue string `mapstructure:"task_queue" validate:"required"`
	Namespace string `mapstructure:"namespace" validate:"required"`
}

func NewConfig() (*Configuration, error) {
	v := viper.New()

	_ = godotenv.Load()

	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath("./internal/config")
	v.AddConfigPath("./config")

	v.SetEnvPrefix("FLEXPRICE")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	if err := v.ReadInConfig(); err != nil && !errors.As(err, &viper.ConfigFileNotFoundError{}) {
		return nil, err
	}

	var cfg Configuration
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unable to decode into config struct, %v", err)
	}

	apiKeysStr := v.GetString("auth.api_key.keys")
	if apiKeysStr != "" {
		var apiKeys map[string]APIKeyDetails
		if err := json.Unmarshal([]byte(apiKeysStr), &apiKeys); err != nil {
			return nil, fmt.Errorf("failed to parse API keys JSON: %v", err)
		}
		cfg.Auth.APIKey.Keys = apiKeys
	}

	return &cfg, nil
}

func (c Configuration) Validate() error {
	validate := validator.New()
	return validate.Struct(c)
}

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

func (c *TemporalConfig) GetClientOptions() client.Options {
	return client.Options{
		HostPort:  c.Address,
		Namespace: c.TaskQueue,
		ConnectionOptions: client.ConnectionOptions{
			MaxPayloadSize: 2 * 1024 * 1024, // 2MB
		},
	}
}
