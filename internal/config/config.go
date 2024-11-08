package config

import (
	"crypto/tls"
	"errors"
	"fmt"

	"strings"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/go-playground/validator/v10"
	"github.com/spf13/viper"
)

type Configuration struct {
	Server     ServerConfig     `validate:"required"`
	Kafka      KafkaConfig      `validate:"required"`
	ClickHouse ClickHouseConfig `validate:"required"`
	Meters     []MeterConfig    `validate:"required,dive"`
	Logging    LoggingConfig    `validate:"required"`
	Postgres   PostgresConfig   `validate:"required"`
}

type ServerConfig struct {
	Address string `validate:"required"`
}

type KafkaConfig struct {
	Brokers       []string
	ConsumerGroup string
	Topic         string
}

type ClickHouseConfig struct {
	Address  string
	TLS      bool
	Username string
	Password string
	Database string
}

type MeterConfig struct {
	ID              string
	AggregationType string
	WindowSize      string
}

type LoggingConfig struct {
	Level string `validate:"required"`
}

type PostgresConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
	SSLMode  string
}

func NewConfig() (*Configuration, error) {
	v := viper.New()

	// Modify config paths to ensure config.yaml is found
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath("./internal/config")
	v.AddConfigPath(".")
	v.AddConfigPath("./config")
	v.AddConfigPath("/etc/flexprice")

	// Set up environment variables support
	v.SetEnvPrefix("FLEXPRICE") // optional: prefix for env vars
	v.SetEnvKeyReplacer(strings.NewReplacer(
		".", "_",
		"-", "_",
	))
	v.AutomaticEnv()

	// Read config file if exists
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

	// Set defaults for meter window sizes
	for i := range config.Meters {
		if config.Meters[i].WindowSize == "" {
			config.Meters[i].WindowSize = "1m"
		}
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
