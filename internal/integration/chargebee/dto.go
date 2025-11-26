package chargebee

import "time"

// ============================================================================
// Item Family DTOs
// ============================================================================

// ItemFamilyCreateRequest represents the request to create an item family
type ItemFamilyCreateRequest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ItemFamilyResponse represents an item family response from Chargebee
type ItemFamilyResponse struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	Status          string    `json:"status"`
	ResourceVersion int64     `json:"resource_version"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// ============================================================================
// Item DTOs (for charge-type items)
// ============================================================================

// ItemCreateRequest represents the request to create an item
type ItemCreateRequest struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Type            string `json:"type"` // "charge" for one-time charges
	ItemFamilyID    string `json:"item_family_id"`
	Description     string `json:"description,omitempty"`
	ExternalName    string `json:"external_name,omitempty"`
	EnabledInPortal bool   `json:"enabled_in_portal"`
}

// ItemResponse represents an item response from Chargebee
type ItemResponse struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Type            string    `json:"type"`
	ItemFamilyID    string    `json:"item_family_id"`
	Description     string    `json:"description"`
	ExternalName    string    `json:"external_name"`
	Status          string    `json:"status"`
	ResourceVersion int64     `json:"resource_version"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// ============================================================================
// Item Price DTOs
// ============================================================================

// ChargebeeTier represents a pricing tier for Chargebee item prices
type ChargebeeTier struct {
	StartingUnit int64  `json:"starting_unit"`         // Starting quantity for this tier (1-based, Chargebee requirement)
	EndingUnit   *int64 `json:"ending_unit,omitempty"` // Ending quantity (nil for last tier)
	Price        int64  `json:"price"`                 // Price per unit in smallest currency unit
}

// ItemPriceCreateRequest represents the request to create an item price
type ItemPriceCreateRequest struct {
	ID           string          `json:"id"`
	ItemID       string          `json:"item_id"`
	Name         string          `json:"name"`
	ExternalName string          `json:"external_name,omitempty"`
	PricingModel string          `json:"pricing_model"` // "flat_fee", "per_unit", "tiered", "volume", "package", "stairstep"
	Price        int64           `json:"price"`         // Amount in cents (for flat_fee/per_unit)
	CurrencyCode string          `json:"currency_code"` // "USD", "INR", etc.
	Description  string          `json:"description,omitempty"`
	Tiers        []ChargebeeTier `json:"tiers,omitempty"`       // For tiered/volume pricing
	Period       *int            `json:"period,omitempty"`      // For package pricing (optional)
	PeriodUnit   string          `json:"period_unit,omitempty"` // For package pricing: "day", "week", "month", "year"
}

// ItemPriceResponse represents an item price response from Chargebee
type ItemPriceResponse struct {
	ID              string    `json:"id"`
	ItemID          string    `json:"item_id"`
	Name            string    `json:"name"`
	ExternalName    string    `json:"external_name"`
	PricingModel    string    `json:"pricing_model"`
	Price           int64     `json:"price"`
	CurrencyCode    string    `json:"currency_code"`
	Description     string    `json:"description"`
	Status          string    `json:"status"`
	ResourceVersion int64     `json:"resource_version"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// ============================================================================
// Customer DTOs
// ============================================================================

// CustomerCreateRequest represents the request to create a customer
type CustomerCreateRequest struct {
	ID             string                 `json:"id,omitempty"`
	FirstName      string                 `json:"first_name,omitempty"`
	LastName       string                 `json:"last_name,omitempty"`
	Email          string                 `json:"email,omitempty"`
	Company        string                 `json:"company,omitempty"`
	Phone          string                 `json:"phone,omitempty"`
	AutoCollection string                 `json:"auto_collection"` // "on" to enable automatic payment collection, "off" for manual
	BillingAddress *BillingAddressRequest `json:"billing_address,omitempty"`
}

// BillingAddressRequest represents billing address in customer request
type BillingAddressRequest struct {
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
	Email     string `json:"email,omitempty"`
	Company   string `json:"company,omitempty"`
	Phone     string `json:"phone,omitempty"`
	Line1     string `json:"line1,omitempty"`
	Line2     string `json:"line2,omitempty"`
	Line3     string `json:"line3,omitempty"`
	City      string `json:"city,omitempty"`
	State     string `json:"state,omitempty"`
	StateCode string `json:"state_code,omitempty"`
	Zip       string `json:"zip,omitempty"`
	Country   string `json:"country,omitempty"`
}

// CustomerResponse represents a customer response from Chargebee
type CustomerResponse struct {
	ID              string          `json:"id"`
	FirstName       string          `json:"first_name"`
	LastName        string          `json:"last_name"`
	Email           string          `json:"email"`
	Company         string          `json:"company"`
	Phone           string          `json:"phone"`
	AutoCollection  string          `json:"auto_collection"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	ResourceVersion int64           `json:"resource_version"`
	BillingAddress  *BillingAddress `json:"billing_address,omitempty"`
}

