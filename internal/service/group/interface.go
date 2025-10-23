package group

import "context"

// GroupEntityManager defines the contract for group-related entity operations
type GroupEntityManager interface {
	// ValidateForGroup validates that entity IDs exist and are not already in another group
	ValidateForGroup(ctx context.Context, ids []string, excludeGroupID string) ([]string, error)

	// GetEntitiesInGroup retrieves all entity IDs associated with a group
	GetEntitiesInGroup(ctx context.Context, groupID string) ([]string, error)

	// GetEntitiesInGroups retrieves all entity IDs associated with multiple groups in a single query
	GetEntitiesInGroups(ctx context.Context, groupIDs []string) (map[string][]string, error)

	// AddToGroup associates entities with a group
	AddToGroup(ctx context.Context, groupID string, ids []string) error

	// RemoveFromGroup removes entities from their current group
	RemoveFromGroup(ctx context.Context, ids []string) error
}
