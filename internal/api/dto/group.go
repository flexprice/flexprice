package dto

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/group"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

// CreateGroupRequest represents the request to create a group
type CreateGroupRequest struct {
	Name       string `json:"name" validate:"required"`
	EntityType string `json:"entity_type" validate:"required"`
	LookupKey  string `json:"lookup_key" validate:"required"`
}

func (r *CreateGroupRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	// Validate entity type
	entityType := types.GroupEntityType(r.EntityType)
	if err := entityType.Validate(); err != nil {
		return err
	}

	return nil
}

func (r *CreateGroupRequest) ToGroup(ctx context.Context) (*group.Group, error) {
	entityType := types.GroupEntityType(r.EntityType)
	return &group.Group{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_GROUP),
		Name:          r.Name,
		EntityType:    entityType,
		EnvironmentID: types.GetEnvironmentID(ctx),
		LookupKey:     r.LookupKey,
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}, nil
}

// GroupResponse represents the group response
type GroupResponse struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	LookupKey  string            `json:"lookup_key"`
	EntityType string            `json:"entity_type"`
	EntityIDs  []string          `json:"entity_ids"`
	Status     string            `json:"status"`
	Metadata   map[string]string `json:"metadata"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

// ListGroupsResponse represents the response for listing groups
type ListGroupsResponse = types.ListResponse[*GroupResponse]

func ToGroupResponse(group *group.Group) *GroupResponse {
	return &GroupResponse{
		ID:         group.ID,
		Name:       group.Name,
		LookupKey:  group.LookupKey,
		EntityType: string(group.EntityType),
		EntityIDs:  []string{},
		Status:     string(group.Status),
		Metadata:   group.Metadata,
		CreatedAt:  group.CreatedAt,
		UpdatedAt:  group.UpdatedAt,
	}
}

func ToGroupResponseWithEntities(group *group.Group, entityIDs []string) *GroupResponse {
	return &GroupResponse{
		ID:         group.ID,
		Name:       group.Name,
		LookupKey:  group.LookupKey,
		EntityType: string(group.EntityType),
		EntityIDs:  entityIDs,
		Status:     string(group.Status),
		Metadata:   group.Metadata,
		CreatedAt:  group.CreatedAt,
		UpdatedAt:  group.UpdatedAt,
	}
}
