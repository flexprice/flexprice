package testutil

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/cache"
)

// InMemoryRedis is a test fake of a Redis-like cache backend. It implements
// cache.Cache for injection via ServiceParams in unit tests where a real
// Redis is unavailable. Each NewInMemoryRedis() returns a fresh instance —
// state is NOT shared across instances, so tests stay isolated.
//
// Modeled after inmemory_kafka.go — keep the fake here in testutil rather
// than in the cache package so production cache code stays free of test-only
// constructors.
type InMemoryRedis struct {
	mu     sync.RWMutex
	values map[string]inMemoryRedisEntry
}

type inMemoryRedisEntry struct {
	value      interface{}
	expiration time.Time // zero ⇒ no expiration
}

// NewInMemoryRedis returns a fresh isolated in-memory cache for tests.
func NewInMemoryRedis() cache.RedisCache {
	return &InMemoryRedis{values: make(map[string]inMemoryRedisEntry)}
}

func (r *InMemoryRedis) IsEnabled() bool {
	return true
}

func (r *InMemoryRedis) IsRedisCache() bool {
	return true
}

// Get returns the cached value if present and not expired.
func (r *InMemoryRedis) Get(_ context.Context, key string) (interface{}, bool) {
	return r.get(key)
}

// ForceCacheGet mirrors Get for the bypass-config-checks variant.
func (r *InMemoryRedis) ForceCacheGet(_ context.Context, key string) (interface{}, bool) {
	return r.get(key)
}

// ForceCacheGetWithTTL returns the cached value plus its remaining TTL.
// A returned ttl of 0 means "found but no expiration set" (matches the
// RedisCache semantics where an unbounded entry has no TTL).
func (r *InMemoryRedis) ForceCacheGetWithTTL(_ context.Context, key string) (interface{}, time.Duration, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.values[key]
	if !ok {
		return nil, 0, false
	}
	if !e.expiration.IsZero() && time.Now().After(e.expiration) {
		return nil, 0, false
	}
	var ttl time.Duration
	if !e.expiration.IsZero() {
		ttl = time.Until(e.expiration)
		if ttl < 0 {
			ttl = 0
		}
	}
	return e.value, ttl, true
}

// Set stores a value with the given expiration.
func (r *InMemoryRedis) Set(_ context.Context, key string, value interface{}, expiration time.Duration) {
	r.set(key, value, expiration)
}

// ForceCacheSet mirrors Set for the bypass-config-checks variant.
func (r *InMemoryRedis) ForceCacheSet(_ context.Context, key string, value interface{}, expiration time.Duration) {
	r.set(key, value, expiration)
}

// Delete removes a key.
func (r *InMemoryRedis) Delete(_ context.Context, key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.values, key)
}

// DeleteByPrefix removes every key with the given prefix.
func (r *InMemoryRedis) DeleteByPrefix(_ context.Context, prefix string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k := range r.values {
		if strings.HasPrefix(k, prefix) {
			delete(r.values, k)
		}
	}
}

// Flush empties the cache.
func (r *InMemoryRedis) Flush(_ context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values = make(map[string]inMemoryRedisEntry)
}

func (r *InMemoryRedis) get(key string) (interface{}, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.values[key]
	if !ok {
		return nil, false
	}
	if !e.expiration.IsZero() && time.Now().After(e.expiration) {
		return nil, false
	}
	return e.value, true
}

func (r *InMemoryRedis) set(key string, value interface{}, expiration time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var exp time.Time
	if expiration > 0 {
		exp = time.Now().Add(expiration)
	}
	r.values[key] = inMemoryRedisEntry{value: value, expiration: exp}
}

func (r *InMemoryRedis) TrySetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.values[key]; ok {
		if !r.values[key].expiration.IsZero() && time.Now().After(r.values[key].expiration) {
			delete(r.values, key)
		} else {
			return false, nil
		}
	}
	var exp time.Time
	if expiration > 0 {
		exp = time.Now().Add(expiration)
	}
	r.values[key] = inMemoryRedisEntry{value: value, expiration: exp}
	return true, nil
}

func (r *InMemoryRedis) TrySetNXWithTTL(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, time.Duration, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.values[key]; ok {
		if !r.values[key].expiration.IsZero() && time.Now().After(r.values[key].expiration) {
			delete(r.values, key)
		} else {
			return false, time.Until(r.values[key].expiration), nil
		}
	}
	var exp time.Time
	if expiration > 0 {
		exp = time.Now().Add(expiration)
	}
	r.values[key] = inMemoryRedisEntry{value: value, expiration: exp}
	return true, expiration, nil
}

func (r *InMemoryRedis) ForceCacheDelete(ctx context.Context, key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.values, key)
}
