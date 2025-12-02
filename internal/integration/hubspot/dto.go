package hubspot

import "time"

// WebhookEvent represents a HubSpot webhook event payload
type WebhookEvent struct {
	EventID          int64  `json:"eventId"`
	SubscriptionID   int64  `json:"subscriptionId"`
	PortalID         int64  `json:"portalId"`
	AppID            int64  `json:"appId"`
	OccurredAt       int64  `json:"occurredAt"`
	SubscriptionType string `json:"subscriptionType"`
	AttemptNumber    int    `json:"attemptNumber"`
	ObjectID         int64  `json:"objectId"`
	PropertyName     string `json:"propertyName,omitempty"`
	PropertyValue    string `json:"propertyValue,omitempty"`
	ChangeSource     string `json:"changeSource,omitempty"`
	SourceID         string `json:"sourceId,omitempty"`
	ChangeFlag       string `json:"changeFlag,omitempty"`
}

// WebhookPayload represents the array of webhook events
type WebhookPayload []WebhookEvent

// DealResponse represents a HubSpot deal object from the API
type DealResponse struct {
	ID         string         `json:"id"`
	Properties DealProperties `json:"properties"`
	CreatedAt  time.Time      `json:"createdAt"`
	UpdatedAt  time.Time      `json:"updatedAt"`
	Archived   bool           `json:"archived"`
}

// DealProperties represents HubSpot deal properties
type DealProperties struct {
	DealName  string `json:"dealname,omitempty"`  // Deal name
	Amount    string `json:"amount,omitempty"`    // Deal amount as decimal string
	DealStage string `json:"dealstage,omitempty"` // Deal stage ID
	Pipeline  string `json:"pipeline,omitempty"`  // Pipeline ID
	// ACV (Annual Contract Value) - calculated by HubSpot based on line items
	ACV string `json:"hs_acv,omitempty"`
	// MRR (Monthly Recurring Revenue) - calculated by HubSpot
	MRR string `json:"hs_mrr,omitempty"`
	// ARR (Annual Recurring Revenue) - calculated by HubSpot
	ARR string `json:"hs_arr,omitempty"`
	// TCV (Total Contract Value) - calculated by HubSpot
	TCV string `json:"hs_tcv,omitempty"`
}

// ContactResponse represents a HubSpot contact object from the API
type ContactResponse struct {
	ID         string            `json:"id"`
	Properties ContactProperties `json:"properties"`
	CreatedAt  time.Time         `json:"createdAt"`
	UpdatedAt  time.Time         `json:"updatedAt"`
	Archived   bool              `json:"archived"`
}

// ContactProperties represents HubSpot contact properties
type ContactProperties struct {
	Email     string `json:"email"`
	FirstName string `json:"firstname"`
	LastName  string `json:"lastname"`
	Phone     string `json:"phone"`
	Company   string `json:"company"`
	City      string `json:"city"`
	State     string `json:"state"`
	Country   string `json:"country"`
	Zip       string `json:"zip"`
	Address   string `json:"address"`
}

// AssociationResponse represents HubSpot deal-contact associations
type AssociationResponse struct {
	Results []AssociationResult `json:"results"`
}

// AssociationResult represents a single association between objects
type AssociationResult struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// Invoice DTOs

// InvoiceCreateRequest represents a request to create a HubSpot invoice
type InvoiceCreateRequest struct {
	Properties InvoiceProperties `json:"properties"`
}

// InvoiceProperties represents HubSpot invoice properties
type InvoiceProperties struct {
	Currency            string `json:"hs_currency"`                        // Required: ISO 4217 currency code (UPPERCASE, e.g., "USD")
	PurchaseOrderNumber string `json:"hs_purchase_order_number,omitempty"` // Optional: PO number (use this for invoice number)
	InvoiceDate         string `json:"hs_invoice_date,omitempty"`          // Unix timestamp in milliseconds
	DueDate             string `json:"hs_due_date,omitempty"`              // Unix timestamp in milliseconds
	AmountBilled        string `json:"hs_amount_billed,omitempty"`         // Amount as decimal string (e.g., "10.00")
	Tax                 string `json:"hs_tax,omitempty"`                   // Tax amount as decimal string
	Terms               string `json:"hs_terms,omitempty"`                 // Payment terms (e.g., "Net 30")
	InvoiceStatus       string `json:"hs_invoice_status,omitempty"`        // "draft" or "open"
	// Note: hs_balance_due is READ-ONLY and calculated by HubSpot automatically
	// Note: hs_invoice_number property doesn't exist, use hs_purchase_order_number instead
}

// InvoiceResponse represents a HubSpot invoice response
type InvoiceResponse struct {
	ID         string                 `json:"id"`
	Properties map[string]interface{} `json:"properties"`
	CreatedAt  time.Time              `json:"createdAt"`
	UpdatedAt  time.Time              `json:"updatedAt"`
}

// LineItemCreateRequest represents a request to create a HubSpot line item
type LineItemCreateRequest struct {
	Properties LineItemProperties `json:"properties"`
}

