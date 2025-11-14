package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainGroup "github.com/flexprice/flexprice/internal/domain/group"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// GroupService interface defines the business logic for group management
type GroupService interface {
	CreateGroup(ctx context.Context, req dto.CreateGroupRequest) (*dto.GroupResponse, error)
	GetGroup(ctx context.Context, id string) (*dto.GroupResponse, error)
	DeleteGroup(ctx context.Context, id string) error
	ListGroups(ctx context.Context, filter *types.GroupFilter) (*dto.ListGroupsResponse, error)
	ValidateGroup(ctx context.Context, id string, entityType types.GroupEntityType) error
	ValidateGroupBulk(ctx context.Context, groupIDs []string, entityType types.GroupEntityType) error
}

type groupService struct {
	ServiceParams
}

func NewGroupService(params ServiceParams) GroupService {
	return &groupService{
		ServiceParams: params,
	}
}

// CreateGroup creates a new group with associated entities
func (s *groupService) CreateGroup(ctx context.Context, req dto.CreateGroupRequest) (*dto.GroupResponse, error) {

	if err := req.Validate(); err != nil {
		s.Logger.Warn("invalid group creation request",
			"error", err,
			"name", req.Name,
		)
		return nil, err
	}

	var result *dto.GroupResponse

	// Execute all operations within a transaction for atomicity
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		// Convert request to group domain object
		groupObj, err := req.ToGroup(txCtx)
		if err != nil {
			return err
		}

		if err := s.GroupRepo.Create(txCtx, groupObj); err != nil {
			return err
		}

		result = dto.ToGroupResponse(groupObj)

		return nil
	})
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to create group").
			Mark(ierr.ErrDatabase)
	}
	return result, nil
}

// GetGroup retrieves a group by ID
func (s *groupService) GetGroup(ctx context.Context, id string) (*dto.GroupResponse, error) {
	groupObj, err := s.GroupRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Get associated entities based on type
	entityIDs, err := s.getAssociatedEntities(ctx, groupObj.EntityType, id)
	if err != nil {
		return nil, err
	}

	return dto.ToGroupResponseWithEntities(groupObj, entityIDs), nil
}

// DeleteGroup deletes a group (entity associations are automatically removed by foreign key constraint)
func (s *groupService) DeleteGroup(ctx context.Context, id string) error {

	// Execute all operations within a transaction for atomicity
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		groupObj, err := s.GroupRepo.Get(txCtx, id)
		if err != nil {
			s.Logger.Error("failed to fetch group for deletion", "error", err, "group_id", id)
			return err
		}

		if err := s.disassociateEntitiesByGroupID(txCtx, groupObj.EntityType, id); err != nil {
			return err
		}

		return s.GroupRepo.Delete(txCtx, id)
	})

	if err != nil {
		return err
	}

	return nil
}

// ListGroups retrieves groups with optional filtering
func (s *groupService) ListGroups(ctx context.Context, filter *types.GroupFilter) (*dto.ListGroupsResponse, error) {
	if filter == nil {
		filter = &types.GroupFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	if err := filter.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation)
	}

	groups, err := s.GroupRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	total, err := s.GroupRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	responses := make([]*dto.GroupResponse, len(groups))
	for i, groupObj := range groups {
		responses[i] = dto.ToGroupResponse(groupObj)
	}

	return &dto.ListGroupsResponse{
		Items:      responses,
		Pagination: types.NewPaginationResponse(total, filter.GetLimit(), filter.GetOffset()),
	}, nil
}

