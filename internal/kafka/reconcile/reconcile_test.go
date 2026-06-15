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

func TestReconcile(t *testing.T) {
	tests := []struct {
		name              string
		live              map[string]liveTopic
		failGrow          bool
		desired           []topicspec.ResolvedTopic
		wantErr           bool
		wantCreated       int
		wantGrown         int
		wantSkippedShrink int
		wantRFMismatch    int
		checkAdmin        func(t *testing.T, f *fakeAdmin)
	}{
		{
			name:        "creates missing topic",
			live:        map[string]liveTopic{},
			desired:     []topicspec.ResolvedTopic{desired("events", 6, 3)},
			wantCreated: 1,
			checkAdmin: func(t *testing.T, f *fakeAdmin) {
				require.Len(t, f.created, 1)
				assert.Equal(t, createCall{"events", 6, 3}, f.created[0])
			},
		},
		{
			name:      "grows partitions when spec exceeds live",
			live:      map[string]liveTopic{"events": {Partitions: 6, ReplicationFactor: 3}},
			desired:   []topicspec.ResolvedTopic{desired("events", 12, 3)},
			wantGrown: 1,
			checkAdmin: func(t *testing.T, f *fakeAdmin) {
				assert.Equal(t, int32(12), f.grown["events"])
			},
		},
		{
			name:              "skips and warns when spec is below live",
			live:              map[string]liveTopic{"events": {Partitions: 12, ReplicationFactor: 3}},
			desired:           []topicspec.ResolvedTopic{desired("events", 6, 3)},
			wantSkippedShrink: 1,
			checkAdmin: func(t *testing.T, f *fakeAdmin) {
				assert.Empty(t, f.grown)
			},
		},
		{
			name:        "leaves unmanaged topics alone",
			live:        map[string]liveTopic{"legacy": {Partitions: 3, ReplicationFactor: 3}},
			desired:     []topicspec.ResolvedTopic{desired("events", 6, 3)},
			wantCreated: 1,
			wantGrown:   0,
			checkAdmin: func(t *testing.T, f *fakeAdmin) {
				assert.Len(t, f.created, 1)
			},
		},
		{
			name:           "warns on RF mismatch but does not change",
			live:           map[string]liveTopic{"events": {Partitions: 6, ReplicationFactor: 1}},
			desired:        []topicspec.ResolvedTopic{desired("events", 6, 3)},
			wantRFMismatch: 1,
			checkAdmin: func(t *testing.T, f *fakeAdmin) {
				assert.Empty(t, f.grown)
			},
		},
		{
			name:     "propagates grow error",
			live:     map[string]liveTopic{"events": {Partitions: 6, ReplicationFactor: 3}},
			failGrow: true,
			desired:  []topicspec.ResolvedTopic{desired("events", 12, 3)},
			wantErr:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeAdmin{live: tc.live, failGrow: tc.failGrow}
			res, err := Reconcile(f, tc.desired)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantCreated, res.Created)
			assert.Equal(t, tc.wantGrown, res.Grown)
			assert.Equal(t, tc.wantSkippedShrink, res.SkippedShrink)
			assert.Equal(t, tc.wantRFMismatch, res.RFMismatch)
			if tc.checkAdmin != nil {
				tc.checkAdmin(t, f)
			}
		})
	}
}
