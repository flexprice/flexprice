package whop

// Base URL
const (
	WhopBaseURL = "https://api.whop.com"
)

// Defaults and constants
const (
	DefaultProductTitle                     = "Flexprice Billing Product"
	DefaultInvoiceDueDays                   = 30
	WhopVisibilityQuickLink                 = "quick_link"
	WhopPlanTypeOneTime                     = "one_time"
	WhopCollectionMethodSendInvoice         = "send_invoice"
	WhopCollectionMethodChargeAutomatically = "charge_automatically"
	WhopInvoiceStatusPaid                   = "paid"
)

// Webhook event types
const (
	WhopEventInvoicePaid      = "invoice.paid"
	WhopEventPaymentSucceeded = "payment.succeeded"
)

// WhopConfig holds decrypted Whop credentials from the connection
type WhopConfig struct {
	APIKey    string
	CompanyID string
	ProductID string
}

// --- Product ---

// CreateProductRequest is the request body for POST /v1/products
type CreateProductRequest struct {
	CompanyID  string `json:"company_id"`
	Title      string `json:"title"`
	Visibility string `json:"visibility,omitempty"`
}

// ProductResponse is the response from POST /v1/products and GET /v1/products/:id
type ProductResponse struct {
	ID string `json:"id"`
}

// --- Plan ---

// PlanProductRef is the product reference embedded in a PlanResponse
type PlanProductRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// PlanInvoiceRef is the invoice reference embedded in a PlanResponse
type PlanInvoiceRef struct {
	ID string `json:"id"`
}

// PlanResponse is the response from GET /v1/plans/:id
type PlanResponse struct {
	ID            string                 `json:"id"`
	PlanType      string                 `json:"plan_type"`
	PurchaseURL   string                 `json:"purchase_url"`
	Currency      string                 `json:"currency"`
	InternalNotes string                 `json:"internal_notes"`
	Metadata      map[string]interface{} `json:"metadata"`
	InitialPrice  float64                `json:"initial_price"`
	Product       PlanProductRef         `json:"product"`
	Invoice       PlanInvoiceRef         `json:"invoice"`
}

// --- Invoice ---

// CreateInvoicePlan is the plan ref within a CreateInvoiceRequest.
// InitialPrice is float64 — decimal.Decimal marshals to a quoted string by default,
// but Whop expects a bare JSON number. Round to 2 dp before setting.
// InternalNotes is the customer_id of the Flexprice customer
type CreateInvoicePlan struct {
	InitialPrice  float64 `json:"initial_price"`
	PlanType      string  `json:"plan_type"`
	InternalNotes string  `json:"internal_notes"`
}

// CreateInvoiceRequest is the request body for POST /v1/invoices
type CreateInvoiceRequest struct {
	CompanyID        string            `json:"company_id"`
	ProductID        string            `json:"product_id"`
	Plan             CreateInvoicePlan `json:"plan"`
	CollectionMethod string            `json:"collection_method"`
	PaymentMethodID  string            `json:"payment_method_id,omitempty"`
	DueDate          string            `json:"due_date"`
	CustomerName     string            `json:"customer_name"`
	EmailAddress     string            `json:"email_address"`
}

// InvoiceCurrentPlanRef is the plan reference embedded in an InvoiceResponse
type InvoiceCurrentPlanRef struct {
	ID string `json:"id"`
}

// InvoiceResponse is the response from POST /v1/invoices and GET /v1/invoices/:id
type InvoiceResponse struct {
	ID          string                `json:"id"`
	Status      string                `json:"status"`
	CurrentPlan InvoiceCurrentPlanRef `json:"current_plan"`
}

// --- Sync ---

// WhopInvoiceSyncRequest is the request for syncing a Flexprice invoice to Whop
type WhopInvoiceSyncRequest struct {
	InvoiceID string
}

// WhopInvoiceSyncResponse is the response from syncing a Flexprice invoice to Whop
type WhopInvoiceSyncResponse struct {
	WhopInvoiceID string
	Status        string
}

// --- Payment Methods ---

// PaymentMethod represents a Whop payment method — only the ID is needed for charge_automatically
type PaymentMethod struct {
	ID string `json:"id"`
}

// PaymentMethodsResponse is the response from GET /v1/payment_methods
type PaymentMethodsResponse struct {
	Data []PaymentMethod `json:"data"`
}
