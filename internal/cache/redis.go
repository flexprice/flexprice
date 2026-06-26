package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	redisClient "github.com/flexprice/flexprice/internal/redis"
	"github.com/redis/go-redis/v9"
)

const (
	// DeleteRetryDelay specifies how long to wait before retrying a failed delete operation
	DeleteRetryDelay = 100 * time.Millisecond

	// ScanCount determines how many keys to scan at once when using SCAN
	ScanCount = 100
)

// redisCacheImpl implements the Cache interface using Redis.
// The client is a UniversalClient so it works for both standalone and cluster deployments.
type redisCacheImpl struct {
	client redis.UniversalClient
	config *config.Configuration
	log    *logger.Logger
}

// Redis cache instance
var redisCache RedisCache

// NewRedisCache creates a new Redis cache
func NewRedisCache() RedisCache {
	if redisCache == nil {
		cfg, err := config.NewConfig()
		if err != nil {
			logger.NewNoopLogger().Error(context.Background(), "Failed to initialize Redis cache", "error", err)
			return nil
		}
		noop := logger.NewNoopLogger()
		InitializeRedisCache(cfg, noop)
	}
	return redisCache
}

// InitializeRedisCache initializes the global Redis cache instance using the
// provided configuration and logger. Dependencies are explicit; callers should
// pass through the values wired by Initialize rather than relying on globals.
func InitializeRedisCache(cfg *config.Configuration, log *logger.Logger) RedisCache {
	if redisCache != nil {
		return redisCache
	}
	client, err := redisClient.NewClient(cfg, log)
	if err != nil {
		log.Error(context.Background(), "Failed to create Redis client", "error", err)
		return nil
	}
	return &redisCacheImpl{
		client: client.GetClient(),
		config: cfg,
		log:    log,
	}
}

// GetRedisCache returns the global Redis cache instance
func GetRedisCache() RedisCache {
	if redisCache == nil {
		cfg, err := config.NewConfig()
		if err != nil {
			logger.NewNoopLogger().Error(context.Background(), "Failed to initialize Redis cache", "error", err)
			return nil
		}
		noop := logger.NewNoopLogger()
		InitializeRedisCache(cfg, noop)
	}
	return redisCache
}

// Helper function to add prefix to key
func (c *redisCacheImpl) GetRedisKey(key string) string {
	if c.config.Redis.KeyPrefix == "" {
		return key
	}
	return c.config.Redis.KeyPrefix + ":" + key
}

func (c *redisCacheImpl) IsEnabled() bool {
	return c.config.Cache.Enabled && c.config.Cache.Redis.Enabled
}

// Get retrieves a value from the cache
func (c *redisCacheImpl) Get(ctx context.Context, key string) (interface{}, bool) {
	if c == nil || !c.IsEnabled() {
		c.log.Debug(ctx, "Cache is disabled")
		return nil, false
	}

	redisKey := c.GetRedisKey(key)

	value, err := c.client.Get(ctx, redisKey).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			// Key does not exist
			return nil, false
		}
		c.log.Error(ctx, "Redis GET error", "key", redisKey, "error", err)
		return nil, false
	}

	return value, true
}

// Set adds a value to the cache with the specified expiration
func (c *redisCacheImpl) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) {
	if c == nil || !c.IsEnabled() {
		c.log.Debug(ctx, "Cache is disabled")
		return
	}
	// Use default expiration if none specified
	if expiration == 0 {
		expiration = ExpiryDefaultRedis
	}

	// Generate Key
	redisKey := c.GetRedisKey(key)

	// Convert value to string if it's not already
	var strValue string
	switch v := value.(type) {
	case string:
		strValue = v
	default:
		// Marshal non-string values to JSON
		jsonBytes, err := json.Marshal(value)
		if err != nil {
			c.log.Error(ctx, "Failed to marshal cache value", "key", redisKey, "error", err)
			return
		}
		strValue = string(jsonBytes)
	}

	if err := c.client.Set(ctx, redisKey, strValue, expiration).Err(); err != nil {
		c.log.Error(ctx, "Redis SET error", "key", redisKey, "error", err)
	}
}

// Delete removes a key from the cache with retry
func (c *redisCacheImpl) Delete(ctx context.Context, key string) {
	if c == nil || !c.IsEnabled() {
		c.log.Debug(ctx, "Cache is disabled")
		return
	}
	redisKey := c.GetRedisKey(key)
	err := c.delete(ctx, redisKey)
	if err != nil {
		c.log.Info(ctx, "Redis DELETE failed, retrying...", "key", redisKey, "error", err)

		// Create a new context with timeout for the retry
		retryCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Wait before retrying
		time.Sleep(DeleteRetryDelay)

		// Retry once
		if retryErr := c.delete(retryCtx, redisKey); retryErr != nil {
			c.log.Error(ctx, "Redis DELETE retry failed", "key", redisKey, "error", retryErr)
		}
	}
}

// delete is a helper function to perform the actual deletion
func (c *redisCacheImpl) delete(ctx context.Context, key string) error {
	return c.client.Unlink(ctx, key).Err()
}

