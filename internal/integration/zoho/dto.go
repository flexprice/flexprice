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

type ItemCreateRequest struct {
	Name           string  `json:"name"`
	Rate           float64 `json:"rate"`
	Description    string  `json:"description,omitempty"`
	ProductType    string  `json:"product_type,omitempty"`
	ItemType       string  `json:"item_type,omitempty"`
	SKU            string  `json:"sku,omitempty"`
	TaxID          string  `json:"tax_id,omitempty"`
	IsTaxable      *bool   `json:"is_taxable,omitempty"`
	TaxExemptionID string  `json:"tax_exemption_id,omitempty"`
}

type ItemResponse struct {
	ItemID string  `json:"item_id"`
	Name   string  `json:"name"`
	Status string  `json:"status,omitempty"`
	Rate   float64 `json:"rate,omitempty"`
}

type CreateItemResponse struct {
	Item *ItemResponse `json:"item"`
}

type InvoiceLineItem struct {
	ItemID         string          `json:"item_id,omitempty"`
	Name           string          `json:"name,omitempty"`
	Description    string          `json:"description,omitempty"`
	Rate           decimal.Decimal `json:"rate"`
	Quantity       decimal.Decimal `json:"quantity"`
	TaxID          string          `json:"tax_id,omitempty"`
	TaxExemptionID string          `json:"tax_exemption_id,omitempty"`
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

// Tax DTOs

type TaxResponse struct {
	TaxID            string  `json:"tax_id"`
	TaxName          string  `json:"tax_name"`
	TaxPercentage    float64 `json:"tax_percentage"`
	TaxType          string  `json:"tax_type"`
	TaxFactor        string  `json:"tax_factor,omitempty"`
	TaxAuthorityID   string  `json:"tax_authority_id,omitempty"`
	TaxAuthorityName string  `json:"tax_authority_name,omitempty"`
	IsValueAdded     bool    `json:"is_value_added"`
	IsDefaultTax     bool    `json:"is_default_tax"`
	IsEditable       bool    `json:"is_editable"`
	TaxSpecificType  string  `json:"tax_specific_type,omitempty"`
}

type PageContext struct {
	Page          int    `json:"page"`
	PerPage       int    `json:"per_page"`
	HasMorePage   bool   `json:"has_more_page"`
	ReportName    string `json:"report_name,omitempty"`
	AppliedFilter string `json:"applied_filter,omitempty"`
	SortColumn    string `json:"sort_column,omitempty"`
	SortOrder     string `json:"sort_order,omitempty"`
}

type ListTaxesResponse struct {
	Taxes       []TaxResponse `json:"taxes"`
	PageContext PageContext   `json:"page_context"`
}

type TaxExemptionResponse struct {
	TaxExemption *TaxExemption `json:"tax_exemption"`
}

type ListTaxExemptionsResponse struct {
	TaxExemptions []TaxExemption `json:"tax_exemptions"`
}

type TaxExemption struct {
	TaxExemptionID   string `json:"tax_exemption_id,omitempty"`
	TaxExemptionCode string `json:"tax_exemption_code"`
	Description      string `json:"description,omitempty"`
	Type             string `json:"type"`
}

type CreateTaxExemptionRequest struct {
	TaxExemptionCode string `json:"tax_exemption_code"`
	Description      string `json:"description,omitempty"`
	Type             string `json:"type"`
}
