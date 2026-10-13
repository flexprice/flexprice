package dto

import (
	"github.com/flexprice/flexprice/internal/types"
)

// Always present on a create response, so a caller that ignores Status cannot
// mistake a conflict for a success. On FailedAlreadyExists nothing was created
// and EntityId is the pre-existing entity that blocked the request, not the
// caller's; on every other status it is the entity the call produced.
type EntityCreationResult struct {
	Status   types.EntityCreationStatus `json:"status"`
	EntityId string                     `json:"entity_id"`
}

type EntityCreationOptions struct {
	EntityCreationConflictPolicies *EntityCreationConflictPolicies `json:"entity_creation_conflict_policies,omitempty"`
}

type EntityCreationConflictPolicies struct {
	OnExistingEntity types.OnExistingEntityPolicy `json:"on_existing_entity,omitempty"`
}

func (c *EntityCreationConflictPolicies) Validate() error {
	if c == nil {
		return nil
	}
	return c.OnExistingEntity.Validate()
}

func (o *EntityCreationOptions) Validate() error {
	if o == nil {
		return nil
	}
	return o.EntityCreationConflictPolicies.Validate()
}

// A nil options block, nil policies, or an omitted policy all resolve to reject.
func (o *EntityCreationOptions) Policy() types.OnExistingEntityPolicy {
	if o == nil || o.EntityCreationConflictPolicies == nil || o.EntityCreationConflictPolicies.OnExistingEntity == "" {
		return types.OnExistingEntityPolicyReject
	}
	return o.EntityCreationConflictPolicies.OnExistingEntity
}
