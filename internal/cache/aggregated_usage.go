package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/aggregatedusage"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/redis/go-redis/v9"
)

const aggregatedUsageHourLayout = "2006010215"

type aggregatedUsageStore struct {
	client    redis.UniversalClient
	keyPrefix string
}

// NewAggregatedUsageStore returns the Redis-backed aggregatedusage.Store. Without Redis it returns nil,
// or an error when aggregated usage is enabled, since it cannot run without it.
func NewAggregatedUsageStore(cfg *config.Configuration, cache RedisCache, log *logger.Logger) (aggregatedusage.Store, error) {
	if err := cfg.ValidateAggregatedUsage(); err != nil {
		return nil, err
	}
	impl, _ := cache.(*redisCacheImpl)
	if impl == nil || impl.client == nil {
		if cfg.AggregatedUsage.IsEnabledForAnyTenant() {
			return nil, fmt.Errorf("aggregated_usage is enabled but Redis is not available: fix the redis config or set aggregated_usage.enabled=false and aggregated_usage.enabled_tenants=[]")
		}
		log.Info(context.Background(), "NewAggregatedUsageStore: Redis client is nil, returning nil store")
		return nil, nil
	}
	return &aggregatedUsageStore{client: impl.client, keyPrefix: impl.config.Redis.KeyPrefix}, nil
}

// aggregatedUsageKeys names the Redis keys for one bucket. The {tenant:env:customer} hash tag
// keeps all of a customer's keys on one cluster slot so a read is one round trip.
type aggregatedUsageKeys struct {
	qty, max, active, field string
}

func newAggregatedUsageKeys(keyPrefix, tenantID, environmentID string, b aggregatedusage.Bucket) aggregatedUsageKeys {
	prefix := ""
	if keyPrefix != "" {
		prefix = keyPrefix + ":"
	}
	hour := b.Hour.UTC().Format(aggregatedUsageHourLayout)
	customerTag := fmt.Sprintf("{%s:%s:%s}", tenantID, environmentID, b.ExternalCustomerID)
	environmentTag := fmt.Sprintf("{%s:%s}", tenantID, environmentID)
	return aggregatedUsageKeys{
		qty:    prefix + "qty:" + customerTag + ":" + hour,
		max:    prefix + "max:" + customerTag + ":" + hour,
		active: prefix + "active:" + environmentTag + ":" + hour,
		field:  aggregatedUsageField(b),
	}
}

// aggregatedUsageField carries the aggregation in the field name so readers merge without loading the meter.
func aggregatedUsageField(b aggregatedusage.Bucket) string {
	if b.Aggregation == types.AggregationCount {
		return "count:" + b.MeterID
	}
	return "sum:" + b.MeterID
}

func (s *aggregatedUsageStore) Increment(ctx context.Context, buckets []aggregatedusage.Bucket) error {
	if len(buckets) == 0 {
		return nil
	}
	tenantID, environmentID := types.GetTenantID(ctx), types.GetEnvironmentID(ctx)
	if tenantID == "" || environmentID == "" {
		return fmt.Errorf("aggregated usage increment: tenant and environment are required")
	}

	// Not a TxPipeline: a batch spans many slots and cluster MULTI rejects that. Each write is
	// followed by its EXPIRE on the same connection, and every later write re-applies it.
	pipe := s.client.Pipeline()
	for _, b := range buckets {
		keys := newAggregatedUsageKeys(s.keyPrefix, tenantID, environmentID, b)
		if b.Aggregation == types.AggregationMax {
			pipe.ZAddArgs(ctx, keys.max, redis.ZAddArgs{GT: true, Members: []redis.Z{{Score: b.Quantity.InexactFloat64(), Member: b.MeterID}}})
			pipe.Expire(ctx, keys.max, aggregatedusage.BucketTTL)
		} else {
			// Sent as a string so Redis parses the exact decimal instead of a rounded float64.
			pipe.Do(ctx, "HINCRBYFLOAT", keys.qty, keys.field, b.Quantity.String())
			pipe.Expire(ctx, keys.qty, aggregatedusage.BucketTTL)
		}
		pipe.SAdd(ctx, keys.active, b.ExternalCustomerID)
		pipe.Expire(ctx, keys.active, aggregatedusage.ActiveSetTTL)
	}

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("aggregated usage increment: %w", err)
	}
	return nil
}

func (s *aggregatedUsageStore) ActiveCustomers(ctx context.Context, hour time.Time) ([]string, error) {
	tenantID, environmentID := types.GetTenantID(ctx), types.GetEnvironmentID(ctx)
	if tenantID == "" || environmentID == "" {
		return nil, fmt.Errorf("aggregated usage active customers: tenant and environment are required")
	}
	key := newAggregatedUsageKeys(s.keyPrefix, tenantID, environmentID, aggregatedusage.Bucket{Hour: hour}).active
	customers, err := s.client.SMembers(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("aggregated usage active customers: %w", err)
	}
	return customers, nil
}
