package cache

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	goCache "github.com/patrickmn/go-cache"
)

// DefaultExpiration is the default expiration time for cache entries
const DefaultExpiration = 2 * time.Minute

// DefaultCleanupInterval is how often expired items are removed from the cache
const DefaultCleanupInterval = 1 * time.Hour

// inMemoryCache implements the Cache interface using github.com/patrickmn/go-cache
type inMemoryCache struct {
	cache *goCache.Cache
	cfg   *config.Configuration
}

// Global cache instance
var globalCache *inMemoryCache

// InitializeInMemoryCache initializes the global cache instance
func InitializeInMemoryCache() InMemoryCache {
	cfg, err := config.NewConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if globalCache == nil {
		globalCache = &inMemoryCache{
			cache: goCache.New(DefaultExpiration, DefaultCleanupInterval),
			cfg:   cfg,
		}
	}
	return globalCache
}

// NewInMemoryCache creates a new inMemoryCache instance
func NewInMemoryCache() InMemoryCache {
	if globalCache == nil {
		InitializeInMemoryCache()
	}
	return globalCache
}

func (c *inMemoryCache) IsEnabled() bool {
	return c.cfg.Cache.Enabled && c.cfg.Cache.InMemory.Enabled
}

// GetCache returns the global cache instance
func GetInMemoryCache() InMemoryCache {
	if globalCache == nil {
		InitializeInMemoryCache()
	}
	return globalCache
}

// Get retrieves a value from the cache
func (c *inMemoryCache) Get(ctx context.Context, key string) (interface{}, bool) {
	if c == nil || !c.IsEnabled() {
		return nil, false
	}
	if ctx.Err() != nil {
		return nil, false
	}
	_, span := startCacheSpan(ctx, storageSpansEnabled(c.cfg), "get", SourceInMemory, key)
	defer endCacheSpan(span)
	value, found := c.cache.Get(key)
	setCacheHit(span, found)
	if found {
		RecordHit(ctx, entityFromKey(key), SourceInMemory)
	} else {
		RecordMiss(ctx, entityFromKey(key), SourceInMemory)
	}
	return value, found
}

func (c *inMemoryCache) ForceCacheGet(ctx context.Context, key string) (interface{}, bool) {
	if c == nil {
		return nil, false
	}
	if ctx.Err() != nil {
		return nil, false
	}
	return c.cache.Get(key)
}

func (c *inMemoryCache) ForceCacheSet(ctx context.Context, key string, value interface{}, expiration time.Duration) {
	if c == nil {
		return
	}
	if ctx.Err() != nil {
		return
	}
	c.cache.Set(key, value, expiration)
}

func (c *inMemoryCache) ForceCacheDelete(ctx context.Context, key string) {
	if c == nil {
		return
	}
	if ctx.Err() != nil {
		return
	}
	c.cache.Delete(key)
}

// Set adds a value to the cache with the specified expiration
func (c *inMemoryCache) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) {
	if c == nil || !c.IsEnabled() {
		return
	}
	if ctx.Err() != nil {
		return
	}
	_, span := startCacheSpan(ctx, storageSpansEnabled(c.cfg), "set", SourceInMemory, key)
	defer endCacheSpan(span)
	c.cache.Set(key, value, expiration)
	RecordSet(ctx, entityFromKey(key), SourceInMemory)
}

// Delete removes a key from the cache
func (c *inMemoryCache) Delete(ctx context.Context, key string) {
	if c == nil || !c.IsEnabled() {
		return
	}
	if ctx.Err() != nil {
		return
	}
	_, span := startCacheSpan(ctx, storageSpansEnabled(c.cfg), "delete", SourceInMemory, key)
	defer endCacheSpan(span)
	c.cache.Delete(key)
	RecordDelete(ctx, entityFromKey(key), SourceInMemory)
}

// DeleteByPrefix removes all keys with the given prefix
func (c *inMemoryCache) DeleteByPrefix(ctx context.Context, prefix string) {
	if c == nil || !c.IsEnabled() {
		return
	}
	if ctx.Err() != nil {
		return
	}
	_, span := startCacheSpan(ctx, storageSpansEnabled(c.cfg), "delete_by_prefix", SourceInMemory, prefix)
	defer endCacheSpan(span)
	// Get all items from the cache
	items := c.cache.Items()

	// Delete items with matching prefix
	for k := range items {
		if strings.HasPrefix(k, prefix) {
			c.cache.Delete(k)
		}
	}
	RecordDelete(ctx, entityFromKey(prefix), SourceInMemory)
}

// Flush removes all items from the cache
func (c *inMemoryCache) Flush(ctx context.Context) {
	if c == nil || !c.IsEnabled() {
		return
	}
	c.cache.Flush()
}
