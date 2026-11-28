package accountingperiod

import (
	"context"
	"time"

	domainAccountingPeriod "github.com/flexprice/flexprice/internal/domain/accountingperiod"
)

// Repository persists accounting periods.
type Repository interface {
	// GetOrCreate returns the published period starting on periodStart, creating it open if missing.
	GetOrCreate(ctx context.Context, periodStart time.Time) (*domainAccountingPeriod.AccountingPeriod, error)
	List(ctx context.Context, filter *Filter) ([]*domainAccountingPeriod.AccountingPeriod, error)
	Count(ctx context.Context, filter *Filter) (int, error)
}
