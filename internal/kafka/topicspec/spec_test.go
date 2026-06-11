package topicspec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvOverridesFromEnv(t *testing.T) {
	t.Setenv("FLEXPRICE_KAFKA_TOPIC_EVENTS_PARTITIONS", "12")
	ov := EnvOverridesFromEnv([]string{"events", "events_dlq"})
	require.Contains(t, ov, "events")
	assert.Equal(t, 12, *ov["events"].Partitions)
	assert.NotContains(t, ov, "events_dlq")
}
