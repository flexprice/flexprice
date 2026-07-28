package events

// EventBatch is the wire format of one `events_bulk` message. Tenant/environment sit on the
// envelope as a fallback for events that omit them.
//
// Consumers must confirm a payload is a batch before decoding: a single event decodes into an
// EventBatch with zero events, so treating "no events" as nothing-to-do drops a billable event.
type EventBatch struct {
	Events        []*Event `json:"events"`
	TenantID      string   `json:"tenant_id"`
	EnvironmentID string   `json:"environment_id"`
}
