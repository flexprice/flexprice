package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/samber/lo"
)

type Locker interface {
	AcquireLock(ctx context.Context, key string, ttl time.Duration) (Lock, error)
}

// KeyReleaser drops lock keys whoever holds them. Neither existing path can:
// Lock.Release deletes only if the key still holds the token its own AcquireLock
// minted, and RedisCache.Delete prefixes the key with Redis.KeyPrefix while
// AcquireLock writes it raw, so the two address different keys.
//
// Bulk event deletion clears the per-event deduplication locks for the ids it
// deleted, which a consumer process took, so a re-ingest under the same id
// inside the dedup window is not skipped. Kept off Locker so implementations
// that never release a key they do not hold need not carry the method.
type KeyReleaser interface {
	ReleaseKeys(ctx context.Context, keys ...string) error
}

type Lock interface {
	AcquiredSuccessfully() bool
	Release(ctx context.Context) error
}

// It returns a nil Lock when locker is nil, and a Lock reporting AcquiredSuccessfully() ==
// false when every attempt was exhausted; a non-nil error means the locker itself failed.
// Callers decide whether to skip the work or proceed unserialized.
func AcquireLockWithRetry(
	ctx context.Context,
	locker Locker,
	key string,
	ttl time.Duration,
	attempts int,
	retryInterval time.Duration,
) (Lock, error) {
	if locker == nil {
		return nil, nil
	}

	var lock Lock
	for attempt := 0; attempt < attempts; attempt++ {
		var err error
		lock, err = locker.AcquireLock(ctx, key, ttl)
		if err != nil {
			return nil, err
		}
		if lock.AcquiredSuccessfully() {
			return lock, nil
		}
		if attempt == attempts-1 {
			break
		}

		select {
		case <-ctx.Done():
			return lock, ctx.Err()
		case <-time.After(retryInterval):
		}
	}

	return lock, nil
}

type redisLocker struct {
	client redis.UniversalClient
}

func NewRedisLocker(cache RedisCache, logger *logger.Logger) (Locker, error) {
	// Cache is nil only when the Redis client could not be built (invalid config);
	// an unreachable Redis still yields a reconnecting client.
	if cache == nil {
		logger.Info(context.Background(), "NewRedisLocker: Cache is nil, returning nil Locker")
		return nil, nil
	}
	redisClient, ok := cache.(*redisCacheImpl)
	if !ok {
		return nil, fmt.Errorf("NewRedisLocker: unsupported RedisCache implementation %T", cache)
	}
	if redisClient == nil || redisClient.client == nil {
		logger.Info(context.Background(), "NewRedisLocker: Redis client is nil, returning nil Locker")
		return nil, nil
	}

	logger.Info(context.Background(), "NewRedisLocker: Initialized Redis Locker")
	return &redisLocker{
		client: redisClient.client,
	}, nil
}

type redisLock struct {
	client  redis.UniversalClient
	key     string
	token   string
	success bool
}

var releaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
else
	return 0
end
`)

func (l *redisLocker) AcquireLock(ctx context.Context, key string, ttl time.Duration) (Lock, error) {
	token := uuid.NewString()

	ok, err := l.client.SetNX(ctx, key, token, ttl).Result()
	if err != nil {
		return nil, err
	}

	return &redisLock{
		client:  l.client,
		key:     key,
		token:   token,
		success: ok,
	}, nil
}

func (l *redisLock) AcquiredSuccessfully() bool {
	return l.success
}

// Release detaches from ctx cancellation so a deferred release still runs after the caller's deadline.
func (l *redisLock) Release(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	_, err := releaseScript.Run(
		ctx,
		l.client,
		[]string{l.key},
		l.token,
	).Result()

	return err
}

// ReleaseKeys unlinks the given lock keys whoever holds them. Detached from ctx
// cancellation for the same reason as Release.
//
// One UNLINK per key, pipelined: a cluster client routes a command by its first
// key, so a multi-key UNLINK spanning hash slots is refused with CROSSSLOT.
func (l *redisLocker) ReleaseKeys(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()

	for _, chunk := range lo.Chunk(keys, 1000) {
		pipe := l.client.Pipeline()
		for _, key := range chunk {
			pipe.Unlink(ctx, key)
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}
