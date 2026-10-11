package types

// ActorType says who made a change.
type ActorType string

const (
	ActorTypeUser     ActorType = "user"
	ActorTypeAPIKey   ActorType = "api_key"
	ActorTypeCustomer ActorType = "customer"
	ActorTypeSystem   ActorType = "system"
)

// Source says which entry point a change came through.
type Source string

const (
	SourceDashboard      Source = "dashboard"
	SourceAPI            Source = "api"
	SourceCustomerPortal Source = "customer_portal"
	SourceWorkflow       Source = "workflow"
	SourceWebhook        Source = "webhook"
	SourceConsumer       Source = "consumer"
)

// Actor identifies who made a change; Label is the human-readable name.
type Actor struct {
	Type  ActorType
	ID    string
	Label string
}
