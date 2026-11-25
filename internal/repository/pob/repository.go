package pob

import (
	"context"

	domainPOB "github.com/flexprice/flexprice/internal/domain/pob"
)

// Repository persists performance obligations.
type Repository interface {
	Create(ctx context.Context, p *domainPOB.PerformanceObligation) error
	GetByID(ctx context.Context, id string) (*domainPOB.PerformanceObligation, error)
	Update(ctx context.Context, p *domainPOB.PerformanceObligation) error
	List(ctx context.Context, filter *Filter) ([]*domainPOB.PerformanceObligation, error)
	Count(ctx context.Context, filter *Filter) (int, error)
}
