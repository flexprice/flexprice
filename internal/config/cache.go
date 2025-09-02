package config

import (
	"time"
)

// CacheConfig holds configuration for the cache system
type CacheConfig struct {
	// Type specifies the type of cache to use (inmemory or redis)
	Type string `mapstructure:"type" default:"inmemory"`

	// Redis holds Redis-specific configuration
	Redis RedisConfig `mapstructure:"redis"`
}

// RedisConfig holds configuration for Redis
type RedisConfig struct {
	// Enabled specifies whether Redis is enabled
	Enabled bool `mapstructure:"enabled" default:"false"`

	// Host is the Redis server hostname
	Host string `mapstructure:"host" default:"localhost"`

	// Port is the Redis server port
	Port int `mapstructure:"port" default:"6379"`

	// Password for Redis authentication (optional)
	Password string `mapstructure:"password" default:""`

	// DB is the Redis database number
	DB int `mapstructure:"db" default:"0"`

	// UseTLS enables TLS for Redis connection
	UseTLS bool `mapstructure:"use_tls" default:"false"`

	// PoolSize sets the maximum number of connections in the pool
	PoolSize int `mapstructure:"pool_size" default:"10"`

	// Timeout for Redis operations
	Timeout time.Duration `mapstructure:"timeout" default:"5s"`
}

// GetCacheType returns the cache type as a string
func (c *CacheConfig) GetCacheType() string {
	if c.Type == "redis" && c.Redis.Enabled {
		return "redis"
	}
	return "inmemory"
}

// ToRedisClientConfig converts RedisConfig to a map for redis.Config
func (c *RedisConfig) ToRedisClientConfig() map[string]interface{} {
	return map[string]interface{}{
		"Host":     c.Host,
		"Port":     c.Port,
		"Password": c.Password,
		"DB":       c.DB,
		"UseTLS":   c.UseTLS,
		"PoolSize": c.PoolSize,
		"Timeout":  c.Timeout,
	}
}
