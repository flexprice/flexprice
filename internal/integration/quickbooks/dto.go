package quickbooks

import (
	"github.com/shopspring/decimal"
)

// QuickBooksItemType represents the type of item in QuickBooks
type QuickBooksItemType string

const (
	// ItemTypeService represents a service item in QuickBooks
	ItemTypeService QuickBooksItemType = "Service"
)

// CustomerCreateRequest represents the request to create a customer
type CustomerCreateRequest struct {
	DisplayName      string        `json:"DisplayName"`
	PrimaryEmailAddr *EmailAddress `json:"PrimaryEmailAddr,omitempty"`
	BillAddr         *Address      `json:"BillAddr,omitempty"`
}

// EmailAddress represents an email address in QuickBooks
type EmailAddress struct {
	Address string `json:"Address"`
}

// Address represents an address in QuickBooks
type Address struct {
	Line1                  string `json:"Line1,omitempty"`
	Line2                  string `json:"Line2,omitempty"`
	City                   string `json:"City,omitempty"`
	CountrySubDivisionCode string `json:"CountrySubDivisionCode,omitempty"` // State/Province
	PostalCode             string `json:"PostalCode,omitempty"`
	Country                string `json:"Country,omitempty"`
}

// CustomerResponse represents a customer response from QuickBooks
type CustomerResponse struct {
	ID               string        `json:"Id"`
	SyncToken        string        `json:"SyncToken,omitempty"`
	DisplayName      string        `json:"DisplayName"`
	PrimaryEmailAddr *EmailAddress `json:"PrimaryEmailAddr,omitempty"`
	BillAddr         *Address      `json:"BillAddr,omitempty"`
	MetaData         struct {
		CreateTime      string `json:"CreateTime,omitempty"`
		LastUpdatedTime string `json:"LastUpdatedTime,omitempty"`
	} `json:"MetaData,omitempty"`
}

// ItemCreateRequest represents the request to create an item
type ItemCreateRequest struct {
	Name             string           `json:"Name"`
	Type             string           `json:"Type"` // "Service"
	Description      string           `json:"Description,omitempty"`
	Active           bool             `json:"Active,omitempty"`
	IncomeAccountRef *AccountRef      `json:"IncomeAccountRef"`
	UnitPrice        *decimal.Decimal `json:"UnitPrice,omitempty"` // Default sales price/rate for the item
}

// AccountRef represents a reference to an account
type AccountRef struct {
	Value string `json:"value"`
	Name  string `json:"name,omitempty"`
}

// ItemResponse represents an item response from QuickBooks
type ItemResponse struct {
	ID               string           `json:"Id"`
	SyncToken        string           `json:"SyncToken,omitempty"`
	Name             string           `json:"Name"`
	Type             string           `json:"Type"`
	Description      string           `json:"Description,omitempty"`
	Active           bool             `json:"Active,omitempty"`
	UnitPrice        *decimal.Decimal `json:"UnitPrice,omitempty"` // Default sales price/rate
	IncomeAccountRef *AccountRef      `json:"IncomeAccountRef,omitempty"`
	MetaData         struct {
		CreateTime      string `json:"CreateTime,omitempty"`
		LastUpdatedTime string `json:"LastUpdatedTime,omitempty"`
	} `json:"MetaData,omitempty"`
}

// InvoiceCreateRequest represents the request to create an invoice
type InvoiceCreateRequest struct {
	CustomerRef AccountRef        `json:"CustomerRef"`
	Line        []InvoiceLineItem `json:"Line"`
	DueDate     *string           `json:"DueDate,omitempty"` // Format: YYYY-MM-DD
}

// InvoiceLineItem represents a line item in invoice request
type InvoiceLineItem struct {
	LineNum             int                  `json:"LineNum,omitempty"`
	Description         string               `json:"Description,omitempty"`
	Amount              decimal.Decimal      `json:"Amount"`
	DetailType          string               `json:"DetailType"` // "SalesItemLineDetail"
	SalesItemLineDetail *SalesItemLineDetail `json:"SalesItemLineDetail"`
}

