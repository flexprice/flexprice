package dto

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

const (
	activityMaxLimit      = 100
	activityMaxStateLimit = 50
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

// Validate checks the request on its own; the service checks entity types, actions and the time range.
func (r *ListActivityRequest) Validate() error {
	if r.EntityID != "" && r.EntityType == "" {
		return activityValidation("entity_type is required when entity_id is set.")
	}
	for _, actorType := range r.ActorTypes {
		if !slices.Contains(types.ActorTypes(), types.ActorType(actorType)) {
			allowed := lo.Map(types.ActorTypes(), func(a types.ActorType, _ int) string { return string(a) })
			return activityValidation("Unknown actor type %q. Allowed actor types: %s.", actorType, strings.Join(allowed, ", "))
		}
	}
	for _, expand := range r.Expand {
		if expand != "state" {
			return activityValidation("Unknown expand %q. Allowed values: state.", expand)
		}
	}
	maxLimit := activityMaxLimit
	if r.WithState() {
		maxLimit = activityMaxStateLimit
	}
	if r.Limit < 0 || r.Limit > maxLimit {
		return activityValidation("limit can be at most 100 (50 with expand state).")
	}
	return nil
}

// WithState reports whether the response should include each entry's state.
func (r *ListActivityRequest) WithState() bool {
	return slices.Contains(r.Expand, "state")
}

func activityValidation(format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	return ierr.NewError(msg).WithHint(msg).Mark(ierr.ErrValidation)
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
