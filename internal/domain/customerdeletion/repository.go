package customerdeletion

import "context"

// Repository scopes every method to tenant and environment
type Repository interface {
	// Create writes the request and its category rows in one transaction
	Create(ctx context.Context, request *CustomerDeletionRequest, categories []*CustomerDeletionCategory) error

	// GetByCustomerID retrieves the deletion request for an internal customer ID
	GetByCustomerID(ctx context.Context, customerID string) (*CustomerDeletionRequest, error)

	// GetByExternalID retrieves the deletion request for a retired external ID
	GetByExternalID(ctx context.Context, externalID string) (*CustomerDeletionRequest, error)

	// ListCategories retrieves the category rows of a request
	ListCategories(ctx context.Context, requestID string) ([]*CustomerDeletionCategory, error)

	// UpdateState transitions state only when the current state equals from
	UpdateState(ctx context.Context, requestID string, from, to string) error
}
