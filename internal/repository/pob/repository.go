package pob

import (
	"context"

	domainPOB "github.com/flexprice/flexprice/internal/domain/pob"
)

// Repository persists performance obligations and their associations.
type Repository interface {
	CreateWithAssociation(ctx context.Context, p *domainPOB.PerformanceObligation, a *domainPOB.Association) error
	CreateAssociation(ctx context.Context, a *domainPOB.Association) error
	GetByID(ctx context.Context, id string) (*domainPOB.PerformanceObligation, error)
	GetByLineItemID(ctx context.Context, lineItemID string) (*domainPOB.PerformanceObligation, error)
	ListAssociations(ctx context.Context, pobID string) ([]*domainPOB.Association, error)
	Update(ctx context.Context, p *domainPOB.PerformanceObligation) error
	List(ctx context.Context, filter *Filter) ([]*domainPOB.PerformanceObligation, error)
	Count(ctx context.Context, filter *Filter) (int, error)
}
