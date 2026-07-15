package topicspec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sampleJSON = `{"defaults":{"replicationFactor":3,"retentionMs":604800000},"topics":{"events":{"partitions":12},"prod_system_events":{"partitions":6}}}`

func TestSpec_ResolveFlattensWithDefaults(t *testing.T) {
	one := int16(1)
	spec := &Spec{
		Defaults: Defaults{ReplicationFactor: 3, RetentionMs: 604800000},
		Topics: map[string]TopicSpec{
			"events":     {Partitions: 6},
			"events_dlq": {Partitions: 3, ReplicationFactor: &one},
		},
	}
	got, err := spec.Resolve()
	require.NoError(t, err)

	events := find(t, got, "events")
	assert.Equal(t, 6, events.Partitions)
	assert.Equal(t, int16(3), events.ReplicationFactor)
	assert.Equal(t, int64(604800000), events.RetentionMs)

	dlq := find(t, got, "events_dlq")
	assert.Equal(t, int16(1), dlq.ReplicationFactor)
}

func TestParseJSON(t *testing.T) {
	spec, err := ParseJSON([]byte(sampleJSON))
	require.NoError(t, err)
	got, err := spec.Resolve()
	require.NoError(t, err)
	assert.Equal(t, 12, find(t, got, "events").Partitions)
	assert.Equal(t, 6, find(t, got, "prod_system_events").Partitions)
}

func TestSpec_RejectsZeroPartitions(t *testing.T) {
	spec := &Spec{Topics: map[string]TopicSpec{"bad": {Partitions: 0}}}
	_, err := spec.Resolve()
	assert.Error(t, err)
}

func TestSpec_RejectsZeroReplicationFactor(t *testing.T) {
	zero := int16(0)
	spec := &Spec{Topics: map[string]TopicSpec{"bad": {Partitions: 3, ReplicationFactor: &zero}}}
	_, err := spec.Resolve()
	assert.Error(t, err)
}

func TestLoadDesired_UsesConfigWhenEnvUnset(t *testing.T) {
	got, source, err := LoadDesired(
		ConfigDefaults{ReplicationFactor: 3, RetentionMs: 604800000},
		map[string]ConfigTopic{"events": {Partitions: 6}},
	)
	require.NoError(t, err)
	assert.Equal(t, "config", source) // source is loud about the config fallback
	assert.Equal(t, 6, find(t, got, "events").Partitions)
}

func TestLoadDesired_EnvOverrideReplacesConfig(t *testing.T) {
	t.Setenv("FLEXPRICE_KAFKA_TOPICS", sampleJSON)
	got, source, err := LoadDesired(ConfigDefaults{}, map[string]ConfigTopic{"events": {Partitions: 1}})
	require.NoError(t, err)
	assert.Equal(t, "env:FLEXPRICE_KAFKA_TOPICS", source)
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
