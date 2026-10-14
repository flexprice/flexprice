package selfbilling // internal/domain/selfbilling

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// TenantEnvHourCount is the distinct events one tenant environment ingested in one hour.
type TenantEnvHourCount struct {
	TenantID      string
	EnvironmentID string
	Hour          time.Time
	Count         uint64
}

// FinalizedInvoice is the billable breakdown of one finalized invoice.
type FinalizedInvoice struct {
	ID                         string
	TenantID                   string
	EnvironmentID              string
	SubscriptionID             string
	InvoiceType                string
	BillingReason              string
	Currency                   string
	Subtotal                   decimal.Decimal
	TotalDiscount              decimal.Decimal
	TotalPrepaidCreditsApplied decimal.Decimal
	TotalTax                   decimal.Decimal
	Total                      decimal.Decimal
	AmountDue                  decimal.Decimal
	AmountPaid                 decimal.Decimal
	PeriodStart                *time.Time
	PeriodEnd                  *time.Time
	FinalizedAt                time.Time
}

// EventCountRepository counts events per tenant environment per ingest hour across every tenant. Only internal/ee/service/selfbilling may call it.
type EventCountRepository interface {
	CountByTenantHour(ctx context.Context, from, to time.Time) ([]*TenantEnvHourCount, error)
}

// InvoiceValueRepository lists finalized invoices across every tenant. Only internal/ee/service/selfbilling may call it.
type InvoiceValueRepository interface {
	ListFinalized(ctx context.Context, from, to time.Time) ([]*FinalizedInvoice, error)
}
