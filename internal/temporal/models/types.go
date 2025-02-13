package models

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
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

// Service interfaces for dependencies
type InvoiceService interface {
	GenerateInvoice(ctx context.Context, req *dto.GenerateInvoiceRequest) (*dto.InvoiceResponse, error)
}

type PlanService interface {
	GetPlan(ctx context.Context, id string) (*dto.PlanResponse, error)
}

type PriceService interface {
	CalculatePrice(ctx context.Context, req *dto.CalculatePriceRequest) (*dto.PriceResponse, error)
}
