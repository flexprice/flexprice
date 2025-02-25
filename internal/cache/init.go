package cache

import (
	"github.com/flexprice/flexprice/internal/logger"
)

// Initialize initializes the cache system
func Initialize(log *logger.Logger) *InMemoryCache {
	log.Info("Initializing cache system")

	// Initialize the global cache instance
	InitializeInMemoryCache()

	log.Info("Cache system initialized")

	// Return the cache instance
	return GetInMemoryCache()
}
