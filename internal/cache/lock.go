package cache

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type Locker interface {
	AcquireLock(ctx context.Context, key string, ttl time.Duration) (Lock, error)
}

type Lock interface {
	AcquiredSuccessfully() bool
	Release(ctx context.Context) error
}

type redisLocker struct {
	client redis.UniversalClient
}

func NewRedisLocker(cache RedisCache) Locker {
	redisClient, ok := cache.(*redisCacheImpl)
	if !ok || redisClient == nil {
		return nil
	}

	return &redisLocker{
		client: redisClient.client,
	}
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

func (l *redisLock) Release(ctx context.Context) error {
	_, err := releaseScript.Run(
		ctx,
		l.client,
		[]string{l.key},
		l.token,
	).Result()

	return err
}
