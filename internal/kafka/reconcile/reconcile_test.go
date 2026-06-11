package reconcile

import (
	"testing"

	"github.com/flexprice/flexprice/internal/kafka/topicspec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAdmin struct {
	live    map[string]liveTopic
	created []createCall
	grown   map[string]int32
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

// CreatePartitions must never be called under create-only semantics. If the
// reconciler ever invokes it, the test fails loudly.
func (f *fakeAdmin) CreatePartitions(name string, count int32) error {
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
		name           string
		live           map[string]liveTopic
		desired        []topicspec.ResolvedTopic
		wantCreated    int
		wantUnchanged  int
		wantRFMismatch int
		checkAdmin     func(t *testing.T, f *fakeAdmin)
	}{
		{
			name:        "creates missing topic with desired sizing",
			live:        map[string]liveTopic{},
			desired:     []topicspec.ResolvedTopic{desired("events", 6, 3)},
			wantCreated: 1,
			checkAdmin: func(t *testing.T, f *fakeAdmin) {
				require.Len(t, f.created, 1)
				assert.Equal(t, createCall{"events", 6, 3}, f.created[0])
			},
		},
		{
			// Live partition count is authoritative; even if desired is higher,
			// NEVER grow. Create-only.
			name:          "never grows an existing topic with fewer live partitions",
			live:          map[string]liveTopic{"events": {Partitions: 6, ReplicationFactor: 3}},
			desired:       []topicspec.ResolvedTopic{desired("events", 12, 3)},
			wantUnchanged: 1,
			checkAdmin: func(t *testing.T, f *fakeAdmin) {
				assert.Empty(t, f.grown)   // no CreatePartitions ever
				assert.Empty(t, f.created) // already exists
			},
		},
		{
			name:          "never shrinks an existing topic with more live partitions",
			live:          map[string]liveTopic{"events": {Partitions: 100, ReplicationFactor: 3}},
			desired:       []topicspec.ResolvedTopic{desired("events", 6, 3)},
			wantUnchanged: 1,
			checkAdmin: func(t *testing.T, f *fakeAdmin) {
				assert.Empty(t, f.grown)
			},
		},
		{
			name:        "leaves unmanaged live topics alone, creates the missing desired one",
			live:        map[string]liveTopic{"legacy": {Partitions: 3, ReplicationFactor: 3}},
			desired:     []topicspec.ResolvedTopic{desired("events", 6, 3)},
			wantCreated: 1,
			checkAdmin: func(t *testing.T, f *fakeAdmin) {
				assert.Len(t, f.created, 1)
				assert.Empty(t, f.grown)
			},
		},
		{
			name:           "warns on RF mismatch but changes nothing",
			live:           map[string]liveTopic{"events": {Partitions: 6, ReplicationFactor: 1}},
			desired:        []topicspec.ResolvedTopic{desired("events", 6, 3)},
			wantRFMismatch: 1,
			wantUnchanged:  1,
			checkAdmin: func(t *testing.T, f *fakeAdmin) {
				assert.Empty(t, f.grown)
				assert.Empty(t, f.created)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeAdmin{live: tc.live}
			res, err := Reconcile(f, tc.desired)
			require.NoError(t, err)
			assert.Equal(t, tc.wantCreated, res.Created)
			assert.Equal(t, tc.wantUnchanged, res.Unchanged)
			assert.Equal(t, tc.wantRFMismatch, res.RFMismatch)
			assert.Zero(t, res.Grown, "create-only: Grown must always be 0")
			assert.Zero(t, res.SkippedShrink, "create-only: SkippedShrink must always be 0")
			if tc.checkAdmin != nil {
				tc.checkAdmin(t, f)
			}
		})
	}
}

func TestPlan_ExistingTopicIsUnchangedRegardlessOfPartitions(t *testing.T) {
	// desired wants 12 partitions, live has 6 — under create-only this must be
	// Unchanged (NOT a grow), plus an RF-mismatch warning for the RF diff.
	f := &fakeAdmin{live: map[string]liveTopic{"events": {Partitions: 6, ReplicationFactor: 1}}}
	plan, err := Plan(f, []topicspec.ResolvedTopic{desired("events", 12, 3)})
	require.NoError(t, err)

	var sawRFMismatch, sawUnchanged bool
	for _, act := range plan {
		switch act.Kind {
		case ActionRFMismatch:
			sawRFMismatch = true
		case ActionUnchanged:
			sawUnchanged = true
		case ActionGrow, ActionSkipShrink:
			t.Fatalf("create-only: unexpected partition action %v", act.Kind)
		}
	}
	assert.True(t, sawRFMismatch, "expected an ActionRFMismatch")
	assert.True(t, sawUnchanged, "expected an ActionUnchanged (never grow existing)")
}
