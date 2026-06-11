package reconcile

import (
	"testing"

	"github.com/Shopify/sarama"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToLiveTopics_MapsPartitionsAndRF(t *testing.T) {
	in := map[string]sarama.TopicDetail{
		"events": {NumPartitions: 6, ReplicationFactor: 3},
	}
	got := toLiveTopics(in)
	require.Contains(t, got, "events")
	assert.Equal(t, int32(6), got["events"].Partitions)
	assert.Equal(t, int16(3), got["events"].ReplicationFactor)
}
