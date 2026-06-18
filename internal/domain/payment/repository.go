package payment

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for payment persistence
type Repository interface {
	// Payment operations
	Create(ctx context.Context, payment *Payment) error
	Get(ctx context.Context, id string) (*Payment, error)
	Update(ctx context.Context, payment *Payment) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, filter *types.PaymentFilter) ([]*Payment, error)
	Count(ctx context.Context, filter *types.PaymentFilter) (int, error)
	GetByIdempotencyKey(ctx context.Context, key string) (*Payment, error)

	// ListSucceededMoyasarAuthPayments returns all SUCCEEDED Moyasar AUTH payments across
	// all tenants and environments (no tenant/env filter applied).
	ListSucceededMoyasarAuthPayments(ctx context.Context) ([]*Payment, error)

	// ListPendingMoyasarPayments returns all PENDING Moyasar payments across all tenants
	// and environments (no tenant/env filter). Used by the reconcile cron.
	ListPendingMoyasarPayments(ctx context.Context) ([]*Payment, error)

	// Payment attempt operations
	CreateAttempt(ctx context.Context, attempt *PaymentAttempt) error
	GetAttempt(ctx context.Context, id string) (*PaymentAttempt, error)
	UpdateAttempt(ctx context.Context, attempt *PaymentAttempt) error
	ListAttempts(ctx context.Context, paymentID string) ([]*PaymentAttempt, error)
	GetLatestAttempt(ctx context.Context, paymentID string) (*PaymentAttempt, error)
}