// getAssociatedEntities gets entities associated with a group by querying the appropriate repository
func (s *groupService) getAssociatedEntities(ctx context.Context, entityType types.GroupEntityType, groupID string) ([]string, error) {
	// Query entities based on entity type
	switch entityType {
	case types.GroupEntityTypePrice:
		prices, err := s.PriceRepo.GetByGroupIDs(ctx, []string{groupID})
		if err != nil {
			return nil, err
		}
		entityIDs := make([]string, len(prices))
		for i, price := range prices {
			entityIDs[i] = price.ID
		}
		return entityIDs, nil
	default:
		return nil, ierr.NewError("unsupported entity type").
			WithHint("Unsupported entity type: " + entityType.String()).
			Mark(ierr.ErrValidation)
	}
}

// getAssociatedEntitiesBulk gets entities associated with multiple groups in bulk
func (s *groupService) getAssociatedEntitiesBulk(ctx context.Context, groups []*domainGroup.Group) (map[string][]string, error) {
	if len(groups) == 0 {
		return make(map[string][]string), nil
	}

	// Group by entity type to handle different entity types
	groupsByType := make(map[types.GroupEntityType][]string)
	for _, group := range groups {
		groupsByType[group.EntityType] = append(groupsByType[group.EntityType], group.ID)
	}

	// Fetch entities for each type in bulk
	result := make(map[string][]string)
	for entityType, groupIDs := range groupsByType {
		switch entityType {
		case types.GroupEntityTypePrice:
			prices, err := s.PriceRepo.GetByGroupIDs(ctx, groupIDs)
			if err != nil {
				return nil, err
			}
			// Group prices by group ID
			entityIDsByGroup := make(map[string][]string)
			for _, price := range prices {
				// Get the group ID from the price
				// Note: This requires the Price domain to have a GroupID field
				groupID := price.GroupID
				entityIDsByGroup[groupID] = append(entityIDsByGroup[groupID], price.ID)
			}
			// Merge results
			for groupID, entityIDs := range entityIDsByGroup {
				result[groupID] = entityIDs
			}
		default:
			return nil, ierr.NewError("unsupported entity type").
				WithHint("Unsupported entity type: " + entityType.String()).
				Mark(ierr.ErrValidation)
		}
	}

	return result, nil
}

// disassociateEntitiesByGroupID removes group associations from all entities in a group
func (s *groupService) disassociateEntitiesByGroupID(ctx context.Context, entityType types.GroupEntityType, groupID string) error {
	switch entityType {
	case types.GroupEntityTypePrice:
		return s.PriceRepo.ClearByGroupID(ctx, groupID)
	default:
		return ierr.NewError("unsupported entity type").
			WithHint("Unsupported entity type: " + entityType.String()).
			Mark(ierr.ErrValidation)
	}
}

func (s *groupService) ValidateGroup(ctx context.Context, id string, entityType types.GroupEntityType) error {
	group, err := s.GroupRepo.Get(ctx, id)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to fetch group for validation").
			Mark(ierr.ErrDatabase)
	}
	if group.EntityType != entityType {
		return ierr.NewError("invalid group type").
			WithHintf("Group must be of type: %s", entityType.String()).
			WithReportableDetails(map[string]any{
				"group_id":    id,
				"entity_type": entityType,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (s *groupService) ValidateGroupBulk(ctx context.Context, groupIDs []string, entityType types.GroupEntityType) error {
	groups, err := s.GroupRepo.List(ctx, &types.GroupFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		GroupIDs:    groupIDs,
	})
	if err != nil {
		return err
	}
	if len(groups) != len(groupIDs) {
		return ierr.NewError("some groups not found").
			WithHint("Some groups not found").
			Mark(ierr.ErrValidation)
	}
	for _, group := range groups {
		if group.EntityType != entityType {
			return ierr.NewError("invalid group type").
				WithHintf("Group must be of type: %s", entityType.String()).
				WithReportableDetails(map[string]any{
					"group_id":    group.ID,
					"entity_type": entityType,
				}).
				Mark(ierr.ErrValidation)
		}
	}
	if len(groups) == 0 {
		return ierr.NewError("no groups found").
			WithHint("No groups found").
			Mark(ierr.ErrValidation)
	}
	return nil
}
