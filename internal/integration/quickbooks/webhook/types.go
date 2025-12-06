package webhook

// QuickBooksWebhookPayload represents the top-level webhook payload from QuickBooks
// Reference: https://developer.intuit.com/app/developer/qbo/docs/develop/webhooks
type QuickBooksWebhookPayload struct {
	EventNotifications []EventNotification `json:"eventNotifications"`
}

// EventNotification represents a single event notification in the webhook payload
type EventNotification struct {
	RealmID         string          `json:"realmId"`
	DataChangeEvent DataChangeEvent `json:"dataChangeEvent"`
}

// DataChangeEvent contains the entities that have changed
type DataChangeEvent struct {
	Entities []EntityChange `json:"entities"`
}

// EntityChange represents a single entity change event
type EntityChange struct {
	Name        string `json:"name"`        // Entity type: "Payment", "Invoice", "Customer", etc.
	ID          string `json:"id"`          // Entity ID in QuickBooks
	Operation   string `json:"operation"`   // "Create", "Update", "Delete", "Merge", "Void"
	LastUpdated string `json:"lastUpdated"` // ISO 8601 timestamp
}

// EntityOperation constants
const (
	OperationCreate = "Create"
	OperationUpdate = "Update"
	OperationDelete = "Delete"
	OperationMerge  = "Merge"
	OperationVoid   = "Void"
)

// EntityName constants
const (
	EntityNamePayment  = "Payment"
	EntityNameInvoice  = "Invoice"
	EntityNameCustomer = "Customer"
)

// IsPaymentEvent checks if the entity change is for a Payment
func (e *EntityChange) IsPaymentEvent() bool {
	return e.Name == EntityNamePayment
}

// IsInvoiceEvent checks if the entity change is for an Invoice
func (e *EntityChange) IsInvoiceEvent() bool {
	return e.Name == EntityNameInvoice
}

// IsCreateOperation checks if the operation is Create
func (e *EntityChange) IsCreateOperation() bool {
	return e.Operation == OperationCreate
}

// IsUpdateOperation checks if the operation is Update
func (e *EntityChange) IsUpdateOperation() bool {
	return e.Operation == OperationUpdate
}

// ServiceDependencies holds the services needed by the webhook handler
type ServiceDependencies struct {
	PaymentService interface{} // interfaces.PaymentService
	InvoiceService interface{} // interfaces.InvoiceService
}

