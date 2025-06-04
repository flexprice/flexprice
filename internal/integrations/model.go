package integrations

// PaymentMethodInfo contains information about a payment method
type PaymentMethodInfo struct {
	ID          string
	Type        string
	IsDefault   bool
	Last4       string
	ExpiryMonth int
	ExpiryYear  int
	Brand       string
	Metadata    map[string]string
}

// BillingDetails contains payment method billing details
type BillingDetails struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
	Phone string `json:"phone,omitempty"`

	AddressLine1   string `json:"address_line1,omitempty"`
	AddressLine2   string `json:"address_line2,omitempty"`
	AddressCity    string `json:"address_city,omitempty"`
	AddressState   string `json:"address_state,omitempty"`
	AddressZip     string `json:"address_zip,omitempty"`
	AddressCountry string `json:"address_country,omitempty"`
}
