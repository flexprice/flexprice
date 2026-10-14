package tenantmembership

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
)

// Membership is a user's membership in a single tenant.
type Membership struct {
	ID     string   `json:"id"`
	UserID string   `json:"user_id"`
	Roles  []string `json:"roles"`
	types.BaseModel
}

// New creates a published membership for the given user and tenant.
func New(userID, tenantID string, roles []string) *Membership {
	now := time.Now().UTC()
	return &Membership{
		ID:     types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TENANT_MEMBERSHIP),
		UserID: userID,
		Roles:  roles,
		BaseModel: types.BaseModel{
			TenantID:  tenantID,
			Status:    types.StatusPublished,
			CreatedBy: types.DefaultUserID,
			UpdatedBy: types.DefaultUserID,
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
}

// FromEnt converts an ent TenantMembership to a domain Membership.
func FromEnt(e *ent.TenantMembership) *Membership {
	if e == nil {
		return nil
	}
	return &Membership{
		ID:     e.ID,
		UserID: e.UserID,
		Roles:  e.Roles,
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
		},
	}
}

// FromEntList converts a list of ent TenantMemberships to domain Memberships.
func FromEntList(rows []*ent.TenantMembership) []*Membership {
	if rows == nil {
		return nil
	}
	result := make([]*Membership, len(rows))
	for i, row := range rows {
		result[i] = FromEnt(row)
	}
	return result
}
