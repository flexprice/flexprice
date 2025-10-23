package service

import (
	"context"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/api/dto"
	domainGroup "github.com/flexprice/flexprice/internal/domain/group"
	"github.com/flexprice/flexprice/internal/domain/price"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service/group"
	"github.com/flexprice/flexprice/internal/types"
)

// GroupService interface defines the business logic for group management
type GroupService interface {
	CreateGroup(ctx context.Context, req dto.CreateGroupRequest) (*dto.GroupResponse, error)
	GetGroup(ctx context.Context, id string) (*dto.GroupResponse, error)
	DeleteGroup(ctx context.Context, id string) error
	ListGroups(ctx context.Context, filter *types.GroupFilter) (*dto.ListGroupsResponse, error)
	AddEntityToGroup(ctx context.Context, id string, req dto.AddEntityToGroupRequest) error
}

type groupService struct {
	ServiceParams
	validators map[types.GroupEntityType]group.GroupEntityManager
	log        *logger.Logger
}

func NewGroupService(params ServiceParams, priceRepo price.Repository, log *logger.Logger) GroupService {
	validators := make(map[types.GroupEntityType]group.GroupEntityManager)
	validators[types.GroupEntityTypePrice] = group.NewPriceGroupManager(priceRepo, log)

	return &groupService{
		ServiceParams: params,
		validators:    validators,
		log:           log,
	}
}

