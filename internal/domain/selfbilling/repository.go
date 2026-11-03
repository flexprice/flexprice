package selfbilling

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// TenantHourEvents is the distinct events one tenant environment ingested in one UTC hour.
type TenantHourEvents struct {
	TenantID      string
	EnvironmentID string
	Hour          time.Time
	Count         uint64
}

// TenantHourInvoices sums the invoices one tenant environment finalized in one currency in one UTC hour.
type TenantHourInvoices struct {
	TenantID                   string
	EnvironmentID              string
	Currency                   string
	Hour                       time.Time
	InvoiceCount               uint64
	Subtotal                   decimal.Decimal
	TotalDiscount              decimal.Decimal
	TotalPrepaidCreditsApplied decimal.Decimal
	TotalTax                   decimal.Decimal
	Total                      decimal.Decimal
	AmountDue                  decimal.Decimal
	AmountPaid                 decimal.Decimal
}

// ClickHouseRepository holds self-billing's cross-tenant analytics reads. Only internal/ee/service/selfbilling may call it.
type ClickHouseRepository interface {
	// CountEventsByTenantHour counts distinct events per tenant environment per ingest hour in [from, to).
	CountEventsByTenantHour(ctx context.Context, from, to time.Time) ([]*TenantHourEvents, error)
}

// PostgresRepository holds self-billing's cross-tenant transactional reads. Only internal/ee/service/selfbilling may call it.
type PostgresRepository interface {
	// SumInvoicesByTenantHour sums finalized invoices per tenant environment, currency and finalized hour in [from, to).
	SumInvoicesByTenantHour(ctx context.Context, from, to time.Time) ([]*TenantHourInvoices, error)
}
