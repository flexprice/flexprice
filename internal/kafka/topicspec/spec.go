package topicspec

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

const envVar = "FLEXPRICE_KAFKA_TOPICS"

type Defaults struct {
	ReplicationFactor int16 `yaml:"replicationFactor" json:"replicationFactor"`
	RetentionMs       int64 `yaml:"retentionMs" json:"retentionMs"`
}

type TopicSpec struct {
	Partitions        int    `yaml:"partitions" json:"partitions"`
	ReplicationFactor *int16 `yaml:"replicationFactor" json:"replicationFactor"`
	RetentionMs       *int64 `yaml:"retentionMs" json:"retentionMs"`
}

type Spec struct {
	Defaults Defaults             `yaml:"defaults" json:"defaults"`
	Topics   map[string]TopicSpec `yaml:"topics" json:"topics"`
}

type ResolvedTopic struct {
	Name              string
	Partitions        int
	ReplicationFactor int16
	RetentionMs       int64
}

func ParseYAML(data []byte) (*Spec, error) {
	var s Spec
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse topics yaml: %w", err)
	}
	return &s, nil
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

func LoadDesired(yamlPath string) ([]ResolvedTopic, error) {
	if v := os.Getenv(envVar); v != "" {
		spec, err := ParseJSON([]byte(v))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", envVar, err)
		}
		return spec.Resolve()
	}
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return nil, fmt.Errorf("read topics spec %s: %w", yamlPath, err)
	}
	spec, err := ParseYAML(data)
	if err != nil {
		return nil, err
	}
	return spec.Resolve()
}
