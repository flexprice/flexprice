package testutil

import (
	"context"
	"sync"

	"github.com/flexprice/flexprice/internal/domain/customerdeletion"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryCustomerDeletionStore implements customerdeletion.Repository
type InMemoryCustomerDeletionStore struct {
	// Stands in for Postgres atomicity
	mu         sync.Mutex
	requests   *InMemoryStore[*customerdeletion.CustomerDeletionRequest]
	categories *InMemoryStore[*customerdeletion.CustomerDeletionCategory]
}

// NewInMemoryCustomerDeletionStore creates a new in-memory customer deletion store
func NewInMemoryCustomerDeletionStore() *InMemoryCustomerDeletionStore {
	return &InMemoryCustomerDeletionStore{
		requests:   NewInMemoryStore[*customerdeletion.CustomerDeletionRequest](),
		categories: NewInMemoryStore[*customerdeletion.CustomerDeletionCategory](),
	}
}

func copyDeletionRequest(r *customerdeletion.CustomerDeletionRequest) *customerdeletion.CustomerDeletionRequest {
	if r == nil {
		return nil
	}
	c := *r
	if r.RetentionPolicy != nil {
		c.RetentionPolicy = lo.Assign(map[string]interface{}{}, r.RetentionPolicy)
	}
	return &c
}

func copyDeletionCategory(c *customerdeletion.CustomerDeletionCategory) *customerdeletion.CustomerDeletionCategory {
	if c == nil {
		return nil
	}
	cp := *c
	if c.VerificationResult != nil {
		cp.VerificationResult = lo.Assign(map[string]interface{}{}, c.VerificationResult)
	}
	return &cp
}

// deletionScoped is strict; leniency would hide leaks
func deletionScoped(ctx context.Context, tenantID, environmentID string) bool {
	return tenantID == types.GetTenantID(ctx) && environmentID == types.GetEnvironmentID(ctx)
}

func (s *InMemoryCustomerDeletionStore) Create(ctx context.Context, request *customerdeletion.CustomerDeletionRequest, categories []*customerdeletion.CustomerDeletionCategory) error {
	if request == nil {
		return ierr.NewError("deletion request is required").
			WithHint("A deletion request must be provided").
			Mark(ierr.ErrValidation)
	}

	// SLA clock cannot start at zero
	if request.RequestedAt.IsZero() {
		return ierr.NewError("requested_at is required").
			WithHint("A deletion request must carry its request time").
			Mark(ierr.ErrValidation)
	}

	if err := customerdeletion.ValidateCategories(request.ID, categories); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	request.TenantID = tenantID
	request.EnvironmentID = environmentID

	existing, err := s.requests.List(ctx, nil, func(ctx context.Context, r *customerdeletion.CustomerDeletionRequest, _ interface{}) bool {
		return deletionScoped(ctx, r.TenantID, r.EnvironmentID) &&
			(r.CustomerID == request.CustomerID || r.ExternalID == request.ExternalID)
	}, nil)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return ierr.NewError("customer deletion request already exists").
			WithHint("A deletion request already exists for this customer or external ID").
			WithReportableDetails(map[string]any{
				"customer_id": request.CustomerID,
				"external_id": request.ExternalID,
			}).
			Mark(ierr.ErrAlreadyExists)
	}

	pending := make([]*customerdeletion.CustomerDeletionCategory, 0, len(categories))
	for _, category := range categories {
		if category == nil {
			continue
		}
		category.TenantID = tenantID
		category.EnvironmentID = environmentID
		category.RequestID = request.ID

		if _, err := s.categories.Get(ctx, category.ID); err == nil {
			return ierr.NewError("customer deletion category already exists").
				WithHintf("Category ID %s already exists", category.ID).
				WithReportableDetails(map[string]any{
					"request_id": request.ID,
					"id":         category.ID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		pending = append(pending, copyDeletionCategory(category))
	}

	if err := s.requests.Create(ctx, request.ID, copyDeletionRequest(request)); err != nil {
		return err
	}

	for _, category := range pending {
		if err := s.categories.Create(ctx, category.ID, category); err != nil {
			return err
		}
	}

	return nil
}

func (s *InMemoryCustomerDeletionStore) GetByCustomerID(ctx context.Context, customerID string) (*customerdeletion.CustomerDeletionRequest, error) {
	return s.getBy(ctx, func(ctx context.Context, r *customerdeletion.CustomerDeletionRequest) bool {
		return r.CustomerID == customerID
	}, "customer_id", customerID)
}

func (s *InMemoryCustomerDeletionStore) GetByExternalID(ctx context.Context, externalID string) (*customerdeletion.CustomerDeletionRequest, error) {
	return s.getBy(ctx, func(ctx context.Context, r *customerdeletion.CustomerDeletionRequest) bool {
		return r.ExternalID == externalID
	}, "external_id", externalID)
}

func (s *InMemoryCustomerDeletionStore) IsExternalIDRetired(ctx context.Context, externalID string) (bool, error) {
	if externalID == "" {
		return false, nil
	}

	items, err := s.requests.List(ctx, nil, func(ctx context.Context, r *customerdeletion.CustomerDeletionRequest, _ interface{}) bool {
		return deletionScoped(ctx, r.TenantID, r.EnvironmentID) && r.ExternalID == externalID
	}, nil)
	if err != nil {
		return false, err
	}

	return len(items) > 0, nil
}

func (s *InMemoryCustomerDeletionStore) getBy(ctx context.Context, match func(context.Context, *customerdeletion.CustomerDeletionRequest) bool, key, value string) (*customerdeletion.CustomerDeletionRequest, error) {
	items, err := s.requests.List(ctx, nil, func(ctx context.Context, r *customerdeletion.CustomerDeletionRequest, _ interface{}) bool {
		return deletionScoped(ctx, r.TenantID, r.EnvironmentID) && match(ctx, r)
	}, nil)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ierr.NewError("customer deletion request not found").
			WithHintf("No deletion request exists for %s %s", key, value).
			WithReportableDetails(map[string]any{
				key: value,
			}).
			Mark(ierr.ErrNotFound)
	}
	return copyDeletionRequest(items[0]), nil
}

func (s *InMemoryCustomerDeletionStore) ListCategories(ctx context.Context, requestID string) ([]*customerdeletion.CustomerDeletionCategory, error) {
	items, err := s.categories.List(ctx, nil, func(ctx context.Context, c *customerdeletion.CustomerDeletionCategory, _ interface{}) bool {
		return deletionScoped(ctx, c.TenantID, c.EnvironmentID) && c.RequestID == requestID
	}, func(i, j *customerdeletion.CustomerDeletionCategory) bool {
		return i.Category < j.Category
	})
	if err != nil {
		return nil, err
	}
	return lo.Map(items, func(c *customerdeletion.CustomerDeletionCategory, _ int) *customerdeletion.CustomerDeletionCategory {
		return copyDeletionCategory(c)
	}), nil
}

func (s *InMemoryCustomerDeletionStore) UpdateState(ctx context.Context, requestID string, from, to string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, err := s.requests.Get(ctx, requestID)
	if err != nil || !deletionScoped(ctx, existing.TenantID, existing.EnvironmentID) || existing.State != from {
		return ierr.NewError("customer deletion request state transition rejected").
			WithHintf("No request %s in state %s for this tenant and environment", requestID, from).
			WithReportableDetails(map[string]any{
				"request_id": requestID,
				"from":       from,
				"to":         to,
			}).
			Mark(ierr.ErrNotFound)
	}

	updated := copyDeletionRequest(existing)
	updated.State = to
	return s.requests.Update(ctx, requestID, updated)
}

func (s *InMemoryCustomerDeletionStore) Clear() {
	s.requests.Clear()
	s.categories.Clear()
}
