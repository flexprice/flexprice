package creditgrantapplication

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for credit grant application data access
type Repository interface {
	Create(ctx context.Context, application *CreditGrantApplication) error
	Get(ctx context.Context, id string) (*CreditGrantApplication, error)
	List(ctx context.Context, filter *types.CreditGrantApplicationFilter) ([]*CreditGrantApplication, error)
	Count(ctx context.Context, filter *types.CreditGrantApplicationFilter) (int, error)
	ListAll(ctx context.Context, filter *types.CreditGrantApplicationFilter) ([]*CreditGrantApplication, error)
	Update(ctx context.Context, application *CreditGrantApplication) error
	Delete(ctx context.Context, application *CreditGrantApplication) error
	ExistsForPeriod(ctx context.Context, grantID, subscriptionID string, periodStart, periodEnd time.Time) (bool, error)

	// Cronjobs (this runs every 15 mins)
	FindAllScheduledApplications(ctx context.Context) ([]*CreditGrantApplication, error)

	// FindByIdempotencyKey finds a credit grant application by idempotency key
	FindByIdempotencyKey(ctx context.Context, idempotencyKey string) (*CreditGrantApplication, error)
}
