package addonassociation

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for addon association repository operations
type Repository interface {
	// AddonAssociation CRUD operations
	Create(ctx context.Context, addonAssociation *AddonAssociation) error
	GetByID(ctx context.Context, id string) (*AddonAssociation, error)
	Update(ctx context.Context, addonAssociation *AddonAssociation) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, filter *types.AddonAssociationFilter) ([]*AddonAssociation, error)
	Count(ctx context.Context, filter *types.AddonAssociationFilter) (int, error)

	CreateBulk(ctx context.Context, addonAssociations []*AddonAssociation) error
	GetByIDs(ctx context.Context, ids []string) ([]*AddonAssociation, error)

	// CancelBulk ends attachments that went live; DeleteBulk archives ones that never did.
	CancelBulk(ctx context.Context, ids []string, effectiveAt time.Time, reason string) error
	ActivateBulk(ctx context.Context, ids []string) error
	DeleteBulk(ctx context.Context, ids []string) error
}
