package activity

import "time"

// Log is one activity entry as stored in ClickHouse.
type Log struct {
	ID            string
	TenantID      string
	EnvironmentID string
	Entity        Entity
	CustomerID    string
	Action        string
	Actor         Actor
	Request       Request
	State         string
	SchemaVersion uint16
	OccurredAt    time.Time
}

// Entity is the entity that changed.
type Entity struct {
	Type  string
	ID    string
	Label string
}

// Actor is who made the change.
type Actor struct {
	Type string
	ID   string
}

// Request is where the change came from.
type Request struct {
	ID        string
	Source    string
	IP        string
	UserAgent string
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
