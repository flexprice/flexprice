package models

import "time"

// BillingWorkflowInput represents the input for a billing workflow.
type BillingWorkflowInput struct {
	CustomerID     string    `json:"customer_id"`
	SubscriptionID string    `json:"subscription_id"`
	PeriodStart    time.Time `json:"period_start"`
	PeriodEnd      time.Time `json:"period_end"`
}

// BillingWorkflowResult represents the result of a billing workflow.
type BillingWorkflowResult struct {
	InvoiceID string `json:"invoice_id"`
	Status    string `json:"status"`
}
