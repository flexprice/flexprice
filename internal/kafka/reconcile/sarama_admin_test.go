package reconcile

import (
	"testing"

	"github.com/Shopify/sarama"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeClusterAdmin implements only the two sarama.ClusterAdmin methods
// SaramaAdmin.toLiveTopics needs; embedding the nil interface satisfies the
// rest of the (large) interface without requiring stub implementations.
type fakeClusterAdmin struct {
	sarama.ClusterAdmin
	configs map[string][]sarama.ConfigEntry
}

func (f *fakeClusterAdmin) DescribeConfig(resource sarama.ConfigResource) ([]sarama.ConfigEntry, error) {
	return f.configs[resource.Name], nil
}

func TestToLiveTopics_MapsPartitionsRFAndRetention(t *testing.T) {
	retention := "604800000"
	in := map[string]sarama.TopicDetail{
		"events": {NumPartitions: 6, ReplicationFactor: 3},
	}
	admin := &SaramaAdmin{Admin: &fakeClusterAdmin{
		configs: map[string][]sarama.ConfigEntry{
			"events": {{Name: "retention.ms", Value: retention}},
		},
	}}
	got, err := admin.toLiveTopics(in)
	require.NoError(t, err)
	require.Contains(t, got, "events")
	assert.Equal(t, int32(6), got["events"].Partitions)
	assert.Equal(t, int16(3), got["events"].ReplicationFactor)
	assert.Equal(t, int64(604800000), got["events"].RetentionMs)
}
