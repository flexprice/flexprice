package cache

import (
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/redis"
)

// CacheType represents the type of cache to use
type CacheType string

const (
	// CacheTypeInMemory represents an in-memory cache
	CacheTypeInMemory CacheType = "inmemory"

	// CacheTypeRedis represents a Redis-backed cache
	CacheTypeRedis CacheType = "redis"
)

// Initialize initializes the cache system based on the specified type
func Initialize(cacheType string, log *logger.Logger, redisClient *redis.Client) Cache {
	log.Info("Initializing cache system", "type", cacheType)

	var cache Cache

	switch CacheType(cacheType) {
	case CacheTypeRedis:
		if redisClient == nil {
			log.Error("Redis client is nil, falling back to in-memory cache")
			InitializeInMemoryCache()
			cache = GetInMemoryCache()
		} else {
			InitializeRedisCache(redisClient, log)
			cache = GetRedisCache()
		}
	case CacheTypeInMemory:
		fallthrough
	default:
		InitializeInMemoryCache()
		cache = GetInMemoryCache()
	}

	log.Info("Cache system initialized", "type", cacheType)
	return cache
}
