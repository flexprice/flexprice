package types

// SystemEntityType represents the type of entity for system events
type SystemEntityType string

const (
	SystemEntityTypeFeature      SystemEntityType = "feature"
	SystemEntityTypeCustomer     SystemEntityType = "customer"
	SystemEntityTypePlan         SystemEntityType = "plan"
	SystemEntityTypeSubscription SystemEntityType = "subscription"
	SystemEntityTypeInvoice      SystemEntityType = "invoice"
	SystemEntityTypePayment      SystemEntityType = "payment"
	SystemEntityTypeCreditNote   SystemEntityType = "credit_note"
	SystemEntityTypeWallet       SystemEntityType = "wallet"
	SystemEntityTypeEntitlement  SystemEntityType = "entitlement"
)
