package topicspec

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Defaults struct {
	ReplicationFactor int16 `yaml:"replicationFactor"`
	RetentionMs       int64 `yaml:"retentionMs"`
}

type EnvOverride struct {
	Partitions        *int
	ReplicationFactor *int16
	RetentionMs       *int64
}

type Topic struct {
	Name              string `yaml:"name"`
	Partitions        int    `yaml:"partitions"`
	ReplicationFactor *int16 `yaml:"replicationFactor"`
	RetentionMs       *int64 `yaml:"retentionMs"`
}

type Spec struct {
	Defaults Defaults `yaml:"defaults"`
	Topics   []Topic  `yaml:"topics"`
}

type ResolvedTopic struct {
	Name              string
	Partitions        int
	ReplicationFactor int16
	RetentionMs       int64
}

func Parse(data []byte) (*Spec, error) {
	var s Spec
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse topics spec: %w", err)
	}
	for _, t := range s.Topics {
		if t.Name == "" {
			return nil, fmt.Errorf("topic with empty name")
		}
		if t.Partitions < 1 {
			return nil, fmt.Errorf("topic %q: partitions must be >= 1", t.Name)
		}
	}
	return &s, nil
}

func Load(path string) (*Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read topics spec %s: %w", path, err)
	}
	return Parse(data)
}

func (s *Spec) Resolve(env string, overrides map[string]EnvOverride) []ResolvedTopic {
	out := make([]ResolvedTopic, 0, len(s.Topics))
	for _, t := range s.Topics {
		r := ResolvedTopic{
			Name:              t.Name,
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
		if ov, ok := overrides[t.Name]; ok {
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
		out = append(out, r)
	}
	return out
}
