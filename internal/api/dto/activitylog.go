package dto

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/activitylog"
)

type ActivityActor struct {
	Type   string  `json:"type"`
	ID     string  `json:"id"`
	Label  string  `json:"label"`
	UserID *string `json:"user_id,omitempty"`
}

type ActivityDisplayParts struct {
	Actor      string `json:"actor"`
	Verb       string `json:"verb"`
	EntityType string `json:"entity_type"`
	Entity     string `json:"entity"`
}

type ActivityDisplay struct {
	Summary     string               `json:"summary"`
	EntityLabel string               `json:"entity_label"`
	Parts       ActivityDisplayParts `json:"parts"`
}

type ActivityResponse struct {
	ID             string          `json:"id"`
	EntityType     string          `json:"entity_type"`
	EntityID       string          `json:"entity_id"`
	EntityLabel    string          `json:"entity_label"`
	Action         string          `json:"action"`
	Actor          ActivityActor   `json:"actor"`
	Source         string          `json:"source"`
	CustomerID     *string         `json:"customer_id,omitempty"`
	SubscriptionID *string         `json:"subscription_id,omitempty"`
	RequestID      *string         `json:"request_id,omitempty"`
	OccurredAt     time.Time       `json:"occurred_at"`
	Changes        map[string]any  `json:"changes,omitempty"`
	Snapshot       map[string]any  `json:"snapshot,omitempty"`
	Metadata       map[string]any  `json:"metadata,omitempty"`
	Display        ActivityDisplay `json:"display"`
}

type ListActivityResponse struct {
	Items      []*ActivityResponse `json:"items"`
	NextCursor *string             `json:"next_cursor,omitempty"`
	HasMore    bool                `json:"has_more"`
}

func NewActivityResponse(a *activitylog.ActivityLog) *ActivityResponse {
	if a == nil {
		return nil
	}
	return &ActivityResponse{
		ID: a.ID, EntityType: a.EntityType, EntityID: a.EntityID, EntityLabel: a.EntityLabel, Action: a.Action,
		Actor:  ActivityActor{Type: a.ActorType, ID: a.ActorID, Label: a.ActorLabel, UserID: a.ActorUserID},
		Source: a.Source, CustomerID: a.CustomerID, SubscriptionID: a.SubscriptionID, RequestID: a.RequestID, OccurredAt: a.OccurredAt,
		Changes: a.Changes, Snapshot: a.Snapshot, Metadata: a.Metadata,
	}
}
