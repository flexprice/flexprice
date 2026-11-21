package cache

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/aggregatedusage"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/redis/go-redis/v9"
)

const (
	aggregatedUsageHourLayout = "2006010215"

	// activeSetTTL outlives the checkpoint run that reads an hour's active set, which fires 1h + LateWindow
	// + CutoffGrace after the hour starts; the TTL may date from the hour's first write.
	activeSetTTL = 2*aggregatedusage.LateWindow + aggregatedusage.CutoffGrace + time.Hour
)

type hourlyUsageRepository struct {
	client    redis.UniversalClient
	keyPrefix string
}

// NewHourlyUsageRepository returns the Redis-backed aggregatedusage.Repository. Without Redis it returns nil,
// or an error when aggregated usage is enabled, since it cannot run without it.
func NewHourlyUsageRepository(cfg *config.Configuration, cache RedisCache, log *logger.Logger) (aggregatedusage.Repository, error) {
	if err := cfg.ValidateAggregatedUsage(); err != nil {
		return nil, err
	}
	impl, _ := cache.(*redisCacheImpl)
	if impl == nil || impl.client == nil {
		if cfg.AggregatedUsage.IsEnabledForAnyTenant() {
			return nil, fmt.Errorf("aggregated_usage is enabled but Redis is not available: fix the redis config or set aggregated_usage.enabled=false")
		}
		log.Info(context.Background(), "NewHourlyUsageRepository: Redis client is nil, returning nil repository")
		return nil, nil
	}
	return &hourlyUsageRepository{client: impl.client, keyPrefix: impl.config.Redis.KeyPrefix}, nil
}

// aggregatedUsageKeys names the Redis keys for one hour of a customer's usage. The {tenant:env:customer} hash tag
// keeps all of a customer's keys on one cluster slot so a read is one round trip.
type aggregatedUsageKeys struct {
	qty, max, active, field string
}

func newAggregatedUsageKeys(keyPrefix, tenantID, environmentID string, b aggregatedusage.HourlyUsage) aggregatedUsageKeys {
	prefix := ""
	if keyPrefix != "" {
		prefix = keyPrefix + ":"
	}
	hour := b.Hour.UTC().Format(aggregatedUsageHourLayout)
	customerTag := fmt.Sprintf("{%s:%s:%s}", tenantID, environmentID, b.ExternalCustomerID)
	return aggregatedUsageKeys{
		qty:    prefix + "qty:" + customerTag + ":" + hour,
		max:    prefix + "max:" + customerTag + ":" + hour,
		active: prefix + "active:" + hour,
		field:  aggregatedUsageField(b),
	}
}

// aggregatedUsageField carries the aggregation in the field name so readers merge without loading the meter.
func aggregatedUsageField(b aggregatedusage.HourlyUsage) string {
	if b.Aggregation == types.AggregationCount {
		return "count:" + b.MeterID
	}
	return "sum:" + b.MeterID
}

func (s *hourlyUsageRepository) Increment(ctx context.Context, usage []aggregatedusage.HourlyUsage) error {
	if len(usage) == 0 {
		return nil
	}
	tenantID, environmentID := types.GetTenantID(ctx), types.GetEnvironmentID(ctx)
	if tenantID == "" || environmentID == "" {
		return fmt.Errorf("aggregated usage increment: tenant and environment are required")
	}

	// TODO(FLE-2405): go-redis retries a pipeline whose reply was lost, which can apply HINCRBYFLOAT twice.
	// If Redis usage comes out above ClickHouse, give the repository a client with MaxRetries -1.
	// Not a TxPipeline: a batch spans many slots and cluster MULTI rejects that. Each write is
	// followed by its EXPIRE on the same connection, and every later write re-applies it.
	pipe := s.client.Pipeline()
	for _, b := range mergeHourlyUsage(usage) {
		keys := newAggregatedUsageKeys(s.keyPrefix, tenantID, environmentID, b)
		if b.Aggregation == types.AggregationMax {
			pipe.ZAddArgs(ctx, keys.max, redis.ZAddArgs{GT: true, Members: []redis.Z{{Score: b.Quantity.InexactFloat64(), Member: b.MeterID}}})
			pipe.Expire(ctx, keys.max, aggregatedusage.HourlyUsageTTL)
		} else {
			// Sent as a string so Redis parses the exact decimal instead of a rounded float64.
			pipe.Do(ctx, "HINCRBYFLOAT", keys.qty, keys.field, b.Quantity.String())
			pipe.Expire(ctx, keys.qty, aggregatedusage.HourlyUsageTTL)
		}
		pipe.SAdd(ctx, keys.active, activeMember(tenantID, environmentID, b.ExternalCustomerID))
		pipe.Expire(ctx, keys.active, activeSetTTL)
	}

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("aggregated usage increment: %w", err)
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
func mergeHourlyUsage(usage []aggregatedusage.HourlyUsage) []aggregatedusage.HourlyUsage {
	merged := make(map[hourlyUsageKey]int, len(usage))
	result := make([]aggregatedusage.HourlyUsage, 0, len(usage))
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

func (s *hourlyUsageRepository) ListActiveCustomers(ctx context.Context, hour time.Time) ([]aggregatedusage.ActiveCustomer, error) {
	key := newAggregatedUsageKeys(s.keyPrefix, "", "", aggregatedusage.HourlyUsage{Hour: hour}).active
	members, err := s.client.SMembers(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("aggregated usage active customers: %w", err)
	}
	customers := make([]aggregatedusage.ActiveCustomer, 0, len(members))
	for _, member := range members {
		parts := strings.SplitN(member, "|", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("aggregated usage active customers: malformed member %q in %s", member, key)
		}
		customers = append(customers, aggregatedusage.ActiveCustomer{TenantID: parts[0], EnvironmentID: parts[1], ExternalCustomerID: parts[2]})
	}
	return customers, nil
}

// activeMember names a customer in the hourly active set, which spans all tenants and environments.
// Tenant and environment IDs contain no "|", so the external ID may.
func activeMember(tenantID, environmentID, externalCustomerID string) string {
	return tenantID + "|" + environmentID + "|" + externalCustomerID
}
