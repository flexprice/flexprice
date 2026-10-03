package activitylog

import (
	"time"

	"github.com/flexprice/flexprice/ent"
)

type ActivityLog struct {
	ID             string
	TenantID       string
	EnvironmentID  string
	Category       string
	EntityType     string
	EntityID       string
	EntityLabel    string
	Action         string
	ActorType      string
	ActorID        string
	ActorLabel     string
	ActorUserID    *string
	Source         string
	CustomerID     *string
	SubscriptionID *string
	RequestID      *string
	Outcome        string
	ErrorCode      *string
	Changes        map[string]any
	Snapshot       map[string]any
	Metadata       map[string]any
	OccurredAt     time.Time
}

func FromEnt(e *ent.ActivityLog) *ActivityLog {
	if e == nil {
		return nil
	}
	return &ActivityLog{
		ID: e.ID, TenantID: e.TenantID, EnvironmentID: e.EnvironmentID, Category: e.Category,
		EntityType: e.EntityType, EntityID: e.EntityID, EntityLabel: e.EntityLabel, Action: e.Action,
		ActorType: e.ActorType, ActorID: e.ActorID, ActorLabel: e.ActorLabel, ActorUserID: e.ActorUserID,
		Source: e.Source, CustomerID: e.CustomerID, SubscriptionID: e.SubscriptionID, RequestID: e.RequestID,
		Outcome: e.Outcome, ErrorCode: e.ErrorCode,
		Changes: e.Changes, Snapshot: e.Snapshot, Metadata: e.Metadata, OccurredAt: e.OccurredAt,
	}
}
