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
func (c *inMemoryCache) Get(_ context.Context, key string) (interface{}, bool) {
	if c == nil || !c.IsEnabled() {
		return nil, false
	}
	return c.cache.Get(key)
}

func (c *inMemoryCache) ForceCacheGet(ctx context.Context, key string) (interface{}, bool) {
	if c == nil {
		return nil, false
	}
	return c.cache.Get(key)
}

func (c *inMemoryCache) ForceCacheSet(ctx context.Context, key string, value interface{}, expiration time.Duration) {
	if c == nil {
		return
	}
	c.cache.Set(key, value, expiration)
}

func (c *inMemoryCache) ForceCacheDelete(_ context.Context, key string) {
	if c == nil {
		return
	}
	c.cache.Delete(key)
}

// Set adds a value to the cache with the specified expiration
func (c *inMemoryCache) Set(_ context.Context, key string, value interface{}, expiration time.Duration) {
	if c == nil || !c.IsEnabled() {
		return
	}
	c.cache.Set(key, value, expiration)
}

// Delete removes a key from the cache
func (c *inMemoryCache) Delete(_ context.Context, key string) {
	if c == nil || !c.IsEnabled() {
		return
	}
	c.cache.Delete(key)
}

// DeleteByPrefix removes all keys with the given prefix
func (c *inMemoryCache) DeleteByPrefix(_ context.Context, prefix string) {
	if c == nil || !c.IsEnabled() {
		return
	}
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
func (c *inMemoryCache) Flush(_ context.Context) {
	if c == nil || !c.IsEnabled() {
		return
	}
	c.cache.Flush()
}
