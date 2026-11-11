package types

// ActorType says who made a change.
type ActorType string

const (
	ActorTypeUser     ActorType = "user"
	ActorTypeAPIKey   ActorType = "api_key"
	ActorTypeCustomer ActorType = "customer"
	ActorTypeSystem   ActorType = "system"
)

// ActorTypes lists every actor type.
func ActorTypes() []ActorType {
	return []ActorType{ActorTypeUser, ActorTypeAPIKey, ActorTypeCustomer, ActorTypeSystem}
}

// Source says which entry point a change came through.
type Source string

const (
	SourceDashboard      Source = "dashboard"
	SourceAPI            Source = "api"
	SourceCustomerPortal Source = "customer_portal"
	SourceWorkflow       Source = "workflow"
	SourceWebhook        Source = "webhook"
	SourceConsumer       Source = "consumer"
	SourceHostedInvoice  Source = "hosted_invoice"
)

// Actor identifies who made a change. It holds ids only; the activity read API resolves labels.
type Actor struct {
	Type ActorType
	ID   string
}

// Actor ids with no database row behind them.
const (
	ActorIDOperatorKey           = "config"
	ActorIDCreditPurchaseBilling = "credit_purchase_billing"
)
