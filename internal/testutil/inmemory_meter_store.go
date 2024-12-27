package testutil

import (
	"context"
	"fmt"
	"sync"

	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/types"
)

type InMemoryMeterRepository struct {
	mu     sync.RWMutex
	meters map[string]*meter.Meter
}

func NewInMemoryMeterStore() *InMemoryMeterRepository {
	return &InMemoryMeterRepository{
		meters: make(map[string]*meter.Meter),
	}
}

func (s *InMemoryMeterRepository) CreateMeter(ctx context.Context, m *meter.Meter) error {
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

func (s *InMemoryMeterRepository) GetMeter(ctx context.Context, id string) (*meter.Meter, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	m, exists := s.meters[id]
	if !exists {
		return nil, fmt.Errorf("meter not found")
	}
	return m, nil
}

func (s *InMemoryMeterRepository) GetAllMeters(ctx context.Context) ([]*meter.Meter, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	meters := make([]*meter.Meter, 0, len(s.meters))
	for _, m := range s.meters {
		meters = append(meters, m)
	}
	return meters, nil
}

func (s *InMemoryMeterRepository) DisableMeter(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	m, exists := s.meters[id]
	if !exists {
		return fmt.Errorf("meter not found")
	}

	m.Status = types.StatusDeleted
	return nil
}

func (s *InMemoryMeterRepository) UpdateMeter(ctx context.Context, id string, filters []meter.Filter) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if id == "" {
		return fmt.Errorf("id is required")
	}

	// Find the meter by ID
	m, exists := s.meters[id]
	if !exists {
		return fmt.Errorf("meter not found")
	}

	if m.Filters == nil {
		m.Filters = []meter.Filter{}
	}

	// Merge new filters into the existing filters
	existingFilters := map[string][]string{}
	for _, f := range m.Filters {
		existingFilters[f.Key] = f.Values
	}

	for _, newFilter := range filters {
		if _, exists := existingFilters[newFilter.Key]; !exists {
			// If the key doesn't exist, add the entire filter
			existingFilters[newFilter.Key] = newFilter.Values
		} else {
			// Append new values for an existing key, avoiding duplicates
			for _, newValue := range newFilter.Values {
				if !contains(existingFilters[newFilter.Key], newValue) {
					existingFilters[newFilter.Key] = append(existingFilters[newFilter.Key], newValue)
				}
			}
		}
	}

	// Update the meter's filters
	updatedFilters := []meter.Filter{}
	for key, values := range existingFilters {
		updatedFilters = append(updatedFilters, meter.Filter{
			Key:    key,
			Values: values,
		})
	}

	m.Filters = updatedFilters
	return nil
}

// Helper function to check if a slice contains a specific value
func contains(slice []string, value string) bool {
	for _, v := range slice {
		if v == value {
			return true
		}
	}
	return false
}