// CreateGroup creates a new group with associated entities
func (s *groupService) CreateGroup(ctx context.Context, req dto.CreateGroupRequest) (*dto.GroupResponse, error) {

	if err := req.Validate(); err != nil {
		s.log.Warn("invalid group creation request",
			"error", err,
			"name", req.Name,
		)
		return nil, err
	}

	var result *dto.GroupResponse

	// Execute all operations within a transaction for atomicity
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		// Check if group with same lookup key already exists
		existingByLookup, err := s.GroupRepo.GetByLookupKey(txCtx, req.LookupKey)
		if err != nil && !ent.IsNotFound(err) {
			return err
		}
		if existingByLookup != nil {
			return ierr.NewError("group with lookup key already exists").
				WithHint("A group with this lookup key already exists").
				WithReportableDetails(map[string]interface{}{
					"lookup_key":        req.LookupKey,
					"existing_group_id": existingByLookup.ID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}

		// Convert request to group domain object
		groupObj, err := req.ToGroup(txCtx)
		if err != nil {
			return err
		}

		// Validate and associate entities based on type
		var entityIDs []string
		if len(req.EntityIDs) > 0 {
			entityIDs, err = s.validateEntities(txCtx, groupObj.EntityType, req.EntityIDs, "")
			if err != nil {
				return err
			}
		}

		// Create group
		if err := s.GroupRepo.Create(txCtx, groupObj); err != nil {
			return err
		}

		// Associate entities with group if provided
		if len(req.EntityIDs) > 0 {
			if err := s.associateEntities(txCtx, groupObj.EntityType, groupObj.ID, req.EntityIDs); err != nil {
				return err
			}
		}

		result = dto.NewGroupResponse(groupObj, entityIDs)
		return nil
	})

	if err != nil {
		return nil, err
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

	return dto.NewGroupResponse(groupObj, entityIDs), nil
}

// DeleteGroup deletes a group (entity associations are automatically removed by foreign key constraint)
func (s *groupService) DeleteGroup(ctx context.Context, id string) error {

	// Execute all operations within a transaction for atomicity
	err := s.DB.WithTx(ctx, func(txCtx context.Context) error {
		groupObj, err := s.GroupRepo.Get(txCtx, id)
		if err != nil {
			s.log.Error("failed to fetch group for deletion", "error", err, "group_id", id)
			return err
		}

		entityIDs, err := s.getAssociatedEntities(txCtx, groupObj.EntityType, id)
		if err != nil {
			s.log.Error("failed to get associated entities", "error", err, "group_id", id)
			return err
		}
		if len(entityIDs) > 0 {
			if err := s.disassociateEntities(txCtx, groupObj.EntityType, entityIDs); err != nil {
				return err
			}
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

	// Get associated entities for all groups in bulk (single query instead of N queries)
	entityMap, err := s.getAssociatedEntitiesBulk(ctx, groups)
	if err != nil {
		return nil, err
	}

	responses := make([]*dto.GroupResponse, len(groups))
	for i, groupObj := range groups {
		entityIDs := entityMap[groupObj.ID] // O(1) lookup
		responses[i] = dto.NewGroupResponse(groupObj, entityIDs)
	}

	return &dto.ListGroupsResponse{
		Items:      responses,
		Pagination: types.NewPaginationResponse(total, filter.GetLimit(), filter.GetOffset()),
	}, nil
}

// AddEntityToGroup adds an entity to a group
func (s *groupService) AddEntityToGroup(ctx context.Context, groupID string, req dto.AddEntityToGroupRequest) error {

	if err := req.Validate(); err != nil {
		return err
	}

	groupObj, err := s.GroupRepo.Get(ctx, groupID)
	if err != nil {
		return err
	}

	entityIDs, err := s.validateEntities(ctx, groupObj.EntityType, req.EntityIDs, groupID)
	if err != nil {
		return err
	}

	return s.associateEntities(ctx, groupObj.EntityType, groupID, entityIDs)
}

// validateEntities validates entities using the appropriate validator
func (s *groupService) validateEntities(ctx context.Context, entityType types.GroupEntityType, entityIDs []string, excludeGroupID string) ([]string, error) {
	validator, exists := s.validators[entityType]
	if !exists {
		return nil, ierr.NewError("unsupported entity type").
			WithHint("Unsupported entity type: " + entityType.String()).
			Mark(ierr.ErrValidation)
	}

	return validator.ValidateForGroup(ctx, entityIDs, excludeGroupID)
}

// getAssociatedEntities gets entities associated with a group using the appropriate validator
func (s *groupService) getAssociatedEntities(ctx context.Context, entityType types.GroupEntityType, groupID string) ([]string, error) {
	validator, exists := s.validators[entityType]
	if !exists {
		return nil, ierr.NewError("unsupported entity type").
			WithHint("Unsupported entity type: " + entityType.String()).
			Mark(ierr.ErrValidation)
	}

	return validator.GetEntitiesInGroup(ctx, groupID)
}

// getAssociatedEntitiesBulk gets entities associated with multiple groups in bulk
func (s *groupService) getAssociatedEntitiesBulk(ctx context.Context, groups []*domainGroup.Group) (map[string][]string, error) {
	if len(groups) == 0 {
		return make(map[string][]string), nil
	}

	// Group by entity type to handle different validators
	groupsByType := make(map[types.GroupEntityType][]string)
	for _, group := range groups {
		groupsByType[group.EntityType] = append(groupsByType[group.EntityType], group.ID)
	}

	// Fetch entities for each type in bulk
	result := make(map[string][]string)
	for entityType, groupIDs := range groupsByType {
		validator, exists := s.validators[entityType]
		if !exists {
			return nil, ierr.NewError("unsupported entity type").
				WithHint("Unsupported entity type: " + entityType.String()).
				Mark(ierr.ErrValidation)
		}

		typeResult, err := validator.GetEntitiesInGroups(ctx, groupIDs)
		if err != nil {
			return nil, err
		}

		// Merge results
		for groupID, entityIDs := range typeResult {
			result[groupID] = entityIDs
		}
	}

	return result, nil
}

// associateEntities associates entities with a group using the appropriate validator
func (s *groupService) associateEntities(ctx context.Context, entityType types.GroupEntityType, groupID string, entityIDs []string) error {
	validator, exists := s.validators[entityType]
	if !exists {
		s.log.Error("unsupported entity type", "entity_type", entityType)
		return ierr.NewError("unsupported entity type").
			WithHint("Unsupported entity type: " + entityType.String()).
			Mark(ierr.ErrValidation)
	}

	return validator.AddToGroup(ctx, groupID, entityIDs)
}

// disassociateEntities removes group associations from entities using the appropriate validator
func (s *groupService) disassociateEntities(ctx context.Context, entityType types.GroupEntityType, entityIDs []string) error {
	validator, exists := s.validators[entityType]
	if !exists {
		return ierr.NewError("unsupported entity type").
			WithHint("Unsupported entity type: " + entityType.String()).
			Mark(ierr.ErrValidation)
	}

	return validator.RemoveFromGroup(ctx, entityIDs)
}
