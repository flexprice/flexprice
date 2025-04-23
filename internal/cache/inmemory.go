package cache

import (
	"context"
	"strings"
	"time"

	goCache "github.com/patrickmn/go-cache"
)

// DefaultExpiration is the default expiration time for cache entries
const DefaultExpiration = 30 * time.Minute

// DefaultCleanupInterval is how often expired items are removed from the cache
const DefaultCleanupInterval = 1 * time.Hour

// InMemoryCache implements the Cache interface using github.com/patrickmn/go-cache
type InMemoryCache struct {
	cache *goCache.Cache
}

// Global cache instance
var globalCache *InMemoryCache

// InitializeInMemoryCache initializes the global cache instance
func InitializeInMemoryCache() {
	if globalCache == nil {
		globalCache = &InMemoryCache{
			cache: goCache.New(DefaultExpiration, DefaultCleanupInterval),
		}
	}
}

// NewInMemoryCache creates a new InMemoryCache instance
func NewInMemoryCache() Cache {
	if globalCache == nil {
		InitializeInMemoryCache()
	}
	return globalCache
}

// GetCache returns the global cache instance
func GetInMemoryCache() *InMemoryCache {
	if globalCache == nil {
		InitializeInMemoryCache()
	}
	return globalCache
}

// Get retrieves a value from the cache
func (c *InMemoryCache) Get(_ context.Context, key string) (interface{}, bool) {
	return c.cache.Get(key)
}

// Set adds a value to the cache with the specified expiration
func (c *InMemoryCache) Set(_ context.Context, key string, value interface{}, expiration time.Duration) {
	c.cache.Set(key, value, expiration)
}

// Delete removes a key from the cache
func (c *InMemoryCache) Delete(_ context.Context, key string) {
	c.cache.Delete(key)
}

// DeleteByPrefix removes all keys with the given prefix
func (c *InMemoryCache) DeleteByPrefix(_ context.Context, prefix string) {
	// Get all items from the cache
	items := c.cache.Items()

	// Delete items with matching prefix
	for k := range items {
		if strings.HasPrefix(k, prefix) {
			c.cache.Delete(k)
		}
	}
}

// Flush removes all items from the cache
func (c *InMemoryCache) Flush(_ context.Context) {
	c.cache.Flush()
}
