package billingentity

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for billing entity data operations
type Repository interface {
	Create(ctx context.Context, entity *BillingEntity) error
	Get(ctx context.Context, id string) (*BillingEntity, error)
	// GetDefault returns the default entity of the environment in ctx.
	GetDefault(ctx context.Context) (*BillingEntity, error)
	List(ctx context.Context, filter *types.BillingEntityFilter) ([]*BillingEntity, error)
	Count(ctx context.Context, filter *types.BillingEntityFilter) (int, error)
	ListByIDs(ctx context.Context, ids []string) ([]*BillingEntity, error)
	Update(ctx context.Context, entity *BillingEntity) error
}