// SalesItemLineDetail represents sales item line detail
type SalesItemLineDetail struct {
	ItemRef   AccountRef       `json:"ItemRef"`
	Qty       *decimal.Decimal `json:"Qty,omitempty"`
	UnitPrice *decimal.Decimal `json:"UnitPrice,omitempty"`
}

// InvoiceResponse represents an invoice response from QuickBooks
type InvoiceResponse struct {
	ID          string            `json:"Id"`
	SyncToken   string            `json:"SyncToken,omitempty"`
	DocNumber   string            `json:"DocNumber,omitempty"`
	TxnDate     string            `json:"TxnDate"`
	DueDate     string            `json:"DueDate,omitempty"`
	CustomerRef AccountRef        `json:"CustomerRef"`
	Line        []InvoiceLineItem `json:"Line,omitempty"`
	SubTotalAmt decimal.Decimal   `json:"SubTotalAmt,omitempty"`
	TotalAmt    decimal.Decimal   `json:"TotalAmt,omitempty"`
	Balance     decimal.Decimal   `json:"Balance,omitempty"`
	MetaData    struct {
		CreateTime      string `json:"CreateTime,omitempty"`
		LastUpdatedTime string `json:"LastUpdatedTime,omitempty"`
	} `json:"MetaData,omitempty"`
}

// AccountResponse represents an account response from QuickBooks
// Used in QueryResponse for querying accounts
type AccountResponse struct {
	ID             string `json:"Id"`
	Name           string `json:"Name"`
	AccountType    string `json:"AccountType"`
	Active         bool   `json:"Active"`
	AccountSubType string `json:"AccountSubType,omitempty"`
}

// QuickBooksInvoiceSyncRequest represents a request to sync an invoice to QuickBooks
type QuickBooksInvoiceSyncRequest struct {
	InvoiceID string `json:"invoice_id" validate:"required"`
}

// QuickBooksInvoiceSyncResponse represents the response from syncing an invoice to QuickBooks
type QuickBooksInvoiceSyncResponse struct {
	QuickBooksInvoiceID string          `json:"quickbooks_invoice_id"`
	Total               decimal.Decimal `json:"total"`
	Currency            string          `json:"currency"`
}

// QueryResponse represents a QuickBooks query API response
type QueryResponse struct {
	QueryResponse struct {
		MaxResults    int                `json:"maxResults"`
		StartPosition int                `json:"startPosition"`
		TotalCount    int                `json:"totalCount"`
		Customer      []CustomerResponse `json:"Customer,omitempty"`
		Item          []ItemResponse     `json:"Item,omitempty"`
		Account       []AccountResponse  `json:"Account,omitempty"`
		Payment       []PaymentResponse  `json:"Payment,omitempty"`
	} `json:"QueryResponse"`
}

// PaymentLine represents a line item in a payment linking to an invoice
// We always link to exactly ONE invoice (1:1 relationship)
type PaymentLine struct {
	Amount    float64     `json:"Amount"`    // REQUIRED: Amount applied to this invoice
	LinkedTxn []LinkedTxn `json:"LinkedTxn"` // REQUIRED: Invoice reference (always single invoice)
}

// LinkedTxn represents a linked transaction (invoice) in a payment
type LinkedTxn struct {
	TxnId   string `json:"TxnId"`   // REQUIRED: Invoice ID in QuickBooks
	TxnType string `json:"TxnType"` // REQUIRED: Always "Invoice" for payments
}

// PaymentResponse represents a payment response from QuickBooks
// Returned when we GET a payment or CREATE a payment
type PaymentResponse struct {
	ID          string        `json:"Id"`                    // QuickBooks Payment ID
	SyncToken   string        `json:"SyncToken,omitempty"`   // Version control for updates (we don't use)
	TxnDate     string        `json:"TxnDate"`               // Payment date (YYYY-MM-DD)
	TotalAmt    float64       `json:"TotalAmt"`              // Total payment amount
	CustomerRef AccountRef    `json:"CustomerRef"`           // Customer who made payment
	Line        []PaymentLine `json:"Line,omitempty"`        // CRITICAL: Contains which invoices were paid
	PrivateNote string        `json:"PrivateNote,omitempty"` // Our memo: "Payment recorded by: flexprice"
}
