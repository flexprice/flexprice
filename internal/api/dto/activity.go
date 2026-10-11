package dto

import (
	"encoding/json"
	"time"
)

// ListActivityRequest filters the activity list; every field is optional.
type ListActivityRequest struct {
	StartTime  *time.Time `json:"start_time,omitempty"`
	EndTime    *time.Time `json:"end_time,omitempty"`
	EntityType string     `json:"entity_type,omitempty"`
	EntityID   string     `json:"entity_id,omitempty"`
	CustomerID string     `json:"customer_id,omitempty"`
	ActorTypes []string   `json:"actor_types,omitempty"`
	ActorID    string     `json:"actor_id,omitempty"`
	Actions    []string   `json:"actions,omitempty"`
	RequestID  string     `json:"request_id,omitempty"`
	Expand     []string   `json:"expand,omitempty"`
	Limit      int        `json:"limit,omitempty"`
	Cursor     string     `json:"cursor,omitempty"`
}

type ActivityEntity struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Label string `json:"label,omitempty"`
}

type ActivityActor struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Label string `json:"label,omitempty"`
}

// ActivityResponse is one logged change; State is set only when requested.
type ActivityResponse struct {
	ID         string           `json:"id"`
	OccurredAt time.Time        `json:"occurred_at"`
	Action     string           `json:"action"`
	Entity     ActivityEntity   `json:"entity"`
	CustomerID string           `json:"customer_id,omitempty"`
	Actor      ActivityActor    `json:"actor"`
	Source     string           `json:"source"`
	RequestID  string           `json:"request_id,omitempty"`
	IP         string           `json:"ip,omitempty"`
	UserAgent  string           `json:"user_agent,omitempty"`
	State      *json.RawMessage `json:"state,omitempty" swaggertype:"object"`
}

type ActivityPagination struct {
	Limit      int     `json:"limit"`
	NextCursor *string `json:"next_cursor"`
	HasMore    bool    `json:"has_more"`
}

type ListActivityResponse struct {
	Items      []*ActivityResponse `json:"items"`
	Pagination ActivityPagination  `json:"pagination"`
}
