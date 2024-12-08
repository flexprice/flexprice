package testutil

import (
	"context"
	"fmt"
	"sync"

	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/types"
)

type InMemoryMeterStore struct {
	mu     sync.RWMutex
	meters map[string]*meter.Meter
}

func NewInMemoryMeterStore() *InMemoryMeterStore {
	return &InMemoryMeterStore{
		meters: make(map[string]*meter.Meter),
	}
}

func (s *InMemoryMeterStore) CreateMeter(ctx context.Context, m *meter.Meter) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if m.ID == "" {
		return fmt.Errorf("meter ID cannot be empty")
	}

	if m.Name == "" {
		return fmt.Errorf("meter name cannot be empty")
	}

	if m.ResetUsage == "" {
		m.ResetUsage = types.ResetUsageBillingPeriod
	}

	s.meters[m.ID] = m
	return nil
}

func (s *InMemoryMeterStore) GetMeter(ctx context.Context, id string) (*meter.Meter, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	m, exists := s.meters[id]
	if !exists {
		return nil, fmt.Errorf("meter not found")
	}
	return m, nil
}

func (s *InMemoryMeterStore) GetAllMeters(ctx context.Context) ([]*meter.Meter, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	meters := make([]*meter.Meter, 0, len(s.meters))
	for _, m := range s.meters {
		meters = append(meters, m)
	}
	return meters, nil
}

func (s *InMemoryMeterStore) DisableMeter(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	m, exists := s.meters[id]
	if !exists {
		return fmt.Errorf("meter not found")
	}

	m.Status = types.StatusDeleted
	return nil
}
