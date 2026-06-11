package topicspec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sampleYAML = `
defaults:
  replicationFactor: 3
  retentionMs: 604800000
topics:
  - name: events
    partitions: 6
  - name: events_dlq
    partitions: 3
    replicationFactor: 1
`

func TestLoad_ParsesTopics(t *testing.T) {
	spec, err := Parse([]byte(sampleYAML))
	require.NoError(t, err)
	assert.Len(t, spec.Topics, 2)
}

func TestResolve_AppliesDefaults(t *testing.T) {
	spec, err := Parse([]byte(sampleYAML))
	require.NoError(t, err)
	got := spec.Resolve("staging", nil)

	events := findTopic(t, got, "events")
	assert.Equal(t, 6, events.Partitions)
	assert.Equal(t, int16(3), events.ReplicationFactor)
	assert.Equal(t, int64(604800000), events.RetentionMs)
}

func TestResolve_PerTopicOverridesDefault(t *testing.T) {
	spec, _ := Parse([]byte(sampleYAML))
	got := spec.Resolve("staging", nil)
	dlq := findTopic(t, got, "events_dlq")
	assert.Equal(t, int16(1), dlq.ReplicationFactor)
}

func TestResolve_EnvOverrideFromHelmEnvVars(t *testing.T) {
	spec, _ := Parse([]byte(sampleYAML))
	p := 12
	ov := map[string]EnvOverride{"events": {Partitions: &p}}
	prod := findTopic(t, spec.Resolve("production", ov), "events")
	assert.Equal(t, 12, prod.Partitions)
	base := findTopic(t, spec.Resolve("staging", nil), "events")
	assert.Equal(t, 6, base.Partitions)
}

func TestParse_RejectsZeroPartitions(t *testing.T) {
	_, err := Parse([]byte("topics:\n  - name: bad\n    partitions: 0\n"))
	assert.Error(t, err)
}

func TestEnvOverridesFromEnv(t *testing.T) {
	t.Setenv("FLEXPRICE_KAFKA_TOPIC_EVENTS_PARTITIONS", "12")
	ov := EnvOverridesFromEnv([]string{"events", "events_dlq"})
	require.Contains(t, ov, "events")
	assert.Equal(t, 12, *ov["events"].Partitions)
	assert.NotContains(t, ov, "events_dlq")
}

func findTopic(t *testing.T, ts []ResolvedTopic, name string) ResolvedTopic {
	t.Helper()
	for _, x := range ts {
		if x.Name == name {
			return x
		}
	}
	t.Fatalf("topic %q not found", name)
	return ResolvedTopic{}
}
