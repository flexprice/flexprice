package topicspec

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

const envVar = "FLEXPRICE_KAFKA_TOPICS"

type Defaults struct {
	ReplicationFactor int16 `json:"replicationFactor"`
	RetentionMs       int64 `json:"retentionMs"`
}

type TopicSpec struct {
	Partitions        int    `json:"partitions"`
	ReplicationFactor *int16 `json:"replicationFactor"`
	RetentionMs       *int64 `json:"retentionMs"`
}

type Spec struct {
	Defaults Defaults             `json:"defaults"`
	Topics   map[string]TopicSpec `json:"topics"`
}

type ResolvedTopic struct {
	Name              string
	Partitions        int
	ReplicationFactor int16
	RetentionMs       int64
}

func ParseJSON(data []byte) (*Spec, error) {
	var s Spec
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse topics json: %w", err)
	}
	return &s, nil
}

func (s *Spec) Resolve() ([]ResolvedTopic, error) {
	out := make([]ResolvedTopic, 0, len(s.Topics))
	for name, t := range s.Topics {
		r := ResolvedTopic{
			Name:              name,
			Partitions:        t.Partitions,
			ReplicationFactor: s.Defaults.ReplicationFactor,
			RetentionMs:       s.Defaults.RetentionMs,
		}
		if t.ReplicationFactor != nil {
			r.ReplicationFactor = *t.ReplicationFactor
		}
		if t.RetentionMs != nil {
			r.RetentionMs = *t.RetentionMs
		}
		if name == "" {
			return nil, fmt.Errorf("topic with empty name")
		}
		if r.Partitions < 1 {
			return nil, fmt.Errorf("topic %q: partitions must be >= 1 (got %d)", name, r.Partitions)
		}
		if r.ReplicationFactor < 1 {
			return nil, fmt.Errorf("topic %q: replicationFactor must be >= 1 (got %d)", name, r.ReplicationFactor)
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ConfigDefaults/ConfigTopics mirror config.KafkaTopicsDefaults/KafkaTopicSpec
// structurally (same fields) without importing internal/config, so the
// config package doesn't need to depend back on topicspec. The caller
// (cmd/kafka-migrate) converts cfg.Kafka.TopicsDefaults/Topics into these.
type ConfigDefaults struct {
	ReplicationFactor int16
	RetentionMs       int64
}

type ConfigTopic struct {
	Partitions        int
	ReplicationFactor *int16
	RetentionMs       *int64
}

// LoadDesired resolves the desired topic set and reports its source. If
// FLEXPRICE_KAFKA_TOPICS is set (non-empty) it is parsed as JSON and FULLY
// REPLACES the config.yaml-sourced base spec (no merge). Otherwise the base
// spec (config.yaml's kafka.topics_defaults/topics, already loaded via Viper
// by the caller) is used.
//
// The returned source string ("env:FLEXPRICE_KAFKA_TOPICS" or "config") lets
// the caller log loudly which source won — the config.yaml fallback carries
// the base/dev topic names (unprefixed), which are WRONG for a shared prod
// cluster, so a deploy that forgot to set the env-var must be obvious in logs.
func LoadDesired(defaults ConfigDefaults, topics map[string]ConfigTopic) (out []ResolvedTopic, source string, err error) {
	if v := os.Getenv(envVar); v != "" {
		spec, perr := ParseJSON([]byte(v))
		if perr != nil {
			return nil, "", fmt.Errorf("%s: %w", envVar, perr)
		}
		r, rerr := spec.Resolve()
		return r, "env:" + envVar, rerr
	}

	spec := &Spec{
		Defaults: Defaults{ReplicationFactor: defaults.ReplicationFactor, RetentionMs: defaults.RetentionMs},
		Topics:   make(map[string]TopicSpec, len(topics)),
	}
	for name, t := range topics {
		spec.Topics[name] = TopicSpec{Partitions: t.Partitions, ReplicationFactor: t.ReplicationFactor, RetentionMs: t.RetentionMs}
	}
	r, resErr := spec.Resolve()
	return r, "config", resErr
}
