package config

import (
	"crypto/tls"
	"errors"
	"fmt"

	"strings"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/Shopify/sarama"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/go-playground/validator/v10"
	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

type Configuration struct {
	Deployment DeploymentConfig `validate:"required"`
	Server     ServerConfig     `validate:"required"`
	Auth       AuthConfig       `validate:"required"`
	Kafka      KafkaConfig      `validate:"required"`
	ClickHouse ClickHouseConfig `validate:"required"`
	Logging    LoggingConfig    `validate:"required"`
	Postgres   PostgresConfig   `validate:"required"`
	Temporal   TemporalConfig
	Stripe     StripeConfig
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
}

type SupabaseConfig struct {
	BaseURL string `mapstructure:"base_url"`
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
	Host     string `mapstructure:"host" validate:"required"`
	Port     int    `mapstructure:"port" validate:"required"`
	User     string `mapstructure:"user" validate:"required"`
	Password string `mapstructure:"password" validate:"required"`
	DBName   string `mapstructure:"dbname" validate:"required"`
	SSLMode  string `mapstructure:"sslmode" validate:"required"`
}

type TemporalConfig struct {
	HostPort string `mapstructure:"host_port"`
}

type StripeConfig struct {
	SecretKey string `mapstructure:"secret_key"`
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

	var config Configuration
	if err := v.Unmarshal(&config); err != nil {
		return nil, err
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &config, nil
}

func (c Configuration) Validate() error {
	validate := validator.New()
	return validate.Struct(c)
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
