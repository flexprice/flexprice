package redis

import (
	"context"
	"crypto/tls"
	"fmt"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/redis/go-redis/v9"
)

// Client wraps Redis client functionality
type Client struct {
	rdb *redis.ClusterClient
	log *logger.Logger
}

// NewClient creates a new Redis client
func NewClient(config *config.Configuration, log *logger.Logger) (*Client, error) {

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), config.Redis.Timeout)
	defer cancel()

	// Create cluster client
	clusterOpts := &redis.ClusterOptions{
		Addrs:        []string{fmt.Sprintf("%s:%d", config.Redis.Host, config.Redis.Port)},
		Password:     config.Redis.Password,
		ReadTimeout:  config.Redis.Timeout,
		WriteTimeout: config.Redis.Timeout,
		PoolSize:     config.Redis.PoolSize,
	}

	if config.Redis.UseTLS {
		clusterOpts.TLSConfig = &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true, // Required for AWS ElastiCache wildcard certificates
		}
	}

	rdb := redis.NewClusterClient(clusterOpts)

	result, err := rdb.Ping(ctx).Result()

	if err != nil {
		return nil, fmt.Errorf("failed to create redis client: %w", err)
	}

	log.Infow("PING result", "result", result)

	log.Infow("Connected to Redis Cluster successfully", "addr", clusterOpts.Addrs)

	return &Client{
		rdb: rdb,
		log: log,
	}, nil
}

// GetClient returns the underlying Redis client
func (c *Client) GetClient() *redis.ClusterClient {
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