// BillingAddress represents billing address in customer response
type BillingAddress struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	Company   string `json:"company"`
	Phone     string `json:"phone"`
	Line1     string `json:"line1"`
	Line2     string `json:"line2"`
	Line3     string `json:"line3"`
	City      string `json:"city"`
	State     string `json:"state"`
	StateCode string `json:"state_code"`
	Zip       string `json:"zip"`
	Country   string `json:"country"`
}

// ============================================================================
// Invoice DTOs
// ============================================================================

// InvoiceCreateRequest represents the request to create an invoice
// Note: Chargebee calculates due dates automatically based on customer payment terms
// and site settings. The due_date cannot be set during invoice creation.
type InvoiceCreateRequest struct {
	CustomerID     string            `json:"customer_id"`
	AutoCollection string            `json:"auto_collection"` // "on" if customer has payment method, "off" otherwise
	LineItems      []InvoiceLineItem `json:"line_items"`
	Date           *time.Time        `json:"date,omitempty"`
}

// InvoiceLineItem represents a line item in invoice request
type InvoiceLineItem struct {
	ItemPriceID string     `json:"item_price_id"`
	Quantity    int        `json:"quantity"`
	UnitAmount  int64      `json:"unit_amount,omitempty"` // Amount in cents
	Description string     `json:"description,omitempty"`
	DateFrom    *time.Time `json:"date_from,omitempty"`
	DateTo      *time.Time `json:"date_to,omitempty"`
}

// InvoiceResponse represents an invoice response from Chargebee
type InvoiceResponse struct {
	ID              string                    `json:"id"`
	CustomerID      string                    `json:"customer_id"`
	Status          string                    `json:"status"`
	AutoCollection  string                    `json:"auto_collection"`
	Total           int64                     `json:"total"`
	AmountDue       int64                     `json:"amount_due"`
	AmountPaid      int64                     `json:"amount_paid"`
	CurrencyCode    string                    `json:"currency_code"`
	Date            time.Time                 `json:"date"`
	DueDate         *time.Time                `json:"due_date,omitempty"`
	CreatedAt       time.Time                 `json:"created_at"`
	UpdatedAt       time.Time                 `json:"updated_at"`
	ResourceVersion int64                     `json:"resource_version"`
	LineItems       []InvoiceLineItemResponse `json:"line_items,omitempty"`
}

// InvoiceLineItemResponse represents a line item in invoice response
type InvoiceLineItemResponse struct {
	ID          string    `json:"id"`
	ItemPriceID string    `json:"item_price_id"`
	EntityType  string    `json:"entity_type"`
	Quantity    int       `json:"quantity"`
	UnitAmount  int64     `json:"unit_amount"`
	Amount      int64     `json:"amount"`
	Description string    `json:"description"`
	DateFrom    time.Time `json:"date_from"`
	DateTo      time.Time `json:"date_to"`
}

// ============================================================================
// Error Response
// ============================================================================

// ErrorResponse represents an error response from Chargebee
type ErrorResponse struct {
	Message      string `json:"message"`
	Type         string `json:"type"`
	APIErrorCode string `json:"api_error_code"`
	Param        string `json:"param,omitempty"`
}