// TrySetNX sets key to value with expiration only if the key does not exist (lock acquire).
// Returns true if the key was set, false if the key already existed. Returns error on Redis failure.
func (c *redisCacheImpl) TrySetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, error) {
	if c == nil || !c.IsEnabled() {
		c.log.Debug(ctx, "Cache is disabled")
		return false, nil
	}

	redisKey := c.GetRedisKey(key)
	var strValue string
	switch v := value.(type) {
	case string:
		strValue = v
	default:
		jsonBytes, err := json.Marshal(value)
		if err != nil {
			return false, fmt.Errorf("marshal cache value: %w", err)
		}
		strValue = string(jsonBytes)
	}
	ok, err := c.client.SetNX(ctx, redisKey, strValue, expiration).Result()
	if err != nil {
		return false, err
	}
	return ok, nil
}

// DeleteByPrefix removes all keys with the given prefix
func (c *redisCacheImpl) DeleteByPrefix(ctx context.Context, prefix string) {
	if c == nil || !c.IsEnabled() {
		c.log.Debug(ctx, "Cache is disabled")
		return
	}

	// TODO: This needs to be implemented properly
	// Use SCAN to iterate through keys matching the pattern
	// iter := c.client.Scan(ctx, 0, prefix+"*", ScanCount).Iterator()

	// var keysToDelete []string

	// for iter.Next(ctx) {
	// 	keysToDelete = append(keysToDelete, iter.Val())

	// 	// Delete in batches of 1000 keys
	// 	if len(keysToDelete) >= 1000 {
	// 		if err := c.client.Del(ctx, keysToDelete...).Err(); err != nil {
	// 			c.log.Error("Redis DEL batch error", "prefix", prefix, "error", err)
	// 		}
	// 		keysToDelete = keysToDelete[:0]
	// 	}
	// }

	// // Delete any remaining keys
	// if len(keysToDelete) > 0 {
	// 	if err := c.client.Del(ctx, keysToDelete...).Err(); err != nil {
	// 		c.log.Error("Redis DEL batch error", "prefix", prefix, "error", err)
	// 	}
	// }

	// if err := iter.Err(); err != nil {
	// 	c.log.Error("Redis SCAN error", "prefix", prefix, "error", err)
	// }
}

// Flush removes all items from the cache
func (c *redisCacheImpl) Flush(ctx context.Context) {
	if c == nil || !c.IsEnabled() {
		return
	}
	if err := c.client.FlushDB(ctx).Err(); err != nil {
		c.log.Error(ctx, "Redis FLUSHDB error", "error", err)
	}
}

// Get value from cache bypassing configuration checks
func (c *redisCacheImpl) ForceCacheGet(ctx context.Context, key string) (interface{}, bool) {
	if c == nil {
		return nil, false
	}

	redisKey := c.GetRedisKey(key)
	value, err := c.client.Get(ctx, redisKey).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			// Key does not exist
			return nil, false
		}
		c.log.Error(ctx, "Redis GET error", "key", redisKey, "error", err)
		return nil, false
	}

	return value, true
}

// ForceCacheGetWithTTL retrieves a value and its remaining TTL from the cache bypassing configuration checks
func (c *redisCacheImpl) ForceCacheGetWithTTL(ctx context.Context, key string) (interface{}, time.Duration, bool) {
	if c == nil {
		return nil, 0, false
	}

	redisKey := c.GetRedisKey(key)
	value, err := c.client.Get(ctx, redisKey).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, 0, false
		}
		c.log.Error(ctx, "Redis GET error", "key", redisKey, "error", err)
		return nil, 0, false
	}

	ttl, err := c.client.TTL(ctx, redisKey).Result()
	if err != nil {
		c.log.Error(ctx, "Redis TTL error", "key", redisKey, "error", err)
		// Still return the value even if TTL lookup fails
		return value, 0, true
	}

	return value, ttl, true
}

// Set value from cache bypassing configuration checks
func (c *redisCacheImpl) ForceCacheSet(ctx context.Context, key string, value interface{}, expiration time.Duration) {
	if c == nil {
		return
	}

	// Use default expiration if none specified
	if expiration == 0 {
		expiration = ExpiryDefaultRedis
	}

	// generate redis key
	redisKey := c.GetRedisKey(key)

	// Convert value to string if it's not already
	var strValue string
	switch v := value.(type) {
	case string:
		strValue = v
	default:
		// Marshal non-string values to JSON
		jsonBytes, err := json.Marshal(value)
		if err != nil {
			c.log.Error(ctx, "Failed to marshal cache value", "key", redisKey, "error", err)
			return
		}
		strValue = string(jsonBytes)
	}

	if err := c.client.Set(ctx, redisKey, strValue, expiration).Err(); err != nil {
		c.log.Error(ctx, "Redis SET error", "key", redisKey, "error", err)
	}
}

func (c *redisCacheImpl) ForceCacheDelete(ctx context.Context, key string) {
	if c == nil {
		return
	}
	redisKey := c.GetRedisKey(key)
	if err := c.client.Unlink(ctx, redisKey).Err(); err != nil {
		c.log.Error(ctx, "Redis UNLINK error", "key", redisKey, "error", err)
	}
}
