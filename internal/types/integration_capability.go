package types

// IntegrationCapabilityType names a payment operation FlexPrice implements for a
// gateway — not what the gateway's own API can do.
type IntegrationCapabilityType string

const (
	IntegrationCapabilityCheckout                IntegrationCapabilityType = "checkout"
	IntegrationCapabilityAutoCharge              IntegrationCapabilityType = "auto_charge"
	IntegrationCapabilitySetDefaultMethod        IntegrationCapabilityType = "set_default_method"
	IntegrationCapabilityPaymentLink             IntegrationCapabilityType = "payment_link"
	IntegrationCapabilityPaymentMethodManagement IntegrationCapabilityType = "payment_method_management"
	IntegrationCapabilityInvoiceSync             IntegrationCapabilityType = "invoice_sync"
)

func (t IntegrationCapabilityType) String() string { return string(t) }

type IntegrationCapability struct {
	Type IntegrationCapabilityType `json:"type"`
}
