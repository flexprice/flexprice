package cache

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	goCache "github.com/patrickmn/go-cache"
)

// DefaultExpiration is the default expiration time for cache entries
const DefaultExpiration = 30 * time.Minute

// DefaultCleanupInterval is how often expired items are removed from the cache
const DefaultCleanupInterval = 1 * time.Hour

// InMemoryCache implements the Cache interface using github.com/patrickmn/go-cache
type InMemoryCache struct {
	cache *goCache.Cache
	cfg   *config.Configuration
}

// Global cache instance
var globalCache *InMemoryCache

// InitializeInMemoryCache initializes the global cache instance
func InitializeInMemoryCache() {
	cfg, err := config.NewConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if globalCache == nil {
		globalCache = &InMemoryCache{
			cache: goCache.New(DefaultExpiration, DefaultCleanupInterval),
			cfg:   cfg,
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
	if !c.cfg.Cache.Enabled {
		fmt.Println("Cache is disabled, returning nil")
		return nil, false
	}
	return c.cache.Get(key)
}

func (c *InMemoryCache) ForceCacheGet(ctx context.Context, key string) (interface{}, bool) {
	return c.cache.Get(key)
}

func (c *InMemoryCache) ForceCacheSet(ctx context.Context, key string, value interface{}, expiration time.Duration) {
	c.cache.Set(key, value, expiration)
}

// Set adds a value to the cache with the specified expiration
func (c *InMemoryCache) Set(_ context.Context, key string, value interface{}, expiration time.Duration) {
	if !c.cfg.Cache.Enabled {
		fmt.Println("Cache is disabled")
		return
	}
	c.cache.Set(key, value, expiration)
}

// Delete removes a key from the cache
func (c *InMemoryCache) Delete(_ context.Context, key string) {
	if !c.cfg.Cache.Enabled {
		fmt.Println("Cache is disabled, returning nil")
		return
	}
	c.cache.Delete(key)
}

// DeleteByPrefix removes all keys with the given prefix
func (c *InMemoryCache) DeleteByPrefix(_ context.Context, prefix string) {
	if !c.cfg.Cache.Enabled {
		fmt.Println("Cache is disabled")
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
func (c *InMemoryCache) Flush(_ context.Context) {
	if !c.cfg.Cache.Enabled {
		fmt.Println("Cache is disabled")
		return
	}
	c.cache.Flush()
}
