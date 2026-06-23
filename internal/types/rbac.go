package types

type Action string
type Entity string

const (
	ActionRead  Action = "read"
	ActionWrite Action = "write"
)

const (
	EntityUser          Entity = "user"
	EntityEnvironment   Entity = "environment"
	EntityEvent         Entity = "event"
	EntityMeter         Entity = "meter"
	EntityPrice         Entity = "price"
	EntityCustomer      Entity = "customer"
	EntityPlan          Entity = "plan"
	EntityAddon         Entity = "addon"
	EntityGroup         Entity = "group"
	EntitySubscription  Entity = "subscription"
	EntityWallet        Entity = "wallet"
	EntityTenant        Entity = "tenant"
	EntityInvoice       Entity = "invoice"
	EntityFeature       Entity = "feature"
	EntityEntitlement   Entity = "entitlement"
	EntityCreditGrant   Entity = "creditgrant"
	EntityPayment       Entity = "payment"
	EntityIntegration   Entity = "integration"
	EntityTask          Entity = "task"
	EntityTax           Entity = "tax"
	EntitySecret        Entity = "secret"
	EntityConnection    Entity = "connection"
	EntityCostsheet     Entity = "costsheet"
	EntityCreditNote    Entity = "creditnote"
	EntityCoupon        Entity = "coupon"
	EntityAI            Entity = "ai"
	EntityPortal        Entity = "portal"
	EntityWebhook       Entity = "webhook"
	EntityCron          Entity = "cron"
	EntitySetting       Entity = "setting"
	EntityOAuth         Entity = "oauth"
)
