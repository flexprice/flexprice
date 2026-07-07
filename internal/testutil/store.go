package testutil

import (
	"context"
	"sort"
	"sync"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// FilterFunc is a generic filter function type
type FilterFunc[T any] func(ctx context.Context, item T, filter interface{}) bool

// SortFunc is a generic sort function type
type SortFunc[T any] func(i, j T) bool

// InMemoryStore implements a generic in-memory store
type InMemoryStore[T any] struct {
	mu    sync.RWMutex
	items map[string]T
	// cloneFn, when set, is applied on every write (Create/Update store a
	// copy) and every read (Get/List return copies). Real repositories
	// materialize a fresh row per read, so callers can never alias the
	// persisted record; without a cloneFn this store hands out raw pointers
	// and a post-persist mutation in a service silently edits "the database".
	// Wire it per store via WithCloneFn for pointer-typed T.
	cloneFn func(T) T
}

// NewInMemoryStore creates a new InMemoryStore
func NewInMemoryStore[T any]() *InMemoryStore[T] {
	return &InMemoryStore[T]{
		items: make(map[string]T),
	}
}

// WithCloneFn sets the clone function used to isolate stored records from
// caller-held references. Returns the store for chained construction.
func (s *InMemoryStore[T]) WithCloneFn(fn func(T) T) *InMemoryStore[T] {
	s.cloneFn = fn
	return s
}

func (s *InMemoryStore[T]) clone(item T) T {
	if s.cloneFn == nil {
		return item
	}
	return s.cloneFn(item)
}

// Create adds a new item to the store
func (s *InMemoryStore[T]) Create(ctx context.Context, id string, item T) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.items[id]; exists {
		return ierr.NewError("item already exists").
			WithHint("An item with this ID already exists").
			WithReportableDetails(map[string]any{
				"id": id,
			}).
			Mark(ierr.ErrAlreadyExists)
	}

	s.items[id] = s.clone(item)
	return nil
}

// Get retrieves an item by ID
func (s *InMemoryStore[T]) Get(ctx context.Context, id string) (T, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if item, exists := s.items[id]; exists {
		return s.clone(item), nil
	}

	var zero T
	return zero, ierr.NewError("item not found").
		WithHintf("Item with ID %s was not found", id).
		WithReportableDetails(map[string]any{
			"id": id,
		}).
		Mark(ierr.ErrNotFound)
}

// List retrieves items based on filter
func (s *InMemoryStore[T]) List(ctx context.Context, filter interface{}, filterFn FilterFunc[T], sortFn SortFunc[T]) ([]T, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []T
	for _, item := range s.items {
		if filterFn == nil || filterFn(ctx, item, filter) {
			result = append(result, s.clone(item))
		}
	}

	if sortFn != nil {
		sort.Slice(result, func(i, j int) bool {
			return sortFn(result[i], result[j])
		})
	}

	// Apply pagination if filter implements BaseFilter
	if f, ok := filter.(types.BaseFilter); ok && !f.IsUnlimited() {
		start := f.GetOffset()
		if start >= len(result) {
			return []T{}, nil
		}

		end := start + f.GetLimit()
		if end > len(result) {
			end = len(result)
		}
		return result[start:end], nil
	}

	return result, nil
}

// Count returns the total number of items matching the filter
func (s *InMemoryStore[T]) Count(ctx context.Context, filter interface{}, filterFn FilterFunc[T]) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, item := range s.items {
		if filterFn == nil || filterFn(ctx, item, filter) {
			count++
		}
	}

	return count, nil
}

// Update updates an existing item
func (s *InMemoryStore[T]) Update(ctx context.Context, id string, item T) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.items[id]; !exists {
		return ierr.NewError("item not found").
			WithHintf("Item with ID %s was not found", id).
			WithReportableDetails(map[string]any{
				"id": id,
			}).
			Mark(ierr.ErrNotFound)
	}

	s.items[id] = s.clone(item)
	return nil
}

// Delete removes an item from the store
func (s *InMemoryStore[T]) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.items[id]; !exists {
		return ierr.NewError("item not found").
			WithHintf("Item with ID %s was not found", id).
			WithReportableDetails(map[string]any{
				"id": id,
			}).
			Mark(ierr.ErrNotFound)
	}

	delete(s.items, id)
	return nil
}

// Clear removes all items from the store
func (s *InMemoryStore[T]) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = make(map[string]T)
}

// CheckEnvironmentFilter is a helper function to check if an item matches the environment filter
func CheckEnvironmentFilter(ctx context.Context, itemEnvID string) bool {
	environmentID := types.GetEnvironmentID(ctx)
	// If no environment ID is set in the context, or the item doesn't have an environment ID,
	// or the environment IDs match, then the item passes the filter
	return environmentID == "" || itemEnvID == "" || itemEnvID == environmentID
}

// CheckTenantFilter is a helper function to check if an item matches the tenant filter
func CheckTenantFilter(ctx context.Context, itemTenantID string) bool {
	tenantID := types.GetTenantID(ctx)
	// If no tenant ID is set in the context, or the item doesn't have a tenant ID,
	// or the tenant IDs match, then the item passes the filter
	return tenantID == "" || itemTenantID == "" || itemTenantID == tenantID
}