// LineItemProperties represents HubSpot line item properties
type LineItemProperties struct {
	Name                 string `json:"name"`                                // Product/service name
	Quantity             string `json:"quantity"`                            // Quantity as string (e.g., "1", "2.5")
	Price                string `json:"price"`                               // Price per unit as decimal (e.g., "10.00")
	Amount               string `json:"amount,omitempty"`                    // Total amount as decimal (quantity * price)
	RecurringBillingFreq string `json:"recurringbillingfrequency,omitempty"` // Billing frequency (e.g., "monthly", "annually")
	Description          string `json:"description,omitempty"`               // Optional description
}

// LineItemResponse represents a HubSpot line item response
type LineItemResponse struct {
	ID         string                 `json:"id"`
	Properties map[string]interface{} `json:"properties"`
	CreatedAt  time.Time              `json:"createdAt"`
	UpdatedAt  time.Time              `json:"updatedAt"`
}

// Deal DTOs

// DealUpdateRequest represents a request to update a HubSpot deal
type DealUpdateRequest struct {
	Properties map[string]string `json:"properties"`
}

// DealUpdateResponse represents a HubSpot deal update response
type DealUpdateResponse struct {
	ID         string         `json:"id"`
	Properties DealProperties `json:"properties"`
	CreatedAt  time.Time      `json:"createdAt"`
	UpdatedAt  time.Time      `json:"updatedAt"`
}

// Deal Line Item DTOs

// DealLineItemCreateRequest represents a request to create a HubSpot line item for a deal
type DealLineItemCreateRequest struct {
	Properties   DealLineItemProperties `json:"properties"`
	Associations []LineItemAssociation  `json:"associations,omitempty"`
}

// DealLineItemProperties represents HubSpot line item properties
type DealLineItemProperties struct {
	Name                 string `json:"name"`                                // Product/service name
	Price                string `json:"price"`                               // Unit price as decimal (e.g., "10.00")
	Quantity             string `json:"quantity"`                            // Quantity as string (e.g., "1", "2.5")
	Amount               string `json:"amount,omitempty"`                    // Total amount as decimal (quantity * price)
	Discount             string `json:"discount,omitempty"`                  // Discount amount or percentage
	RecurringBillingFreq string `json:"recurringbillingfrequency,omitempty"` // Billing frequency (e.g., "monthly", "annually")
	Description          string `json:"description,omitempty"`               // Line item description / pricing model
}

// LineItemAssociation represents an association between a line item and another object (deal, quote, etc.)
type LineItemAssociation struct {
	To    AssociationTarget `json:"to"`
	Types []AssociationType `json:"types"`
}

// AssociationTarget represents the target object for an association
type AssociationTarget struct {
	ID string `json:"id"`
}

// AssociationType represents the type of association
type AssociationType struct {
	AssociationCategory string `json:"associationCategory"` // e.g., "HUBSPOT_DEFINED"
	AssociationTypeID   int    `json:"associationTypeId"`   // e.g., 20 for line item to deal
}

// DealLineItemResponse represents a HubSpot line item response
type DealLineItemResponse struct {
	ID         string                 `json:"id"`
	Properties map[string]interface{} `json:"properties"`
	CreatedAt  time.Time              `json:"createdAt"`
	UpdatedAt  time.Time              `json:"updatedAt"`
}

// Quote DTOs

// QuoteCreateRequest represents a request to create a HubSpot quote
type QuoteCreateRequest struct {
	Properties QuoteProperties `json:"properties"`
}

// QuoteProperties represents HubSpot quote properties
type QuoteProperties struct {
	Title          string `json:"hs_title,omitempty"`           // Quote title
	ExpirationDate string `json:"hs_expiration_date,omitempty"` // Date format: "YYYY-MM-DD" (per docs examples)
	Status         string `json:"hs_status,omitempty"`          // Quote status: DRAFT, PENDING_APPROVAL, APPROVED, REJECTED, etc.
	ESignEnabled   string `json:"hs_esign_enabled,omitempty"`   // Enable e-signature: "true" or "false" as string (per docs line 59)
}

// QuoteResponse represents a HubSpot quote response
type QuoteResponse struct {
	ID         string                 `json:"id"`
	Properties map[string]interface{} `json:"properties"`
	CreatedAt  time.Time              `json:"createdAt"`
	UpdatedAt  time.Time              `json:"updatedAt"`
}

// Quote Line Item DTOs

// QuoteLineItemCreateRequest represents a request to create a HubSpot line item for a quote
type QuoteLineItemCreateRequest struct {
	Properties   DealLineItemProperties `json:"properties"` // Reuse DealLineItemProperties structure
	Associations []LineItemAssociation  `json:"associations,omitempty"`
}

// QuoteTemplate represents a HubSpot quote template
type QuoteTemplate struct {
	ID         string                 `json:"id"`
	Properties map[string]interface{} `json:"properties"`
	CreatedAt  time.Time              `json:"createdAt"`
	UpdatedAt  time.Time              `json:"updatedAt"`
}
