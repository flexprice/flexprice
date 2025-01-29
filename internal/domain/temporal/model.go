package temporal

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
)

// Service defines the temporal service interface
type Service interface {
	StartBillingWorkflow(ctx context.Context, input BillingWorkflowInput) (*BillingWorkflowResult, error)
}

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

// WorkerDependencies holds dependencies required by Temporal workers
type WorkerDependencies struct {
	InvoiceService InvoiceService
	PlanService    PlanService
	PriceService   PriceService
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
