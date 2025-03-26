package types

// IntegrationCapability represents features an integration can support
type IntegrationCapability string

const (
	CapabilityCustomer      IntegrationCapability = "customer"
	CapabilityPaymentMethod IntegrationCapability = "payment_method"
	CapabilityPayment       IntegrationCapability = "payment"
	CapabilityInvoice       IntegrationCapability = "invoice"
)
