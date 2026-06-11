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

// LoadDesired resolves the desired topic set and reports its source. If
// FLEXPRICE_KAFKA_TOPICS is set (non-empty) it is parsed as JSON and FULLY
// REPLACES the baked file. Otherwise the yaml file at yamlPath is used.
//
// The returned source string ("env:FLEXPRICE_KAFKA_TOPICS" or "file:<path>")
// lets the caller log loudly which source won — the file fallback carries the
// baked base/dev topic names (unprefixed), which are WRONG for a shared prod
// cluster, so a deploy that forgot to set the env-var must be obvious in logs.
func LoadDesired(yamlPath string) (topics []ResolvedTopic, source string, err error) {
	if v := os.Getenv(envVar); v != "" {
		spec, perr := ParseJSON([]byte(v))
		if perr != nil {
			return nil, "", fmt.Errorf("%s: %w", envVar, perr)
		}
		r, rerr := spec.Resolve()
		return r, "env:" + envVar, rerr
	}
	data, rerr := os.ReadFile(yamlPath)
	if rerr != nil {
		return nil, "", fmt.Errorf("read topics spec %s: %w", yamlPath, rerr)
	}
	spec, perr := ParseYAML(data)
	if perr != nil {
		return nil, "", perr
	}
	r, resErr := spec.Resolve()
	return r, "file:" + yamlPath, resErr
}
