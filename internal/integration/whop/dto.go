package whop

// Constants for Whop integration
const (
	// WhopBaseURL is the Whop API base URL. Sandbox for now; swap to api.whop.com for prod.
	WhopBaseURL = "https://api.whop.com"
	// WhopBaseURL = "https://sandbox-api.whop.com" // for sandbox whop testing

	DefaultProductTitle       = "Flexprice Subscription"
	DefaultProductDescription = "Flexprice invoice sync product"
)

// WhopConfig holds decrypted Whop configuration
type WhopConfig struct {
	APIKey    string
	CompanyID string
	ProductID string
}

// CreateProductRequest is the request body for POST /v1/products
type CreateProductRequest struct {
	CompanyID   string                 `json:"company_id"`
	Title       string                 `json:"title"`
	Description string                 `json:"description,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// ProductResponse is the response from POST /v1/products and GET /v1/products/:id
type ProductResponse struct {
	ID string `json:"id"`
}

// InvoicePlan represents the plan section of a Whop invoice create request
type InvoicePlan struct {
	InitialPrice  float64 `json:"initial_price"`
	PlanType      string  `json:"plan_type"`
	InternalNotes string  `json:"internal_notes,omitempty"`
}

// CreateInvoiceRequest is the request body for POST /v1/invoices
type CreateInvoiceRequest struct {
	CompanyID        string      `json:"company_id"`
	ProductID        string      `json:"product_id"`
	Plan             InvoicePlan `json:"plan"`
	CollectionMethod string      `json:"collection_method"`
	DueDate          string      `json:"due_date"`
	CustomerName     string      `json:"customer_name"`
	EmailAddress     string      `json:"email_address"`
}

// InvoiceResponse is the response from POST /v1/invoices
type InvoiceResponse struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	CurrentPlan struct {
		ID string `json:"id"`
	} `json:"current_plan"`
}

// PlanResponse is the response from GET /v1/plans/:id
type PlanResponse struct {
	ID            string                 `json:"id"`
	PlanType      string                 `json:"plan_type"`
	PurchaseURL   string                 `json:"purchase_url"`
	Currency      string                 `json:"currency"`
	InternalNotes string                 `json:"internal_notes"`
	Metadata      map[string]interface{} `json:"metadata"`
	Product       struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"product"`
	Invoice struct {
		ID string `json:"id"`
	} `json:"invoice"`
	InitialPrice float64 `json:"initial_price"`
}

// WhopInvoiceSyncRequest is the request for syncing a Flexprice invoice to Whop
type WhopInvoiceSyncRequest struct {
	InvoiceID string
}

// WhopInvoiceSyncResponse is the response from syncing a Flexprice invoice to Whop
type WhopInvoiceSyncResponse struct {
	WhopInvoiceID string
	Status        string
}
