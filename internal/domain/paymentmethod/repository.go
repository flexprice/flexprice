package paymentmethod

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for payment method persistence
type Repository interface {
	Create(ctx context.Context, pm *PaymentMethod) error
	Get(ctx context.Context, id string) (*PaymentMethod, error)
	Update(ctx context.Context, pm *PaymentMethod) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, filter *types.PaymentMethodFilter) ([]*PaymentMethod, error)
	Count(ctx context.Context, filter *types.PaymentMethodFilter) (int, error)
	// GetDefaultForCustomer returns the default payment method for a customer at a given gateway.
	// Returns ErrNotFound if no default is set.
	GetDefaultForCustomer(ctx context.Context, customerID, gateway string) (*PaymentMethod, error)
}
