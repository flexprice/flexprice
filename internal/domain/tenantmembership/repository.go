package tenantmembership

import "context"

// Repository persists tenant memberships.
type Repository interface {
	Create(ctx context.Context, membership *Membership) error
	GetByUserAndTenant(ctx context.Context, userID, tenantID string) (*Membership, error)
	ListByUser(ctx context.Context, userID string) ([]*Membership, error)
	ListByTenant(ctx context.Context, tenantID string) ([]*Membership, error)
	UpdateRoles(ctx context.Context, userID, tenantID string, roles []string) error
	Delete(ctx context.Context, userID, tenantID string) error
	CountByTenant(ctx context.Context, tenantID string) (int64, error)
	CountByUser(ctx context.Context, userID string) (int64, error)
}
