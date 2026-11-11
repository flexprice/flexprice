package contract

import (
	"context"

	domainContract "github.com/flexprice/flexprice/internal/domain/contract"
)

// Repository persists contracts and their associations.
type Repository interface {
	CreateWithAssociation(ctx context.Context, c *domainContract.Contract, a *domainContract.Association) error
	GetByID(ctx context.Context, id string) (*domainContract.Contract, error)
	GetBySubscriptionID(ctx context.Context, subscriptionID string) (*domainContract.Contract, error)
	ListAssociations(ctx context.Context, contractID string) ([]*domainContract.Association, error)
	Update(ctx context.Context, c *domainContract.Contract) error
	List(ctx context.Context, filter *Filter) ([]*domainContract.Contract, error)
	Count(ctx context.Context, filter *Filter) (int, error)
}
