package payment

import (
	"context"
	"errors"

	"github.com/flexprice/flexprice/internal/types"
)

// ErrIdempotencyKeyConflict marks the specific case of a Create losing the race
// on the (tenant_id, environment_id, idempotency_key) unique index — i.e. a
// concurrent request already inserted this exact payment intent.
//
// It exists to distinguish that case from every other constraint violation. The
// repository marks all constraint errors ErrAlreadyExists, so without this a
// duplicate payment ID would also look like an idempotency conflict, and the
// caller's re-fetch by idempotency key would miss and surface a misleading
// ErrNotFound. Errors carrying this are still marked ierr.ErrAlreadyExists, so
// the HTTP status stays 409.
var ErrIdempotencyKeyConflict = errors.New("payment idempotency key conflict")

// IsIdempotencyKeyConflict reports whether err came from losing the race on the
// idempotency-key unique index.
func IsIdempotencyKeyConflict(err error) bool {
	return errors.Is(err, ErrIdempotencyKeyConflict)
}

// ScopedPayment identifies a payment along with its tenant/environment so a
// cron running outside any tenant scope can re-establish context per payment.
type ScopedPayment struct {
	PaymentID        string
	TenantID         string
	EnvironmentID    string
	GatewayPaymentID string
}

// Repository defines the interface for payment persistence
type Repository interface {
	// Payment operations
	Create(ctx context.Context, payment *Payment) error
	Get(ctx context.Context, id string) (*Payment, error)
	Update(ctx context.Context, payment *Payment) error
	// UpdateWithExpectedStatus writes the payment only while its stored status
	// still equals expectedStatus, so a lifecycle check made against a value
	// read earlier cannot be applied on top of a concurrent update. Returns
	// ErrVersionConflict when the stored status has since changed.
	UpdateWithExpectedStatus(ctx context.Context, payment *Payment, expectedStatus types.PaymentStatus) error
	Delete(ctx context.Context, id string) error
	// DeleteWithExpectedStatus deletes the payment only while its stored status
	// still equals expectedStatus, so a deletability check made against a value
	// read earlier cannot be applied after a concurrent transition. Returns
	// ErrVersionConflict when the stored status has since changed.
	DeleteWithExpectedStatus(ctx context.Context, id string, expectedStatus types.PaymentStatus) error
	List(ctx context.Context, filter *types.PaymentFilter) ([]*Payment, error)
	Count(ctx context.Context, filter *types.PaymentFilter) (int, error)
	GetByIdempotencyKey(ctx context.Context, key string) (*Payment, error)

	// Payment attempt operations
	CreateAttempt(ctx context.Context, attempt *PaymentAttempt) error
	GetAttempt(ctx context.Context, id string) (*PaymentAttempt, error)
	UpdateAttempt(ctx context.Context, attempt *PaymentAttempt) error
	ListAttempts(ctx context.Context, paymentID string) ([]*PaymentAttempt, error)
	GetLatestAttempt(ctx context.Context, paymentID string) (*PaymentAttempt, error)

	// ListScopedByDestinationStatusGateway returns payments across all tenants and
	// environments matching the given destination type, payment status, and gateway.
	// Used by reconciliation crons that run outside any tenant scope.
	ListScopedByDestinationStatusGateway(ctx context.Context, destinationType types.PaymentDestinationType, status types.PaymentStatus, gateway types.PaymentGatewayType) ([]ScopedPayment, error)
}
