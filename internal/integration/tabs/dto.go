package tabs

// CreateCustomerRequest is the body for POST /v3/customers.
type CreateCustomerRequest struct {
	Name                       string `json:"name"`
	PrimaryBillingContactEmail string `json:"primaryBillingContactEmail"`
	DefaultCurrency            string `json:"defaultCurrency"`
}

// createCustomerResponse is the envelope returned by POST /v3/customers.
// Tabs runs customer creation as an async job; the real id is fetched from the job.
type CreateCustomerResponse struct {
	Payload struct {
		JobID string `json:"jobId"`
	} `json:"payload"`
	Success bool `json:"success"`
}

// CreateProductRequest is the body for POST /v3/products.
type CreateProductRequest struct {
	Status      string `json:"status"` // e.g. ACTIVE
	Name        string `json:"name"`
	Description string `json:"description"`
}

// CreateProductResponse is the envelope returned by POST /v3/products.
// Product creation is synchronous: the id is in the payload directly.
type CreateProductResponse struct {
	Payload struct {
		ID string `json:"id"`
	} `json:"payload"`
	Success bool `json:"success"`
}

// CreateContractRequest is the body for POST /v3/contracts.
type CreateContractRequest struct {
	Name       string `json:"name"`
	CustomerID string `json:"customerId"` // Tabs customer id
}

// createContractResponse is the envelope returned by POST /v3/contracts.
// Contract creation is synchronous: the contract id is in the payload directly.
type CreateContractResponse struct {
	Payload struct {
		ID string `json:"id"`
	} `json:"payload"`
	Success bool `json:"success"`
}

// TabsInvoice is a Tabs invoice as returned by GET /v3/invoices.
// NOTE: field mapping is a best guess pending a confirmed sample response.
type TabsInvoice struct {
	ID string `json:"id"`
}

// listInvoicesResponse is the envelope returned by GET /v3/invoices.
// NOTE: assumes a paginated payload with a `data` array — confirm against a real response.
type ListInvoicesResponse struct {
	Payload struct {
		Data []TabsInvoice `json:"data"`
	} `json:"payload"`
	Success bool `json:"success"`
}

// CreateObligationRequest is the body for POST /v3/contracts/{id}/obligations.
// An obligation is Tabs' unit for a charge; it maps to one or more flexprice invoice line items.
type CreateObligationRequest struct {
	ServiceStartDate string          `json:"serviceStartDate"` // YYYY-MM-DD
	ServiceEndDate   string          `json:"serviceEndDate"`   // YYYY-MM-DD
	BillingSchedule  BillingSchedule `json:"billingSchedule"`
}

// BillingSchedule describes how an obligation is billed.
type BillingSchedule struct {
	StartDate           string              `json:"startDate"`           // YYYY-MM-DD
	InvoiceDateStrategy string              `json:"invoiceDateStrategy"` // ARREARS | ADVANCE
	IsRecurring         bool                `json:"isRecurring"`
	Interval            string              `json:"interval"` // e.g. NONE
	IntervalFrequency   int                 `json:"intervalFrequency"`
	NetPaymentTerms     int                 `json:"netPaymentTerms"`
	Quantity            int                 `json:"quantity"`
	BillingType         string              `json:"billingType"` // e.g. FLAT
	PricingType         string              `json:"pricingType"` // e.g. SIMPLE
	InvoiceType         string              `json:"invoiceType"` // e.g. INVOICE
	ProductId           string              `json:"productId"`
	Pricing             []ObligationPricing `json:"pricing"`
}

// ObligationPricing is one pricing tier of an obligation.
type ObligationPricing struct {
	Tier        int     `json:"tier"`
	Amount      float64 `json:"amount"`
	AmountType  string  `json:"amountType"` // e.g. TOTAL_INVOICE
	TierMinimum float64 `json:"tierMinimum"`
}

// createObligationResponse is the envelope returned by POST /v3/contracts/{id}/obligations.
type CreateObligationResponse struct {
	Payload struct {
		ID string `json:"id"`
	} `json:"payload"`
	Success bool `json:"success"`
}

// ContractActionRequest is the body for POST /v3/contracts/{id}/actions.
type ContractActionRequest struct {
	Action string `json:"action"`
}

// ContractActionMarkAsProcessed transitions a contract from NEW to PROCESSED.
const ContractActionMarkAsProcessed = "MARK_AS_PROCESSED"

// JobPayload is the job state returned by GET /v3/jobs/{jobId}.
type JobPayload struct {
	ID      string `json:"id"`
	Status  string `json:"status"` // e.g. SUCCESS, FAILED, IN_PROGRESS
	Type    string `json:"type"`
	Results struct {
		Data   map[string]any `json:"data"`
		Status string         `json:"status"`
	} `json:"results"`
}

// jobResponse is the envelope returned by GET /v3/jobs/{jobId}.
type JobResponse struct {
	Payload JobPayload `json:"payload"`
	Success bool       `json:"success"`
}

// TabsInvoiceSyncRequest is the input to InvoiceService.SyncInvoiceToTabs.
type TabsInvoiceSyncRequest struct {
	InvoiceID string `json:"invoice_id"`
}

// TabsInvoiceSyncResponse is the outcome of syncing a FlexPrice invoice to Tabs. The invoice is
// mapped to its Tabs contract; the Tabs customer and invoice ids are surfaced for observability.
type TabsInvoiceSyncResponse struct {
	ContractID     string `json:"contract_id"`
	TabsCustomerID string `json:"tabs_customer_id"`
	TabsInvoiceID  string `json:"tabs_invoice_id"`
	Currency       string `json:"currency"`
}
