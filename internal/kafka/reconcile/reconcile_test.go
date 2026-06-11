package reconcile

import (
	"testing"

	"github.com/flexprice/flexprice/internal/kafka/topicspec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAdmin struct {
	live     map[string]liveTopic
	created  []createCall
	grown    map[string]int32
	failGrow bool
}

type createCall struct {
	name       string
	partitions int32
	rf         int16
}

func (f *fakeAdmin) ListTopics() (map[string]liveTopic, error) { return f.live, nil }
func (f *fakeAdmin) CreateTopic(name string, partitions int32, rf int16) error {
	f.created = append(f.created, createCall{name, partitions, rf})
	return nil
}
func (f *fakeAdmin) CreatePartitions(name string, count int32) error {
	if f.failGrow {
		return assert.AnError
	}
	if f.grown == nil {
		f.grown = map[string]int32{}
	}
	f.grown[name] = count
	return nil
}

func desired(name string, parts int, rf int16) topicspec.ResolvedTopic {
	return topicspec.ResolvedTopic{Name: name, Partitions: parts, ReplicationFactor: rf, RetentionMs: 1000}
}

func TestReconcile_CreatesMissingTopic(t *testing.T) {
	f := &fakeAdmin{live: map[string]liveTopic{}}
	res, err := Reconcile(f, []topicspec.ResolvedTopic{desired("events", 6, 3)})
	require.NoError(t, err)
	require.Len(t, f.created, 1)
	assert.Equal(t, createCall{"events", 6, 3}, f.created[0])
	assert.Equal(t, 1, res.Created)
}

func TestReconcile_GrowsPartitionsWhenFewer(t *testing.T) {
	f := &fakeAdmin{live: map[string]liveTopic{"events": {Partitions: 6, ReplicationFactor: 3}}}
	res, err := Reconcile(f, []topicspec.ResolvedTopic{desired("events", 12, 3)})
	require.NoError(t, err)
	assert.Equal(t, int32(12), f.grown["events"])
	assert.Equal(t, 1, res.Grown)
}

func TestReconcile_SkipsAndWarnsWhenMorePartitions(t *testing.T) {
	f := &fakeAdmin{live: map[string]liveTopic{"events": {Partitions: 12, ReplicationFactor: 3}}}
	res, err := Reconcile(f, []topicspec.ResolvedTopic{desired("events", 6, 3)})
	require.NoError(t, err)
	assert.Empty(t, f.grown)
	assert.Equal(t, 1, res.SkippedShrink)
}

func TestReconcile_LeavesUnmanagedTopicsAlone(t *testing.T) {
	f := &fakeAdmin{live: map[string]liveTopic{"legacy": {Partitions: 3, ReplicationFactor: 3}}}
	res, err := Reconcile(f, []topicspec.ResolvedTopic{desired("events", 6, 3)})
	require.NoError(t, err)
	assert.Len(t, f.created, 1)
	assert.Equal(t, 0, res.Grown)
}

func TestReconcile_WarnsOnRFMismatchButDoesNotChange(t *testing.T) {
	f := &fakeAdmin{live: map[string]liveTopic{"events": {Partitions: 6, ReplicationFactor: 1}}}
	res, err := Reconcile(f, []topicspec.ResolvedTopic{desired("events", 6, 3)})
	require.NoError(t, err)
	assert.Equal(t, 1, res.RFMismatch)
	assert.Empty(t, f.grown)
}

func TestReconcile_PropagatesGrowError(t *testing.T) {
	f := &fakeAdmin{live: map[string]liveTopic{"events": {Partitions: 6, ReplicationFactor: 3}}, failGrow: true}
	_, err := Reconcile(f, []topicspec.ResolvedTopic{desired("events", 12, 3)})
	assert.Error(t, err)
}
