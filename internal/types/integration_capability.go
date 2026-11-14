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
	IntegrationCapabilityListPaymentMethods      IntegrationCapabilityType = "list_payment_methods"
	IntegrationCapabilityAddPaymentMethod        IntegrationCapabilityType = "add_payment_method"
	IntegrationCapabilityDeletePaymentMethod     IntegrationCapabilityType = "delete_payment_method"
)

func (t IntegrationCapabilityType) String() string { return string(t) }

type IntegrationCapability struct {
	Type                    IntegrationCapabilityType `json:"type"`
	AddPaymentMethodOptions *AddPaymentMethodOptions  `json:"add_payment_method_options,omitempty"`
}

// AddPaymentMethodOptions describes what add_payment_method sets up at a gateway.
type AddPaymentMethodOptions struct {
	// PaymentMethodTypes are the methods it sets up, e.g. ["enach"] for Razorpay.
	PaymentMethodTypes []string `json:"payment_method_types"`
	// SetsDefault reports whether it can make the saved method the default (Stripe only).
	SetsDefault bool `json:"sets_default"`
}
