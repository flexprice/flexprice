package testutil

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
)

// InMemorySubscriptionStore implements subscription.Repository
type InMemorySubscriptionStore struct {
	mu            sync.RWMutex
	subscriptions map[string]*subscription.Subscription
}

func NewInMemorySubscriptionStore() *InMemorySubscriptionStore {
	return &InMemorySubscriptionStore{
		subscriptions: make(map[string]*subscription.Subscription),
	}
}

func (s *InMemorySubscriptionStore) Create(ctx context.Context, sub *subscription.Subscription) error {
	if sub == nil {
		return fmt.Errorf("subscription cannot be nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.subscriptions[sub.ID]; exists {
		return fmt.Errorf("subscription already exists")
	}

	s.subscriptions[sub.ID] = sub
	return nil
}

func (s *InMemorySubscriptionStore) Get(ctx context.Context, id string) (*subscription.Subscription, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if sub, exists := s.subscriptions[id]; exists {
		return sub, nil
	}
	return nil, fmt.Errorf("subscription not found")
}

func (s *InMemorySubscriptionStore) List(ctx context.Context, filter *types.SubscriptionFilter) ([]*subscription.Subscription, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*subscription.Subscription
	for _, sub := range s.subscriptions {
		if filter == nil {
			result = append(result, sub)
			continue
		}

		// Keep only basic filters that are essential for tests
		if filter.CustomerID != "" && sub.CustomerID != filter.CustomerID {
			continue
		}

		if filter.PlanID != "" && sub.PlanID != filter.PlanID {
			continue
		}

		if filter.SubscriptionStatus != "" && sub.SubscriptionStatus != filter.SubscriptionStatus {
			continue
		}

		if filter.Status != "" && sub.Status != filter.Status {
			continue
		}

		result = append(result, sub)
	}

	// Sort by created date desc (default)
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})

	// Apply pagination if filter has limit/offset
	if filter != nil {
		start := filter.Offset
		if start >= len(result) {
			return []*subscription.Subscription{}, nil
		}

		end := start + filter.Limit
		if end > len(result) {
			end = len(result)
		}

		result = result[start:end]
	}

	return result, nil
}

func (s *InMemorySubscriptionStore) Update(ctx context.Context, sub *subscription.Subscription) error {
	if sub == nil {
		return fmt.Errorf("subscription cannot be nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.subscriptions[sub.ID]; !exists {
		return fmt.Errorf("subscription not found")
	}

	s.subscriptions[sub.ID] = sub
	return nil
}

func (s *InMemorySubscriptionStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.subscriptions[id]; !exists {
		return fmt.Errorf("subscription not found")
	}

	delete(s.subscriptions, id)
	return nil
}

func (s *InMemorySubscriptionStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.subscriptions = make(map[string]*subscription.Subscription)
}
