package types

import "time"

type ActivityFilter struct {
	EntityType        *string    `form:"entity_type"`
	EntityID          *string    `form:"entity_id"`
	CustomerID        *string    `form:"customer_id"`
	SubscriptionID    *string    `form:"subscription_id"`
	ActorType         *string    `form:"actor_type"`
	ExcludeActorTypes []string   `form:"exclude_actor_types"`
	ActorID           *string    `form:"actor_id"`
	Actions           []string   `form:"actions"`
	RequestID         *string    `form:"request_id"`
	StartTime         *time.Time `form:"start_time" time_format:"2006-01-02T15:04:05Z07:00"`
	EndTime           *time.Time `form:"end_time" time_format:"2006-01-02T15:04:05Z07:00"`
	Cursor            *string    `form:"cursor"`
	Limit             *int       `form:"limit" validate:"omitempty,min=1,max=200"`
}

func (f *ActivityFilter) GetLimit() int {
	if f == nil || f.Limit == nil || *f.Limit <= 0 {
		return 50
	}
	if *f.Limit > 200 {
		return 200
	}
	return *f.Limit
}

// ActivityCursor is the keyset position after the last returned row.
type ActivityCursor struct {
	OccurredAt time.Time `json:"o"`
	ID         string    `json:"i"`
}
