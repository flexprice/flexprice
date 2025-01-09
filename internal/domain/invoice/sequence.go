package invoice

import (
	"time"
)

// InvoiceSequence represents a tenant's invoice number sequence for a specific month
type InvoiceSequence struct {
	ID        string
	TenantID  string
	YearMonth string
	LastValue int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// BillingSequence represents a subscription's billing sequence
type BillingSequence struct {
	ID             string
	TenantID       string
	SubscriptionID string
	LastSequence   int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
