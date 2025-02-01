package testutil

import (
	"context"
	"fmt"
	"sort"
	"sync"

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
}

// NewInMemoryStore creates a new InMemoryStore
func NewInMemoryStore[T any]() *InMemoryStore[T] {
	return &InMemoryStore[T]{
		items: make(map[string]T),
	}
}

// Create adds a new item to the store
func (s *InMemoryStore[T]) Create(ctx context.Context, id string, item T) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.items[id]; exists {
		return fmt.Errorf("item already exists")
	}

	s.items[id] = item
	return nil
}

// Get retrieves an item by ID
func (s *InMemoryStore[T]) Get(ctx context.Context, id string) (T, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if item, exists := s.items[id]; exists {
		return item, nil
	}

	var zero T
	return zero, fmt.Errorf("item not found")
}

// List retrieves items based on filter
func (s *InMemoryStore[T]) List(ctx context.Context, filter interface{}, filterFn FilterFunc[T], sortFn SortFunc[T]) ([]T, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []T
	for _, item := range s.items {
		if filterFn == nil || filterFn(ctx, item, filter) {
			result = append(result, item)
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
		return fmt.Errorf("item not found")
	}

	s.items[id] = item
	return nil
}

// Delete removes an item from the store
func (s *InMemoryStore[T]) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.items[id]; !exists {
		return fmt.Errorf("item not found")
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
