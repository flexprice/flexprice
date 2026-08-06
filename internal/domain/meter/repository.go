package meter

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

type Repository interface {
	CreateMeter(ctx context.Context, meter *Meter) error
	GetMeter(ctx context.Context, id string) (*Meter, error)
	// ListByIDs resolves meters by ID through the meter cache, issuing a single
	// query for the misses. Order is not guaranteed and missing IDs are omitted.
	ListByIDs(ctx context.Context, ids []string) ([]*Meter, error)
	List(ctx context.Context, filter *types.MeterFilter) ([]*Meter, error)
	ListAll(ctx context.Context, filter *types.MeterFilter) ([]*Meter, error)
	Count(ctx context.Context, filter *types.MeterFilter) (int, error)
	DisableMeter(ctx context.Context, id string) error
	UpdateMeter(ctx context.Context, id string, filters []Filter) error

	// GetMatchingMetersByEventName returns published meters matching eventName for current tenant/environment.
	GetMatchingMetersByEventName(ctx context.Context, eventName string) ([]*Meter, error)
}
