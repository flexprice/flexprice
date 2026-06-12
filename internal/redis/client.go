package redis

import (
	"context"
	"crypto/tls"
	"fmt"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/redis/go-redis/v9"
)

// Client wraps a Redis client. The underlying *redis.UniversalClient transparently
// targets a standalone node or a Redis Cluster based on RedisConfig.ClusterMode.
type Client struct {
	rdb redis.UniversalClient
	log *logger.Logger
}

// NewClient creates a new Redis client. Set RedisConfig.ClusterMode=true for
// Redis Cluster (e.g. AWS ElastiCache cluster mode enabled); leave false for
// standalone Redis (single ElastiCache node, in-cluster redis, Redis sentinel
// via the universal client's failover path).
func NewClient(config *config.Configuration, log *logger.Logger) (*Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), config.Redis.Timeout)
	defer cancel()

	var tlsConfig *tls.Config
	if config.Redis.UseTLS {
		tlsConfig = &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true, // Required for AWS ElastiCache wildcard certificates
		}
	}

	opts := &redis.UniversalOptions{
		Addrs:        []string{fmt.Sprintf("%s:%d", config.Redis.Host, config.Redis.Port)},
		Password:     config.Redis.Password,
		DB:           config.Redis.DB,
		ReadTimeout:  config.Redis.Timeout,
		WriteTimeout: config.Redis.Timeout,
		PoolSize:     config.Redis.PoolSize,
		TLSConfig:    tlsConfig,
	}

	var rdb redis.UniversalClient
	if config.Redis.ClusterMode {
		rdb = redis.NewClusterClient(opts.Cluster())
	} else {
		// UniversalOptions.Simple() routes to a standalone *redis.Client; DB index applies.
		rdb = redis.NewClient(opts.Simple())
	}

	result, err := rdb.Ping(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to create redis client: %w", err)
	}

	log.Info(ctx, "PING result", "result", result, "cluster_mode", config.Redis.ClusterMode)
	log.Info(ctx, "Connected to Redis successfully", "addr", opts.Addrs, "cluster_mode", config.Redis.ClusterMode)

	return &Client{
		rdb: rdb,
		log: log,
	}, nil
}

// GetClient returns the underlying Redis client. Callers should depend on
// redis.UniversalClient (or redis.Cmdable) rather than the concrete type so
// they work for both cluster and standalone deployments.
func (c *Client) GetClient() redis.UniversalClient {
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
