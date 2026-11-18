package contract

import (
	"context"
	"encoding/json"

	domainContract "github.com/flexprice/flexprice/internal/domain/contract"
)

// Repository persists contracts and their associations.
type Repository interface {
	Create(ctx context.Context, c *domainContract.Contract) error
	CreateWithAssociation(ctx context.Context, c *domainContract.Contract, a *domainContract.Association) error
	GetByID(ctx context.Context, id string) (*domainContract.Contract, error)
	GetBySubscriptionID(ctx context.Context, subscriptionID string) (*domainContract.Contract, error)
	ListAssociations(ctx context.Context, contractID string) ([]*domainContract.Association, error)
	// Update writes every field except the review, which only UpdateReview writes.
	Update(ctx context.Context, c *domainContract.Contract) error
	// UpdateReview stores the review if the stored version is still version, and returns the new version.
	UpdateReview(ctx context.Context, id string, review json.RawMessage, version int) (int, error)
	List(ctx context.Context, filter *Filter) ([]*domainContract.Contract, error)
	Count(ctx context.Context, filter *Filter) (int, error)
}
