package workflows

import (
	"time"
)

type BillingWorkflowInput struct {
	CustomerID     string
	SubscriptionID string
	PeriodStart    time.Time
	PeriodEnd      time.Time
}

type BillingWorkflowResult struct {
	InvoiceID string
	Status    string
}
