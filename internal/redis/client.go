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

// resolveRedisMode maps config to a topology. Sentinel takes precedence over
// ClusterMode; RouteReadsToReplicas only applies within Sentinel. Pure function
// (no I/O) so the precedence rules are unit-testable without a live Redis.
func resolveRedisMode(c config.RedisConfig) redisMode {
	switch {
	case c.SentinelMasterName != "" && c.RouteReadsToReplicas:
		return modeSentinelReplicaRead
	case c.SentinelMasterName != "":
		return modeSentinel
	case c.ClusterMode:
		return modeCluster
	default:
		return modeStandalone
	}
}

// NewClient creates a new Redis client in one of three mutually exclusive modes,
// selected by RedisConfig:
//
//   - Sentinel   — SentinelMasterName set: HA with automatic failover. go-redis
//     resolves the current master via the sentinel quorum and re-resolves on
//     failover. Set RouteReadsToReplicas to spread reads across replicas (read
//     scaling, not sharding). Ignores Host/Port/ClusterMode.
//   - Cluster    — ClusterMode=true: Redis Cluster (e.g. ElastiCache cluster mode
//     enabled), data sharded across masters.
//   - Standalone — otherwise: single node (single ElastiCache node, in-cluster redis).
//
// Sentinel takes precedence over ClusterMode when both are set.
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
		Password:     config.Redis.Password,
		DB:           config.Redis.DB,
		ReadTimeout:  config.Redis.Timeout,
		WriteTimeout: config.Redis.Timeout,
		PoolSize:     config.Redis.PoolSize,
		TLSConfig:    tlsConfig,
	}

	var rdb redis.UniversalClient
	mode := resolveRedisMode(config.Redis)
	switch mode {
	case modeSentinel, modeSentinelReplicaRead:
		// Sentinel: Addrs are the SENTINEL endpoints; go-redis (via Failover())
		// maps them to SentinelAddrs and discovers the master/replicas itself.
		// Password above still authenticates to the data nodes; SentinelUsername/
		// SentinelPassword authenticate to the sentinels.
		if len(config.Redis.SentinelAddrs) == 0 {
			// go-redis silently defaults an empty Addrs to 127.0.0.1:26379, which
			// would connect to a phantom local sentinel instead of failing. Fail loud.
			return nil, fmt.Errorf("redis sentinel mode requires at least one sentinel address (FLEXPRICE_REDIS_SENTINEL_ADDRS)")
		}
		opts.Addrs = config.Redis.SentinelAddrs
		opts.MasterName = config.Redis.SentinelMasterName
		opts.SentinelUsername = config.Redis.SentinelUsername
		opts.SentinelPassword = config.Redis.SentinelPassword
		if mode == modeSentinelReplicaRead {
			// FailoverCluster client with RouteByLatency: read-only commands go to
			// the lowest-latency node among the master AND its replicas (writes
			// always go to the master). This distributes reads for scaling; it is
			// NOT data sharding — every node holds the full dataset.
			opts.RouteByLatency = true
			rdb = redis.NewFailoverClusterClient(opts.Failover())
		} else {
			rdb = redis.NewFailoverClient(opts.Failover())
		}
	case modeCluster:
		opts.Addrs = []string{fmt.Sprintf("%s:%d", config.Redis.Host, config.Redis.Port)}
		rdb = redis.NewClusterClient(opts.Cluster())
	default: // modeStandalone
		// UniversalOptions.Simple() routes to a standalone *redis.Client; DB index applies.
		opts.Addrs = []string{fmt.Sprintf("%s:%d", config.Redis.Host, config.Redis.Port)}
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
