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
  events:
    partitions: 6
  events_dlq:
    partitions: 3
    replicationFactor: 1
`

const sampleJSON = `{"defaults":{"replicationFactor":3,"retentionMs":604800000},"topics":{"events":{"partitions":12},"prod_system_events":{"partitions":6}}}`

func TestParseYAML_FlattensWithDefaults(t *testing.T) {
	spec, err := ParseYAML([]byte(sampleYAML))
	require.NoError(t, err)
	got, err := spec.Resolve()
	require.NoError(t, err)

	events := find(t, got, "events")
	assert.Equal(t, 6, events.Partitions)
	assert.Equal(t, int16(3), events.ReplicationFactor)
	assert.Equal(t, int64(604800000), events.RetentionMs)

	dlq := find(t, got, "events_dlq")
	assert.Equal(t, int16(1), dlq.ReplicationFactor)
}

func TestParseJSON_SameShapeAsYAML(t *testing.T) {
	spec, err := ParseJSON([]byte(sampleJSON))
	require.NoError(t, err)
	got, err := spec.Resolve()
	require.NoError(t, err)
	assert.Equal(t, 12, find(t, got, "events").Partitions)
	assert.Equal(t, 6, find(t, got, "prod_system_events").Partitions)
}

func TestParse_RejectsZeroPartitions(t *testing.T) {
	spec, err := ParseYAML([]byte("topics:\n  bad:\n    partitions: 0\n"))
	require.NoError(t, err)
	_, err = spec.Resolve()
	assert.Error(t, err)
}

func TestParse_RejectsZeroReplicationFactor(t *testing.T) {
	spec, err := ParseYAML([]byte("topics:\n  bad:\n    partitions: 3\n    replicationFactor: 0\n"))
	require.NoError(t, err)
	_, err = spec.Resolve()
	assert.Error(t, err)
}

func TestLoadDesired_EnvOverrideReplacesFile(t *testing.T) {
	t.Setenv("FLEXPRICE_KAFKA_TOPICS", sampleJSON)
	got, err := LoadDesired("/nonexistent/topics.yaml")
	require.NoError(t, err)
	assert.Equal(t, 12, find(t, got, "events").Partitions)
	assert.Equal(t, 6, find(t, got, "prod_system_events").Partitions)
}

func find(t *testing.T, ts []ResolvedTopic, name string) ResolvedTopic {
	t.Helper()
	for _, x := range ts {
		if x.Name == name {
			return x
		}
	}
	t.Fatalf("topic %q not found", name)
	return ResolvedTopic{}
}
