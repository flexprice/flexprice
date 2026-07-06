package testutil

import (
	"context"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/cache"
	"github.com/google/uuid"
)

func NewInMemoryRedisLocker(redis *InMemoryRedis) cache.Locker {
	return &inMemoryRedisLocker{
		redis:  redis,
		values: make(map[string]inMemoryRedisEntry),
	}
}

type inMemoryRedisLocker struct {
	redis  *InMemoryRedis
	mu     sync.Mutex
	values map[string]inMemoryRedisEntry
}

func (r *inMemoryRedisLocker) AcquireLock(ctx context.Context, key string, expiration time.Duration) (cache.Lock, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if lock exists and is valid
	if entry, ok := r.values[key]; ok {
		// If expired, delete it
		if !entry.expiration.IsZero() && time.Now().After(entry.expiration) {
			delete(r.values, key)
		} else {
			// Lock is held by someone else
			return &inMemoryRedisLock{
				key:        key,
				value:      entry.value.(string),
				expiration: entry.expiration,
				success:    false,
			}, nil
		}
	}

	// Create new lock
	token := uuid.NewString()

	var exp time.Time
	if expiration > 0 {
		exp = time.Now().Add(expiration)
	}

	r.values[key] = inMemoryRedisEntry{
		value:      token,
		expiration: exp,
	}

	return &inMemoryRedisLock{
		key:        key,
		value:      token,
		expiration: exp,
		success:    true,
	}, nil
}

type inMemoryRedisLock struct {
	key        string
	value      string
	expiration time.Time
	expiryOnce sync.Once
	success    bool
	released   bool
	mu         sync.Mutex
}

func (l *inMemoryRedisLock) Key() string {
	return l.key
}

func (l *inMemoryRedisLock) Value() string {
	return l.value
}

func (l *inMemoryRedisLock) AcquiredSuccessfully() bool {
	return l.success
}

func (l *inMemoryRedisLock) Release(ctx context.Context) error {
	if l.released {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.released {
		return nil
	}

	l.released = true
	return nil
}
