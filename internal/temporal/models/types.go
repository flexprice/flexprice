package models

import (
	"time"
)

// BillingWorkflowInput represents the input for a billing workflow
type BillingWorkflowInput struct {
	CustomerID     string    `json:"customer_id"`
	SubscriptionID string    `json:"subscription_id"`
	PeriodStart    time.Time `json:"period_start"`
	PeriodEnd      time.Time `json:"period_end"`
}

// BillingWorkflowResult represents the result of a billing workflow
type BillingWorkflowResult struct {
	InvoiceID string `json:"invoice_id"`
	Status    string `json:"status"`
}

// CalculationResult represents the result of a charge calculation
type CalculationResult struct {
	InvoiceID   string        `json:"invoice_id"`
	TotalAmount float64       `json:"total_amount"`
	Items       []InvoiceItem `json:"items"`
}

// InvoiceItem represents a line item in an invoice
type InvoiceItem struct {
	Description string  `json:"description"`
	Amount      float64 `json:"amount"`
}

// GenerateInvoiceRequest represents the request to generate an invoice
type GenerateInvoiceRequest struct {
	CustomerID     string    `json:"customer_id"`
	SubscriptionID string    `json:"subscription_id"`
	PeriodStart    time.Time `json:"period_start"`
	PeriodEnd      time.Time `json:"period_end"`
}

// CalculatePriceRequest represents the request to calculate price
type CalculatePriceRequest struct {
	CustomerID     string      `json:"customer_id"`
	SubscriptionID string      `json:"subscription_id"`
	UsageData      interface{} `json:"usage_data"`
}

// PriceSyncWorkflowInput represents input for the price sync workflow
type PriceSyncWorkflowInput struct {
	PlanID string `json:"plan_id"`
}
