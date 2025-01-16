package domain

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
)

type BillingWorkflowResult struct {
	InvoiceID string
	Status    string
}

type BillingWorkflowInput struct {
	CustomerID     string
	SubscriptionID string
	PeriodStart    time.Time
	PeriodEnd      time.Time
}

// Simple interfaces without DTOs
type InvoiceService interface {
	GenerateInvoice(ctx context.Context, req *dto.GenerateInvoiceRequest) (*dto.InvoiceResponse, error)
}

type PlanService interface {
	GetPlan(ctx context.Context, id string) (*dto.PlanResponse, error)
}

type PriceService interface {
	CalculatePrice(ctx context.Context, req *dto.CalculatePriceRequest) (*dto.PriceResponse, error)
}

type WorkerDependencies struct {
	InvoiceService InvoiceService
	PlanService    PlanService
	PriceService   PriceService
}

type TemporalService interface {
	StartBillingWorkflow(ctx context.Context, customerID, subscriptionID string, periodStart, periodEnd time.Time) (*BillingWorkflowResult, error)
}
