package activity

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/flexprice/flexprice/internal/metrics"
)

// SchemaVersion versions the Kafka message and snapshot format; it is stored on every row.
const SchemaVersion = 1

var nullState = json.RawMessage("null")

// Message is one Kafka message: the entries of one transaction (or a slice of them).
type Message struct {
	MessageID     string         `json:"message_id"`
	SchemaVersion int            `json:"schema_version"`
	TenantID      string         `json:"tenant_id"`
	EnvironmentID string         `json:"environment_id"`
	CommittedAt   time.Time      `json:"committed_at"`
	Request       MessageRequest `json:"request"`
	Events        []MessageEvent `json:"events"`
}

type MessageRequest struct {
	ID        string `json:"id"`
	Source    string `json:"source"`
	IP        string `json:"ip"`
	UserAgent string `json:"user_agent"`
}

type MessageActor struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type MessageEvent struct {
	ID         string          `json:"id"`
	EntityType string          `json:"entity_type"`
	EntityID   string          `json:"entity_id"`
	Action     string          `json:"action"`
	Actor      MessageActor    `json:"actor"`
	State      json.RawMessage `json:"state"`
}

// split turns a batch into messages of at most maxEntries events and maxBytes encoded size.
// A state over maxSnapshot is sent as null.
func split(b Batch, maxEntries, maxBytes, maxSnapshot int) []Message {
	newMessage := func() Message {
		return Message{
			MessageID: watermill.NewUUID(), SchemaVersion: SchemaVersion,
			TenantID: b.TenantID, EnvironmentID: b.EnvironmentID, CommittedAt: b.CommittedAt,
			Request: MessageRequest{ID: b.RequestID, Source: string(b.Source), IP: b.IP, UserAgent: b.UserAgent},
		}
	}
	var out []Message
	cur := newMessage()
	curBytes := encodedSize(cur)
	for _, e := range b.Entries {
		state := e.State
		if len(state) == 0 {
			state = nullState
		}
		if len(state) > maxSnapshot {
			state = nullState
			metrics.RecordCounter(context.Background(), metrics.ActivitySnapshotTruncated, 1, metrics.L(metrics.KeyEntity, string(e.EntityType)))
		}
		event := MessageEvent{
			ID: e.ID, EntityType: string(e.EntityType), EntityID: e.EntityID, Action: string(e.Action),
			Actor: MessageActor{Type: string(e.Actor.Type), ID: e.Actor.ID}, State: state,
		}
		size := encodedSize(event) + 1
		if len(cur.Events) > 0 && (len(cur.Events) >= maxEntries || curBytes+size > maxBytes) {
			out = append(out, cur)
			cur = newMessage()
			curBytes = encodedSize(cur)
		}
		cur.Events = append(cur.Events, event)
		curBytes += size
	}
	if len(cur.Events) > 0 {
		out = append(out, cur)
	}
	return out
}

func encodedSize(v any) int {
	raw, _ := json.Marshal(v)
	return len(raw)
}
