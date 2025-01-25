package models

import "time"

// BillingWorkflowInput represents the input for a billing workflow.
type BillingWorkflowInput struct {
	// CustomerID is the identifier for the customer in our system.
	CustomerID string `json:"customer_id"`

	// SubscriptionID is the identifier for the subscription in our system.
	SubscriptionID string `json:"subscription_id"`

	// PeriodStart is the start of the billing period.
	PeriodStart time.Time `json:"period_start"`

	// PeriodEnd is the end of the billing period.
	PeriodEnd time.Time `json:"period_end"`
}

// BillingWorkflowResult represents the result of a billing workflow.
type BillingWorkflowResult struct {
	// InvoiceID is the unique identifier for the generated invoice.
	InvoiceID string `json:"invoice_id"`

	// Status indicates the status of the workflow (e.g., "scheduled", "completed", "failed").
	Status string `json:"status"`
}
