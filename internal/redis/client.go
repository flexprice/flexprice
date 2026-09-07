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

// redisMode is the connection topology NewClient selects from RedisConfig.
type redisMode string

const (
	modeStandalone          redisMode = "standalone"
	modeCluster             redisMode = "cluster"
	modeSentinel            redisMode = "sentinel"
	modeSentinelReplicaRead redisMode = "sentinel-replica-read"
)

// resolveRedisMode maps config to a topology (Sentinel > Cluster > Standalone)
// and validates Sentinel coherence: master name and addresses must be set
// together, else HA is silently lost (addrs dropped) or go-redis defaults to a
// phantom localhost sentinel. Pure (no I/O) so both precedence and the guards
// are unit-testable without a live Redis.
func resolveRedisMode(c config.RedisConfig) (redisMode, error) {
	hasMaster := c.SentinelMasterName != ""
	hasAddrs := len(c.SentinelAddrs) > 0
	switch {
	case hasMaster && !hasAddrs:
		return "", fmt.Errorf("redis: FLEXPRICE_REDIS_SENTINEL_MASTER_NAME is set but no sentinel addresses provided (FLEXPRICE_REDIS_SENTINEL_ADDRS)")
	case hasAddrs && !hasMaster:
		return "", fmt.Errorf("redis: sentinel addresses are set but FLEXPRICE_REDIS_SENTINEL_MASTER_NAME is empty — set the master name to enable Sentinel mode, or clear the addresses")
	case hasMaster && c.RouteReadsToReplicas:
		return modeSentinelReplicaRead, nil
	case hasMaster:
		return modeSentinel, nil
	case c.ClusterMode:
		return modeCluster, nil
	default:
		return modeStandalone, nil
	}
}

// buildOptions maps config to go-redis options for the resolved topology. Pure
// (no I/O) so the credential split — Username/Password to the data nodes,
// SentinelUsername/SentinelPassword to the sentinels — is unit-testable without
// a live Redis.
func buildOptions(c config.RedisConfig) (*redis.UniversalOptions, redisMode, error) {
	mode, err := resolveRedisMode(c)
	if err != nil {
		return nil, "", err
	}

	var tlsConfig *tls.Config
	if c.UseTLS {
		tlsConfig = &tls.Config{
			MinVersion:         tls.VersionTLS12,
			ServerName:         c.TLSServerName,
			InsecureSkipVerify: c.TLSSkipVerify, // #nosec G402 -- default true for ElastiCache; set false + ServerName to verify
		}
	}

	opts := &redis.UniversalOptions{
		Username:     c.Username,
		Password:     c.Password,
		DB:           c.DB,
		ReadTimeout:  c.Timeout,
		WriteTimeout: c.Timeout,
		PoolSize:     c.PoolSize,
		TLSConfig:    tlsConfig,
	}

	switch mode {
	case modeSentinel, modeSentinelReplicaRead:
		// Addrs are the sentinel endpoints; go-redis (via Failover()) discovers
		// the master/replicas. resolveRedisMode has already ensured they're set.
		opts.Addrs = c.SentinelAddrs
		opts.MasterName = c.SentinelMasterName
		opts.SentinelUsername = c.SentinelUsername
		opts.SentinelPassword = c.SentinelPassword
		// RouteByLatency: reads go to the lowest-latency node among
		// master+replicas, writes to master. Read scaling, not sharding.
		opts.RouteByLatency = mode == modeSentinelReplicaRead
	default: // modeCluster, modeStandalone
		opts.Addrs = []string{fmt.Sprintf("%s:%d", c.Host, c.Port)}
	}
	return opts, mode, nil
}

// NewClient creates a Redis client in one of three modes (see resolveRedisMode):
// Sentinel (HA/automatic failover), Cluster (sharded), or Standalone (single node).
func NewClient(config *config.Configuration, log *logger.Logger) (*Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), config.Redis.Timeout)
	defer cancel()

	opts, mode, err := buildOptions(config.Redis)
	if err != nil {
		return nil, err
	}

	var rdb redis.UniversalClient
	switch mode {
	case modeSentinelReplicaRead:
		rdb = redis.NewFailoverClusterClient(opts.Failover())
	case modeSentinel:
		rdb = redis.NewFailoverClient(opts.Failover())
	case modeCluster:
		rdb = redis.NewClusterClient(opts.Cluster())
	default: // modeStandalone
		// UniversalOptions.Simple() routes to a standalone *redis.Client; DB index applies.
		rdb = redis.NewClient(opts.Simple())
	}

	result, err := rdb.Ping(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to create redis client (mode=%s): %w", mode, err)
	}

	log.Info(ctx, "PING result", "result", result, "mode", string(mode))
	log.Info(ctx, "Connected to Redis successfully", "addr", opts.Addrs, "mode", string(mode))

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
