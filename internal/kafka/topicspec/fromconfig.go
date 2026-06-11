package topicspec

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/flexprice/flexprice/internal/config"
)

var topicTags = map[string]bool{
	"topic":          true,
	"topic_lazy":     true,
	"topic_dlq":      true,
	"topic_backfill": true,
	"output_topic":   true,
}

const (
	defaultPartitions        = 6
	defaultReplicationFactor = int16(3)
	defaultRetentionMs       = int64(604800000) // 7d
)

func harvestTopicNames(cfg any) []string {
	seen := map[string]bool{}
	var walk func(v reflect.Value)
	walk = func(v reflect.Value) {
		for v.Kind() == reflect.Ptr {
			if v.IsNil() {
				return
			}
			v = v.Elem()
		}
		if v.Kind() != reflect.Struct {
			return
		}
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			f := t.Field(i)
			fv := v.Field(i)
			tag := strings.Split(f.Tag.Get("mapstructure"), ",")[0]
			if fv.Kind() == reflect.String && topicTags[tag] {
				if name := fv.String(); name != "" {
					seen[name] = true
				}
				continue
			}
			if fv.Kind() == reflect.Struct || fv.Kind() == reflect.Ptr {
				walk(fv)
			}
		}
	}
	walk(reflect.ValueOf(cfg))

	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func fromConfigStruct(cfg any, overrides map[string]EnvOverride) ([]ResolvedTopic, error) {
	names := harvestTopicNames(cfg)
	out := make([]ResolvedTopic, 0, len(names))
	for _, name := range names {
		r := ResolvedTopic{
			Name:              name,
			Partitions:        defaultPartitions,
			ReplicationFactor: defaultReplicationFactor,
			RetentionMs:       defaultRetentionMs,
		}
		if ov, ok := overrides[name]; ok {
			if ov.Partitions != nil {
				r.Partitions = *ov.Partitions
			}
			if ov.ReplicationFactor != nil {
				r.ReplicationFactor = *ov.ReplicationFactor
			}
			if ov.RetentionMs != nil {
				r.RetentionMs = *ov.RetentionMs
			}
		}
		// Env overrides bypass any compile-time guarantees, so validate the
		// resolved sizing here and fail fast with a precise message rather than
		// letting an invalid value surface later during reconcile/apply.
		if r.Partitions < 1 {
			return nil, fmt.Errorf("topic %q: resolved partitions must be >= 1 (got %d)", r.Name, r.Partitions)
		}
		if r.ReplicationFactor < 1 {
			return nil, fmt.Errorf("topic %q: resolved replicationFactor must be >= 1 (got %d)", r.Name, r.ReplicationFactor)
		}
		out = append(out, r)
	}
	return out, nil
}

func anyTopicEnvIn(environ []string) bool {
	for _, kv := range environ {
		key := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			key = kv[:i]
		}
		if !strings.HasPrefix(key, "FLEXPRICE_") {
			continue
		}
		if strings.HasSuffix(key, "_PARTITIONS") || strings.HasSuffix(key, "_REPLICATIONFACTOR") || strings.HasSuffix(key, "_RETENTIONMS") {
			continue
		}
		if key == "FLEXPRICE_KAFKA_TOPIC" ||
			strings.HasSuffix(key, "_TOPIC") ||
			strings.HasSuffix(key, "_TOPIC_LAZY") ||
			strings.HasSuffix(key, "_TOPIC_DLQ") ||
			strings.HasSuffix(key, "_TOPIC_BACKFILL") ||
			strings.HasSuffix(key, "_OUTPUT_TOPIC") {
			return true
		}
	}
	return false
}

func hasAnyTopicEnv() bool { return anyTopicEnvIn(os.Environ()) }

// FromConfig builds the desired topic set from a fully-resolved app config.
// Names come from the config (env-vars already won during config.NewConfig),
// sizing from FLEXPRICE_KAFKA_TOPIC_<NAME>_* env overrides else defaults.
func FromConfig(cfg *config.Configuration) ([]ResolvedTopic, error) {
	names := harvestTopicNames(cfg)
	return fromConfigStruct(cfg, EnvOverridesFromEnv(names))
}

// HasAnyTopicEnv reports whether the process environment sets any FLEXPRICE
// topic-name env-var. Used by the kafka-migrate startup guard.
func HasAnyTopicEnv() bool { return hasAnyTopicEnv() }
