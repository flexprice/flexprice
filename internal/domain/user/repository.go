package user

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

type Repository interface {
	Create(ctx context.Context, user *User) error
	GetByID(ctx context.Context, id string) (*User, error)
	// GetByIDGlobal loads a user by ID without filtering on users.tenant_id.
	// Use with a membership check when the user may belong to multiple tenants.
	GetByIDGlobal(ctx context.Context, id string) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	Update(ctx context.Context, user *User) error
	UpdateRoles(ctx context.Context, id string, roles []string) error
	Delete(ctx context.Context, id string) error
	// ArchiveByID archives a user identity regardless of users.tenant_id.
	ArchiveByID(ctx context.Context, id string) error
	ListByFilter(ctx context.Context, filter *types.UserFilter) ([]*User, int64, error)
}
