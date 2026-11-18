package cache

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/usagerollup"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/redis/go-redis/v9"
)

const usageRollupHourLayout = "2006010215"

type usageRollupStore struct {
	client    redis.UniversalClient
	keyPrefix string
}

// NewUsageRollupStore returns the Redis-backed usagerollup.Store. Without Redis it returns nil,
// or an error when usage rollups are enabled, since they cannot run without it.
func NewUsageRollupStore(cfg *config.Configuration, cache RedisCache, log *logger.Logger) (usagerollup.Store, error) {
	if err := cfg.ValidateUsageRollup(); err != nil {
		return nil, err
	}
	impl, _ := cache.(*redisCacheImpl)
	if impl == nil || impl.client == nil {
		if cfg.UsageRollup.IsEnabledForAnyTenant() {
			return nil, fmt.Errorf("usage_rollup is enabled but Redis is not available: fix the redis config or set usage_rollup.enabled=false and usage_rollup.enabled_tenants=[]")
		}
		log.Info(context.Background(), "NewUsageRollupStore: Redis client is nil, returning nil store")
		return nil, nil
	}
	return &usageRollupStore{client: impl.client, keyPrefix: impl.config.Redis.KeyPrefix}, nil
}

// usageRollupKeys names the Redis keys for one bucket. The {tenant:env:customer} hash tag
// keeps all of a customer's keys on one cluster slot so a read is one round trip.
type usageRollupKeys struct {
	qty, max, active, field string
}

func newUsageRollupKeys(keyPrefix, tenantID, environmentID string, b usagerollup.Bucket) usageRollupKeys {
	prefix := ""
	if keyPrefix != "" {
		prefix = keyPrefix + ":"
	}
	hour := b.Hour.UTC().Format(usageRollupHourLayout)
	customerTag := fmt.Sprintf("{%s:%s:%s}", tenantID, environmentID, b.ExternalCustomerID)
	environmentTag := fmt.Sprintf("{%s:%s}", tenantID, environmentID)
	return usageRollupKeys{
		qty:    prefix + "qty:" + customerTag + ":" + hour,
		max:    prefix + "max:" + customerTag + ":" + hour,
		active: prefix + "active:" + environmentTag + ":" + hour,
		field:  usageRollupField(b),
	}
}

// usageRollupField carries the aggregation in the field name so readers merge without loading the meter.
func usageRollupField(b usagerollup.Bucket) string {
	if b.Aggregation == types.AggregationCount {
		return "count:" + b.MeterID
	}
	return "sum:" + b.MeterID
}

func (s *usageRollupStore) Increment(ctx context.Context, buckets []usagerollup.Bucket) error {
	if len(buckets) == 0 {
		return nil
	}
	tenantID, environmentID := types.GetTenantID(ctx), types.GetEnvironmentID(ctx)
	if tenantID == "" || environmentID == "" {
		return fmt.Errorf("usage rollup increment: tenant and environment are required")
	}

	// Not a TxPipeline: a batch spans many slots and cluster MULTI rejects that. Each write is
	// followed by its EXPIRE on the same connection, and every later write re-applies it.
	pipe := s.client.Pipeline()
	for _, b := range buckets {
		keys := newUsageRollupKeys(s.keyPrefix, tenantID, environmentID, b)
		if b.Aggregation == types.AggregationMax {
			pipe.ZAddArgs(ctx, keys.max, redis.ZAddArgs{GT: true, Members: []redis.Z{{Score: b.Quantity.InexactFloat64(), Member: b.MeterID}}})
			pipe.Expire(ctx, keys.max, usagerollup.BucketTTL)
		} else {
			// Sent as a string so Redis parses the exact decimal instead of a rounded float64.
			pipe.Do(ctx, "HINCRBYFLOAT", keys.qty, keys.field, b.Quantity.String())
			pipe.Expire(ctx, keys.qty, usagerollup.BucketTTL)
		}
		pipe.SAdd(ctx, keys.active, b.ExternalCustomerID)
		pipe.Expire(ctx, keys.active, usagerollup.ActiveSetTTL)
	}

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("usage rollup increment: %w", err)
	}
	return nil
}
