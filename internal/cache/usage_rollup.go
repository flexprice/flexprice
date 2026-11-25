package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/usagerollup"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/redis/go-redis/v9"
)

const (
	usageRollupHourLayout = "2006010215"

	// activeSetTTL outlives the checkpoint run that reads an hour's active set, which fires 1h + LateWindow
	// + CutoffGrace after the hour starts; the TTL may date from the hour's first write.
	activeSetTTL = 2*usagerollup.LateWindow + usagerollup.CutoffGrace + time.Hour
)

type hourlyUsageRepository struct {
	client    redis.UniversalClient
	keyPrefix string
}

// NewHourlyUsageRepository returns the Redis-backed usagerollup.Repository. Without Redis it returns nil,
// or an error when usage rollup is enabled, since it cannot run without it.
func NewHourlyUsageRepository(cfg *config.Configuration, cache RedisCache, log *logger.Logger) (usagerollup.Repository, error) {
	if err := cfg.ValidateUsageRollup(); err != nil {
		return nil, err
	}
	impl, _ := cache.(*redisCacheImpl)
	if impl == nil || impl.client == nil {
		if cfg.UsageRollup.IsEnabledForAnyTenant() {
			return nil, fmt.Errorf("usage_rollup is enabled but Redis is not available: fix the redis config or set usage_rollup.enabled=false")
		}
		log.Info(context.Background(), "NewHourlyUsageRepository: Redis client is nil, returning nil repository")
		return nil, nil
	}
	return &hourlyUsageRepository{client: impl.client, keyPrefix: impl.config.Redis.KeyPrefix}, nil
}

// usageRollupKeys names the Redis keys for one hour of a customer's usage. The {tenant:env:customer} hash tag
// keeps all of a customer's keys on one cluster slot so a read is one round trip.
type usageRollupKeys struct {
	qty, max, active, field string
}

func newUsageRollupKeys(keyPrefix, tenantID, environmentID string, b usagerollup.HourlyUsage) usageRollupKeys {
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
func usageRollupField(b usagerollup.HourlyUsage) string {
	if b.Aggregation == types.AggregationCount {
		return "count:" + b.MeterID
	}
	return "sum:" + b.MeterID
}

func (s *hourlyUsageRepository) Increment(ctx context.Context, usage []usagerollup.HourlyUsage) error {
	if len(usage) == 0 {
		return nil
	}
	tenantID, environmentID := types.GetTenantID(ctx), types.GetEnvironmentID(ctx)
	if tenantID == "" || environmentID == "" {
		return fmt.Errorf("usage rollup increment: tenant and environment are required")
	}

	// TODO(FLE-2405): go-redis retries a pipeline whose reply was lost, which can apply HINCRBYFLOAT twice.
	// If Redis usage comes out above ClickHouse, give the repository a client with MaxRetries -1.
	// Not a TxPipeline: a batch spans many slots and cluster MULTI rejects that. Each write is
	// followed by its EXPIRE on the same connection, and every later write re-applies it.
	pipe := s.client.Pipeline()
	for _, b := range mergeHourlyUsage(usage) {
		keys := newUsageRollupKeys(s.keyPrefix, tenantID, environmentID, b)
		if b.Aggregation == types.AggregationMax {
			pipe.ZAddArgs(ctx, keys.max, redis.ZAddArgs{GT: true, Members: []redis.Z{{Score: b.Quantity.InexactFloat64(), Member: b.MeterID}}})
			pipe.Expire(ctx, keys.max, usagerollup.HourlyUsageTTL)
		} else {
			// Sent as a string so Redis parses the exact decimal instead of a rounded float64.
			pipe.Do(ctx, "HINCRBYFLOAT", keys.qty, keys.field, b.Quantity.String())
			pipe.Expire(ctx, keys.qty, usagerollup.HourlyUsageTTL)
		}
		pipe.SAdd(ctx, keys.active, b.ExternalCustomerID)
		pipe.Expire(ctx, keys.active, activeSetTTL)
	}

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("usage rollup increment: %w", err)
	}
	return nil
}

// hourlyUsageKey identifies one Redis field: a customer's meter in one hour.
type hourlyUsageKey struct {
	externalCustomerID string
	hour               time.Time
	meterID            string
	aggregation        types.AggregationType
}

// mergeHourlyUsage folds entries for the same customer, meter and hour into one, so a batch costs one
// Redis write per field instead of one per event. SUM and COUNT add, MAX keeps the higher value.
func mergeHourlyUsage(usage []usagerollup.HourlyUsage) []usagerollup.HourlyUsage {
	merged := make(map[hourlyUsageKey]int, len(usage))
	result := make([]usagerollup.HourlyUsage, 0, len(usage))
	for _, u := range usage {
		key := hourlyUsageKey{externalCustomerID: u.ExternalCustomerID, hour: u.Hour.UTC(), meterID: u.MeterID, aggregation: u.Aggregation}
		i, seen := merged[key]
		switch {
		case !seen:
			merged[key] = len(result)
			result = append(result, u)
		case u.Aggregation == types.AggregationMax:
			if u.Quantity.GreaterThan(result[i].Quantity) {
				result[i].Quantity = u.Quantity
			}
		default:
			result[i].Quantity = result[i].Quantity.Add(u.Quantity)
		}
	}
	return result
}

func (s *hourlyUsageRepository) ListActiveCustomers(ctx context.Context, hour time.Time) ([]string, error) {
	tenantID, environmentID := types.GetTenantID(ctx), types.GetEnvironmentID(ctx)
	if tenantID == "" || environmentID == "" {
		return nil, fmt.Errorf("usage rollup active customers: tenant and environment are required")
	}
	key := newUsageRollupKeys(s.keyPrefix, tenantID, environmentID, usagerollup.HourlyUsage{Hour: hour}).active
	customers, err := s.client.SMembers(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("usage rollup active customers: %w", err)
	}
	return customers, nil
}
