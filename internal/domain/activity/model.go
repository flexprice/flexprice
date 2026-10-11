package activity

import "time"

// Log is one activity entry as stored in ClickHouse.
type Log struct {
	ID            string
	TenantID      string
	EnvironmentID string
	EntityType    string
	EntityID      string
	EntityLabel   string
	CustomerID    string
	Action        string
	ActorType     string
	ActorID       string
	ActorLabel    string
	Source        string
	RequestID     string
	IP            string
	UserAgent     string
	State         string
	SchemaVersion uint16
	OccurredAt    time.Time
}

// Cursor is the keyset position of the last item on a page.
type Cursor struct {
	OccurredAt time.Time `json:"o"`
	ID         string    `json:"i"`
}

// ListQuery is a validated list request; From is inclusive and To exclusive.
type ListQuery struct {
	From, To                                             time.Time
	EntityType, EntityID, CustomerID, ActorID, RequestID string
	ActorTypes, Actions                                  []string
	After                                                *Cursor
	Limit                                                int
}
