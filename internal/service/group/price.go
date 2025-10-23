package group

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/price"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

// PriceGroupManager implements GroupEntityManager for prices
type PriceGroupManager struct {
	priceRepo price.Repository
	log       *logger.Logger
}

// NewPriceGroupManager creates a new price group manager
func NewPriceGroupManager(priceRepo price.Repository, log *logger.Logger) GroupEntityManager {
	return &PriceGroupManager{
		priceRepo: priceRepo,
		log:       log,
	}
}

// ValidateForGroup validates that price IDs exist and are not already in another group
func (m *PriceGroupManager) ValidateForGroup(ctx context.Context, ids []string, excludeGroupID string) ([]string, error) {
	// Validate prices exist using List method
	filter := &types.PriceFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		PriceIDs:    ids,
	}

	prices, err := m.priceRepo.List(ctx, filter)
	if err != nil {
		m.log.Error("failed to list prices", "error", err, "price_ids", ids)
		return nil, err
	}

	if len(prices) != len(ids) {
		m.log.Warn("some prices not found", "expected_count", len(ids), "actual_count", len(prices))
		return nil, ierr.NewError("one or more prices not found").
			WithHint("One or more price IDs are invalid").
			Mark(ierr.ErrValidation)
	}

	// Check if any prices are already in other groups by iterating over the list
	for _, price := range prices {
		if price.GroupID != "" && price.GroupID != excludeGroupID {
			m.log.Warn("price already in another group", "price_id", price.ID, "group_id", price.GroupID)
			return nil, ierr.NewError("one or more prices are already in another group").
				WithHint("One or more prices are already assigned to another group").
				Mark(ierr.ErrValidation)
		}
	}

	return ids, nil
}

// GetEntitiesInGroup retrieves all price IDs associated with a group
func (m *PriceGroupManager) GetEntitiesInGroup(ctx context.Context, groupID string) ([]string, error) {
	prices, err := m.priceRepo.GetByGroupIDs(ctx, []string{groupID})
	if err != nil {
		m.log.Error("failed to get prices in group", "error", err, "group_id", groupID)
		return nil, err
	}

	priceIDs := make([]string, len(prices))
	for i, price := range prices {
		priceIDs[i] = price.ID
	}

	m.log.Debug("retrieved associated prices", "group_id", groupID, "price_count", len(priceIDs))
	return priceIDs, nil
}

// GetEntitiesInGroups retrieves all price IDs associated with multiple groups in a single query
func (m *PriceGroupManager) GetEntitiesInGroups(ctx context.Context, groupIDs []string) (map[string][]string, error) {
	if len(groupIDs) == 0 {
		return make(map[string][]string), nil
	}

	prices, err := m.priceRepo.GetByGroupIDs(ctx, groupIDs)
	if err != nil {
		m.log.Error("failed to get prices by group IDs", "error", err, "group_ids", groupIDs)
		return nil, err
	}

	// Group prices by group_id
	result := make(map[string][]string)
	for _, price := range prices {
		if price.GroupID != "" {
			result[price.GroupID] = append(result[price.GroupID], price.ID)
		}
	}

	// Ensure all groups have entries (even if empty)
	for _, groupID := range groupIDs {
		if _, exists := result[groupID]; !exists {
			result[groupID] = []string{}
		}
	}

	m.log.Debug("retrieved associated prices in bulk", "group_count", len(groupIDs), "total_prices", len(prices))
	return result, nil
}

// AddToGroup associates prices with a group
func (m *PriceGroupManager) AddToGroup(ctx context.Context, groupID string, ids []string) error {
	m.log.Debug("associating prices with group", "group_id", groupID, "price_ids", ids)
	return m.priceRepo.UpdateGroupIDBulk(ctx, ids, &groupID)
}

// RemoveFromGroup removes prices from their current group
func (m *PriceGroupManager) RemoveFromGroup(ctx context.Context, ids []string) error {
	m.log.Debug("disassociating prices from groups", "price_ids", ids)
	return m.priceRepo.ClearGroupIDBulk(ctx, ids)
}
