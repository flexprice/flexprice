package cache

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/flexprice/flexprice/internal/logger"
	redisClient "github.com/flexprice/flexprice/internal/redis"
	"github.com/go-redis/redis/v8"
)

const (
	// DeleteRetryDelay specifies how long to wait before retrying a failed delete operation
	DeleteRetryDelay = 100 * time.Millisecond

	// ScanCount determines how many keys to scan at once when using SCAN
	ScanCount = 100
)

// RedisCache implements the Cache interface using Redis
type RedisCache struct {
	client *redis.Client
	log    *logger.Logger
}

// Redis cache instance
var redisCache *RedisCache

// NewRedisCache creates a new Redis cache
func NewRedisCache(client *redisClient.Client, log *logger.Logger) *RedisCache {
	return &RedisCache{
		client: client.GetClient(),
		log:    log,
	}
}

// InitializeRedisCache initializes the global Redis cache instance
func InitializeRedisCache(client *redisClient.Client, log *logger.Logger) {
	if redisCache == nil {
		redisCache = NewRedisCache(client, log)
	}
}

// GetRedisCache returns the global Redis cache instance
func GetRedisCache() *RedisCache {
	return redisCache
}

// Get retrieves a value from the cache
func (c *RedisCache) Get(ctx context.Context, key string) (interface{}, bool) {
	val, err := c.client.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			// Key does not exist
			return nil, false
		}
		c.log.Error("Redis GET error", "key", key, "error", err)
		return nil, false
	}

	// Try to unmarshal as JSON first
	var result interface{}
	if err := json.Unmarshal([]byte(val), &result); err != nil {
		// If it's not valid JSON, return as string
		return val, true
	}

	return result, true
}

// Set adds a value to the cache with the specified expiration
func (c *RedisCache) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) {
	// Use default expiration if none specified
	if expiration == 0 {
		expiration = ExpiryDefaultRedis
	}

	// Convert value to string if it's not already
	var strValue string
	switch v := value.(type) {
	case string:
		strValue = v
	default:
		// Marshal non-string values to JSON
		jsonBytes, err := json.Marshal(value)
		if err != nil {
			c.log.Error("Failed to marshal cache value", "key", key, "error", err)
			return
		}
		strValue = string(jsonBytes)
	}

	if err := c.client.Set(ctx, key, strValue, expiration).Err(); err != nil {
		c.log.Error("Redis SET error", "key", key, "error", err)
	}
}

// Delete removes a key from the cache with retry
func (c *RedisCache) Delete(ctx context.Context, key string) {
	err := c.delete(ctx, key)
	if err != nil {
		c.log.Warn("Redis DELETE failed, retrying...", "key", key, "error", err)

		// Create a new context with timeout for the retry
		retryCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Wait before retrying
		time.Sleep(DeleteRetryDelay)

		// Retry once
		if retryErr := c.delete(retryCtx, key); retryErr != nil {
			c.log.Error("Redis DELETE retry failed", "key", key, "error", retryErr)
		}
	}
}

// delete is a helper function to perform the actual deletion
func (c *RedisCache) delete(ctx context.Context, key string) error {
	return c.client.Del(ctx, key).Err()
}

// DeleteByPrefix removes all keys with the given prefix
func (c *RedisCache) DeleteByPrefix(ctx context.Context, prefix string) {
	// Use SCAN to iterate through keys matching the pattern
	iter := c.client.Scan(ctx, 0, prefix+"*", ScanCount).Iterator()

	var keysToDelete []string

	for iter.Next(ctx) {
		keysToDelete = append(keysToDelete, iter.Val())

		// Delete in batches of 1000 keys
		if len(keysToDelete) >= 1000 {
			if err := c.client.Del(ctx, keysToDelete...).Err(); err != nil {
				c.log.Error("Redis DEL batch error", "prefix", prefix, "error", err)
			}
			keysToDelete = keysToDelete[:0]
		}
	}

	// Delete any remaining keys
	if len(keysToDelete) > 0 {
		if err := c.client.Del(ctx, keysToDelete...).Err(); err != nil {
			c.log.Error("Redis DEL batch error", "prefix", prefix, "error", err)
		}
	}

	if err := iter.Err(); err != nil {
		c.log.Error("Redis SCAN error", "prefix", prefix, "error", err)
	}
}

// Flush removes all items from the cache
func (c *RedisCache) Flush(ctx context.Context) {
	if err := c.client.FlushDB(ctx).Err(); err != nil {
		c.log.Error("Redis FLUSHDB error", "error", err)
	}
}
