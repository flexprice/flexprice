package topicspec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type subA struct {
	Topic         string `mapstructure:"topic"`
	TopicBackfill string `mapstructure:"topic_backfill"`
}
type subB struct {
	TopicLazy string `mapstructure:"topic_lazy"`
	TopicDLQ  string `mapstructure:"topic_dlq"`
	Other     string `mapstructure:"not_a_topic"`
}
type subC struct {
	OutputTopic string `mapstructure:"output_topic"`
}
type rootCfg struct {
	A   subA
	B   subB
	C   subC
	Ptr *subA
}

func TestHarvestTopicNames_CollectsAllTaggedFields(t *testing.T) {
	c := rootCfg{
		A: subA{Topic: "events", TopicBackfill: "events_backfill"},
		B: subB{TopicLazy: "events_lazy", TopicDLQ: "", Other: "ignore_me"},
		C: subC{OutputTopic: "prod_events_v4"},
	}
	names := harvestTopicNames(c)
	assert.ElementsMatch(t, []string{"events", "events_backfill", "events_lazy", "prod_events_v4"}, names)
	assert.NotContains(t, names, "ignore_me")
	assert.NotContains(t, names, "")
}

func TestHarvestTopicNames_Dedups(t *testing.T) {
	c := rootCfg{
		A: subA{Topic: "events", TopicBackfill: "events"},
		B: subB{TopicLazy: "events"},
		C: subC{OutputTopic: "events"},
	}
	names := harvestTopicNames(c)
	assert.Equal(t, []string{"events"}, names)
}

func TestHarvestTopicNames_HandlesNilPointer(t *testing.T) {
	c := rootCfg{A: subA{Topic: "events"}, Ptr: nil}
	names := harvestTopicNames(c)
	assert.Equal(t, []string{"events"}, names)
}

func TestFromConfigStruct_AppliesDefaultSizing(t *testing.T) {
	c := rootCfg{A: subA{Topic: "events"}}
	got, err := fromConfigStruct(c, nil)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "events", got[0].Name)
	assert.Equal(t, 6, got[0].Partitions)
	assert.Equal(t, int16(3), got[0].ReplicationFactor)
	assert.Equal(t, int64(604800000), got[0].RetentionMs)
}

func TestFromConfigStruct_EnvOverrideWinsOverDefault(t *testing.T) {
	c := rootCfg{A: subA{Topic: "events"}}
	p := 12
	ov := map[string]EnvOverride{"events": {Partitions: &p}}
	got, err := fromConfigStruct(c, ov)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, 12, got[0].Partitions)
	assert.Equal(t, int16(3), got[0].ReplicationFactor)
}

func TestFromConfigStruct_RejectsZeroPartitionOverride(t *testing.T) {
	c := rootCfg{A: subA{Topic: "events"}}
	p := 0
	ov := map[string]EnvOverride{"events": {Partitions: &p}}
	_, err := fromConfigStruct(c, ov)
	assert.Error(t, err) // env-injected partitions=0 must fail fast
}

func TestFromConfigStruct_RejectsZeroRFOverride(t *testing.T) {
	c := rootCfg{A: subA{Topic: "events"}}
	rf := int16(0)
	ov := map[string]EnvOverride{"events": {ReplicationFactor: &rf}}
	_, err := fromConfigStruct(c, ov)
	assert.Error(t, err)
}

func TestHasAnyTopicEnv(t *testing.T) {
	t.Setenv("FLEXPRICE_KAFKA_TOPIC", "events")
	assert.True(t, hasAnyTopicEnv())
}

func TestHasAnyTopicEnv_FalseWhenNoneSet(t *testing.T) {
	assert.False(t, anyTopicEnvIn([]string{"PATH=/bin", "HOME=/root", "FLEXPRICE_KAFKA_BROKERS=x"}))
	assert.True(t, anyTopicEnvIn([]string{"FLEXPRICE_WEBHOOK_TOPIC=prod_system_events"}))
	assert.True(t, anyTopicEnvIn([]string{"FLEXPRICE_KAFKA_TOPIC_LAZY=events_lazy"}))
}
