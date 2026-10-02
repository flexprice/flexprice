package types

import "context"

type ActorType string

const (
	ActorTypeUser           ActorType = "user"
	ActorTypeAPIKey         ActorType = "api_key"
	ActorTypeSystem         ActorType = "system"
	ActorTypeCustomerPortal ActorType = "customer_portal"
)

type Source string

const (
	SourceDashboard Source = "dashboard"
	SourceAPI       Source = "api"
	SourceWorkflow  Source = "workflow"
	SourceWebhook   Source = "webhook"
	SourceConsumer  Source = "consumer"
	SourcePortal    Source = "portal"
)

// Actor identifies who performed a mutation. UserID is the owning user for an
// API key and empty for every other type.
type Actor struct {
	Type   ActorType
	ID     string
	Label  string
	UserID string
}

const (
	CtxActor  ContextKey = "ctx_actor"
	CtxSource ContextKey = "ctx_source"
)

func SetActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, CtxActor, a)
}

func GetActor(ctx context.Context) Actor {
	a, _ := ctx.Value(CtxActor).(Actor)
	return a
}

func SetSource(ctx context.Context, s Source) context.Context {
	return context.WithValue(ctx, CtxSource, s)
}

func GetSource(ctx context.Context) Source {
	s, _ := ctx.Value(CtxSource).(Source)
	return s
}

func SystemActor(id, label string) Actor {
	return Actor{Type: ActorTypeSystem, ID: id, Label: label}
}

// userIDFromActor is the id written to created_by/updated_by: the owning user
// for an API key, the actor id for a user, empty for system actors.
func userIDFromActor(a Actor) string {
	switch a.Type {
	case ActorTypeUser:
		return a.ID
	case ActorTypeAPIKey:
		return a.UserID
	}
	return ""
}
