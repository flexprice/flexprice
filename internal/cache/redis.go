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

// RedisCache implements the Cache interface using Redis
type RedisCache struct {
	client *redis.ClusterClient
	config *config.Configuration
}

// Redis cache instance
var redisCache *RedisCache

// NewRedisCache creates a new Redis cache
func NewRedisCache() *RedisCache {
	if redisCache == nil {
		InitializeRedisCache()
	}
	return redisCache
}

// InitializeRedisCache initializes the global Redis cache instance
func InitializeRedisCache() {
	config, err := config.NewConfig()
	if err != nil {
		fmt.Println("Failed to initialize Redis cache", "error", err)
		return
	}
	if redisCache == nil {
		redisClient, err := redisClient.NewClient(config, logger.GetLogger())
		if err != nil {
			fmt.Println("Failed to create Redis client", "error", err)
			return
		}
		redisCache = &RedisCache{
			client: redisClient.GetClient(),
			config: config,
		}
	}
}

// GetRedisCache returns the global Redis cache instance
func GetRedisCache() *RedisCache {
	if redisCache == nil {
		InitializeRedisCache()
	}
	return redisCache
}

// Helper function to add prefix to key
func (c *RedisCache) GetRedisKey(key string) string {
	if c.config.Redis.KeyPrefix == "" {
		return key
	}
	return c.config.Redis.KeyPrefix + ":" + key
}

// Get retrieves a value from the cache
func (c *RedisCache) Get(ctx context.Context, key string) (interface{}, bool) {

	if !c.config.Cache.Enabled {
		fmt.Println("Cache is disabled")
		return nil, false
	}

	redisKey := c.GetRedisKey(key)

	value, err := c.client.Get(ctx, redisKey).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			// Key does not exist
			return nil, false
		}
		fmt.Print("Redis GET error", "key", redisKey, "error", err)
		return nil, false
	}

	return value, true
}

// Set adds a value to the cache with the specified expiration
func (c *RedisCache) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) {

	if !c.config.Cache.Enabled {
		fmt.Println("Cache is disabled")
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
			fmt.Println("Failed to marshal cache value", "key", redisKey, "error", err)
			return
		}
		strValue = string(jsonBytes)
	}

	if err := c.client.Set(ctx, redisKey, strValue, expiration).Err(); err != nil {
		fmt.Println("Redis SET error", "key", redisKey, "error", err)
	}
}

// Delete removes a key from the cache with retry
func (c *RedisCache) Delete(ctx context.Context, key string) {

	redisKey := c.GetRedisKey(key)
	err := c.delete(ctx, redisKey)
	if err != nil {
		fmt.Println("Redis DELETE failed, retrying...", "key", redisKey, "error", err)

		// Create a new context with timeout for the retry
		retryCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Wait before retrying
		time.Sleep(DeleteRetryDelay)

		// Retry once
		if retryErr := c.delete(retryCtx, redisKey); retryErr != nil {
			fmt.Println("Redis DELETE retry failed", "key", redisKey, "error", retryErr)
		}
	}
}

// delete is a helper function to perform the actual deletion
func (c *RedisCache) delete(ctx context.Context, key string) error {
	return c.client.Del(ctx, key).Err()
}

// DeleteByPrefix removes all keys with the given prefix
func (c *RedisCache) DeleteByPrefix(ctx context.Context, prefix string) {

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
func (c *RedisCache) Flush(ctx context.Context) {
	if err := c.client.FlushDB(ctx).Err(); err != nil {
		fmt.Println("Redis FLUSHDB error", "error", err)
	}
}

// Get value from cache bypassing configuration checks
func (c *RedisCache) ForceCacheGet(ctx context.Context, key string) (interface{}, bool) {
	redisKey := c.GetRedisKey(key)
	value, err := c.client.Get(ctx, redisKey).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			// Key does not exist
			return nil, false
		}
		fmt.Println("Redis GET error", "key", redisKey, "error", err)
		return nil, false
	}

	return value, true
}

// Set value from cache bypassing configuration checks
func (c *RedisCache) ForceCacheSet(ctx context.Context, key string, value interface{}, expiration time.Duration) {
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
			fmt.Println("Failed to marshal cache value", "key", redisKey, "error", err)
			return
		}
		strValue = string(jsonBytes)
	}

	if err := c.client.Set(ctx, redisKey, strValue, expiration).Err(); err != nil {
		fmt.Println("Redis SET error", "key", redisKey, "error", err)
	}
}
