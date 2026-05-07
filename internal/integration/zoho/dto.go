package zoho

import "github.com/shopspring/decimal"

type ContactPerson struct {
	FirstName        string `json:"first_name,omitempty"`
	LastName         string `json:"last_name,omitempty"`
	Email            string `json:"email,omitempty"`
	Phone            string `json:"phone,omitempty"`
	IsPrimaryContact bool   `json:"is_primary_contact,omitempty"`
}

type ContactAddress struct {
	Address string `json:"address,omitempty"`
	City    string `json:"city,omitempty"`
	State   string `json:"state,omitempty"`
	Zip     string `json:"zip,omitempty"`
	Country string `json:"country,omitempty"`
}

type ContactCreateRequest struct {
	ContactName     string          `json:"contact_name"`
	CompanyName     string          `json:"company_name,omitempty"`
	ContactType     string          `json:"contact_type,omitempty"`
	CustomerSubType string          `json:"customer_sub_type,omitempty"`
	BillingAddress  *ContactAddress `json:"billing_address,omitempty"`
	ContactPersons  []ContactPerson `json:"contact_persons,omitempty"`
}

type ContactResponse struct {
	ContactID      string `json:"contact_id"`
	ContactName    string `json:"contact_name,omitempty"`
	Email          string `json:"email,omitempty"`
	PrimaryContact string `json:"primary_contact_id,omitempty"`
}

type InvoiceLineItem struct {
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Rate        decimal.Decimal `json:"rate"`
	Quantity    decimal.Decimal `json:"quantity"`
}

type InvoiceCreateRequest struct {
	CustomerID      string            `json:"customer_id"`
	CurrencyCode    string            `json:"currency_code,omitempty"`
	ExchangeRate    float64           `json:"exchange_rate,omitempty"`
	Date            string            `json:"date,omitempty"`
	DueDate         string            `json:"due_date,omitempty"`
	ReferenceNumber string            `json:"reference_number,omitempty"`
	Notes           string            `json:"notes,omitempty"`
	Terms           string            `json:"terms,omitempty"`
	LineItems       []InvoiceLineItem `json:"line_items"`
}

type InvoiceResponse struct {
	InvoiceID string          `json:"invoice_id"`
	Status    string          `json:"status,omitempty"`
	Total     decimal.Decimal `json:"total,omitempty"`
}

type ZohoInvoiceSyncRequest struct {
	InvoiceID string `json:"invoice_id"`
}

type ZohoInvoiceSyncResponse struct {
	ZohoInvoiceID string          `json:"zoho_invoice_id"`
	Status        string          `json:"status"`
	Total         decimal.Decimal `json:"total"`
	Currency      string          `json:"currency"`
}
