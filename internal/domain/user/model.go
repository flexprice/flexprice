package user

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// UserType represents the type of user
type UserType string

const (
	UserTypeUser           UserType = "user"
	UserTypeServiceAccount UserType = "service_account"
)

// Validate validates the user type
func (ut UserType) Validate() error {
	switch ut {
	case UserTypeUser, UserTypeServiceAccount:
		return nil
	default:
		return ierr.NewError("invalid user type").
			WithHint("User type must be 'user' or 'service_account'").
			Mark(ierr.ErrValidation)
	}
}

type User struct {
	ID    string   `json:"id"`
	Email string   `json:"email"` // Empty for service accounts
	Type  string   `json:"type"`
	Roles []string `json:"roles"`
	types.BaseModel
}

func NewUser(email, tenantID string) *User {
	return &User{
		ID:    types.GenerateUUIDWithPrefix(types.UUID_PREFIX_USER),
		Email: email,
		Type:  "user",
		Roles: []string{},
		BaseModel: types.BaseModel{
			TenantID:  tenantID,
			Status:    types.StatusPublished,
			CreatedBy: types.DefaultUserID,
			UpdatedBy: types.DefaultUserID,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
}

// FromEnt converts an ent User to a domain User
func FromEnt(e *ent.User) *User {
	if e == nil {
		return nil
	}

	return &User{
		ID:    e.ID,
		Email: lo.FromPtrOr(e.Email, ""),
		Type:  e.Type,
		Roles: e.Roles,
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

// FromEntList converts a list of ent Users to domain Users
func FromEntList(users []*ent.User) []*User {
	if users == nil {
		return nil
	}

	result := make([]*User, len(users))
	for i, u := range users {
		result[i] = FromEnt(u)
	}

	return result
}
