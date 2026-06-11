package topicspec

import (
	"os"
	"strconv"
	"strings"
)

const envPrefix = "FLEXPRICE_KAFKA_TOPIC_"

func EnvOverridesFromEnv(topicNames []string) map[string]EnvOverride {
	out := map[string]EnvOverride{}
	for _, name := range topicNames {
		key := envPrefix + strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(name)) + "_"
		var ov EnvOverride
		if v, ok := os.LookupEnv(key + "PARTITIONS"); ok {
			if n, err := strconv.Atoi(v); err == nil {
				ov.Partitions = &n
			}
		}
		if v, ok := os.LookupEnv(key + "REPLICATIONFACTOR"); ok {
			if n, err := strconv.ParseInt(v, 10, 16); err == nil {
				rf := int16(n)
				ov.ReplicationFactor = &rf
			}
		}
		if v, ok := os.LookupEnv(key + "RETENTIONMS"); ok {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				ov.RetentionMs = &n
			}
		}
		if ov.Partitions != nil || ov.ReplicationFactor != nil || ov.RetentionMs != nil {
			out[name] = ov
		}
	}
	return out
}
