package activity

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func batchOf(n, stateBytes int) Batch {
	b := Batch{TenantID: "t", EnvironmentID: "e", CommittedAt: fixedTime, RequestID: "req_1", Source: types.SourceAPI}
	state := json.RawMessage(`"` + strings.Repeat("x", stateBytes) + `"`)
	for i := 0; i < n; i++ {
		b.Entries = append(b.Entries, Entry{
			ID: fmt.Sprintf("act_%d", i), EntityType: types.SystemEntityTypeInvoice, EntityID: fmt.Sprintf("inv_%d", i),
			Action: "invoice.updated", State: state, TenantID: "t", EnvironmentID: "e",
		})
	}
	return b
}

func TestSplit_ByCount(t *testing.T) {
	msgs := split(batchOf(250, 10), 100, 1<<20, 1<<20)
	require.Len(t, msgs, 3)
	require.Len(t, msgs[0].Events, 100)
	require.Len(t, msgs[1].Events, 100)
	require.Len(t, msgs[2].Events, 50)
	for _, m := range msgs {
		require.Equal(t, "t", m.TenantID)
		require.Equal(t, "e", m.EnvironmentID)
		require.Equal(t, fixedTime, m.CommittedAt)
	}
}

func TestSplit_ByBytes(t *testing.T) {
	const limit = 10_000
	msgs := split(batchOf(20, 1_000), 100, limit, 1<<20)
	require.Greater(t, len(msgs), 1)
	total := 0
	for _, m := range msgs {
		raw, err := json.Marshal(m)
		require.NoError(t, err)
		require.LessOrEqual(t, len(raw), limit)
		total += len(m.Events)
	}
	require.Equal(t, 20, total)
}

func TestSplit_OversizedStateNull(t *testing.T) {
	msgs := split(batchOf(1, 300*1024), 100, 1<<20, 256*1024)
	require.Len(t, msgs, 1)
	require.Equal(t, "null", string(msgs[0].Events[0].State))
	require.Equal(t, "inv_0", msgs[0].Events[0].EntityID, "the entry itself is kept")
}

func TestPublisher_FullBufferDrops(t *testing.T) {
	cfg := &config.Configuration{Activity: config.ActivityConfig{Publisher: config.ActivityPublisherConfig{BufferSize: 1, ShutdownTimeout: time.Second}}}
	p := NewPublisher(cfg, nil, types.ActivityPubSub{})
	done := make(chan struct{})
	go func() {
		p.Publish(batchOf(1, 1))
		p.Publish(batchOf(1, 1))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on a full buffer")
	}
	require.Len(t, p.queue, 1)
}
