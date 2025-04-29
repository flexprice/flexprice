package redis

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/go-redis/redis/v8"
)

// Config holds Redis connection configuration
type Config struct {
	Host     string
	Port     int
	Password string
	DB       int
	UseTLS   bool
	PoolSize int
	Timeout  time.Duration
}

// ConfigFromMap creates a Config from a map[string]interface{}
func ConfigFromMap(m map[string]interface{}) Config {
	cfg := Config{}

	if host, ok := m["Host"].(string); ok {
		cfg.Host = host
	}

	if port, ok := m["Port"].(int); ok {
		cfg.Port = port
	}

	if password, ok := m["Password"].(string); ok {
		cfg.Password = password
	}

	if db, ok := m["DB"].(int); ok {
		cfg.DB = db
	}

	if useTLS, ok := m["UseTLS"].(bool); ok {
		cfg.UseTLS = useTLS
	}

	if poolSize, ok := m["PoolSize"].(int); ok {
		cfg.PoolSize = poolSize
	}

	if timeout, ok := m["Timeout"].(time.Duration); ok {
		cfg.Timeout = timeout
	}

	return cfg
}

// Client wraps Redis client functionality
type Client struct {
	rdb  *redis.Client
	log  *logger.Logger
	opts *redis.Options
}

// NewClient creates a new Redis client
func NewClient(cfg Config, log *logger.Logger) (*Client, error) {
	opts := &redis.Options{
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  cfg.Timeout,
		ReadTimeout:  cfg.Timeout,
		WriteTimeout: cfg.Timeout,
		PoolSize:     cfg.PoolSize,
	}

	if cfg.UseTLS {
		opts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	rdb := redis.NewClient(opts)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := rdb.Ping(ctx).Result(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	log.Info("Connected to Redis successfully")

	return &Client{
		rdb:  rdb,
		log:  log,
		opts: opts,
	}, nil
}

// GetClient returns the underlying Redis client
func (c *Client) GetClient() *redis.Client {
	return c.rdb
}

// Close closes the Redis client connection
func (c *Client) Close() error {
	return c.rdb.Close()
}

// Ping checks the Redis connection
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.rdb.Ping(ctx).Result()
	return err
}

// reconnect attempts to reconnect to Redis
func (c *Client) reconnect(ctx context.Context) error {
	if err := c.rdb.Close(); err != nil {
		c.log.Error("Failed to close existing Redis connection", "error", err)
	}

	c.rdb = redis.NewClient(c.opts)

	if _, err := c.rdb.Ping(ctx).Result(); err != nil {
		return fmt.Errorf("failed to reconnect to Redis: %w", err)
	}

	c.log.Info("Successfully reconnected to Redis")
	return nil
}
