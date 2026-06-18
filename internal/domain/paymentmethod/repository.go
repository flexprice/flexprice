package paymentmethod

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for payment method data access.
type Repository interface {
	Create(ctx context.Context, pm *PaymentMethod) error
	GetByID(ctx context.Context, id string) (*PaymentMethod, error)
	List(ctx context.Context, filter *types.PaymentMethodFilter) ([]*PaymentMethod, error)
	Update(ctx context.Context, pm *PaymentMethod) error
}
